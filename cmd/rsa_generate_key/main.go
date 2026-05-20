/**
# @Datetime : 2026/5/19 21:27
# @Project  : adb-go
# @File     : main.go
# @Author   : 眷恋阳阳
*/

package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
)

func main() {
	fmt.Println("=== ADB RSA 密钥生成工具 ===")
	fmt.Println()

	// 1. 生成新的 2048 位 RSA 密钥
	fmt.Println("正在生成 2048 位 RSA 密钥...")
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		fmt.Printf("❌ 生成密钥失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ 密钥生成成功")

	// 2. 保存 PEM 格式的私钥到文件
	pemBlock := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privKey),
	}
	err = os.WriteFile("adbkey.pem", pem.EncodeToMemory(pemBlock), 0600)
	if err != nil {
		fmt.Printf("❌ 保存密钥到文件失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✅ 私钥已保存到: adbkey.pem\n")

	// 3. 输出 Go 代码格式的嵌入密钥（方便硬编码到项目中）
	fmt.Println()
	fmt.Println("=== Go 代码格式（可直接复制到项目中）===")
	derBytes := x509.MarshalPKCS1PrivateKey(privKey)
	fmt.Println("var embeddedPrivKeyBytes = []byte{")
	for i, b := range derBytes {
		if i%12 == 0 {
			fmt.Print("\t")
		}
		fmt.Printf("0x%02x, ", b)
		if i%12 == 11 || i == len(derBytes)-1 {
			fmt.Println()
		}
	}
	fmt.Println("}")

	fmt.Println()
	fmt.Println("=== 使用说明 ===")
	fmt.Println("1. 将生成的 adbkey.pem 文件放置在应用程序运行目录")
	fmt.Println("2. 或复制上面的 Go 代码到你的项目中进行硬编码")
	fmt.Println("3. 使用 LoadOrGeneratePrivateKey(false) 加载密钥")
}
