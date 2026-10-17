package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/yatsenko-ihor/ts-cli/client"
	"github.com/yatsenko-ihor/ts-cli/config"
	"github.com/yatsenko-ihor/ts-cli/util"
)

type model struct {
	// ─── Composed sub-states ──────────────────────────────────────────────────
	list   deviceList    // Device list panel state
	hist   historyPanel  // Command history + output panels
	acct   accounts      // Account/profile management
	ssh    ssh           // SSH connection state
	opts   options       // Options menu
	notify notifications // Transient messages
	inst   install       // PATH installation suggestion
	input  textInput     // Active text input prompt

	// ─── Global state ─────────────────────────────────────────────────────────
	err         error
	width       int
	height      int
	activeFocus panelFocus
	version     string
	reloading   bool
	aboutMode   bool
	helpMode    bool
	helpScroll  int
}

// NewModel builds the initial TUI state. machineCreds is the per-machine
// credential map from config.json (keyed by device ID; older files keyed by
// device name are migrated on first run).
func NewModel(devices []client.Device, version string, sshUsername string, accountList []client.AccountInfo, machineCreds map[string]config.MachineAuth) model {
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

	// Initialize history store
	history, _ := util.NewHistoryStore()

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

	return model{
		list: deviceList{
			devices:         devices,
			filteredDevices: devices,
			cursor:          0,
			selected:        -1,
			viewportTop:     0,
			searchQuery:     "",
			selectedProfile: "",
		},
		hist: historyPanel{
			visible:       false,
			cursor:        0,
			history:       history,
			commandOutput: "",
			outputScroll:  0,
			outputCursor:  0,
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
}

func (m model) Init() tea.Cmd {
	if m.ssh.credsDirty {
		// Persist credentials re-keyed from device name to device ID.
		return replaceMachineCredentials(m.ssh.machineCreds)
	}
	return nil
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
		return m, nil

	case sshMsg:
		// Outcome of an interactive session; see sshflow.go.
		return m.finishSSHAttempt(msg.failure, msg.err)

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

	case addAccountMsg:
		// Handle account addition result
		if msg.err != nil {
			m.notify.sshError = fmt.Errorf("failed to add account: %w", msg.err)
		} else {
			// Account added successfully, could reload devices here
			// For now, just clear any previous errors
			m.notify.sshError = nil
		}
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

		// Re-apply filters if any are active
		if m.list.searchQuery != "" || m.list.selectedProfile != "" {
			m.filterDevices()
		} else {
			m.list.filteredDevices = msg.devices
		}

		// Reset cursor if out of bounds
		if m.list.cursor >= len(m.list.filteredDevices) {
			m.list.cursor = 0
		}
		if m.list.selected >= len(m.list.filteredDevices) {
			m.list.selected = -1
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

	case commandExecutedMsg:
		connected := msg.failure == sshFailureNone || msg.failure == sshFailureRemoteExit
		// Record history here, on the update loop, so the store is never
		// mutated concurrently with the history-panel key handlers. Only
		// commands that actually reached the remote shell are recorded.
		if connected && m.hist.history != nil && msg.machineID != "" {
			m.hist.history.AddCommand(msg.machineID, msg.machineName, msg.command, msg.exitCode, msg.output)
			_ = m.hist.history.Save() // Ignore save errors
		}
		if connected {
			m.hist.commandOutput = msg.output
			m.hist.outputScroll = 0 // Reset scroll when new output arrives
			m.hist.outputCursor = 0
			m.notify.sshError = nil
			// finishSSHAttempt persists credentials and reports a non-zero exit.
			updated, cmd := m.finishSSHAttempt(msg.failure, msg.err)
			um := updated.(model)
			if msg.failure == sshFailureRemoteExit {
				um.notify.sshError = fmt.Errorf("command exited with status %d", msg.exitCode)
			}
			return um, cmd
		}
		// Connection/auth problem: let the flow ask for a password or show the retry menu.
		m.hist.commandOutput = ""
		m.hist.outputCursor = 0
		m.hist.outputScroll = 0
		return m.finishSSHAttempt(msg.failure, msg.err)

	case pasteMsg:
		// Silently ignore paste errors (clipboard tool missing, etc.)
		if msg.err != nil || msg.text == "" {
			return m, nil
		}
		switch msg.target {
		case pasteTargetUsername:
			m.input.value += msg.text
		case pasteTargetSearch:
			m.list.searchQuery += msg.text
			m.filterDevices()
		case pasteTargetCommand:
			m.input.value += msg.text
		case pasteTargetPassword:
			m.input.value += msg.text
		}
		return m, nil

	case tea.KeyMsg:
		// Dispatch to appropriate mode handler
		switch m.input.mode {
		case inputPassword:
			return m.handlePasswordPrompt(msg)
		case inputUsername:
			return m.handleUsernamePrompt(msg)
		case inputCommand:
			return m.handleCommandMode(msg)
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

func (m model) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v\n", m.err)
	}

	// Show profile selection view if in profile selection mode
	if m.acct.profileSelectMode {
		return m.renderProfileSelection()
	}

	// Show account management view if in account management mode
	if m.acct.accountManageMode {
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

	// Show default SSH username
	if m.ssh.username != "" {
		b.WriteString(grayItalicStyle.Render(fmt.Sprintf("Default username: %s", m.ssh.username)))
	} else {
		b.WriteString(grayItalicStyle.Render("Default username: <none>"))
	}
	b.WriteString("\n")

	// Render split view if history panel is shown
	if m.hist.visible {
		// Split view: device list on left, history + output on right
		deviceList := m.renderDeviceList()
		historyPanel := m.renderHistoryPanel()
		outputPanel := m.renderOutputPanel()
		rightSpacer := lipgloss.NewStyle().Width(splitRightSpacerWidth).Render("")

		// Stack history and output vertically
		rightPanel := lipgloss.JoinVertical(
			lipgloss.Left,
			historyPanel,
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
		b.WriteString(deviceList)

	}

	// Show command output if any (when history panel is not shown)
	if m.hist.commandOutput != "" && !m.hist.visible {
		b.WriteString("\n\n")
		outputStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#7A7A7A")).
			Padding(1).
			MarginTop(1)
		b.WriteString(outputStyle.Render("Command Output:\n" + m.hist.commandOutput))
	}

	// Show copy success message if any
	if m.notify.copiedText != "" {
		b.WriteString("\n")
		b.WriteString(successStyle.Render(fmt.Sprintf("✓ Copied to clipboard: %s", m.notify.copiedText)))
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
	if m.hist.visible {
		minWidth = 110 // Need extra width for split view
		minHeight = 30 // Need extra height for stacked panels
	}

	if m.width > 0 && m.height > 0 && (m.width < minWidth || m.height < minHeight) {
		b.WriteString("\n")
		warningStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8A6D3B")).
			Bold(true)

		warningMsg := fmt.Sprintf("⚠️  Warning: Terminal size (%dx%d) is too small. Minimum recommended: %dx%d for optimal display.",
			m.width, m.height, minWidth, minHeight)
		b.WriteString(warningStyle.Render(warningMsg))
	}

	b.WriteString("\n")
	b.WriteString(m.renderHelpPanel())

	return b.String()
}
