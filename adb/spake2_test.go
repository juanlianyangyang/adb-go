/**
# @Datetime : 2026/5/19 21:27
# @Project  : adb-go
# @File     : spake2_test.go
# @Author   : 眷恋阳阳
*/

package adb

import (
	"testing"
)

func TestSpake2_Start(t *testing.T) {
	// 测试基本的 SPAKE2 Start 功能，确保不报错
	password := []byte("test-password-123")
	clientID := []byte("adb pair client\x00")
	serverID := []byte("adb pair server\x00")

	// 初始化客户端
	client := NewSpake2(password, clientID, serverID)

	// 客户端开始密钥交换，生成消息
	clientMsg, err := client.Start()
	if err != nil {
		t.Fatalf("Start() 失败: %v", err)
	}
	if len(clientMsg) != 32 {
		t.Errorf("消息长度期望 32 字节，实际 %d 字节", len(clientMsg))
	}
	t.Logf("客户端 SPAKE2 消息生成成功: %x", clientMsg)
}

func TestSpake2_Finish(t *testing.T) {
	// 测试 Start 和 Finish 能正常配合工作
	password := []byte("test-password-123")
	clientID := []byte("adb pair client\x00")
	serverID := []byte("adb pair server\x00")

	// 客户端
	client := NewSpake2(password, clientID, serverID)
	_, err := client.Start()
	if err != nil {
		t.Fatalf("Start() 失败: %v", err)
	}

	// 模拟服务端，用客户端消息测试 Finish 函数（虽然不会真的交换密钥）
	// 注意：这里只是简单测试 API 调用流程
	server := NewSpake2(password, serverID, clientID)
	serverMsg, err := server.Start()
	if err != nil {
		t.Fatalf("服务端 Start() 失败: %v", err)
	}

	clientKey, err := client.Finish(serverMsg)
	if err != nil {
		t.Fatalf("Finish() 失败: %v", err)
	}
	if len(clientKey) != 64 {
		t.Errorf("Finish() 返回的密钥长度期望 64 字节，实际 %d 字节", len(clientKey))
	}

	t.Log("Start() 和 Finish() API 调用正常完成")
}

func TestSpake2_InvalidMessageLength(t *testing.T) {
	// 测试 Finish 对无效消息长度的处理
	password := []byte("test")
	client := NewSpake2(password, []byte("c\x00"), []byte("s\x00"))
	client.Start()

	// 使用错误长度的消息（太短）
	shortMsg := []byte{1, 2, 3}
	_, err := client.Finish(shortMsg)
	if err == nil {
		t.Error("期望在消息太短时返回错误，但没有返回错误")
	} else {
		t.Logf("正确检测到无效消息长度: %v", err)
	}
}
