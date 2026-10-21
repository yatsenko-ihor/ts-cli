package commands

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yatsenko-ihor/ts-cli/client"
	"github.com/yatsenko-ihor/ts-cli/config"
)

// Requirements-based tests for interactive.go (verification/requirements/interactive.md).
// The Bubble Tea program never runs for real: runProgram is replaced.

type d178IntEnv struct {
	api    *d178API
	models []tea.Model
	runErr error
	bin    string
}

// d178NewIntEnv: fake API, recording runProgram, private PATH (no tailscale: not running).
func d178NewIntEnv(t *testing.T, cfg func(c *config.Config)) *d178IntEnv {
	t.Helper()
	d178SaveConfig(t, cfg)
	e := &d178IntEnv{api: d178NewAPI(t), bin: t.TempDir()}
	t.Setenv("PATH", e.bin)
	d178SetGoos(t, "windows") // "cannot start automatically": no waiting, nothing executed
	oldRun, oldNotice := runProgram, keyStoreNotice
	runProgram = func(m tea.Model) error { e.models = append(e.models, m); return e.runErr }
	keyStoreNotice = func() string { return "" }
	t.Cleanup(func() { runProgram, keyStoreNotice = oldRun, oldNotice })
	return e
}

func d178TwoAccounts(c *config.Config) {
	c.AddAccount("one", d178KeyA, "one.example.com", true)
	c.AddAccount("two", d178KeyB, "two.example.com", false)
	_ = c.SetAccountHidden("two", true)
	c.SSHUsername = "defuser"
}

// Verifies: REQ-INT-001
func TestDO178CInteractiveConfigProblems(t *testing.T) {
	t.Run("no accounts", func(t *testing.T) {
		e := d178NewIntEnv(t, nil)
		_, _, err := d178Run(t, NewInteractiveCommand(), "")
		if err == nil || !strings.Contains(err.Error(), "no accounts configured") || !strings.Contains(err.Error(), "ts-cli login") || len(e.models) != 0 {
			t.Fatalf("err = %v", err)
		}
		_, _, err = d178Run(t, NewInteractiveCommand(), "", "--api-key", d178KeyA, "--tailnet", "x.com")
		if err == nil || !strings.Contains(err.Error(), "no accounts configured") || len(e.models) != 0 {
			t.Fatalf("explicit credentials do not replace the account check: %v", err)
		}
	})
	t.Run("unreadable configuration", func(t *testing.T) {
		e := d178NewIntEnv(t, nil)
		d178WriteCorruptConfig(t)
		_, _, err := d178Run(t, NewInteractiveCommand(), "")
		if err == nil || !strings.Contains(err.Error(), "failed to load configuration") || len(e.models) != 0 {
			t.Fatalf("err = %v", err)
		}
	})
}

// Verifies: REQ-INT-002
func TestDO178CInteractiveKeyStoreNotice(t *testing.T) {
	e := d178NewIntEnv(t, d178OneAccount)
	e.api.devices["work.example.com"] = []client.Device{d178Dev("n1", "box.ts.net", "box", "100.64.0.1")}

	_, stderr, err := d178Run(t, NewInteractiveCommand(), "")
	if err != nil || strings.Contains(stderr, "Warning: keychain") {
		t.Fatalf("err=%v stderr=%q", err, stderr)
	}
	keyStoreNotice = func() string { return "keychain unavailable; using the key file" }
	_, stderr, err = d178Run(t, NewInteractiveCommand(), "")
	if err != nil || !strings.Contains(stderr, "Warning: keychain unavailable; using the key file\n") {
		t.Fatalf("err=%v stderr=%q", err, stderr)
	}
	if len(e.models) != 2 {
		t.Fatal("the notice must not stop the program")
	}
}

// Verifies: REQ-INT-003
func TestDO178CInteractiveExplicitCredentials(t *testing.T) {
	e := d178NewIntEnv(t, d178TwoAccounts)
	e.api.devices["one.example.com"] = []client.Device{d178Live("o1", "stored", "100.64.0.1")}
	e.api.devices["other.example.com"] = []client.Device{d178Live("x1", "explicit", "100.64.0.9")}

	out, _, err := d178Run(t, NewInteractiveCommand(), "", "--api-key", d178KeyB, "--tailnet", "other.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(e.models) != 1 {
		t.Fatalf("program runs = %d", len(e.models))
	}
	if v := e.models[0].View(); !strings.Contains(v, "explicit") || strings.Contains(v, "stored") {
		t.Fatalf("model must hold only the explicit tailnet's devices:\n%s", v)
	}
	if got := e.api.seen("other.example.com"); len(got) != 1 || got[0] != d178KeyB {
		t.Fatalf("tokens = %q", got)
	}
	if len(e.api.seen("one.example.com")) != 0 || strings.Contains(out, "Fetching devices from all") {
		t.Fatal("stored accounts must not be used")
	}

	// no devices: message, no program
	e.models = nil
	e.api.devices["other.example.com"] = nil
	out, _, err = d178Run(t, NewInteractiveCommand(), "", "--api-key", d178KeyB, "--tailnet", "other.example.com")
	if err != nil || !strings.Contains(out, "No devices found in the specified tailnet.") || len(e.models) != 0 {
		t.Fatalf("err=%v out=%q", err, out)
	}
	// API error
	e.api.status["other.example.com"] = 401
	_, _, err = d178Run(t, NewInteractiveCommand(), "", "--api-key", d178KeyB, "--tailnet", "other.example.com")
	if err == nil || !strings.Contains(err.Error(), "failed to list devices") || len(e.models) != 0 {
		t.Fatalf("err = %v", err)
	}
}

