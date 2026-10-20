package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yatsenko-ihor/ts-cli/client"
	"github.com/yatsenko-ihor/ts-cli/config"
	"github.com/yatsenko-ihor/ts-cli/util"
)

func accountsModel(t *testing.T) model {
	t.Helper()
	t.Setenv(util.ConfigDirEnv, t.TempDir())
	if err := config.Update(func(c *config.Config) error {
		c.AddAccount("home", "tskey-api-kAAA-0123456789abcdef", "home.com", true)
		c.AddAccount("work", "tskey-api-kBBB-0123456789abcdef", "work.com", false)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load()
	now := time.Now()
	m := model{
		set:  loadUISettings(nil),
		acct: accounts{list: accountsFromConfig(cfg), accountManageMode: true},
		list: deviceList{devices: []client.Device{
			{ID: "1", Name: "a", AccountName: "home", LastSeen: now},
			{ID: "2", Name: "b", AccountName: "work", LastSeen: now},
		}},
	}
	m.filterDevices()
	return m
}

func key(s string) tea.KeyMsg {
	if s == " " {
		return tea.KeyMsg{Type: tea.KeySpace}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func runCmd(t *testing.T, m model, cmd tea.Cmd) model {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a command")
	}
	updated, _ := m.Update(cmd())
	return updated.(model)
}

func TestHideAccountRemovesItsMachinesAndPersists(t *testing.T) {
	m := accountsModel(t)
	updated, _ := m.handleAccountManagement(key("j"))
	m = updated.(model)
	updated, cmd := m.handleAccountManagement(key(" "))
	m = runCmd(t, updated.(model), cmd)
	if !m.acct.list[1].Hidden || len(m.list.filteredDevices) != 1 || m.list.filteredDevices[0].AccountName != "home" {
		t.Fatalf("hidden account's machines must disappear: %+v", m.list.filteredDevices)
	}
	if cfg, _ := config.Load(); !cfg.Accounts[1].Hidden {
		t.Fatal("hidden flag must be saved")
	}
}

func TestRemoveAccountNeedsConfirmation(t *testing.T) {
	m := accountsModel(t)
	updated, cmd := m.handleAccountManagement(key("d"))
	m = updated.(model)
	if cmd != nil || m.acct.confirmDelete != "home" || !strings.Contains(m.renderAccountManagement(), "Remove account") {
		t.Fatal("d must ask first")
	}
	updated, cmd = m.handleAccountManagement(key("n"))
	if cmd != nil || len(accountsOnDisk(t)) != 2 {
		t.Fatal("anything but y cancels")
	}
	m = updated.(model)
	updated, _ = m.handleAccountManagement(key("d"))
	updated, cmd = updated.(model).handleAccountManagement(key("y"))
	m = runCmd(t, updated.(model), cmd)
	if len(m.acct.list) != 1 || m.acct.list[0].Name != "work" || !m.acct.list[0].Active {
		t.Fatalf("accounts after remove = %+v", m.acct.list)
	}
	if m.acct.manageCursor != 0 {
		t.Fatal("cursor must stay in range")
	}
}

func TestEditPromptChain(t *testing.T) {
	m := accountsModel(t)
	updated, _ := m.handleAccountManagement(key("e"))
	m = updated.(model)
	if m.input.mode != inputText || m.input.value != "home" || m.acct.accountManageMode {
		t.Fatal("edit opens the name prompt, prefilled")
	}
	m.input.value = "house"
	updated, _ = m.input.onSubmit(m, "house")
	m = updated.(model)
	if !m.input.masked {
		t.Fatal("API key prompt must be masked")
	}
	updated, cmd := m.input.onSubmit(m, "")
	m = runCmd(t, updated.(model), cmd)
	if m.acct.list[0].Name != "house" {
		t.Fatalf("rename not applied: %+v", m.acct.list)
	}
	if m.acct.statusErr {
		t.Fatalf("unexpected error: %s", m.acct.status)
	}
}

func TestCancelledPromptReturnsToAccountsWindow(t *testing.T) {
	m := accountsModel(t)
	updated, _ := m.handleAccountManagement(key("a"))
	m = updated.(model)
	updated, _ = m.handleTextPrompt(tea.KeyMsg{Type: tea.KeyEsc})
	if !updated.(model).acct.accountManageMode || updated.(model).input.mode != inputNone {
		t.Fatal("esc must return to the accounts window")
	}
}

func TestAddAccountRejectsBadTailnet(t *testing.T) {
	m := accountsModel(t)
	updated, _ := m.handleAccountManagement(key("a"))
	m = updated.(model)
	updated, cmd := m.input.onSubmit(m, "nodots")
	m = updated.(model)
	if cmd != nil || !m.acct.statusErr || m.input.mode != inputNone {
		t.Fatal("invalid tailnet must be reported in the window")
	}
}

func TestHealthText(t *testing.T) {
	a := accounts{health: map[string]accountHealth{
		"ok":      {checked: true, h: client.Health{Valid: true, HasExpiry: true, Expires: time.Now().Add(60 * 24 * time.Hour)}},
		"soon":    {checked: true, h: client.Health{Valid: true, HasExpiry: true, Expires: time.Now().Add(5 * 24 * time.Hour)}},
		"urgent":  {checked: true, h: client.Health{Valid: true, HasExpiry: true, Expires: time.Now().Add(36 * time.Hour)}},
		"expired": {checked: true, h: client.Health{Valid: true, HasExpiry: true, Expires: time.Now().Add(-time.Hour)}},
		"bad":     {checked: true, h: client.Health{Unauth: true, Err: errors.New("x")}},
	}}
	for name, want := range map[string]int{"ok": 0, "soon": 1, "urgent": 2, "expired": 2, "bad": 2, "pending": 0} {
		if _, level := a.healthText(name); level != want {
			t.Errorf("%s: level %d, want %d", name, level, want)
		}
	}
	if text, _ := a.healthText("expired"); text != "key expired" {
		t.Errorf("expired text = %q", text)
	}
}

func TestKeyWarningOnlyForTroubledKeys(t *testing.T) {
	m := accountsModel(t)
	if m.keyWarning() != "" {
		t.Fatal("unchecked keys must not warn")
	}
	m.acct.health = map[string]accountHealth{
		"home": {checked: true, h: client.Health{Valid: true, HasExpiry: true, Expires: time.Now().Add(48 * time.Hour)}},
		"work": {checked: true, h: client.Health{Valid: true}},
	}
	if w := m.keyWarning(); !strings.Contains(w, "home") || strings.Contains(w, "work") {
		t.Fatalf("warning = %q", w)
	}
}

func TestSameTailnet(t *testing.T) {
	if !sameTailnet("user@example.com", "user@") || !sameTailnet("Work.com", "work.com") || sameTailnet("a.com", "b.com") {
		t.Fatal("sameTailnet")
	}
}

func accountsOnDisk(t *testing.T) []config.Account {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Accounts
}
