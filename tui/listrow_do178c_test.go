package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/yatsenko-ihor/ts-cli/client"
)

// rowColors makes the shared colours distinguishable and forces true-colour
// output, so the colour a marker was drawn in can be asserted. It returns the
// escape sequence prefix of each colour.
type rowColorSeq struct{ online, offline, warn, dim, sel string }

func rowColors(t *testing.T) rowColorSeq {
	t.Helper()
	prevProfile := lipgloss.ColorProfile()
	po, pf, pw, pd, ps := colorOnline, colorOffline, colorWarn, colorDim, colorSel
	// Profile 0 is true colour; the zero value is built without importing termenv.
	lipgloss.SetColorProfile(prevProfile * 0)
	colorOnline, colorOffline, colorWarn, colorDim, colorSel = "#00ff00", "#ff0000", "#ffff00", "#808080", "#0000ff"
	t.Cleanup(func() {
		lipgloss.SetColorProfile(prevProfile)
		colorOnline, colorOffline, colorWarn, colorDim, colorSel = po, pf, pw, pd, ps
	})
	return rowColorSeq{
		online:  "\x1b[38;2;0;255;0m",
		offline: "\x1b[38;2;255;0;0m",
		warn:    "\x1b[38;2;255;255;0m",
		dim:     "\x1b[38;2;128;128;128m",
		sel:     "\x1b[38;2;0;0;255m",
	}
}

// Verifies: REQ-ROW-001, REQ-ROW-002
func TestDO178CShortHostAndTailnetLabel(t *testing.T) {
	cases := []struct{ in, host, label string }{
		{"box-043.tail813b1c.ts.net", "box-043", "tail813b1c"},
		{"box-043.tail813b1c.ts.net.", "box-043", "tail813b1c"},
		{"box.ts.net", "box", "ts"},
		{"a.b.c.tail1.ts.net", "a", "b"},
		{"SkopasLinux", "SkopasLinux", ""},
		{"a.example.com", "a.example.com", ""},
		{"a.example.com.", "a.example.com", ""},
		{"host.ts.network", "host.ts.network", ""},
		{"", "", ""},
		{".", "", ""},
		{"Ünïcode.tail1.ts.net", "Ünïcode", "tail1"},
		{".ts.net", "", "ts"}, // derived: not a real name
	}
	for _, c := range cases {
		if got := shortHost(c.in); got != c.host {
			t.Errorf("shortHost(%q) = %q, want %q", c.in, got, c.host)
		}
		if got := tailnetLabel(c.in); got != c.label {
			t.Errorf("tailnetLabel(%q) = %q, want %q", c.in, got, c.label)
		}
	}
}

func dev(id, name, host, account string) client.Device {
	return client.Device{ID: id, Name: name, Hostname: host, AccountName: account, LastSeen: time.Now()}
}

