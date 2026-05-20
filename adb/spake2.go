/**
# @Datetime : 2026/5/19 21:27
# @Project  : adb-go
# @File     : spake2.go
# @Author   : 眷恋阳阳
*/

package adb

import (
	"crypto/rand"
	"crypto/sha512"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"filippo.io/edwards25519"
	"fmt"
	"hash"
	"math/big"
)

// 以下静态常量来自 Android ADB 源码中的 BoringSSL 实现，
// 必须保持原样，不可修改，否则将无法与 Android 设备进行 SPAKE2 密钥交换。
var (
	// spake2M 和 spake2N 是从 BoringSSL (Android ADB) C 源码中提取的特定基点，
	// 绝对不能使用标准的 RFC SPAKE25519 规范点！
	spake2M = mustDecodePoint("5ada7e4bf6ddd9adb6626d32131c6b5c51a1e347a3478f53cfcf441b88eed12e")
	spake2N = mustDecodePoint("10e3df0ae37d8e7a99b5fe74b44672103dbddcbd06af680d71329a11693bc778")

	// Ed25519 Prime Order (L)
	orderL, _ = new(big.Int).SetString("1000000000000000000000000000000014def9dea2f79cd65812631a5cf5d3ed", 16)
)

// mustDecodePoint 在初始化时进行硬性校验，如果字符串格式不正确会直接 panic。
// 这是一个辅助函数，用于在程序启动时验证基点常量的正确性。
func mustDecodePoint(s string) *edwards25519.Point {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic("无效的十六进制字符串: " + err.Error())
	}
	p, err := new(edwards25519.Point).SetBytes(b)
	if err != nil {
		panic("无效的椭圆曲线点: " + err.Error())
	}
	return p
}

// AdbSpake2 实现了 Android ADB 特有的 SPAKE2+ 密钥交换协议。
// 该实现严格复刻了 Java 版 ADB 的字节级操作，确保跨平台兼容性。
type AdbSpake2 struct {
	myIdentity    []byte // 客户端身份标识
	theirIdentity []byte // 服务端身份标识
	myMsg         []byte // 客户端生成的 SPAKE2 消息 (P*)
	passwordHash  []byte // 经过 SHA-512 哈希的密码
	pwScalarUnmod []byte // 应用 Java 字节级 Hack 后的密码标量
	privateKey    []byte // 64 字节随机私钥（经过左移3位处理）
}

// NewSpake2 创建一个新的 SPAKE2 客户端实例。
// password: 用户输入的配对码（6位数字）+ TLS 导出密钥材料；
// myIdentity: 客户端身份字符串，固定为 "adb pair client\x00"；
// theirIdentity: 服务端身份字符串，固定为 "adb pair server\x00"。
func NewSpake2(password, myIdentity, theirIdentity []byte) *AdbSpake2 {
	return &AdbSpake2{
		myIdentity:    myIdentity,
		theirIdentity: theirIdentity,
		passwordHash:  password,
	}
}

// Start 生成客户端的公开消息 (P*)。
// 返回 32 字节的 SPAKE2 消息，用于发送给服务端。
func (s *AdbSpake2) Start() ([]byte, error) {
	// 1. 对密码进行 SHA-512 哈希（64 字节）
	pwHash64 := sha512.Sum512(s.passwordHash)
	s.passwordHash = pwHash64[:]

	// 2. 将 64 字节哈希值转换为 Ed25519 标量（小端序）
	pwSc, _ := edwards25519.NewScalar().SetUniformBytes(pwHash64[:])
	pwBytesLE := pwSc.Bytes()

	// 3. 应用 Java 字节级 Hack，模拟 Android 的密码处理逻辑
	s.pwScalarUnmod = applyJavaPasswordHack(pwBytesLE)

	// 4. 生成 64 字节随机私钥并规约
	privRand := make([]byte, 64)
	if _, err := rand.Read(privRand); err != nil {
		return nil, fmt.Errorf("生成随机私钥失败: %w", err)
	}
	privSc, _ := edwards25519.NewScalar().SetUniformBytes(privRand)
	privBytes := privSc.Bytes()

	// 5. 应用 Java 的 leftShift3 移位操作（乘以 8）
	leftShift3(privBytes)
	s.privateKey = privBytes

	// 6. 计算 P = privKey * BasePoint
	basePoint := edwards25519.NewGeneratorPoint()
	P := scalarMultUnreduced(basePoint, s.privateKey)

	// 7. 计算 Mask = pwScalar * M
	mask := scalarMultUnreduced(spake2M, s.pwScalarUnmod)

	// 8. P* = P + Mask
	PStar := new(edwards25519.Point).Add(P, mask)
	s.myMsg = PStar.Bytes()

	return s.myMsg, nil
}

