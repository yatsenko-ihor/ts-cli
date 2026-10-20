package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Colour themes. light is the original look and leaves the terminal's own
// background alone; dark and hacker paint the whole screen so the palette is
// readable whatever the terminal background is.

const (
	themeLight  = "light"
	themeDark   = "dark"
	themeHacker = "hacker"
)

// themeOrder is the cycle order in the settings window.
var themeOrder = []string{themeLight, themeDark, themeHacker}

type theme struct {
	bg, text                        lipgloss.Color // "" bg = terminal default
	title, accent, muted            lipgloss.Color
	border, borderDim, borderFocus  lipgloss.Color
	errc, search, success, info     lipgloss.Color
	header, headerStrong, highlight lipgloss.Color
	online, offline, warn, dim      lipgloss.Color
	inputFg, inputBg                lipgloss.Color // text entry box
}

var themes = map[string]theme{
	themeLight: {
		title: "#3F3F3F", accent: "#2D6A8C", muted: "#666666",
		border: "#7A7A7A", borderDim: "#4F4F4F", borderFocus: "#5FAFFF",
		errc: "#B22222", search: "#5B6B47", success: "#2F6F3A", info: "#8A6D3B",
		header: "#435871", headerStrong: "#232e3b", highlight: "#7D56F4",
		online: "#4CAF50", offline: "#EF5350", warn: "#E8A33D", dim: "#8A8A8A",
		inputFg: "#FFFFFF", inputBg: "#333333",
	},
	themeDark: {
		bg: "#1a1b26", text: "#c0caf5",
		title: "#e0e6ff", accent: "#7aa2f7", muted: "#7f86a8",
		border: "#565f89", borderDim: "#3b4261", borderFocus: "#7dcfff",
		errc: "#f7768e", search: "#9ece6a", success: "#9ece6a", info: "#e0af68",
		header: "#7dcfff", headerStrong: "#bb9af7", highlight: "#bb9af7",
		online: "#9ece6a", offline: "#f7768e", warn: "#e0af68", dim: "#737aa2",
		inputFg: "#c0caf5", inputBg: "#33374f",
	},
	themeHacker: {
		bg: "#000000", text: "#00ff41",
		title: "#39ff14", accent: "#39ff14", muted: "#00a82d",
		border: "#008f11", borderDim: "#004d0a", borderFocus: "#39ff14",
		errc: "#ff3b3b", search: "#7cfc00", success: "#39ff14", info: "#c8ff00",
		header: "#00ff41", headerStrong: "#b6ffb0", highlight: "#39ff14",
		online: "#39ff14", offline: "#ff3b3b", warn: "#ffb000", dim: "#007a1f",
		inputFg: "#39ff14", inputBg: "#0b2a0b",
	},
}

// cur is the active palette. Only the UI goroutine changes it (applyTheme).
var cur = themes[themeLight]

// validTheme returns name when it is a known theme, else the default.
func validTheme(name string) string {
	if _, ok := themes[name]; ok {
		return name
	}
	return themeLight
}

// nextTheme is the theme after name in the cycle.
func nextTheme(name string) string {
	for i, n := range themeOrder {
		if n == name {
			return themeOrder[(i+1)%len(themeOrder)]
		}
	}
	return themeOrder[0]
}

// applyTheme makes name the active theme and rebuilds every shared style.
func applyTheme(name string) {
	cur = themes[validTheme(name)]

	colorOnline, colorOffline, colorWarn, colorDim, colorSel = cur.online, cur.offline, cur.warn, cur.dim, cur.accent

	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(cur.title)
	selectedStyle = lipgloss.NewStyle().Foreground(cur.accent).Bold(true).PaddingLeft(2)
	normalStyle = lipgloss.NewStyle().PaddingLeft(2)
	helpStyle = lipgloss.NewStyle().Foreground(cur.muted)
	listStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cur.border).Padding(1, 2)
	errorStyle = lipgloss.NewStyle().Foreground(cur.errc).Bold(true).MarginTop(1)
	searchLabelStyle = lipgloss.NewStyle().Bold(true).Foreground(cur.search)
	searchQueryStyle = lipgloss.NewStyle().Bold(true).Foreground(cur.search)
	grayItalicStyle = lipgloss.NewStyle().Foreground(cur.muted).Italic(true)
	successStyle = lipgloss.NewStyle().Foreground(cur.success)
	infoStyle = lipgloss.NewStyle().Foreground(cur.info)
}

func init() { applyTheme(themeLight) }

// paintScreen fills the whole width x height screen with the theme's
// background and default text colour. The inner styles end with a reset
// (ESC[0m) that would drop the background, so it is re-applied after each one.
// It is a no-op for themes without a background.
func paintScreen(out string, width, height int) string {
	if cur.bg == "" {
		return out
	}
	probe := lipgloss.NewStyle().Foreground(cur.text).Background(cur.bg).Render("x")
	seq := probe[:strings.Index(probe, "x")]
	if seq == "" { // no colour support (NO_COLOR, dumb terminal)
		return out
	}
	const reset = "\x1b[0m"

	lines := strings.Split(out, "\n")
	for len(lines) < height {
		lines = append(lines, "")
	}
	for i, l := range lines {
		pad := width - lipgloss.Width(l)
		if pad < 0 {
			pad = 0
		}
		lines[i] = seq + strings.ReplaceAll(l, reset, reset+seq) + strings.Repeat(" ", pad) + reset
	}
	return strings.Join(lines, "\n")
}
