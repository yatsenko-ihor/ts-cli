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

// legacyDeriveKey reproduces the pre-key-file derivation
// (SHA-256 of "ts-cli:<user>:<uid>:<hostname>") so that passwords saved by
// earlier versions can still be decrypted. It is only used as a fallback.
func legacyDeriveKey() ([]byte, error) {
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

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}
	return gcm, nil
}

// Encrypt encrypts plaintext with the master key using AES-256-GCM and
// returns base64(nonce || ciphertext). Used for SSH passwords and API keys.
func Encrypt(plaintext string) (string, error) {
	key, err := masterKey()
	if err != nil {
		return "", err
	}

	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt reverses Encrypt. It tries the master key first and falls back to
// the legacy host-derived key for data saved by older versions.
func Decrypt(encoded string) (string, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("failed to decode ciphertext: %w", err)
	}

	key, err := masterKey()
	if err != nil {
		return "", err
	}
	if plaintext, err := decryptWith(key, ciphertext); err == nil {
		return plaintext, nil
	}

	legacyKey, err := legacyDeriveKey()
	if err != nil {
		return "", err
	}
	plaintext, err := decryptWith(legacyKey, ciphertext)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt (wrong key or corrupted data): %w", err)
	}
	return plaintext, nil
}

// EncryptPassword is kept as a readable alias for Encrypt.
func EncryptPassword(plaintext string) (string, error) { return Encrypt(plaintext) }

// DecryptPassword is kept as a readable alias for Decrypt.
func DecryptPassword(encoded string) (string, error) { return Decrypt(encoded) }

func decryptWith(key, ciphertext []byte) (string, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, body := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, body, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}
