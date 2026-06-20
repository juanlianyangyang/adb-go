/**
# @Datetime : 2026/5/19 21:27
# @Project  : adb-go
# @File     : client.go
# @Author   : 眷恋阳阳
*/

package adb

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

// Client 是对底层 Connection 的高级封装，提供面向开发者的便捷 API。
// 用户通过 Client 可以方便地执行 Shell 命令、进行端口转发、同步文件等操作。
type Client struct {
	conn *Connection
}

// Dial 拨号连接到指定的 ADB 守护进程并自动完成握手认证。
// ctx: 用于控制连接超时；address: 目标地址，格式如 "192.168.1.100:5555"；
// privKey: RSA 私钥用于认证；deviceName: 客户端标识名称。
// 返回已建立连接的 Client 实例，或错误。
func Dial(ctx context.Context, address string, privKey *rsa.PrivateKey, deviceName string) (*Client, error) {
	var dialer net.Dialer
	tcpConn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("无法连接到 %s: %w", address, err)
	}

	if tcpConn, ok := tcpConn.(*net.TCPConn); ok {
		tcpConn.SetNoDelay(true)
	}

	adbConn := NewConnection(tcpConn, privKey, deviceName, 30)

	LogInfof("正在连接到设备 %s ...", address)

	if err := adbConn.Connect(ctx); err != nil {
		tcpConn.Close()
		return nil, fmt.Errorf("ADB 握手失败: %w", err)
	}

	LogInfof("成功连接到设备 %s", address)
	return &Client{conn: adbConn}, nil
}

func FastStatusDial(ctx context.Context, address string, privKey *rsa.PrivateKey, deviceName string) (*FastConnectResult, error) {
	var dialer net.Dialer
	tcpConn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return &FastConnectResult{Status: ConnectStatusError}, fmt.Errorf("无法连接到 %s: %w", address, err)
	}

	if tcpConn, ok := tcpConn.(*net.TCPConn); ok {
		tcpConn.SetNoDelay(true)
	}
	adbConn := NewConnection(tcpConn, privKey, deviceName, 30)
	defer adbConn.Close()
	return adbConn.ProbeAuthStatus(ctx)
}

func (c *Client) Disconnected() <-chan struct{} {
	return c.conn.disconnected
}

