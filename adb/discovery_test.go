/**
# @Datetime : 2026/5/19 21:27
# @Project  : adb-go
# @File     : discovery_test.go
# @Author   : 眷恋阳阳
*/

package adb

import (
	"context"
	"testing"
	"time"
)

func TestDeviceState_String(t *testing.T) {
	tests := []struct {
		state    DeviceState
		expected string
	}{
		{StateReadyToConnect, "已授权"},
		{StateWaitingForPair, "待配对"},
	}

	for _, tt := range tests {
		result := tt.state.String()
		if result != tt.expected {
			t.Errorf("期望状态字符串 %s，实际得到 %s", tt.expected, result)
		}
	}
}

func TestStartDiscovery_Basic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	deviceCh := make(chan DeviceEvent, 10)

	err := StartDiscovery(ctx, func(event DeviceEvent) {
		deviceCh <- event
	})

	if err != nil {
		t.Fatalf("启动发现服务失败: %v", err)
	}

	t.Log("mDNS 发现服务已启动，等待 5 秒...")

	select {
	case event := <-deviceCh:
		t.Logf("发现设备: %s:%d (%s)", event.IP, event.Port, event.State.String())
	case <-time.After(5 * time.Second):
		t.Log("5 秒内未发现设备（正常，若无设备在网络中）")
	}

	t.Log("测试完成")
}
