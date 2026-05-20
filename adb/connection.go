/**
# @Datetime : 2026/5/19 21:27
# @Project  : adb-go
# @File     : connection.go
# @Author   : 眷恋阳阳
*/

package adb

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
)

// Connection 管理与 ADB 守护进程之间的网络连接。
// 负责握手认证、TLS 加密升级以及多路流管理。
type Connection struct {
	conn       net.Conn        // 底层物理连接（可能是原始 TCP 或 TLS 包装后的连接）
	connMu     sync.RWMutex    // 保护 conn 的读写锁
	privKey    *rsa.PrivateKey // RSA 私钥，用于签名认证
	deviceName string          // 设备名称，用于向目标设备标识自身

	apiLevel int // Android API Level，影响协议版本和载荷限制
	maxData  int // 当前连接允许的最大载荷大小

	streams   map[uint32]*Stream // 活跃的流映射表，key 为本地方 ID
	streamsMu sync.RWMutex       // 保护 streams 的读写锁
	lastLocID atomic.Uint32      // 上一个分配的本地流 ID，递增分配

	cnxnResult    chan error // 连接结果通道，用于通知握手完成或失败
	sentSignature bool       // 是否已发送过 RSA 签名
	// 新增：用于通知连接断开的通道
	disconnected chan struct{}
}

// NewConnection 创建一个新的 ADB 连接包装器。
// conn: 已建立的 TCP 连接；privKey: RSA 私钥；deviceName: 设备标识名称；apiLevel: 目标 Android API Level。
func NewConnection(conn net.Conn, privKey *rsa.PrivateKey, deviceName string, apiLevel int) *Connection {
	return &Connection{
		conn:         conn,
		privKey:      privKey,
		deviceName:   deviceName,
		apiLevel:     apiLevel,
		maxData:      MaxPayloadV1,
		streams:      make(map[uint32]*Stream),
		cnxnResult:   make(chan error, 1),
		disconnected: make(chan struct{}),
	}
}

// Disconnected 暴露断开信号给上层
func (c *Connection) Disconnected() <-chan struct{} {
	return c.disconnected
}

// Connect 启动 ADB 握手流程和后台消息读取循环。
// 握手过程支持 Context 超时控制，超时后自动关闭连接。
func (c *Connection) Connect(ctx context.Context) error {
	go c.readLoop()

	c.sendPacket(GenerateConnect())

	select {
	case err := <-c.cnxnResult:
		return err
	case <-ctx.Done():
		c.closeAllStreams()
		return ctx.Err()
	}
}

