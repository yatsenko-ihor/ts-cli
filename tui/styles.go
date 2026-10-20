package tui

import "github.com/charmbracelet/lipgloss"

// UI Styles for the TUI application
// The shared styles are declared here and (re)built from the active theme
// in theme.go (applyTheme).

var (
	titleStyle       lipgloss.Style
	selectedStyle    lipgloss.Style
	normalStyle      lipgloss.Style
	helpStyle        lipgloss.Style
	listStyle        lipgloss.Style
	errorStyle       lipgloss.Style
	searchLabelStyle lipgloss.Style
	searchQueryStyle lipgloss.Style
	grayItalicStyle  lipgloss.Style
	successStyle     lipgloss.Style
	infoStyle        lipgloss.Style
)

// Layout constants for panel sizing and spacing
const (
	panelBorderWidth       = 2 // left+right or top+bottom border chars
	panelHorizontalPadding = 4 // horizontal padding from Padding(1,2)
	panelVerticalPadding   = 2 // vertical padding from Padding(1,2)
	splitRightSpacerWidth  = 0 // no extra spacer: right panels should reach terminal edge
	splitTerminalSlack     = 0 // no reserved slack: consume full available split width

	runFrameTitle    = "[2] Run"
	outputFrameTitle = "[3] Output"
)
