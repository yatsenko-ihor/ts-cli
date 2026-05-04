package tui

import "github.com/charmbracelet/lipgloss"

// UI Styles for the TUI application
// All lipgloss styles are defined here for centralized style management

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#3F3F3F"))

	selectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#2D6A8C")).
			Bold(true).
			PaddingLeft(2)

	normalStyle = lipgloss.NewStyle().
			PaddingLeft(2)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666666"))

	listStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#7A7A7A")).
			Padding(1, 2)

	detailStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#7A7A7A")).
			Padding(1).
			MarginTop(1)

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#B22222")).
			Bold(true).
			MarginTop(1)

	searchLabelStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#5B6B47"))

	searchQueryStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#5B6B47"))

	grayItalicStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666666")).
			Italic(true)

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#2F6F3A"))

	infoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8A6D3B"))

	promptLabelStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#3F3F3F"))

	promptInputStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#2D6A8C"))
)

// Layout constants for panel sizing and spacing
const (
	panelBorderWidth       = 2 // left+right or top+bottom border chars
	panelHorizontalPadding = 4 // horizontal padding from Padding(1,2)
	panelVerticalPadding   = 2 // vertical padding from Padding(1,2)
	splitRightSpacerWidth  = 0 // no extra spacer: right panels should reach terminal edge
	splitTerminalSlack     = 0 // no reserved slack: consume full available split width

	listFrameTitle    = "[1] List machines"
	historyFrameTitle = "[2] Commands over SSH History"
	outputFrameTitle  = "[3] Command Output"
)
