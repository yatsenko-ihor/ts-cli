package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yatsenko-ihor/ts-cli/client"
)

func rebootModel(t *testing.T) model {
	t.Helper()
	m := flowModel(t, map[string]machineCredential{"dev-1": {username: "pi"}}, "pi")
	return m
}

func withRebootSend(t *testing.T, fn func(client.Device, string, string) (string, error)) {
	t.Helper()
	old := rebootSend
	rebootSend = fn
	t.Cleanup(func() { rebootSend = old })
}

func TestStartRebootRefusals(t *testing.T) {
	m := rebootModel(t)
	d := m.list.devices[0]

	noAddr := d
	noAddr.Addresses = nil
	if u, c := m.startReboot(noAddr); c != nil || u.(model).act.err == nil {
		t.Fatal("no address")
	}

	other := d
	other.AccountTailnet = "other.example.com"
	m2 := m
	m2.acct.tailscaleActiveAccount = "mine.example.com"
	if u, c := m2.startReboot(other); c == nil || u.(model).pendingSwitch == nil || !strings.Contains(u.(model).notify.info, "Switching the Tailscale account") {
		t.Fatal("another account is switched to by itself, not refused")
	}

	m3 := flowModel(t, nil, "")
	if u, c := m3.startReboot(m3.list.devices[0]); c != nil || !strings.Contains(u.(model).act.err.Error(), "username") {
		t.Fatal("no username")
	}

	m4 := m
	m4.reboot = &rebootState{name: "x"}
	if u, c := m4.startReboot(d); c != nil || !strings.Contains(u.(model).act.err.Error(), "already") {
		t.Fatal("already waiting")
	}
}

func TestRebootAndWaitFlow(t *testing.T) {
	var gotUser, gotPass string
	withRebootSend(t, func(d client.Device, u, p string) (string, error) { gotUser, gotPass = u, p; return "", nil })
	m := rebootModel(t)
	d := m.list.devices[0]

	u, cmd := m.startReboot(d)
	m = u.(model)
	if m.reboot == nil || cmd == nil || !strings.Contains(m.rebootLine(), "sending") {
		t.Fatalf("started: %+v", m.reboot)
	}
	if !strings.Contains(m.View(), "esc stops waiting") {
		t.Fatal("the status line must be on screen")
	}
	sent := cmd().(rebootSentMsg)
	if gotUser != "pi" || gotPass != "" {
		t.Fatalf("credentials: %q %q", gotUser, gotPass)
	}

	// A stale message of another watch is ignored.
	if _, c := m.handleRebootSent(rebootSentMsg{seq: 99}); c != nil {
		t.Fatal("stale sent")
	}
	u, cmd = m.handleRebootSent(sent)
	m = u.(model)
	if !m.reboot.sent || cmd == nil || !strings.Contains(m.rebootLine(), "waiting for it to go down") {
		t.Fatal("sent")
	}

	// Still answering right after the command: not back yet.
	pingOK := pingMsg{key: "dev-1", mode: pingReboot, ok: true}
	u, cmd = m.handleRebootPing(pingOK)
	m = u.(model)
	if m.reboot == nil || cmd == nil {
		t.Fatal("an answer before it went down must not end the watch")
	}
	// It goes down ...
	u, _ = m.handleRebootPing(pingMsg{key: "dev-1", mode: pingReboot})
	m = u.(model)
	if !m.reboot.sawDown || !strings.Contains(m.rebootLine(), "it is down") {
		t.Fatal("down")
	}
	// A ping for another machine does nothing.
	if _, c := m.handleRebootPing(pingMsg{key: "other", mode: pingReboot, ok: true}); c != nil {
		t.Fatal("other machine")
	}
	// ... and comes back.
	u, _ = m.handleRebootPing(pingOK)
	m = u.(model)
	if m.reboot != nil || !strings.Contains(m.notify.info, "back online") || m.ping["dev-1"] != pingOK_ {
		t.Fatalf("done: %+v info=%q", m.reboot, m.notify.info)
	}
}

// pingOK_ is the reachable state; the local pingOK message above shadows the name.
const pingOK_ = pingState(1)

