package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderDeviceList renders the scrollable machine list panel.
func (m model) renderDeviceList() string {
	var content strings.Builder

	// In-frame content starts with scope/search. The title is on the border.
	searchScope := "all"
	if m.list.selectedProfile != "" {
		searchScope = m.list.selectedProfile
	}
	content.WriteString(grayItalicStyle.Render(fmt.Sprintf("Search in: %s", searchScope)))
	content.WriteString("\n")
	if m.input.mode == inputSearch {
		content.WriteString(searchLabelStyle.Render("> Search: ") + searchQueryStyle.Render(renderEditable(m.list.searchQuery, m.list.searchCursor, false, "_", 0)))
		content.WriteString("\n")
	} else if m.list.searchQuery != "" {
		content.WriteString(searchLabelStyle.Render("Search: ") + searchQueryStyle.Render(m.list.searchQuery))
		content.WriteString("\n")
	}
	content.WriteString("\n")

	rows := m.listRows()
	capacity := m.listCapacity()
	top := windowTop(rows, cursorRow(rows, m.list.cursor), m.list.viewportTop, capacity)
	end := top + capacity
	if end > len(rows) {
		end = len(rows)
	}

	hasMarks := len(m.list.marked) > 0
	cols := m.listColumns(hasMarks)
	inner := getPanelInnerWidth(m.getMachineListWidth())
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(cur.header)

	if top > 0 {
		content.WriteString(normalStyle.Render("  ↑ more above ↑"))
		content.WriteString("\n")
	}
	if len(rows) == 0 {
		content.WriteString(grayItalicStyle.Render("  No machines match"))
		content.WriteString("\n")
	}
	for i := top; i < end; i++ {
		r := rows[i]
		switch r.kind {
		case rowDevice:
			content.WriteString(m.renderListDevice(r.idx, cols, hasMarks))
		case rowHeader:
			text := truncateForWidth("── "+r.text+" ", inner-2)
			content.WriteString("  " + headerStyle.Render(text+strings.Repeat("─", max(0, inner-2-lipgloss.Width(text)))))
		case rowNote:
			content.WriteString("  " + grayItalicStyle.Render(truncateForWidth(r.text, inner-2)))
		}
		content.WriteString("\n")
	}
	if end < len(rows) {
		content.WriteString(normalStyle.Render("  ↓ more below ↓"))
	}

	// Frame
	style := listStyle
	if m.run.visible {
		_, panelHeight := m.getHistoryPanelSize()
		style = style.Height(panelHeight*2 + panelBorderWidth)
	} else if h := m.listFrameHeight(); h > 0 {
		style = style.Height(h)
	}
	borderColor := cur.border
	if m.run.visible && m.activeFocus == focusList {
		borderColor = cur.borderFocus
	}
	panel := style.Width(m.getMachineListWidth()).BorderForeground(borderColor).Render(content.String())

	// Title: counts for the whole profile (hidden machines included) and sort mode
	online, offline := m.statusCounts()
	focused := m.activeFocus == focusList
	labelColor := cur.header
	if focused {
		labelColor = cur.headerStrong
	}
	dot := func(icon string, color lipgloss.Color, n int) string {
		return lipgloss.NewStyle().Bold(true).Foreground(color).Render(fmt.Sprintf(" %s %d", icon, n))
	}
	onIcon, offIcon := "●", "●"
	if m.set.emojiIcons {
		onIcon, offIcon = "🟢", "🔴"
	}
	title := lipgloss.NewStyle().Bold(focused).Foreground(labelColor).Render("[1] Machines") +
		dot(onIcon, colorOnline, online) + dot(offIcon, colorOffline, offline) +
		lipgloss.NewStyle().Foreground(cur.headerStrong).Render(fmt.Sprintf(" ⇅ %s", m.list.sort.String()))
	if n := len(m.markedDevices()); n > 0 {
		title += lipgloss.NewStyle().Bold(true).Foreground(colorSel).Render(fmt.Sprintf(" ✔ %d selected", n))
	}
	return applyFrameTitleRaw(panel, title, borderColor)
}

// listFrameHeight is the content height of the list frame in the normal
// (Run panel closed) layout, or 0 for "as tall as the content".
func (m model) listFrameHeight() int {
	if m.height > 0 {
		if h := m.height - 25; h > 10 {
			return h
		}
	}
	return 0
}
