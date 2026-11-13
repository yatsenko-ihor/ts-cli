package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yatsenko-ihor/ts-cli/client"
	"github.com/yatsenko-ihor/ts-cli/config"
)

func personalModel(t *testing.T) model {
	t.Helper()
	t.Setenv("TS_CLI_CONFIG_DIR", t.TempDir())
	devs := []client.Device{
		{ID: "a", Name: "alpha.tail1.ts.net", Hostname: "alpha", Addresses: []string{"100.64.0.1"}, OS: "linux", ConnectedToControl: true, Tags: []string{"tag:server"}},
		{ID: "b", Name: "beta.tail1.ts.net", Hostname: "beta", Addresses: []string{"100.64.0.2"}, OS: "linux", ConnectedToControl: true},
		{ID: "c", Name: "gamma.tail1.ts.net", Hostname: "gamma", OS: "macOS", LastSeen: time.Now().Add(-time.Hour)},
	}
	m := NewModel(devs, "t", "", nil, nil, nil, "", nil, Personal{Pinned: []string{"b"}, Notes: map[string]string{"a": "the NAS"}})
	m.set.hideOffline = false // keep the offline machine in the list
	m.filterDevices()
	return m
}

func TestPinnedMachinesComeFirstAndShowAStar(t *testing.T) {
	m := personalModel(t)
	if machineKey(m.list.filteredDevices[0]) != "b" {
		t.Fatalf("first = %v", m.list.filteredDevices[0].Name)
	}
	cols := m.listColumns(false)
	if !strings.HasPrefix(cols.names["b"], pinMark) || strings.HasPrefix(cols.names["a"], pinMark) {
		t.Fatalf("names = %v", cols.names)
	}
	if !m.isPinned(m.list.filteredDevices[0]) {
		t.Fatal("isPinned")
	}

	// * on another machine pins it; the cursor follows it; * again unpins
	t.Setenv("TS_CLI_CONFIG_DIR", t.TempDir())
	m.list.cursor = 1
	u, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("*")})
	m = u.(model)
	if !m.pinned["a"] || cmd == nil || !strings.Contains(m.notify.info, "Pinned") {
		t.Fatalf("pinned = %v", m.pinned)
	}
	if msg := cmd().(personalSavedMsg); msg.err != nil {
		t.Fatal(msg.err)
	}
	cfg, _ := config.Load()
	if len(cfg.PinnedMachines) != 1 || cfg.PinnedMachines[0] != "a" {
		t.Fatalf("config pins = %v", cfg.PinnedMachines)
	}
	if d, _ := m.targetDevice(); machineKey(d) != "a" {
		t.Fatal("the cursor stays on the machine")
	}
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("*")})
	if u.(model).pinned["a"] || !strings.Contains(u.(model).notify.info, "Unpinned") {
		t.Fatal("unpin")
	}
	empty := model{}
	if _, c := empty.togglePin(); c != nil {
		t.Fatal("no machine")
	}
	// A moves the old "mark all" behaviour
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("A")})
	if len(u.(model).list.marked) == 0 {
		t.Fatal("A marks all")
	}
	// a failed save is reported
	u, _ = m.Update(personalSavedMsg{err: os.ErrPermission})
	if u.(model).notify.sshError == nil {
		t.Fatal("save error")
	}
	u, _ = m.Update(personalSavedMsg{})
	if u.(model).notify.sshError != nil {
		t.Fatal("no error")
	}
}

func TestNotes(t *testing.T) {
	m := personalModel(t)
	for i, d := range m.list.filteredDevices {
		if machineKey(d) == "a" {
			m.list.cursor = i
		}
	}
	if !strings.Contains(m.renderDetailsPanel(70, 0), "the NAS") {
		t.Fatal("the details show the note")
	}
	m.run.visible, m.run.tab, m.width, m.height = true, tabMachine, 140, 60
	if !strings.Contains(m.renderRunPanel(), "Note: the NAS") {
		t.Fatal("the Machine tab shows the note")
	}

	u, _ := m.promptNote()
	m = u.(model)
	if m.input.mode != inputText || m.input.value != "the NAS" {
		t.Fatal("prompt prefilled")
	}
	u, cmd := m.input.onSubmit(m, "  backups\nhere ")
	m = u.(model)
	if m.notes["a"] != "backups\nhere" || cmd == nil {
		t.Fatalf("notes = %v", m.notes)
	}
	if msg := cmd().(personalSavedMsg); msg.err != nil {
		t.Fatal(msg.err)
	}
	if cfg, _ := config.Load(); cfg.MachineNotes["a"] != "backups\nhere" {
		t.Fatalf("config notes = %v", cfg.MachineNotes)
	}
	u, _ = m.promptNote()
	u, _ = u.(model).input.onSubmit(u.(model), "   ")
	if _, ok := u.(model).notes["a"]; ok {
		t.Fatal("empty removes")
	}
	if _, c := (model{}).promptNote(); c != nil {
		t.Fatal("no machine")
	}
	found := false
	for _, a := range machineActions(m.list.devices[0]) {
		found = found || strings.HasPrefix(a.title, "Edit note")
	}
	if !found {
		t.Fatal("Machine tab row")
	}
}

