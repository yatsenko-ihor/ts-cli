package util

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/zalando/go-keyring"
)

// Master-key storage.
//
// All secrets in ~/.ts-cli (SSH passwords, Tailscale API keys) are encrypted
// with one random 32-byte master key. The key lives in exactly one place:
//
//   - SecretStoreFile     (default) ~/.ts-cli/key, mode 0600
//   - SecretStoreKeychain the OS credential store (macOS Keychain, Linux
//     Secret Service via D-Bus, Windows Credential Manager)
//
// The active store is recorded in ~/.ts-cli/secret_store. It is a separate
// tiny file because it must be known before config.json can be decrypted.
//
// Fallback: when the store is keychain but the keychain cannot be reached
// (headless session, locked login keychain, no D-Bus), the file key is used
// if it still exists and a notice is recorded; nothing is created silently.

// SecretStore identifies where the master key is kept.
type SecretStore string

const (
	SecretStoreFile     SecretStore = "file"
	SecretStoreKeychain SecretStore = "keychain"

	secretStoreFileName = "secret_store"
	keyFileName         = "key"
	keySize             = 32 // AES-256

	keychainService = "ts-cli"
	keychainAccount = "master-key"
)

// Test seams: replaced only by tests to provoke failures that cannot be
// produced portably (or as root) with real files and keychains.
var (
	keyringGet    = keyring.Get
	keyringSet    = keyring.Set
	keyringDelete = keyring.Delete
	writeFile     = os.WriteFile
	removeFile    = os.Remove
	createTemp    = func(dir, pattern string) (keyTempFile, error) {
		f, err := os.CreateTemp(dir, pattern)
		if err != nil {
			return nil, err
		}
		return f, nil
	}
	openExclusive = func(path string) (io.WriteCloser, error) {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return nil, err
		}
		return f, nil
	}
)

// keyTempFile is the subset of *os.File that writeFileKey needs.
type keyTempFile interface {
	io.WriteCloser
	Chmod(os.FileMode) error
	Name() string
}

var (
	keyMu     sync.Mutex
	keyCache  []byte
	keyNotice string
	keyReset  bool // UseKeychain had to create a new key because the old one was lost
)

// ─── Store selection ──────────────────────────────────────────────────────────

func secretStorePath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, secretStoreFileName), nil
}

// CurrentSecretStore reports which store is configured (file when unset).
func CurrentSecretStore() SecretStore {
	path, err := secretStorePath()
	if err != nil {
		return SecretStoreFile
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return SecretStoreFile
	}
	if SecretStore(strings.TrimSpace(string(data))) == SecretStoreKeychain {
		return SecretStoreKeychain
	}
	return SecretStoreFile
}

func setSecretStore(s SecretStore) error {
	if _, err := EnsureConfigDir(); err != nil {
		return err
	}
	path, err := secretStorePath()
	if err != nil {
		return err
	}
	return writeFile(path, []byte(string(s)+"\n"), 0600)
}

// KeyStoreNotice returns a one-line warning recorded while loading the key
// (e.g. keychain unavailable, file key used instead), or "".
func KeyStoreNotice() string {
	keyMu.Lock()
	defer keyMu.Unlock()
	return keyNotice
}

// KeyWasReset reports whether the last UseKeychain call found the master key
// lost (keychain item gone, no key file) and created a new one. Secrets
// encrypted with the old key can no longer be read; the caller should remove
// them (config.DropUnreadableSecrets) and tell the user.
func KeyWasReset() bool {
	keyMu.Lock()
	defer keyMu.Unlock()
	return keyReset
}

// resetKeyCache forgets the cached key (after moving it between stores).
func resetKeyCache() {
	keyCache = nil
	keyNotice = ""
}

// ─── Keychain access ──────────────────────────────────────────────────────────

// KeychainAvailable reports whether the OS credential store can be used right
// now. A "not found" answer from the probe means the store works.
func KeychainAvailable() error {
	_, err := keyringGet(keychainService, "__probe__")
	if err == nil || errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

func keychainGetKey() ([]byte, error) {
	encoded, err := keyringGet(keychainService, keychainAccount)
	if err != nil {
		return nil, err
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return nil, fmt.Errorf("keychain item is not a valid key: %w", err)
	}
	if len(key) != keySize {
		return nil, fmt.Errorf("keychain item has invalid key length %d", len(key))
	}
	return key, nil
}

func keychainSetKey(key []byte) error {
	return keyringSet(keychainService, keychainAccount, base64.StdEncoding.EncodeToString(key))
}

func keychainDeleteKey() error {
	err := keyringDelete(keychainService, keychainAccount)
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return err
	}
	return nil
}

// ─── File access ──────────────────────────────────────────────────────────────

func keyFilePath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, keyFileName), nil
}

func readFileKey() ([]byte, error) {
	path, err := keyFilePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) != keySize {
		return nil, fmt.Errorf("encryption key file %s has invalid length %d", path, len(data))
	}
	return data, nil
}

func newRandomKey() ([]byte, error) {
	key := make([]byte, keySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("failed to generate encryption key: %w", err)
	}
	return key, nil
}