func TestRebootAssumesDoneWhenNeverSeenDown(t *testing.T) {
	m := rebootModel(t)
	m.reboot = &rebootState{seq: 1, key: "dev-1", name: "x", device: m.list.devices[0], started: time.Now().Add(-rebootAssumeDown - time.Second), sent: true}
	u, _ := m.handleRebootPing(pingMsg{key: "dev-1", mode: pingReboot, ok: true})
	if u.(model).reboot != nil {
		t.Fatal("a reboot that was never seen failing is assumed done after a while")
	}
}

func TestRebootTickAndGiveUp(t *testing.T) {
	m := rebootModel(t)
	if _, c := m.handleRebootTick(rebootTickMsg{seq: 1}); c != nil {
		t.Fatal("no watch")
	}
	m.reboot = &rebootState{seq: 1, key: "dev-1", name: "x", device: m.list.devices[0], started: time.Now(), sent: true}
	if _, c := m.handleRebootTick(rebootTickMsg{seq: 2}); c != nil {
		t.Fatal("stale tick")
	}
	if _, c := m.handleRebootTick(rebootTickMsg{seq: 1}); c == nil {
		t.Fatal("a tick pings")
	}
	m.reboot.started = time.Now().Add(-rebootGiveUp - time.Second)
	u, _ := m.handleRebootTick(rebootTickMsg{seq: 1})
	if u.(model).reboot != nil || !strings.Contains(u.(model).act.err.Error(), "did not come back") {
		t.Fatal("give up")
	}
}

func TestRebootSendFailureAndUnavailablePing(t *testing.T) {
	m := rebootModel(t)
	m.reboot = &rebootState{seq: 1, key: "dev-1", name: "x", device: m.list.devices[0], started: time.Now()}
	u, _ := m.handleRebootSent(rebootSentMsg{seq: 1, out: "reboot needs root or passwordless sudo\n", err: errors.New("exit status 3")})
	m2 := u.(model)
	if m2.reboot != nil || !strings.Contains(m2.act.err.Error(), "passwordless sudo") {
		t.Fatalf("err = %v", m2.act.err)
	}
	u, _ = m.handleRebootSent(rebootSentMsg{seq: 1, err: errors.New("exit status 255")})
	if !strings.Contains(u.(model).act.err.Error(), "exit status 255") {
		t.Fatal("falls back to the error text")
	}

	u, _ = m.handleRebootPing(pingMsg{key: "dev-1", mode: pingReboot, unavailable: true, err: errors.New("no cli")})
	if u.(model).reboot != nil || u.(model).act.err == nil {
		t.Fatal("no tailscale CLI ends the watch")
	}
}

func TestEscStopsRebootWatch(t *testing.T) {
	m := rebootModel(t)
	m.reboot = &rebootState{seq: 1, key: "dev-1", name: "x", started: time.Now()}
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = u.(model)
	if m.reboot != nil || !strings.Contains(m.notify.info, "Stopped waiting") || m.rebootLine() != "" {
		t.Fatal("esc stops")
	}
	if m.stopReboot().reboot != nil {
		t.Fatal("idempotent")
	}
}

func TestRebootSendRunsTheScriptOverSSH(t *testing.T) {
	dir := useFakeBins(t, fakeBins{ssh: true})
	d := rebootModel(t).list.devices[0]
	out, err := rebootSend(d, "pi", "")
	if err != nil || out != "" {
		t.Fatalf("%q %v", out, err)
	}
	argv := strings.Join(fakeArgv(t, dir, "ssh.argv"), " ")
	if !strings.Contains(argv, "pi@100.64.0.1") || !strings.Contains(argv, "sh -c") || !strings.Contains(argv, "sudo -n") {
		t.Fatalf("argv = %q", argv)
	}
	t.Setenv("FAKE_SSH_EXIT", "3")
	if _, err := rebootSend(d, "pi", ""); err == nil {
		t.Fatal("exit 3 is an error")
	}
	if _, err := rebootSend(d, "bad user!", ""); err == nil {
		t.Fatal("invalid username")
	}
}

func TestRebootActionAsksForConfirmation(t *testing.T) {
	m := rebootModel(t)
	var row machineAction
	for _, a := range machineActions(m.list.devices[0]) {
		if strings.HasPrefix(a.title, "Reboot") {
			row = a
		}
	}
	if row.run == nil || !row.danger {
		t.Fatal("a dangerous Reboot row must exist")
	}
	u, _ := row.run(m, m.list.devices[0])
	if u.(model).input.mode != inputText || !strings.Contains(u.(model).input.hint, "to confirm") {
		t.Fatal("must ask to type the name")
	}
}
