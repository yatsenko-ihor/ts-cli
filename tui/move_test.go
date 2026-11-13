package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// moveFixture opens the manager with fake tailscale and scp so a copy really runs.
func moveFixture(t *testing.T, scpExit string) fmFixture {
	t.Helper()
	dir := useFakeBins(t, fakeBins{ssh: true})
	for name, body := range map[string]string{"tailscale": "#!/bin/sh\necho ok\n", "scp": "#!/bin/sh\nexit " + scpExit + "\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return fmOpen(t)
}

// finishCopy runs the pending job to its end and feeds the result back.
func finishCopy(t *testing.T, f *fmFixture) tea.Cmd {
	t.Helper()
	if len(f.m.run.runs) != 1 || f.m.run.runs[0].job == nil {
		t.Fatalf("no copy running (error: %v)", f.m.notify.sshError)
	}
	job := f.m.run.runs[0].job
	for {
		msg := waitRunEvent(job)()
		if done, ok := msg.(runDoneMsg); ok {
			u, cmd := f.m.Update(done)
			f.m = u.(model)
			return cmd
		}
	}
}

// pumpPreflight delivers the reachability check that comes before a launch.
func pumpPreflight(t *testing.T, f *fmFixture, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	for _, c := range flattenCmd(cmd) {
		if p, ok := c().(pingMsg); ok {
			u, _ := f.m.Update(p)
			f.m = u.(model)
		}
	}
}

func answerPrompt(t *testing.T, f *fmFixture, text string) tea.Cmd {
	t.Helper()
	if f.m.input.onSubmit == nil {
		t.Fatal("no prompt")
	}
	u, cmd := f.m.input.onSubmit(f.m, text)
	f.m = u.(model)
	return cmd
}

func runMoveCmd(t *testing.T, f *fmFixture, cmd tea.Cmd) {
	t.Helper()
	for _, c := range flattenCmd(cmd) {
		switch mm := c().(type) {
		case moveDoneMsg:
			u, more := f.m.Update(mm)
			f.m = u.(model)
			runMoveCmd(t, f, more) // the reload that follows
		case fmListMsg:
			u, _ := f.m.Update(mm)
			f.m = u.(model)
		}
	}
}

// flattenCmd runs a Batch and returns its non-nil commands.
func flattenCmd(cmd tea.Cmd) []tea.Cmd {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Cmd
		for _, c := range b {
			out = append(out, flattenCmd(c)...)
		}
		return out
	}
	return []tea.Cmd{func() tea.Msg { return msg }}
}

func TestMoveUploadsThenDeletesTheLocalOriginal(t *testing.T) {
	f := moveFixture(t, "0")
	f.key("down") // docs/ -> a.txt is the second real entry; pick by name
	for f.m.fm.fmSelected(fmLocal).name != "a.txt" {
		f.key("down")
	}
	f.key("f6")
	if !strings.Contains(f.m.input.label, "Move a.txt from this computer to laptop") {
		t.Fatalf("label %q", f.m.input.label)
	}
	if cmd := answerPrompt(t, &f, "no"); cmd != nil || f.m.fm.status != "Not moved" || f.m.afterCopy != nil {
		t.Fatal("declined")
	}
	f.key("M")
	pumpPreflight(t, &f, answerPrompt(t, &f, "yes"))
	if f.m.fm == nil || f.m.afterCopy == nil || f.m.afterCopy.src != filepath.Join(f.local, "a.txt") {
		t.Fatalf("move pending: %+v", f.m.afterCopy)
	}
	cmd := finishCopy(t, &f)
	if _, err := os.Stat(filepath.Join(f.local, "a.txt")); err != nil {
		t.Fatal("the original is still there until the delete runs")
	}
	runMoveCmd(t, &f, cmd)
	if _, err := os.Stat(filepath.Join(f.local, "a.txt")); err == nil {
		t.Fatal("the original must be deleted after a successful copy")
	}
	if f.m.fm == nil || !strings.Contains(f.m.fm.status, "Moved a.txt") || f.m.afterCopy != nil {
		t.Fatalf("status %q", f.m.fm.status)
	}
}

