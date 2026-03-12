package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ihor/ts-cli/client"
)

// TestIsDeviceOnline tests the device online detection logic
func TestIsDeviceOnline(t *testing.T) {
	tests := []struct {
		name     string
		device   client.Device
		expected bool
	}{
		{
			name: "device seen recently (online)",
			device: client.Device{
				LastSeen: time.Now().Add(-2 * time.Minute),
			},
			expected: true,
		},
		{
			name: "device seen long ago (offline)",
			device: client.Device{
				LastSeen: time.Now().Add(-10 * time.Minute),
			},
			expected: false,
		},
		{
			name: "device seen exactly 5 minutes ago (boundary)",
			device: client.Device{
				LastSeen: time.Now().Add(-5 * time.Minute),
			},
			expected: false,
		},
		{
			name: "device seen 4 minutes ago (online)",
			device: client.Device{
				LastSeen: time.Now().Add(-4 * time.Minute),
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isDeviceOnline(tt.device)
			if result != tt.expected {
				t.Errorf("isDeviceOnline() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestGetStatusIcon tests the status icon selection
func TestGetStatusIcon(t *testing.T) {
	tests := []struct {
		name     string
		device   client.Device
		expected string
	}{
		{
			name: "online device",
			device: client.Device{
				LastSeen: time.Now().Add(-2 * time.Minute),
			},
			expected: "🟢",
		},
		{
			name: "offline device",
			device: client.Device{
				LastSeen: time.Now().Add(-10 * time.Minute),
			},
			expected: "🔴",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getStatusIcon(tt.device)
			if result != tt.expected {
				t.Errorf("getStatusIcon() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestSortDevicesByStatus tests device sorting logic
func TestSortDevicesByStatus(t *testing.T) {
	devices := []client.Device{
		{
			ID:       "device1",
			Hostname: "offline-device",
			LastSeen: time.Now().Add(-10 * time.Minute),
		},
		{
			ID:       "device2",
			Hostname: "online-device",
			LastSeen: time.Now().Add(-2 * time.Minute),
		},
		{
			ID:       "device3",
			Hostname: "another-offline",
			LastSeen: time.Now().Add(-20 * time.Minute),
		},
	}

	sortDevicesByStatus(devices)

	// First device should be online
	if !isDeviceOnline(devices[0]) {
		t.Errorf("First device should be online, but got hostname: %s", devices[0].Hostname)
	}

	// Check that we have the online device first
	if devices[0].ID != "device2" {
		t.Errorf("Expected online device (device2) to be first, got %s", devices[0].ID)
	}
}

// TestFilterDevices tests the device filtering logic
func TestFilterDevices(t *testing.T) {
	devices := []client.Device{
		{
			ID:          "device1",
			Hostname:    "laptop",
			Name:        "laptop.example.com",
			OS:          "macOS",
			Addresses:   []string{"100.64.0.1"},
			AccountName: "personal",
		},
		{
			ID:          "device2",
			Hostname:    "server",
			Name:        "server.example.com",
			OS:          "linux",
			Addresses:   []string{"100.64.0.2"},
			AccountName: "work",
		},
		{
			ID:          "device3",
			Hostname:    "desktop",
			Name:        "desktop.example.com",
			OS:          "linux",
			Addresses:   []string{"100.64.0.3"},
			AccountName: "personal",
		},
	}

	tests := []struct {
		name            string
		searchQuery     string
		selectedProfile string
		expectedCount   int
		expectedIDs     []string
	}{
		{
			name:            "no filter",
			searchQuery:     "",
			selectedProfile: "",
			expectedCount:   3,
			expectedIDs:     []string{"device1", "device2", "device3"},
		},
		{
			name:            "filter by hostname search",
			searchQuery:     "laptop",
			selectedProfile: "",
			expectedCount:   1,
			expectedIDs:     []string{"device1"},
		},
		{
			name:            "filter by OS",
			searchQuery:     "linux",
			selectedProfile: "",
			expectedCount:   2,
			expectedIDs:     []string{"device2", "device3"},
		},
		{
			name:            "filter by profile",
			searchQuery:     "",
			selectedProfile: "personal",
			expectedCount:   2,
			expectedIDs:     []string{"device1", "device3"},
		},
		{
			name:            "filter by profile and search",
			searchQuery:     "desktop",
			selectedProfile: "personal",
			expectedCount:   1,
			expectedIDs:     []string{"device3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := model{
				devices:         devices,
				searchQuery:     tt.searchQuery,
				selectedProfile: tt.selectedProfile,
			}
			m.filterDevices()

			if len(m.filteredDevices) != tt.expectedCount {
				t.Errorf("Expected %d filtered devices, got %d", tt.expectedCount, len(m.filteredDevices))
			}

			for i, expectedID := range tt.expectedIDs {
				if i < len(m.filteredDevices) && m.filteredDevices[i].ID != expectedID {
					t.Errorf("Expected device ID %s at position %d, got %s", expectedID, i, m.filteredDevices[i].ID)
				}
			}
		})
	}
}

func TestGetHelpText(t *testing.T) {
	tests := []struct {
		name           string
		m              model
		mustContain    string
		mustNotContain string
	}{
		{
			name: "normal mode includes search and ssh controls",
			m: model{
				showHistoryPanel: false,
			},
			mustContain: "s ssh",
		},
		{
			name: "history focus includes delete and reverse tab",
			m: model{
				showHistoryPanel: true,
				activeFocus:      focusHistory,
			},
			mustContain: "tab/shift+tab switch",
		},
		{
			name: "search mode shows dedicated prompt",
			m: model{
				searchMode: true,
			},
			mustContain:    "Type to search",
			mustNotContain: "s ssh",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			help := tt.m.getHelpText()
			if tt.mustContain != "" && !strings.Contains(help, tt.mustContain) {
				t.Fatalf("expected help text to contain %q, got %q", tt.mustContain, help)
			}
			if tt.mustNotContain != "" && strings.Contains(help, tt.mustNotContain) {
				t.Fatalf("expected help text to not contain %q, got %q", tt.mustNotContain, help)
			}
		})
	}
}

func TestHandleNormalModeShiftTabReverseNavigation(t *testing.T) {
	m := model{
		showHistoryPanel: false,
		activeFocus:      focusList,
	}

	updated, _ := m.handleNormalMode(tea.KeyMsg{Type: tea.KeyShiftTab})
	m2 := updated.(model)

	if !m2.showHistoryPanel {
		t.Fatalf("expected history panel to open on shift+tab")
	}
	if m2.activeFocus != focusOutput {
		t.Fatalf("expected focusOutput on initial shift+tab, got %v", m2.activeFocus)
	}

	updated, _ = m2.handleNormalMode(tea.KeyMsg{Type: tea.KeyShiftTab})
	m3 := updated.(model)
	if m3.activeFocus != focusHistory {
		t.Fatalf("expected reverse cycle to focusHistory, got %v", m3.activeFocus)
	}

	updated, _ = m3.handleNormalMode(tea.KeyMsg{Type: tea.KeyShiftTab})
	m4 := updated.(model)
	if m4.activeFocus != focusList {
		t.Fatalf("expected reverse cycle to focusList, got %v", m4.activeFocus)
	}
}

func TestHandleHistoryNavigationShiftTabToList(t *testing.T) {
	m := model{
		showHistoryPanel: true,
		activeFocus:      focusHistory,
		devices:          []client.Device{{ID: "1", Hostname: "host1", LastSeen: time.Now()}},
		filteredDevices:  []client.Device{{ID: "1", Hostname: "host1", LastSeen: time.Now()}},
		selected:         0,
	}

	updated, _ := m.handleHistoryNavigation(tea.KeyMsg{Type: tea.KeyShiftTab})
	m2 := updated.(model)

	if m2.activeFocus != focusList {
		t.Fatalf("expected shift+tab in history panel to move focus to list, got %v", m2.activeFocus)
	}
}
