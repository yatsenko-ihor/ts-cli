package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yatsenko-ihor/ts-cli/config"
)

// Requirements-based tests for login.go (verification/requirements/login.md).

func runLogin(t *testing.T, stdin string, args ...string) (string, string, error) {
	t.Helper()
	return runCobra(t, NewLoginCommand(), stdin, args...)
}

// Verifies: REQ-LGN-001, REQ-LGN-002
func TestLGNLoginStoresAndActivatesAccount(t *testing.T) {
	dir := seed3(t)
	api := startAPI(t, map[string]int{keyN: 200}, nil)
	stdout, stderr, err := runLogin(t, "", "--tailnet=new.example.com", "--api-key="+keyN)
	if err != nil {
		t.Fatal(err)
	}
	c := loadCfg(t)
	if len(c.Accounts) != 4 {
		t.Fatalf("accounts = %+v", c.Accounts)
	}
	active := 0
	for _, a := range c.Accounts {
		if a.Active {
			active++
			if a.Tailnet != "new.example.com" || a.APIKey != keyN || a.Name != "new.example.com" {
				t.Fatalf("active account = %+v", a)
			}
		}
	}
	if active != 1 {
		t.Fatalf("%d active accounts", active)
	}
	for _, want := range []string{"✓ API key is valid", "✓ Added new account: new.example.com", "✓ Authenticated with tailnet: new.example.com"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout lacks %q: %q", want, stdout)
		}
	}
	if api.tokens[0] != keyN {
		t.Fatalf("validated token %q", api.tokens[0])
	}
	noSecret(t, []string{keyN, keyA, keyB, keyC}, stdout, stderr, string(cfgFile(t, dir)))
}

// Verifies: REQ-LGN-001, REQ-LGN-008
func TestLGNLoginExistingTailnetIsUpdatedAndActivated(t *testing.T) {
	seed3(t)
	startAPI(t, map[string]int{keyN: 200}, nil)
	stdout, _, err := runLogin(t, "", "--tailnet=work@example.com", "--name=job", "--api-key="+keyN)
	if err != nil {
		t.Fatal(err)
	}
	c := loadCfg(t)
	if len(c.Accounts) != 3 || c.Accounts[0].Active || !c.Accounts[1].Active || c.Accounts[2].Active {
		t.Fatalf("accounts = %+v", c.Accounts)
	}
	if c.Accounts[1].APIKey != keyN || c.Accounts[1].Name != "job" || !strings.Contains(stdout, "✓ Updated account: job") {
		t.Fatalf("account %+v out %q", c.Accounts[1], stdout)
	}
}

// Verifies: REQ-LGN-002, REQ-LGN-003
func TestLGNLoginKeySourcesNeverPrinted(t *testing.T) {
	isolate(t)
	startAPI(t, map[string]int{keyA: 200, keyB: 200, keyC: 200}, nil)
	t.Setenv("TAILSCALE_API_KEY", keyA)
	o1, e1, err := runLogin(t, "", "--tailnet=a.example.com")
	if err != nil {
		t.Fatal(err)
	}
	// stdin beats the environment
	o2, e2, err := runLogin(t, keyB+"\n", "--tailnet=b.example.com", "--api-key-stdin")
	if err != nil {
		t.Fatal(err)
	}
	// the flag beats the environment
	o3, e3, err := runLogin(t, "", "--tailnet=c.example.com", "--api-key="+keyC)
	if err != nil {
		t.Fatal(err)
	}
	noSecret(t, []string{keyA, keyB, keyC}, o1, e1, o2, e2, o3, e3)
	c := loadCfg(t)
	if len(c.Accounts) != 3 || c.Accounts[0].APIKey != keyA || c.Accounts[1].APIKey != keyB || c.Accounts[2].APIKey != keyC || !c.Accounts[2].Active {
		t.Fatalf("accounts = %+v", c.Accounts)
	}
	// empty stdin selects no key
	if _, _, err := runLogin(t, "", "--tailnet=d.example.com", "--api-key-stdin"); err == nil || !strings.Contains(err.Error(), "API key not provided") {
		t.Fatalf("empty stdin: %v", err)
	}
}

