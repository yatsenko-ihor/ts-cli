package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/yatsenko-ihor/ts-cli/client"
)

// Verifies: REQ-DKW-001
func TestDeviceKeyState(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	cases := []struct {
		name          string
		d             client.Device
		expired, soon bool
	}{
		{"no expiry date", client.Device{}, false, false},
		{"expiry disabled", client.Device{Expires: now.Add(-day), KeyExpiryDisabled: true}, false, false},
		{"expired", client.Device{Expires: now.Add(-time.Second)}, true, false},
		{"expires exactly now", client.Device{Expires: now}, true, false},
		{"expires tomorrow", client.Device{Expires: now.Add(day)}, false, true},
		{"expires in 13d 23h", client.Device{Expires: now.Add(14*day - time.Hour)}, false, true},
		{"expires in exactly 14d", client.Device{Expires: now.Add(14 * day)}, false, false},
		{"expires in 90d", client.Device{Expires: now.Add(90 * day)}, false, false},
	}
	for _, c := range cases {
		e, s := deviceKeyState(c.d, now)
		if e != c.expired || s != c.soon {
			t.Errorf("%s: got expired=%v soon=%v", c.name, e, s)
		}
	}
}

// Verifies: REQ-DKW-002
func TestDeviceKeyWarningLine(t *testing.T) {
	now := time.Now()
	day := 24 * time.Hour
	devs := []client.Device{
		{ID: "1", Name: "ok.ts.net", AccountName: "a", Expires: now.Add(60 * day)},
		{ID: "2", Name: "pi4.tail1.ts.net", AccountName: "a", Expires: now.Add(2*day + time.Hour)},
		{ID: "3", Name: "old.ts.net", AccountName: "a", Expires: now.Add(-3 * day)},
		{ID: "4", Name: "never.ts.net", AccountName: "a", Expires: now.Add(-day), KeyExpiryDisabled: true},
		{ID: "5", Name: "hidden.ts.net", AccountName: "h", Expires: now.Add(-day)},
	}
	m := model{list: deviceList{devices: devs}, acct: accounts{list: []client.AccountInfo{{Name: "a"}, {Name: "h", Hidden: true}}}}
	got := m.deviceKeyWarning(now)
	for _, want := range []string{"1 expired", "1 expire within 14 days", "soonest: old", "expired 3 days ago"} {
		if !strings.Contains(got, want) {
			t.Errorf("warning lacks %q: %s", want, got)
		}
	}
	if strings.Contains(got, "hidden") || strings.Contains(got, "2 expired") {
		t.Errorf("hidden accounts and disabled expiry must not count: %s", got)
	}

	// only upcoming: names the soonest with days
	m.list.devices = devs[:2]
	if got := m.deviceKeyWarning(now); !strings.Contains(got, "soonest: pi4 in 2d") || strings.Contains(got, "expired,") {
		t.Errorf("warning = %s", got)
	}
	// nothing to warn about
	m.list.devices = devs[:1]
	if m.deviceKeyWarning(now) != "" {
		t.Error("no warning expected")
	}
}

// Verifies: REQ-DKW-003
func TestListFlagShowsExpiringKeysInWarningColour(t *testing.T) {
	now := time.Now()
	soon := client.Device{ID: "1", Name: "a", Expires: now.Add(24 * time.Hour)}
	expired := client.Device{ID: "2", Name: "b", Expires: now.Add(-time.Hour)}
	fine := client.Device{ID: "3", Name: "c", Expires: now.Add(90 * 24 * time.Hour)}

	m := model{ssh: ssh{machineCreds: map[string]machineCredential{}}}
	for _, d := range []client.Device{soon, expired} {
		if !strings.Contains(m.styledFlags(d), "exp") {
			t.Errorf("%s: the symbol style must flag %s", d.Name, d.Name)
		}
	}
	if strings.Contains(m.styledFlags(fine), "exp") {
		t.Error("a key far from expiry is not flagged")
	}
	if m.styledFlags(soon) == m.styledFlags(expired) && strings.Contains(m.styledFlags(soon), "\x1b[") {
		t.Error("soon and expired should differ in colour")
	}

	m.set.emojiIcons = true
	if !strings.Contains(m.styledFlags(soon), "⏳") || !strings.Contains(m.styledFlags(expired), "🔒") || !strings.Contains(m.styledFlags(fine), "🔑") {
		t.Errorf("emoji flags: %q %q %q", m.styledFlags(soon), m.styledFlags(expired), m.styledFlags(fine))
	}
}
