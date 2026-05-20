/**
# @Datetime : 2026/5/20
# @Project  : adb-go
# @File     : sync.go
# @Desc     : 实现 ADB SYNC 协议 (Push/Pull/Stat)
*/

package adb

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

const (
	SyncMaxChunkSize = 64 * 1024 // 每次 DATA 传输的最大块 64KB
)

// =====================================================================
// 选项模式 (Functional Options)
// =====================================================================

// ProgressFunc 进度回调函数签名
type ProgressFunc func(totalSize, currentSize int64, percent float64)

// SyncOptions 包含所有可选的同步参数
type SyncOptions struct {
	Progress  ProgressFunc
	TotalSize int64
}

// SyncOption 定义了修改 SyncOptions 的函数类型
type SyncOption func(*SyncOptions)

// WithProgress 传入进度回调函数
func WithProgress(p ProgressFunc) SyncOption {
	return func(o *SyncOptions) {
		o.Progress = p
	}
}

// WithTotalSize 传入文件的总大小 (主要用于 Push 时计算百分比)
func WithTotalSize(size int64) SyncOption {
	return func(o *SyncOptions) {
		o.TotalSize = size
	}
}

// =====================================================================
// SyncClient 定义与基础方法
// =====================================================================

type SyncClient struct {
	stream *Stream
}

func NewSyncClient(stream *Stream) *SyncClient {
	return &SyncClient{stream: stream}
}

func (sc *SyncClient) Close() error {
	_ = sc.writeRequest("QUIT", 0)
	return sc.stream.Close()
}

func (sc *SyncClient) writeRequest(id string, length uint32) error {
	if len(id) != 4 {
		return errors.New("SYNC ID 必须是 4 个字符")
	}
	buf := make([]byte, 8)
	copy(buf[0:4], id)
	binary.LittleEndian.PutUint32(buf[4:8], length)
	_, err := sc.stream.Write(buf)
	return err
}

func (sc *SyncClient) readReply() (id string, length uint32, err error) {
	buf := make([]byte, 8)
	if _, err := io.ReadFull(sc.stream, buf); err != nil {
		return "", 0, err
	}
	id = string(buf[0:4])
	length = binary.LittleEndian.Uint32(buf[4:8])
	return id, length, nil
}

func (sc *SyncClient) checkOkay() error {
	id, length, err := sc.readReply()
	if err != nil {
		return err
	}
	if id == "OKAY" {
		return nil
	}
	if id == "FAIL" {
		msgBuf := make([]byte, length)
		io.ReadFull(sc.stream, msgBuf)
		return fmt.Errorf("设备返回错误: %s", string(msgBuf))
	}
	return fmt.Errorf("未知的响应标识: %s", id)
}

func (sc *SyncClient) Stat(remotePath string) (mode, size, mtime uint32, err error) {
	if err := sc.writeRequest("STAT", uint32(len(remotePath))); err != nil {
		return 0, 0, 0, err
	}
	if _, err := sc.stream.Write([]byte(remotePath)); err != nil {
		return 0, 0, 0, err
	}

	buf := make([]byte, 16)
	if _, err := io.ReadFull(sc.stream, buf); err != nil {
		return 0, 0, 0, err
	}

	if string(buf[0:4]) != "STAT" {
		return 0, 0, 0, fmt.Errorf("非预期的 STAT 回复: %s", string(buf[0:4]))
	}

	mode = binary.LittleEndian.Uint32(buf[4:8])
	size = binary.LittleEndian.Uint32(buf[8:12])
	mtime = binary.LittleEndian.Uint32(buf[12:16])
	return mode, size, mtime, nil
}

// =====================================================================
// 核心推拉逻辑 (接管 Options 解析)
// =====================================================================

