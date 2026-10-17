package util

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func useTempKeyDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(ConfigDirEnv, dir)
	return dir
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	useTempKeyDir(t)

	for _, pw := range []string{"hunter2", "", "пароль з юнікодом 🔑", strings.Repeat("x", 4096)} {
		enc, err := EncryptPassword(pw)
		if err != nil {
			t.Fatalf("EncryptPassword(%q): %v", pw, err)
		}
		dec, err := DecryptPassword(enc)
		if err != nil {
			t.Fatalf("DecryptPassword: %v", err)
		}
		if dec != pw {
			t.Fatalf("round trip mismatch: got %q want %q", dec, pw)
		}
	}
}

func TestEncryptUsesFreshNonce(t *testing.T) {
	useTempKeyDir(t)

	a, _ := EncryptPassword("same")
	b, _ := EncryptPassword("same")
	if a == b {
		t.Fatal("two encryptions of the same plaintext produced identical ciphertext (nonce reuse)")
	}
}

func TestKeyFileCreatedWithRestrictedPermissions(t *testing.T) {
	dir := useTempKeyDir(t)

	if _, err := EncryptPassword("x"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, keyFileName))
	if err != nil {
		t.Fatalf("key file not created: %v", err)
	}
	if info.Size() != keySize {
		t.Fatalf("key size = %d, want %d", info.Size(), keySize)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("key file perm = %o, want 0600", perm)
	}
}

func TestDecryptRejectsTamperedCiphertext(t *testing.T) {
	useTempKeyDir(t)

	enc, err := EncryptPassword("secret")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := base64.StdEncoding.DecodeString(enc)
	raw[len(raw)-1] ^= 0xFF // flip a bit in the GCM tag
	if _, err := DecryptPassword(base64.StdEncoding.EncodeToString(raw)); err == nil {
		t.Fatal("expected tampered ciphertext to fail authentication")
	}
}

func TestDecryptRejectsShortAndInvalidInput(t *testing.T) {
	useTempKeyDir(t)

	if _, err := DecryptPassword("not base64!!"); err == nil {
		t.Fatal("expected error for invalid base64")
	}
	if _, err := DecryptPassword(base64.StdEncoding.EncodeToString([]byte("short"))); err == nil {
		t.Fatal("expected error for ciphertext shorter than nonce")
	}
}

func TestDecryptFallsBackToLegacyKey(t *testing.T) {
	useTempKeyDir(t)

	// Simulate data written by an older version using the host-derived key.
	legacy, err := legacyDeriveKey()
	if err != nil {
		t.Skip("cannot derive legacy key in this environment:", err)
	}
	gcm, err := newGCM(legacy)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, gcm.NonceSize())
	ct := gcm.Seal(nonce, nonce, []byte("old-password"), nil)

	dec, err := DecryptPassword(base64.StdEncoding.EncodeToString(ct))
	if err != nil {
		t.Fatalf("legacy fallback failed: %v", err)
	}
	if dec != "old-password" {
		t.Fatalf("got %q", dec)
	}
}

func TestDecryptWithDifferentKeyFails(t *testing.T) {
	useTempKeyDir(t)
	enc, _ := EncryptPassword("secret")

	// Switch to a fresh key directory: the ciphertext must no longer open.
	useTempKeyDir(t)
	if _, err := DecryptPassword(enc); err == nil {
		t.Fatal("ciphertext decrypted with a different key")
	}
}
