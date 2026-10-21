package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/yatsenko-ihor/ts-cli/client"
)

func pingModel() model {
	now := time.Now()
	devs := []client.Device{
		{ID: "on", Name: "online", Addresses: []string{"100.64.0.1"}, LastSeen: now},
		{ID: "off", Name: "offline", Addresses: []string{"100.64.0.2"}, LastSeen: now.Add(-time.Hour)},
	}
	return model{set: uiSettings{emojiIcons: true}, list: deviceList{devices: devs, filteredDevices: devs}, ssh: ssh{machineCreds: map[string]machineCredential{}}}
}

func TestStatusIconAndReachability(t *testing.T) {
	m := pingModel()
	on, off := m.list.devices[0], m.list.devices[1]
	if m.statusIcon(on) != "🟢" || m.statusIcon(off) != "🔴" || m.isReachable(off) {
		t.Fatal("default icons follow last-seen")
	}
	m, _ = step(m.handlePing(pingMsg{key: "off", name: "offline", ok: true, took: 12 * time.Millisecond}))
	if m.statusIcon(off) != "🟢" || !m.isReachable(off) {
		t.Fatal("a ping reply makes a red machine green and reachable")
	}
	m, _ = step(m.handlePing(pingMsg{key: "on", name: "online", err: errors.New("no reply")}))
	if m.statusIcon(on) != "⭕️" || m.isReachable(on) {
		t.Fatalf("failed ping must show ⭕️, got %q", m.statusIcon(on))
	}
	if m.notify.sshError == nil {
		t.Fatal("failed ping should be reported")
	}
	m.ping = nil // reload forgets pings
	if m.statusIcon(on) != "🟢" {
		t.Fatal("without ping state the icon follows last-seen again")
	}
}

func TestUnreachableMachinesCannotBeMarked(t *testing.T) {
	m := pingModel()
	m.list.cursor = 1 // offline
	updated, _ := normalModeHandlers[" "](m)
	m = updated.(model)
	if len(m.list.marked) != 0 || m.notify.sshError == nil {
		t.Fatal("space on a red machine must not mark it and should say why")
	}
	m.toggleAllMarks()
	if len(m.markedDevices()) != 1 || m.markedDevices()[0].ID != "on" {
		t.Fatalf("* must mark only reachable machines: %+v", m.markedDevices())
	}
	// A failed ping unmarks a marked machine.
	m, _ = step(m.handlePing(pingMsg{key: "on", name: "online", err: errors.New("x")}))
	if len(m.markedDevices()) != 0 {
		t.Fatal("failed ping must drop the mark")
	}
	// After a successful ping the red machine can be marked.
	m, _ = step(m.handlePing(pingMsg{key: "off", name: "offline", ok: true}))
	m.list.cursor = 1
	updated, _ = normalModeHandlers[" "](m)
	if len(updated.(model).markedDevices()) != 1 {
		t.Fatal("pinged-OK machine should be markable")
	}
}

func TestSavedPasswordIcon(t *testing.T) {
	m := pingModel()
	d := m.list.devices[0]
	if m.hasSavedPassword(d) {
		t.Fatal("no password yet")
	}
	m.ssh.machineCreds["on"] = machineCredential{username: "pi", passwordEncrypted: "x"}
	if !m.hasSavedPassword(d) {
		t.Fatal("password saved")
	}
	m.ssh.machineCreds["on"] = machineCredential{username: "pi"} // key-based: username only
	if m.hasSavedPassword(d) {
		t.Fatal("username-only entry has no password")
	}
}

// step unwraps an Update-style result back into a model.
func step(next tea.Model, cmd tea.Cmd) (model, tea.Cmd) { return next.(model), cmd }

// ─── connectedToControl ───────────────────────────────────────────────────────

