/**
# @Datetime : 2026/5/19 21:27
# @Project  : adb-go
# @File     : protocol.go
# @Author   : 眷恋阳阳
*/

package adb

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
)

// ADB 协议常量定义
// 这些常量与 Android ADB 守护进程的协议规范保持一致，不可随意修改。
const (
	// HeaderLength ADB 协议消息头部固定长度，始终为 24 字节
	HeaderLength = 24

	// 协议命令字（Command），每个命令是一个 ASCII 字符串的大端序表示
	CmdSync = 0x434e5953 // "SYNC"
	CmdCnxn = 0x4e584e43 // "CNXN"
	CmdAuth = 0x48545541 // "AUTH"
	CmdOpen = 0x4e45504f // "OPEN"
	CmdOkay = 0x59414b4f // "OKAY"
	CmdClse = 0x45534c43 // "CLSE"
	CmdWrte = 0x45545257 // "WRTE"
	CmdStls = 0x534c5453 // "STLS"
)

// 载荷大小与协议版本常量
const (
	MaxPayloadV1 = 4 * 1024     // V1 协议最大载荷：4KB
	MaxPayloadV2 = 256 * 1024   // V2 协议最大载荷：256KB
	MaxPayloadV3 = 1024 * 1024  // V3 协议最大载荷：1MB
	MaxPayload   = MaxPayloadV1 // 默认最大载荷，兼容旧设备

	AVersionMin          = 0x01000000  // 最低协议版本
	AVersionSkipChecksum = 0x01000001  // 跳过校验和的协议版本
	AVersion             = AVersionMin // 默认协议版本

	AStlsVersionMin = 0x01000000      // STLS 最低版本
	AStlsVersion    = AStlsVersionMin // STLS 默认版本
)

// 认证类型常量
const (
	AdbAuthToken        = 1 // 认证令牌
	AdbAuthSignature    = 2 // RSA 签名认证
	AdbAuthRsaPublicKey = 3 // RSA 公钥认证
)

// Message 表示一条 ADB 协议消息。
// 每条消息由 24 字节固定头部和可变长度的载荷组成。
type Message struct {
	Command    uint32 // 命令字
	Arg0       uint32 // 参数0
	Arg1       uint32 // 参数1
	DataLength uint32 // 载荷数据长度
	DataCheck  uint32 // 载荷校验和
	Magic      uint32 // 魔术值（Command 的按位取反）
	Payload    []byte // 载荷数据
}

// String 返回消息的可读字符串表示，用于调试和日志输出。
func (m *Message) String() string {
	var tag string
	switch m.Command {
	case CmdSync:
		tag = "SYNC"
	case CmdCnxn:
		tag = "CNXN"
	case CmdOpen:
		tag = "OPEN"
	case CmdOkay:
		tag = "OKAY"
	case CmdClse:
		tag = "CLSE"
	case CmdWrte:
		tag = "WRTE"
	case CmdAuth:
		tag = "AUTH"
	case CmdStls:
		tag = "STLS"
	default:
		tag = "????"
	}
	return fmt.Sprintf("Message{命令=%s, arg0=0x%x, arg1=0x%x, 载荷长度=%d, 校验和=%d, 魔术值=0x%x, 载荷=%v}",
		tag, m.Arg0, m.Arg1, m.DataLength, m.DataCheck, m.Magic, m.Payload)
}

// 获取命令的可读字符串表示
func commandString(cmd uint32) string {
	switch cmd {
	case CmdSync:
		return "SYNC"
	case CmdCnxn:
		return "CNXN"
	case CmdAuth:
		return "AUTH"
	case CmdOpen:
		return "OPEN"
	case CmdOkay:
		return "OKAY"
	case CmdClse:
		return "CLSE"
	case CmdWrte:
		return "WRTE"
	case CmdStls:
		return "STLS"
	default:
		return "UNKNOWN"
	}
}

// GetPayloadChecksum 计算 ADB 载荷数据的校验和。
// 校验和算法：将载荷中所有字节的值累加，返回 uint32 类型的结果。
func GetPayloadChecksum(data []byte) uint32 {
	var checksum uint32
	for _, b := range data {
		checksum += uint32(b)
	}
	return checksum
}

// GenerateMessage 创建一个 ADB 消息的字节数组表示。
// 始终计算校验和以确保与旧设备的兼容性，新系统会自动忽略校验和。
// command: 命令字；arg0/arg1: 参数；data: 载荷数据（可为 nil）。
func GenerateMessage(command, arg0, arg1 uint32, data []byte) []byte {
	var dataLen uint32
	var checksum uint32

	if data != nil {
		dataLen = uint32(len(data))
		checksum = GetPayloadChecksum(data)
	}

	msg := make([]byte, HeaderLength+dataLen)

	binary.LittleEndian.PutUint32(msg[0:4], command)
	binary.LittleEndian.PutUint32(msg[4:8], arg0)
	binary.LittleEndian.PutUint32(msg[8:12], arg1)
	binary.LittleEndian.PutUint32(msg[12:16], dataLen)
	binary.LittleEndian.PutUint32(msg[16:20], checksum)
	binary.LittleEndian.PutUint32(msg[20:24], ^command)

	if data != nil {
		copy(msg[24:], data)
	}

	return msg
}

