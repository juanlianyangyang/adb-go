/**
# @Datetime : 2026/5/19 21:27
# @Project  : adb-go
# @File     : pairing_test.go
# @Author   : 眷恋阳阳
*/

package adb

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"strings"
	"testing"
	"time"
)

// TestPair_NetworkError 测试网络不通的情况
func TestPair_NetworkError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	privKey, _ := rsa.GenerateKey(rand.Reader, 2048)

	err := Pair(ctx, "127.0.0.1:12345", "111111", privKey, "TestDevice")

	if err == nil {
		t.Fatal("预期连接失败，但返回了 nil")
	}

	if !strings.Contains(err.Error(), "连接失败") && !strings.Contains(err.Error(), "connection refused") {
		t.Logf("收到预期的网络错误: %v", err)
	}
}

// TestPair_ContextCancel 测试上下文被中途取消的情况
func TestPair_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	privKey, _ := rsa.GenerateKey(rand.Reader, 2048)

	err := Pair(ctx, "127.0.0.1:5555", "111111", privKey, "TestDevice")

	if err == nil {
		t.Fatal("预期上下文取消导致失败，但返回了 nil")
	}

	t.Logf("收到预期的取消错误: %v", err)
}

// TestPair_Integration 真实设备集成测试
// 测试前准备：
// 1. 在 Android 11+ 设备上进入"开发者选项" -> "无线调试"
// 2. 点击"使用配对码配对设备"
// 3. 记录屏幕上显示的 IP:端口 和 6 位配对码
// 4. 注释掉 t.Skip() 后运行测试
func TestPair_Integration(t *testing.T) {
	t.Skip("跳过集成测试，如需测试请注释此行并填写下方参数")

	address := "192.168.1.100:38555"
	pairingCode := "123456"
	deviceName := "adb-go-test"

	pk := generateTestKey(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	t.Logf("正在向 %s 发起配对请求，配对码: %s...", address, pairingCode)
	if err := Pair(ctx, address, pairingCode, pk, deviceName); err != nil {
		t.Fatalf("配对失败: %v", err)
	}

	t.Log("配对成功！")
}
