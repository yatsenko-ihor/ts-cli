package tui

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/creack/pty"
	"github.com/hinshun/vt10x"
	"github.com/ihor/ts-cli/client"
	xterm "golang.org/x/term"
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

type sshOutputMsg struct {
	output string
}

type sshSessionEndedMsg struct {
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
	usernamePrompt  bool           // Whether we're prompting for username
	usernameInput   string         // Current username being typed
	sshUsername     string         // Stored SSH username
	showSSHPanel    bool           // Whether to show the right SSH panel in split mode
	sshSession      *os.File       // PTY file for SSH session
	sshTerminal     vt10x.Terminal // VT100 terminal emulator
	sshActive       bool           // Whether SSH session is active
	sshDevice       string         // Name of device with active SSH
	sshCmd          *exec.Cmd      // SSH command process
	termWidth       int            // Terminal width for SSH session
	termHeight      int            // Terminal height for SSH session
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
		sshSession:      nil,
		sshTerminal:     nil,
		sshActive:       false,
		sshDevice:       "",
		sshCmd:          nil,
		termWidth:       80,
		termHeight:      24,
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		oldWidth := m.width
		oldHeight := m.height
		m.width = msg.Width
		m.height = msg.Height

		// Resize PTY if SSH session is active and size changed
		if m.sshActive && m.sshSession != nil && (oldWidth != m.width || oldHeight != m.height) {
			panelWidth := m.width/2 - 8
			panelHeight := m.height - 6
			if panelWidth < 40 {
				panelWidth = 40
			}
			if panelHeight < 10 {
				panelHeight = 10
			}

			// Update terminal size
			pty.Setsize(m.sshSession, &pty.Winsize{
				Rows: uint16(panelHeight),
				Cols: uint16(panelWidth),
			})

			m.termWidth = panelWidth
			m.termHeight = panelHeight

			// Recreate terminal emulator with new size
			term := vt10x.New(vt10x.WithSize(panelWidth, panelHeight))
			m.sshTerminal = term
		}
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

	case sshOutputMsg:
		// Write output to terminal emulator
		if m.sshActive && m.sshTerminal != nil {
			m.sshTerminal.Write([]byte(msg.output))
			// Continue reading
			return m, m.readSSHOutput()
		}
		return m, nil

	case sshSessionEndedMsg:
		// Clean up SSH session
		if m.sshSession != nil {
			m.sshSession.Close()
			m.sshSession = nil
		}
		if m.sshCmd != nil && m.sshCmd.Process != nil {
			m.sshCmd.Process.Kill()
			m.sshCmd = nil
		}
		m.sshTerminal = nil
		m.sshActive = false
		if msg.err != nil {
			m.sshError = fmt.Errorf("SSH session ended: %v", msg.err)
		}
		return m, nil

	case tea.KeyMsg:
		// Handle SSH session input first (if active)
		if m.sshActive && m.sshSession != nil {
			// Route input to SSH session
			switch msg.String() {
			case "ctrl+c":
				// Send Ctrl+C to SSH session
				m.sshSession.Write([]byte{3})
				return m, nil
			case "ctrl+d":
				// Close SSH session
				return m, func() tea.Msg {
					return sshSessionEndedMsg{err: nil}
				}
			default:
				// Forward all other input to SSH session
				if msg.Type == tea.KeyRunes {
					m.sshSession.Write([]byte(string(msg.Runes)))
				} else if msg.Type == tea.KeyEnter {
					m.sshSession.Write([]byte{'\r'})
				} else if msg.Type == tea.KeyBackspace || msg.Type == tea.KeyDelete {
					m.sshSession.Write([]byte{127})
				}
				return m, nil
			}
		}

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
						return m, tea.Batch(cmd, m.startSSHSession(target))
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
				// Username exists, start embedded SSH session
				return m, m.startSSHSession(target)
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

	// If SSH session is active, show SSH output
	if m.sshActive && m.sshTerminal != nil {
		content.WriteString(sshPanelTitleStyle.Render(fmt.Sprintf("SSH Session: %s", m.sshDevice)))
		content.WriteString("\n\n")

		// Render the terminal buffer - use String() method which preserves formatting
		m.sshTerminal.Lock()
		terminalOutput := m.sshTerminal.String()
		m.sshTerminal.Unlock()
		
		content.WriteString(terminalOutput)

		panelWidth := m.width/2 - 4
		if panelWidth < 40 {
			panelWidth = 40
		}

		panelHeight := m.height - 2
		panelStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#874BFD")).
			Padding(1, 2).
			Width(panelWidth).
			Height(panelHeight)

		return panelStyle.Render(content.String())
	}

	// Regular SSH panel content (when no session is active)
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

// startSSHSession initiates an embedded SSH session in the right panel
func (m *model) startSSHSession(index int) tea.Cmd {
	device := m.filteredDevices[index]

	// Get the primary IP address
	if len(device.Addresses) == 0 {
		return func() tea.Msg {
			return sshSessionEndedMsg{err: fmt.Errorf("device has no IP addresses")}
		}
	}

	address := device.Addresses[0]
	deviceName := device.Name
	if deviceName == "" {
		deviceName = device.Hostname
	}

	// Build SSH target with username
	var sshTarget string
	if m.sshUsername != "" {
		sshTarget = fmt.Sprintf("%s@%s", m.sshUsername, address)
	} else {
		sshTarget = address
	}

	// Calculate terminal size for the right panel
	panelWidth := m.width/2 - 8 // Account for borders and padding
	panelHeight := m.height - 6
	if panelWidth < 40 {
		panelWidth = 40
	}
	if panelHeight < 10 {
		panelHeight = 10
	}

	m.termWidth = panelWidth
	m.termHeight = panelHeight

	// Create VT100 terminal emulator
	term := vt10x.New(vt10x.WithSize(panelWidth, panelHeight))

	// Create SSH command
	sshCmd := exec.Command("ssh", "-t", sshTarget)
	sshCmd.Env = append(os.Environ(),
		fmt.Sprintf("TERM=xterm-256color"),
		fmt.Sprintf("COLUMNS=%d", panelWidth),
		fmt.Sprintf("LINES=%d", panelHeight),
	)

	// Start the command with a PTY
	ptmx, err := pty.StartWithSize(sshCmd, &pty.Winsize{
		Rows: uint16(panelHeight),
		Cols: uint16(panelWidth),
	})
	if err != nil {
		return func() tea.Msg {
			return sshSessionEndedMsg{err: fmt.Errorf("failed to start SSH: %w", err)}
		}
	}

	// Set the PTY to raw mode to disable local echo
	// This prevents double-echoing of characters
	oldState, err := xterm.MakeRaw(int(ptmx.Fd()))
	if err == nil {
		// Store old state if needed for restoration (though we close PTY on exit anyway)
		_ = oldState
	}

	// Set the SSH session state
	m.sshSession = ptmx
	m.sshTerminal = term
	m.sshActive = true
	m.sshDevice = deviceName
	m.sshCmd = sshCmd

	// Return a command that reads PTY output
	return m.readSSHOutput()
}

// Write implements io.Writer for the terminal emulator
func (m *model) Write(p []byte) (n int, err error) {
	// This is called by vt10x when it needs to write to the terminal
	// We don't need to do anything here as we're reading from PTY
	return len(p), nil
}

// readSSHOutput reads from the SSH PTY and sends output messages
func (m *model) readSSHOutput() tea.Cmd {
	return func() tea.Msg {
		if m.sshSession == nil || m.sshTerminal == nil {
			return nil
		}

		// Read a small chunk
		buf := make([]byte, 1024)
		n, err := m.sshSession.Read(buf)
		if err != nil {
			if err == io.EOF {
				return sshSessionEndedMsg{err: nil}
			}
			return sshSessionEndedMsg{err: err}
		}

		if n > 0 {
			// Write directly to terminal for parsing
			m.sshTerminal.Write(buf[:n])
			return sshOutputMsg{output: string(buf[:n])}
		}

		return nil
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
