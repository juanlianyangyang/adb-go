/**
# @Datetime : 2026/5/19
# @Project  : adb-go
# @File     : main.go
# @Author   : 眷恋阳阳
# @Desc     : 交互式 ADB 测试沙盒，支持动态连接状态和底层命令验证
*/

package main

import (
	"adb-go/adb"
	"bufio"
	"context"
	"crypto/rsa"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"
)

type ReplState struct {
	Client  *adb.Client     // 当前活跃的 ADB 连接
	Address string          // 当前连接的设备地址
	PrivKey *rsa.PrivateKey // 贯穿整个生命周期的 RSA 密钥
}

func main() {
	fmt.Println("======================================================")
	fmt.Println("       ADB 交互式测试沙盒 (Interactive Sandbox)       ")
	fmt.Println("======================================================")
	fmt.Println(" 支持指令:")
	fmt.Println("  - adb pair <ip:port> <配对码>  (无线调试首次配对)")
	fmt.Println("  - adb connect <ip:port>        (建立 ADB 连接)")
	fmt.Println("  - adb disconnect               (断开当前连接)")
	fmt.Println("  - adb shell <命令>             (执行 Shell 指令)")
	fmt.Println("  - adb push / pull              (文件传输能力测试)")
	fmt.Println("  - exit / quit                  (退出沙盒)")
	fmt.Println("======================================================")

	// 1. 初始化并加载全局测试密钥
	// 传 false 表示优先读取本地 adbkey.pem，避免每次重启工具电视都重新弹窗
	privKey, err := adb.LoadOrGeneratePrivateKey(false)
	if err != nil {
		fmt.Printf("❌ 致命错误: 加载或生成 RSA 密钥失败: %v\n", err)
		return
	}

	state := &ReplState{
		PrivKey: privKey,
	}

	reader := bufio.NewReader(os.Stdin)

	// 进入交互式循环 (REPL)
	for {
		// 动态生成命令行提示符
		if state.Client != nil {
			fmt.Printf("\033[32m[%s] adb>\033[0m ", state.Address) // 绿色提示符表示已连接
		} else {
			fmt.Print("adb> ")
		}

		input, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("\n读取输入退出。")
			break
		}

		// 清理输入并解析参数
		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		// 支持用户输入 "adb shell" 或直接输入 "shell"
		if strings.HasPrefix(input, "adb ") {
			input = strings.TrimPrefix(input, "adb ")
		}

		args := strings.Fields(input)
		command := args[0]

		switch command {
		case "exit", "quit", "q":
			if state.Client != nil {
				state.Client.Close()
			}
			fmt.Println("Bye!")
			return

		case "pair":
			if len(args) != 3 {
				fmt.Println("❌ 用法错误: pair <ip:port> <6位配对码>")
				continue
			}
			target := args[1]
			code := args[2]

			fmt.Printf("🔄 正在向 %s 发起配对，配对码: %s...\n", target, code)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			err := adb.Pair(ctx, target, code, state.PrivKey, "Go-ADB-Sandbox")
			cancel()

			if err != nil {
				fmt.Printf("❌ 配对失败: %v\n", err)
			} else {
				fmt.Println("✅ 配对成功！现在你可以使用 connect 命令连接该设备了。")
			}

		case "connect":
			if len(args) != 2 {
				fmt.Println("❌ 用法错误: connect <ip:port>")
				continue
			}
			target := args[1]

			// 如果已经有连接，先断开
			if state.Client != nil {
				fmt.Println("⚠️ 发现已有连接，正在自动断开旧连接...")
				state.Client.Close()
				state.Client = nil
			}

			fmt.Printf("🔄 正在连接到 %s ...\n", target)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			client, err := adb.Dial(ctx, target, state.PrivKey, "Go-ADB-Sandbox")
			cancel()

			if err != nil {
				fmt.Printf("❌ 连接失败: %v\n", err)
			} else {
				fmt.Println("✅ 连接并授权成功！")
				state.Client = client
				state.Address = target
			}

		case "disconnect":
			if state.Client == nil {
				fmt.Println("⚠️ 当前没有活跃的连接。")
				continue
			}
			state.Client.Close()
			state.Client = nil
			state.Address = ""
			fmt.Println("🔌 连接已断开。")

		case "shell":
			if state.Client == nil {
				fmt.Println("❌ 尚未建立连接，请先执行 connect <ip:port>")
				continue
			}

			// 1. 【单次执行模式】: adb shell ls /sdcard
			if len(args) > 1 {
				shellCmds := args[1:]
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				stream, err := state.Client.Shell(ctx, shellCmds...)
				if err != nil {
					fmt.Printf("❌ 执行 Shell 失败: %v\n", err)
					cancel()
					continue
				}

				_, _ = io.Copy(os.Stdout, stream)
				stream.Close()
				cancel()
				fmt.Println()
				continue
			}

			// 2. 【交互式流模式】: 仅输入 adb shell
			fmt.Println("💻 进入交互式 Shell (输入 'exit' 退出)...")

			// 不需要超时，因为交互式会话应该持续保持
			ctx, cancel := context.WithCancel(context.Background())

			// 直接调用我们刚刚封装的高级方法，将标准输入输出传进去
			err := state.Client.InteractiveShell(ctx, os.Stdin, os.Stdout)

			cancel() // 释放 context 资源

			if err != nil {
				fmt.Printf("\n❌ 交互异常结束: %v\n", err)
			} else {
				fmt.Println("\n🔌 已退出交互式 Shell。")
			}
		case "push":
			if state.Client == nil {
				fmt.Println("❌ 尚未建立连接，请先执行 connect <ip:port>")
				continue
			}
			if len(args) != 3 {
				fmt.Println("❌ 用法错误: push <本地文件路径> <设备目标路径>")
				fmt.Println("   示例: push ./test.txt /data/local/tmp/test.txt")
				continue
			}

			localPath, remotePath := args[1], args[2]

			fmt.Printf("⬆️ 正在推送: %s -> %s ...\n", localPath, remotePath)

			// 给文件传输一个长一点的超时时间 (例如 60 秒)
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)

			onProgress := func(total, current int64, percent float64) {
				fmt.Printf("\r🚀 进度: [%6.2f%%] %d / %d Bytes", percent, current, total)
			}

			err := state.Client.PushFile(ctx, localPath, remotePath, adb.WithProgress(onProgress))
			cancel()

			if err != nil {
				fmt.Printf("❌ 推送失败: %v\n", err)
			} else {
				fmt.Println("✅ 推送成功！")
			}
		case "pull":
			if state.Client == nil {
				fmt.Println("❌ 尚未建立连接，请先执行 connect <ip:port>")
				continue
			}
			if len(args) != 3 {
				fmt.Println("❌ 用法错误: pull <设备文件路径> <本地目标路径>")
				fmt.Println("   示例: pull /sdcard/log.txt ./log.txt")
				continue
			}

			remotePath, localPath := args[1], args[2]

			fmt.Printf("⬇️ 正在拉取: %s -> %s ...\n", remotePath, localPath)

			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)

			onProgress := func(total, current int64, percent float64) {
				if total > 0 {
					fmt.Printf("\r📥 进度: [%6.2f%%] %d / %d Bytes", percent, current, total)
				} else {
					fmt.Printf("\r📥 进度: 已接收 %d Bytes", current) // totalSize为0的优雅降级
				}
			}

			err := state.Client.PullFile(ctx, remotePath, localPath, adb.WithProgress(onProgress))
			cancel()

			if err != nil {
				fmt.Printf("❌ 拉取失败: %v\n", err)
			} else {
				fmt.Println("✅ 拉取成功！")
			}
		case "root":
			if state.Client == nil {
				fmt.Println("❌ 尚未建立连接，请先执行 connect <ip:port>")
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			disconnect, msg := state.Client.Root(ctx)
			cancel()

			// 直接将设备的真实响应反馈给用户
			fmt.Printf("📢 设备响应: %s\n", msg)

			if disconnect {
				fmt.Println("🔄 触发断开信号, 设备 adbd 正在重启...")
				// 1. 释放当前已经失效的连接
				state.Client.Close()
				state.Client = nil

				// 2. 等待设备端的 adbd 进程重启完毕
				time.Sleep(3 * time.Second)

				// 3. 发起自动重连
				fmt.Printf("🔄 正在尝试重新连接到 %s ...\n", state.Address)
				reconCtx, reconCancel := context.WithTimeout(context.Background(), 10*time.Second)
				newClient, reconErr := adb.Dial(reconCtx, state.Address, state.PrivKey, "Go-ADB-Sandbox")
				reconCancel()

				if reconErr != nil {
					fmt.Printf("❌ 自动重连失败，设备可能还在启动中，请稍后手动 connect: %v\n", reconErr)
				} else {
					state.Client = newClient
					fmt.Println("✅ 重新连接成功！(建议执行 `shell id` 验证当前权限)")
				}
			} else {
				fmt.Println("✅ 连接未断开 (设备可能是目标状态或拒绝了请求)。")
			}

		case "unroot":
			if state.Client == nil {
				fmt.Println("❌ 尚未建立连接，请先执行 connect <ip:port>")
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			disconnect, msg := state.Client.Unroot(ctx)
			cancel()

			fmt.Printf("📢 设备响应: %s\n", msg)

			if disconnect {
				fmt.Println("🔄 触发断开信号, 设备 adbd 正在重启...")
				state.Client.Close()
				state.Client = nil

				time.Sleep(3 * time.Second)

				fmt.Printf("🔄 正在尝试重新连接到 %s ...\n", state.Address)
				reconCtx, reconCancel := context.WithTimeout(context.Background(), 10*time.Second)
				newClient, reconErr := adb.Dial(reconCtx, state.Address, state.PrivKey, "Go-ADB-Sandbox")
				reconCancel()

				if reconErr != nil {
					fmt.Printf("❌ 自动重连失败，设备可能还在启动中，请稍后手动 connect: %v\n", reconErr)
				} else {
					state.Client = newClient
					fmt.Println("✅ 重新连接成功！(建议执行 `shell id` 验证当前权限)")
				}
			} else {
				fmt.Println("✅ 连接未断开 (设备可能是目标状态或拒绝了请求)。")
			}

		case "test-tunnel":
			if state.Client == nil {
				fmt.Println("❌ 尚未建立连接，请先执行 connect <ip:port>")
				continue
			}

			fmt.Println("🔄 正在启动 极限套娃测试 (PC -> 手机 -> PC) ...")
			ctx, cancel := context.WithCancel(context.Background())

			echoPort := "19999"
			remotePort := "18888"
			localPort := "17777"

			// 1. 本地 Echo Server (加了服务端收到数据的打印)
			go func() {
				listener, err := net.Listen("tcp", "127.0.0.1:"+echoPort)
				if err != nil {
					fmt.Printf("❌ Echo Server 启动失败: %v\n", err)
					return
				}
				go func() {
					<-ctx.Done()
					listener.Close()
				}()

				for {
					conn, err := listener.Accept()
					if err != nil {
						return
					}

					go func(c net.Conn) {
						defer c.Close()
						buf := make([]byte, 1024)
						for {
							n, err := c.Read(buf)
							if err != nil {
								return
							}

							recvText := string(buf[:n])
							fmt.Printf("   \033[36m[服务端(19999)] 收到设备透传数据: %s\033[0m\n", recvText)

							reply := fmt.Sprintf("[Echo回音] 你发的是: %s", recvText)
							c.Write([]byte(reply))
						}
					}(conn)
				}
			}()

			// 2. 建立反向隧道
			go func() {
				err := state.Client.ReverseForward(ctx, remotePort, echoPort)
				if err != nil && ctx.Err() == nil {
					fmt.Printf("❌ 反向转发异常结束: %v\n", err)
				}
			}()

			// 3. 建立正向隧道
			go func() {
				err := state.Client.Forward(ctx, localPort, remotePort)
				if err != nil && ctx.Err() == nil {
					fmt.Printf("❌ 正向转发异常结束: %v\n", err)
				}
			}()

			time.Sleep(1 * time.Second)

			// 4. 客户端连接
			fmt.Printf("🚀 隧道搭建完毕！入口端口: %s\n", localPort)
			conn, err := net.Dial("tcp", "127.0.0.1:"+localPort)
			if err != nil {
				fmt.Printf("❌ 无法连接到入口端口: %v\n", err)
				cancel()
				continue
			}

			// 读取回音
			go func() {
				buf := make([]byte, 1024)
				for {
					n, err := conn.Read(buf)
					if err != nil {
						return
					}
					fmt.Printf("\033[32m📥 客户端收到回音: %s\033[0m\n", string(buf[:n]))
				}
			}()

			// 5. 持续发送
			for i := 0; i < 24; i++ {
				msg := fmt.Sprintf("Hello! 第 %d 次问候 (%s)", i+1, time.Now().Format("15:04:05.000"))
				fmt.Printf("📤 客户端发送数据: %s\n", msg)
				conn.Write([]byte(msg))
				time.Sleep(500 * time.Millisecond)
			}

			// 6. 清理
			conn.Close()
			cancel()
			time.Sleep(500 * time.Millisecond)
			fmt.Println("✅ 极限套娃测试完成，所有隧道资源已清理！")
		case "cap":
			if state.Client == nil {
				fmt.Println("❌ 尚未建立连接，请先执行 connect <ip:port>")
				continue
			}

			// 默认保存到当前目录
			savePath := "./screenshot.png"
			if len(args) > 1 {
				savePath = args[1]
			}

			fmt.Printf("📸 正在截取屏幕并保存到 %s ...\n", savePath)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			err := state.Client.Screenshot(ctx, savePath)
			cancel()

			if err != nil {
				fmt.Printf("❌ 截图失败: %v\n", err)
			} else {
				fmt.Println("✅ 截图完成，快去看看吧！")
			}
		case "bmp":
			if state.Client == nil {
				fmt.Println("❌ 尚未建立连接，请先执行 connect <ip:port>")
				continue
			}

			// 默认保存到当前目录
			savePath := "./screenshot.bmp"
			if len(args) > 1 {
				savePath = args[1]
			}

			fmt.Printf("📸 正在截取屏幕并保存到 %s ...\n", savePath)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			err := state.Client.SaveFramebufferBMP(ctx, savePath)
			cancel()

			if err != nil {
				fmt.Printf("❌ 截图失败: %v\n", err)
			} else {
				fmt.Println("✅ 截图完成，快去看看吧！")
			}
		case "remount":
			if state.Client == nil {
				fmt.Println("❌ 尚未建立连接，请先执行 connect <ip:port>")
				continue
			}

			fmt.Println("🔄 正在请求重新挂载系统分区...")
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)

			// 支持传入额外参数，比如 adb remount -R (自动重启)
			msg, err := state.Client.Remount(ctx, args[1:]...)
			cancel()

			if err != nil {
				fmt.Printf("❌ Remount 失败: %v\n", err)
				fmt.Printf("📢 设备响应: %s\n", msg)
			} else {
				fmt.Printf("✅ Remount 成功！\n📢 设备响应: %s\n", msg)
			}
		case "reboot":
			if state.Client == nil {
				fmt.Println("❌ 尚未建立连接，请先执行 connect <ip:port>")
				continue
			}

			// 解析参数，比如用户输入 "reboot recovery" 还是仅仅 "reboot"
			target := ""
			if len(args) > 1 {
				target = args[1]
			}

			fmt.Printf("🔄 正在请求设备重启 (模式: %s)...\n", map[bool]string{true: "正常", false: target}[target == ""])

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := state.Client.Reboot(ctx, target)
			cancel()

			if err != nil {
				fmt.Printf("⚠️ 重启请求异常: %v\n", err)
			} else {
				fmt.Println("✅ 成功触发重启！底层连接已断开。")
				// 清理本地已失效的客户端资源
				state.Client.Close()
				state.Client = nil
			}
		case "ls":
			// 默认查看当前目录
			targetDir := "."
			if len(args) > 1 {
				targetDir = args[1]
			}

			// 读取本地目录
			entries, err := os.ReadDir(targetDir)
			if err != nil {
				fmt.Printf("❌ 读取本地目录失败: %v\n", err)
				continue
			}

			fmt.Printf("📂 本地目录: %s\n", targetDir)
			fmt.Printf("%-12s %-12s %-20s %s\n", "权限", "大小(B)", "修改时间", "文件名")
			fmt.Println(strings.Repeat("-", 60))

			for _, entry := range entries {
				info, err := entry.Info()
				if err != nil {
					continue
				}

				// 格式化输出
				size := info.Size()
				modTime := info.ModTime().Format("2006-01-02 15:04:05")
				name := entry.Name()

				// 如果是目录，加个斜杠后缀便于区分
				if entry.IsDir() {
					name = "\033[36m" + name + "/\033[0m" // 目录加上青色高亮
				}

				fmt.Printf("%-12s %-12d %-20s %s\n", info.Mode().String(), size, modTime, name)
			}
			fmt.Println()
		default:
			fmt.Printf("❌ 未知指令: %s\n", command)
		}
	}
}
