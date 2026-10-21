package util

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/zalando/go-keyring"
)

var errBoom = errors.New("boom")

func key32(b byte) []byte { return bytes.Repeat([]byte{b}, keySize) }

func cacheState() (key []byte, notice string) {
	keyMu.Lock()
	defer keyMu.Unlock()
	return keyCache, keyNotice
}

func writeStoreFile(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, secretStoreFileName), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func writeKeyFile(t *testing.T, dir string, data []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, keyFileName), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// stubKeyringGet wraps keyringGet; fn may return handled=false to fall through.
func stubKeyringGet(t *testing.T, fn func(service, account string) (string, error, bool)) {
	t.Helper()
	orig := keyringGet
	keyringGet = func(s, a string) (string, error) {
		if v, err, handled := fn(s, a); handled {
			return v, err
		}
		return orig(s, a)
	}
	t.Cleanup(func() { keyringGet = orig })
}

func stubKeyringSet(t *testing.T, fn func(s, a, v string) error) {
	t.Helper()
	orig := keyringSet
	keyringSet = fn
	t.Cleanup(func() { keyringSet = orig })
}

func stubKeyringDelete(t *testing.T, fn func(s, a string) error) {
	t.Helper()
	orig := keyringDelete
	keyringDelete = fn
	t.Cleanup(func() { keyringDelete = orig })
}

func stubWriteFile(t *testing.T, err error) {
	t.Helper()
	orig := writeFile
	writeFile = func(string, []byte, os.FileMode) error { return err }
	t.Cleanup(func() { writeFile = orig })
}

func stubRemoveFile(t *testing.T, err error) {
	t.Helper()
	orig := removeFile
	removeFile = func(string) error { return err }
	t.Cleanup(func() { removeFile = orig })
}

// wrapTemp / wrapWriter inject chmod/write/close failures into real files.
type wrapTemp struct {
	*os.File
	chmodErr, writeErr, closeErr error
}

func (w *wrapTemp) Chmod(m os.FileMode) error {
	if w.chmodErr != nil {
		return w.chmodErr
	}
	return w.File.Chmod(m)
}
func (w *wrapTemp) Write(b []byte) (int, error) {
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	return w.File.Write(b)
}
func (w *wrapTemp) Close() error {
	err := w.File.Close()
	if w.closeErr != nil {
		return w.closeErr
	}
	return err
}

func stubCreateTemp(t *testing.T, mk func(dir, pattern string) (keyTempFile, error)) {
	t.Helper()
	orig := createTemp
	createTemp = mk
	t.Cleanup(func() { createTemp = orig })
}

func stubTempFailure(t *testing.T, chmodErr, writeErr, closeErr error) {
	t.Helper()
	orig := createTemp
	stubCreateTemp(t, func(dir, pattern string) (keyTempFile, error) {
		f, err := os.CreateTemp(dir, pattern)
		if err != nil {
			return nil, err
		}
		return &wrapTemp{File: f, chmodErr: chmodErr, writeErr: writeErr, closeErr: closeErr}, nil
	})
	_ = orig
}

type wrapWriter struct {
	*os.File
	writeErr, closeErr error
}

func (w *wrapWriter) Write(b []byte) (int, error) {
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	return w.File.Write(b)
}
func (w *wrapWriter) Close() error {
	err := w.File.Close()
	if w.closeErr != nil {
		return w.closeErr
	}
	return err
}

func stubOpenExclusive(t *testing.T, fn func(path string) (io.WriteCloser, error)) {
	t.Helper()
	orig := openExclusive
	openExclusive = fn
	t.Cleanup(func() { openExclusive = orig })
}

func noTempFiles(t *testing.T, dir string) {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(dir, "*.tmp"))
	if len(m) != 0 {
		t.Errorf("temporary key files left behind: %v", m)
	}
}

// Verifies: REQ-KEY-001
func TestCurrentSecretStoreDefaultsToFile(t *testing.T) {
	useMockKeychain(t)
	if got := CurrentSecretStore(); got != SecretStoreFile {
		t.Fatalf("got %q", got)
	}
}

// Verifies: REQ-KEY-002
func TestCurrentSecretStoreKeychain(t *testing.T) {
	for _, c := range []string{"keychain", "keychain\n", "  keychain \r\n", "\tkeychain"} {
		useMockKeychain(t)
		dir, _ := ConfigDir()
		writeStoreFile(t, dir, c)
		if got := CurrentSecretStore(); got != SecretStoreKeychain {
			t.Errorf("content %q: got %q", c, got)
		}
	}
}

// Verifies: REQ-KEY-003
func TestCurrentSecretStoreOtherContentIsFile(t *testing.T) {
	for _, c := range []string{"", "file", "file\n", "KEYCHAIN", "Keychain", "keychains", "keychain extra", "xkeychain", "keychain\nfile", "\x00"} {
		useMockKeychain(t)
		dir, _ := ConfigDir()
		writeStoreFile(t, dir, c)
		if got := CurrentSecretStore(); got != SecretStoreFile {
			t.Errorf("content %q: got %q", c, got)
		}
	}
	// Unreadable: secret_store is a directory.
	useMockKeychain(t)
	dir, _ := ConfigDir()
	if err := os.Mkdir(filepath.Join(dir, secretStoreFileName), 0700); err != nil {
		t.Fatal(err)
	}
	if got := CurrentSecretStore(); got != SecretStoreFile {
		t.Fatalf("directory: got %q", got)
	}
}