// writeFileKey writes key to ~/.ts-cli/key (0600), replacing any existing file.
func writeFileKey(key []byte) error {
	if _, err := EnsureConfigDir(); err != nil {
		return err
	}
	path, err := keyFilePath()
	if err != nil {
		return err
	}
	tmp, err := createTemp(filepath.Dir(path), keyFileName+".*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create key file: %w", err)
	}
	tmpName := tmp.Name()
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if _, err := tmp.Write(key); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("failed to write key file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName) // never leave key material behind in a stray temp file
		return err
	}
	return nil
}

// loadOrCreateFileKey returns the file key, creating it on first use.
// O_EXCL guards against two first runs racing to create different keys.
func loadOrCreateFileKey() ([]byte, error) {
	if key, err := readFileKey(); err == nil {
		return key, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("failed to read encryption key: %w", err)
	}

	key, err := newRandomKey()
	if err != nil {
		return nil, err
	}
	if _, err := EnsureConfigDir(); err != nil {
		return nil, err
	}
	path, err := keyFilePath()
	if err != nil {
		return nil, err
	}
	f, err := openExclusive(path)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return readFileKey()
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

func removeFileKey() error {
	path, err := keyFilePath()
	if err != nil {
		return err
	}
	if err := removeFile(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// ─── Master key resolution ────────────────────────────────────────────────────

// masterKey returns the key for the configured store, cached per process so
// the keychain is consulted (and may prompt) at most once per run.
func masterKey() ([]byte, error) {
	keyMu.Lock()
	defer keyMu.Unlock()

	if keyCache != nil {
		return keyCache, nil
	}

	var key []byte
	var err error
	switch CurrentSecretStore() {
	case SecretStoreKeychain:
		key, err = keychainGetKey()
		if err != nil {
			// Fallback: an existing file key, never a freshly created one.
			fileKey, ferr := readFileKey()
			if ferr != nil {
				return nil, fmt.Errorf("secret store is set to keychain but it is unavailable (%v) and no key file exists; "+
					"run 'ts-cli secrets use file' from a session where the keychain works", err)
			}
			keyNotice = fmt.Sprintf("keychain unavailable (%v); using the key file %s instead", err, keyFileName)
			key = fileKey
		}
	default:
		key, err = loadOrCreateFileKey()
		if err != nil {
			return nil, err
		}
	}

	keyCache = key
	return key, nil
}

// ─── Switching stores ─────────────────────────────────────────────────────────

// UseKeychain moves the master key into the OS credential store and removes
// the key file. Existing encrypted data stays readable because the key value
// does not change. It is a no-op when the keychain is already active.
func UseKeychain() error {
	keyMu.Lock()
	defer keyMu.Unlock()
	keyReset = false

	if err := KeychainAvailable(); err != nil {
		return fmt.Errorf("keychain is not available: %w", err)
	}
	if CurrentSecretStore() == SecretStoreKeychain {
		if _, err := keychainGetKey(); err == nil {
			return nil
		}
	}

	// The keychain is the configured store but its item is gone and there is
	// no key file either: the old key is lost and a new one is created below.
	lost := false
	if CurrentSecretStore() == SecretStoreKeychain {
		_, ferr := readFileKey()
		lost = errors.Is(ferr, os.ErrNotExist)
	}

	// The file key is authoritative while the file store is active.
	key, err := loadOrCreateFileKey()
	if err != nil {
		return err
	}
	if err := keychainSetKey(key); err != nil {
		return fmt.Errorf("failed to store key in keychain: %w", err)
	}
	// Verify before deleting anything.
	back, err := keychainGetKey()
	if err != nil || string(back) != string(key) {
		_ = keychainDeleteKey()
		return fmt.Errorf("keychain round-trip failed; key file left untouched (%v)", err)
	}
	if err := setSecretStore(SecretStoreKeychain); err != nil {
		_ = keychainDeleteKey()
		return err
	}
	if err := removeFileKey(); err != nil {
		return fmt.Errorf("key stored in keychain, but the key file could not be removed: %w", err)
	}
	resetKeyCache()
	keyReset = lost
	return nil
}

// UseFile moves the master key back into ~/.ts-cli/key and deletes the
// keychain item. It is a no-op when the file store is already active.
func UseFile() error {
	keyMu.Lock()
	defer keyMu.Unlock()

	if CurrentSecretStore() == SecretStoreFile {
		return nil
	}

	key, err := keychainGetKey()
	if err != nil {
		// Keychain unreachable: if the file key still exists (fallback was in
		// use) we can simply switch the mode back.
		if fileKey, ferr := readFileKey(); ferr == nil {
			key = fileKey
		} else {
			return fmt.Errorf("cannot read the key from the keychain (%v) and no key file exists; "+
				"switch back from a session where the keychain works", err)
		}
	}
	if err := writeFileKey(key); err != nil {
		return err
	}
	if err := setSecretStore(SecretStoreFile); err != nil {
		return err
	}
	if err := keychainDeleteKey(); err != nil {
		return fmt.Errorf("key file written, but the keychain item could not be removed: %w", err)
	}
	resetKeyCache()
	return nil
}
