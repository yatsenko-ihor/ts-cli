package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/yatsenko-ihor/ts-cli/client"
	"github.com/yatsenko-ihor/ts-cli/config"
	"github.com/yatsenko-ihor/ts-cli/scripts"
	"github.com/yatsenko-ihor/ts-cli/util"
)

type model struct {
	// ─── Composed sub-states ──────────────────────────────────────────────────
	list   deviceList    // Device list panel state
	run    runPanel      // Run (scripts/commands/recent) + Output panels
	acct   accounts      // Account/profile management
	ssh    ssh           // SSH connection state
	opts   options       // Options menu
	notify notifications // Transient messages
	inst   install       // PATH installation suggestion
	input  textInput     // Active text input prompt

	// ─── Global state ─────────────────────────────────────────────────────────
	err            error
	width          int
	height         int
	activeFocus    panelFocus
	version        string
	reloading      bool
	aboutMode      bool
	helpMode       bool
	helpScroll     int
	set            uiSettings
	settingsMode   bool
	settingsCursor int
	lastMachine    string               // key of the machine connected to most recently
	ping           map[string]pingState // explicit reachability results by machineKey
	pingAt         map[string]time.Time // when each machine was last pinged
	pinging        map[string]bool      // pings in flight
	pingSeq        int                  // debounce counter for auto-ping on cursor rest
	preflight      *preflight           // launch waiting for its reachability check
}

// NewModel builds the initial TUI state. machineCreds is the per-machine
// credential map from config.json (keyed by device ID; older files keyed by
// device name are migrated on first run). savedCommands feeds the Run panel.
func NewModel(devices []client.Device, version string, sshUsername string, accountList []client.AccountInfo, machineCreds map[string]config.MachineAuth, savedCommands []config.SavedCommand, lastMachine string, ui map[string]string) model {
	// Sort devices with online devices first
	sortDevicesByStatus(devices)

	// Find active account
	activeAccount := ""
	for _, acc := range accountList {
		if acc.Active {
			activeAccount = acc.Name
			break
		}
	}

	// Check if ts-cli is properly installed in PATH
	showInstallSuggestion, installationBroken := checkIfInstallNeeded()

	// Get real active Tailscale account
	tailscaleActiveAccount := getRealTailscaleAccount()

	// Initialize history store and local scripts
	history, _ := util.NewHistoryStore()
	localScripts, scriptsErr := scripts.Load()

	// Convert machine credentials. Usernames come from disk, so re-validate
	// them here rather than trusting the file.
	mCreds := make(map[string]machineCredential, len(machineCreds))
	for id, cred := range machineCreds {
		username := cred.Username
		if username != "" && util.ValidateSSHUsername(username) != nil {
			username = ""
		}
		mCreds[id] = machineCredential{name: cred.Name, username: username, passwordEncrypted: cred.PasswordEncrypted}
	}
	if sshUsername != "" && util.ValidateSSHUsername(sshUsername) != nil {
		sshUsername = ""
	}
	credsDirty := migrateCredentialKeys(mCreds, devices)

	m := model{
		set:         loadUISettings(ui),
		lastMachine: lastMachine,
		list: deviceList{
			devices:         devices,
			filteredDevices: devices,
			cursor:          0,
			selected:        -1,
			viewportTop:     0,
			searchQuery:     "",
			selectedProfile: "",
		},
		run: runPanel{
			history:    history,
			scripts:    localScripts,
			scriptsErr: scriptsErr,
			commands:   savedCommands,
			follow:     true,
		},
		acct: accounts{
			list:                   accountList,
			activeAccount:          activeAccount,
			tailscaleActiveAccount: tailscaleActiveAccount,
			profileSelectMode:      false,
			profileCursor:          0,
			accountManageMode:      false,
			manageCursor:           0,
		},
		ssh: ssh{
			username:     sshUsername,
			machineCreds: mCreds,
			credsDirty:   credsDirty,
		},
		opts: options{
			active: false,
			cursor: 0,
		},
		notify: notifications{},
		inst: install{
			showSuggestion: showInstallSuggestion,
			broken:         installationBroken,
		},
		input:       textInput{mode: inputNone},
		activeFocus: focusList,
		version:     version,
		reloading:   false,
	}
	applyTheme(m.set.theme)
	m.filterDevices()
	// Start on the machine connected to last time, if it is online and listed.
	if lastMachine != "" {
		for i, d := range m.list.filteredDevices {
			if machineKey(d) == lastMachine && isDeviceOnline(d) {
				m.list.cursor = i
				break
			}
		}
	}
	m.ensureListCursorVisible()
	return m
}

