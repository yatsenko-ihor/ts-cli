package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/yatsenko-ihor/ts-cli/util"
)

// Requirements-based tests for config.go (see verification/requirements/config.md).

// ─── helpers ─────────────────────────────────────────────────────────────────

func cfgPath(dir string) string { return filepath.Join(dir, fileName) }

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func noTempFiles(t *testing.T, dir string) {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(dir, "*.tmp"))
	if len(m) != 0 {
		t.Fatalf("temp files left behind: %v", m)
	}
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode().Perm()
}

// seed saves a config with one account and returns the bytes on disk.
func seed(t *testing.T) []byte {
	t.Helper()
	dir, _ := util.ConfigDir()
	cfg := &Config{}
	cfg.AddAccount("orig", "tskey-api-ORIGINAL", "orig.com", true)
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	return mustRead(t, cfgPath(dir))
}

// swap replaces a seam for the duration of the test.
func swapEncrypt(t *testing.T, f func(string) (string, error)) {
	t.Helper()
	old := encryptSecret
	encryptSecret = f
	t.Cleanup(func() { encryptSecret = old })
}

func swapDecrypt(t *testing.T, f func(string) (string, error)) {
	t.Helper()
	old := decryptSecret
	decryptSecret = f
	t.Cleanup(func() { decryptSecret = old })
}

func swapMarshal(t *testing.T, f func(any, string, string) ([]byte, error)) {
	t.Helper()
	old := marshalIndent
	marshalIndent = f
	t.Cleanup(func() { marshalIndent = old })
}

// failFile wraps a real temp file and fails selected operations.
type failFile struct {
	*os.File
	chmod, write, closeErr bool
}

var errInjected = errors.New("injected failure")

func (f *failFile) Chmod(m os.FileMode) error {
	if f.chmod {
		return errInjected
	}
	return f.File.Chmod(m)
}

func (f *failFile) Write(b []byte) (int, error) {
	if f.write {
		return 0, errInjected
	}
	return f.File.Write(b)
}

func (f *failFile) Close() error {
	err := f.File.Close()
	if f.closeErr {
		return errInjected
	}
	return err
}

func swapCreateTemp(t *testing.T, f func(dir, pattern string) (tmpFile, error)) {
	t.Helper()
	old := createTemp
	createTemp = f
	t.Cleanup(func() { createTemp = old })
}

func failingTemp(t *testing.T, chmod, write, closeErr bool) {
	t.Helper()
	swapCreateTemp(t, func(dir, pattern string) (tmpFile, error) {
		f, err := os.CreateTemp(dir, pattern)
		if err != nil {
			return nil, err
		}
		return &failFile{File: f, chmod: chmod, write: write, closeErr: closeErr}, nil
	})
}

// dirIsAFile points the config dir at a regular file so MkdirAll fails.
func dirIsAFile(t *testing.T) {
	t.Helper()
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(util.ConfigDirEnv, blocker)
}

// noConfigDir makes util.ConfigDir fail (no override, no home directory).
func noConfigDir(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("home directory lookup failure is provoked via $HOME on linux")
	}
	t.Setenv(util.ConfigDirEnv, "")
	t.Setenv("HOME", "")
}

// ─── Path / directory ────────────────────────────────────────────────────────

// Verifies: REQ-CFG-001
func TestPathCreatesPrivateDirectoryUnderOverride(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "nested", "cfgdir") // does not exist yet
	t.Setenv(util.ConfigDirEnv, dir)

	p, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if p != filepath.Join(dir, "config.json") {
		t.Fatalf("Path = %q", p)
	}
	if m := fileMode(t, dir); m != 0700 {
		t.Fatalf("dir mode = %o, want 0700", m)
	}
	// Calling again with the directory present is not an error (boundary).
	if _, err := Path(); err != nil {
		t.Fatal(err)
	}
	// Path alone must not create the file.
	if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Path must not create config.json: %v", err)
	}
}

// Verifies: REQ-CFG-002
func TestPathErrorsPropagate(t *testing.T) {
	for name, setup := range map[string]func(*testing.T){
		"dir is a regular file": dirIsAFile,
		"no home directory":     noConfigDir,
	} {
		t.Run(name, func(t *testing.T) {
			setup(t)
			if _, err := Path(); err == nil {
				t.Error("Path: expected error")
			}
			if _, err := Load(); err == nil {
				t.Error("Load: expected error")
			}
			if err := Save(&Config{}); err == nil {
				t.Error("Save: expected error")
			}
			called := false
			if err := Update(func(*Config) error { called = true; return nil }); err == nil || called {
				t.Errorf("Update: err=%v, fn called=%v", err, called)
			}
		})
	}
}

// ─── Load ────────────────────────────────────────────────────────────────────

// Verifies: REQ-CFG-003
func TestLoadMissingFileYieldsEmptyConfigWithoutCreatingIt(t *testing.T) {
	dir := useTempConfigDir(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Accounts == nil || len(cfg.Accounts) != 0 || cfg.ConfigVersion != CurrentVersion {
		t.Fatalf("empty config wrong: %+v", cfg)
	}
	if _, err := os.Stat(cfgPath(dir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Load must not create the file: %v", err)
	}
}

// Verifies: REQ-CFG-004
func TestLoadFillsDefaults(t *testing.T) {
	cases := map[string]struct {
		json        string
		wantVersion string
	}{
		"accounts null, version empty": {`{"accounts":null}`, CurrentVersion},
		"accounts absent":              {`{}`, CurrentVersion},
		"explicit version kept":        {`{"accounts":[],"config_version":"0.9"}`, "0.9"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := useTempConfigDir(t)
			if err := os.WriteFile(cfgPath(dir), []byte(tc.json), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Accounts == nil || cfg.ConfigVersion != tc.wantVersion {
				t.Fatalf("cfg = %+v", cfg)
			}
		})
	}
}

// Verifies: REQ-CFG-005
func TestLoadRejectsCorruptFileAndNeverReplacesIt(t *testing.T) {
	for name, content := range map[string]string{
		"not json":        "{not json",
		"empty file":      "",
		"truncated":       `{"accounts":[{"name":"a"`,
		"wrong type":      `{"accounts":"x"}`,
		"binary garbage":  "\x00\x01\x02\xff",
		"array at top":    `[]`,
		"valid then junk": `{"accounts":[]} trailing`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := useTempConfigDir(t)
			if err := os.WriteFile(cfgPath(dir), []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), "failed to parse config file") || !strings.Contains(err.Error(), cfgPath(dir)) {
				t.Fatalf("err = %v", err)
			}
			// Update must refuse too and must not write.
			if err := Update(func(*Config) error { return nil }); err == nil {
				t.Fatal("Update on corrupt file must fail")
			}
			if got := mustRead(t, cfgPath(dir)); string(got) != content {
				t.Fatalf("corrupt file was modified: %q", got)
			}
			noTempFiles(t, dir)
		})
	}
}

