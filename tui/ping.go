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

// Reachability of a machine.
//
// The Tailscale API (connectedToControl / lastSeen) gives an instant,
// account-wide baseline. Explicit `tailscale ping` results refine it for the
// machines that matter right now and override the baseline in both
// directions. Pings are started
//
//   - manually with p (selected or marked machines),
//   - automatically for the machine under the cursor (at start and after the
//     cursor rests on a machine for a moment), and
//   - as a short pre-flight check before s / running something on a single
//     machine, so an unreachable machine fails in about 2 s with a clear
//     message instead of waiting for ssh to time out.
//
// Machines of another Tailscale account than the active one are never
// pinged automatically: this computer cannot reach them until the account
// is switched, so a ping would only produce a false negative.

type pingState int

const (
	pingUnknown pingState = iota // never pinged: fall back to the API baseline
	pingOK                       // replied: reachable even if the API says offline
	pingFail                     // no reply: unreachable even if the API says online
)

// pingMode says why a ping was started, which decides how its result is shown.
type pingMode int

const (
	pingManual    pingMode = iota // P: report the result
	pingAuto                      // background for the cursor machine: silent
	pingPreflight                 // before connecting: continue or abort
)

const (
	pingFreshFor     = 60 * time.Second       // a result younger than this is reused
	autoPingDelay    = 500 * time.Millisecond // cursor must rest this long
	manualPingWait   = 4 * time.Second
	preflightPingMax = 2 * time.Second
)

// pingFailedIcon replaces the green/red circle after a failed ping.
const pingFailedIcon = "⭕️"

// pingMsg is the result of pinging one machine.
type pingMsg struct {
	key         string
	name        string
	mode        pingMode
	ok          bool
	took        time.Duration
	unavailable bool // the tailscale CLI is missing; says nothing about the machine
	err         error
}

// autoPingTickMsg fires after the cursor rested on a machine.
type autoPingTickMsg struct {
	seq int
	key string
}

// preflight is a launch waiting for its reachability check.
type preflight struct {
	key  string
	name string
	next func(model) (tea.Model, tea.Cmd)
}

// autoPingEnabled switches the background ping of the machine under the
// cursor on or off. It is currently OFF: the code stays in place, set it to
// true to bring the behaviour back. Manual pings (P) and the pre-flight
// check before connecting are not affected.
var autoPingEnabled = false

// Seams for tests.
var (
	tailscaleAvailable = func() bool { _, err := exec.LookPath("tailscale"); return err == nil }
	tailscalePing      = func(ctx context.Context, addr string, wait time.Duration) ([]byte, error) {
		return exec.CommandContext(ctx, "tailscale", "ping", "--c", "1", "--timeout", wait.String(), addr).CombinedOutput()
	}
)

// pingDevice pings a machine with `tailscale ping`, which tests reachability
// inside the tailnet (what ssh needs), not plain ICMP.
func pingDevice(device client.Device, mode pingMode, wait time.Duration) tea.Cmd {
	key, name := machineKey(device), displayName(device)
	return func() tea.Msg {
		fail := func(err error) tea.Msg { return pingMsg{key: key, name: name, mode: mode, err: err} }
		if len(device.Addresses) == 0 {
			return fail(fmt.Errorf("no IP address"))
		}
		addr := device.Addresses[0]
		if net.ParseIP(addr) == nil {
			return fail(fmt.Errorf("invalid address %q", addr))
		}
		ctx, cancel := context.WithTimeout(context.Background(), wait+4*time.Second)
		defer cancel()
		start := time.Now()
		out, err := tailscalePing(ctx, addr, wait)
		took := time.Since(start)
		if err == nil {
			return pingMsg{key: key, name: name, mode: mode, ok: true, took: took}
		}
		var notFound *exec.Error
		if errors.As(err, &notFound) {
			return pingMsg{key: key, name: name, mode: mode, unavailable: true, err: fmt.Errorf("tailscale CLI not found in PATH")}
		}
		detail := strings.TrimSpace(string(out))
		if i := strings.IndexByte(detail, '\n'); i >= 0 {
			detail = detail[:i]
		}
		if detail == "" {
			detail = err.Error()
		}
		return fail(fmt.Errorf("%s", detail))
	}
}

// isReachable reports whether commands should be attempted on device: an
// explicit ping wins, otherwise the API baseline decides.
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

// pingIsFresh reports whether device was pinged less than pingFreshFor ago.
func (m model) pingIsFresh(d client.Device) bool {
	at, ok := m.pingAt[machineKey(d)]
	return ok && time.Since(at) < pingFreshFor
}

