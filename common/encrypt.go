package common

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

// EncryptedPrefix 标识渠道 Key 已加密的密文前缀（存量明文不带此前缀）。
const EncryptedPrefix = "enc:v1:"

// AESGCMEncrypt 使用 AES-256-GCM 加密明文，返回 "enc:v1:" + base64(nonce || ciphertext || tag)。
// key 必须是 32 字节（AES-256）。
func AESGCMEncrypt(key []byte, plaintext string) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}
	ciphertext := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	payload := append(nonce, ciphertext...)
	return EncryptedPrefix + base64.StdEncoding.EncodeToString(payload), nil
}

// AESGCMDecrypt 解密 AESGCMEncrypt 产生的密文。非本格式（无前缀）的输入直接报错。
func AESGCMDecrypt(key []byte, ciphertext string) (string, error) {
	if !strings.HasPrefix(ciphertext, EncryptedPrefix) {
		return "", errors.New("ciphertext missing encrypted prefix")
	}
	payload, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(ciphertext, EncryptedPrefix))
	if err != nil {
		return "", fmt.Errorf("failed to decode ciphertext: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create gcm: %w", err)
	}
	if len(payload) < gcm.NonceSize() {
		return "", errors.New("ciphertext too short")
	}
	nonce := payload[:gcm.NonceSize()]
	raw := payload[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, raw, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}
	return string(plaintext), nil
}
