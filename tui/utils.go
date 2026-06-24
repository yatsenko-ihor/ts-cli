package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Utility functions for text manipulation and formatting
// These functions provide common operations used across the TUI

// getPanelInnerWidth calculates the inner width of a panel after accounting for padding
func getPanelInnerWidth(outerWidth int) int {
	innerWidth := outerWidth - panelHorizontalPadding
	if innerWidth < 1 {
		return 1
	}
	return innerWidth
}

// clampToLines limits content to a maximum number of lines
func clampToLines(content string, maxLines int) string {
	if maxLines <= 0 {
		return ""
	}

	lines := strings.Split(content, "\n")
	if len(lines) <= maxLines {
		return content
	}

	return strings.Join(lines[:maxLines], "\n")
}

// applyFrameTitle injects a title into the top border line of a rendered rounded frame.
// When bold is true the title text is rendered bold.
func applyFrameTitle(frame, title string, borderColor lipgloss.Color, bold bool) string {
	lines := strings.Split(frame, "\n")
	if len(lines) == 0 {
		return frame
	}

	frameWidth := lipgloss.Width(lines[0])
	if frameWidth < 4 {
		return frame
	}

	available := frameWidth - 2 // corners
	titleSegment := " " + title + " "
	if lipgloss.Width(titleSegment) > available {
		truncated := truncateForWidth(title, available)
		titleSegment = " " + truncated
		if lipgloss.Width(titleSegment) < available {
			titleSegment += " "
		}
	}

	fill := available - lipgloss.Width(titleSegment)
	if fill < 0 {
		fill = 0
	}

	borderStyle := lipgloss.NewStyle().Foreground(borderColor)
	titleStyle := borderStyle
	if bold {
		titleStyle = titleStyle.Bold(true)
	}
	lines[0] = borderStyle.Render("╭") + titleStyle.Render(titleSegment) + borderStyle.Render(strings.Repeat("─", fill)+"╮")

	return strings.Join(lines, "\n")
}

// renderTitledPanel renders a panel with a title in the border.
// When bold is true the title text is rendered bold.
func renderTitledPanel(content, title string, contentWidth, contentHeight int, borderColor lipgloss.Color, bold bool) string {
	if contentWidth < 1 {
		contentWidth = 1
	}
	if contentHeight < panelVerticalPadding {
		contentHeight = panelVerticalPadding
	}

	horizontalPad := panelHorizontalPadding / 2
	leftPad := strings.Repeat(" ", horizontalPad)
	rightPad := strings.Repeat(" ", horizontalPad)

	textWidth := contentWidth - panelHorizontalPadding
	if textWidth < 1 {
		textWidth = 1
	}

	textRows := contentHeight - panelVerticalPadding
	if textRows < 1 {
		textRows = 1
	}

	content = clampToLines(content, textRows)
	contentLines := []string{}
	if content != "" {
		contentLines = strings.Split(content, "\n")
	}

	available := contentWidth
	titleSegment := " " + title + " "
	if lipgloss.Width(titleSegment) > available {
		truncated := truncateForWidth(title, available)
		titleSegment = " " + truncated
		if lipgloss.Width(titleSegment) < available {
			titleSegment += " "
		}
	}
	fill := available - lipgloss.Width(titleSegment)
	if fill < 0 {
		fill = 0
	}

	borderStyle := lipgloss.NewStyle().Foreground(borderColor)
	titleStyle := borderStyle
	if bold {
		titleStyle = titleStyle.Bold(true)
	}
	var b strings.Builder
	b.WriteString(borderStyle.Render("╭") + titleStyle.Render(titleSegment) + borderStyle.Render(strings.Repeat("─", fill)+"╮"))
	b.WriteString("\n")

	// Top padding row
	b.WriteString(borderStyle.Render("│") + leftPad + strings.Repeat(" ", textWidth) + rightPad + borderStyle.Render("│"))
	b.WriteString("\n")

	for i := 0; i < textRows; i++ {
		line := ""
		if i < len(contentLines) {
			line = contentLines[i]
		}
		line = truncateForWidth(line, textWidth)
		lineWidth := lipgloss.Width(line)
		if lineWidth < textWidth {
			line += strings.Repeat(" ", textWidth-lineWidth)
		}
		b.WriteString(borderStyle.Render("│") + leftPad + line + rightPad + borderStyle.Render("│"))
		b.WriteString("\n")
	}

	// Bottom padding row
	b.WriteString(borderStyle.Render("│") + leftPad + strings.Repeat(" ", textWidth) + rightPad + borderStyle.Render("│"))
	b.WriteString("\n")
	b.WriteString(borderStyle.Render("╰" + strings.Repeat("─", contentWidth) + "╯"))

	return b.String()
}

