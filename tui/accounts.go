package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/yatsenko-ihor/ts-cli/client"
	"github.com/yatsenko-ihor/ts-cli/config"
	"github.com/yatsenko-ihor/ts-cli/util"
)

// The Accounts window (key m): add, edit, remove, show/hide and switch
// Tailscale accounts, with a health line (API key validity and expiry) for
// each one.

// keyWarnDays is how long before expiry an API key is flagged.
const keyWarnDays = 14

// checkAccountHealth is a variable so tests do not touch the network.
var checkAccountHealth = client.CheckAccount

// accountHealth is the last API key check of one account.
type accountHealth struct {
	checked bool
	h       client.Health
}

type accountHealthMsg struct {
	name string
	h    client.Health
}

// accountsChangedMsg is returned when a change to the account list was
// written (or failed).
type accountsChangedMsg struct {
	err    error
	notice string
	reload bool // the machine list must be fetched again
}

// sameTailnet compares a device's tailnet with the daemon's login. The
// status output may truncate the login ("user@"), so a prefix match either
// way counts.
func sameTailnet(a, b string) bool {
	a, b = strings.ToLower(a), strings.ToLower(b)
	return strings.HasPrefix(a, strings.TrimSuffix(b, "@")) || strings.HasPrefix(b, strings.TrimSuffix(a, "@"))
}

func (m model) isThisComputer(acc client.AccountInfo) bool {
	return m.acct.tailscaleActiveAccount != "" && sameTailnet(acc.Tailnet, m.acct.tailscaleActiveAccount)
}

// accountHidden reports whether the machines of the named account are hidden.
func (a accounts) accountHidden(name string) bool {
	for _, acc := range a.list {
		if acc.Name == name {
			return acc.Hidden
		}
	}
	return false
}

