package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ihor/ts-cli/client"
	"github.com/ihor/ts-cli/util"
)

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7D56F4")).
			MarginBottom(1)

	selectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF06B7")).
			Bold(true).
			PaddingLeft(2)

	normalStyle = lipgloss.NewStyle().
			PaddingLeft(2)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#626262")).
			MarginTop(1)

	listStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#7D56F4")).
			Padding(1, 2)

	detailStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#7D56F4")).
			Padding(1).
			MarginTop(1)

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF0000")).
			Bold(true).
			MarginTop(1)

	searchLabelStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#8d8405"))

	searchQueryStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#8d8405"))

	grayItalicStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#626262")).
			Italic(true)

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00FF00"))

	infoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFA500"))

	promptLabelStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#7D56F4"))

	promptInputStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#00D7FF"))
)

type sshMsg struct {
	err error
}

type copiedMsg struct {
	success bool
	text    string
}

type clearCopiedMsg struct{}

type clearReloadMsg struct{}

type usernameStoredMsg struct {
	err error
}

type tailscaleUpMsg struct {
	err error
}

type addAccountMsg struct {
	err error
}

type accountSwitchedMsg struct {
	accountName    string
	deviceIndex    int
	err            error
	proceedWithSSH bool
}

type reloadMsg struct {
	devices []client.Device
	err     error
}

type commandExecutedMsg struct {
	output   string
	exitCode int
	err      error
}

type panelFocus int

const (
	focusList panelFocus = iota
	focusSearch
	focusSSH
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
		} else {
			m.commandOutput = msg.output
			m.sshError = nil
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

// handleUsernamePrompt handles key presses in username prompt mode
func (m model) handleUsernamePrompt(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		// Cancel username prompt
		m.usernamePrompt = false
		m.usernameInput = ""
		return m, nil
	case "enter":
		// Confirm username and initiate SSH
		if m.usernameInput != "" {
			// Sanitize and validate username
			sanitized := util.SanitizeInput(m.usernameInput)
			if err := util.ValidateSSHUsername(sanitized); err != nil {
				// Ignore invalid input - user needs to re-enter
				m.usernameInput = ""
				return m, nil
			}

			m.sshUsername = sanitized
			m.usernamePrompt = false
			m.usernameInput = ""

			// Store username for future use
			cmd := m.storeUsername(m.sshUsername)

			// SSH to selected device
			target := m.getTargetDevice()
			if target >= 0 && target < len(m.filteredDevices) {
				return m, tea.Batch(cmd, m.sshToDevice(target))
			}
			return m, cmd
		}
		return m, nil
	case "backspace":
		if len(m.usernameInput) > 0 {
			m.usernameInput = m.usernameInput[:len(m.usernameInput)-1]
		}
		return m, nil
	default:
		// Add character to username input
		if len(msg.String()) == 1 {
			m.usernameInput += msg.String()
		}
		return m, nil
	}
}

// handleSearchMode handles key presses in search mode
func (m model) handleSearchMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		// Exit search mode
		m.searchMode = false
		m.searchQuery = ""
		m.filterDevices()
		return m, nil
	case "enter":
		// Confirm search and exit search mode
		m.searchMode = false
		return m, nil
	case "backspace":
		if len(m.searchQuery) > 0 {
			m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
			m.filterDevices()
		}
		return m, nil
	default:
		// Add character to search query
		if len(msg.String()) == 1 {
			m.searchQuery += msg.String()
			m.filterDevices()
		}
		return m, nil
	}
}

// handleCommandMode handles key presses in command input mode
func (m model) handleCommandMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		// Cancel command mode
		m.commandMode = false
		m.commandInput = ""
		return m, nil
	case "enter":
		// Execute command if input is not empty
		if m.commandInput != "" {
			command := util.SanitizeInput(m.commandInput)
			m.commandMode = false
			m.commandInput = ""
			return m, m.executeRemoteCommand(command)
		}
		return m, nil
	case "backspace":
		if len(m.commandInput) > 0 {
			m.commandInput = m.commandInput[:len(m.commandInput)-1]
		}
		return m, nil
	default:
		// Add character to command input
		if len(msg.String()) == 1 {
			m.commandInput += msg.String()
		}
		return m, nil
	}
}

