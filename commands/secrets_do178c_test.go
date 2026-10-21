package commands

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/yatsenko-ihor/ts-cli/config"
	"github.com/yatsenko-ihor/ts-cli/util"
)

// Requirements-based tests for secrets.go (verification/requirements/secrets.md).
// The OS keychain is replaced by go-keyring's in-memory provider; nothing
// touches the real keychain or home directory.

const (
	testKeychainService = "ts-cli"
	testKeychainAccount = "master-key"
)

func runSecrets(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	return runCobra(t, NewSecretsCommand(), "", args...)
}

// secretsEnv isolates the config dir, installs an empty in-memory keychain and
// leaves the process key cache empty with the file store active (key file
// present), so every test starts from a known, coherent state.
func secretsEnv(t *testing.T) string {
	t.Helper()
	dir := isolate(t)
	keyring.MockInit()
	if err := util.UseKeychain(); err != nil {
		t.Fatal(err)
	}
	if err := util.UseFile(); err != nil {
		t.Fatal(err)
	}
	return dir
}

func keychainKey(t *testing.T) []byte {
	t.Helper()
	enc, err := keyring.Get(testKeychainService, testKeychainAccount)
	if err != nil {
		t.Fatal(err)
	}
	k, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// secretStrings lists every representation of key material that must never be
// printed: raw and base64 of the master key.
func keyMaterial(key []byte) []string {
	return []string{string(key), base64.StdEncoding.EncodeToString(key), base64.RawStdEncoding.EncodeToString(key)}
}

func storeFileContent(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "secret_store"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func seedSecrets(t *testing.T, accounts int, passwords map[string]string) {
	t.Helper()
	err := config.Update(func(c *config.Config) error {
		if accounts > 0 {
			c.AddAccount("home", keyA, "home.example.com", true)
		}
		if accounts > 1 {
			c.AddAccount("work", keyB, "work@example.com", false)
		}
		for id, name := range passwords {
			enc, err := util.EncryptPassword("hunter2-" + id)
			if err != nil {
				return err
			}
			c.SetMachineCredential(id, config.MachineAuth{Name: name, Username: "root", PasswordEncrypted: enc})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Verifies: REQ-SEC-001, REQ-SEC-002
func TestSECStatusDefaultFileStore(t *testing.T) {
	secretsEnv(t)
	seedSecrets(t, 2, map[string]string{"m1": "box"})
	stdout, stderr, err := runSecrets(t, "status")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Secret store: file\n", "Keychain:     available\n", "Config:       2 account(s), 1 saved machine credential(s)\n"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout lacks %q: %q", want, stdout)
		}
	}
	if strings.Contains(stdout, "Notice") || stderr != "" {
		t.Fatalf("out %q err %q", stdout, stderr)
	}
	noSecret(t, []string{keyA, keyB, "hunter2"}, stdout, stderr)
}

// Verifies: REQ-SEC-001, REQ-SEC-002
func TestSECStatusEmptyConfigAndUnavailableKeychain(t *testing.T) {
	secretsEnv(t)
	keyring.MockInitWithError(errors.New("no D-Bus session"))
	stdout, _, err := runSecrets(t, "status")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Secret store: file", "Keychain:     unavailable (no D-Bus session)", "Config:       0 account(s), 0 saved machine credential(s)"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout lacks %q: %q", want, stdout)
		}
	}
}

// Verifies: REQ-SEC-001, REQ-SEC-003
func TestSECStatusKeychainStoreHidesKeyMaterial(t *testing.T) {
	dir := secretsEnv(t)
	seedSecrets(t, 1, nil)
	if err := util.UseKeychain(); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := runSecrets(t, "status")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "Secret store: keychain") || !strings.Contains(stdout, "1 account(s)") || strings.Contains(stdout, "Notice") {
		t.Fatalf("stdout %q", stdout)
	}
	noSecret(t, append(keyMaterial(keychainKey(t)), keyA, dir+"/key"), stdout, stderr)
}

// Verifies: REQ-SEC-004
func TestSECStatusReportsUndecryptableConfigWithoutFailing(t *testing.T) {
	secretsEnv(t)
	seedSecrets(t, 1, nil)
	if err := util.UseKeychain(); err != nil { // resets the key cache
		t.Fatal(err)
	}
	other := bytes.Repeat([]byte{0x42}, 32)
	if err := keyring.Set(testKeychainService, testKeychainAccount, base64.StdEncoding.EncodeToString(other)); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := runSecrets(t, "status")
	if err != nil {
		t.Fatalf("status must not fail on an undecryptable config: %v", err)
	}
	if !strings.Contains(stdout, "Config:       cannot be decrypted:") || strings.Contains(stdout, "account(s)") {
		t.Fatalf("stdout %q", stdout)
	}
	noSecret(t, append(keyMaterial(other), keyA), stdout, stderr)
}

