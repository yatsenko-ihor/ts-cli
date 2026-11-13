package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yatsenko-ihor/ts-cli/client"
)

// Health of a machine over ssh: uptime, load, memory and the root disk, read
// with one read-only command and the saved login (Machine tab → "Check
// health"). It never prompts; a machine without a saved login says so.

const healthTimeout = 20 * time.Second

// healthScript prints four sections separated by "---": uptime, /proc/loadavg,
// the root filesystem and memory. Each part is optional; a missing tool
// leaves its section empty.
const healthScript = `(uptime -p 2>/dev/null || uptime 2>/dev/null); echo ---; ` +
	`cat /proc/loadavg 2>/dev/null; echo ---; ` +
	`df -Pk / 2>/dev/null | tail -n 1; echo ---; ` +
	`free -m 2>/dev/null | sed -n 2p`

// healthInfo is what the script reported.
type healthInfo struct {
	at       time.Time
	uptime   string
	load     string // "0.12 0.20 0.18"
	diskUsed string // "36% of 14.3 GB"
	memUsed  string // "412 of 1895 MB (21%)"
	err      string
}

// parseHealth reads the output of healthScript; unknown parts stay empty.
func parseHealth(out string) healthInfo {
	parts := strings.Split(out, "---")
	for len(parts) < 4 {
		parts = append(parts, "")
	}
	var h healthInfo
	h.uptime = strings.TrimPrefix(strings.TrimSpace(parts[0]), "up ")
	if f := strings.Fields(parts[1]); len(f) >= 3 {
		h.load = strings.Join(f[:3], " ")
	}
	if f := strings.Fields(parts[2]); len(f) >= 5 && strings.HasSuffix(f[4], "%") {
		if kb, err := strconv.ParseFloat(f[1], 64); err == nil {
			h.diskUsed = fmt.Sprintf("%s of %s", f[4], humanKB(kb))
		}
	}
	if f := strings.Fields(parts[3]); len(f) >= 3 && f[0] == "Mem:" {
		total, e1 := strconv.Atoi(f[1])
		used, e2 := strconv.Atoi(f[2])
		if e1 == nil && e2 == nil && total > 0 {
			h.memUsed = fmt.Sprintf("%d of %d MB (%d%%)", used, total, used*100/total)
		}
	}
	return h
}

func humanKB(kb float64) string {
	switch {
	case kb >= 1024*1024*1024:
		return fmt.Sprintf("%.1f TB", kb/1024/1024/1024)
	case kb >= 1024*1024:
		return fmt.Sprintf("%.1f GB", kb/1024/1024)
	}
	return fmt.Sprintf("%.0f MB", kb/1024)
}

// healthSend runs the health script; replaced in tests.
var healthSend = func(device client.Device, username, password string) (string, error) {
	return runSSHOnce(device, username, password, "sh -c "+shellQuote(healthScript), healthTimeout)
}

type healthMsg struct {
	key  string
	name string
	info healthInfo
}

// startHealth runs the check for d in the background.
func (m model) startHealth(d client.Device) (tea.Model, tea.Cmd) {
	switch {
	case len(d.Addresses) == 0:
		m.act = actionState{err: fmt.Errorf("the machine has no IP address")}
		return m, nil
	case m.needsAccountSwitch(d):
		m.act = actionState{err: fmt.Errorf("switch to the machine's Tailscale account first (connect once with s)")}
		return m, nil
	}
	username := m.usernameForDevice(d)
	if username == "" {
		m.act = actionState{err: fmt.Errorf("no saved username: connect once with s so the login is remembered")}
		return m, nil
	}
	password := m.savedPassword(machineKey(d))
	key, name := machineKey(d), displayName(d)
	m.act = actionState{busy: true, result: "Checking " + name + "…"}
	return m, func() tea.Msg {
		out, err := healthSend(d, username, password)
		info := parseHealth(out)
		info.at = time.Now()
		if err != nil {
			info.err = firstLine(out, err)
		}
		return healthMsg{key: key, name: name, info: info}
	}
}

// handleHealth stores the result for the details panel.
func (m model) handleHealth(msg healthMsg) (tea.Model, tea.Cmd) {
	if msg.info.err != "" {
		m.act = actionState{err: fmt.Errorf("health check of %s failed: %s", msg.name, msg.info.err)}
		return m, nil
	}
	if m.health == nil {
		m.health = map[string]healthInfo{}
	}
	m.health[msg.key] = msg.info
	m.act = actionState{result: "✓ " + msg.name + ": " + msg.info.summary()}
	return m, nil
}

// summary is the one-line form for the result area.
func (h healthInfo) summary() string {
	var parts []string
	add := func(prefix, v string) {
		if v != "" {
			parts = append(parts, prefix+v)
		}
	}
	add("up ", h.uptime)
	add("load ", h.load)
	add("mem ", h.memUsed)
	add("disk ", h.diskUsed)
	if len(parts) == 0 {
		return "no data"
	}
	return strings.Join(parts, " · ")
}
