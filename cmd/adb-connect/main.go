/**
# @Project  : adb-go
# @Author   : juanlianyangyang
# @Datetime : 2026/6/20 15:21
# @FilePath : cmd/adb-connect/main.go
# @Comment  :
*/

package main

import (
	"context"
	"fmt"
	"github.com/juanlianyangyang/adb-go/adb"
)

func main() {
	fmt.Println("快速连接测试 拿到设备状态")
	privKey, err := adb.LoadOrGeneratePrivateKey(false)
	if err != nil {
		fmt.Printf("❌ 致命错误: 加载或生成 RSA 密钥失败: %v\n", err)
		return
	}
	fmt.Println(adb.FastStatusDial(context.Background(), "192.168.50.163:5555", privKey, "Go-ADB-Sandbox"))
	fmt.Println(adb.FastStatusDial(context.Background(), "192.168.50.126:37645", privKey, "Go-ADB-Sandbox"))
	fmt.Println(adb.FastStatusDial(context.Background(), "192.168.50.197:40509", privKey, "Go-ADB-Sandbox"))
}