// canPing reports whether pinging d can say something useful from here.
func (m model) canPing(d client.Device) bool {
	return len(d.Addresses) > 0 && !m.needsAccountSwitch(d) && tailscaleAvailable()
}

// wantsAutoPing reports whether a background ping of d is worthwhile.
func (m model) wantsAutoPing(d client.Device) bool {
	return autoPingEnabled && m.canPing(d) && !m.pinging[machineKey(d)] && !m.pingIsFresh(d)
}

// startAutoPing marks d as being pinged and returns the ping command.
func (m *model) startAutoPing(d client.Device) tea.Cmd {
	if m.pinging == nil {
		m.pinging = map[string]bool{}
	}
	m.pinging[machineKey(d)] = true
	return pingDevice(d, pingAuto, manualPingWait)
}

// scheduleAutoPing waits autoPingDelay and then pings the machine under the
// cursor, unless the cursor moved on in the meantime.
func (m *model) scheduleAutoPing() tea.Cmd {
	d, ok := m.targetDevice()
	if !ok || !m.wantsAutoPing(d) {
		return nil
	}
	m.pingSeq++
	seq, key := m.pingSeq, machineKey(d)
	return tea.Tick(autoPingDelay, func(time.Time) tea.Msg { return autoPingTickMsg{seq: seq, key: key} })
}

// handleAutoPingTick starts the debounced ping if the cursor is still there.
func (m model) handleAutoPingTick(msg autoPingTickMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.pingSeq {
		return m, nil
	}
	d, ok := m.targetDevice()
	if !ok || machineKey(d) != msg.key || !m.wantsAutoPing(d) {
		return m, nil
	}
	return m, m.startAutoPing(d)
}

// withPreflight runs next right away when d was pinged recently or cannot be
// pinged from here; otherwise it pings d first (2 s at most) and continues on
// success or aborts with a clear message.
func (m model) withPreflight(d client.Device, next func(model) (tea.Model, tea.Cmd)) (tea.Model, tea.Cmd) {
	if !m.canPing(d) || (m.pingIsFresh(d) && m.ping[machineKey(d)] == pingOK) {
		return next(m)
	}
	m.preflight = &preflight{key: machineKey(d), name: displayName(d), next: next}
	m.notify.sshError = nil
	m.notify.info = "Checking " + displayName(d) + "…"
	return m, pingDevice(d, pingPreflight, preflightPingMax)
}

// pingTargets starts a manual ping for every target machine.
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
		cmds = append(cmds, pingDevice(d, pingManual, manualPingWait))
	}
	return m, tea.Batch(cmds...)
}

// handlePing records a ping result and, depending on how it was started,
// reports it, stays silent, or resumes/aborts a pending launch.
func (m model) handlePing(msg pingMsg) (tea.Model, tea.Cmd) {
	delete(m.pinging, msg.key)
	pf := m.preflight
	pending := msg.mode == pingPreflight && pf != nil && pf.key == msg.key

	if msg.unavailable {
		// Nothing was learned about the machine. Do not block a launch on it.
		if pending {
			m.preflight = nil
			m.notify.info = ""
			return pf.next(m)
		}
		if msg.mode == pingManual {
			m.notify.info = ""
			m.notify.sshError = fmt.Errorf("ping: %v", msg.err)
		}
		return m, nil
	}

	if m.ping == nil {
		m.ping = map[string]pingState{}
	}
	if m.pingAt == nil {
		m.pingAt = map[string]time.Time{}
	}
	m.pingAt[msg.key] = time.Now()
	if msg.ok {
		m.ping[msg.key] = pingOK
	} else {
		m.ping[msg.key] = pingFail
		delete(m.list.marked, msg.key) // never keep an unreachable machine selected
	}

	switch {
	case pending:
		m.preflight = nil
		m.notify.info = ""
		if msg.ok {
			return pf.next(m)
		}
		m.notify.sshError = fmt.Errorf("%s did not answer a ping (%v); check that it is on and connected, then press p to retry", msg.name, msg.err)
	case msg.mode == pingManual:
		m.notify.info = ""
		if msg.ok {
			m.notify.info = fmt.Sprintf("✓ %s replied in %s", msg.name, msg.took.Round(time.Millisecond))
		} else {
			m.notify.sshError = fmt.Errorf("ping %s failed: %v", msg.name, msg.err)
		}
	}
	return m, nil
}