// Verifies: REQ-ROW-003, REQ-ROW-004, REQ-ROW-005
func TestDO178CShortNames(t *testing.T) {
	devs := []client.Device{
		dev("a1", "box.tail1.ts.net", "box", "home"),
		dev("b1", "box.tail2.ts.net", "box", "home"),
		dev("c1", "solo.tail1.ts.net", "solo", "home"),
		dev("d1", "x.example.com", "x", "home"),
		dev("e1", "x.example.com", "x", "home"),
		dev("f1", "", "host.tail3.ts.net.", "home"),
		dev("g1", "box.tail9.ts.net", "box", "work"),
		dev("", "", "", "work"),
	}
	m := model{list: deviceList{filteredDevices: devs}}

	// short names off: full display names
	m.set = uiSettings{shortNames: false}
	full := m.shortNames()
	if full["a1"] != "box.tail1.ts.net" || full["f1"] != "host.tail3.ts.net." || full[""] != "" || len(full) != 8 {
		t.Fatalf("full names: %v", full)
	}
	if got := (model{}).shortNames(); len(got) != 0 {
		t.Fatalf("empty list: %v", got)
	}
	if got := (model{set: uiSettings{shortNames: true}}).shortNames(); len(got) != 0 {
		t.Fatalf("empty list, short: %v", got)
	}

	// short names on, grouping off: "box" collides over the whole list (3 machines)
	m.set = uiSettings{shortNames: true}
	got := m.shortNames()
	want := map[string]string{
		"a1": "box ·tail1", "b1": "box ·tail2", "g1": "box ·tail9", // collision with label
		"c1": "solo",                                 // unique: no tag
		"d1": "x.example.com", "e1": "x.example.com", // collision without label: no tag
		"f1": "host", // trailing dot stripped
		"":   "",
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("ungrouped %q: %q want %q", k, got[k], w)
		}
	}

	// grouping on with two accounts: "box" in work is unique within its account
	m.set = uiSettings{shortNames: true, groupByAccount: true}
	got = m.shortNames()
	if got["a1"] != "box ·tail1" || got["b1"] != "box ·tail2" || got["g1"] != "box" {
		t.Errorf("grouped: %v", got)
	}

	// grouping on but one account only: headers are not shown, collisions are global
	one := []client.Device{dev("a1", "box.tail1.ts.net", "", "home"), dev("b1", "box.tail2.ts.net", "", "home")}
	m = model{set: uiSettings{shortNames: true, groupByAccount: true}, list: deviceList{filteredDevices: one}}
	if got := m.shortNames(); got["a1"] != "box ·tail1" {
		t.Errorf("single account: %v", got)
	}
}

// Verifies: REQ-ROW-006, REQ-ROW-007
func TestDO178CAddressAndPadRight(t *testing.T) {
	if deviceAddress(client.Device{Addresses: []string{"100.1.1.1", "fd7a::1"}}) != "100.1.1.1" ||
		deviceAddress(client.Device{Addresses: []string{}}) != "N/A" || deviceAddress(client.Device{}) != "N/A" {
		t.Error("deviceAddress")
	}
	cases := []struct {
		in   string
		w    int
		want string
	}{
		{"ab", 5, "ab   "},
		{"ab", 2, "ab"},
		{"abcd", 2, "abcd"},
		{"", 3, "   "},
		{"ab", 0, "ab"},
		{"ab", -4, "ab"},
		{"日本", 6, "日本  "}, // two wide characters take 4 cells
		{"日本", 4, "日本"},
		{"é", 3, "é  "},
	}
	for _, c := range cases {
		if got := padRight(c.in, c.w); got != c.want {
			t.Errorf("padRight(%q,%d) = %q want %q", c.in, c.w, got, c.want)
		}
	}
}

// Verifies: REQ-ROW-008, REQ-ROW-009, REQ-ROW-010
func TestDO178CStatusMarkers(t *testing.T) {
	c := rowColors(t)
	online := client.Device{ID: "on", ConnectedToControl: true}
	offline := client.Device{ID: "off"}
	failedOnline := client.Device{ID: "fo", ConnectedToControl: true}
	pingedOK := client.Device{ID: "po"}
	m := model{ping: map[string]pingState{"fo": pingFail, "po": pingOK}}

	m.set.emojiIcons = true
	for d, want := range map[string]string{"on": "🟢", "off": "🔴", "fo": pingFailedIcon, "po": "🟢"} {
		dd := map[string]client.Device{"on": online, "off": offline, "fo": failedOnline, "po": pingedOK}[d]
		if got := m.statusIcon(dd); got != want {
			t.Errorf("emoji %s: %q want %q", d, got, want)
		}
		if got := m.styledStatus(dd); got != want {
			t.Errorf("emoji styled %s: %q want %q", d, got, want)
		}
	}
	if m.statusWidth() != 2 || m.flagsWidth() != 6 {
		t.Error("emoji widths")
	}

	m.set.emojiIcons = false
	for _, tc := range []struct {
		d     client.Device
		icon  string
		color string
	}{
		{online, "●", c.online}, {offline, "●", c.offline}, {failedOnline, "○", c.warn}, {pingedOK, "●", c.online},
	} {
		if got := m.statusIcon(tc.d); got != tc.icon {
			t.Errorf("%s icon %q want %q", tc.d.ID, got, tc.icon)
		}
		got := m.styledStatus(tc.d)
		if !strings.Contains(got, tc.color) || !strings.Contains(got, tc.icon) {
			t.Errorf("%s styled %q want colour %q", tc.d.ID, got, tc.color)
		}
	}
	if m.statusWidth() != 1 || m.flagsWidth() != 7 {
		t.Error("symbol widths")
	}
}

