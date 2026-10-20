package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yatsenko-ihor/ts-cli/client"
)

func TestShortHost(t *testing.T) {
	for in, want := range map[string]string{
		"box-043.tail813b1c.ts.net": "box-043",
		"SkopasLinux":               "SkopasLinux",
		"a.example.com":             "a.example.com",
	} {
		if got := shortHost(in); got != want {
			t.Errorf("shortHost(%q)=%q want %q", in, got, want)
		}
	}
}

func TestHideOfflineAndSearchShowsAll(t *testing.T) {
	now := time.Now()
	devs := []client.Device{
		{ID: "a", Name: "alpha", LastSeen: now},
		{ID: "b", Name: "bravo", LastSeen: now.Add(-time.Hour)},
	}
	m := model{set: uiSettings{hideOffline: true}, list: deviceList{devices: devs}}
	m.filterDevices()
	if len(m.list.filteredDevices) != 1 || m.list.hiddenOffline != 1 {
		t.Fatalf("offline must be hidden: %d shown, %d hidden", len(m.list.filteredDevices), m.list.hiddenOffline)
	}
	m.list.searchQuery = "bravo"
	m.filterDevices()
	if len(m.list.filteredDevices) != 1 || m.list.filteredDevices[0].ID != "b" {
		t.Fatal("search must find hidden offline machines")
	}
}

func TestF12SettingsToggle(t *testing.T) {
	t.Setenv("TS_CLI_CONFIG_DIR", t.TempDir())
	m := model{set: loadUISettings(nil), list: deviceList{}}
	updated, _ := m.handleNormalMode(tea.KeyMsg{Type: tea.KeyF12})
	m = updated.(model)
	if !m.settingsMode || !strings.Contains(m.View(), "Settings") {
		t.Fatal("F12 must open the settings window")
	}
	before := m.set.hideOffline
	updated, cmd := m.handleSettings(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(model)
	if cmd == nil {
		t.Fatal("toggling must persist the setting")
	}
	_ = before
	updated, _ = m.handleSettings(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.(model).settingsMode {
		t.Fatal("esc closes settings")
	}
}
