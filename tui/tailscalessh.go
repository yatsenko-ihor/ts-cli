package tui

import (
	"fmt"
	"os"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yatsenko-ihor/ts-cli/client"
	"github.com/yatsenko-ihor/ts-cli/scripts"
)

// Tailscale SSH. A machine that runs the Tailscale SSH server advertises SSH
// host keys in `tailscale status --json`; when the option is on, `s` then runs
// `tailscale ssh [user@]machine` instead of plain ssh. tailscaled
// authenticates the connection from the tailnet's ACL, so no password, key or
// username prompt is involved and nothing is saved.

// tailscaleSSHActive reports whether a session to d should use Tailscale SSH:
// the option is on, the tailscale CLI exists, this computer is logged in to
// d's account, and the machine offers Tailscale SSH.
func (m model) tailscaleSSHActive(d client.Device) bool {
	if !m.set.tailscaleSSH || !tailscaleAvailable() || m.needsAccountSwitch(d) {
		return false
	}
	p, ok := m.peerFor(d)
	return ok && p.ssh
}

// tailscaleSSHDoneMsg is returned when an in-terminal `tailscale ssh` ends.
type tailscaleSSHDoneMsg struct {
	name string
	err  error
}

// launchTailscaleSSH opens `tailscale ssh` to device in a tmux pane when that
// is active, otherwise in ts-cli's own terminal. username may be empty (the
// local user name is then used by tailscale).
func (m model) launchTailscaleSSH(device client.Device, username string) tea.Cmd {
	name := displayName(device)
	if len(device.Addresses) == 0 {
		return func() tea.Msg { return tmuxOpenedMsg{name: name, err: fmt.Errorf("device has no IP addresses")} }
	}
	target, err := buildSSHTarget(username, device.Addresses[0])
	if err != nil {
		return func() tea.Msg { return tmuxOpenedMsg{name: name, err: err} }
	}

	if m.tmuxSplitActive("") {
		script := paneScript("tailscale ssh " + scripts.ShellQuote(target))
		return func() tea.Msg {
			if err := runTmux(tmuxSplitArgs(script, os.Getenv("TMUX_PANE"))...); err != nil {
				return tmuxOpenedMsg{name: name, err: fmt.Errorf("tmux split-window failed: %w", err)}
			}
			return tmuxOpenedMsg{name: name}
		}
	}

	banner := fmt.Sprintf("\nTailscale SSH to %s (%s)\n", name, target)
	return teaExec(&bannerExec{banner: banner, cmd: exec.Command("tailscale", "ssh", target)}, func(err error) tea.Msg {
		return tailscaleSSHDoneMsg{name: name, err: err}
	})
}

// handleTailscaleSSHDone reports how an in-terminal session ended. A remote
// exit status is information, not an error.
func (m model) handleTailscaleSSHDone(msg tailscaleSSHDoneMsg) (tea.Model, tea.Cmd) {
	m.notify.sshError = nil
	if msg.err != nil {
		m.notify.info = fmt.Sprintf("Tailscale SSH to %s ended: %v (F12 → \"Use Tailscale SSH\" switches to plain ssh)", msg.name, msg.err)
	}
	return m, nil
}
