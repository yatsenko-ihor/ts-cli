package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/yatsenko-ihor/ts-cli/client"
)

// The details panel shows everything about the highlighted machine that the
// compact list row leaves out. It appears to the right of the list while the
// Run panel is closed (switch it off in the F12 settings).

const detailsMinTerminalWidth = 100

// detailsVisible reports whether the details panel is shown.
func (m model) detailsVisible() bool {
	return m.set.showDetails && !m.run.visible && m.width >= detailsMinTerminalWidth
}

// ago renders a duration since t as "just now", "5 min ago", "3 days ago".
func ago(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h ago", int(d.Hours()))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	}
	return fmt.Sprintf("%d months ago", int(d.Hours()/24/30))
}

// keyExpiryText describes the device key expiry.
func keyExpiryText(d client.Device) string {
	switch {
	case d.KeyExpiryDisabled:
		return "never expires"
	case d.Expires.IsZero():
		return "—"
	case d.Expires.Before(time.Now()):
		return "EXPIRED " + ago(d.Expires)
	}
	days := int(time.Until(d.Expires).Hours() / 24)
	return fmt.Sprintf("%s (in %d days)", d.Expires.Local().Format("2006-01-02"), days)
}

// sshSetupText describes what is saved for connecting to the machine.
func (m model) sshSetupText(d client.Device) string {
	user := m.usernameForDevice(d)
	switch {
	case m.hasSavedPassword(d):
		return user + " · password saved"
	case m.ssh.machineCreds[machineKey(d)].username != "":
		return user + " · key-based (no password)"
	case user != "":
		return user + " (default) · nothing saved yet"
	}
	return "not set up yet"
}

// pingText describes the last explicit ping.
func (m model) pingText(d client.Device) string {
	if info, ok := m.pingInfo[machineKey(d)]; ok {
		return info.text()
	}
	at := m.pingAt[machineKey(d)]
	switch m.ping[machineKey(d)] {
	case pingOK:
		return "replied " + ago(at)
	case pingFail:
		return "no reply " + ago(at)
	}
	return "not pinged (p)"
}

// statusText describes the machine's state and where it comes from.
func (m model) statusText(d client.Device) string {
	switch {
	case m.ping[machineKey(d)] == pingOK && !isDeviceOnline(d):
		return "reachable (ping), API says offline"
	case m.ping[machineKey(d)] == pingFail:
		return "no reply to ping"
	case d.ConnectedToControl:
		return "online (connected to control)"
	case isDeviceOnline(d):
		return "online (seen " + ago(d.LastSeen) + ")"
	}
	return "offline (last seen " + ago(d.LastSeen) + ")"
}

// renderDetailsPanel renders the panel next to the list.
func (m model) renderDetailsPanel(width, height int) string {
	label := lipgloss.NewStyle().Foreground(colorDim)
	inner := getPanelInnerWidth(width)
	row := func(name, value string) string {
		v := truncateForWidth(value, inner-12)
		return label.Render(padRight(name, 11)) + " " + v
	}

	var b strings.Builder
	d, ok := m.targetDevice()
	if !ok {
		b.WriteString(grayItalicStyle.Render("No machine selected"))
	} else {
		b.WriteString(lipgloss.NewStyle().Bold(true).Render(truncateForWidth(shortHost(displayName(d)), inner)))
		b.WriteString("\n")
		if full := displayName(d); full != shortHost(full) {
			b.WriteString(label.Render(truncateForWidth(full, inner)))
			b.WriteString("\n")
		}
		b.WriteString("\n")
		b.WriteString(row("Status", m.styledStatus(d)+" "+m.statusText(d)) + "\n")
		b.WriteString(row("Ping", m.pingText(d)) + "\n")
		if p, ok := m.peerFor(d); ok {
			b.WriteString(row("Path", p.pathText()) + "\n")
			if t := p.trafficText(); t != "" {
				b.WriteString(row("Traffic", t) + "\n")
			}
			if p.ssh {
				b.WriteString(row("Tailscale SSH", "offered by the machine") + "\n")
			}
		}
		for i, a := range d.Addresses {
			name := ""
			if i == 0 {
				name = "Address"
			}
			b.WriteString(row(name, a) + "\n")
		}
		os := d.OS
		if d.ClientVersion != "" {
			os += "  ·  client " + strings.SplitN(d.ClientVersion, "-", 2)[0]
		}
		if d.UpdateAvailable {
			os += " (update available)"
		}
		b.WriteString(row("OS", os) + "\n")
		acct := d.AccountName
		if d.AccountTailnet != "" && d.AccountTailnet != d.AccountName {
			acct += " (" + d.AccountTailnet + ")"
		}
		b.WriteString(row("Account", acct) + "\n")
		b.WriteString(row("Last seen", map[bool]string{true: "now (connected)", false: ago(d.LastSeen)}[d.ConnectedToControl]) + "\n")
		if !d.Created.IsZero() {
			b.WriteString(row("Added", d.Created.Local().Format("2006-01-02")) + "\n")
		}
		b.WriteString(row("Key expiry", keyExpiryText(d)) + "\n")
		if len(d.Tags) > 0 {
			b.WriteString(row("Tags", strings.Join(d.Tags, ", ")) + "\n")
		}
		b.WriteString("\n")
		b.WriteString(row("SSH", m.sshSetupText(d)) + "\n")
		b.WriteString(row("Run", fmt.Sprintf("%s · %s", plainCount(len(m.run.scripts), "script"), plainCount(len(m.run.commands), "saved command"))))
	}

	borderColor := cur.border
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(1, 2).
		Width(width)
	if height > 0 {
		style = style.Height(height)
	}
	title := lipgloss.NewStyle().Foreground(cur.header).Render("Details")
	return applyFrameTitleRaw(style.Render(b.String()), title, borderColor)
}

func plainCount(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// detailsWidth is the content width of the details panel.
func (m model) detailsWidth() int {
	w := m.width - m.getMachineListWidth() - 5
	if w < 28 {
		w = 28
	}
	return w
}