// Verifies: REQ-KEY-004
func TestCurrentSecretStoreWithoutConfigDir(t *testing.T) {
	stubHome(t, 0)
	if got := CurrentSecretStore(); got != SecretStoreFile {
		t.Fatalf("got %q", got)
	}
}

// Verifies: REQ-KEY-005
func TestSetSecretStoreWritesFile0600(t *testing.T) {
	useMockKeychain(t)
	dir := filepath.Join(t.TempDir(), "new", "cfg")
	t.Setenv(ConfigDirEnv, dir)
	for _, s := range []SecretStore{SecretStoreKeychain, SecretStoreFile} {
		if err := setSecretStore(s); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(dir, secretStoreFileName))
		if err != nil || string(b) != string(s)+"\n" {
			t.Fatalf("content %q %v", b, err)
		}
		if CurrentSecretStore() != s {
			t.Fatalf("round trip failed for %s", s)
		}
	}
	info, _ := os.Stat(filepath.Join(dir, secretStoreFileName))
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
	if d, _ := os.Stat(dir); d.Mode().Perm() != 0700 {
		t.Fatalf("dir mode %o", d.Mode().Perm())
	}
}

// Verifies: REQ-KEY-006
func TestSetSecretStoreErrors(t *testing.T) {
	// Directory cannot be created / resolved at the first lookup.
	stubHome(t, 0)
	if err := setSecretStore(SecretStoreFile); err == nil {
		t.Error("expected error: no config dir")
	}
	// Path cannot be resolved at the second lookup.
	stubHome(t, 1)
	if err := setSecretStore(SecretStoreFile); err == nil {
		t.Error("expected error: second ConfigDir lookup fails")
	}
	// Directory blocked by a regular file.
	brokenConfigDir(t)
	if err := setSecretStore(SecretStoreFile); err == nil {
		t.Error("expected error: file blocks directory")
	}
	// Write fails.
	useMockKeychain(t)
	stubWriteFile(t, errBoom)
	if err := setSecretStore(SecretStoreFile); !errors.Is(err, errBoom) {
		t.Errorf("expected write error, got %v", err)
	}
}

// Verifies: REQ-KEY-007, REQ-KEY-008
func TestKeyStoreNoticeAndResetKeyCache(t *testing.T) {
	useMockKeychain(t)
	if KeyStoreNotice() != "" {
		t.Fatal("notice must start empty")
	}
	keyMu.Lock()
	keyCache = key32(1)
	keyNotice = "something happened"
	keyMu.Unlock()
	if KeyStoreNotice() != "something happened" {
		t.Fatal("notice not returned")
	}
	keyMu.Lock()
	resetKeyCache()
	keyMu.Unlock()
	if k, n := cacheState(); k != nil || n != "" || KeyStoreNotice() != "" {
		t.Fatalf("reset left key=%v notice=%q", k, n)
	}
}

// Verifies: REQ-KEY-009
func TestKeychainAvailableMCDC(t *testing.T) {
	keyring.MockInit()
	// A: nil. Probe item exists in the mock.
	if err := keyring.Set(keychainService, "__probe__", "v"); err != nil {
		t.Fatal(err)
	}
	if err := KeychainAvailable(); err != nil {
		t.Errorf("probe nil: %v", err)
	}
	// B: ErrNotFound
	keyring.MockInit()
	if err := KeychainAvailable(); err != nil {
		t.Errorf("probe not found: %v", err)
	}
	// both false: other error
	keyring.MockInitWithError(errBoom)
	if err := KeychainAvailable(); !errors.Is(err, errBoom) {
		t.Errorf("probe error: %v", err)
	}
	// Wrapped ErrNotFound still counts as available.
	stubKeyringGet(t, func(string, string) (string, error, bool) {
		return "", errors.Join(errBoom, keyring.ErrNotFound), true
	})
	if err := KeychainAvailable(); err != nil {
		t.Errorf("wrapped not found: %v", err)
	}
}

// Verifies: REQ-KEY-010
func TestKeychainGetKeyDecodes(t *testing.T) {
	keyring.MockInit()
	enc := base64.StdEncoding.EncodeToString(key32(5))
	for _, v := range []string{enc, " " + enc + "\n", "\t" + enc} {
		if err := keyring.Set(keychainService, keychainAccount, v); err != nil {
			t.Fatal(err)
		}
		got, err := keychainGetKey()
		if err != nil || !bytes.Equal(got, key32(5)) {
			t.Errorf("%q: %v %v", v, got, err)
		}
	}
}

// Verifies: REQ-KEY-011
func TestKeychainGetKeyRejectsBadItems(t *testing.T) {
	keyring.MockInit()
	for name, v := range map[string]string{
		"not base64": "!!!notbase64",
		"empty":      "",
		"len 31":     base64.StdEncoding.EncodeToString(key32(1)[:31]),
		"len 33":     base64.StdEncoding.EncodeToString(append(key32(1), 1)),
		"len 64":     base64.StdEncoding.EncodeToString(append(key32(1), key32(2)...)),
	} {
		if err := keyring.Set(keychainService, keychainAccount, v); err != nil {
			t.Fatal(err)
		}
		if k, err := keychainGetKey(); err == nil || k != nil {
			t.Errorf("%s accepted: %v", name, k)
		}
	}
	keyring.MockInit() // item missing
	if _, err := keychainGetKey(); !errors.Is(err, keyring.ErrNotFound) {
		t.Errorf("missing item: %v", err)
	}
	keyring.MockInitWithError(errBoom)
	if _, err := keychainGetKey(); !errors.Is(err, errBoom) {
		t.Errorf("read failure: %v", err)
	}
}