// SystemIdentityStringHost 是 CONNECT 消息中携带的宿主身份标识字符串。
var SystemIdentityStringHost = []byte("host::\x00")

// GenerateConnect 生成 ADB CONNECT 报文字节数组。
// 携带的版本号为 AVersionSkipChecksum，最大载荷为 MaxPayloadV3（1MB）。
// 旧设备会自动将最大载荷降级到其支持的范围内。
func GenerateConnect() []byte {
	return GenerateMessage(CmdCnxn, uint32(AVersionSkipChecksum), uint32(MaxPayloadV3), SystemIdentityStringHost)
}

// GetProtocolVersion 根据 Android API Level 返回对应的 ADB 协议版本。
// API >= 28（Android P）时返回支持跳过校验和的版本，否则返回最低版本。
func GetProtocolVersion(api int) int {
	if api >= 28 {
		return AVersionSkipChecksum
	}
	return AVersionMin
}

// PayloadPool 全局载荷内存池，统一分配 1MB 的内存块，
// 用于复用读取载荷时的缓冲区，减少 GC 压力。
var PayloadPool = sync.Pool{
	New: func() interface{} {
		b := make([]byte, MaxPayloadV3)
		return &b
	},
}

// ParseMessage 从 io.Reader 中读取并解析一条 ADB 消息。
// protocolVersion: 当前使用的协议版本；maxData: 允许的最大载荷大小。
// 如果数据损坏或验证失败，返回错误。
func ParseMessage(r io.Reader, protocolVersion int, maxData int) (*Message, error) {
	header := make([]byte, HeaderLength)

	_, err := io.ReadFull(r, header)
	if err != nil {
		return nil, fmt.Errorf("读取消息头部失败，连接已关闭: %w", err)
	}

	msg := &Message{
		Command:    binary.LittleEndian.Uint32(header[0:4]),
		Arg0:       binary.LittleEndian.Uint32(header[4:8]),
		Arg1:       binary.LittleEndian.Uint32(header[8:12]),
		DataLength: binary.LittleEndian.Uint32(header[12:16]),
		DataCheck:  binary.LittleEndian.Uint32(header[16:20]),
		Magic:      binary.LittleEndian.Uint32(header[20:24]),
	}

	// 验证魔术值：Magic 应为 Command 的按位取反
	if msg.Command != ^msg.Magic {
		return nil, fmt.Errorf("消息头部无效：魔术值 0x%x 与命令字 0x%x 不匹配", msg.Magic, msg.Command)
	}

	// 验证载荷长度是否在合法范围内
	if int(msg.DataLength) < 0 || int(msg.DataLength) > maxData {
		return nil, fmt.Errorf("消息头部无效：载荷长度 %d 超出允许范围", msg.DataLength)
	}

	// 无载荷数据，直接返回
	if msg.DataLength == 0 {
		return msg, nil
	}

	// 从内存池获取缓冲区读取载荷
	bufPtr := PayloadPool.Get().(*[]byte)
	msg.Payload = (*bufPtr)[:msg.DataLength]

	_, err = io.ReadFull(r, msg.Payload)
	if err != nil {
		// 读取失败时归还缓冲区
		fullBuf := (*bufPtr)[:cap(*bufPtr)]
		PayloadPool.Put(&fullBuf)
		return nil, fmt.Errorf("读取消息载荷失败，连接已关闭: %w", err)
	}

	// 对旧版本协议或 CNXN 消息进行校验和验证
	if protocolVersion <= AVersionMin || (msg.Command == CmdCnxn && msg.Arg0 <= AVersionMin) {
		if GetPayloadChecksum(msg.Payload) != msg.DataCheck {
			return nil, errors.New("消息头部无效：校验和不匹配")
		}
	}

	return msg, nil
}

type ConnectStatus int

const (
	ConnectStatusError ConnectStatus = iota
	ConnectStatusSuccess
	ConnectStatusUnauthorized
	ConnectStatusTlsUnauthorized
)

type FastConnectResult struct {
	Status ConnectStatus
	Model  string
}

func (f *FastConnectResult) String() string {
	switch f.Status {
	case ConnectStatusError:
		return "连接错误"
	case ConnectStatusSuccess:
		return "已授权:" + f.Model
	case ConnectStatusUnauthorized:
		return "未授权"
	case ConnectStatusTlsUnauthorized:
		return "安卓11未授权"
	}
	return fmt.Sprintf("Status:%v Model:%v", f.Status, f.Model)
}
