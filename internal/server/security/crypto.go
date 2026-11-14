package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// EnvTraefikCryptoKey 系统主密钥环境变量名（用于敏感配置 AES-256-GCM 加解密）
const EnvTraefikCryptoKey = "TRAEFIK_CRYPTO_KEY"

const encryptedEnvelopePrefix = "enc:v1:"

// TODO(security): 后续接入企业 KMS/Vault 信封密钥轮换体系以替代静态环境变量主密钥。

// deriveAES256Key 从 TRAEFIK_CRYPTO_KEY 派生 32 字节 AES-256 密钥；未配置时 fail-closed 报错。
func deriveAES256Key(keyMaterial string) ([]byte, error) {
	trimmed := strings.TrimSpace(keyMaterial)
	if trimmed == "" {
		return nil, fmt.Errorf("未配置环境变量 %s，拒绝执行敏感凭据加密或降级明文存储", EnvTraefikCryptoKey)
	}
	sum := sha256.Sum256([]byte(trimmed))
	return sum[:], nil
}

// EncryptSecret 使用环境变量 TRAEFIK_CRYPTO_KEY 派生的 AES-256-GCM 密钥加密敏感文本。
func EncryptSecret(plaintext string) (string, error) {
	return EncryptSecretWithKey(plaintext, os.Getenv(EnvTraefikCryptoKey))
}

// EncryptSecretWithKey 使用指定密钥材料执行 AES-256-GCM 加密（带随机 96-bit Nonce）。
func EncryptSecretWithKey(plaintext, keyMaterial string) (string, error) {
	key, err := deriveAES256Key(keyMaterial)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("初始化 AES 算法失败: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("初始化 AES-GCM 失败: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("生成随机 Nonce 失败: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return encryptedEnvelopePrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// DecryptSecret 使用环境变量 TRAEFIK_CRYPTO_KEY 解密 AES-256-GCM 密文。
func DecryptSecret(ciphertext string) (string, error) {
	return DecryptSecretWithKey(ciphertext, os.Getenv(EnvTraefikCryptoKey))
}

// DecryptSecretWithKey 使用指定密钥材料解密 AES-256-GCM 密文。
func DecryptSecretWithKey(ciphertext, keyMaterial string) (string, error) {
	key, err := deriveAES256Key(keyMaterial)
	if err != nil {
		return "", err
	}
	trimmed := strings.TrimSpace(ciphertext)
	if !strings.HasPrefix(trimmed, encryptedEnvelopePrefix) {
		return "", errors.New("密文格式非法，缺少 enc:v1: 信封前缀")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(trimmed, encryptedEnvelopePrefix))
	if err != nil {
		return "", fmt.Errorf("密文 Base64 解码失败: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("初始化 AES 算法失败: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("初始化 AES-GCM 失败: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(raw) < nonceSize {
		return "", errors.New("密文长度不足，无法提取 Nonce")
	}
	nonce, payload := raw[:nonceSize], raw[nonceSize:]
	plainBytes, err := gcm.Open(nil, nonce, payload, nil)
	if err != nil {
		return "", fmt.Errorf("密文校验或解密失败: %w", err)
	}
	return string(plainBytes), nil
}

// ConstantTimeCompare 对两段令牌/摘要执行常量时间比对，抵御时序侧信道攻击。
func ConstantTimeCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
