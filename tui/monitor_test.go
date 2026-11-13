package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yatsenko-ihor/ts-cli/client"
)

func samples(ok bool, ms ...int) []sample {
	var out []sample
	for _, v := range ms {
		out = append(out, sample{at: time.Now(), lat: time.Duration(v) * time.Millisecond, ok: ok && v >= 0})
	}
	return out
}

func TestSparklineAndLatencyText(t *testing.T) {
	if got := sparkline(samples(true, 10, 20, 40), 10); got != "▂▄█" {
		t.Fatalf("sparkline = %q", got)
	}
	if got := sparkline([]sample{{ok: true}, {ok: false}}, 10); got != "▁×" {
		t.Fatalf("zero latency / failure = %q", got)
	}
	if got := sparkline(samples(true, 1, 2, 3, 4), 2); len([]rune(got)) != 2 {
		t.Fatalf("only the last n: %q", got)
	}
	if latencyText(nil) != "" {
		t.Fatal("empty history")
	}
	text := latencyText(samples(true, 4, 8, 12))
	for _, want := range []string{"12ms", "min 4", "avg 8", "max 12"} {
		if !strings.Contains(text, want) {
			t.Errorf("%q missing in %q", want, text)
		}
	}
	mixed := latencyText(samples(true, 5, -1, 15))
	if !strings.Contains(mixed, "1 of 3 failed") || !strings.Contains(mixed, "×") {
		t.Fatalf("mixed = %q", mixed)
	}
	if got := latencyText(samples(false, -1, -1)); !strings.Contains(got, "no reply (2 tries)") {
		t.Fatalf("all failed = %q", got)
	}
}

func TestRecordSampleKeepsTheLastOnes(t *testing.T) {
	m := model{}
	for i := 0; i < maxSamples+5; i++ {
		m.recordSample("k", true, time.Duration(i))
	}
	s := m.samples["k"]
	if len(s) != maxSamples || s[0].lat != 5 || s[len(s)-1].lat != time.Duration(maxSamples+4) {
		t.Fatalf("len %d first %v", len(s), s[0].lat)
	}
}

func TestMonitorAlerts(t *testing.T) {
	m := model{}
	fail := pingMsg{key: "k", name: "box.tail1.ts.net"}
	ok := pingMsg{key: "k", name: "box.tail1.ts.net", ok: true}

	m.noteMonitorResult(fail, true)
	if len(m.alerts) != 0 {
		t.Fatal("one failure is not an alert")
	}
	m.noteMonitorResult(fail, true)
	if len(m.alerts) != 1 || !strings.Contains(m.notify.info, "box stopped answering") {
		t.Fatalf("alerts = %v", m.alerts)
	}
	m.noteMonitorResult(fail, true)
	if len(m.alerts) != 1 {
		t.Fatal("one alert per outage")
	}
	m.noteMonitorResult(ok, false)
	if len(m.alerts) != 2 || !strings.Contains(m.notify.info, "answering again") {
		t.Fatalf("alerts = %v", m.alerts)
	}
	m.noteMonitorResult(ok, true)
	if len(m.alerts) != 2 {
		t.Fatal("no alert when nothing was reported down")
	}
	// A machine that was already unreachable does not raise an alert.
	m2 := model{}
	m2.noteMonitorResult(fail, false)
	m2.noteMonitorResult(fail, false)
	if len(m2.alerts) != 0 {
		t.Fatal("unreachable machines are not reported")
	}
	// The log is capped.
	for i := 0; i < maxAlerts+3; i++ {
		m2.pushAlert("x")
	}
	if len(m2.alerts) != maxAlerts {
		t.Fatalf("alerts %d", len(m2.alerts))
	}
}

func monitorModel(t *testing.T, n int) model {
	t.Helper()
	old := tailscaleAvailable
	tailscaleAvailable = func() bool { return true }
	t.Cleanup(func() { tailscaleAvailable = old })
	var devs []client.Device
	for i := 0; i < n; i++ {
		id := string(rune('a' + i))
		devs = append(devs, client.Device{ID: id, Name: id, Hostname: id, Addresses: []string{"100.64.0.1"}, ConnectedToControl: true})
	}
	m := model{list: deviceList{devices: devs, filteredDevices: devs}, set: uiSettings{monitor: true}}
	return m
}

