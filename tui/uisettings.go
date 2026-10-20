package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/yatsenko-ihor/ts-cli/config"
)

// UI settings, opened with F12 and stored in config.json under "ui".
// Every option defaults to ON ("symbols" for the icon style), so an empty
// config gives the compact default look.

// Setting keys in config.json.
const (
	uiGroupByAccount = "group_by_account"
	uiHideOffline    = "hide_offline"
	uiShowDetails    = "show_details"
	uiShortNames     = "short_names"
	uiIcons          = "icons"      // "symbols" (default) or "emoji"
	uiTmuxSplit      = "tmux_split" // ssh sessions in a tmux pane when running inside tmux
	uiTheme          = "theme"      // "light" (default), "dark" or "hacker"
)

// uiSettings are the display options of the machine list.
type uiSettings struct {
	groupByAccount bool   // section headers per Tailscale account (when there are several)
	hideOffline    bool   // collapse offline machines into a "N offline hidden" row
	showDetails    bool   // details panel for the highlighted machine
	shortNames     bool   // strip the tailnet suffix (.tailXXXX.ts.net) from names
	emojiIcons     bool   // 🟢/🔴/⭕️ instead of coloured ●/○ symbols
	theme          string // light, dark or hacker
	tmuxSplit      bool   // open ssh sessions in a tmux split when inside tmux
}

// loadUISettings builds the settings from the config map; absent keys mean "on".
func loadUISettings(ui map[string]string) uiSettings {
	on := func(key string) bool { return ui[key] != "off" }
	return uiSettings{
		groupByAccount: on(uiGroupByAccount),
		hideOffline:    on(uiHideOffline),
		showDetails:    on(uiShowDetails),
		shortNames:     on(uiShortNames),
		emojiIcons:     ui[uiIcons] == "emoji",
		theme:          validTheme(ui[uiTheme]),
		tmuxSplit:      on(uiTmuxSplit),
	}
}

// settingItem is one row of the settings window.
type settingItem struct {
	key   string
	title string
	desc  string
}

var settingItems = []settingItem{
	{uiGroupByAccount, "Group machines by account", "Section headers per Tailscale account (only shown when there are several accounts)"},
	{uiHideOffline, "Hide offline machines", "Collapse offline machines into one row; they stay visible while searching (H toggles)"},
	{uiShowDetails, "Details panel", "Show OS, last seen, key expiry, SSH setup of the highlighted machine when the Run panel is closed"},
	{uiShortNames, "Short names", "Strip the tailnet suffix (.tailXXXX.ts.net) from machine names"},
	{uiTmuxSplit, "SSH in a tmux split", "Inside tmux, s opens the session in a pane next to the list (a real terminal: vim, htop, mouse, scrolling)"},
	{uiTheme, "Theme", "light, dark or hacker (green on black); dark and hacker paint the whole screen"},
	{uiIcons, "Icon style", "symbols: coloured ● ○ and text flags   emoji: 🟢 🔴 ⭕️ 🔐"},
}

// value returns the current setting as stored ("on"/"off" or the icon style).
func (s uiSettings) value(key string) string {
	flag := func(v bool) string {
		if v {
			return "on"
		}
		return "off"
	}
	switch key {
	case uiGroupByAccount:
		return flag(s.groupByAccount)
	case uiHideOffline:
		return flag(s.hideOffline)
	case uiShowDetails:
		return flag(s.showDetails)
	case uiShortNames:
		return flag(s.shortNames)
	case uiTmuxSplit:
		return flag(s.tmuxSplit)
	case uiTheme:
		return validTheme(s.theme)
	case uiIcons:
		if s.emojiIcons {
			return "emoji"
		}
		return "symbols"
	}
	return ""
}

// toggled returns the settings with key flipped.
func (s uiSettings) toggled(key string) uiSettings {
	switch key {
	case uiGroupByAccount:
		s.groupByAccount = !s.groupByAccount
	case uiHideOffline:
		s.hideOffline = !s.hideOffline
	case uiShowDetails:
		s.showDetails = !s.showDetails
	case uiShortNames:
		s.shortNames = !s.shortNames
	case uiTmuxSplit:
		s.tmuxSplit = !s.tmuxSplit
	case uiTheme:
		s.theme = nextTheme(validTheme(s.theme))
	case uiIcons:
		s.emojiIcons = !s.emojiIcons
	}
	return s
}

// uiSavedMsg is returned when a setting has been written to config.json.
type uiSavedMsg struct{ err error }

func saveUISetting(key, value string) tea.Cmd {
	return func() tea.Msg {
		return uiSavedMsg{err: config.Update(func(cfg *config.Config) error {
			if cfg.UI == nil {
				cfg.UI = map[string]string{}
			}
			cfg.UI[key] = value
			return nil
		})}
	}
}

// applySetting flips a setting, refreshes the list for it and persists it.
func (m model) applySetting(key string) (tea.Model, tea.Cmd) {
	m.set = m.set.toggled(key)
	if key == uiTheme {
		applyTheme(m.set.theme)
	}
	m.refilterKeepCursor()
	return m, saveUISetting(key, m.set.value(key))
}

// handleSettings processes keys in the F12 settings window.
func (m model) handleSettings(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "f12":
		m.settingsMode = false
	case "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		if m.settingsCursor > 0 {
			m.settingsCursor--
		}
	case "down", "j":
		if m.settingsCursor < len(settingItems)-1 {
			m.settingsCursor++
		}
	case "enter", " ", "right", "left", "l", "h":
		return m.applySetting(settingItems[m.settingsCursor].key)
	}
	return m, nil
}

// renderSettings renders the F12 window.
func (m model) renderSettings() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(fmt.Sprintf("Settings (ts-cli v%s)", m.version)))
	b.WriteString("\n\n")

	width := 78
	if m.width > 0 && m.width < 90 {
		width = m.width - 10
		if width < 40 {
			width = 40
		}
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(cur.border).
		Padding(1, 2).
		Width(width)

	var c strings.Builder
	for i, it := range settingItems {
		val := m.set.value(it.key)
		var mark string
		switch {
		case it.key == uiIcons, it.key == uiTheme:
			mark = "‹" + val + "›"
		case val == "on":
			mark = "[x]"
		default:
			mark = "[ ]"
		}
		line := fmt.Sprintf("%s %s", mark, it.title)
		if i == m.settingsCursor {
			c.WriteString(selectedStyle.Render("▸ " + line))
		} else {
			c.WriteString(normalStyle.Render("  " + line))
		}
		c.WriteString("\n")
		c.WriteString(grayItalicStyle.Render("      " + truncateForWidth(it.desc, width-12)))
		if i < len(settingItems)-1 {
			c.WriteString("\n\n")
		}
	}
	b.WriteString(box.Render(c.String()))
	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render("↑/k up • ↓/j down • enter/space toggle • esc/F12 close • saved to config.json"))
	return b.String()
}
