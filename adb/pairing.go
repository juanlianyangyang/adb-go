/**
# @Datetime : 2026/5/19 21:27
# @Project  : adb-go
# @File     : pairing.go
# @Author   : 眷恋阳阳
*/

package adb

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"fmt"
	"golang.org/x/crypto/hkdf"
	"io"
	"math/big"
	"net"
	"time"
)

// 配对协议相关常量
const (
	ExportedKeyLabel = "adb-label\x00" // TLS 密钥导出标签
	ExportKeySize    = 64              // 导出密钥长度
	GcmIvLength      = 12              // AES-GCM IV 长度

	HeaderVersion = 1    // 配对包头部版本
	TypeSpake2Msg = 0    // SPAKE2 消息类型
	TypePeerInfo  = 1    // 对等方信息类型
	PeerInfoSize  = 8192 // PeerInfo 明文固定长度
)

// Pair 发起设备配对，用于 Android 11+ 的无线调试首次连接。
// ctx: 用于控制配对超时；address: 设备配对端口地址，格式如 "192.168.1.100:38555"；
// pairingCode: 用户输入的 6 位配对码；privKey: RSA 私钥；deviceName: 客户端标识名称。
func Pair(ctx context.Context, address string, pairingCode string, privKey *rsa.PrivateKey, deviceName string) error {
	LogInfof("[配对] 准备向 %s 发起配对请求，配对码: %s", address, pairingCode)

	// 生成客户端 TLS 证书
	cert, err := generateClientTLSCertificate(privKey, deviceName)
	if err != nil {
		return fmt.Errorf("生成 TLS 证书失败: %w", err)
	}

	// 建立双向认证的 TLS 连接
	tlsConfig := &tls.Config{
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: true, // 信任设备端的自签名证书
	}

	var dialer net.Dialer
	tcpConn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("连接配对端口失败: %w", err)
	}
	defer tcpConn.Close()

	tlsConn := tls.Client(tcpConn, tlsConfig)
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		return fmt.Errorf("TLS 握手失败: %w", err)
	}

	// 导出 TLS 密钥材料，构造 SPAKE2 密码
	cs := tlsConn.ConnectionState()
	exportedKey, err := cs.ExportKeyingMaterial(ExportedKeyLabel, nil, ExportKeySize)
	if err != nil {
		return fmt.Errorf("导出 TLS 密钥材料失败: %w", err)
	}

	// SPAKE2 密码 = 用户输入的 6 位配对码 + 导出的 TLS 密钥材料
	passwordBytes := append([]byte(pairingCode), exportedKey...)

	// SPAKE2 密钥交换阶段
	clientIdentity := []byte("adb pair client\x00")
	serverIdentity := []byte("adb pair server\x00")

	spakeClient := NewSpake2(passwordBytes, clientIdentity, serverIdentity)

	mySpakeMsg, err := spakeClient.Start()
	if err != nil {
		return fmt.Errorf("启动 SPAKE2 失败: %w", err)
	}

	if err := writePairingPacket(tlsConn, TypeSpake2Msg, mySpakeMsg); err != nil {
		return fmt.Errorf("发送 SPAKE2 消息失败: %w", err)
	}

	header, theirSpakeMsg, err := readPairingPacket(tlsConn)
	if err != nil || header.Type != TypeSpake2Msg {
		return fmt.Errorf("读取服务端 SPAKE2 消息失败: %w", err)
	}

	sharedSecret, err := spakeClient.Finish(theirSpakeMsg)
	if err != nil {
		return fmt.Errorf("SPAKE2 验证失败（配对码错误?）: %w", err)
	}

	// AES-GCM 密钥派生与公钥交换
	aesKey := make([]byte, 16)
	hkdfReader := hkdf.New(sha256.New, sharedSecret, nil, []byte("adb pairing_auth aes-128-gcm key"))
	if _, err := io.ReadFull(hkdfReader, aesKey); err != nil {
		return fmt.Errorf("HKDF 密钥派生失败: %w", err)
	}

	pubKeyPayload, err := EncodePublicKeyWithName(&privKey.PublicKey, deviceName)
	if err != nil {
		return fmt.Errorf("编码 RSA 公钥失败: %w", err)
	}

	// ADB 规定 PeerInfo 明文必须严格等于 8192 字节
	peerInfo := make([]byte, PeerInfoSize)
	peerInfo[0] = 0 // Type: ADB_RSA_PUB_KEY
	copy(peerInfo[1:], pubKeyPayload)

	encryptedPeerInfo, err := encryptAesGcm(aesKey, peerInfo, 0)
	if err != nil {
		return fmt.Errorf("加密 PeerInfo 失败: %w", err)
	}
	if err := writePairingPacket(tlsConn, TypePeerInfo, encryptedPeerInfo); err != nil {
		return fmt.Errorf("发送加密的 PeerInfo 失败: %w", err)
	}

	header, theirEncPeerInfo, err := readPairingPacket(tlsConn)
	if err != nil || header.Type != TypePeerInfo {
		return fmt.Errorf("读取设备 PeerInfo 失败: %w", err)
	}

	_, err = decryptAesGcm(aesKey, theirEncPeerInfo, 0)
	if err != nil {
		return fmt.Errorf("设备拒绝配对（解密失败）: %w", err)
	}

	LogInfof("[配对] 配对成功！设备已授权此客户端")
	return nil
}