// Close 关闭底层物理连接并释放所有资源。
func (c *Client) Close() error {
	LogInfof("正在断开与设备的连接...")
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// Shell 在设备上执行 shell 命令并返回双向数据流。
// args: 命令及其参数，例如 Shell(ctx, "ls", "-l")。
// 返回的 Stream 可用于读取命令输出和写入标准输入。
func (c *Client) Shell(ctx context.Context, args ...string) (*Stream, error) {
	dest, err := ServiceShell.GetDestination(args...)
	if err != nil {
		return nil, fmt.Errorf("构建 Shell 命令目标失败: %w", err)
	}
	return c.conn.Open(ctx, dest)
}

// TCPConnect 打开一个到设备内部端口的 TCP 连接，常用于端口转发。
// port: 目标端口号，例如 "8080"。
// 也可以传入 "host:port" 格式的地址。
func (c *Client) TCPConnect(ctx context.Context, port string) (*Stream, error) {
	dest, err := ServiceTcpConnect.GetDestination(port)
	if err != nil {
		return nil, fmt.Errorf("构建 TCP 连接目标失败: %w", err)
	}
	return c.conn.Open(ctx, dest)
}

// FileSync 请求同步服务，用于文件的 Push/Pull 操作。
// 返回的 Stream 可配合 sync 协议进行文件传输。
func (c *Client) FileSync(ctx context.Context) (*Stream, error) {
	dest, err := ServiceSync.GetDestination()
	if err != nil {
		return nil, fmt.Errorf("构建文件同步目标失败: %w", err)
	}
	return c.conn.Open(ctx, dest)
}

// Framebuffer 获取设备屏幕的帧缓冲流，可用于截屏。
func (c *Client) Framebuffer(ctx context.Context) (*Stream, error) {
	dest, err := ServiceFramebuffer.GetDestination()
	if err != nil {
		return nil, fmt.Errorf("构建帧缓冲目标失败: %w", err)
	}
	return c.conn.Open(ctx, dest)
}

// SaveFramebufferBMP 截取屏幕，剥离 ADB 头部，
// 并伪造一个 108 字节的 BMP V4 魔术头，直接指定 RGBA 通道顺序，完美解决阿凡达色问题。
func (c *Client) SaveFramebufferBMP(ctx context.Context, localPath string) error {
	stream, err := c.Framebuffer(ctx)
	if err != nil {
		return fmt.Errorf("打开 Framebuffer 流失败: %w", err)
	}
	defer stream.Close()

	// 1. 读取并跳过 ADB 私有头
	var version uint32
	binary.Read(stream, binary.LittleEndian, &version)

	var bpp, size, width, height uint32
	if version == 1 {
		binary.Read(stream, binary.LittleEndian, &bpp)
		binary.Read(stream, binary.LittleEndian, &size)
		binary.Read(stream, binary.LittleEndian, &width)
		binary.Read(stream, binary.LittleEndian, &height)
		io.CopyN(io.Discard, stream, 32)
	} else if version == 2 {
		var colorSpace uint32
		binary.Read(stream, binary.LittleEndian, &bpp)
		binary.Read(stream, binary.LittleEndian, &colorSpace)
		binary.Read(stream, binary.LittleEndian, &size)
		binary.Read(stream, binary.LittleEndian, &width)
		binary.Read(stream, binary.LittleEndian, &height)
		io.CopyN(io.Discard, stream, 32)
	} else {
		return fmt.Errorf("不支持的 Framebuffer 版本: %d", version)
	}

	if bpp != 32 {
		return fmt.Errorf("当前仅支持 32 位色深，设备色深: %d", bpp)
	}

	// 2. 创建本地文件
	file, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("创建 BMP 文件失败: %w", err)
	}
	defer file.Close()

	// ==========================================
	// 🌟 核心魔法：构建 108 字节的 BITMAPV4HEADER
	// ==========================================
	bmpHeader := new(bytes.Buffer)

	// 文件总大小 = 14 (文件头) + 108 (V4信息头) + 像素数据大小
	headerSize := uint32(122)
	fileSize := headerSize + size

	// --- BITMAPFILEHEADER (14 字节) ---
	bmpHeader.WriteString("BM")
	binary.Write(bmpHeader, binary.LittleEndian, fileSize)
	binary.Write(bmpHeader, binary.LittleEndian, uint16(0))
	binary.Write(bmpHeader, binary.LittleEndian, uint16(0))
	binary.Write(bmpHeader, binary.LittleEndian, headerSize) // 数据偏移量

	// --- BITMAPV4HEADER (108 字节) ---
	binary.Write(bmpHeader, binary.LittleEndian, uint32(108))    // bV4Size: 头大小
	binary.Write(bmpHeader, binary.LittleEndian, int32(width))   // bV4Width
	binary.Write(bmpHeader, binary.LittleEndian, int32(-height)) // bV4Height (负数代表从上往下渲染)
	binary.Write(bmpHeader, binary.LittleEndian, uint16(1))      // bV4Planes
	binary.Write(bmpHeader, binary.LittleEndian, uint16(32))     // bV4BitCount
	binary.Write(bmpHeader, binary.LittleEndian, uint32(3))      // bV4V4Compression: 3 代表 BI_BITFIELDS
	binary.Write(bmpHeader, binary.LittleEndian, size)           // bV4SizeImage
	binary.Write(bmpHeader, binary.LittleEndian, int32(2835))    // bV4XPelsPerMeter
	binary.Write(bmpHeader, binary.LittleEndian, int32(2835))    // bV4YPelsPerMeter
	binary.Write(bmpHeader, binary.LittleEndian, uint32(0))      // bV4ClrUsed
	binary.Write(bmpHeader, binary.LittleEndian, uint32(0))      // bV4ClrImportant

	// --- 颜色掩码 (Color Masks) ---
	// 告诉播放器，在 4 字节的像素中，哪个字节是 R，哪个是 G，哪个是 B
	// Android 的内存顺序是 [R, G, B, A]，按小端序解析为 uint32 就是 0xAABBGGRR
	binary.Write(bmpHeader, binary.LittleEndian, uint32(0x000000FF)) // Red Mask
	binary.Write(bmpHeader, binary.LittleEndian, uint32(0x0000FF00)) // Green Mask
	binary.Write(bmpHeader, binary.LittleEndian, uint32(0x00FF0000)) // Blue Mask
	binary.Write(bmpHeader, binary.LittleEndian, uint32(0xFF000000)) // Alpha Mask

	// 补齐 V4 头的剩余 52 字节 (色彩空间相关，普通图像直接填 0 即可)
	bmpHeader.Write(make([]byte, 52))

	// 3. 写入头文件
	if _, err := file.Write(bmpHeader.Bytes()); err != nil {
		return fmt.Errorf("写入 BMP 头失败: %w", err)
	}

	// 4. 原封不动地写入裸数据，不消耗 CPU 去遍历！
	written, err := io.CopyN(file, stream, int64(size))
	if err != nil {
		return fmt.Errorf("写入像素数据失败: %w", err)
	}

	LogInfof("✅ 成功生成带掩码的完美 BMP！保存至: %s (写入: %d Bytes)", localPath, written+int64(headerSize))
	return nil
}

