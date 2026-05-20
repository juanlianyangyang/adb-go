/**
# @Datetime : 2026/5/19 21:27
# @Project  : adb-go
# @File     : logger.go
# @Author   : 眷恋阳阳
*/

package adb

import (
	"fmt"
	"io"
	"log"
	"os"
)

// 日志级别定义
const (
	LogLevelDebug = iota
	LogLevelInfo
	LogLevelWarn
	LogLevelError
)

// Logger 日志接口，定义了 ADB 库所需的日志输出方法。
// 用户可以传入自定义的日志适配器（如 zap、logrus 等），
// 只需实现此接口即可无缝集成。
type Logger interface {
	// Debugf 输出调试级别的日志
	Debugf(format string, args ...interface{})
	// Infof 输出信息级别的日志
	Infof(format string, args ...interface{})
	// Warnf 输出警告级别的日志
	Warnf(format string, args ...interface{})
	// Errorf 输出错误级别的日志
	Errorf(format string, args ...interface{})
}

// defaultLogger 默认的日志实现，基于 Go 标准库 log 包。
type defaultLogger struct {
	logger *log.Logger
	level  int
}

// 默认的包级别日志实例
var defaultLog Logger = &defaultLogger{
	logger: log.New(os.Stdout, "[ADB] ", log.LstdFlags),
	level:  LogLevelInfo,
}

// 日志级别对应的前缀标签
var levelPrefixes = map[int]string{
	LogLevelDebug: "[调试] ",
	LogLevelInfo:  "[信息] ",
	LogLevelWarn:  "[警告] ",
	LogLevelError: "[错误] ",
}

// SetLogger 设置全局日志实现，允许用户注入自定义日志适配器。
// 例如，可以传入一个基于 zap 的实现来替换默认的标准库日志。
func SetLogger(l Logger) {
	if l != nil {
		defaultLog = l
	}
}

// GetLogger 获取当前的全局日志实现。
func GetLogger() Logger {
	return defaultLog
}

// NewDefaultLogger 创建一个新的默认日志实例。
// writer 参数指定日志输出目标，默认为 os.Stdout；
// prefix 参数指定日志前缀；
// flag 参数指定日志格式标志（参考 log 包的标准标志）。
func NewDefaultLogger(writer io.Writer, prefix string, flag int) Logger {
	if writer == nil {
		writer = os.Stdout
	}
	if prefix == "" {
		prefix = "[ADB] "
	}
	return &defaultLogger{
		logger: log.New(writer, prefix, flag),
		level:  LogLevelInfo,
	}
}

// SetLogLevel 设置默认日志实例的日志级别。
// 仅对 defaultLogger 生效，自定义 Logger 需自行实现级别过滤。
func SetLogLevel(level int) {
	if dl, ok := defaultLog.(*defaultLogger); ok {
		dl.level = level
	}
}

func (d *defaultLogger) Debugf(format string, args ...interface{}) {
	if d.level <= LogLevelDebug {
		d.logger.Output(2, fmt.Sprintf("%s%s", levelPrefixes[LogLevelDebug], fmt.Sprintf(format, args...)))
	}
}

func (d *defaultLogger) Infof(format string, args ...interface{}) {
	if d.level <= LogLevelInfo {
		d.logger.Output(2, fmt.Sprintf("%s%s", levelPrefixes[LogLevelInfo], fmt.Sprintf(format, args...)))
	}
}

func (d *defaultLogger) Warnf(format string, args ...interface{}) {
	if d.level <= LogLevelWarn {
		d.logger.Output(2, fmt.Sprintf("%s%s", levelPrefixes[LogLevelWarn], fmt.Sprintf(format, args...)))
	}
}

func (d *defaultLogger) Errorf(format string, args ...interface{}) {
	if d.level <= LogLevelError {
		d.logger.Output(2, fmt.Sprintf("%s%s", levelPrefixes[LogLevelError], fmt.Sprintf(format, args...)))
	}
}

// 包级别便捷日志函数，直接使用全局日志实例输出

// LogDebugf 使用全局日志实例输出调试级别日志
func LogDebugf(format string, args ...interface{}) {
	defaultLog.Debugf(format, args...)
}

// LogInfof 使用全局日志实例输出信息级别日志
func LogInfof(format string, args ...interface{}) {
	defaultLog.Infof(format, args...)
}

// LogWarnf 使用全局日志实例输出警告级别日志
func LogWarnf(format string, args ...interface{}) {
	defaultLog.Warnf(format, args...)
}

// LogErrorf 使用全局日志实例输出错误级别日志
func LogErrorf(format string, args ...interface{}) {
	defaultLog.Errorf(format, args...)
}