// Verifies: REQ-SEC-005
func TestSECStatusShowsFallbackNotice(t *testing.T) {
	dir := secretsEnv(t)
	seedSecrets(t, 1, nil)
	if err := util.UseKeychain(); err != nil {
		t.Fatal(err)
	}
	key := keychainKey(t)
	// the key file is still around (fallback), the keychain stops working
	if err := os.WriteFile(filepath.Join(dir, "key"), key, 0600); err != nil {
		t.Fatal(err)
	}
	keyring.MockInitWithError(errors.New("locked"))
	stdout, stderr, err := runSecrets(t, "status")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Secret store: keychain", "Keychain:     unavailable (locked)", "Config:       1 account(s)", "Notice:       keychain unavailable (locked); using the key file key instead"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout lacks %q: %q", want, stdout)
		}
	}
	noSecret(t, append(keyMaterial(key), keyA), stdout, stderr)
}

// Verifies: REQ-SEC-006, REQ-SEC-003
func TestSECUseKeychainMovesKeyAndVerifies(t *testing.T) {
	dir := secretsEnv(t)
	seedSecrets(t, 2, map[string]string{"m1": "box"})
	fileKey, err := os.ReadFile(filepath.Join(dir, "key"))
	if err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := runSecrets(t, "use", "keychain")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"✓ Master key is now stored in the OS keychain; ~/.ts-cli/key removed", "✓ Stored credentials verified readable"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout lacks %q: %q", want, stdout)
		}
	}
	if strings.Contains(stdout, "⚠") {
		t.Fatalf("no loss warning expected: %q", stdout)
	}
	if _, err := os.Stat(filepath.Join(dir, "key")); err == nil {
		t.Fatal("key file must be removed")
	}
	if storeFileContent(t, dir) != "keychain" || util.CurrentSecretStore() != util.SecretStoreKeychain {
		t.Fatal("store not recorded")
	}
	if !bytes.Equal(keychainKey(t), fileKey) {
		t.Fatal("the key value must not change")
	}
	if c := loadCfg(t); len(c.Accounts) != 2 || c.Accounts[1].APIKey != keyB || c.MachineCredentials["m1"].PasswordEncrypted == "" {
		t.Fatalf("config after move: %+v", c)
	}
	noSecret(t, append(keyMaterial(fileKey), keyA, keyB, "hunter2"), stdout, stderr)

	// repeating it is a harmless no-op
	stdout, _, err = runSecrets(t, "use", "keychain")
	if err != nil || strings.Contains(stdout, "⚠") || !strings.Contains(stdout, "verified readable") {
		t.Fatalf("repeat: %q %v", stdout, err)
	}
}

// Verifies: REQ-SEC-007
func TestSECUseFileMovesKeyBack(t *testing.T) {
	dir := secretsEnv(t)
	seedSecrets(t, 1, map[string]string{"m1": "box"})
	if _, _, err := runSecrets(t, "use", "keychain"); err != nil {
		t.Fatal(err)
	}
	key := keychainKey(t)
	stdout, stderr, err := runSecrets(t, "use", "file")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"✓ Master key is now stored in ~/.ts-cli/key; keychain item removed", "✓ Stored credentials verified readable"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout lacks %q: %q", want, stdout)
		}
	}
	got, err := os.ReadFile(filepath.Join(dir, "key"))
	if err != nil || !bytes.Equal(got, key) {
		t.Fatalf("key file: %v", err)
	}
	fi, _ := os.Stat(filepath.Join(dir, "key"))
	if fi.Mode().Perm() != 0600 {
		t.Fatalf("key file mode %v", fi.Mode().Perm())
	}
	if _, err := keyring.Get(testKeychainService, testKeychainAccount); !errors.Is(err, keyring.ErrNotFound) {
		t.Fatalf("keychain item must be gone: %v", err)
	}
	if storeFileContent(t, dir) != "file" {
		t.Fatal("store not recorded")
	}
	if c := loadCfg(t); len(c.Accounts) != 1 || c.Accounts[0].APIKey != keyA {
		t.Fatalf("config: %+v", c)
	}
	noSecret(t, append(keyMaterial(key), keyA, "hunter2"), stdout, stderr)

	// already file: no-op, still verified
	stdout, _, err = runSecrets(t, "use", "file")
	if err != nil || !strings.Contains(stdout, "verified readable") {
		t.Fatalf("repeat: %q %v", stdout, err)
	}
}