// Screenshot 截取屏幕并保存为标准的 PNG 图片文件。
// 内部利用 adb shell screencap -p 实现，自动完成 PNG 编码。
func (c *Client) Screenshot(ctx context.Context, localPath string) error {
	// 调用系统的 screencap 命令，-p 表示输出 PNG 格式，不加路径表示输出到标准输出流
	stream, err := c.Shell(ctx, "screencap", "-p")
	if err != nil {
		return fmt.Errorf("触发截屏命令失败: %w", err)
	}
	defer stream.Close()

	file, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("创建本地文件 %s 失败: %w", localPath, err)
	}
	defer file.Close()

	// 把 PNG 数据流写入本地文件
	written, err := io.Copy(file, stream)
	if err != nil {
		return fmt.Errorf("保存截图文件失败: %w", err)
	}

	// 简单的空文件校验，防止由于权限不足等原因导致的截图失败
	if written == 0 {
		return fmt.Errorf("获取到的截图数据为空，可能是设备无响应或权限不足")
	}

	LogInfof("✅ 屏幕截图 (PNG) 已成功保存到: %s", localPath)
	return nil
}

// Remount 重新挂载系统分区为读写模式。
// 在现代 Android 系统中，它会自动处理 OverlayFS 和只读逻辑分区的复杂挂载。
// 成功时返回设备的提示信息（如 "remount succeeded"）。
func (c *Client) Remount(ctx context.Context, args ...string) (string, error) {
	// 复用你在 services.go 中写好的 GetDestination
	dest, err := ServiceRemount.GetDestination(args...)
	if err != nil {
		return "", fmt.Errorf("构建 remount 目标失败: %v", err)
	}

	stream, err := c.RawOpen(ctx, dest)
	if err != nil {
		return "", fmt.Errorf("发送 remount 请求失败: %v", err)
	}
	defer stream.Close()

	// 读取执行结果
	out, _ := io.ReadAll(stream)
	response := strings.TrimSpace(string(out))

	LogInfof("[Remount] 设备响应: %s", response)

	// 常见的失败关键字拦截
	if strings.Contains(response, "Read-only file system") || strings.Contains(response, "Permission denied") {
		return response, fmt.Errorf("remount 失败，可能是未获取 root 权限或设备受 AVB 保护")
	}

	return response, nil
}

