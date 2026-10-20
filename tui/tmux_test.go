//go:build !windows

package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yatsenko-ihor/ts-cli/client"
)

// Requirements-based tests for the tmux split (see verification/requirements/tmux.md).

// fakeTmuxScript records its arguments and runs the pane command like tmux
// would (through a shell), detached, with stdin from /dev/null.
const fakeTmuxScript = `#!/bin/sh
d="$FAKE_DIR"
: > "$d/tmux.argv"
for a in "$@"; do printf '%s\0' "$a" >> "$d/tmux.argv"; done
[ -n "$FAKE_TMUX_FAIL" ] && exit 1
for last in "$@"; do :; done
sh -c "$last" </dev/null >"$d/pane.out" 2>&1 &
exit 0
`

// useFakeTmux puts fake tmux, ssh and (optionally) sshpass on a private PATH
// and pretends ts-cli runs inside tmux.
func useFakeTmux(t *testing.T, withSSHPass bool) string {
	t.Helper()
	dir := useFakeBins(t, fakeBins{ssh: true, sshpass: withSSHPass})
	if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte(fakeTmuxScript), 0o755); err != nil {
		t.Fatal(err)
	}
	// The pane command needs a shell and rm, which a private PATH lacks.
	for _, tool := range []string{"sh", "rm"} {
		p := "/bin/" + tool // PATH is already the private directory, so no LookPath
		if _, err := os.Stat(p); err != nil {
			t.Skipf("%s not found", p)
		}
		if err := os.Symlink(p, filepath.Join(dir, tool)); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("FAKE_TMUX_FAIL", "")
	t.Setenv("TMPDIR", t.TempDir()) // password pipes are created here
	old := inTmux
	inTmux = func() bool { return true }
	t.Cleanup(func() { inTmux = old })
	return dir
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if st, err := os.Stat(path); err == nil && st.Size() > 0 {
			return
		}
	}
	t.Fatalf("%s was not written in time", path)
}

func pwDirs(t *testing.T) []string {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(os.Getenv("TMPDIR"), "ts-cli-pw-*"))
	return m
}

func tmuxDevice() client.Device {
	return client.Device{ID: "d1", Name: "box.example.com", Addresses: []string{"100.64.0.9"}}
}

// Verifies: REQ-TMX-001
func TestTmuxSplitActiveConditions(t *testing.T) {
	cases := []struct {
		name     string
		option   bool
		inTmux   bool
		tmuxBin  bool
		sshpass  bool
		password string
		want     bool
	}{
		{"all conditions met, key auth", true, true, true, false, "", true},
		{"all conditions met, password with sshpass", true, true, true, true, "pw", true},
		{"option off", false, true, true, true, "", false},
		{"not inside tmux", true, false, true, true, "", false},
		{"tmux not installed", true, true, false, true, "", false},
		{"password but no sshpass", true, true, true, false, "pw", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := useFakeBins(t, fakeBins{ssh: true, sshpass: c.sshpass})
			if c.tmuxBin {
				_ = os.WriteFile(filepath.Join(dir, "tmux"), []byte(fakeTmuxScript), 0o755)
			}
			old := inTmux
			inTmux = func() bool { return c.inTmux }
			t.Cleanup(func() { inTmux = old })
			m := model{set: uiSettings{tmuxSplit: c.option}}
			if got := m.tmuxSplitActive(c.password); got != c.want {
				t.Fatalf("tmuxSplitActive = %v, want %v", got, c.want)
			}
		})
	}
}

// Verifies: REQ-TMX-002
func TestTmuxSplitArgsWrapScriptInShellAndTargetThePane(t *testing.T) {
	args := tmuxSplitArgs("echo 'hi'", "%5")
	want := []string{"split-window", "-h", "-t", "%5", `sh -c 'echo '"'"'hi'"'"''`}
	if strings.Join(args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("args = %q", args)
	}
	if got := tmuxSplitArgs("x", ""); len(got) != 3 || got[0] != "split-window" || got[1] != "-h" {
		t.Fatalf("without a pane id there is no -t: %q", got)
	}
}

