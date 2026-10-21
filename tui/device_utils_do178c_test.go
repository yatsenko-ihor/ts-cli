package tui

import (
	"testing"
	"time"

	"github.com/yatsenko-ihor/ts-cli/client"
)

func names(ds []client.Device) []string {
	out := make([]string, len(ds))
	for i, d := range ds {
		out[i] = d.Name
	}
	return out
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Verifies: REQ-DEV-001
func TestDO178CSortModeString(t *testing.T) {
	cases := map[sortMode]string{
		sortByName: "Name", sortByLastSeen: "Last seen", sortByCreated: "Added",
		sortModeCount: "Unknown", sortMode(-1): "Unknown", sortMode(99): "Unknown",
	}
	for mode, want := range cases {
		if got := mode.String(); got != want {
			t.Errorf("sortMode(%d) = %q, want %q", int(mode), got, want)
		}
	}
}

// Verifies: REQ-DEV-002
func TestDO178CIsDeviceOnline(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name string
		d    client.Device
		want bool
	}{
		{"connected, zero last seen", client.Device{ConnectedToControl: true}, true},
		{"connected, long ago", client.Device{ConnectedToControl: true, LastSeen: now.Add(-48 * time.Hour)}, true},
		{"seen just now", client.Device{LastSeen: now}, true},
		{"seen 4m59s ago", client.Device{LastSeen: now.Add(-5*time.Minute + 2*time.Second)}, true},
		{"seen 5m01s ago", client.Device{LastSeen: now.Add(-5*time.Minute - time.Second)}, false},
		{"never seen", client.Device{}, false},
	}
	for _, c := range cases {
		if got := isDeviceOnline(c.d); got != c.want {
			t.Errorf("%s: online=%v want %v", c.name, got, c.want)
		}
	}
}

// Verifies: REQ-DEV-003, REQ-DEV-004, REQ-DEV-005, REQ-DEV-006
func TestDO178CSortOnlineFirst(t *testing.T) {
	now := time.Now()
	old := now.Add(-time.Hour)
	mk := func() []client.Device {
		return []client.Device{
			{Name: "off-b", LastSeen: old.Add(-2 * time.Hour), Created: now.Add(-1 * time.Hour)},
			{Name: "ON-b", LastSeen: now.Add(-3 * time.Minute), Created: now.Add(-5 * time.Hour)},
			{Name: "off-a", LastSeen: old, Created: now.Add(-2 * time.Hour)},
			{Name: "on-a", LastSeen: now, Created: now.Add(-9 * time.Hour)},
		}
	}
	cases := []struct {
		name string
		mode sortMode
		want []string
	}{
		// online-first, offline-last, both-online and both-offline are all present
		{"name", sortByName, []string{"on-a", "ON-b", "off-a", "off-b"}},
		{"last seen", sortByLastSeen, []string{"on-a", "ON-b", "off-a", "off-b"}},
		{"created", sortByCreated, []string{"ON-b", "on-a", "off-b", "off-a"}},
		{"unknown mode sorts by name", sortMode(42), []string{"on-a", "ON-b", "off-a", "off-b"}},
	}
	for _, c := range cases {
		ds := mk()
		sortDevices(ds, c.mode)
		if got := names(ds); !sameStrings(got, c.want) {
			t.Errorf("%s: %v want %v", c.name, got, c.want)
		}
	}

	// sortDevicesByStatus is the name sort
	ds := mk()
	sortDevicesByStatus(ds)
	if got := names(ds); !sameStrings(got, []string{"on-a", "ON-b", "off-a", "off-b"}) {
		t.Errorf("sortDevicesByStatus: %v", got)
	}

	// Hostname stands in for an empty Name; unicode names compare case-insensitively.
	ds = []client.Device{
		{Hostname: "zeta", LastSeen: now}, {Name: "Éclair", LastSeen: now}, {Name: "alpha", LastSeen: now}, {Hostname: "Beta", LastSeen: now},
	}
	sortDevices(ds, sortByName)
	got := []string{ds[0].Name + ds[0].Hostname, ds[1].Name + ds[1].Hostname, ds[2].Name + ds[2].Hostname, ds[3].Name + ds[3].Hostname}
	if !sameStrings(got, []string{"alpha", "Beta", "zeta", "Éclair"}) {
		t.Errorf("hostname fallback order: %v", got)
	}

	// Stability: equal keys keep their order.
	ds = []client.Device{{ID: "1", Name: "x", LastSeen: now}, {ID: "2", Name: "X", LastSeen: now}, {ID: "3", Name: "x", LastSeen: now}}
	sortDevices(ds, sortByName)
	if ds[0].ID != "1" || ds[1].ID != "2" || ds[2].ID != "3" {
		t.Errorf("sort is not stable: %+v", ds)
	}

	// Empty and single element.
	sortDevices(nil, sortByName)
	one := []client.Device{{Name: "only"}}
	sortDevices(one, sortByCreated)
	if one[0].Name != "only" {
		t.Error("single element changed")
	}
}