func TestCopyAddressAndName(t *testing.T) {
	m := personalModel(t)
	if _, cmd := m.copyAddress(false); cmd == nil {
		t.Fatal("copies the address")
	}
	if _, cmd := m.copyAddress(true); cmd == nil {
		t.Fatal("copies the name")
	}
	m.list.cursor = len(m.list.filteredDevices) - 1 // gamma has no address
	u, cmd := m.copyAddress(false)
	if cmd != nil || u.(model).notify.sshError == nil {
		t.Fatal("no address")
	}
	if _, cmd := (model{}).copyAddress(false); cmd != nil {
		t.Fatal("no machine")
	}
	u, cmd = personalModel(t).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if cmd == nil {
		t.Fatal("y")
	}
	_, cmd = personalModel(t).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Y")})
	if cmd == nil {
		t.Fatal("Y")
	}
}

func TestExport(t *testing.T) {
	m := personalModel(t)
	rows := m.exportRows()
	if len(rows) != 3 || rows[0].Name != "beta.tail1.ts.net" || rows[1].Note != "the NAS" || rows[2].Address != "" || rows[2].Tags == nil {
		t.Fatalf("rows = %+v", rows)
	}
	csvOut, err := renderExport(append(rows, exportRow{Name: "=cmd|' /C calc'!A0", Note: "-1+2"}), "x.csv")
	if err != nil || !strings.HasPrefix(string(csvOut), "account,name,hostname,address,os,tags,online,last_seen,note\n") {
		t.Fatalf("%q %v", csvOut, err)
	}
	if !strings.Contains(string(csvOut), "'=cmd") || !strings.Contains(string(csvOut), "'-1+2") {
		t.Fatalf("formulas are neutralised:\n%s", csvOut)
	}
	jsonOut, err := renderExport(rows, "x.JSON")
	var back []exportRow
	if err != nil || json.Unmarshal(jsonOut, &back) != nil || len(back) != 3 || back[0].Online != true {
		t.Fatalf("%v %s", err, jsonOut)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	u, _ := m.promptExport()
	m = u.(model)
	if !strings.Contains(m.input.label, "Export 3 machines") || m.input.value != "~/ts-cli-machines.csv" {
		t.Fatalf("prompt %+v", m.input)
	}
	prompt := m
	u, _ = prompt.input.onSubmit(prompt, path)
	m = u.(model)
	st, err := os.Stat(path)
	if err != nil || st.Mode().Perm() != 0o600 || !strings.Contains(m.notify.info, "Exported 3 machines") {
		t.Fatalf("%v %v %q", st, err, m.notify.info)
	}
	// errors: a directory that does not exist, and an empty answer
	u, _ = prompt.input.onSubmit(prompt, filepath.Join(dir, "no", "such", "x.csv"))
	if u.(model).notify.sshError == nil {
		t.Fatal("write error")
	}
	u, _ = prompt.input.onSubmit(prompt, " ")
	if u.(model).notify.sshError != nil && strings.Contains(u.(model).notify.sshError.Error(), "export") {
		t.Fatal("empty path does nothing")
	}
	// rename onto a directory fails and leaves no temporary file
	os.Mkdir(filepath.Join(dir, "d"), 0o755)
	if err := writeExport(filepath.Join(dir, "d"), []byte("x")); err == nil {
		t.Fatal("rename over a directory")
	}
	if _, err := os.Stat(filepath.Join(dir, "d.tmp")); err == nil {
		t.Fatal("temporary file left behind")
	}
	if _, c := (model{}).promptExport(); c != nil {
		t.Fatal("nothing to export")
	}
	m.list.filteredDevices = nil
	if u, _ := m.promptExport(); u.(model).input.mode != inputNone {
		t.Fatal("empty list")
	}
}

func TestDefaultThemeFollowsTheTerminal(t *testing.T) {
	old := terminalIsDark
	t.Cleanup(func() { terminalIsDark = old })
	terminalIsDark = func() bool { return true }
	if got := loadUISettings(nil).theme; got != "dark" {
		t.Fatalf("dark terminal: %q", got)
	}
	terminalIsDark = func() bool { return false }
	if got := loadUISettings(nil).theme; got != "light" {
		t.Fatalf("light terminal: %q", got)
	}
	// a stored choice wins, and an unknown stored name still means light
	terminalIsDark = func() bool { return true }
	if got := loadUISettings(map[string]string{uiTheme: "hacker"}).theme; got != "hacker" {
		t.Fatalf("stored: %q", got)
	}
	if got := loadUISettings(map[string]string{uiTheme: "nope"}).theme; got != "light" {
		t.Fatalf("unknown: %q", got)
	}
}
