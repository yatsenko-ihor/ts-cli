package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yatsenko-ihor/ts-cli/client"
	"github.com/yatsenko-ihor/ts-cli/scripts"
)

// Reboot and wait: reboot the highlighted machine over ssh, then watch it with
// `tailscale ping` until it answers again. The command goes out in the
// background with the saved credentials (no prompts), and returns before the
// machine goes down, so the usual "connection lost" handling never triggers.

const (
	rebootTick        = 2 * time.Second
	rebootPingWait    = 3 * time.Second
	rebootAssumeDown  = 40 * time.Second // a reboot never seen failing for this long is assumed done
	rebootGiveUp      = 10 * time.Minute
	rebootSendTimeout = 30 * time.Second
)

// rebootScript reboots in the background two seconds later, so the ssh
// command itself returns normally. Root reboots directly; anyone else needs
// passwordless sudo, which is checked first so the failure is explained.
const rebootScript = `if [ "$(id -u)" = 0 ]; then S=; else S="sudo -n"; fi; ` +
	`$S true 2>/dev/null || { echo "reboot needs root or passwordless sudo" >&2; exit 3; }; ` +
	`nohup sh -c "sleep 2; $S reboot" >/dev/null 2>&1 &`

// rebootState is a reboot being watched (nil when none).
type rebootState struct {
	seq     int
	key     string
	name    string
	device  client.Device
	started time.Time
	sent    bool // the reboot command went out
	sawDown bool // the machine stopped answering at least once
}

type rebootSentMsg struct {
	seq int
	out string
	err error
}

type rebootTickMsg struct{ seq int }

// rebootSend runs the reboot command; replaced in tests.
var rebootSend = func(device client.Device, username, password string) (string, error) {
	return runSSHOnce(device, username, password, "sh -c "+scripts.ShellQuote(rebootScript), rebootSendTimeout)
}

// runSSHOnce runs one remote command without a terminal and returns its
// combined output. It never prompts (saved login or key only) and kills the
// whole process group when timeout passes.
func runSSHOnce(device client.Device, username, password, command string, timeout time.Duration) (string, error) {
	target, err := buildSSHTarget(username, device.Addresses[0])
	if err != nil {
		return "", err
	}
	cmd, cleanup, err := newSSHCommand(target, password, true, command)
	if err != nil {
		return "", err
	}
	defer cleanup()
	setProcessGroup(cmd)
	timer := time.AfterFunc(timeout, func() {
		if cmd.Process != nil {
			_ = killProcessTree(cmd)
		}
	})
	defer timer.Stop()
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// startReboot sends the reboot command to d.
func (m model) startReboot(d client.Device) (tea.Model, tea.Cmd) {
	switch {
	case len(d.Addresses) == 0:
		m.act = actionState{err: fmt.Errorf("the machine has no IP address")}
		return m, nil
	case m.needsAccountSwitch(d):
		m.act = actionState{err: fmt.Errorf("switch to the machine's Tailscale account first (connect once with s)")}
		return m, nil
	case m.reboot != nil:
		m.act = actionState{err: fmt.Errorf("already waiting for %s", m.reboot.name)}
		return m, nil
	}
	username := m.usernameForDevice(d)
	if username == "" {
		m.act = actionState{err: fmt.Errorf("no saved username: connect once with s so the login is remembered")}
		return m, nil
	}
	password := m.savedPassword(machineKey(d))
	m.rebootSeq++
	m.reboot = &rebootState{seq: m.rebootSeq, key: machineKey(d), name: displayName(d), device: d, started: time.Now()}
	m.act = actionState{result: "Sending the reboot command to " + displayName(d) + "…"}
	seq := m.reboot.seq
	return m, func() tea.Msg {
		out, err := rebootSend(d, username, password)
		return rebootSentMsg{seq: seq, out: out, err: err}
	}
}

// handleRebootSent starts watching once the command went out.
func (m model) handleRebootSent(msg rebootSentMsg) (tea.Model, tea.Cmd) {
	r := m.reboot
	if r == nil || r.seq != msg.seq {
		return m, nil
	}
	if msg.err != nil {
		m.reboot = nil
		reason := reconnectReason(msg.err)
		if line := reconnectReason(fmt.Errorf("%s", msg.out)); line != "" {
			reason = line
		}
		m.act = actionState{err: fmt.Errorf("reboot of %s failed: %s", r.name, reason)}
		return m, nil
	}
	r.sent = true
	r.started = time.Now()
	m.act = actionState{result: r.name + " is rebooting"}
	return m, tea.Tick(rebootTick, func(time.Time) tea.Msg { return rebootTickMsg{seq: r.seq} })
}

// handleRebootTick pings the machine again.
func (m model) handleRebootTick(msg rebootTickMsg) (tea.Model, tea.Cmd) {
	r := m.reboot
	if r == nil || r.seq != msg.seq {
		return m, nil
	}
	if time.Since(r.started) > rebootGiveUp {
		m.reboot = nil
		m.act = actionState{err: fmt.Errorf("%s did not come back within %s", r.name, rebootGiveUp)}
		return m, nil
	}
	return m, pingDevice(r.device, pingReboot, rebootPingWait)
}

// handleRebootPing records one ping of the watch.
func (m model) handleRebootPing(msg pingMsg) (tea.Model, tea.Cmd) {
	r := m.reboot
	if r == nil || r.key != msg.key {
		return m, nil
	}
	if msg.unavailable {
		m.reboot = nil
		m.act = actionState{err: fmt.Errorf("cannot watch %s: %v", r.name, msg.err)}
		return m, nil
	}
	if !msg.ok {
		r.sawDown = true
	} else if r.sawDown || time.Since(r.started) > rebootAssumeDown {
		took := time.Since(r.started).Round(time.Second)
		m.reboot = nil
		if m.ping == nil {
			m.ping = map[string]pingState{}
		}
		if m.pingAt == nil {
			m.pingAt = map[string]time.Time{}
		}
		m.ping[msg.key], m.pingAt[msg.key] = pingOK, time.Now()
		m.notify.info = fmt.Sprintf("✓ %s is back online (rebooted in %s) · s connects", r.name, took)
		m.act = actionState{result: "✓ " + r.name + " is back online"}
		return m, nil
	}
	seq := r.seq
	return m, tea.Tick(rebootTick, func(time.Time) tea.Msg { return rebootTickMsg{seq: seq} })
}

// stopReboot ends the watch (Esc); the machine keeps rebooting.
func (m model) stopReboot() model {
	if m.reboot != nil {
		m.notify.info = "Stopped waiting for " + m.reboot.name
		m.reboot = nil
	}
	return m
}

// rebootLine is the status line while a reboot is watched.
func (m model) rebootLine() string {
	r := m.reboot
	if r == nil {
		return ""
	}
	state := "sending the command…"
	switch {
	case r.sent && r.sawDown:
		state = "it is down, waiting for it to come back…"
	case r.sent:
		state = "rebooting, waiting for it to go down…"
	}
	return fmt.Sprintf("⟳ %s: %s %s · esc stops waiting", r.name, state, time.Since(r.started).Round(time.Second))
}
