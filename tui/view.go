package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// View rendering functions for the TUI application
// Each function renders a specific UI component

// renderDeviceList renders the scrollable device list panel
func (m model) renderDeviceList() string {
	var listContent strings.Builder
	maxVisible := m.getMaxVisibleItems()
	splitTargetHeight := 0

	if m.showHistoryPanel {
		_, panelHeight := m.getHistoryPanelSize()

		// Match stacked right panel height (including border compensation)
		splitTargetHeight = (panelHeight * 2) + panelBorderWidth

		// Reserve lines for in-frame header/search so device rows fill remaining space.
		listContentHeight := splitTargetHeight - panelVerticalPadding
		headerLines := 2 // "Search in" + spacer line (title is in border)
		if m.searchMode || m.searchQuery != "" {
			headerLines++
		}
		maxVisible = listContentHeight - headerLines
		if m.viewportTop > 0 {
			maxVisible-- // top "more above" indicator
		}
		if maxVisible < 1 {
			maxVisible = 1
		}
	}

	// In-frame content starts with scope/search. Title is rendered on the border.
	searchScope := "all"
	if m.selectedProfile != "" {
		searchScope = m.selectedProfile
	}
	listContent.WriteString(grayItalicStyle.Render(fmt.Sprintf("Search in: %s", searchScope)))
	listContent.WriteString("\n")

	if m.searchMode {
		listContent.WriteString(searchLabelStyle.Render("> Search: ") + searchQueryStyle.Render(m.searchQuery+"_"))
		listContent.WriteString("\n")
	} else if m.searchQuery != "" {
		listContent.WriteString(searchLabelStyle.Render("Search: ") + searchQueryStyle.Render(m.searchQuery))
		listContent.WriteString("\n")
	}

	listContent.WriteString("\n")

	// Calculate visible range
	visibleStart := m.viewportTop
	visibleEnd := m.viewportTop + maxVisible
	if visibleEnd > len(m.filteredDevices) {
		visibleEnd = len(m.filteredDevices)
	}
	if m.showHistoryPanel && visibleEnd < len(m.filteredDevices) && visibleEnd > visibleStart {
		// Reserve one line for bottom "more below" indicator in split view.
		visibleEnd--
	}

	// Show scroll indicator at top if needed
	if m.viewportTop > 0 {
		listContent.WriteString(normalStyle.Render("  ↑ more above ↑"))
		listContent.WriteString("\n")
	}

	// Render visible devices
	for i := visibleStart; i < visibleEnd; i++ {
		device := m.filteredDevices[i]
		cursor := "  "
		style := normalStyle

		if m.cursor == i {
			cursor = "▶ "
			style = selectedStyle
		}

		name := device.Name
		if name == "" {
			name = device.Hostname
		}

		address := "N/A"
		if len(device.Addresses) > 0 {
			address = device.Addresses[0]
		}

		statusIcon := getStatusIcon(device)
		expiryIcon := getKeyExpiryIcon(device)

		line := fmt.Sprintf("%s%s %-28s %s", cursor, statusIcon, name, address)
		if expiryIcon != "" {
			line = line + " " + expiryIcon
		}
		listContent.WriteString(style.Render(line))
		listContent.WriteString("\n")
	}

	// Show scroll indicator at bottom if needed
	if visibleEnd < len(m.filteredDevices) {
		listContent.WriteString(normalStyle.Render("  ↓ more below ↓"))
	}

	// Render the list in a frame
	deviceListStyle := listStyle

	if m.showHistoryPanel {
		if splitTargetHeight > 0 {
			deviceListStyle = deviceListStyle.Height(splitTargetHeight)
		}
	} else if m.height > 0 {
		// Normal view - use available height
		availHeight := m.height - 25 // Account for title, details, help
		if availHeight > 10 {
			deviceListStyle = deviceListStyle.Height(availHeight)
		}
	}

	borderColor := lipgloss.Color("#7A7A7A")
	deviceListStyle = deviceListStyle.Width(m.getMachineListWidth())
	if m.showHistoryPanel && m.activeFocus == focusList {
		borderColor = lipgloss.Color("#2D6A8C")
		deviceListStyle = deviceListStyle.BorderForeground(borderColor)
	} else {
		deviceListStyle = deviceListStyle.BorderForeground(borderColor)
	}

	listPanel := deviceListStyle.Render(listContent.String())
	return applyFrameTitle(listPanel, listFrameTitle, borderColor, m.activeFocus == focusList)
}