// Verifies: REQ-SEC-008
func TestSECUseRejectsUnknownStoreAndBadArity(t *testing.T) {
	dir := secretsEnv(t)
	seedSecrets(t, 1, nil)
	before, _ := os.ReadFile(filepath.Join(dir, "key"))
	for _, arg := range []string{"cloud", "", "KEYCHAIN", "File", " file", "file;rm -rf /", "keychain\x00"} {
		stdout, stderr, err := runSecrets(t, "use", arg)
		if err == nil || !strings.Contains(err.Error(), "unknown store") {
			t.Fatalf("%q: %v", arg, err)
		}
		if strings.Contains(stdout, "✓") {
			t.Fatalf("%q: success text %q", arg, stdout)
		}
		noSecret(t, []string{keyA}, stdout, stderr)
	}
	for _, args := range [][]string{{"use"}, {"use", "file", "keychain"}} {
		if _, _, err := runSecrets(t, args...); err == nil {
			t.Fatalf("%v must fail", args)
		}
	}
	after, _ := os.ReadFile(filepath.Join(dir, "key"))
	if !bytes.Equal(before, after) || storeFileContent(t, dir) != "file" {
		t.Fatal("nothing may change for a rejected argument")
	}
}

// Verifies: REQ-SEC-009
func TestSECUseKeychainFailsWhenKeychainUnavailable(t *testing.T) {
	dir := secretsEnv(t)
	seedSecrets(t, 1, nil)
	keyring.MockInitWithError(errors.New("no secret service"))
	stdout, stderr, err := runSecrets(t, "use", "keychain")
	if err == nil || !strings.Contains(err.Error(), "keychain is not available") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(stdout, "✓") || strings.Contains(stdout, "⚠") {
		t.Fatalf("stdout %q", stdout)
	}
	if _, e := os.Stat(filepath.Join(dir, "key")); e != nil || storeFileContent(t, dir) != "file" {
		t.Fatal("key file and store must be untouched")
	}
	noSecret(t, []string{keyA}, stdout, stderr, errText(err))
}

// Verifies: REQ-SEC-009
func TestSECUseFileFailsWhenKeyIsUnreachable(t *testing.T) {
	dir := secretsEnv(t)
	seedSecrets(t, 1, nil)
	if _, _, err := runSecrets(t, "use", "keychain"); err != nil {
		t.Fatal(err)
	}
	// the keychain item vanishes and there is no key file
	if err := keyring.Delete(testKeychainService, testKeychainAccount); err != nil {
		t.Fatal(err)
	}
	stdout, _, err := runSecrets(t, "use", "file")
	if err == nil || !strings.Contains(err.Error(), "cannot read the key from the keychain") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(stdout, "✓") || storeFileContent(t, dir) != "keychain" {
		t.Fatalf("out %q store %q", stdout, storeFileContent(t, dir))
	}
}

// Verifies: REQ-SEC-010
func TestSECUseReportsConfigThatCannotBeDecryptedAfterSwitch(t *testing.T) {
	dir := secretsEnv(t)
	bad := corruptConfig(t, dir)
	stdout, _, err := runSecrets(t, "use", "keychain")
	if err == nil || !strings.Contains(err.Error(), "switched, but the config can no longer be decrypted") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(stdout, "verified readable") {
		t.Fatalf("must not claim verification: %q", stdout)
	}
	if !bytes.Equal(cfgFile(t, dir), bad) {
		t.Fatal("a corrupt config must stay as it is")
	}
	// the same on the way back
	stdout, _, err = runSecrets(t, "use", "file")
	if err == nil || !strings.Contains(err.Error(), "switched, but the config can no longer be decrypted") || strings.Contains(stdout, "verified readable") {
		t.Fatalf("file: %q %v", stdout, err)
	}
}

// loseKey puts the process into the "keychain store, item gone, no key file"
// state with the given config seeded under the old key.
func loseKey(t *testing.T, accounts int, passwords map[string]string) (dir string, oldKey []byte) {
	t.Helper()
	dir = secretsEnv(t)
	seedSecrets(t, accounts, passwords)
	if err := util.UseKeychain(); err != nil {
		t.Fatal(err)
	}
	oldKey = keychainKey(t)
	if err := keyring.Delete(testKeychainService, testKeychainAccount); err != nil {
		t.Fatal(err)
	}
	return dir, oldKey
}

