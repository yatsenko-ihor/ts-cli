package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ihor/ts-cli/client"
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

	sshPanelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#00D7AF")).
			Padding(1, 2).
			Height(30)

	sshPanelTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#00D7AF")).
				MarginBottom(1)
)

type sshMsg struct {
	err error
}

type copiedMsg struct {
	success bool
	text    string
}

type clearCopiedMsg struct{}

type usernameStoredMsg struct {
	err error
}

type panelFocus int

const (
	focusList panelFocus = iota
	focusSearch
	focusSSH
)

type model struct {
	devices         []client.Device
	filteredDevices []client.Device
	cursor          int
	selected        int
	err             error
	width           int
	height          int
	sshError        error
	viewportTop     int // First visible item in the list
	searchMode      bool
	searchQuery     string
	activeFocus     panelFocus
	copiedText      string
	version         string
	usernamePrompt  bool   // Whether we're prompting for username
	usernameInput   string // Current username being typed
	sshUsername     string // Stored SSH username
	showSSHPanel    bool   // Whether to show the right SSH panel in split mode
}

func NewModel(devices []client.Device, version string, sshUsername string) model {
	return model{
		devices:         devices,
		filteredDevices: devices, // Initially show all devices
		cursor:          0,
		selected:        -1,
		viewportTop:     0,
		searchMode:      false,
		searchQuery:     "",
		activeFocus:     focusList,
		version:         version,
		usernamePrompt:  false,
		usernameInput:   "",
		sshUsername:     sshUsername,
		showSSHPanel:    true, // Start with SSH panel visible
	}
}

func (m model) Init() tea.Cmd {
	return nil
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

	case usernameStoredMsg:
		if msg.err != nil {
			m.sshError = msg.err
		}
		return m, nil

	case tea.KeyMsg:
		// Handle username prompt mode first
		if m.usernamePrompt {
			switch msg.String() {
			case "esc", "ctrl+c":
				// Cancel username prompt
				m.usernamePrompt = false
				m.usernameInput = ""
				return m, nil
			case "enter":
				// Confirm username and initiate SSH
				if m.usernameInput != "" {
					m.sshUsername = m.usernameInput
					m.usernamePrompt = false
					m.usernameInput = ""

					// Store username for future use
					cmd := m.storeUsername(m.sshUsername)

					// SSH to selected device
					target := m.selected
					if target < 0 {
						target = m.cursor
					}
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

		// Handle search mode separately
		if m.searchMode {
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

		// Normal mode key handling
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit

		case "tab":
			// Toggle SSH panel visibility
			m.showSSHPanel = !m.showSSHPanel
			return m, nil

		case "/":
			// Enter search mode (vim-style)
			m.searchMode = true
			m.searchQuery = ""
			return m, nil

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				// Scroll up if cursor goes above viewport
				if m.cursor < m.viewportTop {
					m.viewportTop = m.cursor
				}
				// Clear SSH error when moving cursor
				m.sshError = nil
			}

		case "down", "j":
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

		case "enter", " ":
			m.selected = m.cursor
			// Clear SSH error when selecting
			m.sshError = nil

		case "c":
			// Copy SSH command to clipboard
			target := m.selected
			if target < 0 {
				target = m.cursor
			}
			if target >= 0 && target < len(m.filteredDevices) {
				return m, m.copySSHCommand(target)
			}

		case "s":
			// SSH to currently selected or cursor device
			target := m.selected
			if target < 0 {
				target = m.cursor
			}
			if target >= 0 && target < len(m.filteredDevices) {
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
				
				// Clear any previous SSH errors
				m.sshError = nil
				
				// Check if username is stored
				if m.sshUsername == "" {
					// Prompt for username
					m.usernamePrompt = true
					m.usernameInput = ""
					return m, nil
				}
				// Username exists, SSH directly
				return m, m.sshToDevice(target)
			}
		}
	}

	return m, nil
}

func (m model) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v\n", m.err)
	}

	var b strings.Builder

	// Title
	title := fmt.Sprintf("Tailscale Devices (ts-cli v%s)", m.version)
	if m.usernamePrompt {
		title += " - SSH Username: " + m.usernameInput + "_"
	} else if m.searchMode {
		title += " - Search: /" + m.searchQuery + "_"
	} else if m.searchQuery != "" {
		title += fmt.Sprintf(" - Filtered: %d/%d", len(m.filteredDevices), len(m.devices))
	}
	b.WriteString(titleStyle.Render(title))
	b.WriteString("\n")

	// Render in split mode or single mode
	if m.showSSHPanel && m.width > 80 {
		// Split screen mode - left and right panels
		leftPanel := m.renderLeftPanel()
		rightPanel := m.renderSSHPanel()

		// Join panels horizontally
		panels := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel)
		b.WriteString(panels)
		b.WriteString("\n")
	} else {
		// Single panel mode (original layout)
		b.WriteString(m.renderLeftPanel())
	}

	// Help text
	help := "↑/k up • ↓/j down • / search • enter select • s ssh • c copy • tab panel • q quit"
	if m.usernamePrompt {
		help = "Enter SSH username • esc cancel • enter confirm"
	} else if m.searchMode {
		help = "Type to search • esc cancel • enter confirm"
	}
	b.WriteString(helpStyle.Render(help))

	// Show copy success message if any
	if m.copiedText != "" {
		b.WriteString("\n")
		successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF00"))
		b.WriteString(successStyle.Render(fmt.Sprintf("✓ Copied to clipboard: %s", m.copiedText)))
	}

	// Show SSH error if any
	if m.sshError != nil {
		b.WriteString("\n")
		b.WriteString(errorStyle.Render(fmt.Sprintf("SSH Error: %v", m.sshError)))
	}

	return b.String()
}

