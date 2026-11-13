package commands

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yatsenko-ihor/ts-cli/client"
	"github.com/yatsenko-ihor/ts-cli/config"
	"github.com/yatsenko-ihor/ts-cli/util"
)

type doctorEnv struct {
	missing map[string]bool
}

func doctorSetup(t *testing.T) *doctorEnv {
	t.Helper()
	t.Setenv(util.ConfigDirEnv, t.TempDir())
	env := &doctorEnv{missing: map[string]bool{}}
	oldLook, oldStatus, oldCheck, oldDecrypt := lookPathFn, tailscaleStatusFn, checkAccountFn, decryptSecretFn
	lookPathFn = func(name string) (string, error) {
		if env.missing[name] {
			return "", errors.New("not found")
		}
		return "/usr/bin/" + name, nil
	}
	tailscaleStatusFn = func() (bool, string) { return true, "" }
	checkAccountFn = func(client.AccountInfo) client.Health {
		return client.Health{Valid: true, HasExpiry: true, Expires: time.Now().Add(90 * 24 * time.Hour)}
	}
	decryptSecretFn = func(string) (string, error) { return "pw", nil }
	t.Cleanup(func() {
		lookPathFn, tailscaleStatusFn, checkAccountFn, decryptSecretFn = oldLook, oldStatus, oldCheck, oldDecrypt
	})
	return env
}