// handleProfileSelection handles key presses in profile selection mode
func (m model) handleProfileSelection(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	numProfiles := len(m.accounts) + 1 // +1 for "All Accounts" option
	switch msg.String() {
	case "esc", "ctrl+c", "q":
		// Exit profile selection mode without changing selection
		m.profileSelectMode = false
		return m, nil
	case "enter":
		// Confirm profile selection
		if m.profileCursor == 0 {
			// "All Accounts" selected
			m.selectedProfile = ""
		} else if m.profileCursor <= len(m.accounts) {
			// Specific account selected
			m.selectedProfile = m.accounts[m.profileCursor-1].Name
		}
		m.profileSelectMode = false
		m.filterDevices()
		return m, nil
	case "up", "k":
		if m.profileCursor > 0 {
			m.profileCursor--
		}
		return m, nil
	case "down", "j":
		if m.profileCursor < numProfiles-1 {
			m.profileCursor++
		}
		return m, nil
	}
	return m, nil
}

// handleAccountManagement handles key presses in account management mode
func (m model) handleAccountManagement(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	numOptions := 1 // Currently just "Add Account"
	switch msg.String() {
	case "esc", "ctrl+c", "q":
		// Exit account management mode
		m.accountManageMode = false
		return m, nil
	case "enter":
		// Execute selected option
		m.accountManageMode = false
		if m.manageCursor == 0 {
			// Add account option
			return m, m.runAddAccount()
		}
		return m, nil
	case "up", "k":
		if m.manageCursor > 0 {
			m.manageCursor--
		}
		return m, nil
	case "down", "j":
		if m.manageCursor < numOptions-1 {
			m.manageCursor++
		}
		return m, nil
	}
	return m, nil
}

// handleNormalMode handles key presses in normal (default) mode
func (m model) handleNormalMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit

	case "/":
		// Enter search mode (vim-style)
		m.searchMode = true
		m.searchQuery = ""
		return m, nil

	case "up", "k":
		m.moveCursorUp()

	case "down", "j":
		m.moveCursorDown()

	case "enter", " ":
		m.selected = m.cursor
		// Clear SSH error when selecting
		m.sshError = nil

	case "c":
		// Copy SSH command to clipboard
		target := m.getTargetDevice()
		if target >= 0 && target < len(m.filteredDevices) {
			return m, m.copySSHCommand(target)
		}

	case "s":
		return m.handleSSHRequest()

	case "u":
		// Run tailscale up
		return m, m.runTailscaleUp()

	case "m":
		// Enter account management mode
		m.accountManageMode = true
		m.manageCursor = 0
		return m, nil

	case "r":
		// Reload devices
		if !m.reloading {
			m.reloading = true
			m.sshError = nil // Clear previous errors
			return m, m.reloadDevices()
		}

	case "p":
		// Enter profile selection mode
		m.profileSelectMode = true
		m.profileCursor = 0
		// If a profile is already selected, position cursor there
		if m.selectedProfile != "" {
			for i, acc := range m.accounts {
				if acc.Name == m.selectedProfile {
					m.profileCursor = i + 1 // +1 because index 0 is "All Accounts"
					break
				}
			}
		}
		return m, nil

	case "x":
		// Dismiss installation suggestion
		if m.showInstallSuggestion {
			m.showInstallSuggestion = false
		}
		return m, nil

	case "d":
		// Clear default SSH username
		if m.sshUsername != "" {
			m.sshUsername = ""
			return m, m.clearUsername()
		}
		return m, nil

	case "e":
		// Enter command execution mode
		target := m.getTargetDevice()
		if target >= 0 && target < len(m.filteredDevices) {
			device := m.filteredDevices[target]
			// Check if device is online
			if !isDeviceOnline(device) {
				deviceName := device.Name
				if deviceName == "" {
					deviceName = device.Hostname
				}
				m.sshError = fmt.Errorf("Machine \"%s\" is offline", deviceName)
				return m, nil
			}
			// Enter command mode
			m.commandMode = true
			m.commandInput = ""
			m.commandOutput = ""
			m.sshError = nil
			return m, nil
		}
		return m, nil
	}
	return m, nil
}

// getTargetDevice returns the index of the target device (selected or cursor)
func (m model) getTargetDevice() int {
	target := m.selected
	if target < 0 {
		target = m.cursor
	}
	return target
}

// moveCursorUp moves the cursor up and adjusts viewport if needed
func (m *model) moveCursorUp() {
	if m.cursor > 0 {
		m.cursor--
		// Scroll up if cursor goes above viewport
		if m.cursor < m.viewportTop {
			m.viewportTop = m.cursor
		}
		// Clear SSH error when moving cursor
		m.sshError = nil
	}
}