// Verifies: REQ-LGN-003
func TestLGNLoginUnreadableStdinIsAnError(t *testing.T) {
	isolate(t)
	api := startAPI(t, map[string]int{keyN: 200}, nil)
	cmd := NewLoginCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(failingReader{})
	cmd.SetArgs([]string{"--tailnet=x.example.com", "--api-key-stdin"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "failed to read API key from stdin") {
		t.Fatalf("err = %v", err)
	}
	if api.hitCount() != 0 || len(loadCfg(t).Accounts) != 0 {
		t.Fatal("nothing may be sent or stored")
	}
}

// Verifies: REQ-LGN-004
func TestLGNLoginRejectedKeyStoresNothingAndKeepsActive(t *testing.T) {
	dir := seed3(t)
	startAPI(t, map[string]int{keyN: 401}, nil)
	before := cfgFile(t, dir)
	stdout, stderr, err := runLogin(t, "", "--tailnet=new.example.com", "--api-key="+keyN)
	if err == nil || !strings.Contains(err.Error(), "failed to validate API key") {
		t.Fatalf("err = %v", err)
	}
	if !bytes.Equal(before, cfgFile(t, dir)) {
		t.Fatal("config changed after a rejected key")
	}
	if !loadCfg(t).Accounts[0].Active {
		t.Fatal("active account must not change")
	}
	noSecret(t, []string{keyN}, stdout, stderr, errText(err))
}

// Verifies: REQ-LGN-005
func TestLGNLoginRejectsBadInputBeforeAnyRequest(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"tailnet flag missing", []string{"--api-key=" + keyN}, "tailnet"},
		{"no key", []string{"--tailnet=x.example.com"}, "API key not provided"},
		{"blank tailnet", []string{"--tailnet=\x01", "--api-key=" + keyN}, "tailnet name is required"},
		{"invalid tailnet", []string{"--tailnet=nodots", "--api-key=" + keyN}, "invalid tailnet"},
		{"invalid key", []string{"--tailnet=x.example.com", "--api-key=tskey-short"}, "invalid API key"},
		{"invalid name", []string{"--tailnet=x.example.com", "--name=a/b", "--api-key=" + keyN}, "invalid name"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := isolate(t)
			api := startAPI(t, map[string]int{keyN: 200}, nil)
			stdout, stderr, err := runLogin(t, "", c.args...)
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("err = %v, want %q", err, c.wantErr)
			}
			if api.hitCount() != 0 {
				t.Fatal("request sent for invalid input")
			}
			if _, e := os.Stat(filepath.Join(dir, "config.json")); e == nil {
				t.Fatal("config created")
			}
			noSecret(t, []string{keyN, "tskey-short"}, stdout, stderr, errText(err))
		})
	}
}

// Verifies: REQ-LGN-006
func TestLGNLoginFailsWhenConfigCannotBeStored(t *testing.T) {
	dir := isolate(t)
	startAPI(t, map[string]int{keyN: 200}, nil)
	bad := corruptConfig(t, dir)
	stdout, stderr, err := runLogin(t, "", "--tailnet=x.example.com", "--api-key="+keyN)
	if err == nil || !strings.Contains(err.Error(), "failed to store config") {
		t.Fatalf("err = %v", err)
	}
	if !bytes.Equal(bad, cfgFile(t, dir)) {
		t.Fatal("corrupt config replaced")
	}
	noSecret(t, []string{keyN}, stdout, stderr, errText(err))
	if _, err := config.Load(); err == nil {
		t.Fatal("config should still be corrupt")
	}
}

// Verifies: REQ-LGN-007
func TestLGNLoginHelpWarnsAboutKeyFlag(t *testing.T) {
	cmd := NewLoginCommand()
	if cmd.Use != "login" || !strings.Contains(cmd.Long, "visible in shell history") {
		t.Fatalf("Use/Long: %q / %q", cmd.Use, cmd.Long)
	}
	f := cmd.Flags().Lookup("api-key")
	if f == nil || !strings.Contains(f.Usage, "prefer --api-key-stdin") {
		t.Fatal("api-key flag usage must discourage the flag")
	}
	for _, name := range []string{"tailnet", "name", "api-key-stdin"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("flag %s missing", name)
		}
	}
}
