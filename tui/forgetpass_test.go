package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yatsenko-ihor/ts-cli/config"
)

func TestDForgetsTheSavedPasswordOfTheCursorMachine(t *testing.T) {
	d := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")}
	m := flowModel(t, map[string]machineCredential{
		"dev-1": {name: "laptop.example.com", username: "pi", passwordEncrypted: "enc"},
	}, "pi")

	updated, cmd := m.Update(d)
	m = asModel(t, updated)
	cred := m.ssh.machineCreds["dev-1"]
	if cred.passwordEncrypted != "" || cred.username != "pi" {
		t.Fatalf("password must go, username stay: %+v", cred)
	}
	if !strings.Contains(m.notify.info, "deleted") || cmd == nil {
		t.Fatalf("info = %q, cmd nil = %v", m.notify.info, cmd == nil)
	}
	cmd() // writes config.json
	cfg, err := config.Load()
	if err != nil || cfg.MachineCredentials["dev-1"].PasswordEncrypted != "" {
		t.Fatalf("config not updated: %v %+v", err, cfg)
	}

	// Nothing saved any more: say so, change nothing.
	updated, cmd = m.Update(d)
	m = asModel(t, updated)
	if !strings.Contains(m.notify.info, "No saved password") || cmd != nil {
		t.Fatalf("info = %q", m.notify.info)
	}

	// Empty list: nothing happens.
	m.list.filteredDevices = nil
	if _, cmd = m.deleteSavedPassword(); cmd != nil {
		t.Fatal("no machine, no command")
	}
}
