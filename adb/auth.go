/**
# @Datetime : 2026/5/19 21:27
# @Project  : adb-go
# @File     : auth.go
# @Author   : 眷恋阳阳
*/

package adb

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"time"
)

// ADB 公钥编码相关常量
const (
	PubKeyModulusSize      = 256                       // RSA-2048 公钥模数大小（字节），2048 bits / 8
	PubKeyModulusSizeWords = PubKeyModulusSize / 4     // 模数字大小，以 32 位字为单位
	PubKeyEncodedSize      = 3*4 + 2*PubKeyModulusSize // 编码后的公钥总大小：3个uint32 + 2×256 = 524字节
)

// GenerateTLSCertificate 使用 RSA 私钥在内存中生成自签名的 x509 证书。
// 该证书用于 ADB TLS 加密通道的客户端身份验证。
// priv: 2048 位 RSA 私钥；deviceName: 设备名称，会写入证书的 CommonName 字段。
func GenerateTLSCertificate(priv *rsa.PrivateKey, deviceName string) (tls.Certificate, error) {
	template := x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject: pkix.Name{
			Organization: []string{"adb-go-client"},
			CommonName:   deviceName,
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(10, 0, 0), // 10 年有效期
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("生成 x509 证书失败: %w", err)
	}

	return tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  priv,
	}, nil
}

// Sign 使用 RSA 私钥对 ADB 守护进程发来的 20 字节认证令牌进行签名。
// 使用 PKCS#1 v1.5 填充方案和 SHA1 哈希算法，
// 这与 Android ADB 守护进程的认证要求完全一致。
// token: 必须是恰好 20 字节的随机令牌。
func Sign(priv *rsa.PrivateKey, token []byte) ([]byte, error) {
	if len(token) != 20 {
		return nil, errors.New("ADB 认证令牌必须恰好为 20 字节")
	}
	// rsa.SignPKCS1v15 会自动添加 Android 所需的 PKCS#1 v1.5 填充和 SHA1 ASN.1 DER 头部
	return rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA1, token)
}

// EncodePublicKeyWithName 将 RSA 公钥编码为 ADB Base64 格式，并附加设备名称后缀。
// 这是 ADB 协议中发送公钥给设备时的标准格式：
// Base64(编码后的公钥) + 空格 + 设备名称 + \0
func EncodePublicKeyWithName(pub *rsa.PublicKey, name string) ([]byte, error) {
	encoded, err := EncodePublicKey(pub)
	if err != nil {
		return nil, fmt.Errorf("编码 RSA 公钥失败: %w", err)
	}

	b64Len := base64.StdEncoding.EncodedLen(len(encoded))
	buf := make([]byte, 0, b64Len+len(name)+2)

	b64Buf := make([]byte, b64Len)
	base64.StdEncoding.Encode(b64Buf, encoded)

	buf = append(buf, b64Buf...)
	buf = append(buf, ' ')             // 空格分隔
	buf = append(buf, []byte(name)...) // 设备名称
	buf = append(buf, 0)               // C 风格字符串的 Null 结尾

	return buf, nil
}

// EncodePublicKey 将标准的 RSA 公钥编码为 Android ADB 守护进程期望的自定义 C 结构体二进制格式。
// 编码格式（小端序）：
//   - uint32: modulus_size_words (0x40 = 64)
//   - uint32: n0inv = -1/N[0] mod 2^32
//   - byte[256]: 模数 N（小端序）
//   - byte[256]: rr = 2^4096 mod N（小端序，预计算的蒙哥马利参数）
//   - uint32: 公钥指数 e
//
// 总共 524 字节。
func EncodePublicKey(pub *rsa.PublicKey) ([]byte, error) {
	if pub.N.BitLen() != 2048 {
		return nil, errors.New("ADB 要求 RSA 密钥长度必须为 2048 位")
	}

	buf := new(bytes.Buffer)

	// 1. modulus_size_words (32 位小端序)
	binary.Write(buf, binary.LittleEndian, uint32(PubKeyModulusSizeWords))

	// 2. n0inv = (-1 / N[0]) mod 2^32
	r32 := new(big.Int).Lsh(big.NewInt(1), 32) // 2^32
	n0 := new(big.Int).Mod(pub.N, r32)
	n0inv := new(big.Int).ModInverse(n0, r32)
	if n0inv == nil {
		return nil, errors.New("计算 n0inv 失败：模数和 2^32 不互质")
	}
	n0inv.Sub(r32, n0inv) // n0inv = 2^32 - n0inv
	binary.Write(buf, binary.LittleEndian, uint32(n0inv.Uint64()))

	// 3. 模数 N（小端序，不足 256 字节用 0 填充）
	modBytes := pub.N.Bytes()
	binary.Write(buf, binary.LittleEndian, reverseAndPad(modBytes, PubKeyModulusSize))

	// 4. rr = (2^2048)^2 mod N = 2^4096 mod N（蒙哥马利约简预计算参数）
	r := new(big.Int).Lsh(big.NewInt(1), PubKeyModulusSize*8) // 2^2048
	rr := new(big.Int).Exp(r, big.NewInt(2), pub.N)
	binary.Write(buf, binary.LittleEndian, reverseAndPad(rr.Bytes(), PubKeyModulusSize))

	// 5. 公钥指数 e（32 位小端序，通常为 65537）
	binary.Write(buf, binary.LittleEndian, uint32(pub.E))

	return buf.Bytes(), nil
}

// DecodePublicKey 解码 Android 的自定义二进制格式，还原出标准的 RSA 公钥。
// data 必须是 524 字节的 ADB 编码公钥数据。
func DecodePublicKey(data []byte) (*rsa.PublicKey, error) {
	if len(data) < PubKeyEncodedSize {
		return nil, errors.New("无效的公钥数据长度")
	}

	reader := bytes.NewReader(data)

	var modSizeWords uint32
	binary.Read(reader, binary.LittleEndian, &modSizeWords)
	if modSizeWords != PubKeyModulusSizeWords {
		return nil, errors.New("无效的模数字大小")
	}

	var n0inv uint32
	binary.Read(reader, binary.LittleEndian, &n0inv)

	// 读取模数 N（小端序存储，需转换为大端序供 big.Int 使用）
	modBytesLE := make([]byte, PubKeyModulusSize)
	reader.Read(modBytesLE)
	n := new(big.Int).SetBytes(reverseBytes(modBytesLE))

	// 读取并忽略 rr 值（还原公钥不需要此参数）
	rrBytesLE := make([]byte, PubKeyModulusSize)
	reader.Read(rrBytesLE)

	var e uint32
	binary.Read(reader, binary.LittleEndian, &e)

	return &rsa.PublicKey{
		N: n,
		E: int(e),
	}, nil
}

// reverseAndPad 将大端序字节数组转换为固定大小的小端序字节数组。
// 这是 ADB 公钥编码所需的字节序转换和填充操作。
func reverseAndPad(b []byte, size int) []byte {
	res := make([]byte, size)
	for i := 0; i < len(b) && i < size; i++ {
		res[i] = b[len(b)-1-i]
	}
	return res
}
