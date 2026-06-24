package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ihor/ts-cli/client"
	"github.com/ihor/ts-cli/util"
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
}

func NewModel(devices []client.Device, version string, sshUsername string, accountList []client.AccountInfo, savePasswordEnabled bool, sshPasswordEncrypted string) model {
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
			username:            sshUsername,
			passwordEncrypted:   sshPasswordEncrypted,
			savePasswordEnabled: savePasswordEnabled,
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

		// Fetch devices from all accounts
		devices := client.ListDevicesFromAccounts(m.acct.list)

		return reloadMsg{
			devices: devices,
			err:     nil,
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
		if msg.err != nil {
			m.notify.sshError = msg.err
		}
		return m, nil

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

	case tailscaleUpMsg:
		// Tailscale up command completed
		if msg.err != nil {
			m.notify.sshError = fmt.Errorf("tailscale up failed: %w", msg.err)
		}
		return m, nil

	case tailscaleDownMsg:
		// Tailscale down command completed
		if msg.err != nil {
			m.notify.sshError = fmt.Errorf("tailscale down failed: %w", msg.err)
		} else {
			m.acct.tailscaleActiveAccount = "<not connected>"
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
		// If we should proceed with SSH, do it now
		if msg.proceedWithSSH {
			// Check if username is stored
			if m.ssh.username == "" {
				// Prompt for username
				m.input = textInput{mode: inputUsername}
				m.input.value = ""
				return m, nil
			}
			// Username exists, start SSH session
			return m, m.sshToDevice(msg.deviceIndex)
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

		// Clear any previous SSH error
		m.notify.sshError = nil

		// Show reload success message
		m.notify.reloadSuccess = fmt.Sprintf("Reloaded %d device(s) from %d account(s)", len(msg.devices), len(m.acct.list))

		// Clear the success message after 3 seconds
		return m, tea.Tick(time.Second*3, func(time.Time) tea.Msg {
			return clearReloadMsg{}
		})

	case commandExecutedMsg:
		// Handle command execution result
		if msg.err != nil {
			m.notify.sshError = fmt.Errorf("command failed: %w", msg.err)
			m.hist.commandOutput = ""
			m.hist.outputCursor = 0
			m.hist.outputScroll = 0
		} else {
			m.hist.commandOutput = msg.output
			m.hist.outputScroll = 0 // Reset scroll when new output arrives
			m.hist.outputCursor = 0
			m.notify.sshError = nil
		}
		return m, nil

	case passwordStoredMsg:
		if msg.err != nil {
			m.notify.sshError = msg.err
		}
		return m, nil

	case optionToggledMsg:
		if msg.err != nil {
			m.notify.sshError = msg.err
		}
		return m, nil

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
		if m.acct.profileSelectMode {
			return m.handleProfileSelection(msg)
		}
		if m.acct.accountManageMode {
			return m.handleAccountManagement(msg)
		}
		if m.opts.active {
			return m.handleOptionsMenu(msg)
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

	var b strings.Builder

	// Title
	title := fmt.Sprintf("Tailscale Devices (ts-cli v%s)", m.version)
	if m.reloading {
		title += " - Reloading..."
		b.WriteString(titleStyle.Render(title))
	} else if m.input.mode == inputPassword {
		title += " - SSH Password: " + strings.Repeat("*", len(m.input.value)) + "_"
		b.WriteString(titleStyle.Render(title))
	} else if m.input.mode == inputUsername {
		title += " - SSH Username: " + m.input.value + "_"
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