// moveCursorDown moves the cursor down and adjusts viewport if needed
func (m *model) moveCursorDown() {
	if m.cursor < len(m.filteredDevices)-1 {
		m.cursor++
		// Scroll down if cursor goes below viewport
		maxVisible := m.getMaxVisibleItems()
		if m.cursor >= m.viewportTop+maxVisible {
			m.viewportTop = m.cursor - maxVisible + 1
		}
		// Clear SSH error when moving cursor
		m.sshError = nil
	}
}

// handleSSHRequest handles the SSH request logic
func (m model) handleSSHRequest() (tea.Model, tea.Cmd) {
	target := m.getTargetDevice()
	if target < 0 || target >= len(m.filteredDevices) {
		return m, nil
	}

	device := m.filteredDevices[target]

	// Check if device is offline
	if !isDeviceOnline(device) {
		deviceName := device.Name
		if deviceName == "" {
			deviceName = device.Hostname
		}
		m.sshError = fmt.Errorf("Machine \"%s\" is offline", deviceName)
		return m, nil
	}

	// Check if Tailscale is running locally before attempting SSH
	if isRunning, message := checkLocalTailscaleStatus(); !isRunning {
		m.sshError = fmt.Errorf("Tailscale is not running locally: %s\nPress 'u' to run 'tailscale up' or start Tailscale manually", message)
		return m, nil
	}

	// Clear any previous SSH errors
	m.sshError = nil

	// Check if we need to switch accounts first
	// Compare device's account tailnet against the real Tailscale active account
	if device.AccountTailnet != "" && m.tailscaleActiveAccount != "" {
		// Normalize both for comparison (handle truncated accounts like "user@" vs "user@domain.com")
		deviceAccount := strings.ToLower(device.AccountTailnet)
		activeAccount := strings.ToLower(m.tailscaleActiveAccount)

		// Check if they're different accounts
		// Account in status might be truncated, so check if one starts with the other
		needsSwitch := !strings.HasPrefix(deviceAccount, strings.TrimSuffix(activeAccount, "@")) &&
			!strings.HasPrefix(activeAccount, strings.TrimSuffix(deviceAccount, "@"))

		if needsSwitch {
			// Need to switch Tailscale account before SSH
			return m, m.switchAccountForSSH(target, device.AccountTailnet)
		}
	}

	// Check if username is stored
	if m.sshUsername == "" {
		// Prompt for username
		m.usernamePrompt = true
		m.usernameInput = ""
		return m, nil
	}
	// Username exists, start SSH session
	return m, m.sshToDevice(target)
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
	} else if m.commandMode {
		title += " - Execute Command: " + m.commandInput + "_"
		b.WriteString(titleStyle.Render(title))
	} else if m.searchMode {
		// Render search with different colors on the same line
		baseTitle := titleStyle.Render(title)
		searchLabel := searchLabelStyle.Render("> Search: ")
		searchInput := searchQueryStyle.Render(m.searchQuery + "_")
		b.WriteString(baseTitle)
		b.WriteString("\n")
		b.WriteString(searchLabel + searchInput)
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

	// Show search scope or "all" when viewing all profiles
	if m.selectedProfile == "" {
		// When no profile filter is set, show "all"
		b.WriteString(grayItalicStyle.Render("Search in: all"))
		b.WriteString("\n")
	} else {
		// When a profile is selected, show the selected profile
		b.WriteString(grayItalicStyle.Render(fmt.Sprintf("Search in: %s", m.selectedProfile)))
		b.WriteString("\n")
	}

	// Show default SSH username
	if m.sshUsername != "" {
		b.WriteString(grayItalicStyle.Render(fmt.Sprintf("Default username: %s", m.sshUsername)))
	} else {
		b.WriteString(grayItalicStyle.Render("Default username: <none>"))
	}
	b.WriteString("\n")

	// Render device list
	b.WriteString(m.renderDeviceList())

	// Device details if selected
	if m.selected >= 0 && m.selected < len(m.filteredDevices) {
		b.WriteString(m.renderDeviceDetails())
	}

	// Help text
	help := "↑/k up • ↓/j down • / search • enter select • s ssh • c copy • e cmd • p profile • r reload • u tailscale-up • m manage"
	if m.sshUsername != "" {
		help += " • d clear-user"
	}
	help += " • q quit"
	if m.usernamePrompt {
		help = "Enter SSH username • esc cancel • enter confirm"
	} else if m.commandMode {
		help = "Type command to execute • esc cancel • enter execute"
	} else if m.searchMode {
		help = "Type to search • esc cancel • enter confirm"
	}
	b.WriteString(helpStyle.Render(help))

	// Show command output if any
	if m.commandOutput != "" {
		b.WriteString("\n\n")
		outputStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#00FF00")).
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

	return b.String()
}

// renderDeviceList renders the device list panel
func (m model) renderDeviceList() string {
	var listContent strings.Builder
	maxVisible := m.getMaxVisibleItems()

	// Calculate visible range
	visibleStart := m.viewportTop
	visibleEnd := m.viewportTop + maxVisible
	if visibleEnd > len(m.filteredDevices) {
		visibleEnd = len(m.filteredDevices)
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

		// Get status icon
		statusIcon := getStatusIcon(device)

		line := fmt.Sprintf("%s%s %-28s %s", cursor, statusIcon, name, address)
		listContent.WriteString(style.Render(line))
		listContent.WriteString("\n")
	}

	// Show scroll indicator at bottom if needed
	if visibleEnd < len(m.filteredDevices) {
		listContent.WriteString(normalStyle.Render("  ↓ more below ↓"))
	}

	// Render the list in a frame
	return listStyle.Render(listContent.String())
}

// renderDeviceDetails renders the selected device details panel
func (m model) renderDeviceDetails() string {
	device := m.filteredDevices[m.selected]
	name := device.Name
	if name == "" {
		name = device.Hostname
	}

	statusText := "🟢 Online"
	if !isDeviceOnline(device) {
		statusText = "🔴 Offline"
	}

	details := fmt.Sprintf(
		"Selected Device\n\n"+
			"Name:       %s\n"+
			"Hostname:   %s\n"+
			"Status:     %s\n"+
			"OS:         %s\n"+
			"Authorized: %t\n"+
			"Address:    %v\n"+
			"ID:         %s",
		name,
		device.Hostname,
		statusText,
		device.OS,
		device.Authorized,
		strings.Join(device.Addresses, ", "),
		device.ID,
	)

	return detailStyle.Render(details) + "\n"
}

// getMaxVisibleItems calculates how many items can fit in the viewport
func (m model) getMaxVisibleItems() int {
	// If we don't have terminal size yet, use a default
	if m.height == 0 {
		return 10
	}

	// Account for: title (2 lines), frame borders (4 lines), detail panel (~10 lines), help (2 lines), padding (4)
	// This leaves space for the device list
	availableHeight := m.height - 22
	if availableHeight < 5 {
		availableHeight = 5 // Minimum visible items
	}

	return availableHeight
}

// renderProfileSelection renders the profile selection view
func (m model) renderProfileSelection() string {
	var b strings.Builder

	// Title
	title := fmt.Sprintf("Select Profile (ts-cli v%s)", m.version)
	b.WriteString(titleStyle.Render(title))
	b.WriteString("\n\n")

	// Account list
	profileList := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 2).
		Width(60)

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

	// Help text
	help := "↑/k up • ↓/j down • enter select • esc/q cancel"
	b.WriteString(helpStyle.Render(help))

	return b.String()
}

