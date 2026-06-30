package tui

import (
	"sort"
	"strings"
	"time"

	"github.com/ihor/ts-cli/client"
)

// Device utility functions for filtering, sorting, and managing devices

// sortMode defines how devices are sorted
type sortMode int

const (
	sortByName     sortMode = iota // Online first, then alphabetical
	sortByLastSeen                 // Online first, then by last seen (most recent first)
	sortByCreated                  // Online first, then by created date (newest first)
	sortModeCount                  // Total number of sort modes
)

func (s sortMode) String() string {
	switch s {
	case sortByName:
		return "Name"
	case sortByLastSeen:
		return "Last seen"
	case sortByCreated:
		return "Added"
	}
	return "Unknown"
}

// isDeviceOnline checks if a device is considered online based on LastSeen time
func isDeviceOnline(device client.Device) bool {
	// Consider a device online if it was seen within the last 5 minutes
	return time.Since(device.LastSeen) < 5*time.Minute
}

// sortDevices sorts devices based on the given sort mode.
// Online devices always come first regardless of mode.
func sortDevices(devices []client.Device, mode sortMode) {
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

		// Same online status — apply sort mode
		switch mode {
		case sortByLastSeen:
			return devices[i].LastSeen.After(devices[j].LastSeen)
		case sortByCreated:
			return devices[i].Created.After(devices[j].Created)
		default: // sortByName
			nameI := devices[i].Name
			if nameI == "" {
				nameI = devices[i].Hostname
			}
			nameJ := devices[j].Name
			if nameJ == "" {
				nameJ = devices[j].Hostname
			}
			return strings.ToLower(nameI) < strings.ToLower(nameJ)
		}
	})
}

// sortDevicesByStatus is a convenience wrapper using the default name sort
func sortDevicesByStatus(devices []client.Device) {
	sortDevices(devices, sortByName)
}

// getStatusIcon returns the appropriate status icon for a device
func getStatusIcon(device client.Device) string {
	if isDeviceOnline(device) {
		return "🟢"
	}
	return "🔴"
}

// getKeyExpiryIcon returns an expiry indicator icon when a device has key expiry set.
// Returns empty string when expiry is disabled or not configured.
func getKeyExpiryIcon(device client.Device) string {
	if device.KeyExpiryDisabled || device.Expires.IsZero() {
		return ""
	}
	if device.Expires.Before(time.Now()) {
		return "🔒"
	}
	return "🔑"
}

// filterDevices filters the device list based on the search query and profile filter
func (m *model) filterDevices() {
	// Start with all devices
	filtered := m.list.devices

	// Apply profile filter first
	if m.list.selectedProfile != "" {
		profileFiltered := []client.Device{}
		for _, device := range filtered {
			if device.AccountName == m.list.selectedProfile {
				profileFiltered = append(profileFiltered, device)
			}
		}
		filtered = profileFiltered
	}

	// Apply search filter if query exists
	if m.list.searchQuery != "" {
		query := strings.ToLower(m.list.searchQuery)
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

	// Sort devices based on current sort mode
	sortDevices(filtered, m.list.sort)

	m.list.filteredDevices = filtered
	// Reset cursor to top of filtered list
	m.list.cursor = 0
	m.list.viewportTop = 0
}
