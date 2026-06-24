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
	devices                []client.Device
	filteredDevices        []client.Device
	cursor                 int
	selected               int
	err                    error
	width                  int
	height                 int
	sshError               error
	viewportTop            int // First visible item in the list
	searchMode             bool
	searchQuery            string
	activeFocus            panelFocus
	copiedText             string
	reloadSuccess          string // Success message for reload
	version                string
	usernamePrompt         bool                 // Whether we're prompting for username
	usernameInput          string               // Current username being typed
	sshUsername            string               // Stored SSH username
	accounts               []client.AccountInfo // Store accounts for reload functionality
	reloading              bool                 // Whether we're currently reloading
	profileSelectMode      bool                 // Whether we're in profile selection mode
	profileCursor          int                  // Cursor position in profile list
	selectedProfile        string               // Currently selected profile (empty = all)
	activeAccount          string               // Currently active Tailscale account from config
	tailscaleActiveAccount string               // Real active account from Tailscale daemon
	showInstallSuggestion  bool                 // Whether to show PATH installation suggestion
	installationBroken     bool                 // Whether existing PATH installation is broken
	accountManageMode      bool                 // Whether we're in account management mode
	manageCursor           int                  // Cursor position in account management menu
	commandMode            bool                 // Whether we're in command input mode
	commandInput           string               // Current command being typed
	commandOutput          string               // Output from last command execution
	history                *util.HistoryStore   // Command history store
	historyCursor          int                  // Cursor position in history list
	showHistoryPanel       bool                 // Whether to show the history panel
	outputScroll           int                  // Scroll position in output panel
	outputCursor           int                  // Selected line in output panel
}

