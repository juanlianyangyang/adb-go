/**
# @Datetime : 2026/5/19 21:27
# @Project  : adb-go
# @File     : main.go
# @Author   : 眷恋阳阳
*/

package main

import (
	"bufio"
	"context"
	"crypto/rsa"
	"fmt"
	"os"
	"strings"
	"time"

	"adb-go/adb"
)

func main() {
	fmt.Println("========================================")
	fmt.Println("       ADB Shell 客户端工具 v1.0        ")
	fmt.Println("========================================")
	fmt.Println()

	// 打印模式选择菜单
	fmt.Println("请选择连接模式:")
	fmt.Println("1. 普通模式 - 直接连接已配对的 ADB 设备")
	fmt.Println("2. 配对模式 - 首次配对新设备后再连接")
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("请输入选项 (1/2): ")
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	var (
		address string
		privKey *rsa.PrivateKey
		client  *adb.Client
		err     error
		ctx     context.Context
		cancel  context.CancelFunc
	)

	switch input {
	case "1":
		// 普通模式
		fmt.Print("请输入 ADB 地址 (例如 192.168.50.1:5555): ")
		address, _ := reader.ReadString('\n')
		address = strings.TrimSpace(address)
		if address == "" {
			fmt.Println("地址不能为空，程序退出")
			return
		}

		// 加载或生成私钥
		privKey, err = adb.LoadOrGeneratePrivateKey(false)
		if err != nil {
			fmt.Printf("加载私钥失败: %v\n", err)
			return
		}

		// 连接设备
		fmt.Printf("正在连接到 %s ...\n", address)
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
		client, err = adb.Dial(ctx, address, privKey, "adb-shell-client")
		cancel()
		if err != nil {
			fmt.Printf("连接失败: %v\n", err)
			return
		}
		fmt.Println("连接成功!")

	case "2":
		// 配对模式
		fmt.Print("请输入配对地址 (例如 192.168.50.2:5555): ")
		pairAddress, _ := reader.ReadString('\n')
		pairAddress = strings.TrimSpace(pairAddress)
		if pairAddress == "" {
			fmt.Println("配对地址不能为空，程序退出")
			return
		}

		fmt.Print("请输入配对码 (6位数字): ")
		pairCode, _ := reader.ReadString('\n')
		pairCode = strings.TrimSpace(pairCode)
		if pairCode == "" {
			fmt.Println("配对码不能为空，程序退出")
			return
		}

		// 先加载或生成私钥
		privKey, err = adb.LoadOrGeneratePrivateKey(true)
		if err != nil {
			fmt.Printf("加载私钥失败: %v\n", err)
			return
		}

		// 执行配对
		fmt.Printf("正在配对到 %s ...\n", pairAddress)
		ctx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
		err = adb.Pair(ctx, pairAddress, pairCode, privKey, "adb-shell-client")
		cancel()
		if err != nil {
			fmt.Printf("配对失败: %v\n", err)
			fmt.Println("提示: 请确保设备已开启无线调试并显示配对码")
			return
		}
		fmt.Println("配对成功!")

		// 询问 ADB 连接地址
		fmt.Print("请输入 ADB 连接地址 (例如 192.168.50.1:5555): ")
		address, _ = reader.ReadString('\n')
		address = strings.TrimSpace(address)
		if address == "" {
			fmt.Println("地址不能为空，程序退出")
			return
		}

		// 连接设备
		fmt.Printf("正在连接到 %s ...\n", address)
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
		client, err = adb.Dial(ctx, address, privKey, "adb-shell-client")
		cancel()
		if err != nil {
			fmt.Printf("连接失败: %v\n", err)
			return
		}
		fmt.Println("连接成功!")

	default:
		fmt.Println("无效的选项，程序退出")
		return
	}

	// 进入交互式 Shell
	fmt.Println("\n========================================")
	fmt.Println("已进入交互式 Shell 环境")
	fmt.Println("输入 exit 退出程序")
	fmt.Println("========================================\n")

	runInteractiveShell(client)

	// 关闭连接
	fmt.Println("\n正在关闭连接...")
	client.Close()
	fmt.Println("连接已关闭，再见!")
}

// runInteractiveShell 提供一个简单的交互式 Shell 环境
func runInteractiveShell(client *adb.Client) {
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Print("adb shell> ")
		input, err := reader.ReadString('\n')
		if err != nil {
			fmt.Printf("\n读取输入失败: %v\n", err)
			break
		}

		// 去除换行符并去除首尾空格
		input = strings.TrimSpace(input)

		// 如果为空，跳过
		if input == "" {
			continue
		}

		// 检查是否退出
		if input == "exit" || input == "quit" || input == "q" {
			break
		}

		// 解析命令和参数
		parts := strings.Fields(input)
		if len(parts) == 0 {
			continue
		}

		// 执行命令
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		stream, err := client.Shell(ctx, parts...)
		cancel()

		if err != nil {
			fmt.Printf("执行命令失败: %v\n", err)
			continue
		}

		// 读取命令输出
		output := make([]byte, 4096)
		for {
			n, err := stream.Read(output)
			if n > 0 {
				fmt.Print(string(output[:n]))
			}
			if err != nil {
				break
			}
		}

		stream.Close()
	}
}