// Verifies: REQ-KEY-012
func TestKeychainSetKey(t *testing.T) {
	keyring.MockInit()
	if err := keychainSetKey(key32(3)); err != nil {
		t.Fatal(err)
	}
	v, err := keyring.Get("ts-cli", "master-key")
	if err != nil || v != base64.StdEncoding.EncodeToString(key32(3)) {
		t.Fatalf("stored %q %v", v, err)
	}
	keyring.MockInitWithError(errBoom)
	if err := keychainSetKey(key32(3)); !errors.Is(err, errBoom) {
		t.Fatalf("got %v", err)
	}
}

// Verifies: REQ-KEY-013
func TestKeychainDeleteKeyMCDC(t *testing.T) {
	keyring.MockInit()
	if err := keychainSetKey(key32(1)); err != nil {
		t.Fatal(err)
	}
	if err := keychainDeleteKey(); err != nil { // A=F: success
		t.Errorf("existing item: %v", err)
	}
	if _, err := keyring.Get(keychainService, keychainAccount); !errors.Is(err, keyring.ErrNotFound) {
		t.Error("item not deleted")
	}
	if err := keychainDeleteKey(); err != nil { // A=T, B=F: not found
		t.Errorf("absent item: %v", err)
	}
	keyring.MockInitWithError(errBoom)
	if err := keychainDeleteKey(); !errors.Is(err, errBoom) { // A=T, B=T
		t.Errorf("other error: %v", err)
	}
}

// Verifies: REQ-KEY-014
func TestReadFileKeyLengthBoundary(t *testing.T) {
	dir := useTempKeyDir(t)
	for n, ok := range map[int]bool{0: false, 1: false, 31: false, 32: true, 33: false, 64: false} {
		writeKeyFile(t, dir, bytes.Repeat([]byte{4}, n))
		k, err := readFileKey()
		if (err == nil) != ok {
			t.Errorf("len %d: err=%v", n, err)
		}
		if ok && !bytes.Equal(k, key32(4)) {
			t.Error("wrong key returned")
		}
		if !ok && k != nil {
			t.Errorf("len %d: key returned with error", n)
		}
		if !ok && err != nil && n > 0 && !strings.Contains(err.Error(), "invalid length") {
			t.Errorf("len %d: %v", n, err)
		}
	}
	os.Remove(filepath.Join(dir, keyFileName))
	if _, err := readFileKey(); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing: %v", err)
	}
	stubHome(t, 0)
	if _, err := readFileKey(); err == nil {
		t.Error("expected path error")
	}
}

// Verifies: REQ-KEY-015
func TestNewRandomKey(t *testing.T) {
	a, err := newRandomKey()
	b, _ := newRandomKey()
	if err != nil || len(a) != keySize || len(b) != keySize || bytes.Equal(a, b) || bytes.Equal(a, make([]byte, keySize)) {
		t.Fatalf("bad random keys: %v %x %x", err, a, b)
	}
	failRand(t)
	if k, err := newRandomKey(); err == nil || k != nil {
		t.Fatalf("expected failure, got %v %v", k, err)
	}
}

// Verifies: REQ-KEY-016
func TestWriteFileKeyReplacesAtomically(t *testing.T) {
	dir := useTempKeyDir(t)
	for _, k := range [][]byte{key32(1), key32(2)} {
		if err := writeFileKey(k); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(dir, keyFileName))
		if err != nil || !bytes.Equal(got, k) {
			t.Fatalf("content %v %v", got, err)
		}
		info, _ := os.Stat(filepath.Join(dir, keyFileName))
		if info.Mode().Perm() != 0600 {
			t.Fatalf("mode %o", info.Mode().Perm())
		}
		noTempFiles(t, dir)
	}
	// Creates the directory when needed.
	fresh := filepath.Join(t.TempDir(), "x")
	t.Setenv(ConfigDirEnv, fresh)
	if err := writeFileKey(key32(9)); err != nil || !fileExists(filepath.Join(fresh, keyFileName)) {
		t.Fatalf("fresh dir: %v", err)
	}
}

// Verifies: REQ-KEY-017
func TestWriteFileKeyFailures(t *testing.T) {
	// Directory cannot be ensured / path cannot be resolved.
	stubHome(t, 0)
	if err := writeFileKey(key32(1)); err == nil {
		t.Error("expected error: ensure dir")
	}
	stubHome(t, 1)
	if err := writeFileKey(key32(1)); err == nil {
		t.Error("expected error: keyFilePath")
	}
	brokenConfigDir(t)
	if err := writeFileKey(key32(1)); err == nil {
		t.Error("expected error: file blocks dir")
	}

	// createTemp default implementation fails in a missing directory.
	if f, err := createTemp(filepath.Join(t.TempDir(), "missing"), "k.*.tmp"); err == nil || f != nil {
		t.Error("createTemp must fail in a missing directory")
	}
	// createTemp failure inside writeFileKey.
	useTempKeyDir(t)
	stubCreateTemp(t, func(string, string) (keyTempFile, error) { return nil, errBoom })
	if err := writeFileKey(key32(1)); err == nil || !strings.Contains(err.Error(), "failed to create key file") {
		t.Errorf("createTemp: %v", err)
	}
	for name, tc := range map[string][3]error{
		"chmod": {errBoom, nil, nil},
		"write": {nil, errBoom, nil},
		"close": {nil, nil, errBoom},
	} {
		dir := useTempKeyDir(t)
		stubTempFailure(t, tc[0], tc[1], tc[2])
		if err := writeFileKey(key32(1)); !errors.Is(err, errBoom) {
			t.Errorf("%s: %v", name, err)
		}
		noTempFiles(t, dir)
		if fileExists(filepath.Join(dir, keyFileName)) {
			t.Errorf("%s: key file must not exist after failure", name)
		}
	}
}

