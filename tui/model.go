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

type tailscaleUpMsg struct {
	err error
}

type addAccountMsg struct {
	err error
}

type reloadMsg struct {
	devices []client.Device
	err     error
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
	accounts        []client.AccountInfo // Store accounts for reload functionality
	reloading       bool   // Whether we're currently reloading
}

func NewModel(devices []client.Device, version string, sshUsername string, accounts []client.AccountInfo) model {
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
		accounts:        accounts,
		reloading:       false,
		sshUsername:     sshUsername,
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

	case reloadMsg:
		// Handle reload result
		m.reloading = false
		if msg.err != nil {
			m.sshError = fmt.Errorf("failed to reload devices: %w", msg.err)
		} else {
			m.devices = msg.devices
			m.filteredDevices = msg.devices
			// Reset search if active
			if m.searchQuery != "" {
				m.filterDevices()
			}
			// Reset cursor if out of bounds
			if m.cursor >= len(m.filteredDevices) {
				m.cursor = 0
			}
			if m.selected >= len(m.filteredDevices) {
				m.selected = -1
			}
			m.sshError = nil
		}
		return m, nil

	case tea.KeyMsg:
		// Handle username prompt mode
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
				// Username exists, start SSH session
				return m, m.sshToDevice(target)
			}

		case "u":
			// Run tailscale up
			return m, m.runTailscaleUp()

		case "a":
			// Add new account
			return m, m.runAddAccount()

		case "r":
			// Reload devices
			if !m.reloading {
				m.reloading = true
				m.sshError = nil // Clear previous errors
				return m, m.reloadDevices()
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
	if m.reloading {
		title += " - Reloading..."
	} else if m.usernamePrompt {
		title += " - SSH Username: " + m.usernameInput + "_"
	} else if m.searchMode {
		title += " - Search: /" + m.searchQuery + "_"
	} else if m.searchQuery != "" {
		title += fmt.Sprintf(" - Filtered: %d/%d", len(m.filteredDevices), len(m.devices))
	}
	b.WriteString(titleStyle.Render(title))
	b.WriteString("\n")

	// Render device list
	b.WriteString(m.renderDeviceList())

	// Device details if selected
	if m.selected >= 0 && m.selected < len(m.filteredDevices) {
		b.WriteString(m.renderDeviceDetails())
	}

	// Help text
	help := "↑/k up • ↓/j down • / search • enter select • s ssh • c copy • r reload • u tailscale-up • a add-account • q quit"
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

	if err := tmpFile.Chmod(0755); err != nil {
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
