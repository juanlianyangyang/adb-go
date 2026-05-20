/**
# @Datetime : 2026/5/19 21:27
# @Project  : adb-go
# @File     : connection_test.go
# @Author   : 眷恋阳阳
*/

package adb

import (
	"bytes"
	"context"
	"crypto/rand"
	"net"
	"testing"
	"time"
)

// setupMockEnvironment 创建基于内存管道的客户端和模拟服务端
func setupMockEnvironment(t *testing.T) (*Connection, net.Conn) {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	priv := generateTestKey(t)
	conn := NewConnection(clientConn, priv, "test-client", 30)
	return conn, serverConn
}

func TestConnection_ConnectAndAuth(t *testing.T) {
	conn, serverConn := setupMockEnvironment(t)
	defer serverConn.Close()

	go func() {
		// 1. 接收 CONNECT
		msg, err := ParseMessage(serverConn, AVersionMin, MaxPayloadV1)
		if err != nil {
			t.Errorf("服务端: 读取 CONNECT 失败: %v", err)
			return
		}
		if msg.Command != CmdCnxn {
			t.Errorf("服务端: 期望 CNXN，实际得到 %x", msg.Command)
		}

		// 2. 发送 AUTH Token
		token := make([]byte, 20)
		rand.Read(token)
		serverConn.Write(GenerateMessage(CmdAuth, AdbAuthToken, 0, token))

		// 3. 接收 AUTH Signature
		msg, err = ParseMessage(serverConn, AVersionMin, MaxPayloadV1)
		if err != nil {
			t.Errorf("服务端: 读取 AUTH 签名失败: %v", err)
			return
		}
		if msg.Command != CmdAuth || msg.Arg0 != AdbAuthSignature {
			t.Errorf("服务端: 期望 AUTH Signature，实际得到 %x arg0=%d", msg.Command, msg.Arg0)
		}
		t.Logf("服务端: 收到有效的签名数据，长度: %d", len(msg.Payload))

		// 4. 发送 CNXN 确认
		serverConn.Write(GenerateMessage(CmdCnxn, AVersionSkipChecksum, MaxPayloadV3, []byte("device::")))
	}()

	err := conn.Connect(context.Background())
	if err != nil {
		t.Fatalf("客户端: 连接失败: %v", err)
	}
	t.Log("客户端: ADB 连接并授权成功！")
}

func TestConnection_StreamReadWrite(t *testing.T) {
	conn, serverConn := setupMockEnvironment(t)
	defer serverConn.Close()

	go func() {
		// 握手阶段
		ParseMessage(serverConn, AVersionMin, MaxPayloadV1)
		serverConn.Write(GenerateMessage(CmdCnxn, AVersionSkipChecksum, MaxPayloadV3, []byte("device::")))

		// 流处理阶段
		msg, err := ParseMessage(serverConn, AVersionMin, MaxPayloadV3)
		if err != nil {
			t.Errorf("服务端: 读取 OPEN 失败: %v", err)
			return
		}
		if msg.Command != CmdOpen {
			t.Errorf("服务端: 期望 OPEN，实际得到 %x", msg.Command)
		}
		localID := msg.Arg0
		remoteID := uint32(1234)
		destination := string(bytes.TrimRight(msg.Payload, "\x00"))
		t.Logf("服务端: 收到 OPEN 请求 -> %s", destination)

		// 回复 OKAY
		serverConn.Write(GenerateMessage(CmdOkay, remoteID, localID, nil))

		// 接收 WRTE
		msg, err = ParseMessage(serverConn, AVersionMin, MaxPayloadV3)
		if msg.Command != CmdWrte {
			t.Errorf("服务端: 期望 WRTE，实际得到 %x", msg.Command)
		}
		t.Logf("服务端: 收到客户端数据 -> %s", string(msg.Payload))

		// 回复 OKAY
		serverConn.Write(GenerateMessage(CmdOkay, remoteID, localID, nil))

		// 发送 WRTE
		serverConn.Write(GenerateMessage(CmdWrte, remoteID, localID, []byte("daemon response")))

		// 接收 OKAY
		msg, err = ParseMessage(serverConn, AVersionMin, MaxPayloadV3)
		if msg.Command != CmdOkay {
			t.Errorf("服务端: 期望 OKAY，实际得到 %x", msg.Command)
		}

		// 接收 CLSE
		msg, err = ParseMessage(serverConn, AVersionMin, MaxPayloadV3)
		if msg.Command != CmdClse {
			t.Errorf("服务端: 期望 CLSE，实际得到 %x", msg.Command)
		}
		t.Log("服务端: 收到关闭信号")
	}()

	err := conn.Connect(context.Background())
	if err != nil {
		t.Fatalf("客户端: 连接失败: %v", err)
	}

	stream, err := conn.Open(context.Background(), "shell:ls -l")
	if err != nil {
		t.Fatalf("客户端: 打开流失败: %v", err)
	}
	t.Log("客户端: 流已成功建立")

	writeData := []byte("hello daemon")
	n, err := stream.Write(writeData)
	if err != nil || n != len(writeData) {
		t.Fatalf("客户端: 写入失败: %v", err)
	}

	readBuf := make([]byte, 1024)
	readErrCh := make(chan error)
	var bytesRead int

	go func() {
		bytesRead, err = stream.Read(readBuf)
		readErrCh <- err
	}()

	select {
	case err := <-readErrCh:
		if err != nil {
			t.Fatalf("客户端: 读取失败: %v", err)
		}
		t.Logf("客户端: 成功读取数据 -> %s", string(readBuf[:bytesRead]))
	case <-time.After(2 * time.Second):
		t.Fatal("客户端: 读取超时")
	}

	err = stream.Close()
	if err != nil {
		t.Fatalf("客户端: 关闭流失败: %v", err)
	}
	t.Log("客户端: 流已关闭")
}