// splitOutputLines splits output into lines and trims trailing empty lines
func splitOutputLines(output string) []string {
	if output == "" {
		return nil
	}

	lines := strings.Split(output, "\n")
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	return lines
}

// outputViewport calculates which lines to show in the output panel viewport
// Returns start and end indices, and whether to show scroll indicators
func outputViewport(totalLines, outputHeight, outputScroll int) (startIdx, endIdx int, showTop, showBottom bool) {
	if totalLines <= 0 {
		return 0, 0, false, false
	}

	contentHeight := outputHeight - panelVerticalPadding
	availableLines := contentHeight
	if availableLines < 1 {
		availableLines = 1
	}

	startIdx = outputScroll
	if startIdx < 0 {
		startIdx = 0
	}
	if startIdx >= totalLines {
		startIdx = totalLines - 1
	}

	showTop = startIdx > 0
	lineBudget := availableLines
	if showTop {
		lineBudget--
	}
	if lineBudget < 1 {
		lineBudget = 1
	}

	endIdx = startIdx + lineBudget
	if endIdx > totalLines {
		endIdx = totalLines
	}

	showBottom = endIdx < totalLines
	if showBottom && lineBudget > 1 {
		endIdx-- // reserve one row for bottom indicator
		if endIdx < startIdx {
			endIdx = startIdx
		}
	}

	return startIdx, endIdx, showTop, showBottom
}

// truncateForWidth truncates a string to fit within a maximum width
func truncateForWidth(s string, max int) string {
	if max <= 0 {
		return ""
	}

	if lipgloss.Width(s) <= max {
		return s
	}

	ellipsis := "..."
	if max <= lipgloss.Width(ellipsis) {
		out := make([]rune, 0, len(s))
		for _, r := range s {
			candidate := append(out, r)
			if lipgloss.Width(string(candidate)) > max {
				break
			}
			out = candidate
		}
		return string(out)
	}

	targetWidth := max - lipgloss.Width(ellipsis)
	out := make([]rune, 0, len(s))
	for _, r := range s {
		candidate := append(out, r)
		if lipgloss.Width(string(candidate)) > targetWidth {
			break
		}
		out = candidate
	}

	return string(out) + ellipsis
}

// handleFrameShortcut handles number key shortcuts for switching panel focus.
// Returns true if the key was consumed.
func (m *model) handleFrameShortcut(key string) bool {
	switch key {
	case keyFocusList:
		m.activeFocus = focusList
		return true
	case keyFocusHistory:
		if m.hist.visible {
			m.activeFocus = focusHistory
			return true
		}
	case keyFocusOutput:
		if m.hist.visible {
			m.activeFocus = focusOutput
			m.hist.outputScroll = 0
			return true
		}
	}
	return false
}

// ensureOutputCursorVisible adjusts outputScroll so the cursor line is visible.
func (m *model) ensureOutputCursorVisible() {
	lines := splitOutputLines(m.hist.commandOutput)
	if len(lines) == 0 {
		return
	}
	_, outputHeight := m.getHistoryPanelSize()
	if outputHeight <= 0 {
		return
	}
	if m.hist.outputCursor < m.hist.outputScroll {
		m.hist.outputScroll = m.hist.outputCursor
	} else if m.hist.outputCursor >= m.hist.outputScroll+outputHeight {
		m.hist.outputScroll = m.hist.outputCursor - outputHeight + 1
	}
}
