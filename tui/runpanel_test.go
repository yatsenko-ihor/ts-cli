package tui

import (
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yatsenko-ihor/ts-cli/client"
	"github.com/yatsenko-ihor/ts-cli/config"
	"github.com/yatsenko-ihor/ts-cli/scripts"
	"github.com/yatsenko-ihor/ts-cli/util"
)

func TestPlaceholders(t *testing.T) {
	got := placeholders("tail -n {{lines}} {{ path }} && echo {{lines}}")
	if len(got) != 2 || got[0] != "lines" || got[1] != "path" {
		t.Fatalf("placeholders = %v", got)
	}
	filled := fillPlaceholders("tail -n {{lines}} {{ path }}", map[string]string{"lines": "50", "path": "/var/log/syslog"})
	if filled != "tail -n 50 /var/log/syslog" {
		t.Fatalf("fillPlaceholders = %q", filled)
	}
	if placeholders("no tokens here") != nil {
		t.Fatal("expected no placeholders")
	}
}

func TestRunPanelTabsAndCursors(t *testing.T) {
	m := model{
		run: runPanel{
			visible:  true,
			commands: []config.SavedCommand{{Name: "a", Command: "a"}, {Name: "b", Command: "b"}, {Name: "c", Command: "c"}},
		},
		activeFocus: focusRun,
	}
	key := func(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

	updated, _ := m.handleRunPanel(key("]"))
	m = updated.(model)
	if m.run.tab != tabCommands {
		t.Fatalf("expected Commands tab, got %v", m.run.tab)
	}
	for i := 0; i < 5; i++ {
		updated, _ = m.handleRunPanel(key("j"))
		m = updated.(model)
	}
	if m.tabCursor() != 2 {
		t.Fatalf("cursor should clamp at last row, got %d", m.tabCursor())
	}
	updated, _ = m.handleRunPanel(key("["))
	m = updated.(model)
	if m.run.tab != tabScripts || m.tabCursor() != 0 {
		t.Fatalf("expected Scripts tab with its own cursor, got %v/%d", m.run.tab, m.tabCursor())
	}
	updated, _ = m.handleRunPanel(key("["))
	m = updated.(model)
	if m.run.tab != tabRecent {
		t.Fatalf("tabs should wrap around, got %v", m.run.tab)
	}
}

func TestVisibleScriptsFilteredByOS(t *testing.T) {
	m := model{
		list: deviceList{filteredDevices: []client.Device{{ID: "1", OS: "windows"}}},
		run: runPanel{scripts: []scripts.Script{
			scripts.Parse("any.sh", []byte("echo\n")),
			scripts.Parse("nix.sh", []byte("# ts-cli: os=linux,darwin\n")),
			scripts.Parse("win.ps1", []byte("# ts-cli: os=windows\n")),
		}},
	}
	got := m.visibleScripts()
	if len(got) != 2 || got[0].File != "any.sh" || got[1].File != "win.ps1" {
		t.Fatalf("visibleScripts = %+v", got)
	}
}

func TestRunJobStreamsLinesAndExitCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs sh")
	}
	cmd := exec.Command("sh", "-c", "echo one; echo two 1>&2; printf 'three'; exit 3")
	job, err := startRunJob(cmd, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	var lines []string
	var res runResult
	deadline := time.After(5 * time.Second)
	for done := false; !done; {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for job")
		default:
		}
		switch msg := waitRunEvent(job)().(type) {
		case runLinesMsg:
			lines = append(lines, msg.lines...)
		case runDoneMsg:
			res = msg.res
			done = true
		}
	}
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines (stdout, stderr, unterminated), got %v", lines)
	}
	if res.exitCode != 3 || res.failure != sshFailureRemoteExit || res.killed {
		t.Fatalf("result = %+v", res)
	}
}