// Verifies: REQ-TMX-003
func TestTmuxSSHScriptKeyAuthRunsSSHAndKeepsPaneOpenOnFailure(t *testing.T) {
	dir := useFakeBins(t, fakeBins{ssh: true})
	t.Setenv("FAKE_SSH_EXIT", "3")
	script := tmuxSSHScript("root@100.64.0.9", "", "")
	if strings.Contains(script, "sshpass") || strings.Contains(script, "exec 3<") {
		t.Fatalf("no password: no sshpass or fifo in %q", script)
	}
	out, _ := runShell(t, script, "") // stdin is at EOF, so `read` ends the pane with status 1
	if !strings.Contains(out, "ssh exited with status 3") {
		t.Fatalf("a failure must be shown before the pane closes: %q", out)
	}
	argv := fakeArgv(t, dir, "ssh.argv")
	if argv[len(argv)-1] != "root@100.64.0.9" || strings.Join(argv, " ") != "-o StrictHostKeyChecking=accept-new -o ConnectTimeout=15 root@100.64.0.9" {
		t.Fatalf("ssh argv = %q", argv)
	}

	// A clean exit closes the pane silently.
	t.Setenv("FAKE_SSH_EXIT", "0")
	out, err := runShell(t, script, "")
	if err != nil || strings.Contains(out, "exited") {
		t.Fatalf("clean exit: %q %v", out, err)
	}
}

// Verifies: REQ-TMX-004
func TestTmuxSSHScriptPasswordPathIsQuotedAndOnlyThePipeIsNamed(t *testing.T) {
	script := tmuxSSHScript("root@100.64.0.9", "/tmp/dir with 'quote'/pw", "/tmp/dir with 'quote'")
	for _, want := range []string{`exec 3<'/tmp/dir with '"'"'quote'"'"'/pw'`, `rm -rf '/tmp/dir with '"'"'quote'"'"''`, "sshpass -d 3 ssh "} {
		if !strings.Contains(script, want) {
			t.Fatalf("script lacks %q:\n%s", want, script)
		}
	}
}

// Verifies: REQ-TMX-005, REQ-TMX-006
func TestOpenInTmuxPassesPasswordOnlyThroughThePipe(t *testing.T) {
	dir := useFakeTmux(t, true)
	t.Setenv("TMUX_PANE", "%7")
	msg := openInTmux(tmuxDevice(), "root", "S3cret!pw")()
	if m := msg.(tmuxOpenedMsg); m.err != nil || m.name != "box.example.com" {
		t.Fatalf("msg = %+v", m)
	}

	waitForFile(t, filepath.Join(dir, "sshpass.fd3"))
	if got := fakeFile(t, dir, "sshpass.fd3"); got != "S3cret!pw" {
		t.Fatalf("the pane's sshpass must receive the password on fd 3, got %q", got)
	}
	tmuxArgs := strings.Join(fakeArgv(t, dir, "tmux.argv"), " ")
	if !strings.HasPrefix(tmuxArgs, "split-window -h -t %7 sh -c ") {
		t.Fatalf("tmux argv = %q", tmuxArgs)
	}
	for _, f := range []string{"tmux.argv", "sshpass.argv", "ssh.argv", "pane.out"} {
		if b, err := os.ReadFile(filepath.Join(dir, f)); err == nil && strings.Contains(string(b), "S3cret!pw") {
			t.Fatalf("the password leaked into %s", f)
		}
	}
	if strings.Join(fakeArgv(t, dir, "sshpass.argv"), " ")[:8] != "-d 3 ssh" {
		t.Fatalf("sshpass argv = %q", fakeArgv(t, dir, "sshpass.argv"))
	}
	for deadline := time.Now().Add(3 * time.Second); len(pwDirs(t)) > 0 && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
	}
	if d := pwDirs(t); len(d) != 0 {
		t.Fatalf("the pipe directory must be removed: %v", d)
	}
}

