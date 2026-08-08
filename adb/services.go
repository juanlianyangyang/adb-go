/**
# @Datetime : 2026/5/19 21:27
# @Project  : adb-go
# @File     : services.go
# @Author   : 眷恋阳阳
*/

package adb

import (
	"fmt"
	"strings"
)

// Service 定义了 ADB 协议支持的各种本地服务类型。
// 每种服务类型对应一个特定的目标前缀字符串，
// 通过 GetDestination 方法可以生成发送给 ADB 守护进程的完整目标字符串。
type Service int

// 从 1 开始定义各项服务，对齐 Android 源码中的 SERVICE_FIRST = 1
const (
	ServiceShell                     Service = iota + 1 // Shell 命令执行服务
	ServiceRemount                                      // 重新挂载分区服务
	ServiceFile                                         // 文件设备服务
	ServiceTcpConnect                                   // TCP 端口连接服务
	ServiceLocalUnixSocket                              // 本地 Unix Socket 服务
	ServiceLocalUnixSocketReserved                      // 保留的本地 Unix Socket 服务
	ServiceLocalUnixSocketAbstract                      // 抽象的本地 Unix Socket 服务
	ServiceLocalUnixSocketFileSystem                    // 文件系统本地 Unix Socket 服务
	ServiceFramebuffer                                  // 帧缓冲截图服务
	ServiceConnectJdwp                                  // JDWP 调试连接服务
	ServiceTrackJdwp                                    // JDWP 调试跟踪服务
	ServiceSync                                         // 文件同步服务
	ServiceReverse                                      // 反向端口转发服务
	ServiceBackup                                       // 备份服务
	ServiceRestore                                      // 还原服务
)

// Name 返回该服务类型对应的基础前缀字符串。
// 例如 ServiceShell 返回 "shell:"，ServiceTcpConnect 返回 "tcp:"。
func (s Service) Name() (string, error) {
	switch s {
	case ServiceShell:
		return "shell:", nil
	case ServiceRemount:
		return "remount:", nil
	case ServiceFile:
		return "dev:", nil
	case ServiceTcpConnect:
		return "tcp:", nil
	case ServiceLocalUnixSocket:
		return "local:", nil
	case ServiceLocalUnixSocketReserved:
		return "localreserved:", nil
	case ServiceLocalUnixSocketAbstract:
		return "localabstract:", nil
	case ServiceLocalUnixSocketFileSystem:
		return "localfilesystem:", nil
	case ServiceFramebuffer:
		return "framebuffer:", nil
	case ServiceConnectJdwp:
		return "jdwp:", nil
	case ServiceTrackJdwp:
		return "track-jdwp", nil
	case ServiceSync:
		return "sync:", nil
	case ServiceReverse:
		return "reverse:", nil
	case ServiceBackup:
		return "backup:", nil
	case ServiceRestore:
		return "restore:", nil
	default:
		return "", fmt.Errorf("无效的服务类型: %d", s)
	}
}

// GetDestination 根据服务类型和附加参数生成最终发送给 ADB 守护进程的目标字符串。
// 不同类型的服务对参数的要求不同：
//   - Shell: 支持多个参数，空格自动加引号
//   - File/TCP/Local Socket: 需要恰好一个参数
//   - Sync/Framebuffer: 不接受参数
//   - Reverse: 需要一个转发命令参数
//   - Backup: 至少需要一个包名参数
func (s Service) GetDestination(args ...string) (string, error) {
	name, err := s.Name()
	if err != nil {
		return "", err
	}

	var dest strings.Builder
	dest.WriteString(name)

	switch s {
	case ServiceShell:
		if len(args) == 1 {
			dest.WriteString(args[0])
		} else if len(args) > 1 {
			for i, arg := range args {
				if i > 0 {
					dest.WriteString(" ")
				}
				dest.WriteString(escapeArgForAdb(arg))
			}
		}
	case ServiceFile, ServiceLocalUnixSocket, ServiceLocalUnixSocketAbstract,
		ServiceLocalUnixSocketFileSystem, ServiceLocalUnixSocketReserved, ServiceConnectJdwp:
		if len(args) == 0 {
			return "", fmt.Errorf("此服务必须指定参数")
		} else if len(args) != 1 {
			return "", fmt.Errorf("此服务只需要一个参数，当前提供了 %d 个", len(args))
		}
		dest.WriteString(args[0])
	case ServiceTcpConnect:
		if len(args) == 0 {
			return "", fmt.Errorf("必须指定端口号")
		} else if len(args) == 1 {
			dest.WriteString(args[0])
		} else if len(args) == 2 {
			dest.WriteString(fmt.Sprintf("%s:%s", args[0], args[1]))
		} else {
			return "", fmt.Errorf("提供的参数数量无效，最多支持 2 个")
		}
	case ServiceReverse:
		if len(args) != 1 || args[0] == "" {
			return "", fmt.Errorf("反向转发命令必须指定为一个确切的参数")
		}
		cmd := args[0]
		if cmd == "list-forward" || cmd == "killforward-all" ||
			strings.HasPrefix(cmd, "forward:") || strings.HasPrefix(cmd, "killforward:") {
			dest.WriteString(cmd)
		} else {
			return "", fmt.Errorf("无效的反向转发命令: %s", cmd)
		}
	case ServiceBackup:
		if len(args) == 0 {
			return "", fmt.Errorf("至少需要指定一个包名，或使用 -shared/-all 标志")
		}
		fallthrough
	case ServiceRemount:
		if len(args) > 0 {
			dest.WriteString(strings.Join(args, " "))
		}
	case ServiceRestore, ServiceFramebuffer, ServiceSync, ServiceTrackJdwp:
		if len(args) != 0 {
			return "", fmt.Errorf("此服务不接受额外参数")
		}
	}

	return dest.String(), nil
}

// escapeArgForAdb 完美还原 AOSP (Android Open Source Project) 的 bash 参数转义逻辑
func escapeArgForAdb(arg string) string {
	// 1. 空字符串直接返回 ''
	if arg == "" {
		return "''"
	}

	// 2. 检查是否全是安全的 POSIX 字符
	isSafe := true
	for _, c := range arg {
		// AOSP 认为安全的字符集合：字母、数字，以及 - . / _ : , +
		if !((c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') ||
			c == '-' || c == '.' || c == '/' || c == '_' || c == ':' || c == ',' || c == '+') {
			isSafe = false
			break
		}
	}

	// 如果全部安全，原样透传，不加任何引号
	if isSafe {
		return arg
	}

	// 3. 包含危险字符（如空格、$、&、|、引号、非ASCII字符等）
	// 使用单引号包裹，并将内部原有的单引号替换为 '\'' (闭合单引号 -> 转义单引号 -> 开启单引号)
	escaped := strings.ReplaceAll(arg, "'", "'\\''")
	return "'" + escaped + "'"
}
