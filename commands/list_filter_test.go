package commands

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yatsenko-ihor/ts-cli/client"
)

var filterNow = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

func fixtureDevices() []client.Device {
	return []client.Device{
		{Name: "Zeta.example", Hostname: "zeta", OS: "linux", AccountName: "work", Tags: []string{"tag:server", "tag:prod"}, ConnectedToControl: true},
		{Name: "alpha.example", Hostname: "alpha", OS: "macOS", AccountName: "home", LastSeen: filterNow.Add(-2 * time.Minute)},
		{Name: "beta.example", Hostname: "beta", OS: "Linux", AccountName: "Work", Tags: []string{"tag:server"}, LastSeen: filterNow.Add(-3 * time.Hour)},
		{Name: "gamma.example", Hostname: "gamma", OS: "windows", AccountName: "home", LastSeen: filterNow.Add(-24 * time.Hour)},
		{Name: "delta.example", Hostname: "pi-delta", OS: "iOS"},
	}
}

func names(ds []client.Device) []string {
	var out []string
	for _, d := range ds {
		out = append(out, d.Hostname)
	}
	return out
}

func TestDeviceOnline(t *testing.T) {
	tests := []struct {
		name string
		d    client.Device
		want bool
	}{
		{"connected zero lastSeen", client.Device{ConnectedToControl: true}, true},
		{"never seen", client.Device{}, false},
		{"just under 5m", client.Device{LastSeen: filterNow.Add(-4*time.Minute - 59*time.Second)}, true},
		{"exactly 5m", client.Device{LastSeen: filterNow.Add(-5 * time.Minute)}, true},
		{"just over 5m", client.Device{LastSeen: filterNow.Add(-5*time.Minute - time.Second)}, false},
		{"future lastSeen", client.Device{LastSeen: filterNow.Add(time.Minute)}, true},
	}
	for _, tt := range tests {
		if got := deviceOnline(tt.d, filterNow); got != tt.want {
			t.Errorf("%s: got %v want %v", tt.name, got, tt.want)
		}
	}
}

func TestFilterDevices(t *testing.T) {
	tests := []struct {
		name string
		f    deviceFilter
		want []string
	}{
		{"none", deviceFilter{}, []string{"alpha", "beta", "pi-delta", "gamma", "zeta"}},
		{"online", deviceFilter{Online: true}, []string{"alpha", "zeta"}},
		{"offline", deviceFilter{Offline: true}, []string{"beta", "pi-delta", "gamma"}},
		{"os case-insensitive", deviceFilter{OS: "LINUX"}, []string{"beta", "zeta"}},
		{"os exact not substring", deviceFilter{OS: "mac"}, nil},
		{"tag with prefix", deviceFilter{Tags: []string{"tag:server"}}, []string{"beta", "zeta"}},
		{"tag without prefix", deviceFilter{Tags: []string{"server"}}, []string{"beta", "zeta"}},
		{"tag mixed case", deviceFilter{Tags: []string{"TAG:Prod"}}, []string{"zeta"}},
		{"tags all must match", deviceFilter{Tags: []string{"server", "tag:prod"}}, []string{"zeta"}},
		{"tags none match", deviceFilter{Tags: []string{"server", "nope"}}, nil},
		{"account", deviceFilter{Accounts: []string{"WORK"}}, []string{"beta", "zeta"}},
		{"account any of", deviceFilter{Accounts: []string{"work", "home"}}, []string{"alpha", "beta", "gamma", "zeta"}},
		{"name matches Name", deviceFilter{Name: "GAMMA.ex"}, []string{"gamma"}},
		{"name matches Hostname", deviceFilter{Name: "pi-"}, []string{"pi-delta"}},
		{"online+os", deviceFilter{Online: true, OS: "linux"}, []string{"zeta"}},
		{"offline+account+tag", deviceFilter{Offline: true, Accounts: []string{"work"}, Tags: []string{"server"}}, []string{"beta"}},
		{"all filters no match", deviceFilter{Online: true, OS: "windows"}, nil},
		{"sort os", deviceFilter{Sort: "os"}, []string{"pi-delta", "beta", "zeta", "alpha", "gamma"}},
		{"sort account", deviceFilter{Sort: "ACCOUNT"}, []string{"pi-delta", "alpha", "gamma", "beta", "zeta"}},
		{"sort lastseen", deviceFilter{Sort: "lastseen"}, []string{"zeta", "alpha", "beta", "gamma", "pi-delta"}},
	}
	for _, tt := range tests {
		in := fixtureDevices()
		got := names(filterDevices(in, tt.f, filterNow))
		if len(tt.want) == 0 && len(got) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: got %v want %v", tt.name, got, tt.want)
		}
		if !reflect.DeepEqual(names(in), names(fixtureDevices())) {
			t.Errorf("%s: input slice was modified", tt.name)
		}
	}
}

func TestFilterDevicesEmptyIsNotNil(t *testing.T) {
	if got := filterDevices(nil, deviceFilter{}, filterNow); got == nil {
		t.Fatal("result must be non-nil")
	}
}

