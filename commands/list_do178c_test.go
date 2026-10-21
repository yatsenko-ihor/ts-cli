package commands

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yatsenko-ihor/ts-cli/client"
)

// Requirements-based tests for list.go (verification/requirements/list.md).

var lstNow = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

type lstCall struct {
	apiKey, tailnet string
	quiet           bool
}

// d178StubList replaces listDevicesFn and records its calls.
func d178StubList(t *testing.T, devs []client.Device, err error) *[]lstCall {
	t.Helper()
	var calls []lstCall
	old := listDevicesFn
	listDevicesFn = func(k, tn string, q bool) ([]client.Device, error) {
		calls = append(calls, lstCall{k, tn, q})
		return devs, err
	}
	t.Cleanup(func() { listDevicesFn = old })
	return &calls
}

func lstDev(host string, mod func(d *client.Device)) client.Device {
	d := client.Device{Name: host + ".ts.net", Hostname: host, OS: "linux", AccountName: "work", Addresses: []string{"100.64.0.1"}, Authorized: true}
	if mod != nil {
		mod(&d)
	}
	return d
}

func lstHosts(ds []client.Device) []string {
	out := []string{}
	for _, d := range ds {
		out = append(out, d.Hostname)
	}
	return out
}

// ─── option validation ───────────────────────────────────────────────────────

// Verifies: REQ-LST-001, REQ-LST-002, REQ-LST-003
func TestDO178CListRejectsInvalidOptionsBeforeFetching(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"--online", "--offline"}, "--online and --offline are mutually exclusive"},
		{[]string{"--sort", "bogus"}, "unknown sort 'bogus'"},
		{[]string{"--sort", ""}, "unknown sort ''"},
		{[]string{"--sort", "name;rm"}, "unknown sort"},
		{[]string{"--format", "xml"}, "unknown format 'xml'"},
		{[]string{"--format", ""}, "unknown format ''"},
		{[]string{"--format", "table "}, "unknown format"},
	}
	for _, c := range cases {
		calls := d178StubList(t, nil, nil)
		out, _, err := d178Run(t, NewListCommand(), "", c.args...)
		if err == nil || !strings.Contains(err.Error(), c.want) || len(*calls) != 0 || out != "" {
			t.Errorf("%q: err=%v calls=%d out=%q", c.args, err, len(*calls), out)
		}
	}
	// accepted spellings, in any case
	for _, args := range [][]string{
		{"--sort", "NAME"}, {"--sort", "LastSeen"}, {"--sort", "OS"}, {"--sort", "account"},
		{"--format", "TABLE"}, {"--format", "Json"}, {"--format", "NAMES"}, {"--online"}, {"--offline"},
	} {
		d178StubList(t, []client.Device{lstDev("a", nil)}, nil)
		if _, _, err := d178Run(t, NewListCommand(), "", args...); err != nil {
			t.Errorf("%q: %v", args, err)
		}
	}
}

// Verifies: REQ-LST-004
func TestDO178CListQuietnessOfTheFetch(t *testing.T) {
	cases := []struct {
		args  []string
		quiet bool
	}{
		{nil, false},
		{[]string{"--format", "table"}, false},
		{[]string{"-q"}, true},
		{[]string{"--quiet"}, true},
		{[]string{"--format", "json"}, true},
		{[]string{"--format", "names"}, true},
		{[]string{"--format", "NAMES", "-q"}, true},
	}
	for _, c := range cases {
		calls := d178StubList(t, []client.Device{lstDev("a", nil)}, nil)
		args := append([]string{"--api-key", "K", "--tailnet", "T"}, c.args...)
		if _, _, err := d178Run(t, NewListCommand(), "", args...); err != nil {
			t.Fatal(err)
		}
		if want := []lstCall{{"K", "T", c.quiet}}; !reflect.DeepEqual(*calls, want) {
			t.Errorf("%q: calls = %+v, want %+v", c.args, *calls, want)
		}
	}
}

