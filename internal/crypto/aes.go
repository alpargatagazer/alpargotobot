package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
)

// Encrypt encrypts a plaintext string using AES-256-GCM.
// It returns the ciphertext (which includes the 16-byte auth tag appended) and the 12-byte nonce.
func Encrypt(key []byte, plaintext string) (ciphertext []byte, nonce []byte, err error) {
	if len(key) != 32 {
		return nil, nil, fmt.Errorf("invalid key size: must be exactly 32 bytes")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create aes cipher: %w", err)
	}

	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create gcm: %w", err)
	}

	nonce = make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, fmt.Errorf("failed to generate random nonce: %w", err)
	}

	ciphertext = aesgcm.Seal(nil, nonce, []byte(plaintext), nil)
	return ciphertext, nonce, nil
}

// Decrypt decrypts a ciphertext using AES-256-GCM and the provided 12-byte nonce.
func Decrypt(key []byte, ciphertext []byte, nonce []byte) (string, error) {
	if len(key) != 32 {
		return "", fmt.Errorf("invalid key size: must be exactly 32 bytes")
	}
	if len(nonce) != 12 {
		return "", fmt.Errorf("invalid nonce size: must be exactly 12 bytes")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create aes cipher: %w", err)
	}

	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create gcm: %w", err)
	}

	plaintextBytes, err := aesgcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	return string(plaintextBytes), nil
}
