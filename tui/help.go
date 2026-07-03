package tui

import (
	_ "embed"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

//go:embed help.txt
var helpContent string

func (m model) renderHelp() string {
	width := m.width - 4
	if width < 40 {
		width = 40
	}
	height := m.height - 4
	if height < 10 {
		height = 10
	}

	// Build help view with scroll support
	lines := strings.Split(helpContent, "\n")

	// Apply scroll offset
	visibleLines := height - 4 // account for border + title + footer
	if m.helpScroll > len(lines)-visibleLines {
		m.helpScroll = len(lines) - visibleLines
	}
	if m.helpScroll < 0 {
		m.helpScroll = 0
	}

	end := m.helpScroll + visibleLines
	if end > len(lines) {
		end = len(lines)
	}
	visible := lines[m.helpScroll:end]

	content := strings.Join(visible, "\n")

	// Add scroll indicator
	footer := ""
	if len(lines) > visibleLines {
		footer = "\n" + lipgloss.NewStyle().Faint(true).Render("↑↓ scroll • Esc/F1 close")
	} else {
		footer = "\n" + lipgloss.NewStyle().Faint(true).Render("Esc/F1 close")
	}

	style := lipgloss.NewStyle().
		Width(width).
		Height(height).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 2)

	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		style.Render(content+footer))
}