// checkAllHealth verifies every account's key in the background.
func (m model) checkAllHealth() tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(m.acct.list))
	for _, acc := range m.acct.list {
		cmds = append(cmds, checkHealthCmd(acc))
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

func checkHealthCmd(acc client.AccountInfo) tea.Cmd {
	return func() tea.Msg { return accountHealthMsg{name: acc.Name, h: checkAccountHealth(acc)} }
}

// daysLeft is the whole days until t, rounded down.
func daysLeft(t time.Time) int { return int(time.Until(t).Hours() / 24) }

// healthText is the short description shown for an account key.
func (a accounts) healthText(name string) (text string, level int) {
	hs, ok := a.health[name]
	switch {
	case !ok || !hs.checked:
		return "checking…", 0
	case hs.h.Unauth:
		return "key rejected", 2
	case !hs.h.Valid:
		return "unreachable", 1
	case hs.h.HasExpiry:
		d := daysLeft(hs.h.Expires)
		switch {
		case time.Until(hs.h.Expires) <= 0:
			return "key expired", 2
		case d < 3:
			return fmt.Sprintf("key expires in %dd", d), 2
		case d < keyWarnDays:
			return fmt.Sprintf("key expires in %dd", d), 1
		}
		return fmt.Sprintf("key ok, %dd left", d), 0
	case hs.h.ExpiryNote != "":
		return "key ok", 0
	}
	return "key ok", 0
}

// keyWarning is a one-line notice for the main view when some API key is
// expiring or rejected; "" when all is fine.
func (m model) keyWarning() string {
	var parts []string
	for _, acc := range m.acct.list {
		if text, level := m.acct.healthText(acc.Name); level > 0 && text != "unreachable" {
			parts = append(parts, fmt.Sprintf("%s: %s", acc.Name, text))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "⚠ API key: " + strings.Join(parts, "; ") + "  (m)"
}

// ─── Persisting changes ──────────────────────────────────────────────────────

func accountsFromConfig(cfg *config.Config) []client.AccountInfo {
	list := make([]client.AccountInfo, len(cfg.Accounts))
	for i, a := range cfg.Accounts {
		list[i] = client.AccountInfo{Name: a.Name, APIKey: a.APIKey, Tailnet: a.Tailnet, Active: a.Active, Hidden: a.Hidden}
	}
	return list
}

func addAccountCmd(tailnet, name, apiKey string) tea.Cmd {
	return func() tea.Msg {
		if err := client.NewClient(apiKey).ValidateAPIKey(tailnet); err != nil {
			return accountsChangedMsg{err: fmt.Errorf("API key check failed: %w", err)}
		}
		err := config.Update(func(cfg *config.Config) error {
			cfg.AddAccount(name, apiKey, tailnet, len(cfg.Accounts) == 0)
			return nil
		})
		return accountsChangedMsg{err: err, notice: "Added account " + name, reload: err == nil}
	}
}

func editAccountCmd(current, tailnet, newName, newKey string) tea.Cmd {
	return func() tea.Msg {
		if newKey != "" {
			if err := client.NewClient(newKey).ValidateAPIKey(tailnet); err != nil {
				return accountsChangedMsg{err: fmt.Errorf("API key check failed: %w", err)}
			}
		}
		err := config.Update(func(cfg *config.Config) error { return cfg.UpdateAccount(current, newName, newKey) })
		return accountsChangedMsg{err: err, notice: "Account updated", reload: err == nil && newName != ""}
	}
}

func removeAccountCmd(name string) tea.Cmd {
	return func() tea.Msg {
		err := config.Update(func(cfg *config.Config) error {
			if !cfg.RemoveAccount(name) {
				return fmt.Errorf("account '%s' not found", name)
			}
			return nil
		})
		return accountsChangedMsg{err: err, notice: "Removed account " + name, reload: err == nil}
	}
}

func hideAccountCmd(name string, hidden bool) tea.Cmd {
	return func() tea.Msg {
		err := config.Update(func(cfg *config.Config) error {
			if !cfg.SetAccountHidden(name, hidden) {
				return fmt.Errorf("account '%s' not found", name)
			}
			return nil
		})
		verb := "shown"
		if hidden {
			verb = "hidden"
		}
		return accountsChangedMsg{err: err, notice: fmt.Sprintf("%s is now %s", name, verb)}
	}
}

func useAccountCmd(name, tailnet string) tea.Cmd {
	return func() tea.Msg {
		err := config.Update(func(cfg *config.Config) error {
			if !cfg.SetActiveAccount(name) {
				return fmt.Errorf("account '%s' not found", name)
			}
			return nil
		})
		if err != nil {
			return accountsChangedMsg{err: err}
		}
		if err := switchTailscaleAccount(tailnet); err != nil {
			return accountsChangedMsg{notice: "Active account set, but this computer was not switched: tailscale switch failed"}
		}
		return accountsChangedMsg{notice: "This computer now uses " + tailnet}
	}
}

// handleAccountsChanged reloads the account list from disk after a change
// and refreshes everything that depends on it.
func (m model) handleAccountsChanged(msg accountsChangedMsg) (tea.Model, tea.Cmd) {
	m.acct.busy = false
	if msg.err != nil {
		m.acct.status, m.acct.statusErr = msg.err.Error(), true
		return m, nil
	}
	m.acct.status, m.acct.statusErr = msg.notice, false

	cfg, err := config.Load()
	if err != nil {
		m.acct.status, m.acct.statusErr = err.Error(), true
		return m, nil
	}
	old := m.acct.list
	m.acct.list = accountsFromConfig(cfg)
	m.acct.activeAccount = ""
	if a := cfg.GetActiveAccount(); a != nil {
		m.acct.activeAccount = a.Name
	}
	m.acct.tailscaleActiveAccount = getRealTailscaleAccount()
	if m.acct.manageCursor >= len(m.acct.list) {
		m.acct.manageCursor = max(0, len(m.acct.list)-1)
	}
	if m.list.selectedProfile != "" && !m.acct.hasAccount(m.list.selectedProfile) {
		m.list.selectedProfile = ""
	}

	// Drop the health of vanished accounts; check new or re-keyed ones.
	var cmds []tea.Cmd
	keep := map[string]accountHealth{}
	for _, acc := range m.acct.list {
		if hs, ok := m.acct.health[acc.Name]; ok && sameKey(old, acc) {
			keep[acc.Name] = hs
		} else {
			cmds = append(cmds, checkHealthCmd(acc))
		}
	}
	m.acct.health = keep

	if msg.reload {
		if len(m.acct.list) == 0 {
			m.list.devices = nil
		} else if !m.reloading {
			m.reloading = true
			cmds = append(cmds, m.reloadDevices())
		}
	}
	m.refilterKeepCursor()
	return m, tea.Batch(cmds...)
}

func (a accounts) hasAccount(name string) bool {
	for _, acc := range a.list {
		if acc.Name == name {
			return true
		}
	}
	return false
}

// sameKey reports whether acc existed unchanged (name, tailnet, key) in old.
func sameKey(old []client.AccountInfo, acc client.AccountInfo) bool {
	for _, o := range old {
		if o.Name == acc.Name && o.Tailnet == acc.Tailnet && o.APIKey == acc.APIKey {
			return true
		}
	}
	return false
}

// ─── Prompts ─────────────────────────────────────────────────────────────────

// backToAccounts returns to the Accounts window after a prompt is cancelled.
func backToAccounts(m model) model {
	m.acct.accountManageMode = true
	return m
}

func (m model) promptAddAccount() (tea.Model, tea.Cmd) {
	var tailnet, key string
	m.input = textInput{
		mode:     inputText,
		label:    "Add account: tailnet",
		hint:     "e.g. example.com or user@example.com",
		onCancel: backToAccounts,
		onSubmit: func(m model, v string) (tea.Model, tea.Cmd) {
			tailnet = util.SanitizeInput(v)
			if err := util.ValidateTailnet(tailnet); err != nil {
				m.input = textInput{}
				m.acct.status, m.acct.statusErr = "Invalid tailnet: "+err.Error(), true
				return m, nil
			}
			m.input = textInput{
				mode: inputText, label: "Add account: API key", masked: true,
				hint:     "Tailscale admin console → Settings → Keys (tskey-api-…)",
				onCancel: backToAccounts,
				onSubmit: func(m model, v string) (tea.Model, tea.Cmd) {
					key = util.SanitizeInput(v)
					if err := util.ValidateAPIKey(key); err != nil {
						m.input = textInput{}
						m.acct.status, m.acct.statusErr = "Invalid API key: "+err.Error(), true
						return m, nil
					}
					m.input = textInput{
						mode: inputText, label: "Add account: display name",
						hint:     "Enter keeps the tailnet as the name",
						onCancel: backToAccounts,
						onSubmit: func(m model, v string) (tea.Model, tea.Cmd) {
							name := util.SanitizeInput(v)
							if name == "" {
								name = tailnet
							} else if err := util.ValidateAccountName(name); err != nil {
								m.input = textInput{}
								m.acct.status, m.acct.statusErr = "Invalid name: "+err.Error(), true
								return m, nil
							}
							m.input = textInput{}
							m.acct.busy = true
							m.acct.status, m.acct.statusErr = "Checking API key…", false
							return m, addAccountCmd(tailnet, name, key)
						},
					}
					return m, nil
				},
			}
			return m, nil
		},
	}
	return m, nil
}

func (m model) promptEditAccount(acc client.AccountInfo) (tea.Model, tea.Cmd) {
	var newName string
	m.input = textInput{
		mode: inputText, label: "Edit account: name", value: acc.Name, cursor: len([]rune(acc.Name)),
		hint:     "Enter keeps the current name",
		onCancel: backToAccounts,
		onSubmit: func(m model, v string) (tea.Model, tea.Cmd) {
			newName = util.SanitizeInput(v)
			if newName == acc.Name {
				newName = ""
			} else if newName != "" {
				if err := util.ValidateAccountName(newName); err != nil {
					m.input = textInput{}
					m.acct.status, m.acct.statusErr = "Invalid name: "+err.Error(), true
					return m, nil
				}
			}
			m.input = textInput{
				mode: inputText, label: "Edit account: new API key", masked: true,
				hint:     "Enter keeps the current key",
				onCancel: backToAccounts,
				onSubmit: func(m model, v string) (tea.Model, tea.Cmd) {
					key := util.SanitizeInput(v)
					if key != "" {
						if err := util.ValidateAPIKey(key); err != nil {
							m.input = textInput{}
							m.acct.status, m.acct.statusErr = "Invalid API key: "+err.Error(), true
							return m, nil
						}
					}
					m.input = textInput{}
					if newName == "" && key == "" {
						return m, nil
					}
					m.acct.busy = true
					m.acct.status, m.acct.statusErr = "Saving…", false
					return m, editAccountCmd(acc.Name, acc.Tailnet, newName, key)
				},
			}
			return m, nil
		},
	}
	return m, nil
}

// ─── Keys ────────────────────────────────────────────────────────────────────

func (m model) handleAccountManagement(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	if name := m.acct.confirmDelete; name != "" {
		m.acct.confirmDelete = ""
		if key == "y" || key == "Y" {
			m.acct.busy = true
			return m, removeAccountCmd(name)
		}
		return m, nil
	}

	switch key {
	case "esc", "ctrl+c", "q":
		m.acct.accountManageMode = false
		return m, nil
	case "a":
		m.acct.accountManageMode = false
		return m.promptAddAccount()
	case "c":
		for _, acc := range m.acct.list {
			delete(m.acct.health, acc.Name)
		}
		return m, m.checkAllHealth()
	case "up", "k":
		if m.acct.manageCursor > 0 {
			m.acct.manageCursor--
		}
		return m, nil
	case "down", "j":
		if m.acct.manageCursor < len(m.acct.list)-1 {
			m.acct.manageCursor++
		}
		return m, nil
	}

	if m.acct.busy || m.acct.manageCursor >= len(m.acct.list) {
		return m, nil
	}
	acc := m.acct.list[m.acct.manageCursor]
	switch key {
	case "enter":
		m.acct.busy = true
		m.acct.status, m.acct.statusErr = "Switching…", false
		return m, useAccountCmd(acc.Name, acc.Tailnet)
	case " ", "space":
		m.acct.busy = true
		return m, hideAccountCmd(acc.Name, !acc.Hidden)
	case "e":
		m.acct.accountManageMode = false
		return m.promptEditAccount(acc)
	case "d", "delete":
		m.acct.confirmDelete = acc.Name
	}
	return m, nil
}

// ─── View ────────────────────────────────────────────────────────────────────

func (m model) renderAccountManagement() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(fmt.Sprintf("Accounts (ts-cli v%s)", m.version)))
	b.WriteString("\n\n")

	width := 100
	if m.width > 0 && m.width-6 < width {
		width = m.width - 6
	}
	if width < 50 {
		width = 50
	}

	names, tails := 0, 0
	for _, acc := range m.acct.list {
		names = max(names, lipgloss.Width(acc.Name))
		tails = max(tails, lipgloss.Width(acc.Tailnet))
	}
	// Tailnet is shown only when it differs from the name.
	showTail := false
	for _, acc := range m.acct.list {
		if acc.Tailnet != acc.Name {
			showTail = true
		}
	}

	var rows strings.Builder
	if len(m.acct.list) == 0 {
		rows.WriteString(grayItalicStyle.Render("No accounts yet. Press a to add one."))
	}
	for i, acc := range m.acct.list {
		total, online := 0, 0
		for _, d := range m.list.devices {
			if d.AccountName == acc.Name {
				total++
				if isDeviceOnline(d) {
					online++
				}
			}
		}
		text, level := m.acct.healthText(acc.Name)
		health := lipgloss.NewStyle().Foreground([]lipgloss.Color{colorDim, colorWarn, colorOffline}[level]).Render(text)

		mark := "  "
		if m.isThisComputer(acc) {
			mark = lipgloss.NewStyle().Foreground(colorOnline).Render("◉ ")
		}
		line := mark + padRight(acc.Name, names)
		if showTail {
			line += "  " + padRight(acc.Tailnet, tails)
		}
		line += fmt.Sprintf("  %3d/%-3d online  ", online, total) + health
		if acc.Hidden {
			line += grayItalicStyle.Render("  hidden")
		}

		if i == m.acct.manageCursor {
			rows.WriteString(selectedStyle.Render("▸ ") + line)
		} else {
			rows.WriteString("  " + line)
		}
		rows.WriteString("\n")
	}

	panel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(cur.border).
		Padding(1, 2).
		Width(width)
	b.WriteString(panel.Render(strings.TrimRight(rows.String(), "\n")))
	b.WriteString("\n")
	b.WriteString(grayItalicStyle.Render("◉ = this computer is logged in to it. Hidden accounts' machines are not listed."))
	b.WriteString("\n")

	switch {
	case m.acct.confirmDelete != "":
		b.WriteString(errorStyle.Render(fmt.Sprintf("Remove account %q and its API key? y/N", m.acct.confirmDelete)))
		b.WriteString("\n")
	case m.acct.status != "" && m.acct.statusErr:
		b.WriteString(errorStyle.Render(m.acct.status))
		b.WriteString("\n")
	case m.acct.status != "":
		b.WriteString(successStyle.Render("✓ " + m.acct.status))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("↑/↓ move • enter use (switch this computer) • space show/hide • a add • e edit • d remove • c re-check keys • esc close"))
	return b.String()
}