// renderHistoryPanel renders the command history panel
func (m model) renderHistoryPanel() string {
	var historyContent strings.Builder
	historyWidth, historyHeight := m.getHistoryPanelSize()
	historyInnerWidth := getPanelInnerWidth(historyWidth)

	// Get history for current device
	target := m.getTargetDevice()
	if target < 0 || target >= len(m.filteredDevices) {
		return ""
	}

	device := m.filteredDevices[target]
	machineID := device.ID
	if machineID == "" {
		machineID = device.Hostname
	}

	machineName := device.Name
	if machineName == "" {
		machineName = device.Hostname
	}

	// Check if device is online
	online := isDeviceOnline(device)
	statusIcon := "🔴"
	if online {
		statusIcon = "🟢"
	}

	// Header - device name with icon status
	headerText := truncateForWidth(fmt.Sprintf("%s %s", machineName, statusIcon), historyInnerWidth)
	machineHeader := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#3F3F3F")).
		Render(headerText)
	historyContent.WriteString(machineHeader)
	historyContent.WriteString("\n")

	// Get unique commands from history
	var historyCommands []string
	if m.history != nil {
		historyCommands = m.history.GetUniqueCommands(machineID)
	}

	if len(historyCommands) == 0 {
		historyContent.WriteString(grayItalicStyle.Render(truncateForWidth("No command history for this device", historyInnerWidth)))
		historyContent.WriteString("\n")
		historyContent.WriteString(grayItalicStyle.Render(truncateForWidth("Press 'e' to type a new command in this panel", historyInnerWidth)))
	} else {
		contentHeight := historyHeight - panelVerticalPadding
		helpLines := 1
		if m.commandMode {
			helpLines = 2
		}
		reservedLines := 1 + 1 + helpLines + 1          // header + total + separator + help/input
		maxVisible := contentHeight - reservedLines - 2 // reserve room for top/bottom indicators
		if maxVisible < 1 {
			maxVisible = 1
		}

		// Render command list
		startIdx := 0
		if len(historyCommands) > maxVisible && m.historyCursor >= maxVisible {
			startIdx = m.historyCursor - maxVisible + 1
		}

		endIdx := startIdx + maxVisible
		if endIdx > len(historyCommands) {
			endIdx = len(historyCommands)
		}

		showTop := startIdx > 0
		showBottom := endIdx < len(historyCommands)

		maxCmdWidth := historyInnerWidth - 4
		if maxCmdWidth < 8 {
			maxCmdWidth = 8
		}

		if showTop {
			historyContent.WriteString(grayItalicStyle.Render("  ↑ more above"))
			historyContent.WriteString("\n")
		}

		for i := startIdx; i < endIdx; i++ {
			cmd := historyCommands[i]
			cursor := "  "
			style := lipgloss.NewStyle()

			if i == m.historyCursor && m.activeFocus == focusHistory {
				cursor = "▸ "
				style = lipgloss.NewStyle().Foreground(lipgloss.Color("#2D6A8C")).Bold(true)
			}

			displayCmd := truncateForWidth(cmd, maxCmdWidth)

			historyContent.WriteString(style.Render(cursor + displayCmd))
			historyContent.WriteString("\n")
		}

		if showBottom {
			historyContent.WriteString(grayItalicStyle.Render("  ↓ more below"))
			historyContent.WriteString("\n")
		}

		historyContent.WriteString(grayItalicStyle.Render(truncateForWidth(fmt.Sprintf("Total: %d commands", len(historyCommands)), historyInnerWidth)))
	}

	historyContent.WriteString("\n")
	if m.commandMode {
		historyContent.WriteString(grayItalicStyle.Render("New command:"))
		historyContent.WriteString("\n")
		maxInputWidth := historyInnerWidth - 2
		if maxInputWidth < 1 {
			maxInputWidth = 1
		}
		historyContent.WriteString(searchLabelStyle.Render("> ") + searchQueryStyle.Render(truncateForWidth(m.commandInput+"_", maxInputWidth)))
	} else {
		historyContent.WriteString(grayItalicStyle.Render(truncateForWidth("Press 'e' to type a new command • 'd' delete selected", historyInnerWidth)))
	}

	borderColor := lipgloss.Color("#4F4F4F")
	if m.activeFocus == focusHistory {
		borderColor = lipgloss.Color("#2D6A8C")
	}

	return renderTitledPanel(historyContent.String(), historyFrameTitle, historyWidth, historyHeight, borderColor, m.activeFocus == focusHistory)
}