// readLoop 后台消息读取循环，持续解析来自 ADB 守护进程的消息并分派处理。
// 这是连接的生命周期核心，会一直运行直到连接断开或出现不可恢复的错误。
func (c *Connection) readLoop() {

	// 只要 readLoop 退出（无论是异常断开还是主动关闭），都会关闭此通道，向所有监听者发送广播
	defer close(c.disconnected)

	protocolVersion := GetProtocolVersion(c.apiLevel)

	for {
		c.connMu.RLock()
		currentConn := c.conn
		c.connMu.RUnlock()

		msg, err := ParseMessage(currentConn, protocolVersion, c.maxData)
		if err != nil {
			LogErrorf("[TCP] 读取失败或连接断开: %v", err)
			c.closeAllStreams()

			select {
			case c.cnxnResult <- fmt.Errorf("连接中断: %w", err):
			default:
			}
			return
		}

		switch msg.Command {
		case CmdStls:
			LogInfof("[STLS] 收到 TLS 升级请求，准备切换加密通道...")

			stlsMsg := GenerateMessage(CmdStls, AStlsVersion, 0, nil)
			c.sendPacket(stlsMsg)

			cert, err := GenerateTLSCertificate(c.privKey, c.deviceName)
			if err != nil {
				LogErrorf("[STLS] 生成 TLS 证书失败: %v", err)
				c.closeAllStreams()
				return
			}

			tlsConfig := &tls.Config{
				Certificates:       []tls.Certificate{cert},
				InsecureSkipVerify: true,

				GetClientCertificate: func(cri *tls.CertificateRequestInfo) (*tls.Certificate, error) {
					return &cert, nil
				},

				MinVersion: tls.VersionTLS13,
				MaxVersion: tls.VersionTLS13,
			}

			tlsConn := tls.Client(currentConn, tlsConfig)
			if err := tlsConn.Handshake(); err != nil {
				LogErrorf("[STLS] TLS 握手失败: %v", err)
				c.closeAllStreams()
				return
			}

			c.connMu.Lock()
			c.conn = tlsConn
			c.connMu.Unlock()

			LogInfof("[STLS] TLS 握手成功，通道已加密，继续等待认证/连接...")
			continue

		case CmdAuth:
			if msg.Arg0 == AdbAuthToken {
				if !c.sentSignature {
					LogInfof("[AUTH] 收到认证令牌，尝试发送 RSA 签名...")
					signature, err := Sign(c.privKey, msg.Payload)
					if err != nil {
						LogErrorf("[AUTH] RSA 签名生成失败: %v", err)
						continue
					}

					authMsg := GenerateMessage(CmdAuth, AdbAuthSignature, 0, signature)
					c.sendPacket(authMsg)
					c.sentSignature = true
				} else {
					LogInfof("[AUTH] 签名未被识别，发送 RSA 公钥，请求设备弹窗授权...")
					pubKeyPayload, err := EncodePublicKeyWithName(&c.privKey.PublicKey, c.deviceName)
					if err != nil {
						LogErrorf("[AUTH] 编码 RSA 公钥失败: %v", err)
						continue
					}

					authMsg := GenerateMessage(CmdAuth, AdbAuthRsaPublicKey, 0, pubKeyPayload)
					c.sendPacket(authMsg)
				}
			}

		case CmdCnxn:
			LogInfof("[CNXN] 收到连接确认，握手成功！")
			c.maxData = int(msg.Arg1)
			LogInfof("最大载荷大小已更新为: %d 字节", c.maxData)

			select {
			case c.cnxnResult <- nil:
			default:
			}

		case CmdOkay, CmdWrte, CmdClse:
			c.streamsMu.RLock()
			stream, exists := c.streams[msg.Arg1]
			c.streamsMu.RUnlock()

			if !exists {
				continue
			}

			switch msg.Command {
			case CmdOkay:
				stream.handleOkay(msg.Arg0)
			case CmdWrte:
				stream.handleWrte(msg.Payload)
			case CmdClse:
				stream.handleClse()
				c.streamsMu.Lock()
				delete(c.streams, msg.Arg1)
				c.streamsMu.Unlock()
			}

		case CmdOpen:
			// adbd 主动要求打通流 (通常是因为 Reverse 转发)
			dest := string(bytes.TrimRight(msg.Payload, "\x00"))
			LogInfof("[OPEN] 收到设备端的反向连接请求: %s", dest)

			if strings.HasPrefix(dest, "tcp:") {
				port := strings.TrimPrefix(dest, "tcp:")

				remoteID := msg.Arg0
				localID := c.lastLocID.Add(1)

				stream := newStream(c, localID)
				stream.remoteID.Store(remoteID)

				// 🚀 【关键修复】：为反向流强制注入第一枚写入令牌！
				// 因为反向流是我们发 OKAY 给对面，我们自己不会收到初始 OKAY，
				// 如果不预填令牌，底层的 Stream.Write() 将永久阻塞卡死。
				stream.writeReady <- struct{}{}

				c.streamsMu.Lock()
				c.streams[localID] = stream
				c.streamsMu.Unlock()

				// 向设备回复 OKAY，表示同意建立这条流
				c.sendReady(localID, remoteID)

				// 异步去连接本地实际的服务并搬运数据
				go func(s *Stream, targetPort string) {
					defer s.Close()
					localConn, err := net.Dial("tcp", "127.0.0.1:"+targetPort)
					if err != nil {
						LogErrorf("[OPEN] 无法连接本地端口 %s: %v", targetPort, err)
						return
					}
					defer localConn.Close()

					errc := make(chan error, 2)
					go func() { _, err := io.Copy(s, localConn); errc <- err }()
					go func() { _, err := io.Copy(localConn, s); errc <- err }()
					<-errc
				}(stream, port)
			} else {
				// 不支持的协议，直接拒绝
				c.sendClose(0, msg.Arg0)
			}

		}
	}
}