// Reverse 执行反向端口转发命令。
// cmd: 反向转发命令字符串，例如 "forward:tcp:8080;tcp:8080"。
func (c *Client) Reverse(ctx context.Context, cmd string) (*Stream, error) {
	dest, err := ServiceReverse.GetDestination(cmd)
	if err != nil {
		return nil, fmt.Errorf("构建反向转发目标失败: %w", err)
	}
	return c.conn.Open(ctx, dest)
}

// RawOpen 直接以目标字符串打开一条流，适用于自定义服务。
// destination: ADB 服务目标字符串，例如 "local:my-service"。
func (c *Client) RawOpen(ctx context.Context, destination string) (*Stream, error) {
	return c.conn.Open(ctx, destination)
}

// Connection 返回底层 Connection 实例，供高级用户直接使用。
func (c *Client) Connection() *Connection {
	return c.conn
}

// InteractiveShell 启动一个全双工的交互式终端会话。
// 它可以对接任意的 io.Reader (如标准输入、网络流) 和 io.Writer (如标准输出)。
func (c *Client) InteractiveShell(ctx context.Context, in io.Reader, out io.Writer) error {
	// 1. 打开基础 Shell 流 (不带参数，触发默认的交互式 sh)
	stream, err := c.Shell(ctx)
	if err != nil {
		return fmt.Errorf("打开交互式 Shell 失败: %w", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)

	// 2. 启动输出搬运协程：将设备的输出写入到 out (比如电脑屏幕)
	go func() {
		defer wg.Done() // 搬运工彻底结束时打卡下班
		// io.Copy 会一直阻塞，直到 stream 被 Close() 产生 EOF
		_, _ = io.Copy(out, stream)
	}()

	// 3. 构建一个独立的内部方法来处理输入循环
	processInput := func() error {
		buf := make([]byte, 1024)
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				// 直接从终端读取字节（遇到回车就会触发返回）
				n, err := in.Read(buf)
				if err != nil {
					return nil // 读到 EOF
				}
				if n == 0 {
					continue
				}

				// 提取刚刚输入的数据
				inputData := buf[:n]

				// 拦截 exit 命令 (去掉末尾的换行符进行比对)
				if strings.TrimSpace(string(inputData)) == "exit" {
					return nil
				}

				// 🚀 没有任何延迟，直接拍给底层流
				if _, err := stream.Write(inputData); err != nil {
					return fmt.Errorf("向设备发送指令失败: %w", err)
				}
			}
		}
	}

	// 4. 阻塞执行输入循环
	err = processInput()

	// 5. 优雅停机 (Graceful Shutdown)
	// 第一步：主动关闭双向流，这会向远端发送 EOF，远端会断开连接。
	// 同时，这也使得上面阻塞在 io.Copy 的协程收到 EOF 并结束。
	stream.Close()

	// 第二步：使用 WaitGroup 阻塞等待，确保输出搬运协程把最后一丝日志也打印完！
	wg.Wait()

	return err
}

// Reboot 触发设备重启到指定模式。
// target 可选值:
//   - ""           : 正常重启到系统
//   - "bootloader" : 重启到 Bootloader (Fastboot) 模式
//   - "recovery"   : 重启到 Recovery 恢复模式
//   - "fastboot"   : 重启到 Fastbootd (Android 10+ 动态分区设备)
func (c *Client) Reboot(ctx context.Context, target string) error {
	dest := "reboot:"
	if target != "" {
		dest += target
	}

	// 1. 发送重启指令
	stream, err := c.RawOpen(ctx, dest)
	if err != nil {
		return fmt.Errorf("发送 %s 请求失败: %w", dest, err)
	}
	defer stream.Close()

	LogInfof("[Reboot] 已发送指令 %s，正在等待设备断开连接...", dest)

	// 2. 等待设备断开信号
	// 正常情况下，adbd 收到指令后会立刻通知系统重启并切断当前 TCP 连接
	select {
	case <-c.Disconnected():
		LogInfof("[Reboot] 设备连接已断开，正在重启中。")
		return nil
	case <-time.After(3 * time.Second):
		// 有些深度定制的系统重启流程较慢，或者压根不返回断开信号
		return fmt.Errorf("指令已发送，但在预期时间内未检测到设备断开，请手动确认")
	}
}