func TestMoveDownloadsThenDeletesTheRemoteOriginal(t *testing.T) {
	f := moveFixture(t, "0")
	f.key("tab")
	for f.m.fm.fmSelected(fmRemote).name != "notes.txt" {
		f.key("down")
	}
	f.key("f6")
	if !strings.Contains(f.m.input.label, "from laptop.example.com to this computer") {
		t.Fatalf("label %q", f.m.input.label)
	}
	pumpPreflight(t, &f, answerPrompt(t, &f, "yes"))
	cmd := finishCopy(t, &f)
	if len(*f.ran) != 0 {
		t.Fatal("nothing is deleted before the copy ends")
	}
	runMoveCmd(t, &f, cmd)
	if len(*f.ran) != 1 || !strings.Contains((*f.ran)[0], "rm -rf -- '/srv/notes.txt'") {
		t.Fatalf("remote commands: %v", *f.ran)
	}
}

func TestFailedCopyKeepsTheOriginal(t *testing.T) {
	f := moveFixture(t, "1")
	for f.m.fm.fmSelected(fmLocal).name != "a.txt" {
		f.key("down")
	}
	f.key("f6")
	pumpPreflight(t, &f, answerPrompt(t, &f, "yes"))
	cmd := finishCopy(t, &f)
	runMoveCmd(t, &f, cmd)
	if _, err := os.Stat(filepath.Join(f.local, "a.txt")); err != nil {
		t.Fatal("a failed copy must not delete the original")
	}
	if f.m.afterCopy != nil || !strings.Contains(f.m.notify.info, "original was kept") {
		t.Fatalf("info %q pending %v", f.m.notify.info, f.m.afterCopy)
	}
}

func TestSettleMoveEdgeCases(t *testing.T) {
	spec := copySpec{local: "/tmp/x", remote: "/srv"}
	base := func() model {
		m := flowModel(t, nil, "pi")
		m.run.spec = sshJob{action: sshActionCopy, copy: spec}
		m.afterCopy = &moveJob{name: "x", src: "/tmp/x", spec: spec, device: m.list.devices[0]}
		return m
	}
	// no move pending, or a job still running: nothing happens
	if _, c := flowModel(t, nil, "pi").settleMove(runResult{}); c != nil {
		t.Fatal("no move")
	}
	busy := base()
	busy.run.runs = []*machineRun{{status: runRunning}}
	if nm, c := busy.settleMove(runResult{}); c != nil || nm.afterCopy == nil {
		t.Fatal("still running")
	}
	// a login problem leads to a retry: the move stays pending
	for _, fail := range []sshFailure{sshFailureConnection, sshFailureBadPassword} {
		m := base()
		if nm, c := m.settleMove(runResult{failure: fail, exitCode: 255}); c != nil || nm.afterCopy == nil {
			t.Fatalf("retry %v keeps the move", fail)
		}
	}
	// a stopped copy, another copy, a non-zero exit: the original stays
	for name, mut := range map[string]func(*model, *runResult){
		"killed":     func(m *model, r *runResult) { r.killed = true; r.failure = sshFailureRemoteExit },
		"other copy": func(m *model, r *runResult) { m.run.spec.copy.remote = "/elsewhere" },
		"exit 1":     func(m *model, r *runResult) { r.exitCode = 1; r.failure = sshFailureRemoteExit },
		"not a copy": func(m *model, r *runResult) { m.run.spec.action = sshActionCommand },
	} {
		m, res := base(), runResult{}
		mut(&m, &res)
		nm, c := m.settleMove(res)
		if c != nil || nm.afterCopy != nil || !strings.Contains(nm.notify.info, "original was kept") {
			t.Errorf("%s: pending=%v info=%q", name, nm.afterCopy, nm.notify.info)
		}
	}
	// dangerous sources are refused
	for _, src := range []string{"", "/", ".", ".."} {
		m := base()
		m.afterCopy.src = src
		nm, c := m.settleMove(runResult{})
		if c != nil || nm.notify.sshError == nil || !unsafeToDelete(src) {
			t.Errorf("%q must be refused", src)
		}
	}
	if unsafeToDelete("/home/me/file") {
		t.Fatal("an ordinary path is fine")
	}
	// a failing delete is reported with the reason
	old := fmRemoteExec
	fmRemoteExec = func(remoteSource, string) (string, error) { return "denied\nmore", errors.New("exit status 1") }
	t.Cleanup(func() { fmRemoteExec = old })
	m := base()
	m.afterCopy.fromRemote = true
	_, cmd := m.settleMove(runResult{})
	msg := cmd().(moveDoneMsg)
	u, _ := m.handleMoveDone(msg)
	if e := u.(model).notify.sshError; e == nil || !strings.Contains(e.Error(), "could not delete the original: denied") {
		t.Fatalf("err = %v", u.(model).notify.sshError)
	}
	// a cancelled attempt forgets the move
	if m.cancelSSHAttempt().afterCopy != nil {
		t.Fatal("cancel clears the move")
	}
	// a new launch clears it too
	m2 := base()
	u2, _ := m2.launch(sshJob{action: sshActionCommand, command: "true"})
	if u2.(model).afterCopy != nil {
		t.Fatal("launch clears the move")
	}
}