// Verifies: REQ-LST-005
func TestDO178CListFetchErrorIsReturnedAndNothingPrinted(t *testing.T) {
	d178StubList(t, []client.Device{lstDev("a", nil)}, errors.New("boom"))
	out, errOut, err := d178Run(t, NewListCommand(), "")
	if err == nil || err.Error() != "boom" || out != "" || errOut != "" {
		t.Fatalf("err=%v out=%q err=%q", err, out, errOut)
	}
	// positional arguments are rejected
	if _, _, err := d178Run(t, NewListCommand(), "", "extra"); err == nil {
		t.Fatal("list takes no arguments")
	}
}

// ─── empty result ────────────────────────────────────────────────────────────

// Verifies: REQ-LST-006
func TestDO178CListEmptyResults(t *testing.T) {
	devs := []client.Device{lstDev("a", nil)}
	cases := []struct {
		name         string
		devs         []client.Device
		args         []string
		wantOut      string
		wantErrOut   string
		outIsJSONArr bool
	}{
		{"no devices at all, table", nil, nil, "No devices found in any of your configured accounts.\n", "", false},
		{"no devices at all, names", nil, []string{"--format", "names"}, "No devices found in any of your configured accounts.\n", "", false},
		{"no devices, json", nil, []string{"--format", "json"}, "", "", true},
		{"filter matches nothing, table", devs, []string{"--os", "plan9"}, "", "No devices match the filters.\n", false},
		{"filter matches nothing, names", devs, []string{"--name", "zzz", "--format", "names"}, "", "No devices match the filters.\n", false},
		{"filter matches nothing, json", devs, []string{"--os", "plan9", "--format", "json"}, "", "", true},
		{"sort alone is not a filter", nil, []string{"--sort", "os"}, "No devices found in any of your configured accounts.\n", "", false},
	}
	for _, c := range cases {
		d178StubList(t, c.devs, nil)
		out, errOut, err := d178Run(t, NewListCommand(), "", c.args...)
		if err != nil || errOut != c.wantErrOut {
			t.Errorf("%s: err=%v stderr=%q", c.name, err, errOut)
		}
		if c.outIsJSONArr {
			if strings.TrimSpace(out) != "[]" {
				t.Errorf("%s: stdout = %q, want []", c.name, out)
			}
		} else if out != c.wantOut {
			t.Errorf("%s: stdout = %q", c.name, out)
		}
	}
}

// ─── formats ─────────────────────────────────────────────────────────────────

// Verifies: REQ-LST-007
func TestDO178CListFormatsDispatch(t *testing.T) {
	devs := []client.Device{lstDev("b", nil), lstDev("a", nil)}
	d178StubList(t, devs, nil)

	out, _, _ := d178Run(t, NewListCommand(), "", "--format", "names")
	if out != "a\nb\n" {
		t.Errorf("names: %q", out)
	}
	out, _, _ = d178Run(t, NewListCommand(), "", "--format", "json")
	var dec []map[string]any
	if err := json.Unmarshal([]byte(out), &dec); err != nil || len(dec) != 2 || dec[0]["hostname"] != "a" {
		t.Errorf("json: %v %q", err, out)
	}
	out, _, _ = d178Run(t, NewListCommand(), "")
	if !strings.Contains(out, "ACCOUNT") || !strings.Contains(out, "Total devices: 2") {
		t.Errorf("table: %q", out)
	}
	// input slice order is not changed by sorting
	if devs[0].Hostname != "b" {
		t.Error("the fetched slice must not be reordered")
	}
}

// ─── filters ─────────────────────────────────────────────────────────────────