// Root 尝试重启设备的 adbd 以获取 root 权限。
// 返回值 disconnect: 是否触发了底层连接断开 (通常意味着 adbd 正在重启)。
// 返回值 msg: 设备返回的原始响应文本或内部错误信息。
func (c *Client) Root(ctx context.Context) (disconnect bool, msg string) {
	stream, err := c.RawOpen(ctx, "root:")
	if err != nil {
		return false, fmt.Sprintf("发送 root 请求失败: %v", err)
	}
	defer stream.Close()

	out, _ := io.ReadAll(stream)
	response := strings.TrimSpace(string(out))
	LogInfof("[Root] 设备响应: %s", response)

	// 等待底层断开信号，最多等 1.5 秒
	select {
	case <-c.Disconnected():
		return true, response
	case <-time.After(1500 * time.Millisecond):
		return false, response
	}
}

// Unroot 尝试重启设备的 adbd 以恢复非 root 权限。
func (c *Client) Unroot(ctx context.Context) (disconnect bool, msg string) {
	stream, err := c.RawOpen(ctx, "unroot:")
	if err != nil {
		return false, fmt.Sprintf("发送 unroot 请求失败: %v", err)
	}
	defer stream.Close()

	out, _ := io.ReadAll(stream)
	response := strings.TrimSpace(string(out))
	LogInfof("[Unroot] 设备响应: %s", response)

	select {
	case <-c.Disconnected():
		return true, response
	case <-time.After(1500 * time.Millisecond):
		return false, response
	}
}

// Forward 建立正向端口转发 (本地 PC -> 设备端)
// 这是一个阻塞方法，直到 ctx 被取消、主连接断开或遇到致命错误时才会返回。
func (c *Client) Forward(ctx context.Context, localPort, remotePort string) error {
	// 1. 启动本地监听
	listener, err := net.Listen("tcp", "127.0.0.1:"+localPort)
	if err != nil {
		return fmt.Errorf("无法监听本地端口 %s: %w", localPort, err)
	}
	defer listener.Close()

	LogInfof("[Forward] 正向转发已启动: 127.0.0.1:%s -> 设备 tcp:%s", localPort, remotePort)

	// 2. 监听 Context 取消信号，优雅关闭 Listener，从而打破下面的 Accept 阻塞
	go func() {
		select {
		case <-ctx.Done():
			listener.Close()
		case <-c.Disconnected(): // 兼容主连接断开的情况
			listener.Close()
		}
	}()

	// 3. 阻塞接收连接
	for {
		localConn, err := listener.Accept()
		if err != nil {
			// 如果是 ctx 被取消导致 listener 关闭，属于正常退出
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("接受本地连接失败: %w", err)
		}

		// 4. 有新连接进来，丢入独立协程处理（不阻塞主循环）
		go func(conn net.Conn) {
			defer conn.Close()

			// 向手机端发起 TCP 连接请求
			stream, err := c.TCPConnect(ctx, remotePort)
			if err != nil {
				LogErrorf("[Forward] 连接设备端口 %s 失败: %v", remotePort, err)
				return
			}
			defer stream.Close()

			// 打通双向数据流搬运 (就像水管对接)
			errc := make(chan error, 2)
			go func() {
				_, err := io.Copy(stream, conn)
				errc <- err
			}()
			go func() {
				_, err := io.Copy(conn, stream)
				errc <- err
			}()

			// 任何一端断开，结束当前转发
			<-errc
		}(localConn)
	}
}