func TestRunJobStdinAndKill(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs sh")
	}
	// stdin is delivered to the process (how scripts reach the remote host)
	job, err := startRunJob(exec.Command("sh", "-c", "cat"), []byte("payload\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	msg := waitRunEvent(job)().(runLinesMsg)
	if len(msg.lines) != 1 || msg.lines[0] != "payload" {
		t.Fatalf("stdin not delivered: %v", msg.lines)
	}
	if done, ok := waitRunEvent(job)().(runDoneMsg); !ok || done.res.exitCode != 0 {
		t.Fatalf("expected clean exit, got %+v", done)
	}

	// kill() ends a long-running job and marks the result
	cleaned := false
	job, err = startRunJob(exec.Command("sh", "-c", "sleep 30"), nil, func() { cleaned = true })
	if err != nil {
		t.Fatal(err)
	}
	job.kill()
	done, ok := waitRunEvent(job)().(runDoneMsg)
	if !ok || !done.res.killed || done.res.failure != sshFailureRemoteExit {
		t.Fatalf("expected killed result, got %+v", done)
	}
	if !cleaned {
		t.Fatal("cleanup must run after the process exits")
	}
}

func TestHandleRunDoneRecordsHistoryAndClearsPending(t *testing.T) {
	t.Setenv(util.ConfigDirEnv, t.TempDir())
	hist, err := util.NewHistoryStore()
	if err != nil {
		t.Fatal(err)
	}
	job := &runJob{}
	dev := client.Device{ID: "dev-1", Name: "laptop", Addresses: []string{"100.64.0.1"}}
	m := model{
		list: deviceList{filteredDevices: []client.Device{dev}},
		run: runPanel{history: hist, label: "uptime", startedAt: time.Now(),
			runs: []*machineRun{{device: dev, username: "root", job: job, status: runRunning, lines: []string{"ok"}, startedAt: time.Now()}}},
		ssh: ssh{machineCreds: map[string]machineCredential{}, pending: pendingSSH{
			action: sshActionCommand, machineID: "dev-1", machineName: "laptop", username: "root", command: "uptime",
		}},
	}

	updated, _ := m.handleRunDone(runDoneMsg{job: job, res: runResult{exitCode: 0}})
	m = updated.(model)

	if m.run.running() || !m.run.finished() || m.run.runs[0].job != nil {
		t.Fatalf("output state not finalised: %+v", m.run.runs[0])
	}
	if m.ssh.pending.action != sshActionNone {
		t.Fatal("pending attempt should be cleared after success")
	}
	recent := hist.Recent("dev-1", 10)
	if len(recent) != 1 || recent[0].Command != "uptime" || recent[0].Kind != util.HistoryKindCommand {
		t.Fatalf("history not recorded: %+v", recent)
	}
	if cred := m.ssh.machineCreds["dev-1"]; cred.username != "root" {
		t.Fatalf("username should be persisted after a successful run: %+v", cred)
	}

	// A stale job's completion is ignored.
	updated, _ = m.handleRunDone(runDoneMsg{job: &runJob{}, res: runResult{exitCode: 9}})
	if updated.(model).run.runs[0].exitCode != 0 {
		t.Fatal("stale job result must be ignored")
	}
}

func TestAppendRunLinesFollowsTail(t *testing.T) {
	job := &runJob{}
	m := model{run: runPanel{follow: true, runs: []*machineRun{{job: job, status: runRunning}}}, height: 40, width: 120}
	m.appendRunLines(job, []string{"a", "b", "c"})
	if m.run.outputCursor != 2 {
		t.Fatalf("follow mode should keep cursor on last line, got %d", m.run.outputCursor)
	}
	m.run.follow = false
	m.run.outputCursor = 0
	m.appendRunLines(job, []string{"d"})
	if m.run.outputCursor != 0 {
		t.Fatal("cursor must stay put when the user scrolled up")
	}
	m.appendRunLines(&runJob{}, []string{"stale"})
	if len(m.run.runs[0].lines) != 4 {
		t.Fatal("lines from an unknown job must be dropped")
	}
}

// ─── Multi-machine runs ───────────────────────────────────────────────────────

func multiModel(t *testing.T) model {
	t.Helper()
	t.Setenv(util.ConfigDirEnv, t.TempDir())
	hist, _ := util.NewHistoryStore()
	now := time.Now()
	devices := []client.Device{
		{ID: "a", Name: "alpha", OS: "linux", Addresses: []string{"100.64.0.1"}, LastSeen: now},
		{ID: "b", Name: "bravo", OS: "linux", Addresses: []string{"100.64.0.2"}, LastSeen: now},
		{ID: "c", Name: "charlie", OS: "windows", Addresses: []string{"100.64.0.3"}, LastSeen: now},
		{ID: "d", Name: "delta", OS: "linux", Addresses: []string{"100.64.0.4"}, LastSeen: now.Add(-time.Hour)},
		{ID: "e", Name: "echo", OS: "linux", Addresses: []string{"100.64.0.5"}, LastSeen: now},
	}
	return model{
		width: 140, height: 45,
		list: deviceList{devices: devices, filteredDevices: devices},
		run:  runPanel{history: hist},
		ssh: ssh{username: "admin", machineCreds: map[string]machineCredential{
			"b": {name: "bravo", username: "pi"},
		}},
	}
}

func TestMarksAndTargets(t *testing.T) {
	m := multiModel(t)
	if m.isMulti() || len(m.targets()) != 1 {
		t.Fatal("without marks the highlighted machine is the only target")
	}
	m.toggleMark(m.list.filteredDevices[0])
	m.toggleMark(m.list.filteredDevices[2])
	if !m.isMulti() || len(m.targets()) != 2 || m.targets()[1].ID != "c" {
		t.Fatalf("targets = %+v", m.targets())
	}
	m.toggleMark(m.list.filteredDevices[2])
	if m.isMulti() {
		t.Fatal("one mark is not multi")
	}
	m.toggleAllMarks()
	if len(m.markedDevices()) != 5 {
		t.Fatalf("expected all marked, got %d", len(m.markedDevices()))
	}
	m.toggleAllMarks()
	if len(m.markedDevices()) != 0 {
		t.Fatal("toggle all on a full selection must clear it")
	}
	m.toggleMark(m.list.filteredDevices[0])
	m.clearMarks()
	if len(m.list.marked) != 0 {
		t.Fatal("clearMarks")
	}
}

func TestVisibleScriptsAcrossTargets(t *testing.T) {
	m := multiModel(t)
	m.run.scripts = []scripts.Script{
		scripts.Parse("nix.sh", []byte("# ts-cli: os=linux\n")),
		scripts.Parse("win.ps1", []byte("# ts-cli: os=windows\n")),
		scripts.Parse("mac.sh", []byte("# ts-cli: os=darwin\n")),
	}
	m.toggleMark(m.list.filteredDevices[0]) // linux
	m.toggleMark(m.list.filteredDevices[2]) // windows
	got := m.visibleScripts()
	if len(got) != 2 || got[0].File != "nix.sh" || got[1].File != "win.ps1" {
		t.Fatalf("a script is listed when any target can run it: %+v", got)
	}
}

func TestStartMultiRunSkipsAndStarts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs sh")
	}
	m := multiModel(t)
	m.ssh.username = "" // only bravo has a username now
	m.toggleAllMarks()
	spec := sshJob{action: sshActionScript, script: ptr(scripts.Parse("nix.sh", []byte("#!/bin/sh\n# ts-cli: os=linux\necho hi\n")))}

	// Replace ssh with a local stub so startRemoteJob can actually spawn.
	// We only check planning here, so make every start fail fast instead.
	updated, _ := m.startMultiRun(spec)
	m = updated.(model)

	if !m.run.multi || len(m.run.runs) != 5 {
		t.Fatalf("expected 5 machine runs, got %+v", m.run.runs)
	}
	byID := map[string]*machineRun{}
	for _, mr := range m.run.runs {
		byID[mr.device.ID] = mr
	}
	if byID["c"].status != runSkipped || byID["c"].note != "script is not for windows" {
		t.Fatalf("windows machine: %+v", byID["c"])
	}
	if byID["d"].status != runSkipped || byID["d"].note != "offline" {
		t.Fatalf("offline machine: %+v", byID["d"])
	}
	if byID["a"].status != runSkipped || byID["a"].note == "" {
		t.Fatalf("machine without username must be skipped: %+v", byID["a"])
	}
	// bravo has a saved username; it was attempted (ssh may or may not exist here).
	if byID["b"].status == runSkipped || byID["b"].status == runPending {
		t.Fatalf("bravo should be attempted: %+v", byID["b"])
	}
	if byID["b"].username != "pi" {
		t.Fatalf("bravo must use its saved username, got %q", byID["b"].username)
	}
	m.stopRun()
	if m.outputStatus() == "" {
		t.Fatal("expected a status summary")
	}
}