func TestFileManagerKeyBarAndFunctionKeys(t *testing.T) {
	f := fmOpen(t)
	view := f.m.View()
	for _, want := range []string{"F2 Rename · F5 Copy", "F6 Move", "F7 MkDir", "F8 Delete", "F10 Close"} {
		if !strings.Contains(view, want) {
			t.Errorf("key bar lacks %q", want)
		}
	}
	f.key("f7")
	if f.m.input.mode != inputText || !strings.Contains(f.m.input.label, "New directory") {
		t.Fatal("F7 makes a directory")
	}
	f.m.input = textInput{}
	f.key("f8")
	if !strings.Contains(f.m.input.label, "Delete") {
		t.Fatal("F8 deletes")
	}
	f.m.input = textInput{}
	f.key("f2")
	if !strings.HasPrefix(f.m.input.label, "Rename") {
		t.Fatal("F2 renames")
	}
	f.m.input = textInput{}
	f.key("f10")
	if f.m.fm != nil {
		t.Fatal("F10 closes")
	}
	// nothing selected: F6 says so
	g := fmOpen(t)
	g.key("up")
	g.key("up")
	g.key("f6")
	if !strings.Contains(g.m.fm.status, "Select a file or directory to move") {
		t.Fatalf("status %q", g.m.fm.status)
	}
}

func TestMoveIsNotPendingWhenTheCopyCannotStart(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // no tailscale: the launch is refused
	f := fmOpen(t)
	for f.m.fm.fmSelected(fmLocal).name != "a.txt" {
		f.key("down")
	}
	f.key("f6")
	answerPrompt(t, &f, "yes")
	if f.m.afterCopy != nil || f.m.fm == nil || f.m.fm.err == "" || f.m.notify.sshError != nil {
		t.Fatalf("a refused launch leaves no move pending and says why in the manager: %v / %q", f.m.afterCopy, f.m.fm.err)
	}
}

func TestCopyKeepsTheManagerOpenAndReportsInIt(t *testing.T) {
	f := moveFixture(t, "0")
	for f.m.fm.fmSelected(fmLocal).name != "a.txt" {
		f.key("down")
	}
	pumpPreflight(t, &f, f.key("f5"))
	if f.m.fm == nil || !strings.HasPrefix(f.m.fm.status, "Copying: upload a.txt") {
		t.Fatalf("the manager stays and shows the progress: %+v", f.m.fm)
	}
	if view := f.m.View(); !strings.Contains(view, "⟳ upload a.txt") || !strings.Contains(view, "x stops") {
		t.Fatalf("the progress line is on screen:\n%s", view)
	}
	// another copy while one runs is refused
	f.key("f5")
	if !strings.Contains(f.m.fm.status, "Another job is still running") {
		t.Fatalf("status %q", f.m.fm.status)
	}
	cmd := finishCopy(t, &f)
	runMoveCmd(t, &f, cmd)
	if f.m.fm == nil || !strings.Contains(f.m.fm.status, "Copy finished") || f.m.fm.err != "" {
		t.Fatalf("result on the status line: %q / %q", f.m.fm.status, f.m.fm.err)
	}
	if _, err := os.Stat(filepath.Join(f.local, "a.txt")); err != nil {
		t.Fatal("a copy keeps the original")
	}
}

