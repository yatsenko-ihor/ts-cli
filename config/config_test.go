package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yatsenko-ihor/ts-cli/util"
)

func useTempConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(util.ConfigDirEnv, dir)
	return dir
}

func TestSetActiveAccount(t *testing.T) {
	cfg := &Config{Accounts: []Account{
		{Name: "personal", APIKey: "key1", Tailnet: "personal.com", Active: true},
		{Name: "work", APIKey: "key2", Tailnet: "work.com"},
		{Name: "test", APIKey: "key3", Tailnet: "test.com"},
	}}

	if !cfg.SetActiveAccount("work.com") {
		t.Fatal("expected true for existing tailnet")
	}
	if !cfg.Accounts[1].Active || cfg.Accounts[0].Active || cfg.Accounts[2].Active {
		t.Fatalf("active flags wrong: %+v", cfg.Accounts)
	}

	// By name as well as by tailnet.
	if !cfg.SetActiveAccount("test") || !cfg.Accounts[2].Active {
		t.Fatal("SetActiveAccount by name failed")
	}

	if cfg.SetActiveAccount("doesnotexist.com") {
		t.Fatal("expected false for unknown account")
	}
	if !cfg.Accounts[2].Active {
		t.Fatal("unknown account must not change active state")
	}
}

func TestGetActiveAccount(t *testing.T) {
	if (&Config{}).GetActiveAccount() != nil {
		t.Fatal("empty config should have no active account")
	}
	cfg := &Config{Accounts: []Account{{Name: "a"}, {Name: "b", Active: true}}}
	if got := cfg.GetActiveAccount(); got == nil || got.Name != "b" {
		t.Fatalf("got %+v", got)
	}
}

func TestAddAccountUpdatesByTailnet(t *testing.T) {
	cfg := &Config{}
	cfg.AddAccount("one", "k1", "one.com", true)
	cfg.AddAccount("two", "k2", "two.com", true)
	cfg.AddAccount("one-renamed", "k1b", "one.com", false)

	if len(cfg.Accounts) != 2 {
		t.Fatalf("expected 2 accounts, got %d", len(cfg.Accounts))
	}
	one := cfg.GetAccountByTailnet("one.com")
	if one.Name != "one-renamed" || one.APIKey != "k1b" {
		t.Fatalf("update by tailnet failed: %+v", one)
	}
	if !cfg.GetAccountByTailnet("two.com").Active {
		t.Fatal("two.com should remain active")
	}
}

func TestRemoveAccountPromotesFirstRemaining(t *testing.T) {
	cfg := &Config{Accounts: []Account{
		{Name: "a", Tailnet: "a.com", Active: true},
		{Name: "b", Tailnet: "b.com"},
	}}
	if !cfg.RemoveAccount("a") {
		t.Fatal("expected removal")
	}
	if len(cfg.Accounts) != 1 || !cfg.Accounts[0].Active {
		t.Fatalf("remaining account should become active: %+v", cfg.Accounts)
	}
	if cfg.RemoveAccount("nope") {
		t.Fatal("expected false for unknown account")
	}
}

func TestLoadSaveRoundTripAndPermissions(t *testing.T) {
	dir := useTempConfigDir(t)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Accounts) != 0 || cfg.ConfigVersion != CurrentVersion {
		t.Fatalf("unexpected empty config: %+v", cfg)
	}

	cfg.AddAccount("work", "tskey-api-x", "work.com", true)
	cfg.SSHUsername = "admin"
	cfg.SetMachineCredential("12345", MachineAuth{Name: "laptop", Username: "root", PasswordEncrypted: "enc"})
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(filepath.Join(dir, fileName))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("config perm = %o, want 0600", perm)
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, "*.tmp")); len(matches) != 0 {
		t.Fatalf("temp file left behind: %v", matches)
	}

	back, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if back.SSHUsername != "admin" || back.MachineCredentials["12345"].Name != "laptop" || back.Accounts[0].APIKey != "tskey-api-x" {
		t.Fatalf("round trip lost data: %+v", back)
	}
}