// Verifies: REQ-INT-004
func TestDO178CInteractiveStoredAccounts(t *testing.T) {
	e := d178NewIntEnv(t, d178TwoAccounts)
	e.api.devices["one.example.com"] = []client.Device{d178Live("o1", "alpha", "100.64.0.1")}
	e.api.devices["two.example.com"] = []client.Device{d178Live("t1", "beta", "100.64.0.2")}

	out, _, err := d178Run(t, NewInteractiveCommand(), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"Fetching devices from all configured accounts...", "Found 2 device(s) from 2 account(s)", "Tailscale doesn't appear to be running"} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in\n%s", w, out)
		}
	}
	if len(e.models) != 1 {
		t.Fatalf("program runs = %d", len(e.models))
	}
	v := e.models[0].View()
	// both accounts were fetched (2 online) and the Hidden flag of account "two" reached the model
	// (its device is counted but not listed); the default user comes from the configuration.
	if !strings.Contains(v, "alpha") || strings.Contains(v, "beta") || !strings.Contains(v, "Machines ● 2") || !strings.Contains(v, "Default username: defuser") {
		t.Fatalf("unexpected model view:\n%s", v)
	}

	// only one of the flags: ignored
	for _, args := range [][]string{{"--api-key", d178KeyB}, {"--tailnet", "other.example.com"}} {
		e.models = nil
		out, _, err = d178Run(t, NewInteractiveCommand(), "", args...)
		if err != nil || !strings.Contains(out, "Fetching devices from all configured accounts...") || len(e.models) != 1 {
			t.Fatalf("%q: err=%v out=%q", args, err, out)
		}
	}

	// no devices anywhere: message, no program
	e.models = nil
	e.api.devices = map[string][]client.Device{}
	out, _, err = d178Run(t, NewInteractiveCommand(), "")
	if err != nil || !strings.Contains(out, "No devices found in any of your configured accounts.") || len(e.models) != 0 {
		t.Fatalf("err=%v out=%q", err, out)
	}
}

// Verifies: REQ-INT-005
func TestDO178CInteractiveRunsTheProgram(t *testing.T) {
	e := d178NewIntEnv(t, d178OneAccount)
	e.api.devices["work.example.com"] = []client.Device{d178Dev("n1", "box.ts.net", "box", "100.64.0.1")}

	if _, _, err := d178Run(t, NewInteractiveCommand(), ""); err != nil || len(e.models) != 1 {
		t.Fatalf("err=%v runs=%d", err, len(e.models))
	}
	e.runErr = errors.New("terminal exploded")
	_, _, err := d178Run(t, NewInteractiveCommand(), "")
	if err == nil || err.Error() != "TUI error: terminal exploded" {
		t.Fatalf("err = %v", err)
	}
}

// Verifies: REQ-INT-006
func TestDO178CRunProgramDefaultRunsAndReturnsTheProgramError(t *testing.T) {
	// the default seam: a model that quits at once lets the real program start and stop without a terminal
	old := programOptions
	programOptions = append([]tea.ProgramOption{tea.WithInput(nil), tea.WithOutput(io.Discard)}, old...)
	t.Cleanup(func() { programOptions = old })
	var ok quitModel
	var err error
	d178Capture(t, func() { err = d178DefaultRunProgram(ok) })
	if err != nil {
		t.Fatalf("a model that quits immediately must return nil: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	programOptions = append(programOptions, tea.WithContext(ctx))
	d178Capture(t, func() { err = d178DefaultRunProgram(waitModel{}) })
	if err == nil {
		t.Fatal("a program failure must be returned")
	}
}

// Verifies: REQ-INT-007
func TestDO178CInteractiveCommandMetadata(t *testing.T) {
	cmd := NewInteractiveCommand()
	if cmd.Use != "interactive" || cmd.RunE == nil {
		t.Fatal("not wired")
	}
	if got := strings.Join(cmd.Aliases, ","); got != "i,tui" {
		t.Fatalf("aliases = %q", got)
	}
	for _, n := range []string{"api-key", "tailnet"} {
		if f := cmd.Flags().Lookup(n); f == nil || f.DefValue != "" {
			t.Errorf("flag %s: %+v", n, f)
		}
	}
}

// d178Live is an online device (shown by the TUI without scrolling or filtering).
func d178Live(id, host, addr string) client.Device {
	d := d178Dev(id, host+".ts.net", host, addr)
	d.ConnectedToControl = true
	return d
}

type quitModel struct{}

func (quitModel) Init() tea.Cmd                         { return tea.Quit }
func (m quitModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }
func (quitModel) View() string                          { return "" }

// waitModel never quits by itself.
type waitModel struct{ quitModel }

func (waitModel) Init() tea.Cmd { return nil }
