package commands

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/yatsenko-ihor/ts-cli/client"
	"github.com/yatsenko-ihor/ts-cli/config"
	"github.com/yatsenko-ihor/ts-cli/util"
)

func runAccount(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	cmd := NewAccountCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func seedAccounts(t *testing.T) {
	t.Helper()
	t.Setenv(util.ConfigDirEnv, t.TempDir())
	err := config.Update(func(c *config.Config) error {
		c.AddAccount("home", "tskey-api-kAAA-0123456789abcdef", "home.example.com", true)
		c.AddAccount("work", "tskey-api-kBBB-0123456789abcdef", "work@example.com", false)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestKeyInputPrefersStdinThenFlagThenEnv(t *testing.T) {
	t.Setenv("TAILSCALE_API_KEY", "tskey-api-fromenv-000000000000")
	k := keyInput{stdin: true, flag: "tskey-api-flag-0000000000000"}
	if got, _ := k.resolve(strings.NewReader("tskey-api-stdin-0000000000000\nignored\n")); got != "tskey-api-stdin-0000000000000" {
		t.Fatalf("stdin: %q", got)
	}
	if got, _ := (keyInput{flag: "tskey-api-flag-0000000000000"}).resolve(nil); got != "tskey-api-flag-0000000000000" {
		t.Fatalf("flag: %q", got)
	}
	if got, _ := (keyInput{}).resolve(nil); got != "tskey-api-fromenv-000000000000" {
		t.Fatalf("env: %q", got)
	}
}

func TestAccountListJSONAndHideShow(t *testing.T) {
	seedAccounts(t)
	if _, err := runAccount(t, "", "hide", "work"); err != nil {
		t.Fatal(err)
	}
	out, err := runAccount(t, "", "list", "--format=json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []accountJSON
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("invalid JSON %q: %v", out, err)
	}
	if len(rows) != 2 || rows[0].Hidden || !rows[1].Hidden || !rows[0].Active {
		t.Fatalf("rows = %+v", rows)
	}
	if strings.Contains(out, "tskey") {
		t.Fatal("list must never print API keys")
	}
	if _, err := runAccount(t, "", "show", "work"); err != nil {
		t.Fatal(err)
	}
	if cfg, _ := config.Load(); cfg.Accounts[1].Hidden {
		t.Fatal("show must clear the flag")
	}
	if _, err := runAccount(t, "", "hide", "nope"); err == nil {
		t.Fatal("unknown account must fail")
	}
}

func TestAccountEditRenamesWithoutKey(t *testing.T) {
	seedAccounts(t)
	if _, err := runAccount(t, "", "edit", "work", "--name=job"); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load()
	if cfg.Accounts[1].Name != "job" || cfg.Accounts[1].APIKey != "tskey-api-kBBB-0123456789abcdef" {
		t.Fatalf("account = %+v", cfg.Accounts[1])
	}
	if _, err := runAccount(t, "", "edit", "job"); err == nil {
		t.Fatal("edit without changes must fail")
	}
	if _, err := runAccount(t, "", "edit", "job", "--name=bad/name"); err == nil {
		t.Fatal("invalid name must fail")
	}
	if _, err := runAccount(t, "", "edit", "job", "--name=home"); err == nil {
		t.Fatal("duplicate name must fail")
	}
}

func TestAccountRemove(t *testing.T) {
	seedAccounts(t)
	if _, err := runAccount(t, "", "rm", "home"); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load()
	if len(cfg.Accounts) != 1 || !cfg.Accounts[0].Active {
		t.Fatalf("accounts = %+v", cfg.Accounts)
	}
}

func TestKeyStatus(t *testing.T) {
	now := time.Now()
	if w, _ := keyStatus(client.Health{Valid: true, HasExpiry: true, Expires: now.Add(48 * time.Hour)}, now); w != "ok" {
		t.Fatal("valid key")
	}
	if w, n := keyStatus(client.Health{Valid: true, HasExpiry: true, Expires: now.Add(-time.Hour)}, now); w != "invalid" || n != "expired" {
		t.Fatal("expired key")
	}
	if w, _ := keyStatus(client.Health{Unauth: true}, now); w != "invalid" {
		t.Fatal("rejected key")
	}
	if w, _ := keyStatus(client.Health{Err: nil}, now); w != "unreachable" {
		t.Fatal("unreachable")
	}
}
