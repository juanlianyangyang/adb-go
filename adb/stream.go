/**
# @Datetime : 2026/5/19 21:27
# @Project  : adb-go
# @File     : stream.go
# @Author   : 眷恋阳阳
*/

package adb

import (
	"bytes"
	"errors"
	"io"
	"sync/atomic"
)

// Stream 代表与 ADB 守护进程之间的一条多路复用逻辑流。
// 每条流都有一个唯一的本地 ID，通过后台读取协程接收远程数据。
type Stream struct {
	conn     *Connection   // 所属的 ADB 连接
	localID  uint32        // 本地流标识符
	remoteID atomic.Uint32 // 远端流标识符（由 OKAY 包中的 Arg0 字段赋值）

	readBuf    *bytes.Buffer // 读取缓冲区，存储未消费完的数据
	readCh     chan []byte   // 接收来自后台读取协程的数据包
	writeReady chan struct{} // 写入就绪信号，收到 OKAY 后可写入
	closeCh    chan struct{} // 流关闭信号

	isClosed atomic.Bool // 流是否已关闭
}

// newStream 创建一个新的 Stream 实例。
// 这是一个内部函数，由 Connection 在 OPEN 操作时调用。
func newStream(conn *Connection, localID uint32) *Stream {
	return &Stream{
		conn:       conn,
		localID:    localID,
		readBuf:    new(bytes.Buffer),
		readCh:     make(chan []byte, 100),
		writeReady: make(chan struct{}, 1),
		closeCh:    make(chan struct{}),
	}
}

// Read 实现了 io.Reader 接口，从流中读取数据。
// 首先消费内部缓冲区中的残留数据，若无数据则阻塞等待新的数据包到达。
// 流关闭后返回 io.EOF。
func (s *Stream) Read(p []byte) (n int, err error) {
	if s.isClosed.Load() && s.readBuf.Len() == 0 && len(s.readCh) == 0 {
		return 0, io.EOF
	}

	// 优先消耗缓冲区中的历史残留数据
	if s.readBuf.Len() > 0 {
		return s.readBuf.Read(p)
	}

	// 阻塞等待新的数据包到达
	select {
	case data := <-s.readCh:
		s.conn.sendReady(s.localID, s.remoteID.Load())
		n = copy(p, data)
		// 如果读取的目标缓冲区装不下，将剩余部分存入内部缓冲区
		if n < len(data) {
			s.readBuf.Write(data[n:])
		}
		// 归还从内存池中获取的缓冲区
		if cap(data) == MaxPayloadV3 {
			fullBuf := data[:cap(data)]
			PayloadPool.Put(&fullBuf)
		}
		return n, nil
	case <-s.closeCh:
		return 0, io.EOF
	}
}

// Write 实现了 io.Writer 接口，向流中写入数据。
// 大块数据会被自动拆分成多个 WRTE 包发送，每个包的大小不超过连接的最大载荷限制。
// 流关闭后写入将返回错误。
func (s *Stream) Write(p []byte) (n int, err error) {
	if s.isClosed.Load() {
		return 0, errors.New("流已关闭，无法写入")
	}

	maxPayload := s.conn.maxData
	offset := 0
	total := len(p)

	for offset < total {
		chunkSize := total - offset
		if chunkSize > maxPayload {
			chunkSize = maxPayload
		}

		// 等待写入许可（收到 OKAY 信号）
		select {
		case <-s.writeReady:
		case <-s.closeCh:
			return offset, errors.New("流在写入过程中被关闭")
		}

		chunk := p[offset : offset+chunkSize]
		err = s.conn.sendWrite(s.localID, s.remoteID.Load(), chunk)
		if err != nil {
			return offset, err
		}
		offset += chunkSize
	}
	return total, nil
}

// Close 关闭流，向对端发送 CLSE 包以通知远端释放资源。
// 重复调用 Close 是安全的，不会产生副作用。
func (s *Stream) Close() error {
	if s.isClosed.Swap(true) {
		return nil
	}
	close(s.closeCh)
	s.conn.removeStream(s.localID)
	return s.conn.sendClose(s.localID, s.remoteID.Load())
}

// handleWrte 处理来自远端的 WRTE 数据包。
// 将接收到的数据放入读通道，供 Read 方法消费。
// 如果读通道已满（读取方卡死），则主动关闭此流以防止阻塞主读循环。
func (s *Stream) handleWrte(payload []byte) {
	select {
	case s.readCh <- payload:
	default:
		// 读通道容量已满（100），说明上层读取方处理能力不足。
		// 为避免队头阻塞影响整个连接，主动关闭此流。
		s.Close()
	}
}

// handleOkay 处理来自远端的 OKAY 确认包。
// 保存远端分配的流 ID，并发送写入就绪信号。
func (s *Stream) handleOkay(remoteID uint32) {
	s.remoteID.Store(remoteID)
	select {
	case s.writeReady <- struct{}{}:
	default:
	}
}

// handleClse 处理来自远端的 CLSE 关闭包。
// 关闭本地流资源。
func (s *Stream) handleClse() {
	if !s.isClosed.Swap(true) {
		close(s.closeCh)
	}
}

// MaxPayload 暴露可以写入的最大值
func (s *Stream) MaxPayload() int {
	return s.conn.maxData
}