// Verifies: REQ-SEC-011, REQ-SEC-012
func TestSECLostKeyRemovesUnreadableSecretsAndSaysSo(t *testing.T) {
	dir, oldKey := loseKey(t, 2, map[string]string{"m-named": "srv1", "m-anon": ""})
	stdout, stderr, err := runSecrets(t, "use", "keychain")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"⚠ The master key was missing from the keychain and there was no key file,",
		"a NEW key was created",
		"Removed accounts (add them again with `ts-cli account add`): home, work\n",
		"Removed saved SSH passwords for: m-anon, srv1 (you will be asked again on the next connection)\n",
		"✓ Master key is now stored in the OS keychain",
		"✓ Stored credentials verified readable",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout lacks %q: %q", want, stdout)
		}
	}
	if strings.Contains(stdout, "No stored secret depended") {
		t.Fatalf("stdout %q", stdout)
	}
	c := loadCfg(t)
	if len(c.Accounts) != 0 {
		t.Fatalf("accounts must be removed: %+v", c.Accounts)
	}
	for id, cred := range c.MachineCredentials {
		if cred.PasswordEncrypted != "" || cred.Username != "root" {
			t.Fatalf("credential %s = %+v (password cleared, username kept)", id, cred)
		}
	}
	if bytes.Equal(keychainKey(t), oldKey) {
		t.Fatal("a new key must have been created")
	}
	noSecret(t, append(append(keyMaterial(oldKey), keyMaterial(keychainKey(t))...), keyA, keyB, "hunter2"), stdout, stderr, string(cfgFile(t, dir)))
}

// Verifies: REQ-SEC-011, REQ-SEC-012
func TestSECLostKeyReportsOnlyWhatWasLost(t *testing.T) {
	// only accounts
	loseKey(t, 1, nil)
	stdout, _, err := runSecrets(t, "use", "keychain")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "Removed accounts") || strings.Contains(stdout, "Removed saved SSH passwords") || strings.Contains(stdout, "No stored secret depended") {
		t.Fatalf("accounts only: %q", stdout)
	}
	// only passwords
	loseKey(t, 0, map[string]string{"m1": "srv"})
	stdout, _, err = runSecrets(t, "use", "keychain")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout, "Removed accounts") || !strings.Contains(stdout, "Removed saved SSH passwords for: srv (") {
		t.Fatalf("passwords only: %q", stdout)
	}
	// nothing depended on the key
	loseKey(t, 0, nil)
	stdout, _, err = runSecrets(t, "use", "keychain")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "⚠") || !strings.Contains(stdout, "  No stored secret depended on the old key.\n") || strings.Contains(stdout, "Removed") {
		t.Fatalf("nothing lost: %q", stdout)
	}
}

// Verifies: REQ-SEC-013
func TestSECLostKeyWithUnreadableConfigIsReportedAsFailure(t *testing.T) {
	dir, _ := loseKey(t, 1, nil)
	bad := corruptConfig(t, dir)
	stdout, _, err := runSecrets(t, "use", "keychain")
	if !strings.Contains(stdout, "  Could not remove the unreadable secrets:") {
		t.Fatalf("stdout %q", stdout)
	}
	if err == nil || !strings.Contains(err.Error(), "switched, but the config can no longer be decrypted") {
		t.Fatalf("err = %v", err)
	}
	if !bytes.Equal(cfgFile(t, dir), bad) {
		t.Fatal("corrupt config must not be rewritten")
	}
}

// Verifies: REQ-SEC-014, REQ-SEC-011
func TestSECSwitchingWithoutLossDoesNotWarn(t *testing.T) {
	// A keychain store whose item is still readable is not a lost key.
	secretsEnv(t)
	seedSecrets(t, 1, nil)
	if _, _, err := runSecrets(t, "use", "keychain"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		stdout, _, err := runSecrets(t, "use", "keychain")
		if err != nil || strings.Contains(stdout, "⚠") {
			t.Fatalf("round %d: %q %v", i, stdout, err)
		}
	}
	if len(loadCfg(t).Accounts) != 1 {
		t.Fatal("accounts must survive")
	}
}

// Verifies: REQ-SEC-015
func TestSECCommandWiring(t *testing.T) {
	cmd := NewSecretsCommand()
	if cmd.Use != "secrets" || len(cmd.Commands()) != 2 {
		t.Fatalf("%s with %d subcommands", cmd.Use, len(cmd.Commands()))
	}
	for _, name := range []string{"status", "use"} {
		if sub, _, err := cmd.Find([]string{name}); err != nil || sub.Name() != name {
			t.Fatalf("%s: %v", name, err)
		}
	}
	use, _, _ := cmd.Find([]string{"use"})
	if strings.Join(use.ValidArgs, ",") != "keychain,file" {
		t.Fatalf("ValidArgs %v", use.ValidArgs)
	}
}