// renderAccountManagement renders the account management view
func (m model) renderAccountManagement() string {
	var b strings.Builder

	// Title
	title := fmt.Sprintf("Account Management (ts-cli v%s)", m.version)
	b.WriteString(titleStyle.Render(title))
	b.WriteString("\n\n")

	// Options list
	optionsList := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 2).
		Width(60)

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

	// Help text
	help := "↑/k up • ↓/j down • enter select • esc/q cancel"
	b.WriteString(helpStyle.Render(help))

	return b.String()
}

// sshToDevice creates a command to SSH into a device
func (m model) sshToDevice(index int) tea.Cmd {
	device := m.filteredDevices[index]

	// Note: Account switching is handled by handleSSHRequest before calling this function

	// Get the primary IP address
	if len(device.Addresses) == 0 {
		return func() tea.Msg {
			return sshMsg{err: fmt.Errorf("device has no IP addresses")}
		}
	}

	address := device.Addresses[0]
	name := device.Name
	if name == "" {
		name = device.Hostname
	}

	// Build SSH command with username if available
	var sshTarget string
	if m.sshUsername != "" {
		sshTarget = fmt.Sprintf("%s@%s", m.sshUsername, address)
	} else {
		sshTarget = address
	}

	// Log SSH connection details with account information
	accountLabel := "default"
	if device.AccountName != "" {
		accountLabel = device.AccountName
	}

	// Use tea.Sequence to print logs then execute SSH
	return tea.Sequence(
		tea.Println(fmt.Sprintf("\n🔌 Connecting to %s : %s", name, accountLabel)),
		tea.Println(fmt.Sprintf("📡 SSH command: ssh %s\n", sshTarget)),
		func() tea.Msg {
			// Use tea.ExecProcess to suspend TUI and run SSH
			sshCmd := exec.Command("ssh", sshTarget)
			return tea.ExecProcess(sshCmd, func(err error) tea.Msg {
				if err != nil {
					return sshMsg{err: err}
				}
				return sshMsg{}
			})()
		},
	)
}

