package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Reconnecting.
//
// A machine that is rebooting, or a network that dropped, is not a problem
// with the username or password, so it never shows the "change username /
// password" menu. Instead the attempt is repeated every reconnectDelay
// seconds, with a countdown on screen, until the machine answers or Esc is
// pressed. A session that was already working and then dropped asks first.

const reconnectDelay = 5 // seconds between attempts

// reconnectState is a reconnect loop in progress (nil when none).
type reconnectState struct {
	seq     int    // identifies this loop; ticks of an older loop are ignored
	attempt int    // number of the attempt running or waiting to run
	wait    int    // seconds left until the next attempt
	probing bool   // an attempt is running now
	reason  string // why the last attempt failed
}

// reconnectTickMsg counts the wait down by one second.
type reconnectTickMsg struct{ seq int }

func tickReconnect(seq int) tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return reconnectTickMsg{seq: seq} })
}

// unreachableMarkers are what ssh prints when the machine could not be
// reached or the connection was cut, as opposed to a login being refused.
var unreachableMarkers = []string{
	"connection timed out", "operation timed out", "connection refused", "no route to host",
	"network is unreachable", "host is down", "could not resolve hostname", "connection reset",
	"connection closed by", "kex_exchange_identification", "broken pipe", "timeout",
}

// classifySSHStderr turns a generic connection failure into
// sshFailureUnreachable when ssh's own message says the machine did not
// answer. A refused login ("permission denied") stays a connection failure.
func classifySSHStderr(failure sshFailure, stderr string) sshFailure {
	if failure != sshFailureConnection {
		return failure
	}
	text := strings.ToLower(stderr)
	if strings.Contains(text, "permission denied") {
		return failure
	}
	for _, marker := range unreachableMarkers {
		if strings.Contains(text, marker) {
			return sshFailureUnreachable
		}
	}
	return failure
}

// tailBuffer keeps the last bytes written to it: enough of ssh's stderr to
// tell why it stopped.
type tailBuffer struct{ data []byte }

const tailBufferSize = 4096

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.data = append(t.data, p...)
	if len(t.data) > tailBufferSize {
		t.data = t.data[len(t.data)-tailBufferSize:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string { return string(t.data) }

// reconnectReason is the one-line cause shown while waiting.
func reconnectReason(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(strings.SplitN(err.Error(), "\n", 2)[0])
}

// waitReconnect starts or continues the reconnect loop after a failed
// attempt: it waits reconnectDelay seconds and then tries again.
func (m model) waitReconnect(err error) (tea.Model, tea.Cmd) {
	r := m.ssh.reconnect
	if r == nil {
		m.ssh.reconnectSeq++
		r = &reconnectState{seq: m.ssh.reconnectSeq, attempt: 1}
		m.ssh.reconnect = r
	}
	r.probing = false
	r.wait = reconnectDelay
	r.reason = reconnectReason(err)
	m.notify.sshError = nil
	return m, tickReconnect(r.seq)
}

// startReconnect begins the loop with an immediate attempt.
func (m model) startReconnect() (tea.Model, tea.Cmd) {
	m.ssh.askReconnect = false
	m.notify.sshError = nil
	m.ssh.reconnectSeq++
	m.ssh.reconnect = &reconnectState{seq: m.ssh.reconnectSeq, attempt: 1, probing: true}
	return m, m.reconnectProbe()
}

// reconnectProbe checks that the pending machine accepts a login again,
// without taking over the terminal.
func (m model) reconnectProbe() tea.Cmd {
	p := m.ssh.pending
	if p.deviceIndex < 0 || p.deviceIndex >= len(m.list.filteredDevices) {
		return func() tea.Msg {
			return probeMsg{err: fmt.Errorf("device list changed; please select the device again"), failure: sshFailureExec}
		}
	}
	password := p.password
	if password == "" {
		password = m.savedPassword(p.machineID)
	}
	return probeSSH(m.list.filteredDevices[p.deviceIndex], p.username, password)
}

// handleReconnectTick counts the wait down and starts the next attempt.
func (m model) handleReconnectTick(msg reconnectTickMsg) (tea.Model, tea.Cmd) {
	r := m.ssh.reconnect
	if r == nil || r.seq != msg.seq || r.probing {
		return m, nil
	}
	r.wait--
	if r.wait > 0 {
		return m, tickReconnect(r.seq)
	}
	r.attempt++
	r.probing = true
	return m, m.reconnectProbe()
}

// stopReconnect ends the loop and the attempt behind it.
func (m model) stopReconnect() model {
	name := m.ssh.pending.machineName
	m = m.cancelSSHAttempt()
	m.notify.info = "Reconnect to " + name + " stopped"
	return m
}

// handleReconnectKey: while reconnecting only stopping is accepted.
func (m model) handleReconnectKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if k := msg.String(); k == "esc" || k == "ctrl+c" {
		return m.stopReconnect(), nil
	}
	return m, nil
}

// handleReconnectAskKey answers "reconnect automatically?".
func (m model) handleReconnectAskKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k", "down", "j", "tab":
		m.ssh.askCursor = 1 - m.ssh.askCursor
		return m, nil
	case "y":
		return m.startReconnect()
	case "enter":
		if m.ssh.askCursor == 0 {
			return m.startReconnect()
		}
		return m.declineReconnect(), nil
	case "n", "esc", "q", "ctrl+c":
		return m.declineReconnect(), nil
	}
	return m, nil
}

// declineReconnect leaves the dropped session alone; s reconnects by hand.
func (m model) declineReconnect() model {
	name := m.ssh.pending.machineName
	m = m.cancelSSHAttempt()
	m.notify.sshError = fmt.Errorf("connection to %s lost; press s to reconnect", name)
	return m
}

func (m model) reconnectPopup(title, body string, items []string, cursor int, help string) string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(title))
	b.WriteString("\n\n")
	width := 56
	if m.width > 0 && m.width < 76 {
		width = max(m.width-10, 35)
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cur.warn).Padding(1, 2).Width(width)
	var c strings.Builder
	c.WriteString(lipgloss.NewStyle().Bold(true).Render(body))
	if len(items) > 0 {
		c.WriteString("\n\n")
	}
	for i, item := range items {
		if i == cursor {
			c.WriteString(selectedStyle.Render("▸ " + item))
		} else {
			c.WriteString(normalStyle.Render("  " + item))
		}
		if i < len(items)-1 {
			c.WriteString("\n")
		}
	}
	b.WriteString(box.Render(c.String()))
	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render(help))
	return b.String()
}

// renderReconnectAsk is the question shown after a working session dropped.
func (m model) renderReconnectAsk() string {
	return m.reconnectPopup("SSH Connection Lost",
		fmt.Sprintf("Connection to %s was lost.\nReconnect automatically?", m.ssh.pending.machineName),
		[]string{"Yes, keep trying until it answers", "No"}, m.ssh.askCursor,
		"↑/↓ choose • enter select • y yes • n/esc no")
}

// renderReconnect is the countdown shown while waiting for the machine.
func (m model) renderReconnect() string {
	r := m.ssh.reconnect
	status := fmt.Sprintf("Attempt %d in %d s…", r.attempt+1, r.wait)
	if r.probing {
		status = fmt.Sprintf("Attempt %d: connecting…", r.attempt)
	}
	body := fmt.Sprintf("Waiting for %s\n%s", m.ssh.pending.machineName, status)
	if r.reason != "" {
		body += "\nLast error: " + r.reason
	}
	return m.reconnectPopup("Reconnecting", body, nil, 0, "esc stop")
}