func TestMonitorRoundsAndSettings(t *testing.T) {
	m := monitorModel(t, 8)
	m, cmd := m.startMonitor()
	if cmd == nil || m.monitorSeq != 1 {
		t.Fatal("on: a tick is scheduled")
	}
	if _, c := m.handleMonitorTick(monitorTickMsg{seq: 7}); c != nil {
		t.Fatal("stale tick")
	}
	u, c := m.handleMonitorTick(monitorTickMsg{seq: 1})
	m = u.(model)
	if c == nil || len(m.pinging) != monitorBatch || m.monitorNext != monitorBatch {
		t.Fatalf("round 1: pinging %d next %d", len(m.pinging), m.monitorNext)
	}
	// Machines in flight are not pinged twice; the rest follow, wrapping around.
	u, _ = m.handleMonitorTick(monitorTickMsg{seq: 1})
	m = u.(model)
	if len(m.pinging) != 8 {
		t.Fatalf("round 2: pinging %d", len(m.pinging))
	}
	// A fresh result, an offline machine and one that cannot be pinged are skipped.
	m2 := monitorModel(t, 3)
	m2.monitorSeq = 1
	m2.pingAt = map[string]time.Time{"a": time.Now()}
	m2.list.filteredDevices[1].ConnectedToControl = false
	m2.list.filteredDevices[1].LastSeen = time.Now().Add(-time.Hour)
	m2.list.filteredDevices[2].Addresses = nil
	u, _ = m2.handleMonitorTick(monitorTickMsg{seq: 1})
	if len(u.(model).pinging) != 0 {
		t.Fatalf("pinging %v", u.(model).pinging)
	}
	// ...but an offline machine being watched is pinged.
	m2.downAlerted = map[string]bool{"b": true}
	u, _ = m2.handleMonitorTick(monitorTickMsg{seq: 1})
	if !u.(model).pinging["b"] {
		t.Fatal("a machine reported down keeps being checked")
	}
	// Empty list: nothing to do, the loop continues.
	m3 := monitorModel(t, 0)
	m3.monitorSeq = 1
	if _, c := m3.handleMonitorTick(monitorTickMsg{seq: 1}); c == nil {
		t.Fatal("the loop goes on")
	}

	// Off: no tick, and old ticks die.
	off := m
	off.set.monitor = false
	off, c = off.startMonitor()
	if c != nil || off.monitorSeq != 2 {
		t.Fatal("off")
	}
	if _, c := off.handleMonitorTick(monitorTickMsg{seq: 1}); c != nil {
		t.Fatal("a disabled monitor ignores ticks")
	}
}

func TestMonitorSettingToggle(t *testing.T) {
	t.Setenv("TS_CLI_CONFIG_DIR", t.TempDir())
	m := monitorModel(t, 1)
	m.set.monitor = false
	if loadUISettings(map[string]string{uiMonitor: "on"}).monitor != true || loadUISettings(nil).monitor {
		t.Fatal("default off")
	}
	u, cmd := m.applySetting(uiMonitor)
	m = u.(model)
	if !m.set.monitor || m.set.value(uiMonitor) != "on" || cmd == nil || m.monitorSeq != 1 {
		t.Fatal("toggled on")
	}
	if m.Init() == nil {
		t.Fatal("Init starts the loop")
	}
}

func TestMonitorPingResultFeedsHistoryAndAlerts(t *testing.T) {
	m := monitorModel(t, 1)
	m.pinging = map[string]bool{"a": true}
	for i := 0; i < alertAfterFails; i++ {
		u, _ := m.handlePing(pingMsg{key: "a", name: "a", mode: pingMonitor, err: errors.New("no reply")})
		m = u.(model)
	}
	if len(m.samples["a"]) != alertAfterFails || len(m.alerts) != 1 || m.ping["a"] != pingFail {
		t.Fatalf("samples %d alerts %v ping %v", len(m.samples["a"]), m.alerts, m.ping["a"])
	}
	u, _ := m.handlePing(pingMsg{key: "a", name: "a", mode: pingMonitor, ok: true, latency: 12 * time.Millisecond})
	m = u.(model)
	if len(m.alerts) != 2 || m.ping["a"] != pingOK || m.samples["a"][len(m.samples["a"])-1].lat != 12*time.Millisecond {
		t.Fatalf("recovered: %v", m.alerts)
	}
	if text := m.renderDetailsPanel(70, 0); !strings.Contains(text, "Latency") {
		t.Fatalf("details:\n%s", text)
	}
}

func TestPingDeviceReportsTheLatencyOfThePong(t *testing.T) {
	old := tailscalePing
	t.Cleanup(func() { tailscalePing = old })
	d := client.Device{ID: "a", Name: "a", Addresses: []string{"100.64.0.1"}}
	tailscalePing = func(_ context.Context, _ string, _ time.Duration) ([]byte, error) {
		return []byte("pong from a (100.64.0.1) via DERP(fra) in 45ms\n"), nil
	}
	if msg := pingDevice(d, pingManual, time.Second)().(pingMsg); msg.latency != 45*time.Millisecond {
		t.Fatalf("latency = %v", msg.latency)
	}
	tailscalePing = func(_ context.Context, _ string, _ time.Duration) ([]byte, error) { return []byte("pong"), nil }
	if msg := pingDevice(d, pingManual, time.Second)().(pingMsg); msg.latency != msg.took {
		t.Fatalf("falls back to the wall time: %v vs %v", msg.latency, msg.took)
	}
}