// Verifies: REQ-LST-008
func TestDO178CDeviceFilterActive(t *testing.T) {
	cases := []struct {
		name string
		f    deviceFilter
		want bool
	}{
		{"none", deviceFilter{}, false},
		{"only sort", deviceFilter{Sort: "os"}, false},
		{"empty slices", deviceFilter{Tags: []string{}, Accounts: []string{}}, false},
		{"online", deviceFilter{Online: true}, true},
		{"offline", deviceFilter{Offline: true}, true},
		{"os", deviceFilter{OS: "x"}, true},
		{"tags", deviceFilter{Tags: []string{"a"}}, true},
		{"accounts", deviceFilter{Accounts: []string{"a"}}, true},
		{"name", deviceFilter{Name: "x"}, true},
	}
	for _, c := range cases {
		if got := c.f.active(); got != c.want {
			t.Errorf("%s: %v", c.name, got)
		}
	}
}

// Verifies: REQ-LST-009
func TestDO178CDeviceOnlineBoundaries(t *testing.T) {
	cases := []struct {
		name string
		d    client.Device
		want bool
	}{
		{"connected, never seen", client.Device{ConnectedToControl: true}, true},
		{"connected, long ago", client.Device{ConnectedToControl: true, LastSeen: lstNow.Add(-72 * time.Hour)}, true},
		{"not connected, never seen", client.Device{}, false},
		{"4m59s", client.Device{LastSeen: lstNow.Add(-5*time.Minute + time.Second)}, true},
		{"exactly 5m", client.Device{LastSeen: lstNow.Add(-5 * time.Minute)}, true},
		{"5m01s", client.Device{LastSeen: lstNow.Add(-5*time.Minute - time.Second)}, false},
		{"in the future (clock skew)", client.Device{LastSeen: lstNow.Add(time.Hour)}, true},
	}
	for _, c := range cases {
		if got := deviceOnline(c.d, lstNow); got != c.want {
			t.Errorf("%s: %v", c.name, got)
		}
	}
}

// Verifies: REQ-LST-010
func TestDO178CTagMatching(t *testing.T) {
	d := client.Device{Tags: []string{"tag:Server", " tag:prod "}}
	for _, ok := range []string{"server", "tag:server", "SERVER", "TAG:SERVER", " server ", "prod", "tag:prod"} {
		if !hasTag(d, ok) {
			t.Errorf("%q should match", ok)
		}
	}
	for _, no := range []string{"serve", "servers", "tag:tag:server", "", "tag:", "db"} {
		if hasTag(d, no) {
			t.Errorf("%q must not match", no)
		}
	}
	if hasTag(client.Device{}, "server") {
		t.Error("device without tags has no tag")
	}
	if hasTag(client.Device{Tags: []string{""}}, "") != true {
		t.Error("an empty tag equals an empty query (degenerate but defined)")
	}
	if normalizeTag("  TAG:Foo ") != "foo" {
		t.Error("normalizeTag")
	}
}

