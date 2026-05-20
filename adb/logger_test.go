/**
# @Datetime : 2026/5/19 21:27
# @Project  : adb-go
# @File     : logger_test.go
# @Author   : 眷恋阳阳
*/

package adb

import (
	"bytes"
	"testing"
)

func TestLogger_Levels(t *testing.T) {
	buf := new(bytes.Buffer)
	logger := NewDefaultLogger(buf, "[TEST] ", 0)

	// 设置日志级别为 Debug，以便输出所有级别
	if dl, ok := logger.(*defaultLogger); ok {
		dl.level = LogLevelDebug
	}

	// 测试各个日志级别
	logger.Debugf("调试消息")
	logger.Infof("信息消息")
	logger.Warnf("警告消息")
	logger.Errorf("错误消息")

	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("[调试]")) {
		t.Error("调试消息未输出")
	}
	if !bytes.Contains([]byte(output), []byte("[信息]")) {
		t.Error("信息消息未输出")
	}
	if !bytes.Contains([]byte(output), []byte("[警告]")) {
		t.Error("警告消息未输出")
	}
	if !bytes.Contains([]byte(output), []byte("[错误]")) {
		t.Error("错误消息未输出")
	}

	t.Logf("日志输出内容:\n%s", output)
}

func TestLogger_SetLevel(t *testing.T) {
	buf := new(bytes.Buffer)
	logger := NewDefaultLogger(buf, "", 0)

	// 将默认日志实例替换为我们的测试实例
	SetLogger(logger)
	SetLogLevel(LogLevelWarn)

	// 这些应该被过滤掉
	LogDebugf("调试消息")
	LogInfof("信息消息")

	// 这些应该输出
	LogWarnf("警告消息")
	LogErrorf("错误消息")

	output := buf.String()
	if bytes.Contains([]byte(output), []byte("[调试]")) {
		t.Error("调试消息不应该输出")
	}
	if bytes.Contains([]byte(output), []byte("[信息]")) {
		t.Error("信息消息不应该输出")
	}
	if !bytes.Contains([]byte(output), []byte("[警告]")) {
		t.Error("警告消息应该输出")
	}
	if !bytes.Contains([]byte(output), []byte("[错误]")) {
		t.Error("错误消息应该输出")
	}

	// 恢复默认日志实例
	SetLogger(nil)
}

func TestLogger_CustomAdapter(t *testing.T) {
	// 测试自定义日志适配器
	customLogger := &mockLogger{}
	SetLogger(customLogger)

	LogDebugf("调试")
	LogInfof("信息")
	LogWarnf("警告")
	LogErrorf("错误")

	if customLogger.debugCount != 1 {
		t.Errorf("期望 1 次 Debugf 调用，实际 %d", customLogger.debugCount)
	}
	if customLogger.infoCount != 1 {
		t.Errorf("期望 1 次 Infof 调用，实际 %d", customLogger.infoCount)
	}
	if customLogger.warnCount != 1 {
		t.Errorf("期望 1 次 Warnf 调用，实际 %d", customLogger.warnCount)
	}
	if customLogger.errorCount != 1 {
		t.Errorf("期望 1 次 Errorf 调用，实际 %d", customLogger.errorCount)
	}

	// 恢复默认日志实例
	SetLogger(nil)
}

// mockLogger 用于测试的模拟日志实现
type mockLogger struct {
	debugCount int
	infoCount  int
	warnCount  int
	errorCount int
}

func (m *mockLogger) Debugf(format string, args ...interface{}) {
	m.debugCount++
}

func (m *mockLogger) Infof(format string, args ...interface{}) {
	m.infoCount++
}

func (m *mockLogger) Warnf(format string, args ...interface{}) {
	m.warnCount++
}

func (m *mockLogger) Errorf(format string, args ...interface{}) {
	m.errorCount++
}