// Verifies: REQ-KEY-017
func TestWriteFileKeyRenameFailureCleansUp(t *testing.T) {
	dir := useTempKeyDir(t)
	// A non-empty directory occupies the key path: rename onto it fails.
	if err := os.MkdirAll(filepath.Join(dir, keyFileName, "sub"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := writeFileKey(key32(1)); err == nil {
		t.Fatal("expected rename failure")
	}
	noTempFiles(t, dir)
}

// Verifies: REQ-KEY-018
func TestLoadOrCreateFileKeyReturnsExisting(t *testing.T) {
	dir := useTempKeyDir(t)
	writeKeyFile(t, dir, key32(6))
	before, _ := os.Stat(filepath.Join(dir, keyFileName))
	k, err := loadOrCreateFileKey()
	if err != nil || !bytes.Equal(k, key32(6)) {
		t.Fatalf("%v %v", k, err)
	}
	after, _ := os.Stat(filepath.Join(dir, keyFileName))
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("existing key file was rewritten")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("unexpected files: %d", len(entries))
	}
}

// Verifies: REQ-KEY-019
func TestLoadOrCreateFileKeyCreates(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cfg")
	t.Setenv(ConfigDirEnv, dir)
	k, err := loadOrCreateFileKey()
	if err != nil || len(k) != keySize {
		t.Fatalf("%v %v", k, err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, keyFileName))
	if !bytes.Equal(got, k) {
		t.Error("returned key differs from stored key")
	}
	if info, _ := os.Stat(filepath.Join(dir, keyFileName)); info.Mode().Perm() != 0600 {
		t.Errorf("key mode %o", info.Mode().Perm())
	}
	if info, _ := os.Stat(dir); info.Mode().Perm() != 0700 {
		t.Errorf("dir mode %o", info.Mode().Perm())
	}
	if k2, _ := loadOrCreateFileKey(); !bytes.Equal(k, k2) {
		t.Error("second call must return the same key")
	}
	// Default exclusive-create fails cleanly on a missing directory.
	if w, err := openExclusive(filepath.Join(dir, "no", "such")); err == nil || w != nil {
		t.Error("openExclusive must fail in a missing directory")
	}
}

// Verifies: REQ-KEY-020
func TestLoadOrCreateFileKeyNeverReplacesCorruptKey(t *testing.T) {
	for _, n := range []int{0, 1, 31, 33} {
		dir := useTempKeyDir(t)
		bad := bytes.Repeat([]byte{8}, n)
		writeKeyFile(t, dir, bad)
		if k, err := loadOrCreateFileKey(); err == nil || k != nil || !strings.Contains(err.Error(), "failed to read encryption key") {
			t.Errorf("len %d: %v %v", n, k, err)
		}
		if got, _ := os.ReadFile(filepath.Join(dir, keyFileName)); !bytes.Equal(got, bad) {
			t.Errorf("len %d: corrupt key file was modified", n)
		}
	}
	// Key path occupied by a directory, and config dir blocked by a file.
	dir := useTempKeyDir(t)
	os.Mkdir(filepath.Join(dir, keyFileName), 0700)
	if _, err := loadOrCreateFileKey(); err == nil {
		t.Error("directory as key accepted")
	}
	brokenConfigDir(t)
	if _, err := loadOrCreateFileKey(); err == nil {
		t.Error("blocked config dir accepted")
	}
	stubHome(t, 0)
	if _, err := loadOrCreateFileKey(); err == nil {
		t.Error("unresolvable config dir accepted")
	}
}

// Verifies: REQ-KEY-021
func TestLoadOrCreateFileKeyRace(t *testing.T) {
	dir := useTempKeyDir(t)
	stubOpenExclusive(t, func(path string) (io.WriteCloser, error) {
		// The other process wins the race.
		if err := os.WriteFile(path, key32(0xAA), 0600); err != nil {
			t.Fatal(err)
		}
		return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrExist}
	})
	k, err := loadOrCreateFileKey()
	if err != nil || !bytes.Equal(k, key32(0xAA)) {
		t.Fatalf("%v %v", k, err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, keyFileName)); !bytes.Equal(got, key32(0xAA)) {
		t.Error("winner's key was overwritten")
	}

	// The winner's file is corrupt: its read error is returned, nothing replaced.
	useTempKeyDir(t)
	stubOpenExclusive(t, func(path string) (io.WriteCloser, error) {
		os.WriteFile(path, []byte("short"), 0600)
		return nil, os.ErrExist
	})
	if _, err := loadOrCreateFileKey(); err == nil {
		t.Error("expected error for corrupt winner")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, keyFileName)); string(got) != "short" {
		t.Error("corrupt winner replaced")
	}
}