// Verifies: REQ-DEV-007, REQ-DEV-008
func TestDO178CStatusAndExpiryIcons(t *testing.T) {
	if getStatusIcon(client.Device{ConnectedToControl: true}) != "🟢" || getStatusIcon(client.Device{}) != "🔴" {
		t.Error("status icon")
	}
	now := time.Now()
	cases := []struct {
		name string
		d    client.Device
		want string
	}{
		{"disabled", client.Device{KeyExpiryDisabled: true, Expires: now.Add(time.Hour)}, ""},
		{"no date", client.Device{}, ""},
		{"disabled and no date", client.Device{KeyExpiryDisabled: true}, ""},
		{"future", client.Device{Expires: now.Add(time.Hour)}, "🔑"},
		{"past", client.Device{Expires: now.Add(-time.Hour)}, "🔒"},
	}
	for _, c := range cases {
		if got := getKeyExpiryIcon(c.d); got != c.want {
			t.Errorf("%s: %q want %q", c.name, got, c.want)
		}
	}
}

func filterFixture() model {
	now := time.Now()
	return model{
		set: uiSettings{},
		acct: accounts{list: []client.AccountInfo{
			{Name: "home", Tailnet: "home.com"}, {Name: "work", Tailnet: "work.com", Hidden: true}, {Name: "lab", Tailnet: "lab.com"},
		}},
		list: deviceList{devices: []client.Device{
			{ID: "1", Name: "Alpha", Hostname: "alpha-h", OS: "linux", Addresses: []string{"100.1.1.1"}, AccountName: "home", LastSeen: now},
			{ID: "2", Name: "bravo", Hostname: "bravo-h", OS: "macOS", Addresses: []string{"100.2.2.2"}, AccountName: "work", LastSeen: now},
			{ID: "3", Name: "charlie", Hostname: "charlie-h", OS: "windows", Addresses: []string{"fd7a::3"}, AccountName: "lab", LastSeen: now.Add(-time.Hour)},
			{ID: "4", Name: "delta", Hostname: "delta-h", OS: "linux", AccountName: "ghost", LastSeen: now},
			{ID: "5", Name: "", Hostname: "", AccountName: "lab", LastSeen: now},
		}},
	}
}

func ids(ds []client.Device) string {
	s := ""
	for _, d := range ds {
		s += d.ID
	}
	return s
}

// Verifies: REQ-DEV-009, REQ-DEV-010
func TestDO178CFilterHiddenAccountsAndProfile(t *testing.T) {
	m := filterFixture()
	m.filterDevices()
	// bravo belongs to hidden "work"; "ghost" is not configured and stays visible.
	for _, d := range m.list.filteredDevices {
		if d.ID == "2" {
			t.Fatal("machine of a hidden account is listed")
		}
	}
	if len(m.list.filteredDevices) != 4 {
		t.Fatalf("want 4 machines, got %d", len(m.list.filteredDevices))
	}

	m.list.selectedProfile = "lab"
	m.filterDevices()
	if len(m.list.filteredDevices) != 2 {
		t.Fatalf("profile lab: %d machines", len(m.list.filteredDevices))
	}
	for _, d := range m.list.filteredDevices {
		if d.AccountName != "lab" {
			t.Fatalf("foreign machine in profile: %+v", d)
		}
	}
	m.list.selectedProfile = "work" // hidden: nothing may show
	m.filterDevices()
	if len(m.list.filteredDevices) != 0 {
		t.Fatal("hidden profile must show nothing")
	}
	m.list.selectedProfile = "nonexistent"
	m.filterDevices()
	if len(m.list.filteredDevices) != 0 {
		t.Fatal("unknown profile must show nothing")
	}

	// no devices at all
	e := model{}
	e.filterDevices()
	if len(e.list.filteredDevices) != 0 {
		t.Fatal("empty list")
	}
}