func TestConnectedToControlCountsAsOnlineEvenWithoutLastSeen(t *testing.T) {
	// The API omits lastSeen while connectedToControl is true: zero time.
	if !isDeviceOnline(client.Device{ConnectedToControl: true}) {
		t.Fatal("connectedToControl must mean online even when lastSeen is missing")
	}
	if isDeviceOnline(client.Device{}) {
		t.Fatal("no signal at all is offline")
	}
	if !isDeviceOnline(client.Device{LastSeen: time.Now().Add(-time.Minute)}) {
		t.Fatal("recently seen is online without the flag (older API responses)")
	}
	if isDeviceOnline(client.Device{LastSeen: time.Now().Add(-time.Hour)}) {
		t.Fatal("seen an hour ago is offline")
	}
}

// ─── auto ping, pre-flight ────────────────────────────────────────────────────

func withFakePing(t *testing.T, available bool, fn func(addr string) error) {
	t.Helper()
	oldA, oldP := tailscaleAvailable, tailscalePing
	tailscaleAvailable = func() bool { return available }
	tailscalePing = func(_ context.Context, addr string, _ time.Duration) ([]byte, error) {
		if err := fn(addr); err != nil {
			return []byte("no reply from " + addr + "\nsecond line"), err
		}
		return []byte("pong"), nil
	}
	t.Cleanup(func() { tailscaleAvailable, tailscalePing = oldA, oldP })
}

func TestPingDeviceUsesTailscalePing(t *testing.T) {
	withFakePing(t, true, func(addr string) error {
		if addr == "100.64.0.2" {
			return errors.New("exit status 1")
		}
		return nil
	})
	m := pingModel()
	if r := pingDevice(m.list.devices[0], pingAuto, time.Second)().(pingMsg); !r.ok || r.mode != pingAuto || r.key != "on" {
		t.Fatalf("ok ping: %+v", r)
	}
	r := pingDevice(m.list.devices[1], pingManual, time.Second)().(pingMsg)
	if r.ok || r.err == nil || r.err.Error() != "no reply from 100.64.0.2" {
		t.Fatalf("failed ping should carry the first output line: %+v", r)
	}
	if r := pingDevice(client.Device{ID: "x", Name: "x"}, pingManual, time.Second)().(pingMsg); r.ok || r.err == nil {
		t.Fatal("device without address cannot be pinged")
	}
}

func enableAutoPing(t *testing.T) {
	t.Helper()
	old := autoPingEnabled
	autoPingEnabled = true
	t.Cleanup(func() { autoPingEnabled = old })
}

func TestAutoPingIsOffByDefault(t *testing.T) {
	withFakePing(t, true, func(string) error { return nil })
	m := pingModel()
	if autoPingEnabled || m.wantsAutoPing(m.list.devices[0]) || m.scheduleAutoPing() != nil {
		t.Fatal("auto ping must be disabled by default")
	}
	// The pre-flight check before connecting still works.
	_, cmd := m.withPreflight(m.list.devices[0], func(m model) (tea.Model, tea.Cmd) { return m, nil })
	if cmd == nil {
		t.Fatal("pre-flight check must stay enabled")
	}
}

