/**
# @Datetime : 2026/5/19 21:27
# @Project  : adb-go
# @File     : stream_test.go
# @Author   : 眷恋阳阳
*/

package adb

import (
	"bytes"
	"testing"
)

// TestStream_ReadBuffer 测试从流的读缓冲机制（不用 conn 功能
func TestStream_ReadBuffer(t *testing.T) {
	t.Log("Stream 读取缓冲测试")
	// 只测试 Stream 不需要 conn 的字段
	stream := &Stream{
		readBuf: new(bytes.Buffer),
	}

	// 设置测试数据
	testData := []byte("test data")
	stream.readBuf.Write(testData)

	// 测试读取
	buf := make([]byte, 20)
	n, err := stream.readBuf.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(testData) || !bytes.Equal(buf[:n], testData) {
		t.Error("读取不匹配")
	}
	t.Logf("读取缓冲区测试完成")
}

// TestStream_IsClosedSetters 测试 isClosed 字段的设置
func TestStream_IsClosedSetters(t *testing.T) {
	stream := &Stream{}

	// 初始 false
	if stream.isClosed.Load() {
		t.Error("初始 isClosed 应该是 false")
	}

	// 设置 true
	stream.isClosed.Store(true)
	if !stream.isClosed.Load() {
		t.Error("设置后应该是 true")
	}
	t.Log("isClosed 字段设置测试完成")
}
