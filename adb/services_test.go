/**
# @Datetime : 2026/5/19 21:27
# @Project  : adb-go
# @File     : services_test.go
# @Author   : 眷恋阳阳
*/

package adb

import (
	"testing"
)

func TestServiceDestination_Success(t *testing.T) {
	tests := []struct {
		name     string
		service  Service
		args     []string
		expected string
	}{
		{"Shell 无参数", ServiceShell, nil, "shell:"},
		{"Shell 带参数", ServiceShell, []string{"ls", "-l"}, "shell:ls -l"},
		{"Shell 带空格参数", ServiceShell, []string{"cat", "my file.txt"}, "shell:cat \"my file.txt\""},
		{"TCP 单参数", ServiceTcpConnect, []string{"8080"}, "tcp:8080"},
		{"TCP 双参数", ServiceTcpConnect, []string{"localhost", "8080"}, "tcp:localhost:8080"},
		{"反向转发", ServiceReverse, []string{"forward:tcp:8080;tcp:8080"}, "reverse:forward:tcp:8080;tcp:8080"},
		{"同步服务", ServiceSync, nil, "sync:"},
		{"重新挂载", ServiceRemount, []string{"-R"}, "remount:-R"},
		{"文件设备", ServiceFile, []string{"/dev/input/event0"}, "dev:/dev/input/event0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dest, err := tt.service.GetDestination(tt.args...)
			if err != nil {
				t.Fatalf("意外的错误: %v", err)
			}
			if dest != tt.expected {
				t.Errorf("期望 '%s'，实际得到 '%s'", tt.expected, dest)
			}
			t.Logf("成功生成目标: %s", dest)
		})
	}
}

func TestServiceDestination_Errors(t *testing.T) {
	tests := []struct {
		name    string
		service Service
		args    []string
	}{
		{"Shell 带引号", ServiceShell, []string{"echo", "\"hello\""}},
		{"文件无参数", ServiceFile, nil},
		{"TCP 无参数", ServiceTcpConnect, nil},
		{"TCP 参数过多", ServiceTcpConnect, []string{"1", "2", "3"}},
		{"同步带参数", ServiceSync, []string{"arg1"}},
		{"反向转发无效命令", ServiceReverse, []string{"invalid-cmd"}},
		{"备份无参数", ServiceBackup, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.service.GetDestination(tt.args...)
			if err == nil {
				t.Errorf("期望错误，但返回了 nil")
			} else {
				t.Logf("成功捕获预期错误: %v", err)
			}
		})
	}
}

func TestServiceName(t *testing.T) {
	tests := []struct {
		service Service
		name    string
	}{
		{ServiceShell, "shell:"},
		{ServiceSync, "sync:"},
		{ServiceFramebuffer, "framebuffer:"},
		{ServiceTcpConnect, "tcp:"},
	}

	for _, tt := range tests {
		name, err := tt.service.Name()
		if err != nil {
			t.Errorf("Service.Name() 失败: %v", err)
			continue
		}
		if name != tt.name {
			t.Errorf("期望名称 %s，实际得到 %s", tt.name, name)
		}
	}
}
