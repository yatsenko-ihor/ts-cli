package util

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

// setKeyCache installs a key directly into the process cache.
func setKeyCache(t *testing.T, key []byte) {
	t.Helper()
	keyMu.Lock()
	keyCache = key
	keyMu.Unlock()
}

// brokenConfigDir points the config directory at a path below a regular file,
// so every key operation fails with a non-"not exist" error.
func brokenConfigDir(t *testing.T) {
	t.Helper()
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(ConfigDirEnv, blocker)
	keyMu.Lock()
	resetKeyCache()
	keyMu.Unlock()
}

type failReader struct{}

func (failReader) Read([]byte) (int, error) { return 0, errors.New("entropy exhausted") }

func failRand(t *testing.T) (restore func()) {
	t.Helper()
	orig := rand.Reader
	rand.Reader = failReader{}
	restore = func() { rand.Reader = orig }
	t.Cleanup(restore)
	return restore
}

func legacyKeyFor(t *testing.T, host string) []byte {
	t.Helper()
	u, err := user.Current()
	if err != nil {
		t.Skip("no current user:", err)
	}
	h := sha256.Sum256([]byte("ts-cli:" + u.Username + ":" + u.Uid + ":" + host))
	return h[:]
}

func sealWith(t *testing.T, key []byte, plain string) []byte {
	t.Helper()
	gcm, err := newGCM(key)
	if err != nil {
		t.Fatal(err)
	}
	nonce := bytes.Repeat([]byte{9}, gcm.NonceSize())
	return gcm.Seal(nonce, nonce, []byte(plain), nil)
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

// Verifies: REQ-CRY-001
func TestCryptoRoundTripRange(t *testing.T) {
	useTempKeyDir(t)
	for _, pw := range []string{"", "a", "hunter2", "пароль 🔑", "line\nbreak\x00nul", strings.Repeat("x", 4095), strings.Repeat("y", 4096), strings.Repeat("z", 4097)} {
		enc, err := Encrypt(pw)
		if err != nil {
			t.Fatalf("Encrypt: %v", err)
		}
		dec, err := Decrypt(enc)
		if err != nil || dec != pw {
			t.Fatalf("round trip of %d bytes: %q, %v", len(pw), dec, err)
		}
	}
}

// Verifies: REQ-CRY-002
func TestEncryptFormatAndFreshNonce(t *testing.T) {
	useTempKeyDir(t)
	seen := map[string]bool{}
	nonces := map[string]bool{}
	for _, n := range []int{0, 1, 15, 16, 17, 100} {
		pt := strings.Repeat("p", n)
		enc, err := Encrypt(pt)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := base64.StdEncoding.DecodeString(enc)
		if err != nil {
			t.Fatalf("not standard base64: %v", err)
		}
		if len(raw) != 12+n+16 {
			t.Errorf("plaintext %d: decoded length %d, want %d", n, len(raw), 12+n+16)
		}
		nonces[string(raw[:12])] = true
	}
	for i := 0; i < 50; i++ {
		enc, _ := Encrypt("same")
		if seen[enc] {
			t.Fatal("identical ciphertext produced twice (nonce reuse)")
		}
		seen[enc] = true
		raw, _ := base64.StdEncoding.DecodeString(enc)
		nonces[string(raw[:12])] = true
	}
	if len(nonces) != 56 {
		t.Fatalf("nonce collision: %d distinct of 56", len(nonces))
	}
}

// Verifies: REQ-CRY-003
func TestEncryptUsesAES256GCMWithMasterKey(t *testing.T) {
	useTempKeyDir(t)
	enc, err := Encrypt("super-secret-value")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := base64.StdEncoding.DecodeString(enc)
	key, err := masterKey()
	if err != nil || len(key) != 32 {
		t.Fatalf("master key: %d bytes, %v", len(key), err)
	}
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	pt, err := gcm.Open(nil, raw[:12], raw[12:], nil)
	if err != nil || string(pt) != "super-secret-value" {
		t.Fatalf("independent AES-256-GCM open failed: %q %v", pt, err)
	}
	if bytes.Contains(raw, []byte("super-secret")) || strings.Contains(enc, "super-secret") {
		t.Fatal("plaintext visible in ciphertext")
	}
}

// Verifies: REQ-CRY-004
func TestEncryptFailsWithoutMasterKey(t *testing.T) {
	brokenConfigDir(t)
	if out, err := Encrypt("x"); err == nil || out != "" {
		t.Fatalf("Encrypt = %q, %v; want empty and error", out, err)
	}
}

// Verifies: REQ-CRY-005
func TestEncryptFailsWithInvalidKeyOrGCM(t *testing.T) {
	useTempKeyDir(t)
	for _, n := range []int{0, 5, 31, 33} {
		setKeyCache(t, make([]byte, n))
		out, err := Encrypt("x")
		if err == nil || out != "" || !strings.Contains(err.Error(), "cipher") {
			t.Errorf("key of %d bytes: Encrypt = %q, %v", n, out, err)
		}
	}
	setKeyCache(t, make([]byte, 32))
	orig := cipherNewGCM
	cipherNewGCM = func(cipher.Block) (cipher.AEAD, error) { return nil, errors.New("gcm boom") }
	defer func() { cipherNewGCM = orig }()
	out, err := Encrypt("x")
	if err == nil || out != "" || !strings.Contains(err.Error(), "GCM") {
		t.Fatalf("Encrypt = %q, %v", out, err)
	}
}

// Verifies: REQ-CRY-006
func TestEncryptFailsWhenRandomSourceFails(t *testing.T) {
	useTempKeyDir(t)
	setKeyCache(t, bytes.Repeat([]byte{1}, 32))
	failRand(t)
	out, err := Encrypt("x")
	if err == nil || out != "" || !strings.Contains(err.Error(), "nonce") {
		t.Fatalf("Encrypt = %q, %v", out, err)
	}
	// A short read must fail too, never produce a partially zero nonce.
	rand.Reader = io.LimitReader(bytes.NewReader(make([]byte, 20)), 5)
	if out, err := Encrypt("x"); err == nil || out != "" {
		t.Fatalf("short read: Encrypt = %q, %v", out, err)
	}
}

// Verifies: REQ-CRY-007
func TestDecryptRejectsInvalidBase64(t *testing.T) {
	useTempKeyDir(t)
	for _, s := range []string{"not base64!!", "====", "abc", "a b c d", "\x00"} {
		if out, err := Decrypt(s); err == nil || out != "" {
			t.Errorf("Decrypt(%q) = %q, %v", s, out, err)
		}
	}
}

// Verifies: REQ-CRY-008
func TestDecryptCiphertextLengthBoundary(t *testing.T) {
	useTempKeyDir(t)
	for _, n := range []int{0, 1, 11, 12, 27} {
		if out, err := Decrypt(b64(make([]byte, n))); err == nil || out != "" {
			t.Errorf("%d-byte ciphertext accepted: %q", n, out)
		}
	}
	// decryptWith distinguishes "too short" (< nonce) from authentication failure.
	key := bytes.Repeat([]byte{2}, 32)
	if _, err := decryptWith(key, make([]byte, 11)); err == nil || !strings.Contains(err.Error(), "too short") {
		t.Errorf("11 bytes: %v", err)
	}
	if _, err := decryptWith(key, make([]byte, 12)); err == nil || strings.Contains(err.Error(), "too short") {
		t.Errorf("12 bytes must reach authentication: %v", err)
	}
	// Minimal valid ciphertext: empty plaintext = 28 bytes.
	enc, _ := Encrypt("")
	raw, _ := base64.StdEncoding.DecodeString(enc)
	if len(raw) != 28 {
		t.Fatalf("minimal ciphertext = %d bytes", len(raw))
	}
	if out, err := Decrypt(enc); err != nil || out != "" {
		t.Fatalf("minimal ciphertext rejected: %q %v", out, err)
	}
}

// Verifies: REQ-CRY-009
func TestDecryptDetectsTamperingAnywhere(t *testing.T) {
	useTempKeyDir(t)
	enc, _ := Encrypt("secret-value")
	raw, _ := base64.StdEncoding.DecodeString(enc)
	for i := range raw { // nonce, body and tag
		mod := append([]byte(nil), raw...)
		mod[i] ^= 0x01
		if out, err := Decrypt(b64(mod)); err == nil || out != "" {
			t.Fatalf("byte %d altered but Decrypt = %q, %v", i, out, err)
		}
	}
	for _, mod := range [][]byte{raw[:len(raw)-1], append(append([]byte(nil), raw...), 0)} {
		if _, err := Decrypt(b64(mod)); err == nil {
			t.Fatal("truncated/extended ciphertext accepted")
		}
	}
}

// Verifies: REQ-CRY-010
func TestDecryptFailsWithoutMasterKey(t *testing.T) {
	useTempKeyDir(t)
	enc, _ := Encrypt("x")
	brokenConfigDir(t)
	if out, err := Decrypt(enc); err == nil || out != "" {
		t.Fatalf("Decrypt = %q, %v", out, err)
	}
}

// Verifies: REQ-CRY-011, REQ-CRY-014
func TestDecryptLegacyFallback(t *testing.T) {
	useTempKeyDir(t)
	host, err := os.Hostname()
	if err != nil {
		t.Skip("no hostname")
	}
	ct := sealWith(t, legacyKeyFor(t, host), "old-password")
	if out, err := Decrypt(b64(ct)); err != nil || out != "old-password" {
		t.Fatalf("legacy fallback: %q %v", out, err)
	}
	// The key derivation itself matches the documented formula.
	got, err := legacyDeriveKey()
	if err != nil || !bytes.Equal(got, legacyKeyFor(t, host)) {
		t.Fatalf("legacyDeriveKey mismatch: %v", err)
	}
}

// Verifies: REQ-CRY-014
func TestLegacyKeyUsesUnknownHostWhenHostnameFails(t *testing.T) {
	useTempKeyDir(t)
	orig := osHostname
	osHostname = func() (string, error) { return "", errors.New("no hostname") }
	defer func() { osHostname = orig }()
	got, err := legacyDeriveKey()
	if err != nil || !bytes.Equal(got, legacyKeyFor(t, "unknown-host")) {
		t.Fatalf("expected unknown-host derivation: %v", err)
	}
	ct := sealWith(t, legacyKeyFor(t, "unknown-host"), "pw")
	if out, err := Decrypt(b64(ct)); err != nil || out != "pw" {
		t.Fatalf("Decrypt = %q %v", out, err)
	}
}

// Verifies: REQ-CRY-012
func TestDecryptWrongKeyReportsGenericError(t *testing.T) {
	useTempKeyDir(t)
	ct := sealWith(t, bytes.Repeat([]byte{7}, 32), "TOPSECRET")
	out, err := Decrypt(b64(ct))
	if err == nil || out != "" {
		t.Fatalf("Decrypt = %q, %v", out, err)
	}
	if !strings.Contains(err.Error(), "wrong key or corrupted data") || strings.Contains(err.Error(), "TOPSECRET") {
		t.Fatalf("unexpected error text: %v", err)
	}
}

// Verifies: REQ-CRY-013, REQ-CRY-015
func TestLegacyKeyUserFailure(t *testing.T) {
	useTempKeyDir(t)
	orig := osCurrentUser
	osCurrentUser = func() (*user.User, error) { return nil, errors.New("no passwd entry") }
	defer func() { osCurrentUser = orig }()

	if k, err := legacyDeriveKey(); err == nil || k != nil {
		t.Fatalf("legacyDeriveKey = %v, %v", k, err)
	}
	// Ciphertext that the master key does not open: the derivation error surfaces.
	ct := sealWith(t, bytes.Repeat([]byte{7}, 32), "x")
	if out, err := Decrypt(b64(ct)); err == nil || out != "" || !strings.Contains(err.Error(), "current user") {
		t.Fatalf("Decrypt = %q, %v", out, err)
	}
	// Master-key data does not need the legacy key at all.
	enc, _ := Encrypt("fine")
	if out, err := Decrypt(enc); err != nil || out != "fine" {
		t.Fatalf("Decrypt = %q %v", out, err)
	}
}

// Verifies: REQ-CRY-016
func TestPasswordAliasesMatchEncryptDecrypt(t *testing.T) {
	useTempKeyDir(t)
	a, err := EncryptPassword("pw")
	if err != nil {
		t.Fatal(err)
	}
	if out, err := Decrypt(a); err != nil || out != "pw" {
		t.Fatalf("Decrypt(EncryptPassword) = %q %v", out, err)
	}
	b, _ := Encrypt("pw2")
	if out, err := DecryptPassword(b); err != nil || out != "pw2" {
		t.Fatalf("DecryptPassword(Encrypt) = %q %v", out, err)
	}
	if _, err := DecryptPassword("!!"); err == nil {
		t.Fatal("alias must propagate errors")
	}
	brokenConfigDir(t)
	if _, err := EncryptPassword("x"); err == nil {
		t.Fatal("alias must propagate errors")
	}
}

// Verifies: REQ-CRY-017
func TestNewGCMKeyLengths(t *testing.T) {
	for n, ok := range map[int]bool{0: false, 5: false, 15: false, 16: true, 23: false, 24: true, 31: false, 32: true, 33: false, 64: false} {
		g, err := newGCM(make([]byte, n))
		if (err == nil) != ok || (err == nil) != (g != nil) {
			t.Errorf("newGCM(%d bytes): %v", n, err)
		}
	}
	if _, err := decryptWith(make([]byte, 5), make([]byte, 40)); err == nil || !strings.Contains(err.Error(), "cipher") {
		t.Fatalf("decryptWith bad key: %v", err)
	}
}

// Verifies: REQ-CRY-018
func TestNewGCMReportsGCMFailure(t *testing.T) {
	orig := cipherNewGCM
	cipherNewGCM = func(cipher.Block) (cipher.AEAD, error) { return nil, errors.New("boom") }
	defer func() { cipherNewGCM = orig }()
	if g, err := newGCM(make([]byte, 32)); err == nil || g != nil || !strings.Contains(err.Error(), "failed to create GCM") {
		t.Fatalf("newGCM = %v, %v", g, err)
	}
}
