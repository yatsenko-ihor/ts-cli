package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yatsenko-ihor/ts-cli/client"
)

// The Machine tab of the Run panel: actions on the highlighted machine
// through the Tailscale API (key expiry, rename, tags, delete) and a ping
// that reports the path and latency. They always address the highlighted
// machine only, never the marked set: a destructive action on many machines
// at once is too easy to trigger by accident.

// actionState is the result area under the Machine tab's rows.
type actionState struct {
	result string // last result line (success)
	err    error  // last failure
	busy   bool   // an API call or ping is in flight
}

// machineAction is one row of the Machine tab.
type machineAction struct {
	title  string
	desc   string
	danger bool
	run    func(m model, d client.Device) (tea.Model, tea.Cmd)
}

// machineActions lists the actions for d; the key-expiry row depends on its
// current state.
func machineActions(d client.Device) []machineAction {
	expiry := machineAction{
		title: "Disable key expiry",
		desc:  "The device key never expires. (The API cannot extend a key; to renew one, log in again on the device.)",
		run: func(m model, d client.Device) (tea.Model, tea.Cmd) {
			return m.startAPIAction(d, "Key expiry disabled for "+displayName(d), func(c *client.Client) error {
				return c.SetKeyExpiryDisabled(d.ID, true)
			})
		},
	}
	if d.KeyExpiryDisabled {
		expiry.title = "Enable key expiry"
		expiry.desc = "The device key expires again after the tailnet's key lifetime."
		expiry.run = func(m model, d client.Device) (tea.Model, tea.Cmd) {
			return m.startAPIAction(d, "Key expiry enabled for "+displayName(d), func(c *client.Client) error {
				return c.SetKeyExpiryDisabled(d.ID, false)
			})
		}
	}
	return []machineAction{
		{
			title: "Ping with details",
			desc:  "tailscale ping: direct or through a relay, and the latency",
			run: func(m model, d client.Device) (tea.Model, tea.Cmd) {
				m.act = actionState{busy: true, result: "Pinging " + displayName(d) + "…"}
				return m, pingDetailCmd(d)
			},
		},
		expiry,
		{
			title:  "Expire key now",
			desc:   "The device is logged out and must authenticate again",
			danger: true,
			run: func(m model, d client.Device) (tea.Model, tea.Cmd) {
				return m.confirmByName(d, "Expire the key of "+displayName(d), func(m model) (tea.Model, tea.Cmd) {
					return m.startAPIAction(d, "Key expired for "+displayName(d), func(c *client.Client) error { return c.ExpireKey(d.ID) })
				})
			},
		},
		{
			title: "Rename…",
			desc:  "Set the machine name (a DNS label)",
			run: func(m model, d client.Device) (tea.Model, tea.Cmd) {
				current := shortHost(displayName(d))
				m.input = textInput{
					mode: inputText, label: "Rename " + current, value: current, cursor: len([]rune(current)),
					hint: "Letters, digits and hyphens",
					onSubmit: func(m model, v string) (tea.Model, tea.Cmd) {
						m.input = textInput{}
						name := strings.TrimSpace(v)
						if name == "" || name == current {
							return m, nil
						}
						return m.startAPIAction(d, fmt.Sprintf("Renamed %s to %s", current, name), func(c *client.Client) error {
							return c.RenameDevice(d.ID, name)
						})
					},
				}
				return m, nil
			},
		},
		{
			title: "Edit tags…",
			desc:  "Replace the tags (tag:name, comma separated); the ACL must allow them",
			run: func(m model, d client.Device) (tea.Model, tea.Cmd) {
				current := strings.Join(d.Tags, ", ")
				m.input = textInput{
					mode: inputText, label: "Tags of " + shortHost(displayName(d)), value: current, cursor: len([]rune(current)),
					hint: "e.g. tag:server, tag:prod",
					onSubmit: func(m model, v string) (tea.Model, tea.Cmd) {
						m.input = textInput{}
						tags := client.NormalizeTags(v)
						if len(tags) == 0 {
							m.act = actionState{err: fmt.Errorf("at least one tag is required")}
							return m, nil
						}
						return m.startAPIAction(d, "Tags set: "+strings.Join(tags, ", "), func(c *client.Client) error {
							return c.SetTags(d.ID, tags)
						})
					},
				}
				return m, nil
			},
		},
		{
			title:  "Delete from tailnet…",
			desc:   "Removes the machine from Tailscale; it must log in again to return",
			danger: true,
			run: func(m model, d client.Device) (tea.Model, tea.Cmd) {
				return m.confirmByName(d, "Delete "+displayName(d)+" from the tailnet", func(m model) (tea.Model, tea.Cmd) {
					return m.startAPIAction(d, displayName(d)+" deleted", func(c *client.Client) error { return c.DeleteDevice(d.ID) })
				})
			},
		},
	}
}