// executeRemoteCommand executes a command on a remote device via SSH
func (m model) executeRemoteCommand(command string) tea.Cmd {
	target := m.getTargetDevice()
	if target < 0 || target >= len(m.filteredDevices) {
		return func() tea.Msg {
			return commandExecutedMsg{err: fmt.Errorf("no device selected")}
		}
	}

	device := m.filteredDevices[target]

	// Get the primary IP address
	if len(device.Addresses) == 0 {
		return func() tea.Msg {
			return commandExecutedMsg{err: fmt.Errorf("device has no IP addresses")}
		}
	}

	address := device.Addresses[0]
	name := device.Name
	if name == "" {
		name = device.Hostname
	}

	// Build SSH target
	var sshTarget string
	if m.sshUsername != "" {
		sshTarget = fmt.Sprintf("%s@%s", m.sshUsername, address)
	} else {
		sshTarget = address
	}

	// Get machine ID for history
	machineID := device.ID
	if machineID == "" {
		machineID = device.Hostname
	}

	return func() tea.Msg {
		// Execute command via SSH
		cmd := exec.Command("ssh", sshTarget, command)
		output, err := cmd.CombinedOutput()

		exitCode := 0
		if err != nil {
			// Try to get exit code
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			}
		}

		// Save to history if history store is available
		if m.history != nil {
			m.history.AddCommand(machineID, name, command, exitCode, string(output))
			_ = m.history.Save() // Ignore save errors
		}

		return commandExecutedMsg{
			output:   string(output),
			exitCode: exitCode,
			err:      err,
		}
	}
}

// copySSHCommand copies the SSH command to the clipboard
func (m model) copySSHCommand(index int) tea.Cmd {
	device := m.filteredDevices[index]

	// Get the primary IP address
	if len(device.Addresses) == 0 {
		return func() tea.Msg {
			return copiedMsg{success: false, text: ""}
		}
	}

	address := device.Addresses[0]

	// Build SSH command with username if available
	var sshCommand string
	if m.sshUsername != "" {
		sshCommand = fmt.Sprintf("ssh %s@%s", m.sshUsername, address)
	} else {
		sshCommand = fmt.Sprintf("ssh %s", address)
	}

	// Determine clipboard command based on OS
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin": // macOS
		cmd = exec.Command("pbcopy")
	case "linux":
		cmd = exec.Command("xclip", "-selection", "clipboard")
	case "windows":
		cmd = exec.Command("clip")
	default:
		return func() tea.Msg {
			return copiedMsg{success: false, text: ""}
		}
	}

	// Write command to clipboard
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return func() tea.Msg {
			return copiedMsg{success: false, text: ""}
		}
	}

	if err := cmd.Start(); err != nil {
		return func() tea.Msg {
			return copiedMsg{success: false, text: ""}
		}
	}

	if _, err := stdin.Write([]byte(sshCommand)); err != nil {
		return func() tea.Msg {
			return copiedMsg{success: false, text: ""}
		}
	}

	stdin.Close()
	cmd.Wait()

	return func() tea.Msg {
		return copiedMsg{success: true, text: sshCommand}
	}
}

// storeUsername stores the SSH username preference
func (m model) storeUsername(username string) tea.Cmd {
	return func() tea.Msg {
		// We need to import the commands package, but that creates a cycle
		// So we'll implement the storage directly here
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return usernameStoredMsg{err: err}
		}

		configDir := filepath.Join(homeDir, ".ts-cli")
		if err := os.MkdirAll(configDir, 0700); err != nil {
			return usernameStoredMsg{err: err}
		}

		// Read existing config
		configFile := filepath.Join(configDir, "config")
		content, err := os.ReadFile(configFile)
		if err != nil && !os.IsNotExist(err) {
			return usernameStoredMsg{err: err}
		}

		// Parse existing config and update SSH_USERNAME
		lines := []string{}
		found := false
		for _, line := range strings.Split(string(content), "\n") {
			if strings.HasPrefix(line, "SSH_USERNAME=") {
				lines = append(lines, fmt.Sprintf("SSH_USERNAME=%s", username))
				found = true
			} else if line != "" {
				lines = append(lines, line)
			}
		}

		// Add SSH_USERNAME if not found
		if !found {
			lines = append(lines, fmt.Sprintf("SSH_USERNAME=%s", username))
		}

		// Write back
		newContent := strings.Join(lines, "\n") + "\n"
		if err := os.WriteFile(configFile, []byte(newContent), 0600); err != nil {
			return usernameStoredMsg{err: err}
		}

		return usernameStoredMsg{err: nil}
	}
}

