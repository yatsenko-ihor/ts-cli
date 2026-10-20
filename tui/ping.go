package tui

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yatsenko-ihor/ts-cli/client"
)

// Reachability of a machine as seen by an explicit ping, which overrides the
// "last seen" heuristic from the Tailscale API in both directions.
type pingState int

const (
	pingUnknown pingState = iota // never pinged: fall back to LastSeen
	pingOK                       // replied: reachable even if the API says offline
	pingFail                     // no reply: unreachable even if the API says online
)

// pingFailedIcon replaces the green/red circle after a failed ping.
const pingFailedIcon = "⭕️"

// pingMsg is the result of pinging one machine.
type pingMsg struct {
	key         string
	name        string
	ok          bool
	took        time.Duration
	unavailable bool // the tailscale CLI is missing; says nothing about the machine
	err         error
}

// pingDevice pings a machine with `tailscale ping`, which tests reachability
// inside the tailnet (what ssh needs), not plain ICMP.
func pingDevice(device client.Device) tea.Cmd {
	key, name := machineKey(device), displayName(device)
	return func() tea.Msg {
		if len(device.Addresses) == 0 {
			return pingMsg{key: key, name: name, err: fmt.Errorf("no IP address")}
		}
		addr := device.Addresses[0]
		if net.ParseIP(addr) == nil {
			return pingMsg{key: key, name: name, err: fmt.Errorf("invalid address %q", addr)}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		start := time.Now()
		out, err := exec.CommandContext(ctx, "tailscale", "ping", "--c", "1", "--timeout", "4s", addr).CombinedOutput()
		took := time.Since(start)
		if err == nil {
			return pingMsg{key: key, name: name, ok: true, took: took}
		}
		var notFound *exec.Error
		if errors.As(err, &notFound) {
			return pingMsg{key: key, name: name, unavailable: true, err: fmt.Errorf("tailscale CLI not found in PATH")}
		}
		detail := strings.TrimSpace(string(out))
		if i := strings.IndexByte(detail, '\n'); i >= 0 {
			detail = detail[:i]
		}
		if detail == "" {
			detail = err.Error()
		}
		return pingMsg{key: key, name: name, err: fmt.Errorf("%s", detail)}
	}
}

// isReachable reports whether commands should be attempted on device: an
// explicit ping wins, otherwise the API's last-seen time decides.
func (m model) isReachable(d client.Device) bool {
	switch m.ping[machineKey(d)] {
	case pingOK:
		return true
	case pingFail:
		return false
	}
	return isDeviceOnline(d)
}

// statusIcon is the circle shown for a machine: ⭕️ after a failed ping,
// 🟢 when reachable, 🔴 otherwise.
func (m model) statusIcon(d client.Device) string {
	if m.ping[machineKey(d)] == pingFail {
		return pingFailedIcon
	}
	if m.isReachable(d) {
		return "🟢"
	}
	return "🔴"
}

// hasSavedPassword reports whether a password is stored for the machine.
func (m model) hasSavedPassword(d client.Device) bool {
	return m.ssh.machineCreds[machineKey(d)].passwordEncrypted != ""
}

// pingTargets starts a ping for every target machine.
func (m model) pingTargets() (tea.Model, tea.Cmd) {
	targets := m.targets()
	if len(targets) == 0 {
		return m, nil
	}
	m.notify.sshError = nil
	if len(targets) == 1 {
		m.notify.info = "Pinging " + displayName(targets[0]) + "…"
	} else {
		m.notify.info = fmt.Sprintf("Pinging %d machines…", len(targets))
	}
	cmds := make([]tea.Cmd, 0, len(targets))
	for _, d := range targets {
		cmds = append(cmds, pingDevice(d))
	}
	return m, tea.Batch(cmds...)
}

// handlePing records a ping result.
func (m model) handlePing(msg pingMsg) (tea.Model, tea.Cmd) {
	m.notify.info = ""
	if msg.unavailable {
		m.notify.sshError = fmt.Errorf("ping: %v", msg.err)
		return m, nil
	}
	if m.ping == nil {
		m.ping = map[string]pingState{}
	}
	if msg.ok {
		m.ping[msg.key] = pingOK
		m.notify.info = fmt.Sprintf("✓ %s replied in %s", msg.name, msg.took.Round(time.Millisecond))
		return m, nil
	}
	m.ping[msg.key] = pingFail
	delete(m.list.marked, msg.key) // never keep an unreachable machine selected
	m.notify.sshError = fmt.Errorf("ping %s failed: %v", msg.name, msg.err)
	return m, nil
}