// Verifies: REQ-DEV-011
func TestDO178CFilterSearch(t *testing.T) {
	cases := []struct {
		name, query, want string
	}{
		{"name only, case-insensitive", "ALPHA", "1"},
		{"hostname only", "delta-h", "4"},
		{"os only", "WINDOWS", "3"},
		{"address only", "FD7A", "3"},
		{"nothing matches", "zzz-no-such", ""},
		{"unicode query", "é", ""},
		{"os shared by two", "linux", "14"},
	}
	for _, c := range cases {
		m := filterFixture()
		m.list.searchQuery = c.query
		m.filterDevices()
		got := ids(m.list.filteredDevices)
		if got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
	// the empty machine (no name, hostname, OS, addresses) never matches
	m := filterFixture()
	m.list.searchQuery = "x"
	m.filterDevices()
	for _, d := range m.list.filteredDevices {
		if d.ID == "5" {
			t.Fatal("empty machine matched a query")
		}
	}
}

// Verifies: REQ-DEV-012
func TestDO178CFilterHideOffline(t *testing.T) {
	// hideOffline=T query="" : collapsed
	m := filterFixture()
	m.set.hideOffline = true
	m.list.hiddenOffline = 99 // stale value must be reset
	m.filterDevices()
	if m.list.hiddenOffline != 1 || ids(m.list.filteredDevices) == "" || len(m.list.filteredDevices) != 3 {
		t.Fatalf("collapse: hidden=%d shown=%d", m.list.hiddenOffline, len(m.list.filteredDevices))
	}
	// hideOffline=F : nothing dropped, counter reset
	m.set.hideOffline = false
	m.filterDevices()
	if m.list.hiddenOffline != 0 || len(m.list.filteredDevices) != 4 {
		t.Fatalf("off: hidden=%d shown=%d", m.list.hiddenOffline, len(m.list.filteredDevices))
	}
	// hideOffline=T query!="" : search shows offline machines too
	m.set.hideOffline = true
	m.list.searchQuery = "charlie"
	m.filterDevices()
	if m.list.hiddenOffline != 0 || len(m.list.filteredDevices) != 1 {
		t.Fatalf("search: hidden=%d shown=%d", m.list.hiddenOffline, len(m.list.filteredDevices))
	}
	// an explicit ping result wins over the API state
	m.list.searchQuery = ""
	m.ping = map[string]pingState{"1": pingFail, "3": pingOK}
	m.filterDevices()
	if m.list.hiddenOffline != 1 || len(m.list.filteredDevices) != 3 {
		t.Fatalf("ping: hidden=%d shown=%d", m.list.hiddenOffline, len(m.list.filteredDevices))
	}
	for _, d := range m.list.filteredDevices {
		if d.ID == "1" {
			t.Fatal("ping-failed machine must be collapsed")
		}
	}
}

// Verifies: REQ-DEV-013, REQ-DEV-014, REQ-DEV-015, REQ-DEV-016
func TestDO178CFilterSortGroupingAndCursorReset(t *testing.T) {
	now := time.Now()
	m := model{
		set: uiSettings{groupByAccount: true},
		acct: accounts{list: []client.AccountInfo{
			{Name: "zeta"}, {Name: "alpha"},
		}},
		list: deviceList{devices: []client.Device{
			{ID: "1", Name: "m1", AccountName: "alpha", LastSeen: now},
			{ID: "2", Name: "m2", AccountName: "zeta", LastSeen: now},
			{ID: "3", Name: "m0", AccountName: "zeta", LastSeen: now},
			{ID: "4", Name: "m3", AccountName: "yy", LastSeen: now},
			{ID: "5", Name: "m4", AccountName: "", LastSeen: now},
			{ID: "6", Name: "m5", AccountName: "aa", LastSeen: now},
			{ID: "7", Name: "mz", AccountName: "zeta"},
		}, cursor: 5, viewportTop: 3},
	}
	orig := ids(m.list.devices)
	m.filterDevices()
	// configured: zeta, alpha; then unconfigured alphabetical: "" , aa, yy
	want := []string{"m0", "m2", "mz", "m1", "m4", "m5", "m3"}
	if got := names(m.list.filteredDevices); !sameStrings(got, want) {
		t.Fatalf("grouped order %v want %v", got, want)
	}
	if ids(m.list.devices) != orig {
		t.Fatal("underlying device list was reordered")
	}
	if m.list.cursor != 0 || m.list.viewportTop != 0 {
		t.Fatal("cursor and viewport must reset")
	}

	// grouping off: plain online-first name order across accounts
	m.set.groupByAccount = false
	m.filterDevices()
	want = []string{"m0", "m1", "m2", "m3", "m4", "m5", "mz"}
	if got := names(m.list.filteredDevices); !sameStrings(got, want) {
		t.Fatalf("ungrouped order %v want %v", got, want)
	}

	// sort mode is honoured: created newest first
	m.set.groupByAccount = false
	for i := range m.list.devices {
		m.list.devices[i].Created = now.Add(-time.Duration(i) * time.Hour)
	}
	m.list.sort = sortByCreated
	m.filterDevices()
	if m.list.filteredDevices[0].ID != "1" {
		t.Fatalf("created sort: %v", names(m.list.filteredDevices))
	}

	rank := m.accountRank()
	if rank["zeta"] != 0 || rank["alpha"] != 1 || rank[""] != 2 || rank["aa"] != 3 || rank["yy"] != 4 {
		t.Fatalf("rank %v", rank)
	}
	// no accounts, no devices
	if r := (model{}).accountRank(); len(r) != 0 {
		t.Fatalf("empty rank %v", r)
	}
}

// Verifies: REQ-DEV-017, REQ-DEV-018, REQ-DEV-019
func TestDO178CRefilterKeepsCursorOnMachine(t *testing.T) {
	now := time.Now()
	var devs []client.Device
	for i := 0; i < 30; i++ {
		devs = append(devs, client.Device{ID: string(rune('a' + i)), Name: string(rune('A' + i)), LastSeen: now})
	}
	m := model{set: uiSettings{}, height: 20, list: deviceList{devices: devs}}
	m.filterDevices()
	m.list.cursor = 20
	target := m.list.filteredDevices[20].Name
	m.ensureListCursorVisible()

	// order flips: the machine moves, the cursor follows it
	for i, j := 0, len(m.list.devices)-1; i < j; i, j = i+1, j-1 {
		m.list.devices[i], m.list.devices[j] = m.list.devices[j], m.list.devices[i]
	}
	m.list.devices[0].LastSeen = now.Add(time.Second)
	m.list.sort = sortByLastSeen
	m.refilterKeepCursor()
	if m.list.filteredDevices[m.list.cursor].Name != target {
		t.Fatalf("cursor left its machine: %q want %q", m.list.filteredDevices[m.list.cursor].Name, target)
	}
	rows := m.listRows()
	cr := cursorRow(rows, m.list.cursor)
	if cr < m.list.viewportTop || cr >= m.list.viewportTop+m.listCapacity() {
		t.Fatalf("cursor row %d not visible (top %d, cap %d)", cr, m.list.viewportTop, m.listCapacity())
	}

	// the machine is filtered away: back to the first row
	m.list.searchQuery = "A"
	m.refilterKeepCursor()
	if m.list.cursor != 0 {
		t.Fatalf("cursor %d", m.list.cursor)
	}

	// nothing highlighted (empty list and out-of-range cursor)
	m.list.searchQuery = "no-such"
	m.refilterKeepCursor()
	m.list.cursor = 7
	m.refilterKeepCursor()
	if m.list.cursor != 0 || len(m.list.filteredDevices) != 0 {
		t.Fatal("empty list")
	}
}