// clearUsername removes the stored SSH username from config
func (m model) clearUsername() tea.Cmd {
	return func() tea.Msg {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return usernameStoredMsg{err: err}
		}

		configDir := filepath.Join(homeDir, ".ts-cli")
		configFile := filepath.Join(configDir, "config")

		// Read existing config
		content, err := os.ReadFile(configFile)
		if err != nil {
			if os.IsNotExist(err) {
				// No config file, nothing to clear
				return usernameStoredMsg{err: nil}
			}
			return usernameStoredMsg{err: err}
		}

		// Parse existing config and remove SSH_USERNAME
		lines := []string{}
		for _, line := range strings.Split(string(content), "\n") {
			if !strings.HasPrefix(line, "SSH_USERNAME=") && line != "" {
				lines = append(lines, line)
			}
		}

		// Write back
		newContent := strings.Join(lines, "\n")
		if newContent != "" {
			newContent += "\n"
		}
		if err := os.WriteFile(configFile, []byte(newContent), 0600); err != nil {
			return usernameStoredMsg{err: err}
		}

		return usernameStoredMsg{err: nil}
	}
}

// checkIfInstallNeeded checks if ts-cli needs to be installed or is improperly installed
// Returns (needsInstall, isBroken)
func checkIfInstallNeeded() (bool, bool) {
	// Check if either ts-cli or tsc is in PATH
	tsCliPath, tsCliErr := exec.LookPath("ts-cli")
	tscPath, tscErr := exec.LookPath("tsc")

	// If both are not found, suggest installation
	if tsCliErr != nil && tscErr != nil {
		return true, false
	}

	// Try to verify the found binary (prefer ts-cli)
	pathToCheck := tsCliPath
	if tsCliErr != nil {
		pathToCheck = tscPath
	}

	// Verify it's a valid binary
	// Resolve any symlinks to get the actual binary path
	resolvedPath, err := filepath.EvalSymlinks(pathToCheck)
	if err != nil {
		// Can't resolve symlink, broken installation
		return true, true
	}

	// Check if the resolved path is a valid executable file
	fileInfo, err := os.Stat(resolvedPath)
	if err != nil || fileInfo.IsDir() {
		// File doesn't exist or is a directory, broken installation
		return true, true
	}

	// Check if it's executable (on Unix-like systems)
	if runtime.GOOS != "windows" {
		if fileInfo.Mode()&0111 == 0 {
			// Not executable, broken installation
			return true, true
		}
	}

	// ts-cli or tsc is properly installed
	return false, false
}

// isDeviceOnline checks if a device is considered online based on LastSeen time
func isDeviceOnline(device client.Device) bool {
	// Consider a device online if it was seen within the last 5 minutes
	return time.Since(device.LastSeen) < 5*time.Minute
}

// sortDevicesByStatus sorts devices with online devices first
func sortDevicesByStatus(devices []client.Device) {
	sort.SliceStable(devices, func(i, j int) bool {
		onlineI := isDeviceOnline(devices[i])
		onlineJ := isDeviceOnline(devices[j])

		// Online devices come first
		if onlineI && !onlineJ {
			return true
		}
		if !onlineI && onlineJ {
			return false
		}

		// If both have same online status, maintain original order (stable sort)
		return false
	})
}

// switchTailscaleAccount switches the active Tailscale account
func switchTailscaleAccount(accountName string) error {
	cmd := exec.Command("tailscale", "switch", accountName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to switch account: %w (output: %s)", err, string(output))
	}
	return nil
}

