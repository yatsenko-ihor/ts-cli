package util

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/zalando/go-keyring"
)

func useMockKeychain(t *testing.T) {
	t.Helper()
	keyring.MockInit()
	dir := t.TempDir()
	t.Setenv(ConfigDirEnv, dir)
	keyMu.Lock()
	resetKeyCache()
	keyMu.Unlock()
	t.Cleanup(func() {
		keyMu.Lock()
		resetKeyCache()
		keyMu.Unlock()
	})
}

func TestDefaultStoreIsFile(t *testing.T) {
	useMockKeychain(t)
	if CurrentSecretStore() != SecretStoreFile {
		t.Fatal("default store should be file")
	}
}

func TestUseKeychainMovesKeyAndKeepsDataReadable(t *testing.T) {
	useMockKeychain(t)
	dir, _ := ConfigDir()

	enc, err := Encrypt("secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, keyFileName)); err != nil {
		t.Fatal("file key should exist in file mode")
	}

	if err := UseKeychain(); err != nil {
		t.Fatalf("UseKeychain: %v", err)
	}
	if CurrentSecretStore() != SecretStoreKeychain {
		t.Fatal("store should now be keychain")
	}
	if _, err := os.Stat(filepath.Join(dir, keyFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("file key should be removed after moving to keychain")
	}
	if _, err := keyring.Get(keychainService, keychainAccount); err != nil {
		t.Fatalf("key missing from keychain: %v", err)
	}

	// Same key value → old ciphertext still opens.
	if plain, err := Decrypt(enc); err != nil || plain != "secret" {
		t.Fatalf("data unreadable after switch: %q %v", plain, err)
	}

	// Idempotent.
	if err := UseKeychain(); err != nil {
		t.Fatalf("second UseKeychain: %v", err)
	}
}

func TestUseFileMovesKeyBack(t *testing.T) {
	useMockKeychain(t)
	dir, _ := ConfigDir()

	enc, _ := Encrypt("secret")
	if err := UseKeychain(); err != nil {
		t.Fatal(err)
	}
	if err := UseFile(); err != nil {
		t.Fatalf("UseFile: %v", err)
	}
	if CurrentSecretStore() != SecretStoreFile {
		t.Fatal("store should be file again")
	}
	info, err := os.Stat(filepath.Join(dir, keyFileName))
	if err != nil {
		t.Fatal("key file should be recreated")
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("key file perm = %o", info.Mode().Perm())
	}
	if _, err := keyring.Get(keychainService, keychainAccount); !errors.Is(err, keyring.ErrNotFound) {
		t.Fatalf("keychain item should be deleted, got %v", err)
	}
	if plain, err := Decrypt(enc); err != nil || plain != "secret" {
		t.Fatalf("data unreadable after switching back: %q %v", plain, err)
	}
}

func TestKeychainUnavailableFallsBackToFileKeyWithNotice(t *testing.T) {
	useMockKeychain(t)
	enc, _ := Encrypt("secret")
	if err := UseKeychain(); err != nil {
		t.Fatal(err)
	}

	// Simulate a session where the keychain cannot be reached, but the user
	// still has a key file (e.g. restored from backup).
	key, _ := keychainGetKey()
	if err := writeFileKey(key); err != nil {
		t.Fatal(err)
	}
	keyring.MockInitWithError(errors.New("dbus: no session bus"))
	keyMu.Lock()
	resetKeyCache()
	keyMu.Unlock()

	plain, err := Decrypt(enc)
	if err != nil || plain != "secret" {
		t.Fatalf("expected fallback to file key, got %q %v", plain, err)
	}
	if KeyStoreNotice() == "" {
		t.Fatal("fallback must leave a notice for the UI")
	}
}

func TestKeychainUnavailableWithoutFileKeyFails(t *testing.T) {
	useMockKeychain(t)
	if err := UseKeychain(); err != nil {
		t.Fatal(err)
	}
	keyring.MockInitWithError(errors.New("keychain locked"))
	keyMu.Lock()
	resetKeyCache()
	keyMu.Unlock()

	if _, err := Encrypt("x"); err == nil {
		t.Fatal("expected an error: keychain unavailable and no key file; a new key must never be created silently")
	}
	if err := UseKeychain(); err == nil {
		t.Fatal("UseKeychain must fail when the keychain is unavailable")
	}
}

func TestKeychainAvailableProbe(t *testing.T) {
	keyring.MockInit()
	if err := KeychainAvailable(); err != nil {
		t.Fatalf("mock keychain should be available: %v", err)
	}
	keyring.MockInitWithError(errors.New("nope"))
	if err := KeychainAvailable(); err == nil {
		t.Fatal("expected unavailable")
	}
}
