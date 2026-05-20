/**
# @Datetime : 2026/5/19 21:27
# @Project  : adb-go
# @File     : auth_test.go
# @Author   : 眷恋阳阳
*/

package adb

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"testing"
)

// generateTestKey 辅助函数：生成 2048 位的测试密钥
func generateTestKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("生成测试 RSA 密钥失败: %v", err)
	}
	return priv
}

func TestEncodeDecodePublicKey(t *testing.T) {
	priv := generateTestKey(t)
	pub := &priv.PublicKey

	// 1. 编码公钥
	encodedBytes, err := EncodePublicKey(pub)
	if err != nil {
		t.Fatalf("EncodePublicKey 失败: %v", err)
	}

	if len(encodedBytes) != PubKeyEncodedSize {
		t.Errorf("期望编码长度 %d，实际得到 %d", PubKeyEncodedSize, len(encodedBytes))
	}
	t.Logf("成功编码公钥，长度: %d", len(encodedBytes))

	// 2. 解码公钥
	decodedPub, err := DecodePublicKey(encodedBytes)
	if err != nil {
		t.Fatalf("DecodePublicKey 失败: %v", err)
	}

	// 3. 验证一致性
	if decodedPub.E != pub.E {
		t.Errorf("指数不匹配: 期望 %d，实际得到 %d", pub.E, decodedPub.E)
	}
	if decodedPub.N.Cmp(pub.N) != 0 {
		t.Errorf("模数不匹配")
	} else {
		t.Logf("编码与解码无损，公钥还原成功！")
	}
}

func TestEncodePublicKeyWithName(t *testing.T) {
	priv := generateTestKey(t)
	pub := &priv.PublicKey
	deviceName := "test-device"

	encoded, err := EncodePublicKeyWithName(pub, deviceName)
	if err != nil {
		t.Fatalf("EncodePublicKeyWithName 失败: %v", err)
	}

	expectedSuffix := []byte(" " + deviceName + "\x00")
	if !bytes.HasSuffix(encoded, expectedSuffix) {
		t.Errorf("期望后缀 %q，实际后缀 %q", string(expectedSuffix), string(encoded[len(encoded)-len(expectedSuffix):]))
	}

	t.Logf("带名字的公钥编码成功")
}

func TestSignToken(t *testing.T) {
	priv := generateTestKey(t)

	// 模拟 20 字节的认证令牌
	token := make([]byte, 20)
	rand.Read(token)

	signature, err := Sign(priv, token)
	if err != nil {
		t.Fatalf("Sign 失败: %v", err)
	}

	// RSA-2048 签名长度应为 256 字节
	if len(signature) != 256 {
		t.Errorf("期望签名长度 256，实际得到 %d", len(signature))
	}

	// 验证签名
	err = rsa.VerifyPKCS1v15(&priv.PublicKey, crypto.SHA1, token, signature)
	if err != nil {
		t.Errorf("签名验证失败: %v", err)
	} else {
		t.Logf("成功对 20 字节令牌签名并验证通过")
	}
}

func TestGenerateTLSCertificate(t *testing.T) {
	priv := generateTestKey(t)

	cert, err := GenerateTLSCertificate(priv, "test-device")
	if err != nil {
		t.Fatalf("GenerateTLSCertificate 失败: %v", err)
	}

	if len(cert.Certificate) != 1 {
		t.Error("证书链长度不正确")
	}
	if cert.PrivateKey == nil {
		t.Error("私钥为空")
	}

	t.Logf("TLS 证书生成成功")
}