// Verifies: REQ-TMX-007
func TestOpenInTmuxWithoutPasswordUsesPlainSSHAndNoPipe(t *testing.T) {
	dir := useFakeTmux(t, false)
	t.Setenv("TMUX_PANE", "")
	if m := openInTmux(tmuxDevice(), "", "")().(tmuxOpenedMsg); m.err != nil {
		t.Fatal(m.err)
	}
	waitForFile(t, filepath.Join(dir, "ssh.argv"))
	if got := strings.Join(fakeArgv(t, dir, "ssh.argv"), " "); !strings.HasSuffix(got, " 100.64.0.9") {
		t.Fatalf("ssh argv = %q", got)
	}
	if strings.Contains(strings.Join(fakeArgv(t, dir, "tmux.argv"), " "), " -t ") {
		t.Fatal("without TMUX_PANE there is no -t")
	}
	if d := pwDirs(t); len(d) != 0 {
		t.Fatalf("no password: no pipe expected: %v", d)
	}
}

// Verifies: REQ-TMX-008
func TestOpenInTmuxRejectsBadInputAndCleansUpWhenTmuxFails(t *testing.T) {
	useFakeTmux(t, true)

	if m := openInTmux(client.Device{Name: "x"}, "root", "")().(tmuxOpenedMsg); m.err == nil || !strings.Contains(m.err.Error(), "no IP") {
		t.Fatalf("no address: %+v", m)
	}
	if m := openInTmux(tmuxDevice(), "-oProxyCommand=x", "")().(tmuxOpenedMsg); m.err == nil || !strings.Contains(m.err.Error(), "username") {
		t.Fatalf("bad username must be rejected before tmux runs: %+v", m)
	}
	bad := tmuxDevice()
	bad.Addresses = []string{"1.2.3.4;rm -rf /"}
	if m := openInTmux(bad, "root", "")().(tmuxOpenedMsg); m.err == nil {
		t.Fatal("bad address must be rejected")
	}

	// the password pipe cannot be created (no usable temp directory)
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
	if m := openInTmux(tmuxDevice(), "root", "pw")().(tmuxOpenedMsg); m.err == nil {
		t.Fatal("a missing temp directory must be reported, not ignored")
	}
	t.Setenv("TMPDIR", t.TempDir())

	t.Setenv("FAKE_TMUX_FAIL", "1")
	if m := openInTmux(tmuxDevice(), "root", "pw")().(tmuxOpenedMsg); m.err == nil || !strings.Contains(m.err.Error(), "tmux split-window failed") {
		t.Fatalf("tmux failure must be reported: %+v", m)
	}
	if d := pwDirs(t); len(d) != 0 {
		t.Fatalf("the pipe must be removed when the pane could not be created: %v", d)
	}
}

// Verifies: REQ-TMX-009
func TestPasswordPipeIsPrivateAndFeederNeverBlocks(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	fifo, dir, err := makePasswordFIFO()
	if err != nil {
		t.Fatal(err)
	}
	dst, _ := os.Stat(dir)
	fst, _ := os.Stat(fifo)
	if dst.Mode().Perm() != 0o700 || fst.Mode().Perm() != 0o600 || fst.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("dir %v, fifo %v", dst.Mode(), fst.Mode())
	}

	// Nobody reads: the feeder gives up at the timeout and removes the directory.
	started := time.Now()
	feedPasswordFIFO(fifo, dir, "pw", 150*time.Millisecond)
	if time.Since(started) > 2*time.Second {
		t.Fatal("the feeder must not block on a missing reader")
	}
	if _, err := os.Stat(dir); err == nil {
		t.Fatal("the directory must be removed after the timeout")
	}

	// The pipe is already gone (the pane removed it): return at once.
	started = time.Now()
	feedPasswordFIFO(filepath.Join(t.TempDir(), "gone"), t.TempDir(), "pw", 5*time.Second)
	if time.Since(started) > time.Second {
		t.Fatal("a vanished pipe must end the wait immediately")
	}
}

