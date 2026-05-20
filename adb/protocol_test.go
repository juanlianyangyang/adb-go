/**
# @Datetime : 2026/5/19 21:27
# @Project  : adb-go
# @File     : protocol_test.go
# @Author   : 眷恋阳阳
*/

package adb

import (
	"bytes"
	"testing"
)

func TestGetPayloadChecksum(t *testing.T) {
	payload := []byte("hello adb")
	expected := uint32(859)

	checksum := GetPayloadChecksum(payload)
	if checksum != expected {
		t.Errorf("期望校验和 %d，实际得到 %d", expected, checksum)
	}
	t.Logf("成功计算校验和: payload=%q, 结果=%d", string(payload), checksum)
}

func TestGenerateAndParseMessage(t *testing.T) {
	payload := []byte("test-payload")
	cmd := uint32(CmdOpen)
	arg0 := uint32(1)
	arg1 := uint32(0)

	// 1. 生成消息
	msgBytes := GenerateMessage(cmd, arg0, arg1, payload)
	t.Logf("生成的字节数组 (长度: %d): %x", len(msgBytes), msgBytes)

	// 2. 解析消息
	reader := bytes.NewReader(msgBytes)
	parsedMsg, err := ParseMessage(reader, AVersionMin, MaxPayloadV1)
	if err != nil {
		t.Fatalf("ParseMessage 失败: %v", err)
	}

	t.Logf("解析成功: %s", parsedMsg.String())

	// 3. 验证字段
	if parsedMsg.Command != cmd {
		t.Errorf("期望命令 %x，实际得到 %x", cmd, parsedMsg.Command)
	}
	if parsedMsg.Arg0 != arg0 {
		t.Errorf("期望 arg0 %d，实际得到 %d", arg0, parsedMsg.Arg0)
	}
	if !bytes.Equal(parsedMsg.Payload, payload) {
		t.Errorf("期望载荷 %q，实际得到 %q", string(payload), string(parsedMsg.Payload))
	}
}

func TestGenerateAndParseMessage_NilPayload(t *testing.T) {
	cmd := uint32(CmdClse)

	msgBytes := GenerateMessage(cmd, 1, 2, nil)
	t.Logf("生成的空载荷消息 (长度: %d): %x", len(msgBytes), msgBytes)

	reader := bytes.NewReader(msgBytes)
	parsedMsg, err := ParseMessage(reader, AVersionMin, MaxPayloadV1)
	if err != nil {
		t.Fatalf("ParseMessage 失败: %v", err)
	}

	t.Logf("解析成功: %s", parsedMsg.String())

	if parsedMsg.DataLength != 0 {
		t.Errorf("期望长度 0，实际得到 %d", parsedMsg.DataLength)
	}
	if len(parsedMsg.Payload) != 0 {
		t.Errorf("期望空载荷，实际长度 %d", len(parsedMsg.Payload))
	}
}

func TestGenerateConnect(t *testing.T) {
	msgBytes := GenerateConnect()
	if len(msgBytes) != HeaderLength+len(SystemIdentityStringHost) {
		t.Errorf("CONNECT 消息长度不正确")
	}

	reader := bytes.NewReader(msgBytes)
	msg, err := ParseMessage(reader, AVersionMin, MaxPayloadV1)
	if err != nil {
		t.Fatalf("解析 CONNECT 消息失败: %v", err)
	}

	if msg.Command != CmdCnxn {
		t.Errorf("期望命令 CNXN，实际得到 %x", msg.Command)
	}
	if msg.Arg0 != uint32(AVersionSkipChecksum) {
		t.Errorf("期望版本 %d，实际得到 %d", AVersionSkipChecksum, msg.Arg0)
	}

	t.Logf("CONNECT 消息生成成功")
}