// Verifies: REQ-ROW-010, REQ-ROW-011, REQ-ROW-012
func TestDO178CStyledFlags(t *testing.T) {
	c := rowColors(t)
	now := time.Now()
	fine := client.Device{ID: "fine", Expires: now.Add(90 * 24 * time.Hour)}
	none := client.Device{ID: "none"}
	disabled := client.Device{ID: "dis", KeyExpiryDisabled: true, Expires: now.Add(-time.Hour)}
	expired := client.Device{ID: "exp", Expires: now.Add(-time.Hour)}
	soon := client.Device{ID: "soon", Expires: now.Add(24 * time.Hour)}
	m := model{ssh: ssh{machineCreds: map[string]machineCredential{
		"fine": {passwordEncrypted: "x"}, "exp": {passwordEncrypted: "x"}, "soon": {}, // soon: credential without password
	}}}

	// symbol mode
	for _, d := range []client.Device{fine, none, disabled, expired, soon} {
		if w := lipgloss.Width(m.styledFlags(d)); w != m.flagsWidth() {
			t.Errorf("symbol %s: width %d want %d", d.ID, w, m.flagsWidth())
		}
	}
	if s := m.styledFlags(fine); !strings.Contains(s, c.online+"pw") || strings.Contains(s, "exp") {
		t.Errorf("fine: %q", s)
	}
	for _, d := range []client.Device{none, disabled} {
		if s := m.styledFlags(d); strings.TrimSpace(s) != "" {
			t.Errorf("%s: want blanks, got %q", d.ID, s)
		}
	}
	if s := m.styledFlags(expired); !strings.Contains(s, c.offline+"exp") || !strings.Contains(s, c.online+"pw") {
		t.Errorf("expired with password: %q", s)
	}
	if s := m.styledFlags(soon); !strings.Contains(s, c.warn+"exp") || strings.Contains(s, "pw") {
		t.Errorf("soon without password: %q", s)
	}

	// emoji mode
	m.set.emojiIcons = true
	for _, d := range []client.Device{fine, none, disabled, expired, soon} {
		if w := lipgloss.Width(m.styledFlags(d)); w != m.flagsWidth() {
			t.Errorf("emoji %s: width %d want %d", d.ID, w, m.flagsWidth())
		}
	}
	if s := m.styledFlags(fine); !strings.Contains(s, "🔑") || !strings.Contains(s, "🔐") {
		t.Errorf("fine emoji: %q", s)
	}
	if s := m.styledFlags(expired); !strings.Contains(s, "🔒") || !strings.Contains(s, "🔐") {
		t.Errorf("expired emoji: %q", s)
	}
	if s := m.styledFlags(soon); !strings.Contains(s, "⏳") || strings.Contains(s, "🔑") || strings.Contains(s, "🔐") {
		t.Errorf("soon emoji: %q", s)
	}
	for _, d := range []client.Device{none, disabled} {
		if s := m.styledFlags(d); strings.TrimSpace(s) != "" {
			t.Errorf("%s emoji: want blanks, got %q", d.ID, s)
		}
	}
}

