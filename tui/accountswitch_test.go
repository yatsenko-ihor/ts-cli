package tui

import (
	"errors"
	"github.com/yatsenko-ihor/ts-cli/client"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestActionsSwitchTheAccountThemselves(t *testing.T) {
	var switched []string
	old := switchAccountFn
	switchAccountFn = func(name string) error { switched = append(switched, name); return nil }
	t.Cleanup(func() { switchAccountFn = old })
	withRebootSend(t, func(d client.Device, u, p string) (string, error) { return "", nil })

	m := flowModel(t, map[string]machineCredential{"dev-1": {username: "pi"}}, "pi")
	m.list.devices[0].AccountTailnet = "other.example.com"
	m.list.filteredDevices = m.list.devices
	m.acct.tailscaleActiveAccount = "mine.example.com"
	d := m.list.devices[0]

	u, cmd := m.startReboot(d)
	m = u.(model)
	if !strings.Contains(m.notify.info, "Switching the Tailscale account to other.example.com") || cmd == nil || m.reboot != nil {
		t.Fatalf("info = %q", m.notify.info)
	}
	msg := cmd().(accountSwitchedMsg)
	if len(switched) != 1 || switched[0] != "other.example.com" || msg.err != nil {
		t.Fatalf("switched %v %+v", switched, msg)
	}
	// the daemon now reports the new account, then the action goes on by itself
	m.acct.tailscaleActiveAccount = "other.example.com"
	u, cmd = m.Update(msg)
	m = u.(model)
	if m.pendingSwitch != nil || !strings.Contains(m.notify.info, "Switched the Tailscale account") || m.reboot == nil || cmd == nil {
		t.Fatalf("after the switch: info=%q reboot=%v", m.notify.info, m.reboot)
	}
}

func TestFailedAccountSwitchStopsTheAction(t *testing.T) {
	old := switchAccountFn
	switchAccountFn = func(string) error { return errors.New("no such account") }
	t.Cleanup(func() { switchAccountFn = old })
	m := flowModel(t, map[string]machineCredential{"dev-1": {username: "pi"}}, "pi")
	m.list.devices[0].AccountTailnet = "other.example.com"
	m.list.filteredDevices = m.list.devices
	m.acct.tailscaleActiveAccount = "mine.example.com"

	u, cmd := m.startHealth(m.list.devices[0])
	m = u.(model)
	u, c2 := m.Update(cmd().(accountSwitchedMsg))
	m = u.(model)
	if c2 != nil || m.pendingSwitch != nil || m.notify.info != "" || m.notify.sshError == nil || !strings.Contains(m.notify.sshError.Error(), "failed to switch") {
		t.Fatalf("err=%v info=%q", m.notify.sshError, m.notify.info)
	}
	// the file manager does the same
	m2 := flowModel(t, map[string]machineCredential{"dev-1": {username: "pi"}}, "pi")
	m2.list.devices[0].AccountTailnet = "other.example.com"
	m2.list.filteredDevices = m2.list.devices
	m2.acct.tailscaleActiveAccount = "mine.example.com"
	if u, c := m2.openFileManager(); c == nil || u.(model).pendingSwitch == nil {
		t.Fatal("file manager switches too")
	}
	// on the right account the action runs at once
	m3 := flowModel(t, map[string]machineCredential{"dev-1": {username: "pi"}}, "pi")
	ran := false
	if _, _ = m3.ensureAccount(m3.list.devices[0], func(m model) (tea.Model, tea.Cmd) { ran = true; return m, nil }); !ran {
		t.Fatal("no switch needed")
	}
}
