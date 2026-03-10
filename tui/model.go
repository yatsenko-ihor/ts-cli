package tui

import (
	"fmt"
	"strings"

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

	detailStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#7D56F4")).
			Padding(1).
			MarginTop(1)
)

type model struct {
	devices  []client.Device
	cursor   int
	selected int
	err      error
	width    int
	height   int
}

func NewModel(devices []client.Device) model {
	return model{
		devices:  devices,
		cursor:   0,
		selected: -1,
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

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}

		case "down", "j":
			if m.cursor < len(m.devices)-1 {
				m.cursor++
			}

		case "enter", " ":
			m.selected = m.cursor
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

	// Device list
	for i, device := range m.devices {
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

		line := fmt.Sprintf("%s%-30s %s", cursor, name, address)
		b.WriteString(style.Render(line))
		b.WriteString("\n")
	}

	// Device details if selected
	if m.selected >= 0 && m.selected < len(m.devices) {
		device := m.devices[m.selected]
		name := device.Name
		if name == "" {
			name = device.Hostname
		}

		details := fmt.Sprintf(
			"Selected Device\n\n"+
				"Name:       %s\n"+
				"Hostname:   %s\n"+
				"OS:         %s\n"+
				"Authorized: %t\n"+
				"Address:    %v\n"+
				"ID:         %s",
			name,
			device.Hostname,
			device.OS,
			device.Authorized,
			strings.Join(device.Addresses, ", "),
			device.ID,
		)

		b.WriteString(detailStyle.Render(details))
		b.WriteString("\n")
	}

	// Help text
	help := "↑/k up • ↓/j down • enter select • q quit"
	b.WriteString(helpStyle.Render(help))

	return b.String()
}
