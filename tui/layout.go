package tui

// Layout calculation functions for determining panel sizes and dimensions

// getMachineListWidth returns the width to use for the machine list panel
func (m model) getMachineListWidth() int {
	// Width should depend only on terminal width/mode, not content length.
	if m.showHistoryPanel {
		listWidth, _ := m.getSplitPanelWidths()
		return listWidth
	}

	if m.width <= 0 {
		return 70
	}

	// In non-split view keep list comfortably wide and stable.
	w := m.width - 4
	if w < 48 {
		w = 48
	} else if w > 90 {
		w = 90
	}

	return w
}

// getSplitPanelWidths returns content widths (excluding borders) for split view.
// It guarantees the total rendered width stays within terminal width.
func (m model) getSplitPanelWidths() (int, int) {
	if m.width <= 0 {
		return 60, 40
	}

	totalContentWidth := m.width - (panelBorderWidth * 2) - splitRightSpacerWidth - splitTerminalSlack
	if totalContentWidth < 2 {
		return 1, 1
	}

	// Preferred proportional split for normal terminals.
	listWidth := int(float64(totalContentWidth) * 0.45)
	if listWidth > 90 {
		listWidth = 90
	}
	if listWidth < 24 {
		listWidth = 24
	}

	rightWidth := totalContentWidth - listWidth
	minRightWidth := 24
	if rightWidth < minRightWidth {
		listWidth -= (minRightWidth - rightWidth)
		rightWidth = minRightWidth
	}

	if listWidth < 18 {
		listWidth = 18
		rightWidth = totalContentWidth - listWidth
	}

	if rightWidth < 18 {
		rightWidth = 18
		listWidth = totalContentWidth - rightWidth
	}

	if listWidth < 1 {
		listWidth = 1
	}
	if rightWidth < 1 {
		rightWidth = 1
	}

	return listWidth, rightWidth
}

// getHistoryPanelSize returns the width and height for the history/output panels
func (m model) getHistoryPanelSize() (int, int) {
	panelWidth := 45
	panelHeight := 25

	if m.width > 0 {
		_, rightWidth := m.getSplitPanelWidths()
		panelWidth = rightWidth
	}

	if m.height > 0 {
		availHeight := (m.height - 12) / 2
		if availHeight < 15 {
			panelHeight = 15
		} else if availHeight < panelHeight {
			panelHeight = availHeight
		}
	}

	return panelWidth, panelHeight
}

// getMaxVisibleItems returns the number of items that can be shown in the list panel
func (m model) getMaxVisibleItems() int {
	if m.height <= 0 {
		return 10
	}

	// Subtract headers, help text, borders, and extra rows
	headerRows := 7 // approximate header area rows
	footerRows := 6 // help panel rows
	if m.showHistoryPanel {
		footerRows = 2
	}

	available := m.height - headerRows - footerRows
	if available < 1 {
		return 1
	}
	return available
}