func NewModel(devices []client.Device, version string, sshUsername string, accounts []client.AccountInfo) model {
	// Sort devices with online devices first
	sortDevicesByStatus(devices)

	// Find active account
	activeAccount := ""
	for _, acc := range accounts {
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
	history, err := util.NewHistoryStore()
	if err != nil {
		// If history fails to load, continue without it
		history = nil
	}

	return model{
		devices:                devices,
		filteredDevices:        devices, // Initially show all devices
		cursor:                 0,
		selected:               -1,
		viewportTop:            0,
		searchMode:             false,
		searchQuery:            "",
		activeFocus:            focusList,
		version:                version,
		usernamePrompt:         false,
		usernameInput:          "",
		accounts:               accounts,
		reloading:              false,
		sshUsername:            sshUsername,
		profileSelectMode:      false,
		profileCursor:          0,
		selectedProfile:        "", // Empty means show all
		activeAccount:          activeAccount,
		tailscaleActiveAccount: tailscaleActiveAccount,
		showInstallSuggestion:  showInstallSuggestion,
		installationBroken:     installationBroken,
		accountManageMode:      false,
		manageCursor:           0,
		commandMode:            false,
		commandInput:           "",
		commandOutput:          "",
		history:                history,
		historyCursor:          0,
		showHistoryPanel:       false,
		outputCursor:           0,
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

// reloadDevices fetches fresh device list from all accounts
func (m model) reloadDevices() tea.Cmd {
	return func() tea.Msg {
		if len(m.accounts) == 0 {
			return reloadMsg{
				devices: nil,
				err:     fmt.Errorf("no accounts configured for reload"),
			}
		}

		// Fetch devices from all accounts
		devices := client.ListDevicesFromAccounts(m.accounts)

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
			m.sshError = msg.err
		}
		return m, nil

	case copiedMsg:
		if msg.success {
			m.copiedText = msg.text
			// Clear the message after 3 seconds
			return m, tea.Tick(time.Second*3, func(time.Time) tea.Msg {
				return clearCopiedMsg{}
			})
		}
		return m, nil

	case clearCopiedMsg:
		m.copiedText = ""
		return m, nil

	case clearReloadMsg:
		m.reloadSuccess = ""
		return m, nil

	case usernameStoredMsg:
		if msg.err != nil {
			m.sshError = msg.err
		}
		return m, nil

	case tailscaleUpMsg:
		// Tailscale up command completed
		if msg.err != nil {
			m.sshError = fmt.Errorf("tailscale up failed: %w", msg.err)
		}
		return m, nil

	case tailscaleDownMsg:
		// Tailscale down command completed
		if msg.err != nil {
			m.sshError = fmt.Errorf("tailscale down failed: %w", msg.err)
		} else {
			m.tailscaleActiveAccount = "<not connected>"
		}
		return m, nil

	case addAccountMsg:
		// Handle account addition result
		if msg.err != nil {
			m.sshError = fmt.Errorf("failed to add account: %w", msg.err)
		} else {
			// Account added successfully, could reload devices here
			// For now, just clear any previous errors
			m.sshError = nil
		}
		return m, nil

	case accountSwitchedMsg:
		// Handle account switch result
		if msg.err != nil {
			m.sshError = fmt.Errorf("failed to switch to account %s: %w", msg.accountName, msg.err)
			return m, nil
		}
		// Account switched successfully, update active account
		m.activeAccount = msg.accountName
		// Update the real Tailscale active account
		m.tailscaleActiveAccount = getRealTailscaleAccount()
		// If we should proceed with SSH, do it now
		if msg.proceedWithSSH {
			// Check if username is stored
			if m.sshUsername == "" {
				// Prompt for username
				m.usernamePrompt = true
				m.usernameInput = ""
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
			m.sshError = fmt.Errorf("failed to reload devices: %w", msg.err)
			return m, nil
		}

		// Sort devices before storing
		sortDevicesByStatus(msg.devices)
		m.devices = msg.devices

		// Re-apply filters if any are active
		if m.searchQuery != "" || m.selectedProfile != "" {
			m.filterDevices()
		} else {
			m.filteredDevices = msg.devices
		}

		// Reset cursor if out of bounds
		if m.cursor >= len(m.filteredDevices) {
			m.cursor = 0
		}
		if m.selected >= len(m.filteredDevices) {
			m.selected = -1
		}

		// Clear any previous SSH error
		m.sshError = nil

		// Show reload success message
		m.reloadSuccess = fmt.Sprintf("Reloaded %d device(s) from %d account(s)", len(msg.devices), len(m.accounts))

		// Clear the success message after 3 seconds
		return m, tea.Tick(time.Second*3, func(time.Time) tea.Msg {
			return clearReloadMsg{}
		})

	case commandExecutedMsg:
		// Handle command execution result
		if msg.err != nil {
			m.sshError = fmt.Errorf("command failed: %w", msg.err)
			m.commandOutput = ""
			m.outputCursor = 0
			m.outputScroll = 0
		} else {
			m.commandOutput = msg.output
			m.outputScroll = 0 // Reset scroll when new output arrives
			m.outputCursor = 0
			m.sshError = nil
		}
		return m, nil

	case pasteMsg:
		// Silently ignore paste errors (clipboard tool missing, etc.)
		if msg.err != nil || msg.text == "" {
			return m, nil
		}
		switch msg.target {
		case pasteTargetUsername:
			m.usernameInput += msg.text
		case pasteTargetSearch:
			m.searchQuery += msg.text
			m.filterDevices()
		case pasteTargetCommand:
			m.commandInput += msg.text
		}
		return m, nil

	case tea.KeyMsg:
		// Dispatch to appropriate mode handler
		if m.usernamePrompt {
			return m.handleUsernamePrompt(msg)
		}
		if m.commandMode {
			return m.handleCommandMode(msg)
		}
		if m.searchMode {
			return m.handleSearchMode(msg)
		}
		if m.profileSelectMode {
			return m.handleProfileSelection(msg)
		}
		if m.accountManageMode {
			return m.handleAccountManagement(msg)
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
	if m.profileSelectMode {
		return m.renderProfileSelection()
	}

	// Show account management view if in account management mode
	if m.accountManageMode {
		return m.renderAccountManagement()
	}

	var b strings.Builder

	// Title
	title := fmt.Sprintf("Tailscale Devices (ts-cli v%s)", m.version)
	if m.reloading {
		title += " - Reloading..."
		b.WriteString(titleStyle.Render(title))
	} else if m.usernamePrompt {
		title += " - SSH Username: " + m.usernameInput + "_"
		b.WriteString(titleStyle.Render(title))
	} else if m.searchQuery != "" {
		title += fmt.Sprintf(" - Filtered: %d/%d", len(m.filteredDevices), len(m.devices))
		b.WriteString(titleStyle.Render(title))
	} else if m.selectedProfile != "" {
		title += fmt.Sprintf(" - Profile: %s", m.selectedProfile)
		b.WriteString(titleStyle.Render(title))
	} else {
		b.WriteString(titleStyle.Render(title))
	}
	b.WriteString("\n")

	// Show real active Tailscale account at the top
	if m.tailscaleActiveAccount != "" {
		b.WriteString(grayItalicStyle.Render(fmt.Sprintf("Active account: %s", m.tailscaleActiveAccount)))
		b.WriteString("\n")
	}

	// Show default SSH username
	if m.sshUsername != "" {
		b.WriteString(grayItalicStyle.Render(fmt.Sprintf("Default username: %s", m.sshUsername)))
	} else {
		b.WriteString(grayItalicStyle.Render("Default username: <none>"))
	}
	b.WriteString("\n")

	// Render split view if history panel is shown
	if m.showHistoryPanel {
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
	if m.commandOutput != "" && !m.showHistoryPanel {
		b.WriteString("\n\n")
		outputStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#7A7A7A")).
			Padding(1).
			MarginTop(1)
		b.WriteString(outputStyle.Render("Command Output:\n" + m.commandOutput))
	}

	// Show copy success message if any
	if m.copiedText != "" {
		b.WriteString("\n")
		b.WriteString(successStyle.Render(fmt.Sprintf("✓ Copied to clipboard: %s", m.copiedText)))
	}

	// Show reload success message if any
	if m.reloadSuccess != "" {
		b.WriteString("\n")
		b.WriteString(successStyle.Render(fmt.Sprintf("✓ %s", m.reloadSuccess)))
	}

	// Show SSH error if any
	if m.sshError != nil {
		b.WriteString("\n")
		b.WriteString(errorStyle.Render(fmt.Sprintf("SSH Error: %v", m.sshError)))
	}

	// Show installation suggestion if not in PATH
	if m.showInstallSuggestion && !m.usernamePrompt && !m.searchMode && !m.profileSelectMode && !m.accountManageMode {
		b.WriteString("\n")
		var message string
		if m.installationBroken {
			message = "💡 Tip: Run 'ts-cli install' to reinstall ts-cli (current PATH installation is broken). Press 'x' to dismiss."
		} else {
			message = "💡 Tip: Run 'ts-cli install' to add ts-cli to your PATH for easier access. Press 'x' to dismiss."
		}
		b.WriteString(infoStyle.Render(message))
	}

	// Show terminal size warning if too small
	minWidth := 80
	minHeight := 24
	if m.showHistoryPanel {
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
