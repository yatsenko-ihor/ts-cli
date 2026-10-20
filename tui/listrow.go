package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/yatsenko-ihor/ts-cli/client"
)

// Rendering of the machine list: rows (devices, account headers, the
// "offline hidden" note), aligned columns, short names, coloured status.

var (
	colorOnline  lipgloss.Color // set by applyTheme
	colorOffline lipgloss.Color
	colorWarn    lipgloss.Color
	colorDim     lipgloss.Color
	colorSel     lipgloss.Color
)

// ─── Names ────────────────────────────────────────────────────────────────────

// shortHost strips the MagicDNS tailnet suffix: box-043.tail813b1c.ts.net → box-043.
func shortHost(name string) string {
	n := strings.TrimSuffix(name, ".")
	if strings.HasSuffix(n, ".ts.net") {
		if parts := strings.Split(n, "."); len(parts) >= 3 {
			return parts[0]
		}
	}
	return n
}

// tailnetLabel returns the tailnet part of a MagicDNS name (tail813b1c), or "".
func tailnetLabel(name string) string {
	n := strings.TrimSuffix(name, ".")
	if strings.HasSuffix(n, ".ts.net") {
		if parts := strings.Split(n, "."); len(parts) >= 3 {
			return parts[1]
		}
	}
	return ""
}