// Verifies: REQ-KEY-022
func TestLoadOrCreateFileKeyFailures(t *testing.T) {
	// Random source.
	dir := useTempKeyDir(t)
	restore := failRand(t)
	if k, err := loadOrCreateFileKey(); err == nil || k != nil {
		t.Error("expected RNG failure")
	}
	if fileExists(filepath.Join(dir, keyFileName)) {
		t.Error("no key file may exist after RNG failure")
	}
	restore()

	// Ensure-dir (second lookup) and keyFilePath (third lookup).
	stubHome(t, 1)
	if _, err := loadOrCreateFileKey(); err == nil {
		t.Error("expected ensure-dir failure")
	}
	stubHome(t, 2)
	if _, err := loadOrCreateFileKey(); err == nil {
		t.Error("expected keyFilePath failure")
	}

	// Exclusive create fails for a reason other than "exists".
	useTempKeyDir(t)
	stubOpenExclusive(t, func(string) (io.WriteCloser, error) { return nil, errBoom })
	if _, err := loadOrCreateFileKey(); !errors.Is(err, errBoom) || !strings.Contains(err.Error(), "failed to create encryption key") {
		t.Errorf("open: %v", err)
	}

	// Write and close failures remove the partial file.
	for name, tc := range map[string][2]error{"write": {errBoom, nil}, "close": {nil, errBoom}} {
		dir = useTempKeyDir(t)
		stubOpenExclusive(t, func(path string) (io.WriteCloser, error) {
			f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				return nil, err
			}
			return &wrapWriter{File: f, writeErr: tc[0], closeErr: tc[1]}, nil
		})
		if k, err := loadOrCreateFileKey(); !errors.Is(err, errBoom) || k != nil {
			t.Errorf("%s: %v %v", name, k, err)
		}
		if fileExists(filepath.Join(dir, keyFileName)) {
			t.Errorf("%s: partial key file not removed", name)
		}
	}
}

// Verifies: REQ-KEY-023
func TestRemoveFileKeyMCDC(t *testing.T) {
	dir := useTempKeyDir(t)
	writeKeyFile(t, dir, key32(1))
	if err := removeFileKey(); err != nil { // A=F
		t.Fatal(err)
	}
	if fileExists(filepath.Join(dir, keyFileName)) {
		t.Fatal("key file not removed")
	}
	if err := removeFileKey(); err != nil { // A=T B=F: absent
		t.Fatalf("absent: %v", err)
	}
	stubRemoveFile(t, errBoom)
	if err := removeFileKey(); !errors.Is(err, errBoom) { // A=T B=T
		t.Fatalf("other error: %v", err)
	}
	// Real non-removable case: non-empty directory at the key path.
	dir = useTempKeyDir(t)
	os.MkdirAll(filepath.Join(dir, keyFileName, "sub"), 0700)
	removeFile = os.Remove
	if err := removeFileKey(); err == nil {
		t.Fatal("expected error removing non-empty directory")
	}
	stubHome(t, 0)
	if err := removeFileKey(); err == nil {
		t.Fatal("expected path error")
	}
}

// Verifies: REQ-KEY-024
func TestMasterKeyIsCached(t *testing.T) {
	dir := useTempKeyDir(t)
	k1, err := masterKey()
	if err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(dir, keyFileName))
	writeStoreFile(t, dir, "keychain\n") // would now fail if consulted
	keyring.MockInitWithError(errBoom)
	k2, err := masterKey()
	if err != nil || !bytes.Equal(k1, k2) {
		t.Fatalf("cache not used: %v", err)
	}
	if fileExists(filepath.Join(dir, keyFileName)) {
		t.Fatal("key file recreated although the key was cached")
	}
}

// Verifies: REQ-KEY-025
func TestMasterKeyFileStore(t *testing.T) {
	dir := useTempKeyDir(t)
	k, err := masterKey()
	if err != nil || len(k) != keySize {
		t.Fatalf("%v %v", k, err)
	}
	if onDisk, _ := os.ReadFile(filepath.Join(dir, keyFileName)); !bytes.Equal(onDisk, k) {
		t.Error("created key not persisted")
	}
	// Failure from loadOrCreateFileKey is propagated and nothing cached.
	dir = useTempKeyDir(t)
	writeKeyFile(t, dir, []byte("short"))
	if k, err := masterKey(); err == nil || k != nil {
		t.Fatalf("%v %v", k, err)
	}
	if c, _ := cacheState(); c != nil {
		t.Error("failed lookup must not be cached")
	}
}

// Verifies: REQ-KEY-026
func TestMasterKeyKeychainStore(t *testing.T) {
	useMockKeychain(t)
	dir, _ := ConfigDir()
	writeStoreFile(t, dir, "keychain\n")
	if err := keychainSetKey(key32(0x42)); err != nil {
		t.Fatal(err)
	}
	k, err := masterKey()
	if err != nil || !bytes.Equal(k, key32(0x42)) {
		t.Fatalf("%v %v", k, err)
	}
	if KeyStoreNotice() != "" {
		t.Error("no notice expected when the keychain works")
	}
	if fileExists(filepath.Join(dir, keyFileName)) {
		t.Error("no key file may be created in keychain mode")
	}
}