// switchAccountForSSH returns a command that switches Tailscale account and prepares for SSH
func (m model) switchAccountForSSH(deviceIndex int, accountName string) tea.Cmd {
	return tea.Sequence(
		tea.Println(fmt.Sprintf("\n🔄 Switching to account: %s", accountName)),
		func() tea.Msg {
			err := switchTailscaleAccount(accountName)
			if err == nil {
				tea.Println(fmt.Sprintf("✓ Switched to account: %s", accountName))
			}
			return accountSwitchedMsg{
				accountName:    accountName,
				err:            err,
				proceedWithSSH: true,
				deviceIndex:    deviceIndex,
			}
		},
	)
}

// getStatusIcon returns the appropriate status icon for a device
func getStatusIcon(device client.Device) string {
	if isDeviceOnline(device) {
		return "🟢"
	}
	return "🔴"
}

// filterDevices filters the device list based on the search query
func (m *model) filterDevices() {
	// Start with all devices
	filtered := m.devices

	// Apply profile filter first
	if m.selectedProfile != "" {
		profileFiltered := []client.Device{}
		for _, device := range filtered {
			if device.AccountName == m.selectedProfile {
				profileFiltered = append(profileFiltered, device)
			}
		}
		filtered = profileFiltered
	}

	// Apply search filter if query exists
	if m.searchQuery != "" {
		query := strings.ToLower(m.searchQuery)
		searchFiltered := []client.Device{}

		for _, device := range filtered {
			name := strings.ToLower(device.Name)
			hostname := strings.ToLower(device.Hostname)
			os := strings.ToLower(device.OS)

			// Search in name, hostname, OS, and addresses
			if strings.Contains(name, query) ||
				strings.Contains(hostname, query) ||
				strings.Contains(os, query) {
				searchFiltered = append(searchFiltered, device)
				continue
			}

			// Search in addresses
			for _, addr := range device.Addresses {
				if strings.Contains(strings.ToLower(addr), query) {
					searchFiltered = append(searchFiltered, device)
					break
				}
			}
		}
		filtered = searchFiltered
	}

	// Sort devices with online devices first
	sortDevicesByStatus(filtered)

	m.filteredDevices = filtered
	// Reset cursor to top of filtered list
	m.cursor = 0
	m.viewportTop = 0
}

// runTailscaleUp runs 'tailscale up' command
func (m model) runTailscaleUp() tea.Cmd {
	return func() tea.Msg {
		tailscaleCmd := exec.Command("tailscale", "up")
		return tea.ExecProcess(tailscaleCmd, func(err error) tea.Msg {
			if err != nil {
				return tailscaleUpMsg{err: err}
			}
			return tailscaleUpMsg{err: nil}
		})()
	}
}

// runAddAccount prompts user to add a new account via login command
func (m model) runAddAccount() tea.Cmd {
	return func() tea.Msg {
		cmd := m.createAddAccountScript()
		return tea.ExecProcess(cmd, func(err error) tea.Msg {
			if err != nil {
				return addAccountMsg{err: err}
			}
			return addAccountMsg{err: nil}
		})()
	}
}