// Verifies: REQ-ROW-013
func TestDO178CStatusCounts(t *testing.T) {
	devs := []client.Device{
		{ID: "1", AccountName: "home", ConnectedToControl: true},
		{ID: "2", AccountName: "home"},
		{ID: "3", AccountName: "work", ConnectedToControl: true},
		{ID: "4", AccountName: "work", ConnectedToControl: true},
		{ID: "5", AccountName: "work"},
	}
	m := model{
		acct: accounts{list: []client.AccountInfo{{Name: "work", Hidden: true}}},
		list: deviceList{devices: devs},
		ping: map[string]pingState{"4": pingFail, "2": pingOK},
	}
	if on, off := m.statusCounts(); on != 3 || off != 2 {
		t.Errorf("all: %d/%d", on, off)
	}
	m.list.selectedProfile = "work" // hidden account: still counted
	if on, off := m.statusCounts(); on != 1 || off != 2 {
		t.Errorf("work: %d/%d", on, off)
	}
	m.list.selectedProfile = "nobody"
	if on, off := m.statusCounts(); on != 0 || off != 0 {
		t.Errorf("nobody: %d/%d", on, off)
	}
	if on, off := (model{}).statusCounts(); on != 0 || off != 0 {
		t.Error("empty")
	}
}

// Verifies: REQ-ROW-014
func TestDO178CListColumnsWidths(t *testing.T) {
	m := model{}
	if c := m.listColumns(false); c.name != 0 || c.addr != 0 || len(c.names) != 0 {
		t.Errorf("empty: %+v", c)
	}
	m.list.filteredDevices = []client.Device{
		{ID: "1", Name: "日本語", Addresses: []string{"100.1.1.1"}},
		{ID: "2", Name: "ab", Addresses: []string{"fd7a:115c:a1e0::1"}},
		{ID: "3", Name: "x"},
	}
	c := m.listColumns(true)
	if c.name != 6 || c.addr != len("fd7a:115c:a1e0::1") {
		t.Errorf("widths %+v", c)
	}
	m.list.filteredDevices = []client.Device{{ID: "3", Name: "x"}}
	if c := m.listColumns(false); c.addr != 3 || c.name != 1 {
		t.Errorf("N/A width: %+v", c)
	}
}

// Verifies: REQ-ROW-015
func TestDO178CListColumnsTruncation(t *testing.T) {
	mk := func(width int, name, addr string) model {
		return model{width: width, list: deviceList{filteredDevices: []client.Device{
			{ID: "1", Name: name, Addresses: []string{addr}},
		}}}
	}
	rep := func(n int) string { return strings.Repeat("n", n) }
	// width 60 -> list width 56 -> inner 52; fixed = 2+2+mark+1+1+1+addr+7
	cases := []struct {
		label   string
		m       model
		marks   bool
		wantCol int
	}{
		{"K1 width unknown", mk(0, rep(50), "100.64.0.1"), false, 50},
		{"K2 cut", mk(60, rep(50), "100.64.0.1"), false, 28},
		{"K3 avail below 12", mk(60, rep(40), strings.Repeat("a", 29)), false, 40},
		{"K4 fits", mk(60, rep(20), "100.64.0.1"), false, 20},
		{"K5 avail exactly 12", mk(60, rep(30), strings.Repeat("a", 26)), false, 12},
		{"K6 avail 11", mk(60, rep(30), strings.Repeat("a", 27)), false, 30},
		{"K7 name equals avail", mk(60, rep(28), "100.64.0.1"), false, 28},
		{"mark column costs two", mk(60, rep(50), "100.64.0.1"), true, 26},
	}
	for _, c := range cases {
		if got := c.m.listColumns(c.marks).name; got != c.wantCol {
			t.Errorf("%s: name column %d want %d", c.label, got, c.wantCol)
		}
	}
}

