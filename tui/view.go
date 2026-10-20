package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/yatsenko-ihor/ts-cli/scripts"
	"github.com/yatsenko-ihor/ts-cli/util"
)

// View rendering functions for the TUI application
// Each function renders a specific UI component

// renderDeviceList renders the scrollable device list panel
func (m model) renderDeviceList() string {
	var listContent strings.Builder
	maxVisible := m.getMaxVisibleItems()
	splitTargetHeight := 0

	if m.run.visible {
		_, panelHeight := m.getHistoryPanelSize()

		// Match stacked right panel height (including border compensation)
		splitTargetHeight = (panelHeight * 2) + panelBorderWidth

		// Reserve lines for in-frame header/search so device rows fill remaining space.
		listContentHeight := splitTargetHeight - panelVerticalPadding
		headerLines := 2 // "Search in" + spacer line (title is in border)
		if m.input.mode == inputSearch || m.list.searchQuery != "" {
			headerLines++
		}
		maxVisible = listContentHeight - headerLines
		if m.list.viewportTop > 0 {
			maxVisible-- // top "more above" indicator
		}
		if maxVisible < 1 {
			maxVisible = 1
		}
	}

	// In-frame content starts with scope/search. Title is rendered on the border.
	searchScope := "all"
	if m.list.selectedProfile != "" {
		searchScope = m.list.selectedProfile
	}
	listContent.WriteString(grayItalicStyle.Render(fmt.Sprintf("Search in: %s", searchScope)))
	listContent.WriteString("\n")

	if m.input.mode == inputSearch {
		listContent.WriteString(searchLabelStyle.Render("> Search: ") + searchQueryStyle.Render(renderEditable(m.list.searchQuery, m.list.searchCursor, false, "_", 0)))
		listContent.WriteString("\n")
	} else if m.list.searchQuery != "" {
		listContent.WriteString(searchLabelStyle.Render("Search: ") + searchQueryStyle.Render(m.list.searchQuery))
		listContent.WriteString("\n")
	}

	listContent.WriteString("\n")

	// Calculate visible range
	visibleStart := m.list.viewportTop
	visibleEnd := m.list.viewportTop + maxVisible
	if visibleEnd > len(m.list.filteredDevices) {
		visibleEnd = len(m.list.filteredDevices)
	}
	if m.run.visible && visibleEnd < len(m.list.filteredDevices) && visibleEnd > visibleStart {
		// Reserve one line for bottom "more below" indicator in split view.
		visibleEnd--
	}

	// Show scroll indicator at top if needed
	if m.list.viewportTop > 0 {
		listContent.WriteString(normalStyle.Render("  ↑ more above ↑"))
		listContent.WriteString("\n")
	}

	// Render visible devices
	hasMarks := len(m.list.marked) > 0
	cols := m.listColumns(hasMarks)
	for i := visibleStart; i < visibleEnd; i++ {
		device := m.list.filteredDevices[i]
		cursor := "  "
		style := normalStyle

		if m.list.cursor == i {
			cursor = "▶ "
			style = selectedStyle
		}

		mark := ""
		if hasMarks {
			mark = "  "
			if m.list.marked[machineKey(device)] {
				mark = "✔ "
			}
		}

		line := cursor + mark + m.deviceRow(device, cols)
		listContent.WriteString(style.Render(line) + "\n")
	}

	// Show scroll indicator at bottom if needed
	if visibleEnd < len(m.list.filteredDevices) {
		listContent.WriteString(normalStyle.Render("  ↓ more below ↓"))
	}

	// Render the list in a frame
	deviceListStyle := listStyle

	if m.run.visible {
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
	if m.run.visible && m.activeFocus == focusList {
		borderColor = lipgloss.Color("#5FAFFF")
		deviceListStyle = deviceListStyle.BorderForeground(borderColor)
	} else {
		deviceListStyle = deviceListStyle.BorderForeground(borderColor)
	}

	listPanel := deviceListStyle.Render(listContent.String())
	// Dynamic title with online/offline count and sort mode
	online := 0
	for _, d := range m.list.filteredDevices {
		if m.isReachable(d) {
			online++
		}
	}
	offline := len(m.list.filteredDevices) - online

	focused := m.activeFocus == focusList
	labelColor := lipgloss.Color("#435871")
	if focused {
		labelColor = lipgloss.Color("#232e3b")
	}
	titleLabel := lipgloss.NewStyle().Bold(focused).Foreground(labelColor).Render("[1] Machines")
	onlineSeg := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#4CAF50")).Render(fmt.Sprintf(" 🟢 %d", online))
	offlineSeg := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#EF5350")).Render(fmt.Sprintf(" 🔴 %d", offline))
	sortSeg := lipgloss.NewStyle().Foreground(lipgloss.Color("#232e3b")).Render(fmt.Sprintf(" ⇅ %s", m.list.sort.String()))

	styledTitle := titleLabel + onlineSeg + offlineSeg + sortSeg
	if n := len(m.markedDevices()); n > 0 {
		styledTitle += lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#2D6A8C")).Render(fmt.Sprintf(" ✔ %d selected", n))
	}
	return applyFrameTitleRaw(listPanel, styledTitle, borderColor)
}

// renderRunPanel renders the launcher: tabs (Scripts / Commands / Recent),
// the active tab's rows, and the ad-hoc ":" command line when open.
func (m model) renderRunPanel() string {
	var b strings.Builder
	width, height := m.getHistoryPanelSize()
	inner := getPanelInnerWidth(width)
	focused := m.activeFocus == focusRun

	device, _ := m.targetDevice()
	targets := m.targets()

	// Header: target machine(s)
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#3F3F3F")).Render(truncateForWidth(m.targetsLabel(), inner)))
	b.WriteString("\n")

	// Tabs
	var tabs []string
	for t := runTab(0); t < tabCount; t++ {
		label := t.String()
		if t == m.run.tab {
			tabs = append(tabs, lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#2D6A8C")).Underline(true).Render(label))
		} else {
			tabs = append(tabs, grayItalicStyle.Render(label))
		}
	}
	b.WriteString(strings.Join(tabs, grayItalicStyle.Render(" │ ")))
	b.WriteString("\n")

	// Rows of the active tab
	type row struct {
		main string
		sub  string
	}
	var rows []row
	var empty string
	switch m.run.tab {
	case tabScripts:
		for _, s := range m.visibleScripts() {
			sub := s.Description
			if len(s.Args) > 0 {
				sub = strings.TrimSpace(sub + "  args: " + strings.Join(s.Args, ", "))
			}
			rows = append(rows, row{main: s.Name, sub: sub})
		}
		dir, _ := scripts.Dir()
		if m.run.scriptsErr != nil {
			empty = "Cannot read scripts: " + m.run.scriptsErr.Error()
		} else if len(m.run.scripts) == 0 {
			empty = "No scripts yet. Put executable files in " + dir
		} else if len(targets) > 1 {
			empty = "No scripts for the selected machines' OSes"
		} else {
			empty = "No scripts for this machine's OS (" + device.OS + ")"
		}
	case tabCommands:
		for _, c := range m.run.commands {
			rows = append(rows, row{main: c.Name, sub: c.Command})
		}
		empty = "No saved commands. Press n to add one, or : to run something now"
	case tabRecent:
		for _, e := range m.recentEntries() {
			main := e.Command
			if e.Kind == util.HistoryKindScript {
				main = "⚙ " + e.Command
				if len(e.Args) > 0 {
					main += " " + strings.Join(e.Args, " ")
				}
			}
			status := "✓"
			if e.ExitCode != 0 {
				status = fmt.Sprintf("✗%d", e.ExitCode)
			}
			rows = append(rows, row{main: main, sub: status + " " + e.Timestamp.Local().Format("Jan 2 15:04")})
		}
		empty = "Nothing has run on this machine yet"
	}

	contentHeight := height - panelVerticalPadding
	footerLines := 1
	if m.input.mode == inputCommand {
		footerLines = 2
	}
	// header + tabs + footer; each row takes 1 line (main) — sub shown only for the cursor row
	maxVisible := contentHeight - 2 - footerLines - 1
	if maxVisible < 1 {
		maxVisible = 1
	}

	if len(rows) == 0 {
		b.WriteString(grayItalicStyle.Render(truncateForWidth(empty, inner)))
		b.WriteString("\n")
	} else {
		cursor := m.tabCursor()
		start := 0
		if cursor >= maxVisible {
			start = cursor - maxVisible + 1
		}
		end := start + maxVisible
		if end > len(rows) {
			end = len(rows)
		}
		if start > 0 {
			b.WriteString(grayItalicStyle.Render("  ↑ more above"))
			b.WriteString("\n")
		}
		for i := start; i < end; i++ {
			r := rows[i]
			if i == cursor && focused {
				b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#2D6A8C")).Bold(true).Render("▸ " + truncateForWidth(r.main, inner-2)))
			} else if i == cursor {
				b.WriteString(lipgloss.NewStyle().Bold(true).Render("  " + truncateForWidth(r.main, inner-2)))
			} else {
				b.WriteString("  " + truncateForWidth(r.main, inner-2))
			}
			b.WriteString("\n")
		}
		if end < len(rows) {
			b.WriteString(grayItalicStyle.Render("  ↓ more below"))
			b.WriteString("\n")
		}
		if cursor >= 0 && cursor < len(rows) && rows[cursor].sub != "" {
			b.WriteString(grayItalicStyle.Render(truncateForWidth("  "+rows[cursor].sub, inner)))
			b.WriteString("\n")
		}
	}

	// Footer: ad-hoc prompt or key hints
	if m.input.mode == inputCommand {
		where := displayName(device)
		if len(targets) > 1 {
			where = fmt.Sprintf("%d machines", len(targets))
		}
		b.WriteString(grayItalicStyle.Render("Run on " + truncateForWidth(where, inner-7) + ":"))
		b.WriteString("\n")
		b.WriteString(searchLabelStyle.Render(": ") + searchQueryStyle.Render(renderEditable(m.input.value, m.input.cursor, false, "_", inner-2)))
	} else {
		var hint string
		switch m.run.tab {
		case tabScripts:
			hint = "enter run • r reload • : command • [/] tabs"
		case tabCommands:
			hint = "enter run • n new • e edit • d delete • : command"
		case tabRecent:
			hint = "enter run again • s save as command • d forget • : command"
		}
		b.WriteString(grayItalicStyle.Render(truncateForWidth(hint, inner)))
	}

	borderColor := lipgloss.Color("#4F4F4F")
	if focused {
		borderColor = lipgloss.Color("#5FAFFF")
	}
	return renderTitledPanel(b.String(), runFrameTitle, width, height, borderColor, focused)
}

// renderOutputPanel renders the streamed output of the current/last job.
func (m model) renderOutputPanel() string {
	outputWidth, outputHeight := m.getHistoryPanelSize()
	inner := getPanelInnerWidth(outputWidth)
	focused := m.activeFocus == focusOutput
	selectedOutputStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#2D6A8C")).Bold(true)

	var b strings.Builder
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#435871"))
	noteStyle := grayItalicStyle

	lines := m.outputLines()

	if len(lines) == 0 {
		if m.run.running() {
			b.WriteString(grayItalicStyle.Render(truncateForWidth("Waiting for output…", inner)))
		} else if m.run.finished() {
			b.WriteString(grayItalicStyle.Render(truncateForWidth("(no output)", inner)))
		} else {
			b.WriteString(grayItalicStyle.Render(truncateForWidth("No output yet", inner)))
			b.WriteString("\n")
			b.WriteString(grayItalicStyle.Render(truncateForWidth("Run a script or command to see its output here", inner)))
		}
	} else {
		startIdx, endIdx, showTop, showBottom := outputViewport(len(lines), outputHeight, m.run.outputScroll)
		cursor := m.run.outputCursor
		if cursor < 0 {
			cursor = 0
		}
		if cursor >= len(lines) {
			cursor = len(lines) - 1
		}
		if showTop {
			b.WriteString(grayItalicStyle.Render(fmt.Sprintf("  ↑ %d more", startIdx)))
			b.WriteString("\n")
		}
		for i := startIdx; i < endIdx; i++ {
			l := lines[i]
			var text string
			switch {
			case i == cursor && focused:
				text = selectedOutputStyle.Render("▸ " + truncateForWidth(l.text, inner-2))
			case l.header:
				text = headerStyle.Render(truncateForWidth(l.text, inner))
			case l.run != nil && len(l.run.lines) == 0 && l.run.note != "":
				text = noteStyle.Render(truncateForWidth(l.text, inner))
			default:
				text = truncateForWidth(l.text, inner)
			}
			b.WriteString(text)
			if i < endIdx-1 || showBottom {
				b.WriteString("\n")
			}
		}
		if showBottom {
			b.WriteString(grayItalicStyle.Render(fmt.Sprintf("  ↓ %d more", len(lines)-endIdx)))
		}
	}

	title := outputFrameTitle
	if m.run.label != "" {
		title += " · " + truncateForWidth(m.run.label, 24)
	}
	if status := m.outputStatus(); status != "" {
		title += "  " + status
	}

	borderColor := lipgloss.Color("#4F4F4F")
	if focused {
		borderColor = lipgloss.Color("#5FAFFF")
	}
	return renderTitledPanel(b.String(), title, outputWidth, outputHeight, borderColor, focused)
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
	if m.list.selectedProfile == "" {
		allAccountsLabel += " ✓"
	}
	if m.acct.profileCursor == 0 {
		listContent.WriteString(selectedStyle.Render("▸ " + allAccountsLabel))
	} else {
		listContent.WriteString(normalStyle.Render("  " + allAccountsLabel))
	}
	listContent.WriteString("\n")

	// Add individual accounts
	for i, acc := range m.acct.list {
		label := acc.Name
		if acc.Tailnet != acc.Name {
			label += fmt.Sprintf(" (%s)", acc.Tailnet)
		}
		if m.list.selectedProfile == acc.Name {
			label += " ✓"
		}

		if m.acct.profileCursor == i+1 {
			listContent.WriteString(selectedStyle.Render("▸ " + label))
		} else {
			listContent.WriteString(normalStyle.Render("  " + label))
		}
		if i < len(m.acct.list)-1 {
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
	if m.acct.manageCursor == 0 {
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

// renderAbout renders the about/info screen
func (m model) renderAbout() string {
	var b strings.Builder

	title := "About ts-cli"
	b.WriteString(titleStyle.Render(title))
	b.WriteString("\n\n")

	listWidth := 60
	if m.width > 0 && m.width < 80 {
		listWidth = m.width - 10
		if listWidth < 40 {
			listWidth = 40
		}
	}

	aboutBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7A7A7A")).
		Padding(1, 2).
		Width(listWidth)

	var content strings.Builder
	content.WriteString(lipgloss.NewStyle().Bold(true).Render("ts-cli"))
	content.WriteString(fmt.Sprintf(" v%s\n", m.version))
	content.WriteString("\n")
	content.WriteString("CLI tool for managing Tailscale devices\n")
	content.WriteString("\n")
	content.WriteString(grayItalicStyle.Render("Author:") + "  Ihor Yatsenko\n")
	content.WriteString(grayItalicStyle.Render("Email:") + "   yatsenko.ihor@gmail.com\n")
	content.WriteString(grayItalicStyle.Render("GitHub:") + "  https://github.com/yatsenko-ihor/ts-cli\n")
	content.WriteString("\n")
	content.WriteString(grayItalicStyle.Render("License: MIT + Commons Clause"))

	b.WriteString(aboutBox.Render(content.String()))
	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render("Press any key to return"))

	return b.String()
}

// renderInputPopup renders a centered popup for username/password input
func (m model) renderInputPopup(label string, value string, masked bool) string {
	var b strings.Builder

	title := fmt.Sprintf("Tailscale Devices (ts-cli v%s)", m.version)
	b.WriteString(titleStyle.Render(title))
	b.WriteString("\n\n")

	popupWidth := 50
	if m.width > 0 && m.width < 70 {
		popupWidth = m.width - 10
		if popupWidth < 35 {
			popupWidth = 35
		}
	}

	popupStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#5FAFFF")).
		Padding(1, 2).
		Width(popupWidth)

	var content strings.Builder
	content.WriteString(lipgloss.NewStyle().Bold(true).Render(label))
	if m.input.mode == inputText && m.input.hint != "" {
		content.WriteString("\n")
		content.WriteString(grayItalicStyle.Render(truncateForWidth(m.input.hint, popupWidth-8)))
	} else if p := m.ssh.pending; p.machineName != "" {
		content.WriteString("\n")
		content.WriteString(grayItalicStyle.Render(truncateForWidth(p.machineName, popupWidth-8)))
	}
	content.WriteString("\n\n")

	// Explain why we are asking (key auth failed, password rejected, ...)
	if m.notify.sshError != nil {
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5555"))
		content.WriteString(errStyle.Render(truncateForWidth(m.notify.sshError.Error(), popupWidth-8)))
		content.WriteString("\n\n")
	}

	// Show the input value (masked for password)
	inputStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#333333")).
		Padding(0, 1).
		Width(popupWidth - 8)
	content.WriteString(inputStyle.Render(renderEditable(value, m.input.cursor, masked, "▌", popupWidth-12)))
	content.WriteString("\n\n")
	content.WriteString(grayItalicStyle.Render("enter confirm • esc cancel • ctrl+v paste"))

	b.WriteString(popupStyle.Render(content.String()))

	return b.String()
}

// renderSSHRetryMenu renders the retry menu after SSH connection failure
func (m model) renderSSHRetryMenu() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("SSH Connection Failed"))
	b.WriteString("\n\n")

	// Show the error
	if m.notify.sshError != nil {
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5555"))
		b.WriteString(errStyle.Render(fmt.Sprintf("  ✗ %v", m.notify.sshError)))
		b.WriteString("\n\n")
	}

	popupWidth := 50
	if m.width > 0 && m.width < 70 {
		popupWidth = m.width - 10
		if popupWidth < 35 {
			popupWidth = 35
		}
	}

	popupStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#FFAF00")).
		Padding(1, 2).
		Width(popupWidth)

	items := []string{
		"Change username",
		"Change password",
		"Change both",
		"Cancel",
	}

	var content strings.Builder
	content.WriteString(lipgloss.NewStyle().Bold(true).Render("What would you like to change?"))
	content.WriteString("\n\n")
	for i, item := range items {
		if i == m.ssh.retryCursor {
			content.WriteString(selectedStyle.Render("▸ " + item))
		} else {
			content.WriteString(normalStyle.Render("  " + item))
		}
		if i < len(items)-1 {
			content.WriteString("\n")
		}
	}

	b.WriteString(popupStyle.Render(content.String()))
	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render("↑/k up • ↓/j down • enter select • esc cancel"))

	return b.String()
}

// renderOptionsMenu renders the options/settings menu
func (m model) renderOptionsMenu() string {
	if m.opts.showMachineCreds {
		return m.renderMachineCredsView()
	}

	var b strings.Builder

	title := fmt.Sprintf("Options (ts-cli v%s)", m.version)
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

	// Option: View machines with saved passwords
	machineCredsLabel := fmt.Sprintf("Machines with saved passwords (%d)", len(m.ssh.machineCreds))
	if m.opts.cursor == optionMachineCredentials {
		listContent.WriteString(selectedStyle.Render("▸ " + machineCredsLabel))
	} else {
		listContent.WriteString(normalStyle.Render("  " + machineCredsLabel))
	}
	listContent.WriteString("\n")

	// Option: where the master key lives
	store := util.CurrentSecretStore()
	var secretLabel string
	if store == util.SecretStoreKeychain {
		secretLabel = "Encryption key: OS keychain  → switch to key file"
	} else {
		secretLabel = "Encryption key: ~/.ts-cli/key  → switch to OS keychain"
	}
	if m.opts.secretStoreBusy {
		secretLabel = "Encryption key: switching…"
	}
	if m.opts.cursor == optionSecretStore {
		listContent.WriteString(selectedStyle.Render("▸ " + secretLabel))
	} else {
		listContent.WriteString(normalStyle.Render("  " + secretLabel))
	}

	b.WriteString(optionsList.Render(listContent.String()))
	b.WriteString("\n\n")

	// Info text
	b.WriteString(grayItalicStyle.Render("Passwords and API keys are encrypted with AES-256-GCM"))
	b.WriteString("\n")
	savedCount := len(m.ssh.machineCreds)
	if savedCount > 0 {
		b.WriteString(successStyle.Render(fmt.Sprintf("✓ %d machine(s) with saved credentials", savedCount)))
	} else {
		b.WriteString(grayItalicStyle.Render("Credentials will be saved per-machine after the first successful SSH connection"))
	}
	b.WriteString("\n")
	if m.opts.secretStoreMsg != "" {
		style := successStyle
		if strings.HasPrefix(m.opts.secretStoreMsg, "✗") {
			style = errorStyle
		}
		b.WriteString(style.Render(truncateForWidth(m.opts.secretStoreMsg, listWidth)))
		b.WriteString("\n")
	} else if notice := util.KeyStoreNotice(); notice != "" {
		b.WriteString(infoStyle.Render(truncateForWidth("⚠ "+notice, listWidth)))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	help := "↑/k up • ↓/j down • enter toggle/select • esc/q back"
	b.WriteString(helpStyle.Render(help))

	return b.String()
}

// renderMachineCredsView renders the list of machines with saved credentials
func (m model) renderMachineCredsView() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("Machines with saved passwords"))
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

	machineIDs := m.getSortedMachineCredIDs()

	if len(machineIDs) == 0 {
		listContent.WriteString(grayItalicStyle.Render("  No machines with saved passwords"))
	} else {
		for i, id := range machineIDs {
			cred := m.ssh.machineCreds[id]
			label := fmt.Sprintf("%s (user: %s)", m.credDisplayName(id), cred.username)
			if cred.passwordEncrypted == "" {
				label += " 🔑" // key-based auth
			}
			if i == m.opts.machineCredsCursor {
				listContent.WriteString(selectedStyle.Render("▸ " + label))
			} else {
				listContent.WriteString(normalStyle.Render("  " + label))
			}
			if i < len(machineIDs)-1 {
				listContent.WriteString("\n")
			}
		}
	}

	b.WriteString(optionsList.Render(listContent.String()))
	b.WriteString("\n\n")

	help := "↑/k up • ↓/j down • d delete • esc/q back"
	b.WriteString(helpStyle.Render(help))

	return b.String()
}

// getHelpText returns context-sensitive help text based on current mode
func (m model) getHelpText() string {
	help := "↑/k ↓/j • space select • * all • p ping • / search • s ssh • : run • c copy • tab run panel • P profile • S sort • r reload • o options • a about"
	if m.run.visible {
		switch m.activeFocus {
		case focusRun:
			help = "1/2/3 frame • [/] tabs • ↑/k ↓/j • enter run • : command • tab/shift+tab switch • esc close"
		case focusOutput:
			if m.run.multi {
				help = "1/2/3 frame • ↑/k ↓/j • {/} machine • g/G top/end • c copy path • N copy name • x stop • C clear • esc close"
			} else {
				help = "1/2/3 frame • ↑/k ↓/j • g/G top/end • c copy path • n copy name • x stop • C clear • tab/shift+tab switch • esc close"
			}
		default:
			help = "1/2/3 frame • ↑/k ↓/j • space select • * all • s ssh • : run • tab/shift+tab switch • esc close"
		}
	}
	if !m.run.visible {
		if m.ssh.username != "" {
			help += " • d clear-user"
		}
		help += " • F1 help • q quit"
	}

	switch m.input.mode {
	case inputUsername:
		help = "Enter SSH username • esc cancel • enter confirm"
	case inputPassword:
		help = "Enter SSH password • esc cancel • enter save"
	case inputCommand:
		help = "Type command to run on the selected machine • esc cancel • enter run"
	case inputText:
		help = "esc cancel • enter confirm • ctrl+v paste"
	case inputSearch:
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
