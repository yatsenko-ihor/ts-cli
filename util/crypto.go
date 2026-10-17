package util

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
)

const (
	keyFileName = "key"
	keySize     = 32 // AES-256
)

// keyFilePath returns the location of the random encryption key.
// It can be overridden in tests via TS_CLI_CONFIG_DIR.
func keyFilePath() (string, error) {
	dir := os.Getenv("TS_CLI_CONFIG_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		dir = filepath.Join(home, ".ts-cli")
	}
	return filepath.Join(dir, keyFileName), nil
}

// loadOrCreateKey returns the random 32-byte key stored in ~/.ts-cli/key,
// creating it (mode 0600) on first use. Keeping the key in a separate
// file means a leaked config.json alone is not enough to recover passwords,
// and the key no longer depends on the hostname (which changes on macOS
// with network location and breaks decryption silently).
func loadOrCreateKey() ([]byte, error) {
	path, err := keyFilePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err == nil {
		if len(data) != keySize {
			return nil, fmt.Errorf("encryption key file %s has invalid length %d", path, len(data))
		}
		return data, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("failed to read encryption key: %w", err)
	}

	key := make([]byte, keySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("failed to generate encryption key: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}
	// O_EXCL guards against a concurrent first run overwriting a key that
	// another process just created.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return loadOrCreateKey()
		}
		return nil, fmt.Errorf("failed to create encryption key: %w", err)
	}
	if _, err := f.Write(key); err != nil {
		f.Close()
		os.Remove(path)
		return nil, fmt.Errorf("failed to write encryption key: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return nil, fmt.Errorf("failed to write encryption key: %w", err)
	}
	return key, nil
}

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

// EncryptPassword encrypts a plaintext password using AES-256-GCM.
// Returns a base64-encoded ciphertext string suitable for storing in config.
func EncryptPassword(plaintext string) (string, error) {
	key, err := loadOrCreateKey()
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

// DecryptPassword decrypts a base64-encoded AES-256-GCM ciphertext
// back to the original plaintext password. It tries the key file first and
// falls back to the legacy host-derived key for data saved by older versions.
func DecryptPassword(encoded string) (string, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("failed to decode ciphertext: %w", err)
	}

	key, err := loadOrCreateKey()
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