// renderLeftPanel renders the device list and details panel
func (m model) renderLeftPanel() string {
	var b strings.Builder

	// Build device list content
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

		// Adjust width for split mode
		nameWidth := 28
		if m.showSSHPanel && m.width > 80 {
			nameWidth = 20
		}

		line := fmt.Sprintf("%s%s %-*s %s", cursor, statusIcon, nameWidth, name, address)
		listContent.WriteString(style.Render(line))
		listContent.WriteString("\n")
	}

	// Show scroll indicator at bottom if needed
	if visibleEnd < len(m.filteredDevices) {
		listContent.WriteString(normalStyle.Render("  ↓ more below ↓"))
	}

	// Render the list in a frame
	listPanel := listStyle.Render(listContent.String())

	// Set width for split mode
	if m.showSSHPanel && m.width > 80 {
		listPanel = lipgloss.NewStyle().Width(m.width/2 - 4).Render(listPanel)
	}

	b.WriteString(listPanel)

	// Device details if selected (only in single panel mode)
	if !m.showSSHPanel && m.selected >= 0 && m.selected < len(m.filteredDevices) {
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

		b.WriteString(detailStyle.Render(details))
		b.WriteString("\n")
	}

	return b.String()
}

// renderSSHPanel renders the right panel with SSH session information
func (m model) renderSSHPanel() string {
	var content strings.Builder

	content.WriteString(sshPanelTitleStyle.Render("SSH Connection"))
	content.WriteString("\n\n")

	// Show selected device info or prompt
	if m.selected >= 0 && m.selected < len(m.filteredDevices) {
		device := m.filteredDevices[m.selected]
		name := device.Name
		if name == "" {
			name = device.Hostname
		}

		address := "N/A"
		if len(device.Addresses) > 0 {
			address = device.Addresses[0]
		}

		statusText := "🟢 Online"
		statusColor := "#00FF00"
		if !isDeviceOnline(device) {
			statusText = "🔴 Offline"
			statusColor = "#FF0000"
		}

		// Device info
		content.WriteString(lipgloss.NewStyle().Bold(true).Render("Device Details"))
		content.WriteString("\n\n")
		content.WriteString(fmt.Sprintf("Name:       %s\n", name))
		content.WriteString(fmt.Sprintf("Hostname:   %s\n", device.Hostname))
		content.WriteString(fmt.Sprintf("Status:     %s\n",
			lipgloss.NewStyle().Foreground(lipgloss.Color(statusColor)).Render(statusText)))
		content.WriteString(fmt.Sprintf("OS:         %s\n", device.OS))
		content.WriteString(fmt.Sprintf("Address:    %s\n", address))
		content.WriteString(fmt.Sprintf("Authorized: %t\n", device.Authorized))
		content.WriteString("\n")

		// SSH command info
		content.WriteString(lipgloss.NewStyle().Bold(true).Render("SSH Command"))
		content.WriteString("\n\n")

		var sshCommand string
		if m.sshUsername != "" {
			sshCommand = fmt.Sprintf("ssh %s@%s", m.sshUsername, address)
		} else {
			sshCommand = fmt.Sprintf("ssh %s", address)
		}

		cmdStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00D7AF")).
			Background(lipgloss.Color("#1a1a1a")).
			Padding(0, 1)
		content.WriteString(cmdStyle.Render(sshCommand))
		content.WriteString("\n\n")

		// Instructions
		content.WriteString(lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("#808080")).Render(
			"Press 's' to open SSH connection\n" +
				"Press 'c' to copy SSH command\n" +
				"Press 'tab' to toggle this panel"))

		if m.sshUsername == "" {
			content.WriteString("\n\n")
			content.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#FFA500")).Render(
				"⚠ No SSH username set\n" +
					"Press 's' to configure"))
		}
	} else {
		// No device selected
		content.WriteString(lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("#808080")).Render(
			"No device selected\n\n" +
				"Select a device from the list\n" +
				"to view SSH connection details.\n\n" +
				"Press 'tab' to toggle this panel"))
	}

	panelWidth := m.width/2 - 4
	if panelWidth < 40 {
		panelWidth = 40
	}

	return lipgloss.NewStyle().
		Width(panelWidth).
		Render(sshPanelStyle.Render(content.String()))
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

