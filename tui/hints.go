package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Key hints in the bar at the bottom of the screen. The most useful ones come
// first; when the terminal is too narrow for all of them the least useful are
// dropped, but the way to the full help and out of the program always stays.

const hintSep = " • "

// hintTail is always shown.
const hintTail = "? help" + hintSep + "q quit"

// fitHints joins as many hints as fit into width cells, then the tail.
// width <= 0 means no limit.
func fitHints(hints []string, tail string, width int) string {
	line := ""
	for _, h := range hints {
		next := h
		if line != "" {
			next = line + hintSep + h
		}
		if width > 0 && lipgloss.Width(next+hintSep+tail) > width {
			break
		}
		line = next
	}
	if line == "" {
		return tail
	}
	return line + hintSep + tail
}

// listHints are the hints of the machine list, in priority order.
func (m model) listHints() []string {
	hints := []string{
		"↑/k ↓/j move", "space mark", "s ssh", "/ search", "p ping", ": run",
		"tab run panel + machine actions", "H offline", "m accounts", "F12 settings",
		"S sort", "r reload", "c copy", "P profile", "o options", "a about",
	}
	if m.ssh.username != "" {
		hints = append(hints, "d clear-user")
	}
	return hints
}

// runTabVerb is what Enter does on the active Run-panel tab.
func runTabVerb(t runTab) string {
	switch t {
	case tabRecent:
		return "run again"
	case tabMachine:
		return "do it"
	}
	return "run"
}

// hintWidth is the room inside the help bar (border and padding excluded).
func (m model) hintWidth() int {
	if m.width <= 0 {
		return 0
	}
	return max(40, m.width-2) - 4
}

// runFocusHints are the hints while the Run panel has focus.
func (m model) runFocusHints() string {
	parts := []string{"1/2/3 frame", "[/] tabs", "↑/k ↓/j", "enter " + runTabVerb(m.run.tab), ": command", "tab/shift+tab switch", "esc close"}
	return strings.Join(parts, hintSep)
}