// Verifies: REQ-KEY-027
func TestMasterKeyKeychainFallbackToFile(t *testing.T) {
	useMockKeychain(t)
	dir, _ := ConfigDir()
	writeStoreFile(t, dir, "keychain\n")
	writeKeyFile(t, dir, key32(0x43))
	keyring.MockInitWithError(errors.New("dbus gone"))
	k, err := masterKey()
	if err != nil || !bytes.Equal(k, key32(0x43)) {
		t.Fatalf("%v %v", k, err)
	}
	if n := KeyStoreNotice(); !strings.Contains(n, "dbus gone") || !strings.Contains(n, "keychain unavailable") {
		t.Fatalf("notice %q", n)
	}
}

// Verifies: REQ-KEY-028
func TestMasterKeyKeychainUnavailableNoFile(t *testing.T) {
	for name, prep := range map[string]func(dir string){
		"no file":      func(string) {},
		"invalid file": func(dir string) { writeKeyFile(t, dir, []byte("short")) },
	} {
		useMockKeychain(t)
		dir, _ := ConfigDir()
		writeStoreFile(t, dir, "keychain\n")
		prep(dir)
		keyring.MockInitWithError(errBoom)
		k, err := masterKey()
		if err == nil || k != nil || !strings.Contains(err.Error(), "ts-cli secrets use file") {
			t.Fatalf("%s: %v %v", name, k, err)
		}
		if c, n := cacheState(); c != nil || n != "" {
			t.Errorf("%s: state cached", name)
		}
		if name == "no file" && fileExists(filepath.Join(dir, keyFileName)) {
			t.Error("a key file was created silently")
		}
	}
}

// Verifies: REQ-KEY-029
func TestUseKeychainUnavailableChangesNothing(t *testing.T) {
	useMockKeychain(t)
	dir, _ := ConfigDir()
	writeKeyFile(t, dir, key32(1))
	keyring.MockInitWithError(errBoom)
	if err := UseKeychain(); err == nil || !strings.Contains(err.Error(), "keychain is not available") {
		t.Fatalf("%v", err)
	}
	if CurrentSecretStore() != SecretStoreFile || !fileExists(filepath.Join(dir, keyFileName)) {
		t.Error("state changed")
	}
}

// Verifies: REQ-KEY-030
func TestUseKeychainNoOpWhenAlreadyActive(t *testing.T) {
	useMockKeychain(t)
	if _, err := Encrypt("x"); err != nil {
		t.Fatal(err)
	}
	if err := UseKeychain(); err != nil {
		t.Fatal(err)
	}
	sets := 0
	stubKeyringSet(t, func(string, string, string) error { sets++; return nil })
	if err := UseKeychain(); err != nil {
		t.Fatal(err)
	}
	if sets != 0 {
		t.Error("no-op must not write the keychain")
	}
}

// Verifies: REQ-KEY-031
func TestUseKeychainMovesKeyUnchanged(t *testing.T) {
	useMockKeychain(t)
	dir, _ := ConfigDir()
	enc, _ := Encrypt("s3cret")
	before, _ := os.ReadFile(filepath.Join(dir, keyFileName))
	// Warm cache with a marker notice to prove the reset.
	keyMu.Lock()
	keyNotice = "stale"
	keyMu.Unlock()
	if err := UseKeychain(); err != nil {
		t.Fatal(err)
	}
	got, err := keychainGetKey()
	if err != nil || !bytes.Equal(got, before) {
		t.Fatalf("keychain key differs: %v", err)
	}
	if fileExists(filepath.Join(dir, keyFileName)) || CurrentSecretStore() != SecretStoreKeychain {
		t.Error("file not removed or store not switched")
	}
	if k, n := cacheState(); k != nil || n != "" {
		t.Error("cache not reset")
	}
	if p, err := Decrypt(enc); err != nil || p != "s3cret" {
		t.Fatalf("data unreadable: %q %v", p, err)
	}
}

// Verifies: REQ-KEY-032
func TestUseKeychainRepairsMissingItem(t *testing.T) {
	useMockKeychain(t)
	dir, _ := ConfigDir()
	writeStoreFile(t, dir, "keychain\n")
	writeKeyFile(t, dir, key32(0x55)) // item missing, key file available
	if err := UseKeychain(); err != nil {
		t.Fatal(err)
	}
	if got, err := keychainGetKey(); err != nil || !bytes.Equal(got, key32(0x55)) {
		t.Fatalf("not re-uploaded: %v", err)
	}
	if fileExists(filepath.Join(dir, keyFileName)) || CurrentSecretStore() != SecretStoreKeychain {
		t.Error("final state wrong")
	}
	// Invalid (wrong length) item is replaced as well.
	useMockKeychain(t)
	dir, _ = ConfigDir()
	writeStoreFile(t, dir, "keychain\n")
	writeKeyFile(t, dir, key32(0x56))
	keyring.Set(keychainService, keychainAccount, "bad")
	if err := UseKeychain(); err != nil {
		t.Fatal(err)
	}
	if got, _ := keychainGetKey(); !bytes.Equal(got, key32(0x56)) {
		t.Error("invalid item not replaced")
	}
}

