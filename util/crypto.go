package util

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/user"
)

// deriveKey derives a 32-byte AES key from a machine-specific seed.
// The seed combines the hostname and current OS user to tie the encrypted
// data to this machine/user pair. This is NOT a substitute for a proper
// key management system but provides reasonable protection for locally
// stored passwords (similar to how tools like KeePass derive keys).
func deriveKey() ([]byte, error) {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown-host"
	}

	u, err := user.Current()
	if err != nil {
		return nil, fmt.Errorf("failed to get current user: %w", err)
	}

	seed := fmt.Sprintf("ts-cli:%s:%s:%s", u.Username, u.Uid, hostname)
	hash := sha256.Sum256([]byte(seed))
	return hash[:], nil
}

// EncryptPassword encrypts a plaintext password using AES-256-GCM.
// Returns a base64-encoded ciphertext string suitable for storing in config.
func EncryptPassword(plaintext string) (string, error) {
	key, err := deriveKey()
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// DecryptPassword decrypts a base64-encoded AES-256-GCM ciphertext
// back to the original plaintext password.
func DecryptPassword(encoded string) (string, error) {
	key, err := deriveKey()
	if err != nil {
		return "", err
	}

	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("failed to decode ciphertext: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt (wrong machine or corrupted data): %w", err)
	}

	return string(plaintext), nil
}
