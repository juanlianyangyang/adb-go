/**
# @Datetime : 2026/5/19 21:27
# @Project  : adb-go
# @File     : client_test.go
# @Author   : 眷恋阳阳
*/

package adb

import (
	"bytes"
	"context"
	"net"
	"testing"
)

func TestClient_Shell(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()

	priv := generateTestKey(t)

	go func() {
		ParseMessage(serverConn, AVersionMin, MaxPayloadV1)
		serverConn.Write(GenerateMessage(CmdCnxn, AVersionSkipChecksum, MaxPayloadV3, []byte("device::")))

		msg, _ := ParseMessage(serverConn, AVersionMin, MaxPayloadV3)
		if msg != nil && msg.Command == CmdOpen {
			destination := string(bytes.TrimRight(msg.Payload, "\x00"))
			t.Logf("服务端: 收到命令请求 -> %s", destination)
			serverConn.Write(GenerateMessage(CmdOkay, 9999, msg.Arg0, nil))
		}

		ParseMessage(serverConn, AVersionMin, MaxPayloadV3)
	}()

	adbConn := NewConnection(clientConn, priv, "test-client", 30)

	ctx := context.Background()
	err := adbConn.Connect(ctx)
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}

	client := &Client{conn: adbConn}

	stream, err := client.Shell(ctx, "echo", "hello")
	if err != nil {
		t.Fatalf("Shell 失败: %v", err)
	}
	t.Log("客户端: 成功打开 Shell 流！")

	stream.Close()
	client.Close()
}

func TestClient_TCPConnect(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()

	priv := generateTestKey(t)

	go func() {
		ParseMessage(serverConn, AVersionMin, MaxPayloadV1)
		serverConn.Write(GenerateMessage(CmdCnxn, AVersionSkipChecksum, MaxPayloadV3, []byte("device::")))

		msg, _ := ParseMessage(serverConn, AVersionMin, MaxPayloadV3)
		if msg != nil && msg.Command == CmdOpen {
			t.Logf("服务端: 收到 TCP 连接请求")
			serverConn.Write(GenerateMessage(CmdOkay, 8888, msg.Arg0, nil))
		}

		ParseMessage(serverConn, AVersionMin, MaxPayloadV3)
	}()

	adbConn := NewConnection(clientConn, priv, "test-client", 30)

	ctx := context.Background()
	err := adbConn.Connect(ctx)
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}

	client := &Client{conn: adbConn}

	stream, err := client.TCPConnect(ctx, "8080")
	if err != nil {
		t.Fatalf("TCPConnect 失败: %v", err)
	}
	t.Log("客户端: 成功打开 TCP 流！")

	stream.Close()
	client.Close()
}

func TestClient_FileSync(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()

	priv := generateTestKey(t)

	go func() {
		ParseMessage(serverConn, AVersionMin, MaxPayloadV1)
		serverConn.Write(GenerateMessage(CmdCnxn, AVersionSkipChecksum, MaxPayloadV3, []byte("device::")))

		msg, _ := ParseMessage(serverConn, AVersionMin, MaxPayloadV3)
		if msg != nil && msg.Command == CmdOpen {
			t.Logf("服务端: 收到文件同步请求")
			serverConn.Write(GenerateMessage(CmdOkay, 10000, msg.Arg0, nil))
		}

		ParseMessage(serverConn, AVersionMin, MaxPayloadV3)
	}()

	adbConn := NewConnection(clientConn, priv, "test-client", 30)

	ctx := context.Background()
	err := adbConn.Connect(ctx)
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}

	client := &Client{conn: adbConn}

	stream, err := client.FileSync(ctx)
	if err != nil {
		t.Fatalf("FileSync 失败: %v", err)
	}
	t.Log("客户端: 成功打开文件同步流！")

	stream.Close()
	client.Close()
}