// confirmByName asks the user to type the machine's short name before a
// destructive action runs.
func (m model) confirmByName(d client.Device, what string, then func(model) (tea.Model, tea.Cmd)) (tea.Model, tea.Cmd) {
	name := shortHost(displayName(d))
	m.input = textInput{
		mode: inputText, label: what + "?", hint: "Type " + name + " to confirm",
		onSubmit: func(m model, v string) (tea.Model, tea.Cmd) {
			m.input = textInput{}
			if strings.TrimSpace(v) != name {
				m.act = actionState{err: fmt.Errorf("not confirmed: nothing was changed")}
				return m, nil
			}
			return then(m)
		},
	}
	return m, nil
}

// apiClientFor returns a client with the API key of the device's account.
func (m model) apiClientFor(d client.Device) (*client.Client, error) {
	for _, acc := range m.acct.list {
		if acc.Name == d.AccountName {
			return client.NewClient(acc.APIKey), nil
		}
	}
	return nil, fmt.Errorf("no account %q configured for this machine", d.AccountName)
}

// deviceActionMsg is the result of an API action.
type deviceActionMsg struct {
	done string
	err  error
}

// startAPIAction runs call against the device's account in the background.
func (m model) startAPIAction(d client.Device, done string, call func(*client.Client) error) (tea.Model, tea.Cmd) {
	c, err := m.apiClientFor(d)
	if err != nil {
		m.act = actionState{err: err}
		return m, nil
	}
	m.act = actionState{busy: true, result: "Working…"}
	return m, func() tea.Msg { return deviceActionMsg{done: done, err: call(c)} }
}

// handleDeviceAction shows the result and refreshes the machine list.
func (m model) handleDeviceAction(msg deviceActionMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.act = actionState{err: msg.err}
		return m, nil
	}
	m.act = actionState{result: "✓ " + msg.done}
	if m.reloading {
		return m, nil
	}
	m.reloading = true
	return m, m.reloadDevices()
}

// handlePingDetail stores the detailed ping result and updates reachability.
func (m model) handlePingDetail(msg pingDetailMsg) (tea.Model, tea.Cmd) {
	if m.pingInfo == nil {
		m.pingInfo = map[string]pingDetail{}
	}
	if m.ping == nil {
		m.ping = map[string]pingState{}
	}
	if m.pingAt == nil {
		m.pingAt = map[string]time.Time{}
	}
	m.pingInfo[msg.key] = msg.detail
	m.pingAt[msg.key] = msg.detail.at
	m.act = actionState{result: msg.name + ": " + msg.detail.text()}
	switch {
	case msg.detail.ok:
		m.ping[msg.key] = pingOK
	case msg.detail.err != "tailscale CLI not found in PATH":
		m.ping[msg.key] = pingFail
	}
	return m, nil
}

// selectedAction is the highlighted row of the Machine tab.
func (m model) selectedAction() (machineAction, client.Device, bool) {
	d, ok := m.targetDevice()
	if !ok {
		return machineAction{}, d, false
	}
	list := machineActions(d)
	i := m.tabCursor()
	if i < 0 || i >= len(list) {
		return machineAction{}, d, false
	}
	return list[i], d, true
}

// runMachineAction executes the highlighted Machine row.
func (m model) runMachineAction() (tea.Model, tea.Cmd) {
	if m.act.busy {
		return m, nil
	}
	a, d, ok := m.selectedAction()
	if !ok {
		return m, nil
	}
	m.act = actionState{}
	return a.run(m, d)
}