// Verifies: REQ-TMX-010
func TestTmuxOpenedMessageIsShownAsInfoOrError(t *testing.T) {
	m := model{}
	updated, _ := m.Update(tmuxOpenedMsg{name: "box"})
	if got := updated.(model).notify.info; !strings.Contains(got, "box") || !strings.Contains(got, "tmux") {
		t.Fatalf("info = %q", got)
	}
	m.notify.sshError = errTest("old")
	updated, _ = m.Update(tmuxOpenedMsg{name: "box"})
	if updated.(model).notify.sshError != nil {
		t.Fatal("success clears the previous error")
	}
	updated, _ = m.Update(tmuxOpenedMsg{name: "box", err: errTest("boom")})
	if updated.(model).notify.sshError == nil {
		t.Fatal("failure is reported")
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }

// Verifies: REQ-TMX-011
func TestSessionFlowInTmuxProbesFirstThenOpensPaneAndFinishes(t *testing.T) {
	dir := useFakeTmux(t, true)
	var opened [][]string
	oldRun := runTmux
	runTmux = func(args ...string) error { opened = append(opened, args); return nil }
	t.Cleanup(func() { runTmux = oldRun })

	for _, c := range []struct {
		name         string
		typed, saved string
	}{
		{"key auth", "", ""},
		{"typed password", "typed", ""},
		{"saved password", "", "saved"},
	} {
		t.Run(c.name, func(t *testing.T) {
			opened = nil
			cap := captureTeaExec(t)
			m := flowModel(t, nil, "admin")
			m.set.tmuxSplit = true
			if c.saved != "" {
				m.ssh.machineCreds["dev-1"] = machineCredential{username: "admin", passwordEncrypted: encFor(t, c.saved)}
			}
			m.ssh.pending = pendingSSH{action: sshActionSession, machineID: "dev-1", machineName: "n", username: "admin", password: c.typed}

			updated, cmd := m.runPendingAttempt()
			msg := runCmdMsg(t, cmd)
			probe, ok := msg.(probeMsg)
			if !ok || cap.called {
				t.Fatalf("in tmux the credentials are proven first, got %#v (interactive exec=%v)", msg, cap.called)
			}

			// The probe succeeded: credentials are saved and the pane opens.
			updated, cmd = updated.(model).handleProbe(probe)
			m = updated.(model)
			if m.ssh.pending.action != sshActionNone {
				t.Fatal("the attempt is complete once the pane is open")
			}
			msgs := runAllCmds(cmd)
			var got *tmuxOpenedMsg
			for _, mm := range msgs {
				if o, ok := mm.(tmuxOpenedMsg); ok {
					got = &o
				}
			}
			if got == nil || got.err != nil || cap.called {
				t.Fatalf("expected a tmux pane and no in-process session: msgs %#v", msgs)
			}
			if len(opened) != 1 || opened[0][0] != "split-window" {
				t.Fatalf("tmux calls: %q", opened)
			}
			_ = dir
		})
	}
}

// Verifies: REQ-TMX-012
func TestSessionFlowOutsideTmuxIsUnchanged(t *testing.T) {
	useFakeTmux(t, true)
	inTmux = func() bool { return false }
	cap := captureTeaExec(t)
	m := flowModel(t, nil, "admin")
	m.set.tmuxSplit = true
	m.ssh.pending = pendingSSH{action: sshActionSession, machineID: "dev-1", machineName: "n", username: "admin"}
	_, cmd := m.runPendingAttempt()
	if _, ok := runCmdMsg(t, cmd).(capturedExecMsg); !ok || !cap.called {
		t.Fatal("outside tmux the session runs in ts-cli's own terminal")
	}
	_ = tea.Quit
}

// runShell runs script with sh -c under the test's private PATH.
func runShell(t *testing.T, script, stdin string) (string, error) {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", script)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// Verifies: REQ-TMX-001, REQ-TMX-002
func TestDefaultTmuxSeamsReadEnvironmentAndRunTmux(t *testing.T) {
	t.Setenv("TMUX", "")
	if inTmux() {
		t.Fatal("empty $TMUX means not inside tmux")
	}
	t.Setenv("TMUX", "/tmp/tmux-1000/default,123,0")
	if !inTmux() {
		t.Fatal("$TMUX set means inside tmux")
	}

	dir := useFakeBins(t, fakeBins{})
	if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte(fakeTmuxScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_TMUX_FAIL", "")
	if err := runTmux("display-message", "hi"); err != nil {
		t.Fatalf("runTmux: %v", err)
	}
	if got := fakeArgv(t, dir, "tmux.argv"); strings.Join(got, " ") != "display-message hi" {
		t.Fatalf("argv = %q", got)
	}
	t.Setenv("FAKE_TMUX_FAIL", "1")
	if err := runTmux("x"); err == nil {
		t.Fatal("a failing tmux must be an error")
	}
}
