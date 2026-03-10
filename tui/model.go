package tui

import (
	"fmt"
	"os/exec"
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

type model struct {
	devices     []client.Device
	cursor      int
	selected    int
	err         error
	width       int
	height      int
	sshError    error
	viewportTop int // First visible item in the list
}

func NewModel(devices []client.Device) model {
	return model{
		devices:     devices,
		cursor:      0,
		selected:    -1,
		viewportTop: 0,
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

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				// Scroll up if cursor goes above viewport
				if m.cursor < m.viewportTop {
					m.viewportTop = m.cursor
				}
			}

		case "down", "j":
			if m.cursor < len(m.devices)-1 {
				m.cursor++
				// Scroll down if cursor goes below viewport
				maxVisible := m.getMaxVisibleItems()
				if m.cursor >= m.viewportTop+maxVisible {
					m.viewportTop = m.cursor - maxVisible + 1
				}
			}

		case "enter", " ":
			m.selected = m.cursor

		case "s":
			// SSH to currently selected or cursor device
			target := m.selected
			if target < 0 {
				target = m.cursor
			}
			if target >= 0 && target < len(m.devices) {
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
	b.WriteString(titleStyle.Render("Tailscale Devices"))
	b.WriteString("\n")

	// Build device list content
	var listContent strings.Builder
	maxVisible := m.getMaxVisibleItems()

	// Calculate visible range
	visibleStart := m.viewportTop
	visibleEnd := m.viewportTop + maxVisible
	if visibleEnd > len(m.devices) {
		visibleEnd = len(m.devices)
	}

	// Show scroll indicator at top if needed
	if m.viewportTop > 0 {
		listContent.WriteString(normalStyle.Render("  ↑ more above ↑"))
		listContent.WriteString("\n")
	}

	// Render visible devices
	for i := visibleStart; i < visibleEnd; i++ {
		device := m.devices[i]
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
	if visibleEnd < len(m.devices) {
		listContent.WriteString(normalStyle.Render("  ↓ more below ↓"))
	}

	// Render the list in a frame
	b.WriteString(listStyle.Render(listContent.String()))

	// Device details if selected
	if m.selected >= 0 && m.selected < len(m.devices) {
		device := m.devices[m.selected]
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

	// Help text
	help := "↑/k up • ↓/j down • enter select • s ssh • q quit"
	b.WriteString(helpStyle.Render(help))

	// Show SSH error if any
	if m.sshError != nil {
		b.WriteString("\n")
		b.WriteString(errorStyle.Render(fmt.Sprintf("SSH Error: %v", m.sshError)))
	}

	return b.String()
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
	device := m.devices[index]

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

	// Use tea.ExecProcess to suspend TUI and run SSH
	sshCmd := exec.Command("ssh", address)
	return tea.ExecProcess(sshCmd, func(err error) tea.Msg {
		if err != nil {
			return sshMsg{err: err}
		}
		return sshMsg{}
	})
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
