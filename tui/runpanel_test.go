package tui

import (
	"os/exec"
	"runtime"
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
	m := model{
		list: deviceList{filteredDevices: []client.Device{{ID: "dev-1", Name: "laptop", Addresses: []string{"100.64.0.1"}}}},
		run:  runPanel{history: hist, job: job, running: true, lines: []string{"ok"}, startedAt: time.Now()},
		ssh: ssh{machineCreds: map[string]machineCredential{}, pending: pendingSSH{
			action: sshActionCommand, machineID: "dev-1", machineName: "laptop", username: "root", command: "uptime",
		}},
	}

	updated, _ := m.handleRunDone(runDoneMsg{job: job, res: runResult{exitCode: 0}})
	m = updated.(model)

	if m.run.running || !m.run.finished || m.run.job != nil {
		t.Fatalf("output state not finalised: %+v", m.run)
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
	if updated.(model).run.exitCode != 0 {
		t.Fatal("stale job result must be ignored")
	}
}

func TestAppendJobLinesFollowsTail(t *testing.T) {
	m := model{run: runPanel{follow: true}, height: 40, width: 120}
	m.appendJobLines([]string{"a", "b", "c"})
	if m.run.outputCursor != 2 {
		t.Fatalf("follow mode should keep cursor on last line, got %d", m.run.outputCursor)
	}
	m.run.follow = false
	m.run.outputCursor = 0
	m.appendJobLines([]string{"d"})
	if m.run.outputCursor != 0 {
		t.Fatal("cursor must stay put when the user scrolled up")
	}
}

func TestViewRendersRunAndOutputPanels(t *testing.T) {
	t.Setenv(util.ConfigDirEnv, t.TempDir())
	devices := []client.Device{{ID: "1", Name: "laptop.example.ts.net", Hostname: "laptop", OS: "linux", Addresses: []string{"100.64.0.1"}, LastSeen: time.Now()}}
	m := model{
		width: 140, height: 45,
		list: deviceList{devices: devices, filteredDevices: devices},
		run: runPanel{
			visible:  true,
			scripts:  []scripts.Script{scripts.Parse("du.sh", []byte("# ts-cli: name=Disk usage; args=path\n"))},
			commands: []config.SavedCommand{{Name: "uptime", Command: "uptime"}},
			lines:    []string{"line 1", "line 2"},
			finished: true, exitCode: 2, label: "uptime",
		},
		activeFocus: focusRun,
	}
	for _, tab := range []runTab{tabScripts, tabCommands, tabRecent} {
		m.run.tab = tab
		for _, f := range []panelFocus{focusList, focusRun, focusOutput} {
			m.activeFocus = f
			if out := m.View(); len(out) == 0 {
				t.Fatalf("empty view for tab %v focus %v", tab, f)
			}
		}
	}
	m.input = textInput{mode: inputCommand, value: "ls"}
	if out := m.View(); len(out) == 0 {
		t.Fatal("empty view with ad-hoc prompt")
	}
	m.input = textInput{mode: inputText, label: "Value for {{path}}", hint: "Script: Disk usage"}
	if out := m.View(); len(out) == 0 {
		t.Fatal("empty view with text prompt")
	}
}