func ptr[T any](v T) *T { return &v }

func TestContinueMultiRunRecordsAndStartsNext(t *testing.T) {
	m := multiModel(t)
	m.run.multi = true
	m.run.spec = sshJob{action: sshActionCommand, command: "uptime"}
	m.run.label = "uptime"
	m.run.startedAt = time.Now()
	jobA := &runJob{}
	a := &machineRun{device: m.list.filteredDevices[0], username: "admin", job: jobA, status: runRunning, startedAt: time.Now(), lines: []string{"up"}}
	skipped := &machineRun{device: m.list.filteredDevices[3], status: runSkipped, note: "offline"}
	m.run.runs = []*machineRun{a, skipped}

	m.settleMachineRun(a, runResult{exitCode: 0})
	updated, _ := m.continueMultiRun(a, runResult{exitCode: 0})
	m = updated.(model)

	if a.status != runDone || a.exitCode != 0 {
		t.Fatalf("alpha: %+v", a)
	}
	if got := m.run.history.Recent("a", 5); len(got) != 1 || got[0].Command != "uptime" {
		t.Fatalf("history for alpha: %+v", got)
	}
	if cred := m.ssh.machineCreds["a"]; cred.username != "admin" || cred.name != "alpha" {
		t.Fatalf("default username should be persisted per machine after success: %+v", cred)
	}
	if m.run.running() || !m.run.finished() {
		t.Fatal("run should be finished")
	}
	if s := m.outputStatus(); !strings.Contains(s, "1/1 done") || !strings.Contains(s, "1 skipped") {
		t.Fatalf("status = %q", s)
	}

	// Auth failure is reported, not prompted for.
	b := &machineRun{device: m.list.filteredDevices[1], username: "pi", job: &runJob{}, status: runRunning, startedAt: time.Now()}
	m.run.runs = append(m.run.runs, b)
	m.settleMachineRun(b, runResult{exitCode: 255, failure: sshFailureConnection})
	updated, _ = m.continueMultiRun(b, runResult{exitCode: 255, failure: sshFailureConnection})
	m = updated.(model)
	if b.status != runFailed || !strings.Contains(b.note, "connect once with s") {
		t.Fatalf("bravo: %+v", b)
	}
	if m.input.mode != inputNone || m.ssh.retryMode {
		t.Fatal("multi runs must never open prompts")
	}
}

