package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/yatsenko-ihor/ts-cli/client"
)

// Aligned device rows: the name and address columns are as wide as the
// longest entry in the list and the icons live in fixed-width slots at the
// right, so they line up one below the other whatever the name lengths.

// iconSlotWidth is the cell width reserved for one emoji icon.
const iconSlotWidth = 2

// listCols are the column widths of the device list.
type listCols struct {
	name int
	addr int
}

func deviceAddress(d client.Device) string {
	if len(d.Addresses) > 0 {
		return d.Addresses[0]
	}
	return "N/A"
}

// padRight pads s with spaces to w terminal cells.
func padRight(s string, w int) string {
	if gap := w - lipgloss.Width(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}

// listColumns computes the column widths for the current list. When the
// longest name does not fit the panel, names are cut to what is left.
func (m model) listColumns(hasMarks bool) listCols {
	var c listCols
	for _, d := range m.list.filteredDevices {
		if w := lipgloss.Width(displayName(d)); w > c.name {
			c.name = w
		}
		if w := lipgloss.Width(deviceAddress(d)); w > c.addr {
			c.addr = w
		}
	}
	markW := 0
	if hasMarks {
		markW = 2
	}
	// cursor(2) + style padding(2) + mark + status icon + gaps + two icon slots
	fixed := 2 + 2 + markW + iconSlotWidth + 1 + 1 + c.addr + 1 + iconSlotWidth + 1 + iconSlotWidth
	if avail := getPanelInnerWidth(m.getMachineListWidth()) - fixed; m.width > 0 && avail >= 12 && c.name > avail {
		c.name = avail
	}
	return c
}

// deviceRow renders everything after the cursor/mark prefix:
//
//	<status icon> <name> <address> <key-expiry slot> <saved-password slot>
func (m model) deviceRow(d client.Device, c listCols) string {
	name := displayName(d)
	if lipgloss.Width(name) > c.name {
		name = truncateForWidth(name, c.name)
	}
	slot := func(icon string) string { return padRight(icon, iconSlotWidth) }
	pwd := ""
	if m.hasSavedPassword(d) {
		pwd = "🔐"
	}
	return m.statusIcon(d) + " " + padRight(name, c.name) + " " + padRight(deviceAddress(d), c.addr) +
		" " + slot(getKeyExpiryIcon(d)) + " " + slot(pwd)
}
