package tui

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yatsenko-ihor/ts-cli/client"
)

// Verifies: REQ-PNG-001
func TestParsePingOutput(t *testing.T) {
	cases := []struct {
		name         string
		out          string
		ok, direct   bool
		via          string
		pongs, first int
		latency      time.Duration
	}{
		{"relay only", "pong from box (100.64.0.5) via DERP(fra) in 45ms\n", true, false, "fra", 1, 0, 45 * time.Millisecond},
		{"direct", "pong from box (100.64.0.5) via 192.168.1.20:41641 in 3ms\n", true, true, "192.168.1.20:41641", 1, 1, 3 * time.Millisecond},
		{"direct after relay", "pong from b (100.1.1.1) via DERP(ams) in 50ms\npong from b (100.1.1.1) via DERP(ams) in 48ms\npong from b (100.1.1.1) via [2001:db8::1]:41641 in 900µs\n", true, true, "[2001:db8::1]:41641", 3, 3, 900 * time.Microsecond},
		{"relay after direct is relay", "pong from b (1.1.1.1) via 10.0.0.2:41641 in 2ms\npong from b (1.1.1.1) via DERP(fra) in 40ms\n", true, false, "fra", 2, 1, 40 * time.Millisecond},
		{"no pong", "no reply\nping \"100.64.0.9\" timed out\n", false, false, "", 0, 0, 0},
		{"garbage latency ignored", "pong from b (1.1.1.1) via DERP(x) in soon\n", false, false, "", 0, 0, 0},
		{"empty", "", false, false, "", 0, 0, 0},
		{"windows line endings", "pong from b (1.1.1.1) via DERP(fra) in 5ms\r\n", true, false, "fra", 1, 0, 5 * time.Millisecond},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := parsePingOutput(c.out)
			if d.ok != c.ok || d.direct != c.direct || d.via != c.via || d.pongs != c.pongs || d.directAt != c.first || d.latency != c.latency {
				t.Fatalf("got %+v", d)
			}
		})
	}
}

// Verifies: REQ-PNG-002
func TestPingDetailText(t *testing.T) {
	cases := []struct {
		d    pingDetail
		want string
	}{
		{pingDetail{err: "boom"}, "no reply: boom"},
		{pingDetail{}, "no reply"},
		{pingDetail{ok: true, direct: true, via: "1.2.3.4:5", latency: 3 * time.Millisecond, directAt: 1}, "direct 1.2.3.4:5 · 3ms"},
		{pingDetail{ok: true, direct: true, via: "1.2.3.4:5", latency: 3 * time.Millisecond, directAt: 3}, "direct 1.2.3.4:5 · 3ms (after 2 pings via relay)"},
		{pingDetail{ok: true, via: "fra", latency: 45 * time.Millisecond}, "relay fra · 45ms (no direct path yet)"},
		{pingDetail{ok: true, via: "fra", latency: 300 * time.Microsecond}, "relay fra · <1ms (no direct path yet)"},
	}
	for _, c := range cases {
		if got := c.d.text(); got != c.want {
			t.Errorf("text() = %q, want %q", got, c.want)
		}
	}
}

func stubPingN(t *testing.T, fn func(addr string, count int) ([]byte, error)) {
	t.Helper()
	old := tailscalePingN
	tailscalePingN = func(_ context.Context, addr string, count int) ([]byte, error) { return fn(addr, count) }
	t.Cleanup(func() { tailscalePingN = old })
}

// Verifies: REQ-PNG-003
func TestPingDetailCmd(t *testing.T) {
	dev := client.Device{ID: "d1", Name: "box.example.com", Addresses: []string{"100.64.0.5"}}
	run := func(d client.Device) pingDetailMsg { return pingDetailCmd(d)().(pingDetailMsg) }

	stubPingN(t, func(addr string, count int) ([]byte, error) {
		if addr != "100.64.0.5" || count != pingDetailCount {
			t.Fatalf("addr %q count %d", addr, count)
		}
		return []byte("pong from box (100.64.0.5) via DERP(fra) in 45ms\n"), nil
	})
	if m := run(dev); !m.detail.ok || m.key != "d1" || m.name != "box.example.com" || m.detail.at.IsZero() {
		t.Fatalf("msg = %+v", m)
	}

	// no address / invalid address: never reaches the CLI
	called := false
	stubPingN(t, func(string, int) ([]byte, error) { called = true; return nil, nil })
	if m := run(client.Device{ID: "x"}); m.detail.err == "" {
		t.Fatal("no address must fail")
	}
	if m := run(client.Device{ID: "x", Addresses: []string{"1.2.3.4;rm"}}); m.detail.err == "" {
		t.Fatal("invalid address must fail")
	}
	if called {
		t.Fatal("the CLI must not run for a bad address")
	}

	// no pong: CLI missing / failure with output / failure without output / clean exit
	stubPingN(t, func(string, int) ([]byte, error) { return nil, &exec.Error{Name: "tailscale", Err: exec.ErrNotFound} })
	if m := run(dev); !strings.Contains(m.detail.err, "not found") {
		t.Fatalf("err = %q", m.detail.err)
	}
	stubPingN(t, func(string, int) ([]byte, error) { return []byte("no reply\nsecond line\n"), errors.New("exit 1") })
	if m := run(dev); m.detail.err != "no reply" {
		t.Fatalf("err = %q", m.detail.err)
	}
	stubPingN(t, func(string, int) ([]byte, error) { return nil, errors.New("exit 1") })
	if m := run(dev); m.detail.err != "exit 1" {
		t.Fatalf("err = %q", m.detail.err)
	}
	stubPingN(t, func(string, int) ([]byte, error) { return []byte("nothing useful"), nil })
	if m := run(dev); m.detail.ok || m.detail.err != "" {
		t.Fatalf("unparseable output with a clean exit is just 'no reply': %+v", m.detail)
	}
}