// Verifies: REQ-ROW-016, REQ-ROW-017
func TestDO178CRenderListDevice(t *testing.T) {
	rowColors(t)
	devs := []client.Device{
		{ID: "1", Name: "alpha", Addresses: []string{"100.1.1.1"}, ConnectedToControl: true},
		{ID: "2", Name: strings.Repeat("long", 20), Addresses: []string{"100.2.2.2"}},
		{ID: "3", Name: "日本語のマシン", Addresses: []string{"fd7a::3"}},
		{ID: "4"}, // no name, no hostname, no address
	}
	m := model{width: 70, list: deviceList{
		filteredDevices: devs, cursor: 1, marked: map[string]bool{"1": true},
	}, set: uiSettings{shortNames: true}}
	c := m.listColumns(true)
	if c.name >= len(strings.Repeat("long", 20)) {
		t.Fatal("test needs a truncated column")
	}
	var width int
	for i := range devs {
		row := m.renderListDevice(i, c, true)
		w := lipgloss.Width(row)
		if i == 0 {
			width = w
		} else if w != width {
			t.Errorf("row %d width %d, row 0 width %d", i, w, width)
		}
	}
	r0 := m.renderListDevice(0, c, true)
	r1 := m.renderListDevice(1, c, true)
	if !strings.HasPrefix(r0, "    ✔ ") || strings.Contains(r0, "▶") {
		t.Errorf("unselected, marked row: %q", r0)
	}
	if !strings.Contains(r1, "▶ ") || strings.Contains(r1, "✔") || !strings.Contains(r1, "...") {
		t.Errorf("selected, unmarked, truncated row: %q", r1)
	}
	if !strings.Contains(m.renderListDevice(3, c, true), "N/A") {
		t.Error("N/A for a machine without address")
	}
	// no mark column: two cells narrower, no check mark even for a marked machine
	noMarks := m.listColumns(false)
	r := m.renderListDevice(0, noMarks, false)
	if strings.Contains(r, "✔") || lipgloss.Width(r) != lipgloss.Width(m.renderListDevice(0, noMarks, false)) {
		t.Errorf("no marks: %q", r)
	}
	// the truncated name column absorbs the two cells of the mark column
	if lipgloss.Width(r) != width {
		t.Errorf("no-mark row width %d, want %d", lipgloss.Width(r), width)
	}
	// without truncation the mark column is two cells wide
	short := model{list: deviceList{filteredDevices: devs[:1]}}
	wm := lipgloss.Width(short.renderListDevice(0, short.listColumns(true), true))
	wn := lipgloss.Width(short.renderListDevice(0, short.listColumns(false), false))
	if wm-wn != 2 {
		t.Errorf("mark column width: %d vs %d", wm, wn)
	}
}

func rowsFixture() model {
	now := time.Now()
	return model{
		set: uiSettings{groupByAccount: true},
		list: deviceList{
			devices: []client.Device{
				{ID: "1", AccountName: "home", LastSeen: now},
				{ID: "2", AccountName: "home"},
				{ID: "3", AccountName: "work", LastSeen: now},
				{ID: "4", AccountName: ""},
				{ID: "5", AccountName: "work", LastSeen: now},
			},
		},
	}
}

// Verifies: REQ-ROW-018, REQ-ROW-019, REQ-ROW-020, REQ-ROW-021, REQ-ROW-022, REQ-ROW-023
func TestDO178CListRows(t *testing.T) {
	m := rowsFixture()
	m.list.filteredDevices = []client.Device{m.list.devices[0], m.list.devices[1], m.list.devices[2], m.list.devices[4], m.list.devices[3]}

	if m.groupCount() != 3 || !m.showGroupHeaders() {
		t.Fatal("grouping on, 3 accounts")
	}
	counts := m.accountCounts()
	if counts["home"] != [2]int{1, 1} || counts["work"] != [2]int{2, 0} || counts[""] != [2]int{0, 1} || len(counts) != 3 {
		t.Fatalf("counts %v", counts)
	}
	if len((model{}).accountCounts()) != 0 {
		t.Fatal("empty counts")
	}

	rows := m.listRows()
	wantKinds := []listRowKind{rowHeader, rowDevice, rowDevice, rowHeader, rowDevice, rowDevice, rowHeader, rowDevice}
	if len(rows) != len(wantKinds) {
		t.Fatalf("%d rows", len(rows))
	}
	for i, k := range wantKinds {
		if rows[i].kind != k {
			t.Errorf("row %d kind %v want %v", i, rows[i].kind, k)
		}
	}
	if rows[0].text != "home  ·  1 online, 1 offline" || rows[3].text != "work  ·  2 online, 0 offline" || rows[6].text != "(no account)  ·  0 online, 1 offline" {
		t.Errorf("headers: %q %q %q", rows[0].text, rows[3].text, rows[6].text)
	}
	if rows[1].idx != 0 || rows[2].idx != 1 || rows[4].idx != 2 || rows[5].idx != 3 || rows[7].idx != 4 {
		t.Errorf("device indexes: %+v", rows)
	}

	// grouping off: no headers
	m.set.groupByAccount = false
	if m.showGroupHeaders() || len(m.listRows()) != 5 {
		t.Fatal("grouping off")
	}
	// grouping on, one account: no headers
	m.set.groupByAccount = true
	m.list.filteredDevices = m.list.filteredDevices[:2]
	if m.groupCount() != 1 || m.showGroupHeaders() || len(m.listRows()) != 2 {
		t.Fatal("one account")
	}
	m.list.hiddenOffline = 3
	rows = m.listRows()
	if last := rows[len(rows)-1]; last.kind != rowNote || last.text != "▸ 3 offline hidden · H shows them" {
		t.Fatalf("note: %+v", last)
	}
	m.list.hiddenOffline = 0
	if rows = m.listRows(); rows[len(rows)-1].kind == rowNote {
		t.Fatal("no note without hidden machines")
	}
	// empty list with hidden machines: only the note
	e := model{list: deviceList{hiddenOffline: 1}}
	if rows = e.listRows(); len(rows) != 1 || rows[0].kind != rowNote {
		t.Fatalf("empty: %+v", rows)
	}
	if rows = (model{}).listRows(); len(rows) != 0 {
		t.Fatal("empty without note")
	}
	if countText(0, 0) != "0 online, 0 offline" || countText(12, 3) != "12 online, 3 offline" {
		t.Error("countText")
	}
}