// generateClientTLSCertificate 使用 RSA 私钥在内存中生成自签名的 x509 证书。
// 该证书用于配对过程中的 TLS 双向认证。
func generateClientTLSCertificate(priv *rsa.PrivateKey, deviceName string) (tls.Certificate, error) {
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
		return tls.Certificate{}, err
	}

	return tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  priv,
	}, nil
}

// PairingHeader 配对包头部结构
type PairingHeader struct {
	Version byte   // 版本号，固定为 1
	Type    byte   // 消息类型：0=SPAKE2, 1=PeerInfo
	Length  uint32 // 载荷长度
}

// writePairingPacket 向连接写入配对协议包
func writePairingPacket(w io.Writer, msgType byte, payload []byte) error {
	header := make([]byte, 6)
	header[0] = HeaderVersion
	header[1] = msgType
	binary.BigEndian.PutUint32(header[2:], uint32(len(payload)))

	if _, err := w.Write(header); err != nil {
		return fmt.Errorf("写入头部失败: %w", err)
	}
	_, err := w.Write(payload)
	return err
}

// readPairingPacket 从连接读取配对协议包
func readPairingPacket(r io.Reader) (PairingHeader, []byte, error) {
	headerBuf := make([]byte, 6)
	if _, err := io.ReadFull(r, headerBuf); err != nil {
		return PairingHeader{}, nil, fmt.Errorf("读取头部失败: %w", err)
	}

	header := PairingHeader{
		Version: headerBuf[0],
		Type:    headerBuf[1],
		Length:  binary.BigEndian.Uint32(headerBuf[2:]),
	}

	payload := make([]byte, header.Length)
	_, err := io.ReadFull(r, payload)
	return header, payload, err
}

// encryptAesGcm 使用 AES-GCM 加密数据
func encryptAesGcm(key []byte, plaintext []byte, ivCounter uint64) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("创建 AES 加密器失败: %w", err)
	}
	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("创建 GCM 模式失败: %w", err)
	}

	iv := make([]byte, GcmIvLength)
	binary.LittleEndian.PutUint64(iv, ivCounter)

	return aesgcm.Seal(nil, iv, plaintext, nil), nil
}

// decryptAesGcm 使用 AES-GCM 解密数据
func decryptAesGcm(key []byte, ciphertext []byte, ivCounter uint64) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("创建 AES 解密器失败: %w", err)
	}
	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("创建 GCM 模式失败: %w", err)
	}

	iv := make([]byte, GcmIvLength)
	binary.LittleEndian.PutUint64(iv, ivCounter)

	return aesgcm.Open(nil, iv, ciphertext, nil)
}