// Finish 处理服务端发来的消息 (Q*) 并派生 AES 共享密钥。
// theirMsg: 服务端发送的 32 字节 SPAKE2 消息。
// 返回 64 字节的派生共享密钥。
func (s *AdbSpake2) Finish(theirMsg []byte) ([]byte, error) {
	if len(theirMsg) != 32 {
		return nil, errors.New("服务端消息必须为 32 字节")
	}

	QStar, err := new(edwards25519.Point).SetBytes(theirMsg)
	if err != nil {
		return nil, fmt.Errorf("无效的服务端椭圆曲线点: %w", err)
	}

	// 1. 计算服务端的 Mask = pwScalar * N
	peerMask := scalarMultUnreduced(spake2N, s.pwScalarUnmod)

	// 2. 去掩码: Q_ext = Q* - PeerMask
	QExt := new(edwards25519.Point).Subtract(QStar, peerMask)

	// 3. 计算 DH 共享密钥 = privKey * Q_ext
	dhSharedPoint := scalarMultUnreduced(QExt, s.privateKey)
	dhShared := dhSharedPoint.Bytes()

	// 4. 最终密钥派生（按 Alice/Client 顺序）
	sha := sha512.New()
	updateLengthPrefix(sha, s.myIdentity)
	updateLengthPrefix(sha, s.theirIdentity)
	updateLengthPrefix(sha, s.myMsg)
	updateLengthPrefix(sha, theirMsg)
	updateLengthPrefix(sha, dhShared)
	updateLengthPrefix(sha, s.passwordHash)

	return sha.Sum(nil), nil
}

// scalarMultUnreduced 使用非规约方式将点与标量相乘。
// 这是为了兼容 Java 版 ADB 的实现，避免规约操作导致的差异。
func scalarMultUnreduced(p *edwards25519.Point, scalar []byte) *edwards25519.Point {
	res := edwards25519.NewIdentityPoint()
	for i := 255; i >= 0; i-- {
		res.Add(res, res)
		bit := (scalar[i/8] >> (i % 8)) & 1
		if bit == 1 {
			res.Add(res, p)
		}
	}
	return res
}

// updateLengthPrefix 向哈希函数写入带长度前缀的数据。
// 长度使用 8 字节小端序表示。
func updateLengthPrefix(h hash.Hash, data []byte) {
	lenBuf := make([]byte, 8)
	binary.LittleEndian.PutUint64(lenBuf, uint64(len(data)))
	h.Write(lenBuf)
	h.Write(data)
}

// reverseBytes 反转字节数组的顺序。
func reverseBytes(b []byte) []byte {
	res := make([]byte, len(b))
	for i := range b {
		res[i] = b[len(b)-1-i]
	}
	return res
}

// padTo32 将字节数组补齐到 32 字节（从末尾截取或填充）。
func padTo32(b []byte) []byte {
	if len(b) >= 32 {
		return b[len(b)-32:]
	}
	res := make([]byte, 32)
	copy(res[32-len(b):], b)
	return res
}

// padTo32LE 将字节数组补齐到 32 字节（小端序，从开头填充）。
func padTo32LE(b []byte) []byte {
	if len(b) >= 32 {
		return b[:32]
	}
	res := make([]byte, 32)
	copy(res, b)
	return res
}

// JavaScalar 用于模拟 Java 版 ADB 中的标量类型（32 字节小端序）。
type JavaScalar [32]byte

// add 将另一个标量加到当前标量上（小端序逐字节加法）。
func (s *JavaScalar) add(src *JavaScalar) {
	var carry int = 0
	for i := 0; i < 32; i++ {
		tmp := int(s[i]) + int(src[i]) + carry
		s[i] = byte(tmp)
		carry = tmp >> 8
	}
}

// dbl 将标量翻倍（左移一位）。
func (s *JavaScalar) dbl() {
	var carry int = 0
	for i := 0; i < 32; i++ {
		carryOut := int(s[i]) >> 7
		s[i] = byte((int(s[i]) << 1) | carry)
		carry = carryOut
	}
}

// cmov 条件移动：如果 mask 为 0xFFFFFFFF，则将 src 复制到 s。
func (s *JavaScalar) cmov(src *JavaScalar, mask uint32) {
	m := byte(mask)
	for i := 0; i < 32; i++ {
		s[i] = (m & src[i]) | (^m & s[i])
	}
}

// isEqual 比较两个字节是否相等，相等返回 0xFFFFFFFF，否则返回 0。
func isEqual(a, b byte) uint32 {
	if a == b {
		return 0xFFFFFFFF
	}
	return 0
}

// javaOrderL 是 Java 代码中写死的小端序 Ed25519 阶 L。
var javaOrderL = JavaScalar{
	0xed, 0xd3, 0xf5, 0x5c, 0x1a, 0x63, 0x12, 0x58,
	0xd6, 0x9c, 0xf7, 0xa2, 0xde, 0xf9, 0xde, 0x14,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x10,
}

// applyJavaPasswordHack 完美复刻 Java 版 ADB 针对密码标量低端 3 位的累加 Hack。
// 这是确保与 Android 设备兼容的关键步骤。
func applyJavaPasswordHack(pwBytes []byte) []byte {
	var pw JavaScalar
	copy(pw[:], pwBytes)
	order := javaOrderL

	var tmp JavaScalar
	tmp.cmov(&order, isEqual(pw[0]&1, 1))
	pw.add(&tmp)
	order.dbl()

	tmp = JavaScalar{}
	tmp.cmov(&order, isEqual(pw[0]&2, 2))
	pw.add(&tmp)
	order.dbl()

	tmp = JavaScalar{}
	tmp.cmov(&order, isEqual(pw[0]&4, 4))
	pw.add(&tmp)

	res := make([]byte, 32)
	copy(res, pw[:])
	return res
}

// leftShift3 完美复刻 Java 的左移 3 位操作（乘以 8）。
func leftShift3(n []byte) {
	var carry byte = 0
	for i := 0; i < 32; i++ {
		nextCarry := n[i] >> 5
		n[i] = (n[i] << 3) | carry
		carry = nextCarry
	}
}