// Verifies: REQ-LST-011
func TestDO178CFilterMatchesEachCondition(t *testing.T) {
	online := lstDev("on", func(d *client.Device) { d.ConnectedToControl = true })
	offline := lstDev("off", func(d *client.Device) { d.LastSeen = lstNow.Add(-time.Hour) })
	cases := []struct {
		name string
		f    deviceFilter
		d    client.Device
		want bool
	}{
		// MC/DC: Online && !online(d)
		{"online filter, online device", deviceFilter{Online: true}, online, true},
		{"online filter, offline device", deviceFilter{Online: true}, offline, false},
		{"no online filter, offline device", deviceFilter{}, offline, true},
		// MC/DC: Offline && online(d)
		{"offline filter, offline device", deviceFilter{Offline: true}, offline, true},
		{"offline filter, online device", deviceFilter{Offline: true}, online, false},
		{"no offline filter, online device", deviceFilter{}, online, true},
		// MC/DC: OS != "" && !EqualFold
		{"os filter equal (case-insensitive)", deviceFilter{OS: "LINUX"}, online, true},
		{"os filter different", deviceFilter{OS: "macos"}, online, false},
		{"os filter empty, any os", deviceFilter{OS: ""}, online, true},
		// tags: all must match
		{"two tags, both present", deviceFilter{Tags: []string{"a", "b"}}, lstDev("t", func(d *client.Device) { d.Tags = []string{"tag:a", "tag:b"} }), true},
		{"two tags, one missing", deviceFilter{Tags: []string{"a", "b"}}, lstDev("t", func(d *client.Device) { d.Tags = []string{"tag:a"} }), false},
		// accounts: any of, case-insensitive
		{"account list contains it", deviceFilter{Accounts: []string{"x", "WORK"}}, online, true},
		{"account list lacks it", deviceFilter{Accounts: []string{"x", "y"}}, online, false},
		{"account list on device without account", deviceFilter{Accounts: []string{"x"}}, lstDev("z", func(d *client.Device) { d.AccountName = "" }), false},
		// name: substring of Name OR Hostname, case-insensitive
		{"name matches hostname only", deviceFilter{Name: "ON"}, lstDev("on", func(d *client.Device) { d.Name = "zzz" }), true},
		{"name matches name only", deviceFilter{Name: "ZZ"}, lstDev("on", func(d *client.Device) { d.Name = "zzz" }), true},
		{"name matches neither", deviceFilter{Name: "qq"}, online, false},
		// filters combine with AND
		{"AND of os and name, both ok", deviceFilter{OS: "linux", Name: "on"}, online, true},
		{"AND of os and name, name fails", deviceFilter{OS: "linux", Name: "qq"}, online, false},
		{"AND of os and name, os fails", deviceFilter{OS: "ios", Name: "on"}, online, false},
		{"no filter matches everything", deviceFilter{}, client.Device{}, true},
	}
	for _, c := range cases {
		if got := c.f.matches(c.d, lstNow); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

// ─── sorting ─────────────────────────────────────────────────────────────────

// Verifies: REQ-LST-012
func TestDO178CFilterDevicesSortOrders(t *testing.T) {
	devs := []client.Device{
		{Name: "Charlie", Hostname: "c", OS: "linux", AccountName: "beta", LastSeen: lstNow.Add(-time.Hour)},
		{Name: "", Hostname: "Bravo", OS: "Linux", AccountName: "Alpha", LastSeen: lstNow.Add(-time.Hour)}, // empty Name: sorts by hostname
		{Name: "alpha", Hostname: "a", OS: "ios", AccountName: "alpha", ConnectedToControl: true},
		{Name: "delta", Hostname: "d", OS: "windows", AccountName: "", LastSeen: lstNow.Add(-2 * time.Hour)},
		{Name: "echo", Hostname: "e", OS: "", AccountName: "beta"}, // never seen
	}
	cases := []struct {
		sort string
		want []string
	}{
		{"", []string{"a", "Bravo", "c", "d", "e"}},     // default: name
		{"name", []string{"a", "Bravo", "c", "d", "e"}}, // case-insensitive, hostname fallback
		{"NAME", []string{"a", "Bravo", "c", "d", "e"}},
		{"lastseen", []string{"a", "Bravo", "c", "d", "e"}}, // connected first, equal times by name, never seen last
		{"os", []string{"e", "a", "Bravo", "c", "d"}},       // "" < ios < linux (==Linux, by name) < windows
		{"account", []string{"d", "a", "Bravo", "c", "e"}},  // "" < alpha (==Alpha, by name) < beta (by name)
		{"bogus", []string{"a", "Bravo", "c", "d", "e"}},    // unknown sort falls back to name
	}
	for _, c := range cases {
		before := append([]client.Device(nil), devs...)
		got := lstHosts(filterDevices(devs, deviceFilter{Sort: c.sort}, lstNow))
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("sort %q: %v, want %v", c.sort, got, c.want)
		}
		if !reflect.DeepEqual(before, devs) {
			t.Errorf("sort %q modified the input", c.sort)
		}
	}
	if got := filterDevices(nil, deviceFilter{}, lstNow); got == nil || len(got) != 0 {
		t.Error("the result is never nil")
	}
	if sortKeyName(client.Device{Name: "X", Hostname: "y"}) != "x" || sortKeyName(client.Device{Hostname: "Y"}) != "y" || sortKeyName(client.Device{}) != "" {
		t.Error("sortKeyName")
	}
}

// ─── table ───────────────────────────────────────────────────────────────────

// Verifies: REQ-LST-013
func TestDO178CTableContent(t *testing.T) {
	devs := []client.Device{
		{Name: "full.ts.net", Hostname: "full", OS: "linux", AccountName: "work", Addresses: []string{"100.64.0.1", "fd7a::1"}, Authorized: true, LastSeen: time.Now().Add(-2 * time.Hour), Tags: []string{"tag:a", "tag:b"}},
		{Hostname: "bare", ConnectedToControl: true}, // no name, account, address; connected without lastSeen
		{Name: "never.ts.net", Hostname: "never"},    // never seen, unauthorized
		{Name: "old.ts.net", Hostname: "old", Authorized: true, LastSeen: time.Now().Add(-49 * time.Hour)},
	}
	var sb strings.Builder
	writeDevicesTable(&sb, devs, false)
	out := sb.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	hdr := strings.Fields(lines[0])
	if strings.Join(hdr, " ") != "ACCOUNT NAME HOSTNAME ADDRESS OS TAGS LAST SEEN AUTHORIZED" {
		t.Fatalf("header: %q", lines[0])
	}
	if strings.Trim(lines[1], "- ") != "" {
		t.Fatalf("underline: %q", lines[1])
	}
	row := func(host string) []string {
		for _, l := range lines[2:] {
			if f := strings.Fields(l); len(f) > 2 && (f[1] == host || f[2] == host) {
				return f
			}
		}
		t.Fatalf("no row for %s in\n%s", host, out)
		return nil
	}
	if got := strings.Join(row("full"), "|"); got != "work|full.ts.net|full|100.64.0.1|linux|tag:a,tag:b|2|hours|ago|Yes" {
		t.Errorf("full row: %q", got)
	}
	bare := ""
	never := ""
	old := ""
	for _, l := range lines[2:] {
		switch {
		case strings.Contains(l, "bare"):
			bare = l
		case strings.Contains(l, "never.ts.net"):
			never = l
		case strings.Contains(l, "old.ts.net"):
			old = l
		}
	}
	for _, w := range []string{"-", "bare", "N/A", "online now", "No"} {
		if !strings.Contains(bare, w) {
			t.Errorf("bare row lacks %q: %q", w, bare)
		}
	}
	if strings.Count(bare, "bare") != 2 {
		t.Errorf("name must fall back to the hostname: %q", bare)
	}
	if !strings.Contains(never, "never") || !strings.HasSuffix(strings.TrimSpace(never), "No") {
		t.Errorf("never row: %q", never)
	}
	if !strings.Contains(old, "2 days ago") || !strings.HasSuffix(strings.TrimSpace(old), "Yes") {
		t.Errorf("old row: %q", old)
	}
	if strings.Count(strings.Join(lines, "\n"), "-") < 3 {
		t.Error("tag-less device shows '-' in the TAGS column")
	}
	if !strings.HasSuffix(out, "\nTotal devices: 4\n") {
		t.Errorf("footer: %q", out)
	}
}

// Verifies: REQ-LST-014
func TestDO178CTableHeaderFooterAndTagsColumnConditions(t *testing.T) {
	plain := []client.Device{lstDev("a", nil)}
	tagged := []client.Device{lstDev("a", nil), lstDev("b", func(d *client.Device) { d.Tags = []string{"tag:x"} })}
	cases := []struct {
		name          string
		devs          []client.Device
		quiet         bool
		header, tags  bool
		footer        bool
		wantDataLines int
	}{
		{"normal, no tags", plain, false, true, false, true, 1},
		{"normal, with tags", tagged, false, true, true, true, 2},
		{"quiet, no tags", plain, true, false, false, false, 1},
		{"quiet, with tags", tagged, true, false, false, false, 2},
	}
	for _, c := range cases {
		var sb strings.Builder
		writeDevicesTable(&sb, c.devs, c.quiet)
		out := sb.String()
		if strings.Contains(out, "ACCOUNT") != c.header || strings.Contains(out, "TAGS") != c.tags || strings.Contains(out, "Total devices") != c.footer {
			t.Errorf("%s:\n%s", c.name, out)
		}
		data := 0
		for _, l := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
			if strings.Contains(l, "100.64.0.1") {
				data++
			}
		}
		if data != c.wantDataLines {
			t.Errorf("%s: %d rows\n%s", c.name, data, out)
		}
	}
	// with tags, a device without tags shows "-" in the TAGS column
	var sb strings.Builder
	writeDevicesTable(&sb, tagged, true)
	if !strings.Contains(sb.String(), "100.64.0.1  linux  -  ") {
		t.Errorf("TAGS column of a tag-less device:\n%s", sb.String())
	}
}

