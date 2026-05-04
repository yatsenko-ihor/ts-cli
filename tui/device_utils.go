package tui

import (
	"sort"
	"strings"
	"time"

	"github.com/ihor/ts-cli/client"
)

// Device utility functions for filtering, sorting, and managing devices
// These functions handle device-related operations that don't require full model state

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

// getStatusIcon returns the appropriate status icon for a device
func getStatusIcon(device client.Device) string {
	if isDeviceOnline(device) {
		return "🟢"
	}
	return "🔴"
}

// filterDevices filters the device list based on the search query and profile filter
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