// shortNames maps machineKey → the name shown in the list. With short names
// on, the tailnet suffix is dropped; machines whose short names collide (the
// same host in two tailnets) keep a "·tailnet" tag.
func (m model) shortNames() map[string]string {
	out := make(map[string]string, len(m.list.filteredDevices))
	if !m.set.shortNames {
		for _, d := range m.list.filteredDevices {
			out[machineKey(d)] = displayName(d)
		}
		return out
	}
	group := func(d client.Device) string {
		if m.showGroupHeaders() {
			return d.AccountName
		}
		return ""
	}
	count := map[string]int{}
	for _, d := range m.list.filteredDevices {
		count[group(d)+"|"+shortHost(displayName(d))]++
	}
	for _, d := range m.list.filteredDevices {
		s := shortHost(displayName(d))
		if count[group(d)+"|"+s] > 1 {
			if lab := tailnetLabel(displayName(d)); lab != "" {
				s += " ·" + lab
			}
		}
		out[machineKey(d)] = s
	}
	return out
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

// ─── Status ───────────────────────────────────────────────────────────────────

// statusIcon is the plain status marker used in headers and titles:
// 🟢/🔴/⭕️ in emoji mode, ●/○ otherwise. ⭕️/○ means "no reply to the last ping".
func (m model) statusIcon(d client.Device) string {
	failed := m.ping[machineKey(d)] == pingFail
	if m.set.emojiIcons {
		switch {
		case failed:
			return pingFailedIcon
		case m.isReachable(d):
			return "🟢"
		}
		return "🔴"
	}
	if failed {
		return "○"
	}
	return "●"
}

// styledStatus is statusIcon coloured for the list (symbols only).
func (m model) styledStatus(d client.Device) string {
	icon := m.statusIcon(d)
	if m.set.emojiIcons {
		return icon
	}
	color := colorOffline
	switch {
	case m.ping[machineKey(d)] == pingFail:
		color = colorWarn
	case m.isReachable(d):
		color = colorOnline
	}
	return lipgloss.NewStyle().Foreground(color).Render(icon)
}

// statusWidth is the cell width of the status marker.
func (m model) statusWidth() int {
	if m.set.emojiIcons {
		return 2
	}
	return 1
}

// flagsWidth is the width of the flags block (leading separator included).
func (m model) flagsWidth() int {
	if m.set.emojiIcons {
		return 1 + 2 + 1 + 2 // " <expiry> <password>"
	}
	return 1 + 2 + 1 + 3 // " pw exp"
}

// styledFlags renders the fixed-width flags after the address: a saved
// password and an expired device key, each in its own slot.
func (m model) styledFlags(d client.Device) string {
	if m.set.emojiIcons {
		pwd := ""
		if m.hasSavedPassword(d) {
			pwd = "🔐"
		}
		return " " + padRight(getKeyExpiryIcon(d), 2) + " " + padRight(pwd, 2)
	}
	pw := "  "
	if m.hasSavedPassword(d) {
		pw = lipgloss.NewStyle().Foreground(colorOnline).Render("pw")
	}
	exp := "   "
	if getKeyExpiryIcon(d) == "🔒" { // key already expired
		exp = lipgloss.NewStyle().Foreground(colorOffline).Render("exp")
	}
	return " " + pw + " " + exp
}

// statusCounts counts reachable and unreachable machines of the current
// profile, including hidden ones.
func (m model) statusCounts() (online, offline int) {
	for _, d := range m.list.devices {
		if m.list.selectedProfile != "" && d.AccountName != m.list.selectedProfile {
			continue
		}
		if m.isReachable(d) {
			online++
		} else {
			offline++
		}
	}
	return
}

// ─── Columns and rows ─────────────────────────────────────────────────────────

// listCols are the column widths of the device list.
type listCols struct {
	name  int
	addr  int
	names map[string]string
}

// listColumns computes the column widths for the current list. When the
// longest name does not fit the panel, names are cut to what is left.
func (m model) listColumns(hasMarks bool) listCols {
	c := listCols{names: m.shortNames()}
	for _, d := range m.list.filteredDevices {
		if w := lipgloss.Width(c.names[machineKey(d)]); w > c.name {
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
	// indent(2) + cursor(2) + mark + status + gap + [name] + gap + addr + flags
	fixed := 2 + 2 + markW + m.statusWidth() + 1 + 1 + c.addr + m.flagsWidth()
	if avail := getPanelInnerWidth(m.getMachineListWidth()) - fixed; m.width > 0 && avail >= 12 && c.name > avail {
		c.name = avail
	}
	return c
}

// renderListDevice renders the row of filteredDevices[i]:
//
//	▶ ✔ ● box-043   100.76.50.39  pw exp
func (m model) renderListDevice(i int, c listCols, hasMarks bool) string {
	d := m.list.filteredDevices[i]
	selected := m.list.cursor == i

	base := lipgloss.NewStyle()
	dim := lipgloss.NewStyle().Foreground(colorDim)
	if selected {
		base = base.Foreground(colorSel).Bold(true)
		dim = base
	}

	name := c.names[machineKey(d)]
	if lipgloss.Width(name) > c.name {
		name = truncateForWidth(name, c.name)
	}

	var b strings.Builder
	b.WriteString("  ") // aligns with the 2-cell indent of the other panels
	cursor := "  "
	if selected {
		cursor = "▶ "
	}
	b.WriteString(base.Render(cursor))
	if hasMarks {
		mark := "  "
		if m.list.marked[machineKey(d)] {
			mark = "✔ "
		}
		b.WriteString(base.Render(mark))
	}
	b.WriteString(m.styledStatus(d))
	b.WriteString(" ")
	b.WriteString(base.Render(padRight(name, c.name)))
	b.WriteString(" ")
	b.WriteString(dim.Render(padRight(deviceAddress(d), c.addr)))
	b.WriteString(m.styledFlags(d))
	return b.String()
}

// ─── Rows: devices, group headers, hidden note ────────────────────────────────

type listRowKind int

const (
	rowDevice listRowKind = iota
	rowHeader
	rowNote
)

// listRow is one display line of the list.
type listRow struct {
	kind listRowKind
	idx  int    // index into filteredDevices for rowDevice
	text string // header / note text
}

// groupCount returns how many different accounts the visible machines belong to.
func (m model) groupCount() int {
	seen := map[string]bool{}
	for _, d := range m.list.filteredDevices {
		seen[d.AccountName] = true
	}
	return len(seen)
}

// showGroupHeaders reports whether account section headers are shown.
func (m model) showGroupHeaders() bool { return m.set.groupByAccount && m.groupCount() > 1 }

// accountCounts returns online/offline machine counts per account.
func (m model) accountCounts() map[string][2]int {
	out := map[string][2]int{}
	for _, d := range m.list.devices {
		c := out[d.AccountName]
		if m.isReachable(d) {
			c[0]++
		} else {
			c[1]++
		}
		out[d.AccountName] = c
	}
	return out
}

// listRows builds the display rows of the list.
func (m model) listRows() []listRow {
	var rows []listRow
	headers := m.showGroupHeaders()
	counts := m.accountCounts()
	last := "\x00"
	for i, d := range m.list.filteredDevices {
		if headers && d.AccountName != last {
			last = d.AccountName
			c := counts[d.AccountName]
			title := d.AccountName
			if title == "" {
				title = "(no account)"
			}
			rows = append(rows, listRow{kind: rowHeader, text: title + "  ·  " + countText(c[0], c[1])})
		}
		rows = append(rows, listRow{kind: rowDevice, idx: i})
	}
	if n := m.list.hiddenOffline; n > 0 {
		rows = append(rows, listRow{kind: rowNote, text: fmt.Sprintf("▸ %d offline hidden · H shows them", n)})
	}
	return rows
}

func countText(online, offline int) string {
	return fmt.Sprintf("%d online, %d offline", online, offline)
}

// cursorRow returns the row index of the cursor machine (0 when absent).
func cursorRow(rows []listRow, cursor int) int {
	for i, r := range rows {
		if r.kind == rowDevice && r.idx == cursor {
			return i
		}
	}
	return 0
}

// windowTop returns the first visible row so the cursor row stays inside a
// window of capacity rows; the header right above the cursor is kept in view.
func windowTop(rows []listRow, cursorRow, top, capacity int) int {
	if len(rows) == 0 || capacity < 1 {
		return 0
	}
	if cursorRow < top {
		top = cursorRow
		if top > 0 && rows[top-1].kind == rowHeader {
			top--
		}
	} else if cursorRow >= top+capacity {
		top = cursorRow - capacity + 1
	}
	if top < 0 {
		top = 0
	}
	return top
}

// listCapacity is the number of rows the list panel can show.
func (m model) listCapacity() int {
	if !m.run.visible {
		return m.getMaxVisibleItems()
	}
	_, panelHeight := m.getHistoryPanelSize()
	content := panelHeight*2 + panelBorderWidth - panelVerticalPadding
	header := 2 // "Search in" + spacer
	if m.input.mode == inputSearch || m.list.searchQuery != "" {
		header++
	}
	c := content - header - 2 // top and bottom scroll indicators
	if c < 1 {
		c = 1
	}
	return c
}

// ensureListCursorVisible scrolls the list so the cursor row is on screen
// (also needed once the real terminal height is known).
func (m *model) ensureListCursorVisible() {
	rows := m.listRows()
	m.list.viewportTop = windowTop(rows, cursorRow(rows, m.list.cursor), m.list.viewportTop, m.listCapacity())
}