func TestFilterDevicesSortIsStable(t *testing.T) {
	in := []client.Device{
		{ID: "1", Name: "same", OS: "linux"},
		{ID: "2", Name: "SAME", OS: "linux"},
		{ID: "3", Name: "same", OS: "linux"},
	}
	got := filterDevices(in, deviceFilter{}, filterNow)
	if got[0].ID != "1" || got[1].ID != "2" || got[2].ID != "3" {
		t.Fatalf("not stable: %v %v %v", got[0].ID, got[1].ID, got[2].ID)
	}
}

func runList(t *testing.T, devs []client.Device, args ...string) (string, string, error) {
	t.Helper()
	old := listDevicesFn
	listDevicesFn = func(string, string, bool) ([]client.Device, error) { return devs, nil }
	defer func() { listDevicesFn = old }()
	cmd := NewListCommand()
	var o, e bytes.Buffer
	cmd.SetOut(&o)
	cmd.SetErr(&e)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return o.String(), e.String(), err
}

func TestListCommandNames(t *testing.T) {
	out, _, err := runList(t, fixtureDevices(), "--tag", "tag:server", "--format", "names")
	if err != nil {
		t.Fatal(err)
	}
	if out != "beta\nzeta\n" {
		t.Fatalf("got %q", out)
	}
	// Hostname empty falls back to Name.
	out, _, _ = runList(t, []client.Device{{Name: "only-name"}}, "--format", "names")
	if out != "only-name\n" {
		t.Fatalf("got %q", out)
	}
}

func TestListCommandRepeatedFlags(t *testing.T) {
	out, _, err := runList(t, fixtureDevices(), "--account", "home", "--account", "work", "--tag", "server", "--tag", "prod", "-q", "--format", "names")
	if err != nil {
		t.Fatal(err)
	}
	if out != "zeta\n" {
		t.Fatalf("got %q", out)
	}
}

func TestListCommandTableTagsColumnAndQuiet(t *testing.T) {
	out, _, err := runList(t, fixtureDevices(), "--sort", "name")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "TAGS") || !strings.Contains(out, "Total devices: 5") {
		t.Fatalf("unexpected table:\n%s", out)
	}
	out, _, _ = runList(t, fixtureDevices(), "--quiet")
	if strings.Contains(out, "ACCOUNT") || strings.Contains(out, "Total") || strings.Count(out, "\n") != 5 {
		t.Fatalf("quiet table wrong:\n%s", out)
	}
	// no tags anywhere: no TAGS column
	out, _, _ = runList(t, []client.Device{{Name: "a", Hostname: "a"}})
	if strings.Contains(out, "TAGS") {
		t.Fatalf("TAGS column should be hidden:\n%s", out)
	}
}

func TestListCommandNoMatch(t *testing.T) {
	out, errOut, err := runList(t, fixtureDevices(), "--os", "plan9")
	if err != nil || out != "" || strings.TrimSpace(errOut) != "No devices match the filters." {
		t.Fatalf("table: out=%q err=%q error=%v", out, errOut, err)
	}
	out, errOut, err = runList(t, fixtureDevices(), "--os", "plan9", "--format", "names")
	if err != nil || out != "" || !strings.Contains(errOut, "No devices match") {
		t.Fatalf("names: out=%q err=%q error=%v", out, errOut, err)
	}
}

func TestListCommandJSON(t *testing.T) {
	out, _, err := runList(t, fixtureDevices(), "--os", "plan9", "--format", "json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "[]" {
		t.Fatalf("empty result must be [], got %q", out)
	}
	out, _, _ = runList(t, nil, "--format", "json")
	if strings.TrimSpace(out) != "[]" {
		t.Fatalf("no devices must be [], got %q", out)
	}

	out, _, err = runList(t, fixtureDevices(), "--account", "work", "--format", "JSON")
	if err != nil {
		t.Fatal(err)
	}
	var dec []map[string]any
	if err := json.Unmarshal([]byte(out), &dec); err != nil {
		t.Fatal(err)
	}
	if len(dec) != 2 || dec[0]["hostname"] != "beta" || dec[1]["online"] != true || dec[0]["online"] != false {
		t.Fatalf("unexpected: %v", dec)
	}
	if tags, ok := dec[1]["tags"].([]any); !ok || len(tags) != 2 {
		t.Fatalf("tags: %v", dec[1]["tags"])
	}
	out, _, _ = runList(t, []client.Device{{Name: "x"}}, "--format", "json")
	if !strings.Contains(out, `"tags": []`) {
		t.Fatalf("tags must be [] when none:\n%s", out)
	}
}

func TestListCommandErrors(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"--online", "--offline"}, "mutually exclusive"},
		{[]string{"--sort", "bogus"}, "unknown sort"},
		{[]string{"--format", "xml"}, "unknown format"},
	}
	for _, tt := range tests {
		_, _, err := runList(t, fixtureDevices(), tt.args...)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%v: got %v want %q", tt.args, err, tt.want)
		}
	}
}