// sshToDevice creates a command to SSH into a device
func (m model) sshToDevice(index int) tea.Cmd {
	device := m.filteredDevices[index]

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

	// Use tea.ExecProcess to suspend TUI and run SSH
	sshCmd := exec.Command("ssh", sshTarget)
	return tea.ExecProcess(sshCmd, func(err error) tea.Msg {
		if err != nil {
			return sshMsg{err: err}
		}
		return sshMsg{}
	})
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

// isDeviceOnline checks if a device is considered online based on LastSeen time
func isDeviceOnline(device client.Device) bool {
	// Consider a device online if it was seen within the last 5 minutes
	return time.Since(device.LastSeen) < 5*time.Minute
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
	if m.searchQuery == "" {
		m.filteredDevices = m.devices
		m.cursor = 0
		m.viewportTop = 0
		return
	}

	query := strings.ToLower(m.searchQuery)
	filtered := []client.Device{}

	for _, device := range m.devices {
		name := strings.ToLower(device.Name)
		hostname := strings.ToLower(device.Hostname)
		os := strings.ToLower(device.OS)

		// Search in name, hostname, OS, and addresses
		if strings.Contains(name, query) ||
			strings.Contains(hostname, query) ||
			strings.Contains(os, query) {
			filtered = append(filtered, device)
			continue
		}

		// Search in addresses
		for _, addr := range device.Addresses {
			if strings.Contains(strings.ToLower(addr), query) {
				filtered = append(filtered, device)
				break
			}
		}
	}

	m.filteredDevices = filtered
	// Reset cursor to top of filtered list
	m.cursor = 0
	m.viewportTop = 0
}