func (m model) Init() tea.Cmd {
	var cmds []tea.Cmd
	if m.ssh.credsDirty {
		// Persist credentials re-keyed from device name to device ID.
		cmds = append(cmds, replaceMachineCredentials(m.ssh.machineCreds))
	}
	// Check the pre-selected machine in the background right away.
	if d, ok := m.targetDevice(); ok && m.wantsAutoPing(d) {
		cmds = append(cmds, pingDevice(d, pingAuto, manualPingWait))
	}
	cmds = append(cmds, m.checkAllHealth())
	return tea.Batch(cmds...)
}

// reloadDevices fetches fresh device list from all accounts
func (m model) reloadDevices() tea.Cmd {
	return func() tea.Msg {
		if len(m.acct.list) == 0 {
			return reloadMsg{
				devices: nil,
				err:     fmt.Errorf("no accounts configured for reload"),
			}
		}

		// Fetch devices from all accounts; per-account failures are returned
		// to the UI instead of being printed over the alt-screen.
		devices, warnings := client.ListDevicesFromAccountsWithErrors(m.acct.list)

		return reloadMsg{
			devices:  devices,
			warnings: warnings,
			err:      nil,
		}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ensureListCursorVisible()
		return m, nil

	case sshMsg:
		// Outcome of an interactive session; see sshflow.go.
		return m.finishSSHAttempt(msg.failure, msg.err)

	case tmuxOpenedMsg:
		if msg.err != nil {
			m.notify.sshError = msg.err
		} else {
			m.notify.sshError = nil
			m.notify.info = "Opened ssh to " + msg.name + " in a tmux pane"
		}
		return m, nil

	case probeMsg:
		return m.handleProbe(msg)

	case pingMsg:
		return m.handlePing(msg)

	case autoPingTickMsg:
		return m.handleAutoPingTick(msg)

	case copiedMsg:
		if msg.success {
			m.notify.copiedText = msg.text
			// Clear the message after 3 seconds
			return m, tea.Tick(time.Second*3, func(time.Time) tea.Msg {
				return clearCopiedMsg{}
			})
		}
		return m, nil

	case clearCopiedMsg:
		m.notify.copiedText = ""
		return m, nil

	case clearReloadMsg:
		m.notify.reloadSuccess = ""
		m.notify.info = ""
		return m, nil

	case usernameStoredMsg:
		if msg.err != nil {
			m.notify.sshError = msg.err
		}
		return m, nil

	case machineCredsSavedMsg:
		if msg.err != nil {
			m.notify.sshError = fmt.Errorf("failed to save machine credentials: %w", msg.err)
		}
		return m, nil

	case secretStoreChangedMsg:
		m.opts.secretStoreBusy = false
		if msg.err != nil {
			m.opts.secretStoreMsg = fmt.Sprintf("✗ Could not switch to %s: %v", msg.target, msg.err)
		} else {
			m.opts.secretStoreMsg = fmt.Sprintf("✓ Master key moved to %s", msg.store)
		}
		return m, nil

	case tailscaleUpMsg:
		// Tailscale up command completed
		if msg.err != nil {
			m.notify.sshError = fmt.Errorf("tailscale up failed: %w", msg.err)
		}
		return m, nil

	case accountsChangedMsg:
		return m.handleAccountsChanged(msg)

	case accountHealthMsg:
		if m.acct.health == nil {
			m.acct.health = map[string]accountHealth{}
		}
		m.acct.health[msg.name] = accountHealth{checked: true, h: msg.h}
		return m, nil

	case accountSwitchedMsg:
		// Handle account switch result
		if msg.err != nil {
			m.notify.sshError = fmt.Errorf("failed to switch to account %s: %w", msg.accountName, msg.err)
			return m, nil
		}
		// Account switched successfully, update active account
		m.acct.activeAccount = msg.accountName
		// Update the real Tailscale active account
		m.acct.tailscaleActiveAccount = getRealTailscaleAccount()
		// Resume the SSH attempt that triggered the switch
		if msg.proceedWithSSH {
			return m.continueSSH()
		}
		return m, nil

	case uiSavedMsg:
		if msg.err != nil {
			m.notify.sshError = fmt.Errorf("failed to save setting: %w", msg.err)
		}
		return m, nil

	case reloadMsg:
		// Handle reload result
		m.reloading = false
		if msg.err != nil {
			m.notify.sshError = fmt.Errorf("failed to reload devices: %w", msg.err)
			return m, nil
		}

		// Sort devices before storing
		sortDevicesByStatus(msg.devices)
		m.list.devices = msg.devices

		m.ping, m.pingAt = nil, nil
		m.refilterKeepCursor()

		// Reset cursor if out of bounds
		if m.list.cursor >= len(m.list.filteredDevices) {
			m.list.cursor = 0
		}
		if m.list.selected >= len(m.list.filteredDevices) {
			m.list.selected = -1
		}
		// A reload refreshes last-seen times; forget stale ping results
		m.ping, m.pingAt = nil, nil
		// Drop marks for machines that disappeared
		for k := range m.list.marked {
			found := false
			for _, d := range msg.devices {
				if machineKey(d) == k {
					found = true
					break
				}
			}
			if !found {
				delete(m.list.marked, k)
			}
		}

		// Clear any previous SSH error, surface per-account fetch failures
		m.notify.sshError = nil
		if len(msg.warnings) > 0 {
			parts := make([]string, 0, len(msg.warnings))
			for _, w := range msg.warnings {
				parts = append(parts, w.Error())
			}
			m.notify.sshError = fmt.Errorf("some accounts failed to load: %s", strings.Join(parts, "; "))
		}

		// Show reload success message
		m.notify.reloadSuccess = fmt.Sprintf("Reloaded %d device(s) from %d account(s)", len(msg.devices), len(m.acct.list))

		// Clear the success message after 3 seconds
		return m, tea.Tick(time.Second*3, func(time.Time) tea.Msg {
			return clearReloadMsg{}
		})

	case runLinesMsg:
		if m.run.runFor(msg.job) == nil {
			return m, nil // output of a job that was replaced
		}
		m.appendRunLines(msg.job, msg.lines)
		return m, waitRunEvent(msg.job)

	case runDoneMsg:
		return m.handleRunDone(msg)

	case runTickMsg:
		if mr := m.run.runFor(msg.job); mr == nil || mr.status != runRunning {
			return m, nil
		}
		m.run.elapsed = time.Since(m.run.startedAt)
		return m, tickRun(msg.job)

	case commandsChangedMsg:
		if msg.err != nil {
			m.notify.sshError = fmt.Errorf("failed to save command: %w", msg.err)
			return m, nil
		}
		m.run.commands = msg.commands
		m.clampTabCursor()
		return m, nil

	case pasteMsg:
		// Silently ignore paste errors (clipboard tool missing, etc.)
		if msg.err != nil || msg.text == "" {
			return m, nil
		}
		if msg.target == pasteTargetSearch {
			m.list.searchQuery, m.list.searchCursor = insertText(m.list.searchQuery, m.list.searchCursor, msg.text)
			m.filterDevices()
		} else {
			m.input.value, m.input.cursor = insertText(m.input.value, m.input.cursor, msg.text)
		}
		return m, nil

	case tea.KeyMsg:
		// While a pre-flight check runs (at most ~2 s) only cancel is accepted.
		if m.preflight != nil {
			if k := msg.String(); k == "esc" || k == "ctrl+c" {
				m.preflight = nil
				m.notify.info = ""
			}
			return m, nil
		}
		if m.settingsMode {
			return m.handleSettings(msg)
		}
		// Dispatch to appropriate mode handler
		switch m.input.mode {
		case inputPassword:
			return m.handlePasswordPrompt(msg)
		case inputUsername:
			return m.handleUsernamePrompt(msg)
		case inputCommand:
			return m.handleCommandMode(msg)
		case inputText:
			return m.handleTextPrompt(msg)
		case inputSearch:
			return m.handleSearchMode(msg)
		}
		if m.ssh.retryMode {
			return m.handleSSHRetryMenu(msg)
		}
		if m.acct.profileSelectMode {
			return m.handleProfileSelection(msg)
		}
		if m.acct.accountManageMode {
			return m.handleAccountManagement(msg)
		}
		if m.opts.active {
			return m.handleOptionsMenu(msg)
		}
		if m.aboutMode {
			return m.handleAboutMode(msg)
		}
		if m.helpMode {
			return m.handleHelpMode(msg)
		}
		return m.handleNormalMode(msg)
	}

	return m, nil
}