// Verifies: REQ-CFG-006
func TestLoadReportsUnreadableFile(t *testing.T) {
	dir := useTempConfigDir(t)
	// config.json is a directory: ReadFile fails with something other than "not exist".
	if err := os.Mkdir(cfgPath(dir), 0700); err != nil {
		t.Fatal(err)
	}
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "failed to read config file") {
		t.Fatalf("err = %v", err)
	}
	if fi, err := os.Stat(cfgPath(dir)); err != nil || !fi.IsDir() {
		t.Fatal("the unreadable entry must be left alone")
	}
}

// Verifies: REQ-CFG-007, REQ-CFG-011
func TestLoadDecryptsAPIKeys(t *testing.T) {
	dir := useTempConfigDir(t)
	cfg := &Config{}
	cfg.AddAccount("a", "tskey-api-AAA-secretA", "a.com", true)
	cfg.AddAccount("b", "tskey-api-BBB-secretB", "b.com", false)
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	// Save must not have changed the caller's in-memory cleartext keys.
	if cfg.Accounts[0].APIKey != "tskey-api-AAA-secretA" || cfg.Accounts[0].APIKeyEncrypted != "" {
		t.Fatalf("Save mutated its argument: %+v", cfg.Accounts[0])
	}
	raw := mustRead(t, cfgPath(dir))
	for _, secret := range []string{"secretA", "secretB", "tskey-api-AAA", "tskey-api-BBB"} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatalf("plaintext %q found on disk:\n%s", secret, raw)
		}
	}
	if bytes.Contains(raw, []byte(`"api_key"`)) || bytes.Count(raw, []byte(`"api_key_enc"`)) != 2 {
		t.Fatalf("expected exactly two api_key_enc and no api_key:\n%s", raw)
	}
	back, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if back.Accounts[0].APIKey != "tskey-api-AAA-secretA" || back.Accounts[1].APIKey != "tskey-api-BBB-secretB" {
		t.Fatalf("keys not decrypted: %+v", back.Accounts)
	}
	// Loading an already encrypted file must not rewrite it.
	if after := mustRead(t, cfgPath(dir)); !bytes.Equal(after, raw) {
		t.Fatal("Load rewrote an up-to-date file")
	}
}

// Verifies: REQ-CFG-008
func TestLoadFailsWhenAPIKeyCannotBeDecrypted(t *testing.T) {
	cases := map[string]string{
		"not base64":      "!!!not-base64!!!",
		"too short":       "AAAA",
		"wrong key/GCM":   "QUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQQ==",
		"valid b64 empty": "",
	}
	for name, enc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := useTempConfigDir(t)
			// Make sure the master key exists first so failure is about the ciphertext.
			if _, err := util.Encrypt("x"); err != nil {
				t.Fatal(err)
			}
			content := fmt.Sprintf(`{"accounts":[{"name":"acme","api_key_enc":%q,"tailnet":"t.com","active":true}]}`, enc)
			if enc == "" {
				// An empty ciphertext is "no encrypted key": no decrypt attempted, no error.
				content = `{"accounts":[{"name":"acme","api_key_enc":"","tailnet":"t.com","active":true}]}`
			}
			if err := os.WriteFile(cfgPath(dir), []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load()
			if enc == "" {
				if err != nil || cfg.Accounts[0].APIKey != "" {
					t.Fatalf("empty ciphertext: cfg=%+v err=%v", cfg, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), `"acme"`) || !strings.Contains(err.Error(), "failed to decrypt API key") {
				t.Fatalf("err = %v", err)
			}
			if cfg != nil {
				t.Fatal("no partial config may be returned")
			}
			if got := mustRead(t, cfgPath(dir)); string(got) != content {
				t.Fatal("file modified after decrypt failure")
			}
		})
	}
}