// Verifies: REQ-ROW-024
func TestDO178CCursorRow(t *testing.T) {
	rows := []listRow{
		{kind: rowHeader, idx: 0, text: "h"},
		{kind: rowDevice, idx: 0}, {kind: rowDevice, idx: 1},
		{kind: rowHeader, idx: 0, text: "h2"},
		{kind: rowDevice, idx: 2},
		{kind: rowNote, idx: 3, text: "n"},
	}
	for cursor, want := range map[int]int{0: 1, 1: 2, 2: 4, 3: 0, -1: 0, 99: 0} {
		if got := cursorRow(rows, cursor); got != want {
			t.Errorf("cursorRow(%d) = %d, want %d", cursor, got, want)
		}
	}
	if cursorRow(nil, 0) != 0 {
		t.Error("no rows")
	}
}

func deviceRows(n int) []listRow {
	rows := make([]listRow, n)
	for i := range rows {
		rows[i] = listRow{kind: rowDevice, idx: i}
	}
	return rows
}

// Verifies: REQ-ROW-025, REQ-ROW-026
func TestDO178CWindowTop(t *testing.T) {
	flat := deviceRows(20)
	withHeaders := []listRow{
		{kind: rowHeader}, {kind: rowDevice, idx: 0}, {kind: rowDevice, idx: 1}, {kind: rowDevice, idx: 2},
		{kind: rowHeader}, {kind: rowDevice, idx: 3}, {kind: rowDevice, idx: 4},
	}
	cases := []struct {
		label                 string
		rows                  []listRow
		cursorRow, top, capac int
		want                  int
	}{
		{"no rows", nil, 3, 5, 3, 0},
		{"capacity zero", flat, 3, 5, 0, 0},
		{"capacity negative", flat, 3, 5, -2, 0},
		{"inside, first row", flat, 5, 5, 3, 5},
		{"inside, last row", flat, 7, 5, 3, 5},
		{"above", flat, 2, 5, 3, 2},
		{"below by one", flat, 8, 5, 3, 6},
		{"below, far", flat, 19, 0, 3, 17},
		{"cursor at first row, scrolled down", flat, 0, 9, 3, 0},
		{"above, no header above", flat, 3, 10, 4, 3},
		{"above, header directly above is kept", withHeaders, 5, 6, 3, 4},
		{"above, header above first row", withHeaders, 1, 3, 3, 0},
		{"above, device row above", withHeaders, 3, 5, 3, 3},
		{"negative top is clamped", flat, 0, -5, 3, 0},
		{"window larger than list", flat, 19, 0, 50, 0},
		{"capacity one", flat, 10, 0, 1, 10},
	}
	for _, c := range cases {
		if got := windowTop(c.rows, c.cursorRow, c.top, c.capac); got != c.want {
			t.Errorf("%s: windowTop = %d, want %d", c.label, got, c.want)
		}
	}
}

