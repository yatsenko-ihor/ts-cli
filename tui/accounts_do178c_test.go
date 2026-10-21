package tui

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/yatsenko-ihor/ts-cli/client"
	"github.com/yatsenko-ihor/ts-cli/config"
	"github.com/yatsenko-ihor/ts-cli/util"
)

const (
	awnKeyHome = "tskey-api-kAAA-0123456789abcdef"
	awnKeyWork = "tskey-api-kBBB-0123456789abcdef"
	awnKeyNew  = "tskey-api-kNEW-0123456789abcdef"
)

// awnMsgs runs a command and every command of a batch it returns.
func awnMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, awnMsgs(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func awnChanged(t *testing.T, cmd tea.Cmd) accountsChangedMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a command")
	}
	msg, ok := cmd().(accountsChangedMsg)
	if !ok {
		t.Fatalf("expected accountsChangedMsg, got %T", cmd())
	}
	return msg
}

func awnModel(m tea.Model) model { return m.(model) }

// awnFakeTailscale puts a fake `tailscale` first (and only) on PATH. It logs
// its arguments, fails `switch` when switchExit is not 0 and answers
// `status --json` with a login of home.com.
func awnFakeTailscale(t *testing.T, switchExit string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("needs sh")
	}
	dir := t.TempDir()
	script := `#!/bin/sh
echo "$@" >> "$FAKE_TS_DIR/ts.log"
case "$1" in
switch) exit "$FAKE_TS_SWITCH_EXIT" ;;
status) if [ "$2" = "--json" ]; then echo '{"Self":{"UserID":1},"User":{"1":{"LoginName":"home.com"}}}'; else exit 1; fi ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "tailscale"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("FAKE_TS_DIR", dir)
	t.Setenv("FAKE_TS_SWITCH_EXIT", switchExit)
	return dir
}

func awnTSLog(t *testing.T, dir string) string {
	t.Helper()
	b, _ := os.ReadFile(filepath.Join(dir, "ts.log"))
	return string(b)
}

// Verifies: REQ-AWN-001, REQ-AWN-002
func TestDO178CSameTailnetAndThisComputer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"user@example.com", "user@", true}, // first condition
		{"user@", "user@example.com", true}, // second condition
		{"home.com", "work.com", false},     // neither
		{"Home.COM", "home.com", true},      // case-insensitive, equal
		{"example.com", "user@example.com", false},
		{"", "anything", true}, // derived: the empty string matches everything
		{"anything", "", true},
		{"日本.example", "日本.example", true},
		{"a", "ab", true},
	}
	for _, c := range cases {
		if got := sameTailnet(c.a, c.b); got != c.want {
			t.Errorf("sameTailnet(%q,%q) = %v want %v", c.a, c.b, got, c.want)
		}
	}

	acc := client.AccountInfo{Name: "home", Tailnet: "home.com"}
	m := model{}
	m.acct.tailscaleActiveAccount = "Home.com"
	if !m.isThisComputer(acc) {
		t.Error("same login must be this computer")
	}
	m.acct.tailscaleActiveAccount = ""
	if m.isThisComputer(acc) {
		t.Error("unknown login must not match, although sameTailnet matches the empty string")
	}
	m.acct.tailscaleActiveAccount = "other.com"
	if m.isThisComputer(acc) {
		t.Error("different login")
	}
	m.acct.tailscaleActiveAccount = "<not connected>"
	if m.isThisComputer(acc) {
		t.Error("<not connected> matches nothing")
	}
}

// Verifies: REQ-AWN-003
func TestDO178CAccountLookups(t *testing.T) {
	a := accounts{list: []client.AccountInfo{{Name: "home"}, {Name: "work", Hidden: true}}}
	if a.accountHidden("home") || !a.accountHidden("work") || a.accountHidden("nobody") || a.accountHidden("") {
		t.Error("accountHidden")
	}
	if !a.hasAccount("home") || !a.hasAccount("work") || a.hasAccount("nobody") || a.hasAccount("") {
		t.Error("hasAccount")
	}
	var empty accounts
	if empty.hasAccount("home") || empty.accountHidden("home") {
		t.Error("empty list")
	}
}

// Verifies: REQ-AWN-004
func TestDO178CCheckAllHealth(t *testing.T) {
	old := checkAccountHealth
	t.Cleanup(func() { checkAccountHealth = old })
	checkAccountHealth = func(acc client.AccountInfo) client.Health {
		return client.Health{Valid: true, ExpiryNote: "checked " + acc.Name}
	}
	if cmd := (model{}).checkAllHealth(); cmd != nil {
		t.Fatal("no accounts, no command")
	}
	m := model{acct: accounts{list: []client.AccountInfo{{Name: "a"}, {Name: "b"}, {Name: "日本"}}}}
	msgs := awnMsgs(m.checkAllHealth())
	if len(msgs) != 3 {
		t.Fatalf("%d messages", len(msgs))
	}
	got := map[string]string{}
	for _, msg := range msgs {
		hm := msg.(accountHealthMsg)
		got[hm.name] = hm.h.ExpiryNote
	}
	if got["a"] != "checked a" || got["b"] != "checked b" || got["日本"] != "checked 日本" {
		t.Fatalf("results %v", got)
	}
	// one account
	m.acct.list = m.acct.list[:1]
	if msgs := awnMsgs(m.checkAllHealth()); len(msgs) != 1 || msgs[0].(accountHealthMsg).name != "a" {
		t.Fatalf("one account: %v", msgs)
	}
}

// Verifies: REQ-AWN-005, REQ-AWN-006
func TestDO178CHealthText(t *testing.T) {
	if daysLeft(time.Now().Add(23*time.Hour)) != 0 || daysLeft(time.Now().Add(49*time.Hour)) != 2 || daysLeft(time.Now().Add(-time.Hour)) != 0 {
		t.Error("daysLeft")
	}
	day := 24 * time.Hour
	now := time.Now()
	valid := func(mod func(*client.Health)) accountHealth {
		h := client.Health{Valid: true}
		if mod != nil {
			mod(&h)
		}
		return accountHealth{checked: true, h: h}
	}
	expires := func(d time.Duration) accountHealth {
		return valid(func(h *client.Health) { h.HasExpiry, h.Expires = true, now.Add(d) })
	}
	a := accounts{health: map[string]accountHealth{
		"unchecked": {checked: false, h: client.Health{Valid: true}},
		"rejected":  {checked: true, h: client.Health{Unauth: true, Valid: true}},
		"down":      {checked: true, h: client.Health{Valid: false, Err: errors.New("net")}},
		"downexp":   {checked: true, h: client.Health{Valid: false, HasExpiry: true, Expires: now.Add(-day)}},
		"expired":   expires(-time.Minute),
		"just":      expires(-time.Nanosecond),
		"d2":        expires(3*day - time.Minute),
		"d3":        expires(3*day + time.Minute),
		"d13":       expires(14*day - time.Minute),
		"d14":       expires(14*day + time.Minute),
		"d90":       expires(90*day + time.Minute),
		"note":      valid(func(h *client.Health) { h.ExpiryNote = "expiry not readable" }),
		"plain":     valid(nil),
	}}
	cases := []struct {
		name  string
		text  string
		level int
	}{
		{"missing", "checking…", 0},
		{"unchecked", "checking…", 0},
		{"rejected", "key rejected", 2},
		{"down", "unreachable", 1},
		{"downexp", "unreachable", 1},
		{"expired", "key expired", 2},
		{"just", "key expired", 2},
		{"d2", "key expires in 2d", 2},
		{"d3", "key expires in 3d", 1},
		{"d13", "key expires in 13d", 1},
		{"d14", "key ok, 14d left", 0},
		{"d90", "key ok, 90d left", 0},
		{"note", "key ok", 0},
		{"plain", "key ok", 0},
	}
	for _, c := range cases {
		text, level := a.healthText(c.name)
		if text != c.text || level != c.level {
			t.Errorf("%s: (%q,%d) want (%q,%d)", c.name, text, level, c.text, c.level)
		}
	}
	// nil map
	if text, level := (accounts{}).healthText("x"); text != "checking…" || level != 0 {
		t.Error("nil health map")
	}
}

// Verifies: REQ-AWN-007
func TestDO178CKeyWarning(t *testing.T) {
	now := time.Now()
	m := model{acct: accounts{
		list: []client.AccountInfo{{Name: "a"}, {Name: "b"}, {Name: "c"}, {Name: "d"}, {Name: "e"}},
		health: map[string]accountHealth{
			"a": {checked: true, h: client.Health{Valid: true, HasExpiry: true, Expires: now.Add(-time.Hour)}},
			"b": {checked: true, h: client.Health{Valid: true}},
			"c": {checked: true, h: client.Health{Valid: false}},
			"d": {checked: true, h: client.Health{Valid: true, HasExpiry: true, Expires: now.Add(10*24*time.Hour + time.Hour)}},
			// "e" is still being checked
		},
	}}
	want := "⚠ API key: a: key expired; d: key expires in 10d  (m)"
	if got := m.keyWarning(); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	m.acct.list = []client.AccountInfo{{Name: "b"}, {Name: "c"}, {Name: "e"}}
	if got := m.keyWarning(); got != "" {
		t.Fatalf("healthy, unreachable and unchecked must not warn: %q", got)
	}
	m.acct.health["b"] = accountHealth{checked: true, h: client.Health{Unauth: true}}
	if got := m.keyWarning(); got != "⚠ API key: b: key rejected  (m)" {
		t.Fatalf("got %q", got)
	}
	if got := (model{}).keyWarning(); got != "" {
		t.Fatalf("no accounts: %q", got)
	}
}

// Verifies: REQ-AWN-008
func TestDO178CAccountsFromConfig(t *testing.T) {
	cfg := &config.Config{Accounts: []config.Account{
		{Name: "a", APIKey: "k1", Tailnet: "a.com", Active: true},
		{Name: "日本", APIKey: "k2", Tailnet: "b.com", Hidden: true},
	}}
	got := accountsFromConfig(cfg)
	want := []client.AccountInfo{
		{Name: "a", APIKey: "k1", Tailnet: "a.com", Active: true},
		{Name: "日本", APIKey: "k2", Tailnet: "b.com", Hidden: true},
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %+v", got)
	}
	if got := accountsFromConfig(&config.Config{}); got == nil || len(got) != 0 {
		t.Fatalf("empty: %#v", got)
	}
}

// Verifies: REQ-AWN-009, REQ-AWN-021
func TestDO178CAddAccountCmd(t *testing.T) {
	t.Setenv(util.ConfigDirEnv, t.TempDir())
	api := startFakeAPI(t)

	// the API rejects the key: nothing is stored
	api.code = 401
	msg := awnChanged(t, addAccountCmd("home.com", "home", awnKeyHome))
	if msg.err == nil || !strings.HasPrefix(msg.err.Error(), "API key check failed: ") || msg.reload || msg.notice != "" {
		t.Fatalf("rejected: %+v", msg)
	}
	if strings.Contains(msg.err.Error(), awnKeyHome) {
		t.Fatal("the API key leaked into the error")
	}
	if len(accountsOnDisk(t)) != 0 {
		t.Fatal("nothing may be stored for a rejected key")
	}
	if call, ok := api.last(); !ok || !strings.Contains(call.path, "home.com") {
		t.Fatalf("the key must be checked against the tailnet: %+v", call)
	}

	// network/server error is also a failure
	api.code = 500
	if msg := awnChanged(t, addAccountCmd("home.com", "home", awnKeyHome)); msg.err == nil || len(accountsOnDisk(t)) != 0 {
		t.Fatalf("server error: %+v", msg)
	}

	// valid: the first account becomes active, the second does not
	api.code = 200
	msg = awnChanged(t, addAccountCmd("home.com", "home", awnKeyHome))
	if msg.err != nil || msg.notice != "Added account home" || !msg.reload {
		t.Fatalf("first: %+v", msg)
	}
	msg = awnChanged(t, addAccountCmd("work.com", "work", awnKeyWork))
	if msg.err != nil || msg.notice != "Added account work" || !msg.reload {
		t.Fatalf("second: %+v", msg)
	}
	accs := accountsOnDisk(t)
	if len(accs) != 2 || !accs[0].Active || accs[1].Active || accs[1].APIKey != awnKeyWork || accs[1].Tailnet != "work.com" {
		t.Fatalf("stored: %+v", accs)
	}

	// the configuration cannot be written (config dir is a file)
	file := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(util.ConfigDirEnv, file)
	msg = awnChanged(t, addAccountCmd("x.com", "x", awnKeyNew))
	if msg.err == nil || msg.reload {
		t.Fatalf("unwritable config: %+v", msg)
	}
}

// Verifies: REQ-AWN-010
func TestDO178CEditAccountCmd(t *testing.T) {
	accountsModel(t)
	api := startFakeAPI(t)

	// rename only: the API is not contacted
	msg := awnChanged(t, editAccountCmd("home", "home.com", "homey", ""))
	if msg.err != nil || msg.notice != "Account updated" || !msg.reload {
		t.Fatalf("rename: %+v", msg)
	}
	if _, called := api.last(); called {
		t.Fatal("no key given: no API call")
	}
	if accountsOnDisk(t)[0].Name != "homey" {
		t.Fatal("rename not stored")
	}

	// key only: verified, stored, no reload
	msg = awnChanged(t, editAccountCmd("work", "work.com", "", awnKeyNew))
	if msg.err != nil || msg.reload || msg.notice != "Account updated" {
		t.Fatalf("key: %+v", msg)
	}
	if accountsOnDisk(t)[1].APIKey != awnKeyNew {
		t.Fatal("key not stored")
	}
	if call, ok := api.last(); !ok || !strings.Contains(call.path, "work.com") {
		t.Fatalf("key must be checked for the tailnet: %+v", call)
	}

	// rejected key: nothing stored
	api.code = 403
	msg = awnChanged(t, editAccountCmd("work", "work.com", "renamed", awnKeyHome))
	if msg.err == nil || !strings.HasPrefix(msg.err.Error(), "API key check failed: ") || msg.reload {
		t.Fatalf("rejected: %+v", msg)
	}
	if a := accountsOnDisk(t)[1]; a.Name != "work" || a.APIKey != awnKeyNew {
		t.Fatalf("rejected edit changed the config: %+v", a)
	}
	api.code = 200

	// name already taken, unknown account
	msg = awnChanged(t, editAccountCmd("work", "work.com", "homey", ""))
	if msg.err == nil || msg.reload {
		t.Fatalf("taken: %+v", msg)
	}
	msg = awnChanged(t, editAccountCmd("ghost", "ghost.com", "", ""))
	if msg.err == nil || msg.reload {
		t.Fatalf("unknown: %+v", msg)
	}
	if len(accountsOnDisk(t)) != 2 {
		t.Fatal("accounts changed")
	}
}

// Verifies: REQ-AWN-011, REQ-AWN-012
func TestDO178CRemoveAndHideAccountCmd(t *testing.T) {
	accountsModel(t)

	msg := awnChanged(t, removeAccountCmd("ghost"))
	if msg.err == nil || msg.err.Error() != "account 'ghost' not found" || msg.reload || len(accountsOnDisk(t)) != 2 {
		t.Fatalf("unknown removal: %+v", msg)
	}
	msg = awnChanged(t, hideAccountCmd("ghost", true))
	if msg.err == nil || msg.err.Error() != "account 'ghost' not found" || msg.reload {
		t.Fatalf("unknown hide: %+v", msg)
	}

	msg = awnChanged(t, hideAccountCmd("work", true))
	if msg.err != nil || msg.notice != "work is now hidden" || msg.reload || !accountsOnDisk(t)[1].Hidden {
		t.Fatalf("hide: %+v", msg)
	}
	msg = awnChanged(t, hideAccountCmd("work", false))
	if msg.err != nil || msg.notice != "work is now shown" || accountsOnDisk(t)[1].Hidden {
		t.Fatalf("show: %+v", msg)
	}

	// removing the active account makes the remaining one active (config invariant)
	msg = awnChanged(t, removeAccountCmd("home"))
	if msg.err != nil || msg.notice != "Removed account home" || !msg.reload {
		t.Fatalf("remove: %+v", msg)
	}
	if accs := accountsOnDisk(t); len(accs) != 1 || accs[0].Name != "work" || !accs[0].Active {
		t.Fatalf("after removal: %+v", accs)
	}
	awnChanged(t, removeAccountCmd("work"))
	if len(accountsOnDisk(t)) != 0 {
		t.Fatal("last account not removed")
	}
}

// Verifies: REQ-AWN-013
func TestDO178CUseAccountCmd(t *testing.T) {
	accountsModel(t)

	dir := awnFakeTailscale(t, "0")
	msg := awnChanged(t, useAccountCmd("work", "work.com"))
	if msg.err != nil || msg.notice != "This computer now uses work.com" || msg.reload {
		t.Fatalf("success: %+v", msg)
	}
	if got := awnTSLog(t, dir); got != "switch work.com\n" {
		t.Fatalf("tailscale invocation: %q", got)
	}
	if accs := accountsOnDisk(t); accs[0].Active || !accs[1].Active {
		t.Fatalf("active account: %+v", accs)
	}

	// the switch fails: the configuration change stays, the message is a notice
	awnFakeTailscale(t, "1")
	msg = awnChanged(t, useAccountCmd("home", "home.com"))
	if msg.err != nil || !strings.Contains(msg.notice, "Active account set, but this computer was not switched") {
		t.Fatalf("failed switch: %+v", msg)
	}
	if !accountsOnDisk(t)[0].Active {
		t.Fatal("config change must stay")
	}

	// unknown account: error, tailscale not run
	dir = awnFakeTailscale(t, "0")
	msg = awnChanged(t, useAccountCmd("ghost", "ghost.com"))
	if msg.err == nil || awnTSLog(t, dir) != "" {
		t.Fatalf("unknown: %+v log=%q", msg, awnTSLog(t, dir))
	}
}

// Verifies: REQ-AWN-014
func TestDO178CAccountsChangedError(t *testing.T) {
	m := accountsModel(t)
	m.acct.busy = true
	m.acct.manageCursor = 1
	m.acct.status, m.acct.statusErr = "old", false
	before := m.acct.list
	updated, cmd := m.handleAccountsChanged(accountsChangedMsg{err: errors.New("boom"), notice: "ignored", reload: true})
	got := awnModel(updated)
	if cmd != nil || got.acct.busy || got.acct.status != "boom" || !got.acct.statusErr || got.reloading {
		t.Fatalf("error handling: %+v", got.acct)
	}
	if len(got.acct.list) != len(before) || got.acct.manageCursor != 1 {
		t.Fatal("an error must change nothing else")
	}
}

// Verifies: REQ-AWN-015, REQ-AWN-016
func TestDO178CAccountsChangedReload(t *testing.T) {
	m := accountsModel(t)
	awnFakeTailscale(t, "0") // daemon login: home.com
	m.acct.busy = true
	m.acct.manageCursor = 1
	m.list.selectedProfile = "work"

	// work is removed on disk: cursor clamped, profile reset, list/active/login reloaded
	if err := config.Update(func(c *config.Config) error { c.RemoveAccount("work"); return nil }); err != nil {
		t.Fatal(err)
	}
	updated, _ := m.handleAccountsChanged(accountsChangedMsg{notice: "Removed account work"})
	got := awnModel(updated)
	if got.acct.busy || got.acct.status != "Removed account work" || got.acct.statusErr {
		t.Fatalf("status: %+v", got.acct)
	}
	if len(got.acct.list) != 1 || got.acct.activeAccount != "home" || got.acct.tailscaleActiveAccount != "home.com" {
		t.Fatalf("reloaded: %+v", got.acct)
	}
	if got.acct.manageCursor != 0 {
		t.Fatalf("cursor %d", got.acct.manageCursor)
	}
	if got.list.selectedProfile != "" {
		t.Fatal("profile of a removed account must be reset")
	}

	// the profile still exists: kept; cursor inside the list: kept
	m = accountsModel(t)
	m.list.selectedProfile = "home"
	m.acct.manageCursor = 1
	updated, _ = m.handleAccountsChanged(accountsChangedMsg{notice: "x"})
	got = awnModel(updated)
	if got.list.selectedProfile != "home" || got.acct.manageCursor != 1 {
		t.Fatalf("profile %q cursor %d", got.list.selectedProfile, got.acct.manageCursor)
	}
	// no profile selected: stays empty
	m.list.selectedProfile = ""
	updated, _ = m.handleAccountsChanged(accountsChangedMsg{notice: "x"})
	if awnModel(updated).list.selectedProfile != "" {
		t.Fatal("profile appeared")
	}

	// all accounts gone: no active account, cursor 0
	m = accountsModel(t)
	m.acct.manageCursor = 1
	if err := config.Update(func(c *config.Config) error { c.Accounts = nil; return nil }); err != nil {
		t.Fatal(err)
	}
	updated, _ = m.handleAccountsChanged(accountsChangedMsg{notice: "x"})
	got = awnModel(updated)
	if len(got.acct.list) != 0 || got.acct.activeAccount != "" || got.acct.manageCursor != 0 {
		t.Fatalf("no accounts: %+v", got.acct)
	}

	// the configuration cannot be read back
	m = accountsModel(t)
	m.acct.busy = true
	path, err := config.Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	updated, cmd := m.handleAccountsChanged(accountsChangedMsg{notice: "ok", reload: true})
	got = awnModel(updated)
	if cmd != nil || got.acct.busy || !got.acct.statusErr || got.acct.status == "ok" || len(got.acct.list) != 2 || got.reloading {
		t.Fatalf("unreadable config: %+v", got.acct)
	}
}

// Verifies: REQ-AWN-017
func TestDO178CAccountsChangedHealth(t *testing.T) {
	old := checkAccountHealth
	t.Cleanup(func() { checkAccountHealth = old })
	var checked []string
	checkAccountHealth = func(acc client.AccountInfo) client.Health {
		checked = append(checked, acc.Name)
		return client.Health{Valid: true}
	}
	m := accountsModel(t)
	awnFakeTailscale(t, "0")
	if err := config.Update(func(c *config.Config) error {
		c.AddAccount("lab", "tskey-api-kCCC-0123456789abcdef", "lab.com", false)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// home unchanged with health; work has health but will be re-keyed;
	// lab is new; "gone" has health but is not in the config any more.
	m.acct.health = map[string]accountHealth{
		"home": {checked: true, h: client.Health{Valid: true, ExpiryNote: "kept"}},
		"work": {checked: true, h: client.Health{Valid: true, ExpiryNote: "stale"}},
		"gone": {checked: true},
	}
	if err := config.Update(func(c *config.Config) error { return c.UpdateAccount("work", "", awnKeyNew) }); err != nil {
		t.Fatal(err)
	}
	updated, cmd := m.handleAccountsChanged(accountsChangedMsg{notice: "x"})
	got := awnModel(updated)
	if hs, ok := got.acct.health["home"]; !ok || hs.h.ExpiryNote != "kept" {
		t.Fatal("unchanged account must keep its health")
	}
	if _, ok := got.acct.health["work"]; ok {
		t.Fatal("re-keyed account must lose its health")
	}
	if _, ok := got.acct.health["gone"]; ok {
		t.Fatal("removed account must lose its health")
	}
	awnMsgs(cmd)
	if len(checked) != 2 || !(checked[0] == "work" && checked[1] == "lab" || checked[0] == "lab" && checked[1] == "work") {
		t.Fatalf("checked %v, want work and lab", checked)
	}

	// an unchanged account without a health entry is checked too; a changed tailnet re-checks
	checked = nil
	m = accountsModel(t)
	m.acct.health = map[string]accountHealth{"work": {checked: true}}
	_, cmd = m.handleAccountsChanged(accountsChangedMsg{notice: "x"})
	awnMsgs(cmd)
	if len(checked) != 1 || checked[0] != "home" {
		t.Fatalf("checked %v, want home only", checked)
	}
	if !sameKey(m.acct.list, client.AccountInfo{Name: "home", Tailnet: "home.com", APIKey: awnKeyHome}) ||
		sameKey(m.acct.list, client.AccountInfo{Name: "home", Tailnet: "other.com", APIKey: awnKeyHome}) ||
		sameKey(m.acct.list, client.AccountInfo{Name: "home", Tailnet: "home.com", APIKey: "other"}) ||
		sameKey(m.acct.list, client.AccountInfo{Name: "other", Tailnet: "home.com", APIKey: awnKeyHome}) ||
		sameKey(nil, client.AccountInfo{Name: "home"}) {
		t.Fatal("sameKey")
	}
}

// Verifies: REQ-AWN-018
func TestDO178CAccountsChangedReloadDevices(t *testing.T) {
	// reload with accounts left: a reload starts
	m := accountsModel(t)
	awnFakeTailscale(t, "0")
	updated, cmd := m.handleAccountsChanged(accountsChangedMsg{notice: "x", reload: true})
	got := awnModel(updated)
	if !got.reloading || cmd == nil {
		t.Fatal("reload must start")
	}

	// already reloading: not started again (no extra command, flag stays)
	m.reloading = true
	updated, cmd = m.handleAccountsChanged(accountsChangedMsg{notice: "x", reload: true})
	got = awnModel(updated)
	if !got.reloading {
		t.Fatal("flag lost")
	}
	for _, msg := range awnMsgs(cmd) {
		if _, isReload := msg.(reloadMsg); isReload {
			t.Fatal("a second reload was started")
		}
	}

	// reload not requested: nothing starts
	m.reloading = false
	updated, _ = m.handleAccountsChanged(accountsChangedMsg{notice: "x"})
	if awnModel(updated).reloading {
		t.Fatal("no reload requested")
	}

	// no account left: the list is emptied, no reload
	if err := config.Update(func(c *config.Config) error { c.Accounts = nil; return nil }); err != nil {
		t.Fatal(err)
	}
	updated, _ = m.handleAccountsChanged(accountsChangedMsg{notice: "x", reload: true})
	got = awnModel(updated)
	if got.reloading || got.list.devices != nil || len(got.list.filteredDevices) != 0 {
		t.Fatalf("empty: reloading=%v devices=%v", got.reloading, got.list.devices)
	}
}

// Verifies: REQ-AWN-019
func TestDO178CBackToAccounts(t *testing.T) {
	m := model{}
	if !backToAccounts(m).acct.accountManageMode {
		t.Fatal("must reopen the window")
	}
}

func awnSubmit(t *testing.T, m model, v string) (model, tea.Cmd) {
	t.Helper()
	if m.input.onSubmit == nil {
		t.Fatal("no prompt is open")
	}
	updated, cmd := m.input.onSubmit(m, v)
	return awnModel(updated), cmd
}

// Verifies: REQ-AWN-020, REQ-AWN-021
func TestDO178CPromptAddAccount(t *testing.T) {
	m := accountsModel(t)
	api := startFakeAPI(t)
	secret := "tskey-api-kSECRET-0123456789abcdef"

	updated, cmd := m.handleAccountManagement(key("a"))
	m = awnModel(updated)
	if cmd != nil || m.acct.accountManageMode || m.input.mode != inputText || m.input.label != "Add account: tailnet" || m.input.masked {
		t.Fatalf("tailnet prompt: %+v", m.input)
	}

	// cancelling returns to the window
	if !m.input.onCancel(m).acct.accountManageMode {
		t.Fatal("cancel at the tailnet step")
	}

	// invalid tailnets
	for _, bad := range []string{"", "nodot", "   ", "\x00\x01"} {
		got, cmd := awnSubmit(t, m, bad)
		if cmd != nil || got.input.mode != inputNone || !got.acct.statusErr || !strings.HasPrefix(got.acct.status, "Invalid tailnet: ") || got.acct.busy {
			t.Errorf("tailnet %q: %+v", bad, got.acct)
		}
	}

	// valid tailnet (sanitised) -> key prompt, masked
	m, cmd = awnSubmit(t, m, " \x00new.example.com\x07 ")
	if cmd != nil || m.input.label != "Add account: API key" || !m.input.masked {
		t.Fatalf("key prompt: %+v", m.input)
	}
	if !m.input.onCancel(m).acct.accountManageMode {
		t.Fatal("cancel at the key step")
	}
	// the prompt shows bullets only
	m.input.value, m.input.cursor = secret, len(secret)
	popup := m.renderInputPopup(m.input.label, m.input.value, m.input.masked)
	if strings.Contains(popup, "SECRET") || strings.Contains(popup, "tskey") || !strings.Contains(popup, "•") {
		t.Fatalf("key echoed in the prompt: %q", popup)
	}
	if strings.Contains(m.renderAccountManagement(), "tskey") {
		t.Fatal("the accounts window must not show keys")
	}

	// invalid keys: error without echo
	for _, bad := range []string{"", "short-SECRETVALUE", strings.Repeat("k", 201)} {
		got, cmd := awnSubmit(t, m, bad)
		if cmd != nil || got.input.mode != inputNone || !got.acct.statusErr || !strings.HasPrefix(got.acct.status, "Invalid API key: ") {
			t.Errorf("key %q: %+v", bad, got.acct)
		}
		if strings.Contains(got.acct.status, "SECRETVALUE") {
			t.Errorf("key echoed in status: %q", got.acct.status)
		}
	}

	// valid key -> name prompt
	m, cmd = awnSubmit(t, m, secret+"\x00")
	if cmd != nil || m.input.label != "Add account: display name" || m.input.masked {
		t.Fatalf("name prompt: %+v", m.input)
	}
	if !m.input.onCancel(m).acct.accountManageMode {
		t.Fatal("cancel at the name step")
	}
	got, cmd := awnSubmit(t, m, "bad/name")
	if cmd != nil || got.input.mode != inputNone || !strings.HasPrefix(got.acct.status, "Invalid name: ") || !got.acct.statusErr {
		t.Fatalf("invalid name: %+v", got.acct)
	}
	got, cmd = awnSubmit(t, m, strings.Repeat("n", 65))
	if cmd != nil || !strings.HasPrefix(got.acct.status, "Invalid name: ") {
		t.Fatal("too long name")
	}

	// empty name -> tailnet; the command stores the typed values
	got, cmd = awnSubmit(t, m, "")
	if cmd == nil || !got.acct.busy || got.acct.status != "Checking API key…" || got.acct.statusErr || got.input.mode != inputNone {
		t.Fatalf("final: %+v", got.acct)
	}
	msg := awnChanged(t, cmd)
	if msg.err != nil || msg.notice != "Added account new.example.com" || !msg.reload {
		t.Fatalf("add: %+v", msg)
	}
	accs := accountsOnDisk(t)
	last := accs[len(accs)-1]
	if last.Name != "new.example.com" || last.Tailnet != "new.example.com" || last.APIKey != secret {
		t.Fatalf("stored %+v", last)
	}
	if call, _ := api.last(); !strings.Contains(call.path, "new.example.com") {
		t.Fatalf("verified against %q", call.path)
	}

	// explicit unicode-free name
	got, cmd = awnSubmit(t, m, "My Lab_1.x")
	if cmd == nil || !got.acct.busy {
		t.Fatal("named account")
	}
	if msg := awnChanged(t, cmd); msg.notice != "Added account My Lab_1.x" {
		t.Fatalf("notice %q", msg.notice)
	}
}

// Verifies: REQ-AWN-021
func TestDO178CAccountsWindowNeverShowsKeys(t *testing.T) {
	m := accountsModel(t)
	m.acct.status, m.acct.statusErr = "Invalid API key: API key too short (seems invalid)", true
	out := m.renderAccountManagement()
	for _, k := range []string{awnKeyHome, awnKeyWork, "tskey"} {
		if strings.Contains(out, k) {
			t.Fatalf("the window shows %q", k)
		}
	}
	// the edit prompt for the key is masked too
	acc := m.acct.list[0]
	updated, _ := m.promptEditAccount(acc)
	m = awnModel(updated)
	m, _ = awnSubmit(t, m, "")
	if !m.input.masked || m.input.label != "Edit account: new API key" {
		t.Fatalf("edit key prompt: %+v", m.input)
	}
	popup := m.renderInputPopup(m.input.label, awnKeyNew, m.input.masked)
	if strings.Contains(popup, "tskey") || strings.Contains(popup, "NEW") {
		t.Fatalf("key echoed: %q", popup)
	}
}

// Verifies: REQ-AWN-022
func TestDO178CPromptEditAccount(t *testing.T) {
	m := accountsModel(t)
	api := startFakeAPI(t)
	uni := client.AccountInfo{Name: "Ünï 日本", Tailnet: "uni.com"}
	updated, cmd := m.promptEditAccount(uni)
	pm := awnModel(updated)
	if cmd != nil || pm.input.value != "Ünï 日本" || pm.input.cursor != 6 || pm.input.masked || pm.input.label != "Edit account: name" {
		t.Fatalf("name prompt: %+v", pm.input)
	}
	acc := m.acct.list[0] // home / home.com
	updated, _ = m.promptEditAccount(acc)
	m = awnModel(updated)
	if m.input.value != "home" || m.input.cursor != 4 {
		t.Fatalf("prefill: %+v", m.input)
	}
	if !m.input.onCancel(m).acct.accountManageMode {
		t.Fatal("cancel at the name step")
	}

	// invalid new name
	got, cmd := awnSubmit(t, m, "bad/name")
	if cmd != nil || got.input.mode != inputNone || !got.acct.statusErr || !strings.HasPrefix(got.acct.status, "Invalid name: ") {
		t.Fatalf("invalid name: %+v", got.acct)
	}

	// helper: go through both prompts
	run := func(name, keyIn string) (model, tea.Cmd) {
		t.Helper()
		step, cmd := awnSubmit(t, m, name)
		if cmd != nil || step.input.label != "Edit account: new API key" || !step.input.masked {
			t.Fatalf("key prompt: %+v", step.input)
		}
		if !step.input.onCancel(step).acct.accountManageMode {
			t.Fatal("cancel at the key step")
		}
		return awnSubmit(t, step, keyIn)
	}

	// nothing changed: same name, empty key -> no command, not busy
	for _, same := range []string{"home", "  home  ", ""} {
		got, cmd = run(same, "")
		if cmd != nil || got.acct.busy || got.input.mode != inputNone || got.acct.status != "" {
			t.Errorf("unchanged %q: busy=%v status=%q", same, got.acct.busy, got.acct.status)
		}
	}

	// invalid key
	got, cmd = run("", "short-SECRETVALUE")
	if cmd != nil || got.acct.busy || !got.acct.statusErr || !strings.HasPrefix(got.acct.status, "Invalid API key: ") || strings.Contains(got.acct.status, "SECRETVALUE") {
		t.Fatalf("invalid key: %+v", got.acct)
	}

	// name only
	got, cmd = run("homey", "")
	if cmd == nil || !got.acct.busy || got.acct.status != "Saving…" || got.acct.statusErr {
		t.Fatalf("name only: %+v", got.acct)
	}
	if msg := awnChanged(t, cmd); msg.err != nil || !msg.reload {
		t.Fatalf("name only cmd: %+v", msg)
	}
	if accountsOnDisk(t)[0].Name != "homey" {
		t.Fatal("name not stored under the current account")
	}
	if _, called := api.last(); called {
		t.Fatal("no new key: no API call")
	}

	// key only (the account is addressed by the name/tailnet captured when the prompt opened)
	m = accountsModel(t)
	updated, _ = m.promptEditAccount(m.acct.list[1])
	m = awnModel(updated)
	got, cmd = run("work", awnKeyNew+"\x00")
	if cmd == nil || !got.acct.busy {
		t.Fatal("key only")
	}
	if msg := awnChanged(t, cmd); msg.err != nil || msg.reload {
		t.Fatalf("key only cmd: %+v", msg)
	}
	if accountsOnDisk(t)[1].APIKey != awnKeyNew || accountsOnDisk(t)[0].APIKey != awnKeyHome {
		t.Fatal("key stored for the wrong account")
	}
}

// Verifies: REQ-AWN-023
func TestDO178CRemoveConfirmation(t *testing.T) {
	for _, yes := range []string{"y", "Y"} {
		m := accountsModel(t)
		m.acct.confirmDelete = "work"
		updated, cmd := m.handleAccountManagement(key(yes))
		got := awnModel(updated)
		if cmd == nil || !got.acct.busy || got.acct.confirmDelete != "" {
			t.Fatalf("%s: busy=%v", yes, got.acct.busy)
		}
		if msg := awnChanged(t, cmd); msg.err != nil || msg.notice != "Removed account work" {
			t.Fatalf("%s: %+v", yes, msg)
		}
	}
	for _, no := range []string{"n", "N", "esc", "enter", "d", "x", " "} {
		m := accountsModel(t)
		m.acct.confirmDelete = "work"
		var msg tea.KeyMsg
		switch no {
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		default:
			msg = key(no)
		}
		updated, cmd := m.handleAccountManagement(msg)
		got := awnModel(updated)
		if cmd != nil || got.acct.busy || got.acct.confirmDelete != "" || !got.acct.accountManageMode || len(accountsOnDisk(t)) != 2 {
			t.Errorf("%q must cancel the removal without a command", no)
		}
	}
}

// Verifies: REQ-AWN-024
func TestDO178CWindowKeys(t *testing.T) {
	m := accountsModel(t)
	for _, k := range []tea.KeyMsg{{Type: tea.KeyEsc}, {Type: tea.KeyCtrlC}, key("q")} {
		updated, cmd := m.handleAccountManagement(k)
		if cmd != nil || awnModel(updated).acct.accountManageMode {
			t.Errorf("%v must close the window", k)
		}
	}
	updated, cmd := m.handleAccountManagement(key("a"))
	got := awnModel(updated)
	if cmd != nil || got.acct.accountManageMode || got.input.label != "Add account: tailnet" {
		t.Fatal("a opens the add prompt and closes the window")
	}

	// c discards and re-checks all
	old := checkAccountHealth
	t.Cleanup(func() { checkAccountHealth = old })
	var checked []string
	checkAccountHealth = func(acc client.AccountInfo) client.Health {
		checked = append(checked, acc.Name)
		return client.Health{Valid: true}
	}
	m.acct.health = map[string]accountHealth{"home": {checked: true}, "work": {checked: true}, "extra": {checked: true}}
	updated, cmd = m.handleAccountManagement(key("c"))
	got = awnModel(updated)
	if _, ok := got.acct.health["home"]; ok {
		t.Fatal("health must be discarded")
	}
	if _, ok := got.acct.health["work"]; ok {
		t.Fatal("health must be discarded")
	}
	awnMsgs(cmd)
	if len(checked) != 2 {
		t.Fatalf("checked %v", checked)
	}
	// c with no accounts: nothing to check, nil health map is fine
	e := model{}
	updated, cmd = e.handleAccountManagement(key("c"))
	if cmd != nil || awnModel(updated).acct.health != nil {
		t.Fatal("empty list")
	}
}

// Verifies: REQ-AWN-025
func TestDO178CCursorMovement(t *testing.T) {
	m := accountsModel(t)
	if err := config.Update(func(c *config.Config) error {
		c.AddAccount("lab", "tskey-api-kCCC-0123456789abcdef", "lab.com", false)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load()
	m.acct.list = accountsFromConfig(cfg) // 3 accounts
	step := func(k tea.KeyMsg) {
		updated, cmd := m.handleAccountManagement(k)
		if cmd != nil {
			t.Fatalf("%v returned a command", k)
		}
		m = awnModel(updated)
	}
	up, down := tea.KeyMsg{Type: tea.KeyUp}, tea.KeyMsg{Type: tea.KeyDown}
	step(up) // first row stays
	if m.acct.manageCursor != 0 {
		t.Fatal("up at the first row")
	}
	step(down)
	step(key("j"))
	if m.acct.manageCursor != 2 {
		t.Fatalf("cursor %d", m.acct.manageCursor)
	}
	step(down) // last row stays
	step(key("j"))
	if m.acct.manageCursor != 2 {
		t.Fatal("down at the last row")
	}
	step(up)
	step(key("k"))
	if m.acct.manageCursor != 0 {
		t.Fatalf("cursor %d", m.acct.manageCursor)
	}
	// empty list: no movement
	e := model{}
	for _, k := range []tea.KeyMsg{up, down, key("j"), key("k")} {
		updated, _ := e.handleAccountManagement(k)
		if awnModel(updated).acct.manageCursor != 0 {
			t.Fatal("empty list moved")
		}
	}
}

// Verifies: REQ-AWN-026
func TestDO178CActionsIgnoredWhenBusyOrEmpty(t *testing.T) {
	actions := []tea.KeyMsg{{Type: tea.KeyEnter}, key(" "), key("e"), key("d"), {Type: tea.KeyDelete}}
	// busy
	m := accountsModel(t)
	m.acct.busy = true
	for _, k := range actions {
		updated, cmd := m.handleAccountManagement(k)
		got := awnModel(updated)
		if cmd != nil || !got.acct.accountManageMode || got.acct.confirmDelete != "" || got.input.mode != inputNone || got.acct.status != "" {
			t.Errorf("busy: %v acted", k)
		}
	}
	// empty list / cursor beyond the list
	m = accountsModel(t)
	m.acct.manageCursor = 5
	e := model{}
	e.acct.accountManageMode = true
	for _, mm := range []model{m, e} {
		for _, k := range actions {
			updated, cmd := mm.handleAccountManagement(k)
			got := awnModel(updated)
			if cmd != nil || !got.acct.accountManageMode || got.acct.confirmDelete != "" || got.acct.busy || got.input.mode != inputNone {
				t.Errorf("no account: %v acted", k)
			}
		}
	}
	// unknown keys do nothing on an account
	m = accountsModel(t)
	for _, k := range []tea.KeyMsg{key("x"), key("Z"), {Type: tea.KeyTab}, {Type: tea.KeyF5}} {
		updated, cmd := m.handleAccountManagement(k)
		got := awnModel(updated)
		if cmd != nil || got.acct.busy || !got.acct.accountManageMode || got.acct.confirmDelete != "" {
			t.Errorf("unknown key %v acted", k)
		}
	}
}

// Verifies: REQ-AWN-027
func TestDO178CActionsOnAccount(t *testing.T) {
	m := accountsModel(t)
	dir := awnFakeTailscale(t, "0")
	m.acct.manageCursor = 1 // work

	// enter: switch
	updated, cmd := m.handleAccountManagement(tea.KeyMsg{Type: tea.KeyEnter})
	got := awnModel(updated)
	if cmd == nil || !got.acct.busy || got.acct.status != "Switching…" || got.acct.statusErr {
		t.Fatalf("enter: %+v", got.acct)
	}
	if msg := awnChanged(t, cmd); msg.notice != "This computer now uses work.com" || awnTSLog(t, dir) != "switch work.com\n" {
		t.Fatalf("enter cmd: %+v", msg)
	}

	// space: toggles hidden (hide, then show)
	updated, cmd = m.handleAccountManagement(key(" "))
	got = awnModel(updated)
	if cmd == nil || !got.acct.busy {
		t.Fatal("space")
	}
	if msg := awnChanged(t, cmd); msg.notice != "work is now hidden" {
		t.Fatalf("hide: %+v", msg)
	}
	m.acct.list[1].Hidden = true
	updated, cmd = m.handleAccountManagement(key(" "))
	if msg := awnChanged(t, cmd); msg.notice != "work is now shown" || !awnModel(updated).acct.busy {
		t.Fatalf("show: %+v", msg)
	}
	m.acct.list[1].Hidden = false

	// e: edit prompt
	updated, cmd = m.handleAccountManagement(key("e"))
	got = awnModel(updated)
	if cmd != nil || got.acct.accountManageMode || got.input.label != "Edit account: name" || got.input.value != "work" {
		t.Fatalf("e: %+v", got.input)
	}

	// d and delete: confirmation naming the account, no command
	for _, k := range []tea.KeyMsg{key("d"), {Type: tea.KeyDelete}} {
		updated, cmd = m.handleAccountManagement(k)
		got = awnModel(updated)
		if cmd != nil || got.acct.confirmDelete != "work" || got.acct.busy || len(accountsOnDisk(t)) != 2 {
			t.Fatalf("%v: %+v", k, got.acct)
		}
		if !strings.Contains(got.renderAccountManagement(), `Remove account "work" and its API key? y/N`) {
			t.Fatal("confirmation question missing")
		}
	}
}

func awnViewModel() model {
	now := time.Now()
	m := model{
		version: "1.2.3",
		acct: accounts{list: []client.AccountInfo{
			{Name: "home", Tailnet: "home.com"},
			{Name: "work", Tailnet: "work.com", Hidden: true},
			{Name: "日本語", Tailnet: "jp.example"},
		}},
		list: deviceList{devices: []client.Device{
			{ID: "1", AccountName: "home", LastSeen: now},
			{ID: "2", AccountName: "home"},
			{ID: "3", AccountName: "work", ConnectedToControl: true},
			{ID: "4", AccountName: "ghost", LastSeen: now},
		}},
	}
	m.acct.tailscaleActiveAccount = "work.com"
	m.acct.manageCursor = 1
	m.acct.health = map[string]accountHealth{
		"home": {checked: true, h: client.Health{Valid: true}},
		"work": {checked: true, h: client.Health{Unauth: true}},
	}
	return m
}

// Verifies: REQ-AWN-028
func TestDO178CRenderRows(t *testing.T) {
	m := awnViewModel()
	out := m.renderAccountManagement()
	if !strings.Contains(out, "Accounts (ts-cli v1.2.3)") {
		t.Error("title")
	}
	lines := strings.Split(out, "\n")
	find := func(sub string) string {
		for _, l := range lines {
			if strings.Contains(l, sub) {
				return l
			}
		}
		t.Fatalf("no line with %q in:\n%s", sub, out)
		return ""
	}
	home, work, jp := find("home.com"), find("work.com"), find("jp.example")
	if !strings.Contains(home, "1/2") || !strings.Contains(home, "online") || !strings.Contains(home, "key ok") {
		t.Errorf("home row: %q", home)
	}
	if !strings.Contains(work, "1/1") || !strings.Contains(work, "key rejected") || !strings.Contains(work, "hidden") {
		t.Errorf("work row: %q", work)
	}
	if !strings.Contains(work, "◉") || strings.Contains(home, "◉") || strings.Contains(jp, "◉") {
		t.Error("this-computer marker on the wrong row")
	}
	if !strings.Contains(work, "▸") || strings.Contains(home, "▸") || strings.Contains(jp, "▸") {
		t.Error("cursor marker on the wrong row")
	}
	if !strings.Contains(jp, "0/0") || !strings.Contains(jp, "checking…") || strings.Contains(jp, "hidden") {
		t.Errorf("jp row: %q", jp)
	}
	// every row of the panel has the same width (unicode aligned)
	var widths []int
	for _, l := range lines {
		if strings.HasPrefix(l, "│") {
			widths = append(widths, lipgloss.Width(l))
		}
	}
	for _, w := range widths {
		if w != widths[0] {
			t.Errorf("panel rows differ in width: %v", widths)
			break
		}
	}

	// tailnet column only when some tailnet differs from the name
	same := model{acct: accounts{list: []client.AccountInfo{{Name: "a.example", Tailnet: "a.example"}, {Name: "b.example", Tailnet: "b.example"}}}}
	out = same.renderAccountManagement()
	if strings.Count(out, "a.example") != 1 || strings.Count(out, "b.example") != 1 {
		t.Errorf("tailnet must not repeat the name:\n%s", out)
	}
	same.acct.list[1].Tailnet = "other.example"
	out = same.renderAccountManagement()
	if !strings.Contains(out, "other.example") || strings.Count(out, "a.example") != 2 {
		t.Errorf("tailnet column expected when one differs:\n%s", out)
	}

	// empty list
	out = (model{}).renderAccountManagement()
	if !strings.Contains(out, "No accounts yet. Press a to add one.") {
		t.Errorf("empty hint missing:\n%s", out)
	}
	if !strings.Contains(out, "esc close") || !strings.Contains(out, "◉ = this computer is logged in to it.") {
		t.Error("help lines")
	}

	// many accounts
	many := model{}
	for i := 0; i < 40; i++ {
		many.acct.list = append(many.acct.list, client.AccountInfo{Name: strings.Repeat("n", i%7) + string(rune('A'+i%26)), Tailnet: "t.example"})
	}
	many.acct.manageCursor = 39
	if out = many.renderAccountManagement(); strings.Count(out, "t.example") != 40 {
		t.Error("all accounts must be rendered")
	}
}

// Verifies: REQ-AWN-029
func TestDO178CRenderStatusLine(t *testing.T) {
	m := awnViewModel()
	count := func(out string) (confirm, errs, ok bool) {
		return strings.Contains(out, "Remove account"), strings.Contains(out, "bad news"), strings.Contains(out, "✓ good news")
	}
	if c, e, o := count(m.renderAccountManagement()); c || e || o {
		t.Fatal("no status expected")
	}
	m.acct.status, m.acct.statusErr = "bad news", true
	if c, e, o := count(m.renderAccountManagement()); c || !e || o {
		t.Fatal("error status")
	}
	m.acct.status, m.acct.statusErr = "good news", false
	if c, e, o := count(m.renderAccountManagement()); c || e || !o {
		t.Fatal("success status")
	}
	m.acct.confirmDelete = "work"
	m.acct.status, m.acct.statusErr = "bad news", true
	c, e, o := count(m.renderAccountManagement())
	if !c || e || o {
		t.Fatal("confirmation wins over the status")
	}
	m.acct.status = "good news"
	m.acct.statusErr = false
	if c, _, o = count(m.renderAccountManagement()); !c || o {
		t.Fatal("confirmation wins over success")
	}
	// an API key never reaches the view, even when the model holds it
	for _, a := range m.acct.list {
		if a.APIKey != "" {
			t.Fatal("fixture has keys")
		}
	}
	m.acct.list[0].APIKey = awnKeyHome
	if strings.Contains(m.renderAccountManagement(), "tskey") {
		t.Fatal("API key rendered")
	}
}

// Verifies: REQ-AWN-030
func TestDO178CRenderSizes(t *testing.T) {
	panelWidth := func(m model) int {
		for _, l := range strings.Split(m.renderAccountManagement(), "\n") {
			if strings.HasPrefix(l, "╭") {
				return lipgloss.Width(l)
			}
		}
		return -1
	}
	for _, c := range []struct{ width, want int }{
		{0, 102}, {-7, 102}, {1, 52}, {10, 52}, {56, 52}, {57, 53}, {80, 76}, {106, 102}, {107, 102}, {200, 102},
	} {
		m := awnViewModel()
		m.width = c.width
		if got := panelWidth(m); got != c.want {
			t.Errorf("width %d: panel %d want %d", c.width, got, c.want)
		}
	}
	// zero height and a tiny terminal render without panicking
	m := awnViewModel()
	m.width, m.height = 3, 0
	if m.renderAccountManagement() == "" {
		t.Error("empty render")
	}
}
