package tui

import (
	"sort"
	"strings"
	"time"

	"github.com/yatsenko-ihor/ts-cli/client"
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

// isDeviceOnline reports whether the Tailscale API considers a device online.
// connectedToControl is authoritative when true (and the API then omits
// lastSeen, which would otherwise read as "never seen"); otherwise a device
// seen within the last 5 minutes counts as online.
func isDeviceOnline(device client.Device) bool {
	if device.ConnectedToControl {
		return true
	}
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

// filterDevices rebuilds filteredDevices from the profile filter, the search
// query, the hide-offline option and the grouping/sort order, and puts the
// cursor back on the first row.
func (m *model) filterDevices() {
	filtered := m.list.devices

	if m.list.selectedProfile != "" {
		profileFiltered := []client.Device{}
		for _, device := range filtered {
			if device.AccountName == m.list.selectedProfile {
				profileFiltered = append(profileFiltered, device)
			}
		}
		filtered = profileFiltered
	}

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

			for _, addr := range device.Addresses {
				if strings.Contains(strings.ToLower(addr), query) {
					searchFiltered = append(searchFiltered, device)
					break
				}
			}
		}
		filtered = searchFiltered
	}

	// Offline machines are collapsed, except while searching: a search should
	// find a machine whatever its state.
	m.list.hiddenOffline = 0
	if m.set.hideOffline && m.list.searchQuery == "" {
		visible := make([]client.Device, 0, len(filtered))
		for _, d := range filtered {
			if m.isReachable(d) {
				visible = append(visible, d)
			} else {
				m.list.hiddenOffline++
			}
		}
		filtered = visible
	}

	// sortDevices sorts in place; work on a copy so m.list.devices keeps its order.
	filtered = append([]client.Device(nil), filtered...)
	sortDevices(filtered, m.list.sort)
	if m.set.groupByAccount {
		rank := m.accountRank()
		sort.SliceStable(filtered, func(i, j int) bool {
			return rank[filtered[i].AccountName] < rank[filtered[j].AccountName]
		})
	}

	m.list.filteredDevices = filtered
	m.list.cursor = 0
	m.list.viewportTop = 0
}

// accountRank orders accounts as configured (then alphabetically) for grouping.
func (m model) accountRank() map[string]int {
	rank := map[string]int{}
	for i, a := range m.acct.list {
		rank[a.Name] = i
	}
	extra := []string{}
	for _, d := range m.list.devices {
		if _, ok := rank[d.AccountName]; !ok {
			rank[d.AccountName] = -1
			extra = append(extra, d.AccountName)
		}
	}
	sort.Strings(extra)
	for i, n := range extra {
		rank[n] = len(m.acct.list) + i
	}
	return rank
}

// refilterKeepCursor re-applies the filters and keeps the cursor on the same
// machine when it is still listed.
func (m *model) refilterKeepCursor() {
	key := ""
	if d, ok := m.targetDevice(); ok {
		key = machineKey(d)
	}
	m.filterDevices()
	if key != "" {
		for i, d := range m.list.filteredDevices {
			if machineKey(d) == key {
				m.list.cursor = i
				break
			}
		}
	}
	m.ensureListCursorVisible()
}