// Verifies: REQ-ROW-027
func TestDO178CListCapacity(t *testing.T) {
	// Run panel closed: the layout's item count
	m := model{}
	if m.listCapacity() != 10 {
		t.Errorf("unknown height: %d", m.listCapacity())
	}
	m.height = 40
	if m.listCapacity() != 27 { // 40 - 7 - 6
		t.Errorf("closed, height 40: %d", m.listCapacity())
	}
	m.height = 5
	if m.listCapacity() != 1 {
		t.Errorf("closed, tiny: %d", m.listCapacity())
	}

	// Run panel open: panel height 25 (unknown height) -> 2*25+2-2 = 50 rows, minus header 2 and indicators 2
	m = model{run: runPanel{visible: true}}
	if m.listCapacity() != 46 {
		t.Errorf("open, no search: %d", m.listCapacity())
	}
	m.input.mode = inputSearch
	if m.listCapacity() != 45 {
		t.Errorf("open, search prompt: %d", m.listCapacity())
	}
	m.input.mode = inputNone
	m.list.searchQuery = "x"
	if m.listCapacity() != 45 {
		t.Errorf("open, query: %d", m.listCapacity())
	}
	m.list.searchQuery = ""
	m.height = 20 // panel height 15 -> 30 rows
	if m.listCapacity() != 26 {
		t.Errorf("open, height 20: %d", m.listCapacity())
	}
	m.height = 50 // panel height 19 -> 38 rows
	if m.listCapacity() != 34 {
		t.Errorf("open, height 50: %d", m.listCapacity())
	}
}

// Verifies: REQ-ROW-028
func TestDO178CEnsureListCursorVisible(t *testing.T) {
	mkList := func(n int) []client.Device {
		ds := make([]client.Device, n)
		for i := range ds {
			ds[i] = client.Device{ID: string(rune('a'+i%26)) + string(rune('A'+i/26)), AccountName: "acc", LastSeen: time.Now()}
		}
		return ds
	}
	check := func(label string, m model, wantTop int) {
		t.Helper()
		m.ensureListCursorVisible()
		if m.list.viewportTop != wantTop {
			t.Errorf("%s: top %d want %d", label, m.list.viewportTop, wantTop)
		}
		rows := m.listRows()
		if len(rows) > 0 {
			cr := cursorRow(rows, m.list.cursor)
			if cr < m.list.viewportTop || cr >= m.list.viewportTop+m.listCapacity() {
				t.Errorf("%s: cursor row %d outside [%d,%d)", label, cr, m.list.viewportTop, m.list.viewportTop+m.listCapacity())
			}
		}
	}
	base := func(n, cursor, top, height int) model {
		return model{height: height, list: deviceList{filteredDevices: mkList(n), cursor: cursor, viewportTop: top}}
	}
	// height 15 -> capacity 2
	check("first row", base(10, 0, 5, 15), 0)
	check("last row", base(10, 9, 0, 15), 8)
	check("middle, below window", base(10, 5, 0, 15), 4)
	check("middle, above window", base(10, 2, 6, 15), 2)
	check("inside window", base(10, 5, 4, 15), 4)
	check("list shorter than window", base(3, 2, 0, 40), 0)
	check("empty list", base(0, 0, 3, 40), 0)
	check("unknown window size", base(30, 12, 0, 0), 3) // capacity 10

	// headers: scrolling up keeps the header above the cursor in view
	m := base(6, 3, 6, 15)
	for i := range m.list.filteredDevices {
		if i >= 3 {
			m.list.filteredDevices[i].AccountName = "other"
		}
	}
	m.set.groupByAccount = true
	// rows: H d0 d1 d2 H d3 d4 d5; cursor 3 = row 5, previous row 4 is a header
	check("header kept", m, 4)
}