func TestAutoPingOnlyForCursorMachineAfterItRests(t *testing.T) {
	enableAutoPing(t)
	withFakePing(t, true, func(string) error { return nil })
	m := pingModel()

	cmd := m.scheduleAutoPing()
	if cmd == nil || m.pingSeq != 1 {
		t.Fatalf("expected a debounced ping to be scheduled, seq=%d", m.pingSeq)
	}
	// A tick from an older cursor position is ignored.
	if _, c := m.handleAutoPingTick(autoPingTickMsg{seq: 0, key: "on"}); c != nil {
		t.Fatal("stale tick must not start a ping")
	}
	// A tick for a machine the cursor left is ignored too.
	if _, c := m.handleAutoPingTick(autoPingTickMsg{seq: 1, key: "off"}); c != nil {
		t.Fatal("tick for another machine must not start a ping")
	}
	updated, c := m.handleAutoPingTick(autoPingTickMsg{seq: 1, key: "on"})
	m = updated.(model)
	if c == nil || !m.pinging["on"] {
		t.Fatal("matching tick starts the ping and marks it in flight")
	}
	// Not started twice while in flight.
	if m.wantsAutoPing(m.list.devices[0]) {
		t.Fatal("a machine being pinged must not be pinged again")
	}
	// Silent result: no status line, no error, state recorded.
	updated, _ = m.handlePing(pingMsg{key: "on", name: "online", mode: pingAuto, ok: true})
	m = updated.(model)
	if m.notify.info != "" || m.notify.sshError != nil || m.ping["on"] != pingOK || m.pinging["on"] {
		t.Fatalf("auto ping must be silent: %+v", m.notify)
	}
	if m.wantsAutoPing(m.list.devices[0]) {
		t.Fatal("a fresh result must be reused")
	}
	// Failed auto ping is silent as well but switches the icon.
	updated, _ = m.handlePing(pingMsg{key: "off", name: "offline", mode: pingAuto, err: errors.New("x")})
	m = updated.(model)
	if m.notify.sshError != nil || m.statusIcon(m.list.devices[1]) != "⭕️" {
		t.Fatal("failed auto ping: silent, but ⭕️")
	}
}

func TestAutoPingSkippedWithoutCLIOrForOtherAccount(t *testing.T) {
	enableAutoPing(t)
	m := pingModel()
	withFakePing(t, false, func(string) error { return nil })
	if m.wantsAutoPing(m.list.devices[0]) {
		t.Fatal("no tailscale CLI: nothing to ping with")
	}
	withFakePing(t, true, func(string) error { return nil })
	m.acct.tailscaleActiveAccount = "me@example.com"
	m.list.devices[0].AccountTailnet = "other-company.com"
	if m.wantsAutoPing(m.list.devices[0]) {
		t.Fatal("machines of another account cannot be reached from here; do not ping them")
	}
}

func TestPreflightContinuesOnReplyAndAbortsOnSilence(t *testing.T) {
	withFakePing(t, true, func(string) error { return nil })
	ran := 0
	next := func(m model) (tea.Model, tea.Cmd) { ran++; return m, nil }

	// Reply: the launch resumes.
	m := pingModel()
	dev := m.list.devices[0]
	updated, cmd := m.withPreflight(dev, next)
	m = updated.(model)
	if cmd == nil || m.preflight == nil || ran != 0 {
		t.Fatal("launch must wait for the ping")
	}
	updated, _ = m.handlePing(cmd().(pingMsg))
	m = updated.(model)
	if ran != 1 || m.preflight != nil || m.ping["on"] != pingOK {
		t.Fatalf("reply must resume the launch (ran=%d)", ran)
	}

	// A fresh OK result skips the check entirely.
	updated, cmd = m.withPreflight(dev, next)
	if ran != 2 || cmd != nil || updated.(model).preflight != nil {
		t.Fatalf("fresh ping must not be repeated (ran=%d)", ran)
	}

	// Silence: no launch, clear message, machine marked unreachable.
	withFakePing(t, true, func(string) error { return errors.New("timeout") })
	m = pingModel()
	m.list.marked = map[string]bool{"on": true}
	updated, cmd = m.withPreflight(m.list.devices[0], next)
	m = updated.(model)
	updated, _ = m.handlePing(cmd().(pingMsg))
	m = updated.(model)
	if ran != 2 {
		t.Fatal("an unreachable machine must not be launched on")
	}
	if m.sshErrText() == "" || !strings.Contains(m.sshErrText(), "did not answer a ping") || m.statusIcon(m.list.devices[0]) != "⭕️" || len(m.list.marked) != 0 {
		t.Fatalf("expected clear failure state, got err=%q icon=%q", m.sshErrText(), m.statusIcon(m.list.devices[0]))
	}
}