// ─── names / json ────────────────────────────────────────────────────────────

// Verifies: REQ-LST-015
func TestDO178CNamesOutput(t *testing.T) {
	devs := []client.Device{{Name: "n1", Hostname: "h1"}, {Name: "n2"}, {Hostname: "h3"}, {}}
	var sb strings.Builder
	writeDeviceNames(&sb, devs)
	if sb.String() != "h1\nn2\nh3\n\n" {
		t.Fatalf("got %q", sb.String())
	}
	if shortName(client.Device{Name: "n", Hostname: "h"}) != "h" || shortName(client.Device{Name: "n"}) != "n" {
		t.Fatal("shortName")
	}
	var empty strings.Builder
	writeDeviceNames(&empty, nil)
	if empty.String() != "" {
		t.Fatal("no devices, no output")
	}
}

// Verifies: REQ-LST-016
func TestDO178CJSONShape(t *testing.T) {
	seen := time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("x", 2*3600))
	devs := []client.Device{
		{ID: "1", Name: "a", Hostname: "h", OS: "linux", Addresses: []string{"100.64.0.1"}, Authorized: true, LastSeen: seen, AccountName: "w", Tags: []string{"tag:x"}},
		{ID: "2", ConnectedToControl: true},
		{ID: "3", LastSeen: time.Now()},
	}
	var sb strings.Builder
	if err := writeDevicesJSON(&sb, devs); err != nil {
		t.Fatal(err)
	}
	var dec []map[string]any
	if err := json.Unmarshal([]byte(sb.String()), &dec); err != nil {
		t.Fatal(err)
	}
	if dec[0]["lastSeen"] != "2026-01-02T03:04:05+02:00" || dec[0]["account"] != "w" || dec[0]["online"] != false || dec[0]["authorized"] != true {
		t.Errorf("first: %v", dec[0])
	}
	if _, has := dec[1]["lastSeen"]; has || dec[1]["online"] != true || dec[1]["connectedToControl"] != true {
		t.Errorf("connected without lastSeen: %v", dec[1])
	}
	if _, has := dec[1]["account"]; has {
		t.Errorf("empty account must be omitted: %v", dec[1])
	}
	if dec[2]["online"] != true {
		t.Errorf("recently seen is online: %v", dec[2])
	}
	for _, k := range []string{"addresses", "tags"} {
		if a, ok := dec[1][k].([]any); !ok || len(a) != 0 {
			t.Errorf("%s must be []: %v", k, dec[1][k])
		}
	}
	// writer failure is returned
	if err := writeDevicesJSON(failWriter{}, devs); err == nil {
		t.Error("write error must be returned")
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

// ─── formatDuration ──────────────────────────────────────────────────────────

// Verifies: REQ-LST-017
func TestDO178CFormatDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{-time.Hour, "just now"}, {0, "just now"}, {59 * time.Second, "just now"}, {59*time.Second + 999*time.Millisecond, "just now"},
		{time.Minute, "1 minute ago"}, {119 * time.Second, "1 minute ago"}, {2 * time.Minute, "2 minutes ago"}, {59*time.Minute + 59*time.Second, "59 minutes ago"},
		{time.Hour, "1 hour ago"}, {119 * time.Minute, "1 hour ago"}, {2 * time.Hour, "2 hours ago"}, {24*time.Hour - time.Second, "23 hours ago"},
		{24 * time.Hour, "1 day ago"}, {47 * time.Hour, "1 day ago"}, {48 * time.Hour, "2 days ago"}, {365 * 24 * time.Hour, "365 days ago"},
	}
	for _, c := range cases {
		if got := formatDuration(c.d); got != c.want {
			t.Errorf("%v: %q, want %q", c.d, got, c.want)
		}
	}
}