// Verifies: REQ-KEY-032
func TestUseKeychainLostItemCreatesNewKey(t *testing.T) {
	// Pins current behaviour (see "Derived requirements"): keychain store set,
	// item lost, no key file: a new key is generated and stored.
	useMockKeychain(t)
	dir, _ := ConfigDir()
	writeStoreFile(t, dir, "keychain\n")
	if err := UseKeychain(); err != nil {
		t.Fatal(err)
	}
	if k, err := keychainGetKey(); err != nil || len(k) != keySize {
		t.Fatalf("%v %v", k, err)
	}
	if !KeyWasReset() {
		t.Fatal("a lost key must be reported so the caller can remove the unreadable secrets")
	}
	// A later, ordinary switch clears the flag.
	if err := UseFile(); err != nil {
		t.Fatal(err)
	}
	if err := UseKeychain(); err != nil {
		t.Fatal(err)
	}
	if KeyWasReset() {
		t.Fatal("a normal move of an existing key is not a reset")
	}
}

// Verifies: REQ-KEY-044
func TestUseKeychainFromFileStoreIsNotAReset(t *testing.T) {
	useMockKeychain(t)
	if err := UseKeychain(); err != nil {
		t.Fatal(err)
	}
	if KeyWasReset() {
		t.Fatal("first move into the keychain must not look like a lost key")
	}
}

// Verifies: REQ-KEY-044
func TestUseKeychainWithFileKeyFallbackIsNotAReset(t *testing.T) {
	// Keychain is the configured store, its item is gone, but the key file is
	// still there: the existing key is moved, nothing is lost.
	useMockKeychain(t)
	dir, _ := ConfigDir()
	writeStoreFile(t, dir, "keychain\n")
	writeKeyFile(t, dir, key32(0x42))
	if err := UseKeychain(); err != nil {
		t.Fatal(err)
	}
	if KeyWasReset() {
		t.Fatal("the key file still held the key: not a reset")
	}
}

// Verifies: REQ-KEY-033
func TestUseKeychainRoundTripMCDC(t *testing.T) {
	run := func(name string, get func(string) (string, error, bool), wantErr string) {
		useMockKeychain(t)
		dir, _ := ConfigDir()
		writeKeyFile(t, dir, key32(0x61))
		var deleted bool
		stubKeyringGet(t, func(s, a string) (string, error, bool) {
			if a != keychainAccount {
				return "", nil, false
			}
			return get(a)
		})
		orig := keyringDelete
		stubKeyringDelete(t, func(s, a string) error { deleted = true; return orig(s, a) })
		err := UseKeychain()
		if wantErr == "" {
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if deleted {
				t.Errorf("%s: keychain item deleted on success", name)
			}
			return
		}
		if err == nil || !strings.Contains(err.Error(), "round-trip failed") {
			t.Fatalf("%s: %v", name, err)
		}
		if !deleted {
			t.Errorf("%s: keychain item not deleted", name)
		}
		if !fileExists(filepath.Join(dir, keyFileName)) || CurrentSecretStore() != SecretStoreFile {
			t.Errorf("%s: key file or store changed", name)
		}
	}
	same := base64.StdEncoding.EncodeToString(key32(0x61))
	other := base64.StdEncoding.EncodeToString(key32(0x62))
	// Baseline: both false -> continue.
	run("same", func(string) (string, error, bool) { return same, nil, true }, "")
	// A: read-back error.
	run("get error", func(string) (string, error, bool) { return "", errBoom, true }, "x")
	// B: read-back differs.
	run("differs", func(string) (string, error, bool) { return other, nil, true }, "x")
}

// Verifies: REQ-KEY-034
func TestUseKeychainEarlyFailures(t *testing.T) {
	// Key file invalid: loadOrCreateFileKey fails.
	useMockKeychain(t)
	dir, _ := ConfigDir()
	writeKeyFile(t, dir, []byte("short"))
	if err := UseKeychain(); err == nil {
		t.Error("expected key file error")
	}
	if _, err := keyring.Get(keychainService, keychainAccount); !errors.Is(err, keyring.ErrNotFound) {
		t.Error("keychain written despite failure")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, keyFileName)); string(got) != "short" || CurrentSecretStore() != SecretStoreFile {
		t.Error("state changed")
	}
	// Keychain write fails.
	useMockKeychain(t)
	dir, _ = ConfigDir()
	writeKeyFile(t, dir, key32(1))
	stubKeyringSet(t, func(string, string, string) error { return errBoom })
	if err := UseKeychain(); !errors.Is(err, errBoom) || !strings.Contains(err.Error(), "failed to store key in keychain") {
		t.Errorf("set failure: %v", err)
	}
	if !fileExists(filepath.Join(dir, keyFileName)) || CurrentSecretStore() != SecretStoreFile {
		t.Error("state changed")
	}
}

// Verifies: REQ-KEY-035
func TestUseKeychainStoreWriteFailureRollsBack(t *testing.T) {
	useMockKeychain(t)
	dir, _ := ConfigDir()
	writeKeyFile(t, dir, key32(1))
	stubWriteFile(t, errBoom)
	if err := UseKeychain(); !errors.Is(err, errBoom) {
		t.Fatalf("%v", err)
	}
	if _, err := keyring.Get(keychainService, keychainAccount); !errors.Is(err, keyring.ErrNotFound) {
		t.Error("keychain item not rolled back")
	}
	if !fileExists(filepath.Join(dir, keyFileName)) {
		t.Error("key file lost")
	}
}

// Verifies: REQ-KEY-036
func TestUseKeychainKeyFileRemovalFailure(t *testing.T) {
	useMockKeychain(t)
	dir, _ := ConfigDir()
	writeKeyFile(t, dir, key32(1))
	stubRemoveFile(t, errBoom)
	err := UseKeychain()
	if !errors.Is(err, errBoom) || !strings.Contains(err.Error(), "key stored in keychain") {
		t.Fatalf("%v", err)
	}
	if CurrentSecretStore() != SecretStoreKeychain {
		t.Error("store should already be keychain")
	}
}