// ReverseForward 建立反向端口转发 (设备端 -> 本地 PC)
// 这是一个阻塞方法，直到 ctx 被取消或主连接断开时，会自动向设备发送注销指令并返回。
func (c *Client) ReverseForward(ctx context.Context, remotePort, localPort string) error {
	// 1. 注册反向转发
	cmd := fmt.Sprintf("forward:tcp:%s;tcp:%s", remotePort, localPort)
	stream, err := c.Reverse(ctx, cmd)
	if err != nil {
		return fmt.Errorf("注册反向转发失败: %w", err)
	}
	// adbd 收到注册指令后会返回 OKAY，然后结束这个指令流
	stream.Close()

	LogInfof("[Reverse] 反向转发注册成功: 设备 tcp:%s -> 本地 127.0.0.1:%s", remotePort, localPort)

	// 2. 阻塞等待结束信号
	select {
	case <-ctx.Done():
		LogInfof("[Reverse] 收到取消信号，正在注销反向转发...")
		cancelCmd := fmt.Sprintf("killforward:tcp:%s", remotePort)
		// 使用后台 ctx 确保注销命令能发出去
		if cancelStream, _ := c.Reverse(context.Background(), cancelCmd); cancelStream != nil {
			cancelStream.Close()
		}
		return nil

	case <-c.Disconnected():
		return errors.New("主连接已断开，反向转发失效")
	}
}

// =====================================================================
// 核心基础方法：无脑透传 Options 给底层
// =====================================================================

// Push 推送流数据
func (c *Client) Push(ctx context.Context, remotePath string, reader io.Reader, mode os.FileMode, mtime uint32, opts ...SyncOption) error {
	stream, err := c.FileSync(ctx)
	if err != nil {
		return fmt.Errorf("打开 Sync 流失败: %w", err)
	}
	syncClient := NewSyncClient(stream)
	defer syncClient.Close()

	return syncClient.Send(remotePath, mode, mtime, reader, opts...)
}

// Pull 拉取流数据
func (c *Client) Pull(ctx context.Context, remotePath string, writer io.Writer, opts ...SyncOption) error {
	stream, err := c.FileSync(ctx)
	if err != nil {
		return fmt.Errorf("打开 Sync 流失败: %w", err)
	}
	syncClient := NewSyncClient(stream)
	defer syncClient.Close()

	return syncClient.Recv(remotePath, writer, opts...)
}

// =====================================================================
// 语法糖方法
// =====================================================================

// PushFile 推送本地文件
func (c *Client) PushFile(ctx context.Context, localPath, remotePath string, opts ...SyncOption) error {
	file, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("无法打开本地文件: %w", err)
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return fmt.Errorf("获取文件状态失败: %w", err)
	}

	mtime := uint32(stat.ModTime().Unix())

	// 【重点】：偷偷把文件大小注入到 Options 里，覆盖或补充用户可能传的选项
	finalOpts := append(opts, WithTotalSize(stat.Size()))

	return c.Push(ctx, remotePath, file, stat.Mode(), mtime, finalOpts...)
}

// PushBytes 推送内存数据
func (c *Client) PushBytes(ctx context.Context, remotePath string, content []byte, mode os.FileMode, opts ...SyncOption) error {
	reader := bytes.NewReader(content)
	mtime := uint32(time.Now().Unix())

	finalOpts := append(opts, WithTotalSize(int64(len(content))))

	return c.Push(ctx, remotePath, reader, mode, mtime, finalOpts...)
}

// PullFile 拉取到本地文件
func (c *Client) PullFile(ctx context.Context, remotePath, localPath string, opts ...SyncOption) error {
	file, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("无法创建本地文件: %w", err)
	}
	defer file.Close()

	return c.Pull(ctx, remotePath, file, opts...)
}

// PullBytes 拉取到内存
func (c *Client) PullBytes(ctx context.Context, remotePath string, opts ...SyncOption) ([]byte, error) {
	var buf bytes.Buffer
	err := c.Pull(ctx, remotePath, &buf, opts...)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