// Send 推送数据 (终极批量发包优化版)
func (sc *SyncClient) Send(remotePath string, mode os.FileMode, mtime uint32, reader io.Reader, opts ...SyncOption) error {
	options := &SyncOptions{}
	for _, opt := range opts {
		opt(options)
	}

	// 记录实际发送的字节数（必须在 defer 之前声明，以便闭包捕获）
	var currentSize int64

	// ⏳ 1. 记录开始时间
	startTime := time.Now()

	// 🚀 2. 使用 defer 在函数退出时（无论成功还是报错）自动打印性能报告
	defer func() {
		duration := time.Since(startTime)
		secs := duration.Seconds()

		// 确定计算基数：优先使用实际跑完的 currentSize，如果没有再看 TotalSize
		calcSize := currentSize
		if calcSize == 0 && options.TotalSize > 0 {
			calcSize = options.TotalSize
		}

		// 防止除以 0 的情况
		if calcSize > 0 && secs > 0 {
			mbSize := float64(calcSize) / 1024 / 1024
			speed := mbSize / secs
			// 注意：这里的 logInfof 请替换为你项目中实际的日志打印函数 (比如 globalLogger.Infof)
			LogInfof("🚀 [Push 测速] 目标: %s | 耗时: %dms | 大小: %.2f MB | 平均速度: %.2f MB/s",
				remotePath, duration.Milliseconds(), mbSize, speed)
		} else {
			LogInfof("🚀 [Push 测速] 目标: %s | 耗时: %dms", remotePath, duration.Milliseconds())
		}
	}()

	adbMode := uint32(mode&os.ModePerm) | 0x8000
	pathAndMode := fmt.Sprintf("%s,%d", remotePath, adbMode)

	// 1. 发送 SEND 头部
	if err := sc.writeRequest("SEND", uint32(len(pathAndMode))); err != nil {
		return err
	}
	if _, err := sc.stream.Write([]byte(pathAndMode)); err != nil {
		return err
	}

	// ---------------------------------------------------------
	// 🚀 核心优化：你的“集装箱”思路落地
	// ---------------------------------------------------------

	// 动态获取当前连接的最佳载荷大小
	optimalBatchSize := sc.stream.MaxPayload()
	if optimalBatchSize < SyncMaxChunkSize {
		optimalBatchSize = SyncMaxChunkSize
	}

	// 准备一个 64KB 的小盒子 (复用内存)
	packetBuf := make([]byte, 8+SyncMaxChunkSize)
	copy(packetBuf[0:4], "DATA")

	// 准备一个集装箱
	var batchBuf bytes.Buffer
	// 提前分配好内存，避免追加切片时频繁扩容造成的 GC 停顿
	batchBuf.Grow(optimalBatchSize + SyncMaxChunkSize + 8)

	for {
		n, err := reader.Read(packetBuf[8:])
		if n > 0 {
			binary.LittleEndian.PutUint32(packetBuf[4:8], uint32(n))

			packetSize := 8 + n

			// 🚀 【完美修正】：预判逻辑
			// 如果 当前集装箱已有的数据 + 马上要塞进去的这个包 > 最大载荷
			// 说明塞进去就超重了，必须立刻把之前的货先发走！
			if batchBuf.Len() > 0 && batchBuf.Len()+packetSize > optimalBatchSize {
				// 发车！这时候发出去的大小，绝对 <= optimalBatchSize
				if _, wErr := sc.stream.Write(batchBuf.Bytes()); wErr != nil {
					return wErr
				}
				batchBuf.Reset()
			}

			// 📦 确保存储空间充足后，再把这个 64KB 的包安全地装入空出来的集装箱
			batchBuf.Write(packetBuf[:packetSize])

			// --- 触发进度回调 ---
			currentSize += int64(n)
			if options.Progress != nil {
				var percent float64
				if options.TotalSize > 0 {
					percent = float64(currentSize) / float64(options.TotalSize) * 100
				}
				options.Progress(options.TotalSize, currentSize, percent)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}

	// 循环结束后，清空集装箱最后一批尾货
	if batchBuf.Len() > 0 {
		if _, wErr := sc.stream.Write(batchBuf.Bytes()); wErr != nil {
			return wErr
		}
	}

	// 发送 DONE 尾部
	if err := sc.writeRequest("DONE", mtime); err != nil {
		return err
	}
	return sc.checkOkay()
}

// Recv 拉取数据
func (sc *SyncClient) Recv(remotePath string, writer io.Writer, opts ...SyncOption) error {
	options := &SyncOptions{}
	for _, opt := range opts {
		opt(options)
	}

	// 【核心优化】：只有当用户要求进度，且没有显式传入 TotalSize 时，才去发 STAT 网络请求
	if options.Progress != nil {
		if _, size, _, err := sc.Stat(remotePath); err == nil {
			options.TotalSize = int64(size)
		}
	}

	if err := sc.writeRequest("RECV", uint32(len(remotePath))); err != nil {
		return err
	}
	if _, err := sc.stream.Write([]byte(remotePath)); err != nil {
		return err
	}

	var currentSize int64

	for {
		id, length, err := sc.readReply()
		if err != nil {
			return err
		}

		if id == "DONE" {
			break
		}
		if id == "FAIL" {
			msgBuf := make([]byte, length)
			io.ReadFull(sc.stream, msgBuf)
			return fmt.Errorf("设备返回错误: %s", string(msgBuf))
		}
		if id != "DATA" {
			return fmt.Errorf("期望 DATA 块，实际得到 %s", id)
		}

		chunk := make([]byte, length)
		if _, err := io.ReadFull(sc.stream, chunk); err != nil {
			return err
		}
		if _, err := writer.Write(chunk); err != nil {
			return err
		}

		// --- 触发进度回调 ---
		currentSize += int64(length)
		if options.Progress != nil {
			var percent float64
			if options.TotalSize > 0 {
				percent = float64(currentSize) / float64(options.TotalSize) * 100
			}
			options.Progress(options.TotalSize, currentSize, percent)
		}
	}
	return nil
}