// Verifies: REQ-KEY-037
func TestUseFileNoOpWhenFileStore(t *testing.T) {
	useMockKeychain(t)
	dir, _ := ConfigDir()
	stubKeyringGet(t, func(string, string) (string, error, bool) { t.Error("keychain consulted"); return "", nil, false })
	if err := UseFile(); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Error("no-op created files")
	}
}

// Verifies: REQ-KEY-038
func TestUseFileMovesKeyBackUnchanged(t *testing.T) {
	useMockKeychain(t)
	dir, _ := ConfigDir()
	enc, _ := Encrypt("p")
	orig, _ := os.ReadFile(filepath.Join(dir, keyFileName))
	if err := UseKeychain(); err != nil {
		t.Fatal(err)
	}
	if _, err := Decrypt(enc); err != nil { // populate cache
		t.Fatal(err)
	}
	if err := UseFile(); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, keyFileName))
	info, _ := os.Stat(filepath.Join(dir, keyFileName))
	if !bytes.Equal(got, orig) || info.Mode().Perm() != 0600 || CurrentSecretStore() != SecretStoreFile {
		t.Error("key file wrong")
	}
	if _, err := keyring.Get(keychainService, keychainAccount); !errors.Is(err, keyring.ErrNotFound) {
		t.Error("keychain item not deleted")
	}
	if k, _ := cacheState(); k != nil {
		t.Error("cache not reset")
	}
}

// Verifies: REQ-KEY-039, REQ-KEY-040
func TestUseFileKeychainUnreachable(t *testing.T) {
	setup := func() string {
		useMockKeychain(t)
		dir, _ := ConfigDir()
		writeStoreFile(t, dir, "keychain\n")
		keyring.MockInitWithError(errBoom)
		return dir
	}
	// Valid key file exists: switch back.
	dir := setup()
	writeKeyFile(t, dir, key32(0x70))
	if err := UseFile(); err == nil || !errors.Is(err, errBoom) {
		// keychain delete fails afterwards (unreachable), but the switch happened
		t.Logf("UseFile returned %v", err)
	}
	if CurrentSecretStore() != SecretStoreFile {
		t.Error("store not switched back")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, keyFileName)); !bytes.Equal(got, key32(0x70)) {
		t.Error("file key changed")
	}
	// No key file, and an invalid one.
	for _, prep := range []func(string){func(string) {}, func(d string) { writeKeyFile(t, d, []byte("short")) }} {
		dir = setup()
		prep(dir)
		if err := UseFile(); err == nil || !strings.Contains(err.Error(), "no key file exists") {
			t.Errorf("%v", err)
		}
		if CurrentSecretStore() != SecretStoreKeychain {
			t.Error("store changed")
		}
	}
}

// Verifies: REQ-KEY-041
func TestUseFileWriteFailures(t *testing.T) {
	// Key file cannot be written (non-empty directory at the key path).
	useMockKeychain(t)
	dir, _ := ConfigDir()
	if _, err := Encrypt("x"); err != nil {
		t.Fatal(err)
	}
	if err := UseKeychain(); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(dir, keyFileName, "sub"), 0700)
	if err := UseFile(); err == nil {
		t.Fatal("expected writeFileKey failure")
	}
	noTempFiles(t, dir)
	if CurrentSecretStore() != SecretStoreKeychain {
		t.Error("store changed")
	}
	if _, err := keychainGetKey(); err != nil {
		t.Error("keychain item lost")
	}

	// Store setting cannot be written.
	useMockKeychain(t)
	if _, err := Encrypt("x"); err != nil {
		t.Fatal(err)
	}
	if err := UseKeychain(); err != nil {
		t.Fatal(err)
	}
	stubWriteFile(t, errBoom)
	if err := UseFile(); !errors.Is(err, errBoom) {
		t.Fatalf("%v", err)
	}
	if _, err := keychainGetKey(); err != nil {
		t.Error("keychain item must be kept")
	}
	if CurrentSecretStore() != SecretStoreKeychain {
		t.Error("store changed")
	}
}

// Verifies: REQ-KEY-042
func TestUseFileKeychainDeleteFailure(t *testing.T) {
	useMockKeychain(t)
	if _, err := Encrypt("x"); err != nil {
		t.Fatal(err)
	}
	if err := UseKeychain(); err != nil {
		t.Fatal(err)
	}
	stubKeyringDelete(t, func(string, string) error { return errBoom })
	err := UseFile()
	if !errors.Is(err, errBoom) || !strings.Contains(err.Error(), "key file written") {
		t.Fatalf("%v", err)
	}
	if CurrentSecretStore() != SecretStoreFile {
		t.Error("store should be file")
	}
}

// Verifies: REQ-KEY-043
func TestMasterKeyConcurrent(t *testing.T) {
	dir := useTempKeyDir(t)
	const n = 16
	keys := make([][]byte, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			keys[i], errs[i] = masterKey()
		}(i)
	}
	wg.Wait()
	for i := range keys {
		if errs[i] != nil || !bytes.Equal(keys[i], keys[0]) {
			t.Fatalf("goroutine %d: %v", i, errs[i])
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("expected exactly one key file, got %d entries", len(entries))
	}
}
