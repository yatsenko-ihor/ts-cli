package tui

import (
	"errors"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yatsenko-ihor/ts-cli/client"
)

func pingModel() model {
	now := time.Now()
	devs := []client.Device{
		{ID: "on", Name: "online", Addresses: []string{"100.64.0.1"}, LastSeen: now},
		{ID: "off", Name: "offline", Addresses: []string{"100.64.0.2"}, LastSeen: now.Add(-time.Hour)},
	}
	return model{list: deviceList{devices: devs, filteredDevices: devs}, ssh: ssh{machineCreds: map[string]machineCredential{}}}
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