// Verifies: REQ-PNG-004
func TestParsePeersIndexesByEveryAddress(t *testing.T) {
	data := []byte(`{"Peer":{
	 "nodekey:a":{"TailscaleIPs":["100.64.0.5","fd7a::5"],"Online":true,"Active":true,"CurAddr":"192.168.1.20:41641","RxBytes":1500000,"TxBytes":2000,"LastHandshake":"2026-10-20T10:00:00Z","SSHHostKeys":["ssh-ed25519 AAA"]},
	 "nodekey:b":{"TailscaleIPs":["100.64.0.6"],"Online":true,"Relay":"fra","ExitNode":true},
	 "nodekey:c":{"TailscaleIPs":["100.64.0.7"],"Online":true},
	 "nodekey:d":{"TailscaleIPs":["100.64.0.8"],"Online":false}}}`)
	peers, err := parsePeers(data)
	if err != nil {
		t.Fatal(err)
	}
	a := peers["100.64.0.5"]
	if !a.ssh || !a.online || !a.active || a.rx != 1500000 || peers["fd7a::5"] != a {
		t.Fatalf("a = %+v", a)
	}
	if got := a.pathText(); got != "direct 192.168.1.20:41641" {
		t.Fatalf("path = %q", got)
	}
	if got := peers["100.64.0.6"].pathText(); got != "relay fra" || !peers["100.64.0.6"].exitNode {
		t.Fatalf("path = %q", got)
	}
	if got := peers["100.64.0.7"].pathText(); got != "idle (no traffic yet)" {
		t.Fatalf("path = %q", got)
	}
	if got := peers["100.64.0.8"].pathText(); got != "offline" {
		t.Fatalf("path = %q", got)
	}
	if _, err := parsePeers([]byte("{broken")); err == nil {
		t.Fatal("invalid JSON must be an error")
	}
	if p, err := parsePeers([]byte(`{}`)); err != nil || len(p) != 0 {
		t.Fatalf("no peers: %v %v", p, err)
	}
}

// Verifies: REQ-PNG-005
func TestTrafficTextAndHumanBytes(t *testing.T) {
	if (peerInfo{}).trafficText() != "" {
		t.Fatal("no traffic, no text")
	}
	if got := (peerInfo{rx: 1500000, tx: 2000}).trafficText(); got != "↓ 1.5 MB  ↑ 2.0 kB" {
		t.Fatalf("traffic = %q", got)
	}
	if got := (peerInfo{tx: 5}).trafficText(); got != "↓ 0 B  ↑ 5 B" {
		t.Fatalf("traffic = %q", got)
	}
	for n, want := range map[int64]string{0: "0 B", 999: "999 B", 1000: "1.0 kB", 1234567: "1.2 MB", 5e9: "5.0 GB", 3e12: "3.0 TB", 4e15: "4000.0 TB"} {
		if got := humanBytes(n); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", n, got, want)
		}
	}
}

// Verifies: REQ-PNG-006
func TestLoadPeersAndPeerFor(t *testing.T) {
	old := tailscaleStatusJSON
	t.Cleanup(func() { tailscaleStatusJSON = old })

	tailscaleStatusJSON = func() ([]byte, error) {
		return []byte(`{"Peer":{"k":{"TailscaleIPs":["100.64.0.5"],"Online":true,"Relay":"ams"}}}`), nil
	}
	msg := loadPeersCmd()().(peersMsg)
	if msg.err != nil || msg.peers["100.64.0.5"].relay != "ams" {
		t.Fatalf("msg = %+v", msg)
	}
	m := model{peers: msg.peers}
	if p, ok := m.peerFor(client.Device{Addresses: []string{"100.64.0.5"}}); !ok || p.relay != "ams" {
		t.Fatal("peerFor must find the device by its first address")
	}
	if _, ok := m.peerFor(client.Device{Addresses: []string{"100.64.9.9"}}); ok {
		t.Fatal("unknown address")
	}
	if _, ok := m.peerFor(client.Device{}); ok {
		t.Fatal("no address")
	}

	tailscaleStatusJSON = func() ([]byte, error) { return nil, errors.New("not running") }
	if msg := loadPeersCmd()().(peersMsg); msg.err == nil {
		t.Fatal("a failing CLI must be reported")
	}
	tailscaleStatusJSON = func() ([]byte, error) { return []byte("junk"), nil }
	if msg := loadPeersCmd()().(peersMsg); msg.err == nil {
		t.Fatal("bad JSON must be reported")
	}
}

// Verifies: REQ-PNG-003, REQ-PNG-006
func TestDefaultSeamsRunTheTailscaleCLI(t *testing.T) {
	dir := useFakeBins(t, fakeBins{})
	script := "#!/bin/sh\nprintf '%s ' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "tailscale"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := tailscalePingN(context.Background(), "100.64.0.5", 3)
	if err != nil || strings.TrimSpace(string(out)) != "ping --c 3 --timeout 3s 100.64.0.5" {
		t.Fatalf("ping args = %q, %v", out, err)
	}
	out, err = tailscaleStatusJSON()
	if err != nil || strings.TrimSpace(string(out)) != "status --json" {
		t.Fatalf("status args = %q, %v", out, err)
	}
}