func TestFailedCopyShowsTheReasonInTheManager(t *testing.T) {
	f := moveFixture(t, "1")
	for f.m.fm.fmSelected(fmLocal).name != "a.txt" {
		f.key("down")
	}
	pumpPreflight(t, &f, f.key("f5"))
	runMoveCmd(t, &f, finishCopy(t, &f))
	if f.m.fm == nil || !strings.Contains(f.m.fm.err, "Copy failed (exit 1)") {
		t.Fatalf("err %q", f.m.fm.err)
	}
	f.key("down") // any key dismisses the result
	if f.m.fm.err != "" || f.m.fm.keep {
		t.Fatalf("dismissed: %q keep=%v", f.m.fm.err, f.m.fm.keep)
	}
	// with a line of scp output before the result, it is shown too
	g := moveFixture(t, "1")
	g.m.run.spec = sshJob{action: sshActionCopy, copy: copySpec{local: "/x", remote: "/y"}}
	g.m.run.runs = []*machineRun{{status: runDone, lines: []string{"scp: /y: Permission denied", "✗ Copy failed (exit 1) after 1s: upload x → /y"}}}
	nm, _ := g.m.fmAfterRun(runResult{exitCode: 1, failure: sshFailureRemoteExit})
	if !strings.Contains(nm.fm.err, "Copy failed") || !strings.Contains(nm.fm.err, "Permission denied") {
		t.Fatalf("err %q", nm.fm.err)
	}
}

func TestFmAfterRunIgnoresWhatIsNotItsBusiness(t *testing.T) {
	f := moveFixture(t, "0")
	m := f.m
	// not a copy
	m.run.spec = sshJob{action: sshActionCommand}
	if nm, c := m.fmAfterRun(runResult{}); c != nil || nm.fm.status != "" {
		t.Fatal("only copies")
	}
	// a login problem: the credential flow runs the copy again
	m.run.spec = sshJob{action: sshActionCopy}
	if _, c := m.fmAfterRun(runResult{failure: sshFailureConnection}); c != nil {
		t.Fatal("retry")
	}
	// still running
	busy := m
	busy.run.runs = []*machineRun{{status: runRunning}}
	if _, c := busy.fmAfterRun(runResult{}); c != nil {
		t.Fatal("running")
	}
	// no manager
	none := m
	none.fm = nil
	if _, c := none.fmAfterRun(runResult{}); c != nil {
		t.Fatal("no manager")
	}
	// no output at all: reload only
	m.run.runs = []*machineRun{{status: runDone}}
	if nm, c := m.fmAfterRun(runResult{}); c == nil || nm.fm.status != "" {
		t.Fatalf("reload without a status: %+v", nm.fm)
	}
	// x stops a running copy; without one it does nothing
	m.run.runs = []*machineRun{{status: runRunning, job: &runJob{}}}
	m.run.spec = sshJob{action: sshActionCopy, copy: copySpec{local: "/x", remote: "/y"}}
	u, _ := m.Update(fmKey("x"))
	if u.(model).fm.status != "Stopping…" {
		t.Fatal("x stops")
	}
	m.run.runs = nil
	u, _ = m.Update(fmKey("x"))
	if u.(model).fm.status != "" {
		t.Fatal("x without a job")
	}
	// a failed move delete shows in the manager
	mv := f.m
	u, _ = mv.handleMoveDone(moveDoneMsg{name: "a.txt", err: errors.New("denied")})
	if !strings.Contains(u.(model).fm.err, "could not delete the original: denied") {
		t.Fatalf("err %q", u.(model).fm.err)
	}
}