func TestAPIKeysAreEncryptedOnDisk(t *testing.T) {
	dir := useTempConfigDir(t)
	cfg := &Config{}
	cfg.AddAccount("work", "tskey-api-SECRET-VALUE", "work.com", true)
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, fileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "tskey-api-SECRET-VALUE") {
		t.Fatalf("API key stored in cleartext:\n%s", raw)
	}
	if !strings.Contains(string(raw), `"api_key_enc"`) || strings.Contains(string(raw), `"api_key"`) {
		t.Fatalf("expected only api_key_enc on disk:\n%s", raw)
	}

	back, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if back.Accounts[0].APIKey != "tskey-api-SECRET-VALUE" {
		t.Fatalf("API key not decrypted on load: %+v", back.Accounts[0])
	}
}

func TestLegacyCleartextAPIKeyIsMigrated(t *testing.T) {
	dir := useTempConfigDir(t)
	legacy := `{"accounts":[{"name":"old","api_key":"tskey-api-legacy","tailnet":"old.com","active":true}],"config_version":"1.0"}`
	if err := os.WriteFile(filepath.Join(dir, fileName), []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Accounts[0].APIKey != "tskey-api-legacy" {
		t.Fatalf("legacy key not readable: %+v", cfg.Accounts[0])
	}

	raw, _ := os.ReadFile(filepath.Join(dir, fileName))
	if strings.Contains(string(raw), "tskey-api-legacy") {
		t.Fatal("legacy cleartext key should be re-encrypted on load")
	}
}

func TestSavedCommandsUpsertDelete(t *testing.T) {
	cfg := &Config{}
	cfg.UpsertCommand("", SavedCommand{Name: "uptime", Command: "uptime"})
	cfg.UpsertCommand("", SavedCommand{Name: "disk", Command: "df -h {{path}}"})
	cfg.UpsertCommand("", SavedCommand{Name: "uptime", Command: "uptime -p"})
	if len(cfg.Commands) != 2 || cfg.Commands[0].Command != "uptime -p" {
		t.Fatalf("upsert by name failed: %+v", cfg.Commands)
	}
	cfg.UpsertCommand("disk", SavedCommand{Name: "disk usage", Command: "df -h"})
	if len(cfg.Commands) != 2 || cfg.Commands[1].Name != "disk usage" {
		t.Fatalf("rename failed: %+v", cfg.Commands)
	}
	if !cfg.DeleteCommand("uptime") || len(cfg.Commands) != 1 || cfg.DeleteCommand("nope") {
		t.Fatalf("delete failed: %+v", cfg.Commands)
	}

	useTempConfigDir(t)
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	back, _ := Load()
	if len(back.Commands) != 1 || back.Commands[0].Name != "disk usage" {
		t.Fatalf("commands not persisted: %+v", back.Commands)
	}
}

func TestLoadRefusesCorruptFile(t *testing.T) {
	dir := useTempConfigDir(t)
	if err := os.WriteFile(filepath.Join(dir, fileName), []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil {
		t.Fatal("expected error for corrupt config")
	}
}

func TestUpdateIsReadModifyWrite(t *testing.T) {
	useTempConfigDir(t)
	if err := Save(&Config{SSHUsername: "keep-me"}); err != nil {
		t.Fatal(err)
	}
	err := Update(func(c *Config) error {
		c.SetMachineCredential("1", MachineAuth{Username: "u"})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := Load()
	if cfg.SSHUsername != "keep-me" || cfg.MachineCredentials["1"].Username != "u" {
		t.Fatalf("Update lost data: %+v", cfg)
	}
}

func TestMigrateOldConfig(t *testing.T) {
	dir := useTempConfigDir(t)
	old := "TAILSCALE_API_KEY=tskey-api-old\nTAILNET=old.com\nSSH_USERNAME=bob\n# comment\n"
	if err := os.WriteFile(filepath.Join(dir, oldFileName), []byte(old), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Accounts) != 1 || cfg.Accounts[0].APIKey != "tskey-api-old" || cfg.SSHUsername != "bob" {
		t.Fatalf("migration wrong: %+v", cfg)
	}
	if _, err := os.Stat(filepath.Join(dir, oldFileName+".backup")); err != nil {
		t.Fatal("old config should be renamed to .backup")
	}
	if _, err := os.Stat(filepath.Join(dir, fileName)); err != nil {
		t.Fatal("new config should be written")
	}
}