// ─── fetchDevicesForList ─────────────────────────────────────────────────────

// Verifies: REQ-LST-018
func TestDO178CFetchDevicesConfigProblems(t *testing.T) {
	t.Run("unreadable configuration", func(t *testing.T) {
		d178SaveConfig(t, nil)
		d178WriteCorruptConfig(t)
		_, err := fetchDevicesForList("", "", true)
		if err == nil || !strings.Contains(err.Error(), "failed to load configuration") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("no accounts, even with explicit credentials", func(t *testing.T) {
		d178SaveConfig(t, nil)
		api := d178NewAPI(t)
		for _, c := range [][2]string{{"", ""}, {d178KeyA, "x.com"}} {
			_, err := fetchDevicesForList(c[0], c[1], true)
			if err == nil || !strings.Contains(err.Error(), "no accounts configured") || !strings.Contains(err.Error(), "ts-cli login") {
				t.Fatalf("err = %v", err)
			}
		}
		if len(api.seen("x.com")) != 0 {
			t.Fatal("no request may be made")
		}
	})
}

// Verifies: REQ-LST-019
func TestDO178CFetchDevicesSources(t *testing.T) {
	d178SaveConfig(t, d178TwoAccounts)
	api := d178NewAPI(t)
	api.devices["one.example.com"] = []client.Device{lstDev("alpha", nil)}
	api.devices["two.example.com"] = []client.Device{lstDev("beta", nil)}
	api.devices["x.example.com"] = []client.Device{lstDev("xray", nil)}
	t.Setenv("PATH", t.TempDir())

	t.Run("stored accounts: merged and tagged with the account name", func(t *testing.T) {
		devs, err := fetchDevicesForList("", "", true)
		if err != nil || len(devs) != 2 {
			t.Fatalf("err=%v devs=%d", err, len(devs))
		}
		byHost := map[string]client.Device{}
		for _, d := range devs {
			byHost[d.Hostname] = d
		}
		if byHost["alpha"].AccountName != "one" || byHost["alpha"].AccountTailnet != "one.example.com" || byHost["beta"].AccountName != "two" {
			t.Errorf("tagging: %+v", byHost)
		}
		if got := api.seen("one.example.com"); got[len(got)-1] != d178KeyA {
			t.Errorf("stored key not used: %q", got)
		}
	})
	t.Run("one failing account is only a warning", func(t *testing.T) {
		api.status["two.example.com"] = 500
		defer delete(api.status, "two.example.com")
		var devs []client.Device
		var err error
		_, errOut := d178Capture(t, func() { devs, err = fetchDevicesForList("", "", true) })
		if err != nil || len(devs) != 1 || devs[0].Hostname != "alpha" || !strings.Contains(errOut, "two") {
			t.Fatalf("err=%v devs=%v stderr=%q", err, lstHosts(devs), errOut)
		}
	})
	t.Run("explicit credentials replace the stored accounts", func(t *testing.T) {
		devs, err := fetchDevicesForList("tskey-explicit-0123456789abcdef", "x.example.com", true)
		if err != nil || len(devs) != 1 || devs[0].Hostname != "xray" || devs[0].AccountName != "x.example.com" || devs[0].AccountTailnet != "x.example.com" {
			t.Fatalf("err=%v devs=%+v", err, devs)
		}
		if got := api.seen("x.example.com"); len(got) != 1 || got[0] != "tskey-explicit-0123456789abcdef" {
			t.Errorf("tokens = %q", got)
		}
	})
	t.Run("explicit credentials rejected by the API", func(t *testing.T) {
		api.status["x.example.com"] = 401
		defer delete(api.status, "x.example.com")
		_, err := fetchDevicesForList("tskey-explicit-0123456789abcdef", "x.example.com", true)
		if err == nil || !strings.Contains(err.Error(), "failed to list devices") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("only one of key and tailnet: stored accounts are used", func(t *testing.T) {
		for _, c := range [][2]string{{"tskey-explicit-0123456789abcdef", ""}, {"", "x.example.com"}} {
			devs, err := fetchDevicesForList(c[0], c[1], true)
			if err != nil || len(devs) != 2 {
				t.Fatalf("%q: err=%v devs=%v", c, err, lstHosts(devs))
			}
		}
	})
}

// Verifies: REQ-LST-020
func TestDO178CFetchDevicesWarnsAboutTailscaleOnlyWhenNotQuiet(t *testing.T) {
	d178SaveConfig(t, d178OneAccount)
	api := d178NewAPI(t)
	api.devices["work.example.com"] = []client.Device{lstDev("alpha", nil)}
	t.Setenv("PATH", t.TempDir()) // tailscale missing = not running
	d178SetGoos(t, "windows")     // cannot be started automatically: no wait, nothing executed

	out, _ := d178Capture(t, func() { _, _ = fetchDevicesForList("", "", false) })
	if !strings.Contains(out, "Warning: Tailscale doesn't appear to be running.") {
		t.Fatalf("warning expected:\n%s", out)
	}
	var devs []client.Device
	out, _ = d178Capture(t, func() { devs, _ = fetchDevicesForList("", "", true) })
	if out != "" || len(devs) != 1 {
		t.Fatalf("quiet must print nothing: %q (%d devices)", out, len(devs))
	}
}

// Verifies: REQ-LST-021
func TestDO178CListCommandEndToEnd(t *testing.T) {
	d178SaveConfig(t, d178TwoAccounts)
	api := d178NewAPI(t)
	api.devices["one.example.com"] = []client.Device{lstDev("alpha", func(d *client.Device) { d.ConnectedToControl = true })}
	api.devices["two.example.com"] = []client.Device{lstDev("beta", func(d *client.Device) { d.LastSeen = time.Now().Add(-3 * time.Hour) })}
	t.Setenv("PATH", t.TempDir())

	out, _, err := d178Run(t, NewListCommand(), "", "--format", "names", "--offline")
	if err != nil || out != "beta\n" {
		t.Fatalf("err=%v out=%q", err, out)
	}
	out, _, err = d178Run(t, NewListCommand(), "", "--format", "names", "--account", "one")
	if err != nil || out != "alpha\n" {
		t.Fatalf("err=%v out=%q", err, out)
	}
	// metadata
	cmd := NewListCommand()
	want := map[string]string{"api-key": "", "tailnet": "", "format": "table", "quiet": "false", "online": "false", "offline": "false", "os": "", "name": "", "sort": "name"}
	for n, def := range want {
		if f := cmd.Flags().Lookup(n); f == nil || f.DefValue != def {
			t.Errorf("flag %s: %+v", n, f)
		}
	}
	if cmd.Flags().ShorthandLookup("q") == nil || cmd.Use != "list" {
		t.Error("list command metadata")
	}
}
