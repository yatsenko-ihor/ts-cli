package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yatsenko-ihor/ts-cli/client"
)

const healthSample = `3 days, 4 hours
---
0.12 0.20 0.18 1/234 5678
---
/dev/root 15000000 5400000 9000000 38% /
---
Mem:           1895         412        1100          10         380        1400
`

func TestParseHealth(t *testing.T) {
	h := parseHealth("up " + healthSample)
	if h.uptime != "3 days, 4 hours" || h.load != "0.12 0.20 0.18" || h.diskUsed != "38% of 14.3 GB" || h.memUsed != "412 of 1895 MB (21%)" {
		t.Fatalf("%+v", h)
	}
	if got := h.summary(); got != "up 3 days, 4 hours · load 0.12 0.20 0.18 · mem 412 of 1895 MB (21%) · disk 38% of 14.3 GB" {
		t.Fatalf("summary = %q", got)
	}
	empty := parseHealth("")
	if empty != (healthInfo{}) || empty.summary() != "no data" {
		t.Fatalf("empty: %+v", empty)
	}
	odd := parseHealth("x\n---\nnot numbers\n---\nfs 1 2 3 4\n---\nMem: a b c")
	if odd.load != "" || odd.diskUsed != "" || odd.memUsed != "" || odd.uptime != "x" {
		t.Fatalf("odd: %+v", odd)
	}
	if parseHealth("---\n---\nfs x 1 2 3%\n---\nMem: 0 0 0").diskUsed != "" || parseHealth("---\n---\n---\nMem: 0 0 0").memUsed != "" {
		t.Fatal("bad numbers and zero memory are ignored")
	}
	for kb, want := range map[float64]string{512 * 1024: "512 MB", 2 * 1024 * 1024: "2.0 GB", 3 * 1024 * 1024 * 1024: "3.0 TB"} {
		if got := humanKB(kb); got != want {
			t.Errorf("humanKB(%v) = %q, want %q", kb, got, want)
		}
	}
}

func withHealthSend(t *testing.T, fn func(client.Device, string, string) (string, error)) {
	t.Helper()
	old := healthSend
	healthSend = fn
	t.Cleanup(func() { healthSend = old })
}

func TestCheckHealthFlow(t *testing.T) {
	var user string
	withHealthSend(t, func(d client.Device, u, p string) (string, error) { user = u; return healthSample, nil })
	m := flowModel(t, map[string]machineCredential{"dev-1": {username: "pi"}}, "pi")
	d := m.list.devices[0]

	u, cmd := m.startHealth(d)
	m = u.(model)
	if cmd == nil || !m.act.busy {
		t.Fatal("started")
	}
	msg := cmd().(healthMsg)
	if user != "pi" || msg.info.err != "" || msg.info.at.IsZero() {
		t.Fatalf("%q %+v", user, msg)
	}
	u, _ = m.handleHealth(msg)
	m = u.(model)
	if m.health["dev-1"].load == "" || !strings.Contains(m.act.result, "Output panel") {
		t.Fatalf("%+v %q", m.health, m.act.result)
	}
	// the facts are one per line in the Output panel
	if !m.run.visible || len(m.run.runs) != 1 || !strings.HasPrefix(m.run.label, "health ") {
		t.Fatalf("run = %+v", m.run)
	}
	out := strings.Join(m.run.runs[0].lines, "\n")
	for _, want := range []string{"laptop.example.com  ·  health at", "Uptime  3 days, 4 hours", "Load    0.12 0.20 0.18", "Memory  412 of 1895 MB (21%)", "Disk /  38% of 14.3 GB"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if got := len(m.outputLines()); got != 6 { // title, blank, four facts
		t.Fatalf("%d output lines", got)
	}
	// while a job owns the Output panel, the result stays a single line there
	busy := m
	busy.run.runs = []*machineRun{{status: runRunning, lines: []string{"job output"}}}
	u2, _ := busy.handleHealth(msg)
	if b := u2.(model); b.run.runs[0].lines[0] != "job output" || !strings.Contains(b.act.result, "load 0.12") {
		t.Fatalf("a running job keeps its output: %+v / %q", b.run.runs[0].lines, b.act.result)
	}
	// nothing readable
	var empty model = m
	empty.run = runPanel{}
	empty.showHealthOutput(m.list.devices[0], healthInfo{at: time.Now()})
	if l := empty.run.runs[0].lines; l[len(l)-1] != "The machine reported nothing readable." {
		t.Fatalf("empty: %v", l)
	}
	m.health["dev-1"] = healthInfo{at: time.Now(), uptime: "1 hour", load: "0.1 0.1 0.1", memUsed: "1 of 2 MB (50%)", diskUsed: "1% of 1 MB"}
	panel := m.renderDetailsPanel(70, 0)
	for _, want := range []string{"Uptime", "Load", "Memory", "Disk"} {
		if !strings.Contains(panel, want) {
			t.Errorf("details lack %q", want)
		}
	}

	// A failing command reports its first line and stores nothing.
	withHealthSend(t, func(client.Device, string, string) (string, error) { return "boom\nmore", errors.New("exit status 1") })
	_, cmd = m.startHealth(d)
	u, _ = m.handleHealth(cmd().(healthMsg))
	if !strings.Contains(u.(model).act.err.Error(), "failed: boom") {
		t.Fatalf("err = %v", u.(model).act.err)
	}
}

func TestCheckHealthRefusals(t *testing.T) {
	m := flowModel(t, map[string]machineCredential{"dev-1": {username: "pi"}}, "pi")
	d := m.list.devices[0]
	noAddr := d
	noAddr.Addresses = nil
	if u, c := m.startHealth(noAddr); c != nil || u.(model).act.err == nil {
		t.Fatal("no address")
	}
	other := d
	other.AccountTailnet = "other.example.com"
	m2 := m
	m2.acct.tailscaleActiveAccount = "mine.example.com"
	if u, c := m2.startHealth(other); c == nil || u.(model).pendingSwitch == nil {
		t.Fatal("another account is switched to by itself, not refused")
	}
	m3 := flowModel(t, nil, "")
	if u, c := m3.startHealth(m3.list.devices[0]); c != nil || !strings.Contains(u.(model).act.err.Error(), "username") {
		t.Fatal("no username")
	}
	if found := func() bool {
		for _, a := range machineActions(d) {
			if a.title == "Check health" {
				return true
			}
		}
		return false
	}(); !found {
		t.Fatal("the Machine tab offers it")
	}
}

func TestHealthSendRunsTheScriptOverSSH(t *testing.T) {
	dir := useFakeBins(t, fakeBins{ssh: true})
	t.Setenv("FAKE_SSH_OUT", "hello")
	d := flowModel(t, nil, "pi").list.devices[0]
	out, err := healthSend(d, "pi", "")
	if err != nil || strings.TrimSpace(out) != "hello" {
		t.Fatalf("%q %v", out, err)
	}
	argv := strings.Join(fakeArgv(t, dir, "ssh.argv"), " ")
	if !strings.Contains(argv, "pi@100.64.0.1") || !strings.Contains(argv, "/proc/loadavg") {
		t.Fatalf("argv = %q", argv)
	}
}
