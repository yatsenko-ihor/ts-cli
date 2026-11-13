package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yatsenko-ihor/ts-cli/client"
)

// Actions on a machine of another Tailscale account (reboot, health check,
// file manager) switch this computer's account themselves, say so on the main
// screen, and then carry on, instead of asking the user to do it.

// switchAccountFn runs `tailscale switch`; a seam for tests.
var switchAccountFn = switchTailscaleAccount

// ensureAccount runs next at once when the daemon is on the machine's
// account, otherwise switches first and runs next once the switch is done.
func (m model) ensureAccount(d client.Device, next func(model) (tea.Model, tea.Cmd)) (tea.Model, tea.Cmd) {
	if !m.needsAccountSwitch(d) {
		return next(m)
	}
	m.pendingSwitch = next
	m.notify.sshError = nil
	m.notify.info = fmt.Sprintf("Switching the Tailscale account to %s to reach %s…", d.AccountTailnet, displayName(d))
	tailnet := d.AccountTailnet
	return m, func() tea.Msg {
		return accountSwitchedMsg{accountName: tailnet, err: switchAccountFn(tailnet)}
	}
}