// renderOutputPanel renders the command output panel
func (m model) renderOutputPanel() string {
	outputWidth, outputHeight := m.getHistoryPanelSize()
	outputInnerWidth := getPanelInnerWidth(outputWidth)
	selectedOutputStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#2D6A8C")).
		Bold(true)

	var outputContent strings.Builder

	if m.commandOutput != "" {
		lines := splitOutputLines(m.commandOutput)
		startIdx, endIdx, showTop, showBottom := outputViewport(len(lines), outputHeight, m.outputScroll)
		cursor := m.outputCursor
		if cursor < 0 {
			cursor = 0
		}
		if len(lines) > 0 && cursor >= len(lines) {
			cursor = len(lines) - 1
		}

		if showTop {
			outputContent.WriteString(grayItalicStyle.Render("  ↑ more above"))
			outputContent.WriteString("\n")
		}

		if endIdx > startIdx {
			visibleLines := lines[startIdx:endIdx]
			for i, line := range visibleLines {
				lineIdx := startIdx + i
				if lineIdx == cursor {
					visibleLines[i] = selectedOutputStyle.Render("▸ " + truncateForWidth(line, outputInnerWidth-2))
				} else {
					visibleLines[i] = truncateForWidth(line, outputInnerWidth)
				}
			}
			outputContent.WriteString(strings.Join(visibleLines, "\n"))
		}

		if showBottom {
			if endIdx > startIdx {
				outputContent.WriteString("\n")
			}
			outputContent.WriteString(grayItalicStyle.Render("  ↓ more below"))
		}
	} else {
		outputContent.WriteString(grayItalicStyle.Render(truncateForWidth("No output yet", outputInnerWidth)))
		outputContent.WriteString("\n")
		outputContent.WriteString(grayItalicStyle.Render(truncateForWidth("Execute a command to see output here", outputInnerWidth)))
	}

	borderColor := lipgloss.Color("#4F4F4F")
	if m.activeFocus == focusOutput {
		borderColor = lipgloss.Color("#2D6A8C")
	}

	return renderTitledPanel(outputContent.String(), outputFrameTitle, outputWidth, outputHeight, borderColor, m.activeFocus == focusOutput)
}