func TestOutputLinesSectionsAndNavigation(t *testing.T) {
	m := multiModel(t)
	m.run.multi = true
	m.run.runs = []*machineRun{
		{device: m.list.filteredDevices[0], status: runDone, lines: []string{"a1", "a2"}},
		{device: m.list.filteredDevices[3], status: runSkipped, note: "offline"},
		{device: m.list.filteredDevices[4], status: runDone, exitCode: 2, lines: []string{"e1"}},
	}
	lines := m.outputLines()
	// header a, a1, a2, sep, header d, note, sep, header e, e1
	if len(lines) != 9 || !lines[0].header || lines[4].header != true || lines[7].header != true {
		t.Fatalf("flattened lines = %+v", lines)
	}
	if lines[5].text != "  offline" {
		t.Fatalf("skip note missing: %q", lines[5].text)
	}
	m.jumpToSection(+1)
	if m.run.outputCursor != 4 {
		t.Fatalf("} should land on next header, got %d", m.run.outputCursor)
	}
	m.jumpToSection(+1)
	if m.run.outputCursor != 7 {
		t.Fatalf("} again, got %d", m.run.outputCursor)
	}
	m.jumpToSection(-1)
	if m.run.outputCursor != 4 {
		t.Fatalf("{ should go back, got %d", m.run.outputCursor)
	}
	m.run.outputCursor = 8
	if entry, ok := m.selectedOutputItem(); !ok || entry != "e1" {
		t.Fatalf("selected = %q %v", entry, ok)
	}
	m.run.outputCursor = 7
	if _, ok := m.selectedOutputItem(); ok {
		t.Fatal("headers are not copyable")
	}
	m.activeFocus = focusOutput
	m.run.visible = true
	if out := m.View(); len(out) == 0 {
		t.Fatal("empty multi view")
	}
}