// createAddAccountScript creates an interactive script for adding a new account
func (m model) createAddAccountScript() *exec.Cmd {
	// Get the path to the current ts-cli executable
	execPath, err := os.Executable()
	if err != nil {
		// Fallback to assuming ts-cli is in PATH
		execPath = "ts-cli"
	}

	// Create an interactive shell script that guides the user
	script := fmt.Sprintf(`#!/bin/bash
set -e

# Colors for better UX
BLUE='\033[0;34m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

clear
echo -e "${BLUE}============================================${NC}"
echo -e "${BLUE}       Add New Tailscale Account${NC}"
echo -e "${BLUE}============================================${NC}"
echo ""
echo -e "${YELLOW}To add a new account, you need:${NC}"
echo "  1. Your tailnet name (e.g., example.com)"
echo "  2. A Tailscale API key"
echo ""
echo -e "${YELLOW}To generate an API key:${NC}"
echo "  1. Visit: https://login.tailscale.com/admin/settings/keys"
echo "  2. Click 'Generate API key'"
echo "  3. Give it a description (e.g., 'ts-cli')"
echo "  4. Copy the key (starts with 'tskey-api-')"
echo ""
echo "Press Enter to continue (or Ctrl+C to cancel)..."
read

# Prompt for tailnet
echo ""
echo -e "${BLUE}Enter your tailnet name:${NC}"
echo -n "Tailnet: "
read TAILNET

if [ -z "$TAILNET" ]; then
    echo -e "${YELLOW}Tailnet name cannot be empty. Exiting.${NC}"
    sleep 2
    exit 1
fi

# Prompt for API key
echo ""
echo -e "${BLUE}Enter your Tailscale API key:${NC}"
echo -n "API Key: "
read -s API_KEY
echo ""

if [ -z "$API_KEY" ]; then
    echo -e "${YELLOW}API key cannot be empty. Exiting.${NC}"
    sleep 2
    exit 1
fi

# Run the login command
echo ""
echo -e "${BLUE}Validating and saving account...${NC}"
%s login --tailnet="$TAILNET" --api-key="$API_KEY"

if [ $? -eq 0 ]; then
    echo ""
    echo -e "${GREEN}✓ Account added successfully!${NC}"
    echo ""
    echo "Press Enter to return to interactive mode..."
    read
else
    echo ""
    echo -e "${YELLOW}Failed to add account. Press Enter to continue...${NC}"
    read
    exit 1
fi
`, execPath)

	// Create temp script file
	tmpFile, err := os.CreateTemp("", "ts-cli-add-account-*.sh")
	if err != nil {
		// Fallback to simpler approach
		return exec.Command("sh", "-c", fmt.Sprintf("echo 'Failed to create script: %v'; sleep 2", err))
	}

	if _, err := tmpFile.WriteString(script); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return exec.Command("sh", "-c", fmt.Sprintf("echo 'Failed to write script: %v'; sleep 2", err))
	}

	// Use 0700 (user execute only) for better security
	if err := tmpFile.Chmod(0700); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return exec.Command("sh", "-c", fmt.Sprintf("echo 'Failed to set permissions: %v'; sleep 2", err))
	}

	scriptPath := tmpFile.Name()
	tmpFile.Close()

	// Create a command that runs the script and then deletes it
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", scriptPath)
	} else {
		// Use bash to run the script, then remove it
		cmd = exec.Command("bash", "-c", fmt.Sprintf("%s; rm -f %s", scriptPath, scriptPath))
	}

	return cmd
}

// checkLocalTailscaleStatus checks if Tailscale is running locally
func checkLocalTailscaleStatus() (bool, string) {
	cmd := exec.Command("tailscale", "status")
	output, err := cmd.CombinedOutput()

	if err != nil {
		// Tailscale command failed - daemon might not be running or not installed
		return false, "Tailscale daemon is not running"
	}

	// Check if output indicates we're not connected
	outputStr := string(output)
	if strings.Contains(strings.ToLower(outputStr), "logged out") {
		return false, "You are logged out from Tailscale"
	}

	// Tailscale is running and connected
	return true, ""
}

// getRealTailscaleAccount gets the currently active account from Tailscale daemon
func getRealTailscaleAccount() string {
	cmd := exec.Command("tailscale", "status", "--json")
	output, err := cmd.CombinedOutput()

	if err != nil {
		// If tailscale is not running or not installed, return unknown
		return "<not connected>"
	}

	// Parse JSON to get the account email
	// The status JSON contains a "Self" object with "UserProfile" that has "LoginName"
	// For simplicity, let's extract it using string parsing
	outputStr := string(output)

	// Look for "LoginName" field in JSON
	// Note: JSON is formatted with whitespace, so we need flexible parsing
	loginNamePattern := `"LoginName"`
	if idx := strings.Index(outputStr, loginNamePattern); idx != -1 {
		// Find the colon after "LoginName"
		afterKey := outputStr[idx+len(loginNamePattern):]
		colonIdx := strings.Index(afterKey, ":")
		if colonIdx != -1 {
			// Find the opening quote
			afterColon := afterKey[colonIdx+1:]
			quoteIdx := strings.Index(afterColon, `"`)
			if quoteIdx != -1 {
				// Find the closing quote
				afterQuote := afterColon[quoteIdx+1:]
				endQuoteIdx := strings.Index(afterQuote, `"`)
				if endQuoteIdx != -1 {
					return afterQuote[:endQuoteIdx]
				}
			}
		}
	}

	// Fallback: try to get it from regular status output
	cmd = exec.Command("tailscale", "status")
	output, err = cmd.CombinedOutput()
	if err != nil {
		return "<not connected>"
	}

	// The status output typically shows the account email
	// Parse the first line to extract the account
	// Format is: "IP  hostname  account@domain  OS  status"
	lines := strings.Split(string(output), "\n")
	if len(lines) > 0 && len(lines[0]) > 0 {
		fields := strings.Fields(lines[0])
		// The account is in the 3rd field (index 2)
		if len(fields) >= 3 {
			account := fields[2]
			// Verify it looks like an account (contains @)
			if strings.Contains(account, "@") {
				return account
			}
		}
	}

	return "<not connected>"
}