// Verifies: REQ-CFG-009
func TestLegacyCleartextKeysAreReencryptedOnLoad(t *testing.T) {
	dir := useTempConfigDir(t)
	legacy := `{"accounts":[` +
		`{"name":"old1","api_key":"tskey-api-LEGACY1-zzz","tailnet":"o1.com","active":true},` +
		`{"name":"old2","api_key":"tskey-api-LEGACY2-zzz","tailnet":"o2.com"}],"config_version":"1.0"}`
	if err := os.WriteFile(cfgPath(dir), []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Accounts[0].APIKey != "tskey-api-LEGACY1-zzz" || cfg.Accounts[1].APIKey != "tskey-api-LEGACY2-zzz" {
		t.Fatalf("keys unreadable: %+v", cfg.Accounts)
	}
	raw := mustRead(t, cfgPath(dir))
	if bytes.Contains(raw, []byte("LEGACY")) || bytes.Contains(raw, []byte(`"api_key"`)) {
		t.Fatalf("cleartext still on disk:\n%s", raw)
	}
	if m := fileMode(t, cfgPath(dir)); m != 0600 {
		t.Fatalf("rewritten file mode = %o", m)
	}
	noTempFiles(t, dir)
	// A second load returns the same keys from the encrypted file.
	again, err := Load()
	if err != nil || again.Accounts[1].APIKey != "tskey-api-LEGACY2-zzz" {
		t.Fatalf("second load: %+v %v", again, err)
	}
}

// Verifies: REQ-CFG-010
func TestLegacyRewriteFailureDoesNotBlockLoad(t *testing.T) {
	dir := useTempConfigDir(t)
	legacy := `{"accounts":[{"name":"old","api_key":"tskey-api-LEGACY","tailnet":"o.com","active":true}]}`
	if err := os.WriteFile(cfgPath(dir), []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	swapEncrypt(t, func(string) (string, error) { return "", errInjected })
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load must succeed: %v", err)
	}
	if cfg.Accounts[0].APIKey != "tskey-api-LEGACY" {
		t.Fatalf("cfg = %+v", cfg.Accounts[0])
	}
	if got := mustRead(t, cfgPath(dir)); string(got) != legacy {
		t.Fatal("file must be untouched when the rewrite fails")
	}
	noTempFiles(t, dir)
}

// Verifies: REQ-CFG-010
func TestEncryptedKeyWinsOverCleartextAndDoesNotTriggerRewrite(t *testing.T) {
	dir := useTempConfigDir(t)
	enc, err := util.Encrypt("tskey-api-REAL")
	if err != nil {
		t.Fatal(err)
	}
	content := fmt.Sprintf(`{"accounts":[{"name":"a","api_key":"tskey-api-STALE","api_key_enc":%q,"tailnet":"a.com","active":true}]}`, enc)
	if err := os.WriteFile(cfgPath(dir), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Accounts[0].APIKey != "tskey-api-REAL" {
		t.Fatalf("APIKey = %q", cfg.Accounts[0].APIKey)
	}
	if got := mustRead(t, cfgPath(dir)); string(got) != content {
		t.Fatal("file must not be rewritten when every account has an encrypted key")
	}
}

// ─── Save ────────────────────────────────────────────────────────────────────

// Verifies: REQ-CFG-012
func TestSaveFileModeIs0600EvenWhenReplacingLooserFile(t *testing.T) {
	dir := useTempConfigDir(t)
	if err := os.WriteFile(cfgPath(dir), []byte(`{"accounts":[]}`), 0666); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(cfgPath(dir), 0666); err != nil {
		t.Fatal(err)
	}
	if err := Save(&Config{}); err != nil {
		t.Fatal(err)
	}
	if m := fileMode(t, cfgPath(dir)); m != 0600 {
		t.Fatalf("mode = %o, want 0600", m)
	}
	// Fresh directory created by Save is 0700.
	fresh := filepath.Join(t.TempDir(), "new")
	t.Setenv(util.ConfigDirEnv, fresh)
	if err := Save(&Config{}); err != nil {
		t.Fatal(err)
	}
	if m := fileMode(t, fresh); m != 0700 {
		t.Fatalf("dir mode = %o, want 0700", m)
	}
	if m := fileMode(t, cfgPath(fresh)); m != 0600 {
		t.Fatalf("file mode = %o", m)
	}
}

// Verifies: REQ-CFG-013
func TestSaveLeavesNoTempFileOnSuccess(t *testing.T) {
	dir := useTempConfigDir(t)
	for i := 0; i < 3; i++ {
		if err := Save(&Config{SSHUsername: fmt.Sprint("u", i)}); err != nil {
			t.Fatal(err)
		}
		noTempFiles(t, dir)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("temp file %s", e.Name())
		}
	}
}

// Verifies: REQ-CFG-013
func TestSaveFailureAtEveryStageKeepsOriginalAndLeavesNoTemp(t *testing.T) {
	stages := map[string]struct {
		setup   func(*testing.T)
		wantErr string
	}{
		"create temp": {func(t *testing.T) {
			swapCreateTemp(t, func(string, string) (tmpFile, error) { return nil, errInjected })
		}, "failed to create temp config"},
		"chmod": {func(t *testing.T) { failingTemp(t, true, false, false) }, "failed to set config permissions"},
		"write": {func(t *testing.T) { failingTemp(t, false, true, false) }, "failed to write config"},
		"close": {func(t *testing.T) { failingTemp(t, false, false, true) }, "failed to write config"},
	}
	for name, st := range stages {
		t.Run(name, func(t *testing.T) {
			dir := useTempConfigDir(t)
			before := seed(t)
			st.setup(t)
			cfg := &Config{SSHUsername: "changed"}
			cfg.AddAccount("new", "tskey-api-NEWSECRET", "new.com", true)
			err := Save(cfg)
			if err == nil || !strings.Contains(err.Error(), st.wantErr) {
				t.Fatalf("err = %v, want %q", err, st.wantErr)
			}
			if got := mustRead(t, cfgPath(dir)); !bytes.Equal(got, before) {
				t.Fatal("original file changed by a failed save")
			}
			noTempFiles(t, dir)
		})
	}

	t.Run("rename onto non-empty directory", func(t *testing.T) {
		dir := useTempConfigDir(t)
		target := cfgPath(dir)
		if err := os.MkdirAll(filepath.Join(target, "child"), 0700); err != nil {
			t.Fatal(err)
		}
		err := Save(&Config{})
		if err == nil || !strings.Contains(err.Error(), "failed to replace config") {
			t.Fatalf("err = %v", err)
		}
		if _, err := os.Stat(filepath.Join(target, "child")); err != nil {
			t.Fatal("existing target damaged")
		}
		noTempFiles(t, dir)
	})
}

// Verifies: REQ-CFG-014
func TestSaveEncryptOrMarshalFailureKeepsOriginal(t *testing.T) {
	t.Run("encrypt failure names the account but not the key", func(t *testing.T) {
		dir := useTempConfigDir(t)
		before := seed(t)
		swapEncrypt(t, func(string) (string, error) { return "", errInjected })
		cfg := &Config{}
		cfg.AddAccount("victim", "tskey-api-TOPSECRET", "v.com", true)
		err := Save(cfg)
		if err == nil || !strings.Contains(err.Error(), `"victim"`) || strings.Contains(err.Error(), "TOPSECRET") {
			t.Fatalf("err = %v", err)
		}
		if !bytes.Equal(mustRead(t, cfgPath(dir)), before) {
			t.Fatal("original changed")
		}
		noTempFiles(t, dir)
	})
	t.Run("marshal failure", func(t *testing.T) {
		dir := useTempConfigDir(t)
		before := seed(t)
		swapMarshal(t, func(any, string, string) ([]byte, error) { return nil, errInjected })
		err := Save(&Config{})
		if err == nil || !strings.Contains(err.Error(), "failed to marshal config") {
			t.Fatalf("err = %v", err)
		}
		if !bytes.Equal(mustRead(t, cfgPath(dir)), before) {
			t.Fatal("original changed")
		}
		noTempFiles(t, dir)
	})
}

// Verifies: REQ-CFG-015
func TestSaveDefaultsEmptyVersion(t *testing.T) {
	dir := useTempConfigDir(t)
	cfg := &Config{}
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.ConfigVersion != CurrentVersion || !bytes.Contains(mustRead(t, cfgPath(dir)), []byte(`"config_version": "1.0"`)) {
		t.Fatalf("version not defaulted: %q", cfg.ConfigVersion)
	}
	if err := Save(&Config{ConfigVersion: "9.9"}); err != nil {
		t.Fatal(err)
	}
	back, _ := Load()
	if back.ConfigVersion != "9.9" {
		t.Fatalf("explicit version replaced: %q", back.ConfigVersion)
	}
}

// Verifies: REQ-CFG-016
func TestRoundTripPreservesEveryField(t *testing.T) {
	useTempConfigDir(t)
	in := &Config{
		SSHUsername:        "admin",
		MachineCredentials: map[string]MachineAuth{"id1": {Name: "laptop", Username: "root", PasswordEncrypted: "CIPHERTEXT=="}},
		Commands:           []SavedCommand{{Name: "up", Command: "uptime"}, {Name: "df", Command: "df -h {{p}}"}},
		UI:                 map[string]string{"theme": "dark"},
		LastMachine:        "id1",
	}
	in.AddAccount("a", "tskey-api-A-1", "a.com", true)
	in.AddAccount("b", "tskey-api-B-2", "b.com", false)
	in.SetAccountHidden("b", true)
	if err := Save(in); err != nil {
		t.Fatal(err)
	}
	out, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if out.SSHUsername != "admin" || out.LastMachine != "id1" || out.UI["theme"] != "dark" ||
		out.MachineCredentials["id1"] != in.MachineCredentials["id1"] ||
		len(out.Commands) != 2 || out.Commands[1] != in.Commands[1] ||
		len(out.Accounts) != 2 || !out.Accounts[0].Active || out.Accounts[1].Active || !out.Accounts[1].Hidden ||
		out.Accounts[1].APIKey != "tskey-api-B-2" || out.Accounts[1].Tailnet != "b.com" {
		t.Fatalf("round trip lost data: %+v", out)
	}
	// The credential ciphertext is stored verbatim (the caller encrypted it).
	if out.MachineCredentials["id1"].PasswordEncrypted != "CIPHERTEXT==" {
		t.Fatal("password ciphertext altered")
	}
}

// Verifies: REQ-CFG-016
func TestSavePreservesAlreadyEncryptedKeyWithoutCleartext(t *testing.T) {
	// forDisk boundary: an account that carries only api_key_enc keeps it.
	dir := useTempConfigDir(t)
	enc, err := util.Encrypt("tskey-api-ONLYENC")
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(&Config{Accounts: []Account{{Name: "e", APIKeyEncrypted: enc, Tailnet: "e.com", Active: true}}}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(mustRead(t, cfgPath(dir)), []byte(enc)) {
		t.Fatal("encrypted key dropped")
	}
	back, err := Load()
	if err != nil || back.Accounts[0].APIKey != "tskey-api-ONLYENC" {
		t.Fatalf("%+v %v", back, err)
	}
	// An account with no key at all stays keyless.
	if err := Save(&Config{Accounts: []Account{{Name: "none", Tailnet: "n.com"}}}); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(mustRead(t, cfgPath(dir)), []byte("api_key")) {
		t.Fatal("empty key must be omitted")
	}
}

// ─── Update ──────────────────────────────────────────────────────────────────

// Verifies: REQ-CFG-017
func TestUpdateIsReadModifyWriteOfCurrentDiskState(t *testing.T) {
	useTempConfigDir(t)
	seed(t)
	// Another writer changes the file between two Updates; the second must see it.
	if err := Update(func(c *Config) error { c.SSHUsername = "first"; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := Update(func(c *Config) error {
		if c.SSHUsername != "first" || len(c.Accounts) != 1 || c.Accounts[0].APIKey != "tskey-api-ORIGINAL" {
			t.Errorf("fn saw stale state: %+v", c)
		}
		c.LastMachine = "m"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	cfg, _ := Load()
	if cfg.SSHUsername != "first" || cfg.LastMachine != "m" || len(cfg.Accounts) != 1 {
		t.Fatalf("lost data: %+v", cfg)
	}
}

// Verifies: REQ-CFG-018
func TestUpdateDoesNotWriteWhenCallbackFailsOrLoadFails(t *testing.T) {
	t.Run("callback error", func(t *testing.T) {
		dir := useTempConfigDir(t)
		before := seed(t)
		want := errors.New("boom")
		err := Update(func(c *Config) error { c.SSHUsername = "dirty"; return want })
		if !errors.Is(err, want) {
			t.Fatalf("err = %v", err)
		}
		if !bytes.Equal(mustRead(t, cfgPath(dir)), before) {
			t.Fatal("file changed despite callback error")
		}
	})
	t.Run("save error is returned", func(t *testing.T) {
		dir := useTempConfigDir(t)
		before := seed(t)
		swapEncrypt(t, func(string) (string, error) { return "", errInjected })
		err := Update(func(c *Config) error { c.AddAccount("x", "tskey-api-x", "x.com", false); return nil })
		if err == nil {
			t.Fatal("expected save error")
		}
		if !bytes.Equal(mustRead(t, cfgPath(dir)), before) {
			t.Fatal("file changed")
		}
	})
	t.Run("load error: callback not called", func(t *testing.T) {
		dir := useTempConfigDir(t)
		if err := os.WriteFile(cfgPath(dir), []byte("garbage"), 0600); err != nil {
			t.Fatal(err)
		}
		called := false
		if err := Update(func(*Config) error { called = true; return nil }); err == nil || called {
			t.Fatalf("err=%v called=%v", err, called)
		}
	})
}

// Verifies: REQ-CFG-019
func TestConcurrentUpdatesLoseNothing(t *testing.T) {
	useTempConfigDir(t)
	const n = 24
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- Update(func(c *Config) error {
				c.SetMachineCredential(fmt.Sprint("m", i), MachineAuth{Username: fmt.Sprint("u", i)})
				return nil
			})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.MachineCredentials) != n {
		t.Fatalf("lost updates: %d of %d survived", len(cfg.MachineCredentials), n)
	}
}

// ─── Accounts ────────────────────────────────────────────────────────────────

func threeAccounts() *Config {
	return &Config{Accounts: []Account{
		{Name: "personal", APIKey: "k1", Tailnet: "personal.com", Active: true},
		{Name: "work", APIKey: "k2", Tailnet: "work.com"},
		{Name: "test", APIKey: "k3", Tailnet: "test.com"},
	}}
}

// Verifies: REQ-CFG-020
func TestFindAccount(t *testing.T) {
	cfg := threeAccounts()
	cases := map[string]string{ // query -> expected account name ("" = nil)
		"work":         "work", // match by Name only
		"test.com":     "test", // match by Tailnet only
		"personal":     "personal",
		"nothing":      "", // neither
		"":             "", // boundary: empty query matches nothing here
		"WORK":         "", // case-sensitive
		"work.com/..":  "", // hostile
		"personal.com": "personal",
	}
	for q, want := range cases {
		t.Run("query="+q, func(t *testing.T) {
			got := cfg.FindAccount(q)
			switch {
			case want == "" && got != nil:
				t.Fatalf("want nil, got %+v", got)
			case want != "" && (got == nil || got.Name != want):
				t.Fatalf("want %s, got %+v", want, got)
			}
		})
	}
	if (&Config{}).FindAccount("x") != nil {
		t.Fatal("empty config")
	}
	// The returned pointer refers to the stored account (mutations stick).
	cfg.FindAccount("work").APIKey = "changed"
	if cfg.Accounts[1].APIKey != "changed" {
		t.Fatal("FindAccount must return a pointer into Accounts")
	}
}

// Verifies: REQ-CFG-021, REQ-CFG-023
func TestAddAccountNew(t *testing.T) {
	t.Run("appends inactive when another is active", func(t *testing.T) {
		cfg := threeAccounts()
		cfg.AddAccount("n", "kn", "n.com", false)
		if len(cfg.Accounts) != 4 || cfg.Accounts[3].Active || !cfg.Accounts[0].Active {
			t.Fatalf("%+v", cfg.Accounts)
		}
	})
	t.Run("setActive steals the active flag", func(t *testing.T) {
		cfg := threeAccounts()
		cfg.AddAccount("n", "kn", "n.com", true)
		if cfg.GetActiveAccount().Name != "n" || countActive(cfg) != 1 {
			t.Fatalf("%+v", cfg.Accounts)
		}
	})
	t.Run("first account becomes active even when setActive=false", func(t *testing.T) {
		cfg := &Config{}
		cfg.AddAccount("first", "k", "f.com", false)
		if !cfg.Accounts[0].Active {
			t.Fatal("invariant: exactly one active account")
		}
	})
	t.Run("no active among existing: new one becomes active", func(t *testing.T) {
		cfg := &Config{Accounts: []Account{{Name: "x", Tailnet: "x.com"}}}
		cfg.AddAccount("y", "k", "y.com", false)
		if countActive(cfg) != 1 || !cfg.Accounts[1].Active {
			t.Fatalf("%+v", cfg.Accounts)
		}
	})
	t.Run("setActive true on empty config", func(t *testing.T) {
		cfg := &Config{}
		cfg.AddAccount("first", "k", "f.com", true)
		if countActive(cfg) != 1 {
			t.Fatal("exactly one active")
		}
	})
}

func countActive(c *Config) int {
	n := 0
	for _, a := range c.Accounts {
		if a.Active {
			n++
		}
	}
	return n
}

// Verifies: REQ-CFG-022
func TestAddAccountExistingTailnetUpdatesInPlace(t *testing.T) {
	t.Run("setActive false keeps flags", func(t *testing.T) {
		cfg := threeAccounts()
		cfg.AddAccount("renamed", "newkey", "work.com", false)
		if len(cfg.Accounts) != 3 || cfg.Accounts[1].Name != "renamed" || cfg.Accounts[1].APIKey != "newkey" {
			t.Fatalf("%+v", cfg.Accounts)
		}
		if !cfg.Accounts[0].Active || cfg.Accounts[1].Active {
			t.Fatal("active flags must be unchanged")
		}
	})
	t.Run("setActive true moves active", func(t *testing.T) {
		cfg := threeAccounts()
		cfg.AddAccount("renamed", "newkey", "work.com", true)
		if len(cfg.Accounts) != 3 || !cfg.Accounts[1].Active || countActive(cfg) != 1 {
			t.Fatalf("%+v", cfg.Accounts)
		}
	})
	t.Run("hidden flag survives update", func(t *testing.T) {
		cfg := threeAccounts()
		cfg.Accounts[1].Hidden = true
		cfg.AddAccount("w2", "k", "work.com", false)
		if !cfg.Accounts[1].Hidden {
			t.Fatal("hidden lost")
		}
	})
	t.Run("matching is by tailnet, not by name", func(t *testing.T) {
		cfg := threeAccounts()
		cfg.AddAccount("work", "k", "other.com", false) // same name, new tailnet
		if len(cfg.Accounts) != 4 {
			t.Fatalf("must append: %+v", cfg.Accounts)
		}
	})
}

// Verifies: REQ-CFG-024, REQ-CFG-025
func TestRemoveAccount(t *testing.T) {
	t.Run("by name", func(t *testing.T) {
		cfg := threeAccounts()
		if !cfg.RemoveAccount("work") || len(cfg.Accounts) != 2 || cfg.FindAccount("work") != nil {
			t.Fatalf("%+v", cfg.Accounts)
		}
	})
	t.Run("by tailnet", func(t *testing.T) {
		cfg := threeAccounts()
		if !cfg.RemoveAccount("test.com") || len(cfg.Accounts) != 2 {
			t.Fatalf("%+v", cfg.Accounts)
		}
	})
	t.Run("unknown leaves everything", func(t *testing.T) {
		cfg := threeAccounts()
		if cfg.RemoveAccount("zzz") || len(cfg.Accounts) != 3 || !cfg.Accounts[0].Active {
			t.Fatalf("%+v", cfg.Accounts)
		}
		if cfg.RemoveAccount("") {
			t.Fatal("empty name must match nothing")
		}
	})
	t.Run("removing inactive keeps the active account", func(t *testing.T) {
		cfg := threeAccounts()
		cfg.RemoveAccount("test")
		if cfg.GetActiveAccount().Name != "personal" || countActive(cfg) != 1 {
			t.Fatalf("%+v", cfg.Accounts)
		}
	})
	t.Run("removing active promotes first remaining", func(t *testing.T) {
		cfg := threeAccounts()
		cfg.RemoveAccount("personal")
		if cfg.Accounts[0].Name != "work" || !cfg.Accounts[0].Active || countActive(cfg) != 1 {
			t.Fatalf("%+v", cfg.Accounts)
		}
	})
	t.Run("removing the last account leaves an empty list", func(t *testing.T) {
		cfg := &Config{Accounts: []Account{{Name: "only", Tailnet: "o.com", Active: true}}}
		if !cfg.RemoveAccount("only") || len(cfg.Accounts) != 0 || cfg.GetActiveAccount() != nil {
			t.Fatalf("%+v", cfg.Accounts)
		}
	})
}

// Verifies: REQ-CFG-026
func TestAccountGetters(t *testing.T) {
	cfg := threeAccounts()
	if got := cfg.GetActiveAccount(); got == nil || got.Name != "personal" {
		t.Fatalf("active = %+v", got)
	}
	if (&Config{}).GetActiveAccount() != nil {
		t.Fatal("no accounts, no active")
	}
	none := &Config{Accounts: []Account{{Name: "a"}}}
	if none.GetActiveAccount() != nil {
		t.Fatal("no flag set, no active")
	}
	if got := cfg.GetAccountByTailnet("work.com"); got == nil || got.Name != "work" {
		t.Fatalf("by tailnet = %+v", got)
	}
	if cfg.GetAccountByTailnet("work") != nil {
		t.Fatal("name must not match GetAccountByTailnet")
	}
	if cfg.GetAccountByTailnet("") != nil {
		t.Fatal("empty tailnet")
	}
	if got := cfg.GetAllAccounts(); len(got) != 3 || got[2].Name != "test" {
		t.Fatalf("all = %+v", got)
	}
	if got := (&Config{}).GetAllAccounts(); len(got) != 0 {
		t.Fatal("empty")
	}
}

// Verifies: REQ-CFG-027
func TestSetActiveAccountKeepsExactlyOneActive(t *testing.T) {
	cfg := threeAccounts()
	for _, target := range []string{"work", "test.com", "personal", "work.com"} {
		if !cfg.SetActiveAccount(target) || countActive(cfg) != 1 || cfg.GetActiveAccount().Name != cfg.FindAccount(target).Name {
			t.Fatalf("target %s: %+v", target, cfg.Accounts)
		}
	}
	before := fmt.Sprint(cfg.Accounts)
	for _, bad := range []string{"nope", "", "WORK"} {
		if cfg.SetActiveAccount(bad) || fmt.Sprint(cfg.Accounts) != before {
			t.Fatalf("%q must change nothing", bad)
		}
	}
	// Even a (corrupt) config with two active flags is repaired.
	cfg.Accounts[1].Active, cfg.Accounts[2].Active = true, true
	cfg.SetActiveAccount("test")
	if countActive(cfg) != 1 || !cfg.Accounts[2].Active {
		t.Fatalf("%+v", cfg.Accounts)
	}
}

// Verifies: REQ-CFG-028
func TestSetAccountHidden(t *testing.T) {
	cfg := threeAccounts()
	if !cfg.SetAccountHidden("work", true) || !cfg.Accounts[1].Hidden {
		t.Fatal("hide by name")
	}
	if !cfg.SetAccountHidden("work.com", false) || cfg.Accounts[1].Hidden {
		t.Fatal("unhide by tailnet")
	}
	if cfg.SetAccountHidden("nope", true) {
		t.Fatal("unknown account")
	}
	for _, a := range cfg.Accounts {
		if a.Hidden {
			t.Fatalf("unexpected hidden: %+v", a)
		}
	}
	if !cfg.Accounts[0].Active {
		t.Fatal("hiding must not change the active flag")
	}
}

// Verifies: REQ-CFG-029
func TestUpdateAccount(t *testing.T) {
	// base: a(a.com) b(b.com), "self" case uses account a.
	mk := func() *Config {
		return &Config{Accounts: []Account{
			{Name: "a", Tailnet: "a.com", APIKey: "k1", Active: true},
			{Name: "b", Tailnet: "b.com", APIKey: "k2"},
		}}
	}
	t.Run("unknown account", func(t *testing.T) {
		cfg := mk()
		err := cfg.UpdateAccount("zzz", "x", "k")
		if err == nil || !strings.Contains(err.Error(), "not found") || cfg.Accounts[0].Name != "a" {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("rename and key", func(t *testing.T) {
		cfg := mk()
		if err := cfg.UpdateAccount("a.com", "work", "k3"); err != nil || cfg.Accounts[0].Name != "work" || cfg.Accounts[0].APIKey != "k3" {
			t.Fatalf("%v %+v", err, cfg.Accounts[0])
		}
	})
	t.Run("empty args change nothing", func(t *testing.T) {
		cfg := mk()
		if err := cfg.UpdateAccount("a", "", ""); err != nil || cfg.Accounts[0].Name != "a" || cfg.Accounts[0].APIKey != "k1" {
			t.Fatalf("%v %+v", err, cfg.Accounts[0])
		}
	})
	t.Run("key only", func(t *testing.T) {
		cfg := mk()
		if err := cfg.UpdateAccount("a", "", "k9"); err != nil || cfg.Accounts[0].Name != "a" || cfg.Accounts[0].APIKey != "k9" {
			t.Fatalf("%v %+v", err, cfg.Accounts[0])
		}
	})
	t.Run("rename to own current name is a no-op", func(t *testing.T) {
		cfg := mk()
		if err := cfg.UpdateAccount("a", "a", "k5"); err != nil || cfg.Accounts[0].Name != "a" || cfg.Accounts[0].APIKey != "k5" {
			t.Fatalf("%v %+v", err, cfg.Accounts[0])
		}
	})
	t.Run("conflict: other account has that Name", func(t *testing.T) {
		cfg := mk()
		err := cfg.UpdateAccount("a", "b", "k-new")
		if err == nil || !strings.Contains(err.Error(), "already exists") || cfg.Accounts[0].Name != "a" || cfg.Accounts[0].APIKey != "k1" {
			t.Fatalf("err=%v %+v", err, cfg.Accounts[0])
		}
	})
	t.Run("conflict: other account has that Tailnet", func(t *testing.T) {
		cfg := mk()
		err := cfg.UpdateAccount("a", "b.com", "")
		if err == nil || cfg.Accounts[0].Name != "a" {
			t.Fatalf("err=%v %+v", err, cfg.Accounts[0])
		}
	})
	t.Run("no conflict: rename to own tailnet", func(t *testing.T) {
		cfg := mk()
		if err := cfg.UpdateAccount("a", "a.com", ""); err != nil || cfg.Accounts[0].Name != "a.com" {
			t.Fatalf("err=%v %+v", err, cfg.Accounts[0])
		}
	})
	t.Run("no conflict: new name unused", func(t *testing.T) {
		cfg := mk()
		if err := cfg.UpdateAccount("a", "fresh", ""); err != nil || cfg.Accounts[0].Name != "fresh" {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("no conflict: new name equals own old name while another account uses it as tailnet", func(t *testing.T) {
		cfg := &Config{Accounts: []Account{
			{Name: "n", Tailnet: "a.com"},
			{Name: "m", Tailnet: "n"},
		}}
		if err := cfg.UpdateAccount("a.com", "n", ""); err != nil {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("conflict: new name differs from own and another account uses it as tailnet", func(t *testing.T) {
		cfg := &Config{Accounts: []Account{
			{Name: "n", Tailnet: "a.com"},
			{Name: "m", Tailnet: "n2"},
		}}
		if err := cfg.UpdateAccount("a.com", "n2", ""); err == nil {
			t.Fatal("expected conflict")
		}
	})
}

// ─── Machine credentials ─────────────────────────────────────────────────────

// Verifies: REQ-CFG-030
func TestMachineCredentials(t *testing.T) {
	cfg := &Config{}
	cfg.DeleteMachineCredential("x") // nil map must not panic
	cfg.SetMachineCredential("id1", MachineAuth{Username: "u1", PasswordEncrypted: "p1"})
	if cfg.MachineCredentials["id1"].Username != "u1" {
		t.Fatal("not stored")
	}
	cfg.SetMachineCredential("id1", MachineAuth{Username: "u2"})
	cfg.SetMachineCredential("id2", MachineAuth{Name: "n"})
	if len(cfg.MachineCredentials) != 2 || cfg.MachineCredentials["id1"].Username != "u2" || cfg.MachineCredentials["id1"].PasswordEncrypted != "" {
		t.Fatalf("overwrite wrong: %+v", cfg.MachineCredentials)
	}
	cfg.DeleteMachineCredential("nope")
	if len(cfg.MachineCredentials) != 2 {
		t.Fatal("unknown key must be a no-op")
	}
	cfg.DeleteMachineCredential("id1")
	if _, ok := cfg.MachineCredentials["id1"]; ok || len(cfg.MachineCredentials) != 1 {
		t.Fatalf("%+v", cfg.MachineCredentials)
	}
	cfg.DeleteMachineCredential("") // empty key boundary
}

// ─── Saved commands ──────────────────────────────────────────────────────────

// Verifies: REQ-CFG-031
func TestUpsertCommand(t *testing.T) {
	cfg := &Config{}
	cfg.UpsertCommand("", SavedCommand{Name: "a", Command: "1"})
	cfg.UpsertCommand("", SavedCommand{Name: "b", Command: "2"})
	cfg.UpsertCommand("", SavedCommand{Name: "a", Command: "1b"}) // replace by name
	if len(cfg.Commands) != 2 || cfg.Commands[0].Command != "1b" {
		t.Fatalf("%+v", cfg.Commands)
	}
	cfg.UpsertCommand("b", SavedCommand{Name: "b2", Command: "22"}) // rename via oldName
	if len(cfg.Commands) != 2 || cfg.Commands[1].Name != "b2" || cfg.Commands[1].Command != "22" {
		t.Fatalf("%+v", cfg.Commands)
	}
	cfg.UpsertCommand("ghost", SavedCommand{Name: "c", Command: "3"}) // oldName not found: append
	if len(cfg.Commands) != 3 || cfg.Commands[2].Name != "c" {
		t.Fatalf("%+v", cfg.Commands)
	}
	// oldName takes precedence: cmd.Name "a" exists but oldName "c" is replaced.
	cfg.UpsertCommand("c", SavedCommand{Name: "a", Command: "dup"})
	if cfg.Commands[2].Command != "dup" || cfg.Commands[0].Command != "1b" {
		t.Fatalf("%+v", cfg.Commands)
	}
}

// Verifies: REQ-CFG-032
func TestDeleteCommand(t *testing.T) {
	cfg := &Config{Commands: []SavedCommand{{Name: "a"}, {Name: "b"}, {Name: "c"}}}
	if cfg.DeleteCommand("nope") || cfg.DeleteCommand("") || len(cfg.Commands) != 3 {
		t.Fatal("unknown must not delete")
	}
	if !cfg.DeleteCommand("b") || len(cfg.Commands) != 2 || cfg.Commands[0].Name != "a" || cfg.Commands[1].Name != "c" {
		t.Fatalf("middle: %+v", cfg.Commands)
	}
	if !cfg.DeleteCommand("c") || !cfg.DeleteCommand("a") || len(cfg.Commands) != 0 {
		t.Fatalf("ends: %+v", cfg.Commands)
	}
	if (&Config{}).DeleteCommand("a") {
		t.Fatal("empty list")
	}
}

// ─── Legacy migration ────────────────────────────────────────────────────────

// Verifies: REQ-CFG-033
func TestMigrationParsesOldFormatAndEncrypts(t *testing.T) {
	dir := useTempConfigDir(t)
	old := "\r\n# comment TAILNET=evil.com\n  TAILSCALE_API_KEY =  tskey-api-OLDSECRET-x  \nno equals sign\nUNKNOWN=1\n\n TAILNET=old.com\nSSH_USERNAME=bob=smith\n"
	if err := os.WriteFile(filepath.Join(dir, oldFileName), []byte(old), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Accounts) != 1 {
		t.Fatalf("%+v", cfg)
	}
	a := cfg.Accounts[0]
	if a.Name != "old.com" || a.Tailnet != "old.com" || a.APIKey != "tskey-api-OLDSECRET-x" || !a.Active || cfg.SSHUsername != "bob=smith" || cfg.ConfigVersion != CurrentVersion {
		t.Fatalf("migration wrong: %+v / %+v", a, cfg)
	}
	raw := mustRead(t, cfgPath(dir))
	if bytes.Contains(raw, []byte("OLDSECRET")) {
		t.Fatal("migrated key stored in cleartext")
	}
	if m := fileMode(t, cfgPath(dir)); m != 0600 {
		t.Fatalf("mode %o", m)
	}
	if bk := mustRead(t, filepath.Join(dir, oldFileName+".backup")); string(bk) != old {
		t.Fatal("backup differs from the original")
	}
	if _, err := os.Stat(filepath.Join(dir, oldFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("old file must be gone")
	}
	noTempFiles(t, dir)
	// Next load reads the new file.
	again, err := Load()
	if err != nil || again.Accounts[0].APIKey != "tskey-api-OLDSECRET-x" {
		t.Fatalf("%+v %v", again, err)
	}
}

// Verifies: REQ-CFG-034
func TestMigrationRequiresKeyAndTailnet(t *testing.T) {
	cases := map[string]string{
		"missing key":       "TAILNET=t.com\nSSH_USERNAME=u\n",
		"missing tailnet":   "TAILSCALE_API_KEY=k\n",
		"empty key value":   "TAILSCALE_API_KEY=\nTAILNET=t.com\n",
		"empty tailnet val": "TAILSCALE_API_KEY=k\nTAILNET=   \n",
		"empty file":        "",
		"only comments":     "# TAILSCALE_API_KEY=k\n# TAILNET=t.com\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			dir := useTempConfigDir(t)
			oldPath := filepath.Join(dir, oldFileName)
			if err := os.WriteFile(oldPath, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load()
			if err != nil || len(cfg.Accounts) != 0 {
				t.Fatalf("%+v %v", cfg, err)
			}
			if got := mustRead(t, oldPath); string(got) != content {
				t.Fatal("old file must be untouched")
			}
			if _, err := os.Stat(cfgPath(dir)); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("no config.json may be created")
			}
		})
	}
}

// Verifies: REQ-CFG-035
func TestMigrationSaveFailureLeavesOldFile(t *testing.T) {
	dir := useTempConfigDir(t)
	old := "TAILSCALE_API_KEY=tskey-api-k\nTAILNET=t.com\n"
	oldPath := filepath.Join(dir, oldFileName)
	if err := os.WriteFile(oldPath, []byte(old), 0600); err != nil {
		t.Fatal(err)
	}
	swapEncrypt(t, func(string) (string, error) { return "", errInjected })
	cfg, err := Load()
	if err != nil || len(cfg.Accounts) != 0 {
		t.Fatalf("%+v %v", cfg, err)
	}
	if got := mustRead(t, oldPath); string(got) != old {
		t.Fatal("old file must survive")
	}
	if _, err := os.Stat(oldPath + ".backup"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("no backup may exist")
	}
	noTempFiles(t, dir)
}

// Verifies: REQ-CFG-036
func TestMigrationBackupRenameFailureIsOnlyAWarning(t *testing.T) {
	dir := useTempConfigDir(t)
	old := "TAILSCALE_API_KEY=tskey-api-k\nTAILNET=t.com\n"
	oldPath := filepath.Join(dir, oldFileName)
	if err := os.WriteFile(oldPath, []byte(old), 0600); err != nil {
		t.Fatal(err)
	}
	// The backup name is taken by a non-empty directory, so the rename fails.
	if err := os.MkdirAll(filepath.Join(oldPath+".backup", "x"), 0700); err != nil {
		t.Fatal(err)
	}
	// Capture stderr.
	r, w, _ := os.Pipe()
	saved := os.Stderr
	os.Stderr = w
	cfg, err := Load()
	os.Stderr = saved
	w.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	if err != nil || len(cfg.Accounts) != 1 || cfg.Accounts[0].APIKey != "tskey-api-k" {
		t.Fatalf("%+v %v", cfg, err)
	}
	if !strings.Contains(buf.String(), "Warning: could not rename old config") {
		t.Fatalf("stderr = %q", buf.String())
	}
	if _, err := os.Stat(cfgPath(dir)); err != nil {
		t.Fatal("new config must be written")
	}
}

// Verifies: REQ-CFG-037
func TestMigrationOnlyWhenConfigJSONIsMissing(t *testing.T) {
	t.Run("existing config.json wins", func(t *testing.T) {
		dir := useTempConfigDir(t)
		oldPath := filepath.Join(dir, oldFileName)
		old := "TAILSCALE_API_KEY=k\nTAILNET=t.com\n"
		if err := os.WriteFile(oldPath, []byte(old), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(cfgPath(dir), []byte(`{"accounts":[]}`), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load()
		if err != nil || len(cfg.Accounts) != 0 {
			t.Fatalf("%+v %v", cfg, err)
		}
		if got := mustRead(t, oldPath); string(got) != old {
			t.Fatal("old file must be untouched")
		}
	})
	t.Run("old path is a directory (unreadable)", func(t *testing.T) {
		dir := useTempConfigDir(t)
		if err := os.Mkdir(filepath.Join(dir, oldFileName), 0700); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load()
		if err != nil || len(cfg.Accounts) != 0 {
			t.Fatalf("%+v %v", cfg, err)
		}
	})
	t.Run("config dir cannot be determined", func(t *testing.T) {
		noConfigDir(t)
		if cfg, ok := migrateOldConfig(); ok || cfg != nil {
			t.Fatalf("%+v %v", cfg, ok)
		}
	})
}
