package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Colour themes. light is the original look and leaves the terminal's own
// background alone; dark and hacker paint the whole screen so the palette is
// readable whatever the terminal background is.

const (
	themeLight      = "light"
	themeDark       = "dark"
	themeHacker     = "hacker"
	themeDarkPastel = "dark-pastel"
)

// themeOrder is the cycle order in the settings window.
var themeOrder = []string{themeLight, themeDark, themeDarkPastel, themeHacker}

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
	// Near-black with a soft green-grey text; green is kept for the cursor and
	// the online state, the rest is cyan, amber and red so not everything is green.
	themeHacker: {
		bg: "#0a0d0a", text: "#b4c8b8",
		title: "#d8f0dc", accent: "#3dff7a", muted: "#6b8570",
		border: "#2f5d4a", borderDim: "#1c2f27", borderFocus: "#00d7c8",
		errc: "#ff4d5e", search: "#00d7c8", success: "#3dff7a", info: "#e6c34a",
		header: "#00d7c8", headerStrong: "#e0fff0", highlight: "#00d7c8",
		online: "#3dff7a", offline: "#ff4d5e", warn: "#ffb000", dim: "#56705c",
		inputFg: "#e0fff0", inputBg: "#16241c",
	},
	// Soft pastels on a deep slate background.
	themeDarkPastel: {
		bg: "#1e2230", text: "#d6dbea",
		title: "#eef0f8", accent: "#a6c8ff", muted: "#8a90a8",
		border: "#4a5068", borderDim: "#343950", borderFocus: "#c9b6ff",
		errc: "#ffadad", search: "#b5ead7", success: "#b5ead7", info: "#ffe5b4",
		header: "#c9b6ff", headerStrong: "#ffd6e8", highlight: "#ffc6e0",
		online: "#b5ead7", offline: "#ffadad", warn: "#ffd6a5", dim: "#6f7590",
		inputFg: "#eef0f8", inputBg: "#2c3146",
	},
}

// cur is the active palette. Only the UI goroutine changes it (applyTheme).
var cur = themes[themeLight]

// validTheme returns name when it is a known theme, else the default.
func validTheme(name string) string {
	if _, ok := lookupTheme(name); ok {
		return name
	}
	return themeLight
}

// lookupTheme finds a built-in or a custom theme (themefile.go).
func lookupTheme(name string) (theme, bool) {
	if t, ok := themes[name]; ok {
		return t, true
	}
	t, ok := customThemes[name]
	return t, ok
}

// themeNames lists the built-in themes, then the custom ones.
func themeNames() []string {
	return append(append([]string(nil), themeOrder...), customThemeOrder...)
}

// nextTheme is the theme after name in the cycle.
func nextTheme(name string) string {
	names := themeNames()
	for i, n := range names {
		if n == name {
			return names[(i+1)%len(names)]
		}
	}
	return names[0]
}

// applyTheme makes name the active theme and rebuilds every shared style.
func applyTheme(name string) {
	cur, _ = lookupTheme(validTheme(name))

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