func (m model) sshErrText() string {
	if m.notify.sshError == nil {
		return ""
	}
	return m.notify.sshError.Error()
}

func TestPreflightSkippedOrNonBlockingWhenPingImpossible(t *testing.T) {
	ran := 0
	next := func(m model) (tea.Model, tea.Cmd) { ran++; return m, nil }

	// No tailscale CLI: connect directly.
	withFakePing(t, false, func(string) error { return nil })
	m := pingModel()
	if _, cmd := m.withPreflight(m.list.devices[0], next); ran != 1 || cmd != nil {
		t.Fatal("without the CLI the launch must proceed immediately")
	}

	// CLI vanished between check and run: proceed, never block on "unavailable".
	withFakePing(t, true, func(string) error { return nil })
	updated, _ := m.withPreflight(m.list.devices[0], next)
	m = updated.(model)
	updated, _ = m.handlePing(pingMsg{key: "on", mode: pingPreflight, unavailable: true, err: errors.New("no cli")})
	if ran != 2 || updated.(model).preflight != nil {
		t.Fatalf("unavailable must not block (ran=%d)", ran)
	}
}

func TestKeysAreIgnoredDuringPreflightExceptCancel(t *testing.T) {
	m := pingModel()
	m.preflight = &preflight{key: "on", next: func(m model) (tea.Model, tea.Cmd) { return m, nil }}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	if m.preflight == nil || m.list.cursor != 0 {
		t.Fatal("ordinary keys are ignored while checking")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.(model).preflight != nil {
		t.Fatal("esc cancels the pending launch")
	}
}

func TestDeviceRowsAlignColumns(t *testing.T) {
	now := time.Now()
	devs := []client.Device{
		{ID: "1", Name: "box-043.tail813b1c.ts.net", Addresses: []string{"100.76.50.39"}, LastSeen: now},
		{ID: "2", Name: "ihors-macbook-air.tail41a337.ts.net", Addresses: []string{"100.67.115.3"}, LastSeen: now, Expires: now.Add(24 * time.Hour)},
		{ID: "3", Name: "SkopasLinux", Addresses: []string{"100.107.26.80"}, LastSeen: now, Expires: now.Add(24 * time.Hour)},
		{ID: "4", Name: "cm4", Addresses: nil, LastSeen: now},
	}
	for _, emoji := range []bool{false, true} {
		m := model{width: 140, height: 40, set: uiSettings{emojiIcons: emoji}, list: deviceList{devices: devs, filteredDevices: devs},
			ssh: ssh{machineCreds: map[string]machineCredential{"1": {passwordEncrypted: "x"}, "4": {passwordEncrypted: "x"}}}}
		cols := m.listColumns(false)
		var widths []int
		for i, d := range devs {
			row := m.renderListDevice(i, cols, false)
			widths = append(widths, lipgloss.Width(row))
			if want := 2 + 2 + m.statusWidth() + 1 + cols.name + 1; !strings.Contains(row, deviceAddress(d)) ||
				lipgloss.Width(row[:strings.Index(row, deviceAddress(d))]) != want {
				t.Fatalf("emoji=%v: address column misaligned in %q", emoji, row)
			}
		}
		for _, w := range widths[1:] {
			if w != widths[0] {
				t.Fatalf("emoji=%v: rows must have equal width: %v", emoji, widths)
			}
		}
	}
}

func TestPingAndProfileKeys(t *testing.T) {
	withFakePing(t, true, func(string) error { return nil })
	m := pingModel()

	// p pings
	updated, cmd := normalModeHandlers["p"](m)
	if cmd == nil || updated.(model).acct.profileSelectMode {
		t.Fatal("p must start a ping, not open the profile selector")
	}
	// P (Shift+P) opens the profile selector
	updated, cmd = normalModeHandlers["P"](m)
	if !updated.(model).acct.profileSelectMode || cmd != nil {
		t.Fatal("P must open the profile selector")
	}
}