// View renders the screen, painted with the theme background if it has one.
func (m model) View() string {
	return paintScreen(m.viewBody(), m.width, m.height)
}

func (m model) viewBody() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v\n", m.err)
	}
	if m.settingsMode {
		return m.renderSettings()
	}

	// Show profile selection view if in profile selection mode
	if m.acct.profileSelectMode {
		return m.renderProfileSelection()
	}

	// Show account management view if in account management mode
	if m.acct.accountManageMode && m.input.mode == inputNone {
		return m.renderAccountManagement()
	}

	// Show options menu if in options mode
	if m.opts.active {
		return m.renderOptionsMenu()
	}

	// Show about screen
	if m.aboutMode {
		return m.renderAbout()
	}

	// Show help screen
	if m.helpMode {
		return m.renderHelp()
	}

	// Show SSH retry menu after failed connection
	if m.ssh.retryMode {
		return m.renderSSHRetryMenu()
	}

	// Show SSH input prompts as popups
	if m.input.mode == inputUsername {
		return m.renderInputPopup("SSH Username", m.input.value, false)
	}
	if m.input.mode == inputPassword {
		return m.renderInputPopup("SSH Password", m.input.value, true)
	}
	if m.input.mode == inputText {
		return m.renderInputPopup(m.input.label, m.input.value, m.input.masked)
	}

	var b strings.Builder

	// Title
	title := fmt.Sprintf("Tailscale Devices (ts-cli v%s)", m.version)
	if m.reloading {
		title += " - Reloading..."
		b.WriteString(titleStyle.Render(title))
	} else if m.list.searchQuery != "" {
		title += fmt.Sprintf(" - Filtered: %d/%d", len(m.list.filteredDevices), len(m.list.devices))
		b.WriteString(titleStyle.Render(title))
	} else if m.list.selectedProfile != "" {
		title += fmt.Sprintf(" - Profile: %s", m.list.selectedProfile)
		b.WriteString(titleStyle.Render(title))
	} else {
		b.WriteString(titleStyle.Render(title))
	}
	b.WriteString("\n")

	// Show real active Tailscale account at the top
	if m.acct.tailscaleActiveAccount != "" {
		b.WriteString(grayItalicStyle.Render(fmt.Sprintf("Active account: %s", m.acct.tailscaleActiveAccount)))
		b.WriteString("\n")
	}

	if w := m.keyWarning(); w != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(colorWarn).Render(w))
		b.WriteString("\n")
	}

	// Show default SSH username
	if m.ssh.username != "" {
		b.WriteString(grayItalicStyle.Render(fmt.Sprintf("Default username: %s", m.ssh.username)))
	} else {
		b.WriteString(grayItalicStyle.Render("Default username: <none>"))
	}
	b.WriteString("\n")

	// Render split view if the Run panel is shown
	if m.run.visible {
		// Split view: device list on left, Run + Output on right
		deviceList := m.renderDeviceList()
		runPanel := m.renderRunPanel()
		outputPanel := m.renderOutputPanel()
		rightSpacer := lipgloss.NewStyle().Width(splitRightSpacerWidth).Render("")

		// Stack Run and Output vertically
		rightPanel := lipgloss.JoinVertical(
			lipgloss.Left,
			runPanel,
			outputPanel,
		)

		// Join left and right columns horizontally
		splitView := lipgloss.JoinHorizontal(
			lipgloss.Top,
			deviceList,
			rightPanel,
			rightSpacer,
		)
		b.WriteString(splitView)
	} else {
		// Normal view
		deviceList := m.renderDeviceList()
		if m.detailsVisible() {
			deviceList = lipgloss.JoinHorizontal(lipgloss.Top, deviceList, " ",
				m.renderDetailsPanel(m.detailsWidth(), m.listFrameHeight()))
		}
		b.WriteString(deviceList)

	}

	// A job finishing while the Run panel is closed is still reported briefly.
	if !m.run.visible && len(m.run.runs) > 0 && m.run.label != "" {
		b.WriteString("\n")
		b.WriteString(grayItalicStyle.Render(fmt.Sprintf("Last run: %s  %s  (Tab to open Output)", m.run.label, m.outputStatus())))
	}

	// Show copy success message if any
	if m.notify.copiedText != "" {
		b.WriteString("\n")
		b.WriteString(successStyle.Render(fmt.Sprintf("✓ Copied to clipboard: %s", m.notify.copiedText)))
	}

	// Show neutral status (ping progress/result)
	if m.notify.info != "" {
		b.WriteString("\n")
		b.WriteString(infoStyle.Render(m.notify.info))
	}

	// Show reload success message if any
	if m.notify.reloadSuccess != "" {
		b.WriteString("\n")
		b.WriteString(successStyle.Render(fmt.Sprintf("✓ %s", m.notify.reloadSuccess)))
	}

	// Show SSH error if any
	if m.notify.sshError != nil {
		b.WriteString("\n")
		b.WriteString(errorStyle.Render(fmt.Sprintf("SSH Error: %v", m.notify.sshError)))
	}

	// Show installation suggestion if not in PATH
	if m.inst.showSuggestion && m.input.mode != inputUsername && m.input.mode != inputSearch && !m.acct.profileSelectMode && !m.acct.accountManageMode {
		b.WriteString("\n")
		var message string
		if m.inst.broken {
			message = "💡 Tip: Run 'ts-cli install' to reinstall ts-cli (current PATH installation is broken). Press 'x' to dismiss."
		} else {
			message = "💡 Tip: Run 'ts-cli install' to add ts-cli to your PATH for easier access. Press 'x' to dismiss."
		}
		b.WriteString(infoStyle.Render(message))
	}

	// Show terminal size warning if too small
	minWidth := 80
	minHeight := 24
	if m.run.visible {
		minWidth = 110 // Need extra width for split view
		minHeight = 30 // Need extra height for stacked panels
	}

	if m.width > 0 && m.height > 0 && (m.width < minWidth || m.height < minHeight) {
		b.WriteString("\n")
		warningStyle := lipgloss.NewStyle().
			Foreground(cur.info).
			Bold(true)

		warningMsg := fmt.Sprintf("⚠️  Warning: Terminal size (%dx%d) is too small. Minimum recommended: %dx%d for optimal display.",
			m.width, m.height, minWidth, minHeight)
		b.WriteString(warningStyle.Render(warningMsg))
	}

	b.WriteString("\n")
	b.WriteString(m.renderHelpPanel())

	return b.String()
}