// renderProfileSelection renders the profile selection view
func (m model) renderProfileSelection() string {
	var b strings.Builder

	title := fmt.Sprintf("Select Profile (ts-cli v%s)", m.version)
	b.WriteString(titleStyle.Render(title))
	b.WriteString("\n\n")

	listWidth := 60
	if m.width > 0 && m.width < 80 {
		listWidth = m.width - 10
		if listWidth < 40 {
			listWidth = 40
		}
	}

	profileList := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7A7A7A")).
		Padding(1, 2).
		Width(listWidth)

	var listContent strings.Builder

	// Add "All Accounts" option
	allAccountsLabel := "All Accounts"
	if m.selectedProfile == "" {
		allAccountsLabel += " ✓"
	}
	if m.profileCursor == 0 {
		listContent.WriteString(selectedStyle.Render("▸ " + allAccountsLabel))
	} else {
		listContent.WriteString(normalStyle.Render("  " + allAccountsLabel))
	}
	listContent.WriteString("\n")

	// Add individual accounts
	for i, acc := range m.accounts {
		label := acc.Name
		if acc.Tailnet != acc.Name {
			label += fmt.Sprintf(" (%s)", acc.Tailnet)
		}
		if m.selectedProfile == acc.Name {
			label += " ✓"
		}

		if m.profileCursor == i+1 {
			listContent.WriteString(selectedStyle.Render("▸ " + label))
		} else {
			listContent.WriteString(normalStyle.Render("  " + label))
		}
		if i < len(m.accounts)-1 {
			listContent.WriteString("\n")
		}
	}

	b.WriteString(profileList.Render(listContent.String()))
	b.WriteString("\n\n")

	help := "↑/k up • ↓/j down • enter select • esc/q cancel"
	b.WriteString(helpStyle.Render(help))

	return b.String()
}

// renderAccountManagement renders the account management view
func (m model) renderAccountManagement() string {
	var b strings.Builder

	title := fmt.Sprintf("Account Management (ts-cli v%s)", m.version)
	b.WriteString(titleStyle.Render(title))
	b.WriteString("\n\n")

	listWidth := 60
	if m.width > 0 && m.width < 80 {
		listWidth = m.width - 10
		if listWidth < 40 {
			listWidth = 40
		}
	}

	optionsList := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7A7A7A")).
		Padding(1, 2).
		Width(listWidth)

	var listContent strings.Builder

	// Add account option
	addLabel := "Add Account"
	if m.manageCursor == 0 {
		listContent.WriteString(selectedStyle.Render("▸ " + addLabel))
	} else {
		listContent.WriteString(normalStyle.Render("  " + addLabel))
	}

	b.WriteString(optionsList.Render(listContent.String()))
	b.WriteString("\n\n")

	help := "↑/k up • ↓/j down • enter select • esc/q cancel"
	b.WriteString(helpStyle.Render(help))

	return b.String()
}

// getHelpText returns context-sensitive help text based on current mode
func (m model) getHelpText() string {
	help := "1/2/3 frame • ↑/k up • ↓/j down • / search • enter select • s ssh • c copy • tab history • p profile • r reload • u tailscale-up • m manage"
	if m.showHistoryPanel {
		if m.activeFocus == focusHistory {
			help = "1/2/3 frame • ↑/k up • ↓/j down • e new-command • d delete • enter execute • tab/shift+tab switch • esc close"
		} else if m.activeFocus == focusOutput {
			help = "1/2/3 frame • ↑/k up • ↓/j down • c copy-full-path • n copy-name • tab/shift+tab switch • esc close"
		} else {
			help = "1/2/3 frame • ↑/k up • ↓/j down • enter select • tab/shift+tab switch • esc close"
		}
	}
	if m.sshUsername != "" && !m.showHistoryPanel {
		help += " • d clear-user"
	}
	if !m.showHistoryPanel {
		help += " • q quit"
	}
	if m.usernamePrompt {
		help = "Enter SSH username • esc cancel • enter confirm"
	} else if m.commandMode {
		help = "Type command to execute • esc cancel • enter execute"
	} else if m.searchMode {
		help = "Type to search • esc cancel • enter confirm"
	}

	return help
}

// renderHelpPanel renders the context-sensitive help bar at the bottom
func (m model) renderHelpPanel() string {
	helpPanelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7A7A7A")).
		Padding(0, 1)

	if m.width > 0 {
		panelWidth := m.width - 2
		if panelWidth < 40 {
			panelWidth = 40
		}
		helpPanelStyle = helpPanelStyle.Width(panelWidth)
	}

	return helpPanelStyle.Render(helpStyle.Render(m.getHelpText()))
}