// Open 向 ADB 守护进程发起一个服务请求并创建一条新的多路复用流。
// destination: 目标服务字符串，例如 "shell:ls -l" 或 "tcp:8080"。
// 返回的 Stream 可用于读写数据。
func (c *Connection) Open(ctx context.Context, destination string) (*Stream, error) {
	localID := c.lastLocID.Add(1)
	stream := newStream(c, localID)

	c.streamsMu.Lock()
	c.streams[localID] = stream
	c.streamsMu.Unlock()

	payload := append([]byte(destination), 0)
	c.sendPacket(GenerateMessage(CmdOpen, localID, 0, payload))

	select {
	case <-stream.writeReady:
		stream.writeReady <- struct{}{}
		return stream, nil
	case <-stream.closeCh:
		c.streamsMu.Lock()
		delete(c.streams, localID)
		c.streamsMu.Unlock()
		return nil, errors.New("远端主动拒绝了流的打开请求")
	case <-ctx.Done():
		stream.Close()
		c.streamsMu.Lock()
		delete(c.streams, localID)
		c.streamsMu.Unlock()
		return nil, ctx.Err()
	}
}

// sendPacket 底层数据包发送函数，线程安全。
// 自动记录发送的命令类型和数据包大小到日志。
func (c *Connection) sendPacket(data []byte) error {
	if len(data) >= 24 {
		cmdUint := binary.LittleEndian.Uint32(data[0:4])
		cmdStr := commandString(cmdUint)
		LogDebugf("[TCP发送] %s 包 (长度: %d 字节)", cmdStr, len(data))
	} else {
		LogDebugf("[TCP发送] 短数据包 (长度: %d 字节)", len(data))
	}

	c.connMu.RLock()
	currentConn := c.conn
	c.connMu.RUnlock()

	_, err := currentConn.Write(data)
	if err != nil {
		LogErrorf("[TCP] 发送失败: %v", err)
	}
	return err
}

// sendReady 发送 OKAY 确认包。
func (c *Connection) sendReady(localID, remoteID uint32) error {
	return c.sendPacket(GenerateMessage(CmdOkay, localID, remoteID, nil))
}

// sendWrite 发送 WRTE 数据包。
func (c *Connection) sendWrite(localID, remoteID uint32, data []byte) error {
	return c.sendPacket(GenerateMessage(CmdWrte, localID, remoteID, data))
}

// sendClose 发送 CLSE 关闭包。
func (c *Connection) sendClose(localID, remoteID uint32) error {
	return c.sendPacket(GenerateMessage(CmdClse, localID, remoteID, nil))
}

// closeAllStreams 关闭所有活跃的流，通常在连接断开时调用。
func (c *Connection) closeAllStreams() {
	c.streamsMu.Lock()
	for _, stream := range c.streams {
		stream.handleClse()
	}
	c.streamsMu.Unlock()
}

// removeStream 从活跃流集合中移除指定的流。
func (c *Connection) removeStream(localID uint32) {
	c.streamsMu.Lock()
	delete(c.streams, localID)
	c.streamsMu.Unlock()
}

// Close 关闭底层物理连接并清理所有流资源。
func (c *Connection) Close() error {
	c.closeAllStreams()
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