func doctorAccounts(t *testing.T, names ...string) {
	t.Helper()
	err := config.Update(func(cfg *config.Config) error {
		for _, n := range names {
			cfg.Accounts = append(cfg.Accounts, config.Account{Name: n, Tailnet: n + ".example.com", APIKey: "tskey-api-k" + n + "CNTRL-0123456789abcdef0123456789"})
		}
		cfg.SetMachineCredential("m1", config.MachineAuth{Username: "pi", PasswordEncrypted: "enc"})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Verifies: REQ-DOC-001, REQ-DOC-002
func TestDoctorToolsAndDaemon(t *testing.T) {
	env := doctorSetup(t)
	for _, c := range toolChecks() {
		if c.level != checkOK || !strings.HasPrefix(c.detail, "/usr/bin/") {
			t.Fatalf("%+v", c)
		}
	}
	env.missing["ssh"], env.missing["sshpass"], env.missing["tailscale"] = true, true, true
	got := map[string]checkLevel{}
	for _, c := range toolChecks() {
		got[c.name] = c.level
	}
	if got["ssh"] != checkFail || got["scp"] != checkOK || got["sshpass"] != checkWarn || got["tailscale"] != checkWarn {
		t.Fatalf("levels = %v", got)
	}
	if c := tailscaleCheck(); c.level != checkWarn || !strings.Contains(c.detail, "not checked") {
		t.Fatalf("no CLI: %+v", c)
	}
	delete(env.missing, "tailscale")
	if c := tailscaleCheck(); c.level != checkOK {
		t.Fatalf("running: %+v", c)
	}
	tailscaleStatusFn = func() (bool, string) {
		return false, "Tailscale daemon is running but you're logged out.\nRun: tailscale up"
	}
	if c := tailscaleCheck(); c.level != checkFail || strings.Contains(c.detail, "\n") || !strings.Contains(c.detail, "logged out") {
		t.Fatalf("not running: %+v", c)
	}
}

// Verifies: REQ-DOC-003
func TestDoctorConfigChecks(t *testing.T) {
	doctorSetup(t)
	checks, cfg := configChecks()
	if cfg == nil || len(checks) != 3 || checks[0].level != checkOK || checks[2].name != "accounts" || checks[2].level != checkWarn {
		t.Fatalf("empty config: %+v", checks)
	}
	doctorAccounts(t, "work")
	checks, cfg = configChecks()
	if cfg == nil || len(checks) != 2 || !strings.Contains(checks[0].detail, "1 account(s), 1 machine login(s)") {
		t.Fatalf("%+v", checks)
	}
	decryptSecretFn = func(string) (string, error) { return "", errors.New("wrong key") }
	checks, _ = configChecks()
	if len(checks) != 3 || checks[2].name != "passwords" || checks[2].level != checkWarn || !strings.Contains(checks[2].detail, "1 saved password") {
		t.Fatalf("%+v", checks)
	}
	// a machine without a password is not counted
	config.Update(func(c *config.Config) error {
		c.SetMachineCredential("m1", config.MachineAuth{Username: "pi"})
		return nil
	})
	if checks, _ = configChecks(); len(checks) != 2 {
		t.Fatalf("%+v", checks)
	}

	dir := os.Getenv(util.ConfigDirEnv)
	os.WriteFile(filepath.Join(dir, "config.json"), []byte("{broken"), 0o600)
	checks, cfg = configChecks()
	if cfg != nil || len(checks) != 1 || checks[0].level != checkFail || !strings.Contains(checks[0].detail, "cannot be read") {
		t.Fatalf("broken: %+v", checks)
	}
}

// Verifies: REQ-DOC-004
func TestDoctorAccountChecks(t *testing.T) {
	doctorSetup(t)
	now := time.Now()
	cases := []struct {
		name  string
		h     client.Health
		level checkLevel
		text  string
	}{
		{"rejected", client.Health{Unauth: true, Err: errors.New("invalid API key")}, checkFail, "rejected"},
		{"unreachable", client.Health{Err: errors.New("network down")}, checkWarn, "could not be checked"},
		{"expired", client.Health{Valid: true, HasExpiry: true, Expires: now.Add(-24 * time.Hour)}, checkFail, "expired on"},
		{"soon", client.Health{Valid: true, HasExpiry: true, Expires: now.Add(5*24*time.Hour + time.Hour)}, checkWarn, "expires in 5 day"},
		{"fine", client.Health{Valid: true, HasExpiry: true, Expires: now.Add(60*24*time.Hour + time.Hour)}, checkOK, "expires in 60 days"},
		{"no expiry note", client.Health{Valid: true, ExpiryNote: "unknown key format"}, checkOK, "unknown key format"},
		{"no expiry at all", client.Health{Valid: true}, checkOK, "expiry unknown"},
	}
	for _, c := range cases {
		h := c.h
		checkAccountFn = func(client.AccountInfo) client.Health { return h }
		got := accountChecks(&config.Config{Accounts: []config.Account{{Name: "work", Tailnet: "w.example.com", APIKey: "k"}}}, now)
		if len(got) != 1 || got[0].level != c.level || !strings.Contains(got[0].detail, c.text) || got[0].name != "account work" {
			t.Errorf("%s: %+v", c.name, got)
		}
	}
}

// Verifies: REQ-DOC-004, REQ-DOC-005
func TestDoctorCommand(t *testing.T) {
	env := doctorSetup(t)
	doctorAccounts(t, "work", "home")
	stdout, _, err := d178Run(t, NewDoctorCommand(), "")
	if err != nil {
		t.Fatalf("all fine: %v\n%s", err, stdout)
	}
	for _, want := range []string{"ts-cli doctor (v", "Tools", "Configuration", "Accounts", "account work", "account home", "0 problem(s), 0 warning(s)"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("report lacks %q:\n%s", want, stdout)
		}
	}

	// --offline asks no one
	asked := 0
	checkAccountFn = func(client.AccountInfo) client.Health { asked++; return client.Health{Valid: true} }
	stdout, _, err = d178Run(t, NewDoctorCommand(), "", "--offline")
	if err != nil || asked != 0 || !strings.Contains(stdout, "not checked (--offline)") || !strings.Contains(stdout, "1 warning(s)") {
		t.Fatalf("offline: %v asked=%d\n%s", err, asked, stdout)
	}

	// a failure makes the exit status non-zero
	env.missing["ssh"] = true
	stdout, _, err = d178Run(t, NewDoctorCommand(), "", "--offline")
	if err == nil || !strings.Contains(err.Error(), "1 problem(s) found") || !strings.Contains(stdout, "✗ ssh") {
		t.Fatalf("failure: %v\n%s", err, stdout)
	}
	if _, _, err := d178Run(t, NewDoctorCommand(), "", "extra"); err == nil {
		t.Fatal("no arguments")
	}

	// no accounts: no Accounts group; a broken config stops at the configuration group
	t.Setenv(util.ConfigDirEnv, t.TempDir())
	delete(env.missing, "ssh")
	stdout, _, err = d178Run(t, NewDoctorCommand(), "")
	if err != nil || strings.Contains(stdout, "Accounts") {
		t.Fatalf("no accounts: %v\n%s", err, stdout)
	}
	dir := t.TempDir()
	t.Setenv(util.ConfigDirEnv, dir)
	os.WriteFile(filepath.Join(dir, "config.json"), []byte("{broken"), 0o600)
	stdout, _, err = d178Run(t, NewDoctorCommand(), "")
	if err == nil || strings.Contains(stdout, "Accounts") || !strings.Contains(stdout, "cannot be read") {
		t.Fatalf("broken config: %v\n%s", err, stdout)
	}
}
