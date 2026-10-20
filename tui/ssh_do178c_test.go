package tui

// Requirements-based tests for tui/ssh.go (see verification/requirements/ssh.md
// and docs/do178c/README.md). No test here ever reaches the network: every
// process that gets executed is a small fake ssh/sshpass/xclip script placed on
// a private PATH.

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yatsenko-ihor/ts-cli/client"
	"github.com/yatsenko-ihor/ts-cli/scripts"
	"github.com/yatsenko-ihor/ts-cli/util"
)

// ─── fake executables ─────────────────────────────────────────────────────────

const fakeSSHScript = `#!/bin/sh
d="$FAKE_DIR"
: > "$d/ssh.argv"
for a in "$@"; do printf '%s\0' "$a" >> "$d/ssh.argv"; done
: > "$d/ssh.stdin"
if [ -n "$FAKE_READ_STDIN" ]; then
  while IFS= read -r l; do printf '%s\n' "$l" >> "$d/ssh.stdin"; done
fi
if [ -n "$FAKE_SSH_OUT" ]; then printf '%s\n' "$FAKE_SSH_OUT"; fi
exit ${FAKE_SSH_EXIT:-0}
`

const fakeSSHPassScript = `#!/bin/sh
d="$FAKE_DIR"
: > "$d/sshpass.argv"
for a in "$@"; do printf '%s\0' "$a" >> "$d/sshpass.argv"; done
if IFS= read -r fdline <&3; then printf '%s' "$fdline" > "$d/sshpass.fd3"; else printf 'NOFD3' > "$d/sshpass.fd3"; fi
unset fdline
set > "$d/sshpass.env"
if [ -n "$FAKE_SSHPASS_EXIT" ]; then exit "$FAKE_SSHPASS_EXIT"; fi
shift 2
exec "$@"
`

const fakeXclipScript = `#!/bin/sh
d="$FAKE_DIR"
: > "$d/xclip.stdin"
while IFS= read -r l || [ -n "$l" ]; do printf '%s' "$l" >> "$d/xclip.stdin"; l=; done
exit 0
`

type fakeBins struct{ ssh, sshpass, xclip bool }

// useFakeBins points PATH at a private directory holding only the requested
// fake executables. The scripts need nothing but shell builtins.
func useFakeBins(t *testing.T, f fakeBins) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("needs sh")
	}
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if f.ssh {
		write("ssh", fakeSSHScript)
	}
	if f.sshpass {
		write("sshpass", fakeSSHPassScript)
	}
	if f.xclip {
		write("xclip", fakeXclipScript)
	}
	t.Setenv("PATH", dir)
	t.Setenv("FAKE_DIR", dir)
	for _, k := range []string{"FAKE_READ_STDIN", "FAKE_SSH_OUT", "FAKE_SSH_EXIT", "FAKE_SSHPASS_EXIT"} {
		t.Setenv(k, "")
	}
	return dir
}

func fakeFile(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("fake %s not written (was the process run?): %v", name, err)
	}
	return string(b)
}

func fakeArgv(t *testing.T, dir, name string) []string {
	t.Helper()
	raw := fakeFile(t, dir, name)
	if raw == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(raw, "\x00"), "\x00")
}

func idx(args []string, want string) int {
	for i, a := range args {
		if a == want {
			return i
		}
	}
	return -1
}

func testDevice() client.Device {
	return client.Device{ID: "dev-1", Name: "laptop.example.com", Hostname: "laptop", Addresses: []string{"100.64.0.1"}}
}

type teaExecCapture struct {
	cmd    tea.ExecCommand
	fn     tea.ExecCallback
	called bool
}

type capturedExecMsg struct{}

func captureTeaExec(t *testing.T) *teaExecCapture {
	t.Helper()
	c := &teaExecCapture{}
	old := teaExec
	teaExec = func(cmd tea.ExecCommand, fn tea.ExecCallback) tea.Cmd {
		c.cmd, c.fn, c.called = cmd, fn, true
		return func() tea.Msg { return capturedExecMsg{} }
	}
	t.Cleanup(func() { teaExec = old })
	return c
}

func withOSPipe(t *testing.T, fn func() (*os.File, *os.File, error)) {
	t.Helper()
	old := osPipe
	osPipe = fn
	t.Cleanup(func() { osPipe = old })
}

// ─── classifySSHError ─────────────────────────────────────────────────────────

// Verifies: REQ-SSH-001, REQ-SSH-002, REQ-SSH-003, REQ-SSH-004, REQ-SSH-005
func TestDO178CClassifySSHError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want sshFailure
	}{
		{"nil error", nil, sshFailureNone},
		{"plain error is a start failure", errors.New("boom"), sshFailureExec},
		{"wrapped plain error", fmt.Errorf("wrap: %w", errors.New("boom")), sshFailureExec},
		{"exit 5 bad password", exitError(t, 5), sshFailureBadPassword},
		{"exit 255 connection", exitError(t, 255), sshFailureConnection},
		{"wrapped exit 5 still bad password", fmt.Errorf("wrap: %w", exitError(t, 5)), sshFailureBadPassword},
		{"wrapped exit 255 still connection", fmt.Errorf("wrap: %w", exitError(t, 255)), sshFailureConnection},
		{"boundary exit 1", exitError(t, 1), sshFailureRemoteExit},
		{"boundary exit 4 (just below 5)", exitError(t, 4), sshFailureRemoteExit},
		{"boundary exit 6 (just above 5)", exitError(t, 6), sshFailureRemoteExit},
		{"boundary exit 254 (just below 255)", exitError(t, 254), sshFailureRemoteExit},
		{"exit 127 command not found", exitError(t, 127), sshFailureRemoteExit},
		{"exit 130 interrupted", exitError(t, 130), sshFailureRemoteExit},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := classifySSHError(c.err); got != c.want {
				t.Fatalf("classifySSHError = %v, want %v", got, c.want)
			}
		})
	}
}

// ─── buildSSHTarget ───────────────────────────────────────────────────────────

// Verifies: REQ-SSH-006, REQ-SSH-007, REQ-SSH-008, REQ-SSH-009
func TestDO178CBuildSSHTarget(t *testing.T) {
	long32 := strings.Repeat("a", 32)
	cases := []struct {
		name, user, addr, want string
		wantErr                bool
	}{
		{name: "no username gives bare address", addr: "100.64.0.1", want: "100.64.0.1"},
		{name: "no username ipv6", addr: "fd7a:115c:a1e0::1", want: "fd7a:115c:a1e0::1"},
		{name: "username and ipv4", user: "admin", addr: "100.64.0.1", want: "admin@100.64.0.1"},
		{name: "username and ipv6", user: "admin", addr: "fd7a:115c:a1e0::1", want: "admin@fd7a:115c:a1e0::1"},
		{name: "boundary 1 char username", user: "a", addr: "100.64.0.1", want: "a@100.64.0.1"},
		{name: "boundary 32 char username", user: long32, addr: "100.64.0.1", want: long32 + "@100.64.0.1"},
		{name: "boundary 33 char username", user: long32 + "b", addr: "100.64.0.1", wantErr: true},
		{name: "empty address", user: "admin", addr: "", wantErr: true},
		{name: "hostname instead of ip", user: "admin", addr: "example.com", wantErr: true},
		{name: "address with command separator", addr: "1.2.3.4;rm", wantErr: true},
		{name: "address with option prefix", addr: "-oProxyCommand=x", wantErr: true},
		{name: "address with space", addr: "1.2.3.4 -v", wantErr: true},
		{name: "address with newline", addr: "1.2.3.4\n", wantErr: true},
		{name: "address with cidr", addr: "100.64.0.0/10", wantErr: true},
		{name: "ipv6 with zone", addr: "fe80::1%eth0", wantErr: true},
		{name: "hostile address and empty username still rejected", user: "", addr: "$(id)", wantErr: true},
		{name: "username option injection", user: "-oProxyCommand=x", addr: "100.64.0.1", wantErr: true},
		{name: "username single hyphen", user: "-", addr: "100.64.0.1", wantErr: true},
		{name: "username with at sign", user: "a@b", addr: "100.64.0.1", wantErr: true},
		{name: "username with space", user: "a b", addr: "100.64.0.1", wantErr: true},
		{name: "username with semicolon", user: "a;b", addr: "100.64.0.1", wantErr: true},
		{name: "username with newline", user: "a\nb", addr: "100.64.0.1", wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := buildSSHTarget(c.user, c.addr)
			if c.wantErr {
				if err == nil {
					t.Fatalf("accepted hostile/invalid input, got %q", got)
				}
				if got != "" {
					t.Fatalf("a rejected target must be empty, got %q", got)
				}
				return
			}
			if err != nil || got != c.want {
				t.Fatalf("got %q, %v; want %q", got, err, c.want)
			}
		})
	}
}

// ─── newSSHCommand ────────────────────────────────────────────────────────────

// Verifies: REQ-SSH-010, REQ-SSH-013, REQ-SSH-014
func TestDO178CNewSSHCommandPlainSSH(t *testing.T) {
	useFakeBins(t, fakeBins{}) // no sshpass on PATH

	t.Run("no password, interactive", func(t *testing.T) {
		cmd, cleanup, err := newSSHCommand("admin@100.64.0.1", "", false)
		if err != nil {
			t.Fatal(err)
		}
		defer cleanup()
		if cmd.Args[0] != "ssh" || len(cmd.ExtraFiles) != 0 {
			t.Fatalf("expected plain ssh, got %v (%d extra files)", cmd.Args, len(cmd.ExtraFiles))
		}
		want := []string{"ssh", "-o", "StrictHostKeyChecking=accept-new", "-o", "ConnectTimeout=15", "admin@100.64.0.1"}
		if strings.Join(cmd.Args, "\x00") != strings.Join(want, "\x00") {
			t.Fatalf("args = %q, want %q", cmd.Args, want)
		}
	})
	t.Run("remote args come after target in order", func(t *testing.T) {
		cmd, cleanup, _ := newSSHCommand("100.64.0.1", "", false, "echo", "a b", "")
		defer cleanup()
		n := len(cmd.Args)
		if cmd.Args[n-4] != "100.64.0.1" || cmd.Args[n-3] != "echo" || cmd.Args[n-2] != "a b" || cmd.Args[n-1] != "" {
			t.Fatalf("remote args misplaced: %q", cmd.Args)
		}
	})
	t.Run("batch adds BatchMode", func(t *testing.T) {
		cmd, cleanup, _ := newSSHCommand("100.64.0.1", "", true, "pwd")
		defer cleanup()
		i := idx(cmd.Args, "BatchMode=yes")
		if i < 1 || cmd.Args[i-1] != "-o" {
			t.Fatalf("BatchMode=yes missing: %q", cmd.Args)
		}
		if i > idx(cmd.Args, "100.64.0.1") {
			t.Fatalf("BatchMode option must precede the target: %q", cmd.Args)
		}
	})
	t.Run("not batch has no BatchMode", func(t *testing.T) {
		cmd, cleanup, _ := newSSHCommand("100.64.0.1", "", false)
		defer cleanup()
		if idx(cmd.Args, "BatchMode=yes") != -1 {
			t.Fatalf("interactive run must not force BatchMode: %q", cmd.Args)
		}
	})
	t.Run("password without sshpass is never put on the command line", func(t *testing.T) {
		cmd, cleanup, err := newSSHCommand("100.64.0.1", "s3cret", true, "uptime")
		if err != nil {
			t.Fatal(err)
		}
		defer cleanup()
		if cmd.Args[0] != "ssh" || len(cmd.ExtraFiles) != 0 {
			t.Fatalf("expected plain ssh fallback, got %q", cmd.Args)
		}
		if strings.Contains(strings.Join(cmd.Args, " "), "s3cret") {
			t.Fatalf("password leaked: %q", cmd.Args)
		}
		if idx(cmd.Args, "BatchMode=yes") == -1 {
			t.Fatalf("fallback must still honour batch: %q", cmd.Args)
		}
	})
}

// Verifies: REQ-SSH-011, REQ-SSH-012, REQ-SSH-017
func TestDO178CNewSSHCommandSSHPassUsesPrivatePipe(t *testing.T) {
	dir := useFakeBins(t, fakeBins{ssh: true, sshpass: true})

	passwords := []string{"s3cret-pass", "x", "it's a \"p@ss\" $(id) `id`; -oProxyCommand=x", "  spaced  ", "unié中"}
	for _, pw := range passwords {
		t.Run(pw, func(t *testing.T) {
			cmd, cleanup, err := newSSHCommand("admin@100.64.0.1", pw, true, "uptime", "-a")
			if err != nil {
				t.Fatal(err)
			}
			defer cleanup()

			if filepath.Base(cmd.Path) != "sshpass" || len(cmd.ExtraFiles) != 1 {
				t.Fatalf("expected sshpass with one extra fd, got %q (%d)", cmd.Args, len(cmd.ExtraFiles))
			}
			want := []string{"-d", "3", "ssh", "-o", "StrictHostKeyChecking=accept-new", "-o", "ConnectTimeout=15", "admin@100.64.0.1", "uptime", "-a"}
			if strings.Join(cmd.Args[1:], "\x00") != strings.Join(want, "\x00") {
				t.Fatalf("args = %q, want %q", cmd.Args[1:], want)
			}
			// batch=true is irrelevant when the password goes through sshpass.
			if idx(cmd.Args, "BatchMode=yes") != -1 {
				t.Fatalf("sshpass path must not add BatchMode: %q", cmd.Args)
			}
			// A 1-char password is a substring of almost anything, so
			// substring checks are only meaningful for longer ones; exact
			// element equality is checked for all.
			leaks := func(hay []string) bool {
				if idx(hay, pw) != -1 {
					return true
				}
				return len(pw) >= 4 && strings.Contains(strings.Join(hay, "\x00"), pw)
			}
			if leaks(cmd.Args) {
				t.Fatalf("password leaked into argv: %q", cmd.Args)
			}
			if leaks(cmd.Env) {
				t.Fatal("password leaked into env")
			}

			if err := cmd.Run(); err != nil {
				t.Fatal(err)
			}
			if got := fakeFile(t, dir, "sshpass.fd3"); got != pw {
				t.Fatalf("fd 3 carried %q, want %q", got, pw)
			}
			if leaks(strings.Split(fakeFile(t, dir, "sshpass.env"), "\n")) {
				t.Fatal("password visible in the child's environment")
			}
			if leaks(fakeArgv(t, dir, "sshpass.argv")) {
				t.Fatal("password visible in sshpass's argv")
			}
			sshArgs := fakeArgv(t, dir, "ssh.argv")
			if leaks(sshArgs) {
				t.Fatal("password visible in ssh's argv")
			}
			if sshArgs[len(sshArgs)-2] != "uptime" || sshArgs[len(sshArgs)-1] != "-a" {
				t.Fatalf("ssh got wrong args: %q", sshArgs)
			}
		})
	}

	t.Run("cleanup closes the read end", func(t *testing.T) {
		cmd, cleanup, err := newSSHCommand("100.64.0.1", "pw", false)
		if err != nil {
			t.Fatal(err)
		}
		r := cmd.ExtraFiles[0]
		cleanup()
		if _, err := r.Read(make([]byte, 1)); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("read end still open after cleanup: %v", err)
		}
	})
	t.Run("plain ssh cleanup is a harmless no-op", func(t *testing.T) {
		_, cleanup, err := newSSHCommand("100.64.0.1", "", false)
		if err != nil {
			t.Fatal(err)
		}
		cleanup()
		cleanup()
	})
}

// Verifies: REQ-SSH-012
func TestDO178CNewSSHCommandEmptyPasswordNeverUsesSSHPass(t *testing.T) {
	useFakeBins(t, fakeBins{ssh: true, sshpass: true})
	cmd, cleanup, err := newSSHCommand("100.64.0.1", "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if cmd.Args[0] != "ssh" || len(cmd.ExtraFiles) != 0 {
		t.Fatalf("sshpass must not be used without a password: %q", cmd.Args)
	}
}

// Verifies: REQ-SSH-015, REQ-SSH-016
func TestDO178CNewSSHCommandPipeFailures(t *testing.T) {
	useFakeBins(t, fakeBins{ssh: true, sshpass: true})

	t.Run("pipe creation fails", func(t *testing.T) {
		withOSPipe(t, func() (*os.File, *os.File, error) { return nil, nil, errors.New("no fds left") })
		cmd, cleanup, err := newSSHCommand("100.64.0.1", "pw", true)
		if err == nil || !strings.Contains(err.Error(), "failed to create password pipe") || !strings.Contains(err.Error(), "no fds left") {
			t.Fatalf("err = %v", err)
		}
		if cmd != nil || cleanup != nil {
			t.Fatal("no command or cleanup may be returned on failure")
		}
	})
	t.Run("pipe write fails and both ends are released", func(t *testing.T) {
		var r, w *os.File
		withOSPipe(t, func() (*os.File, *os.File, error) {
			var err error
			r, w, err = os.Pipe()
			r.Close() // reader gone: the write fails with EPIPE
			return r, w, err
		})
		cmd, cleanup, err := newSSHCommand("100.64.0.1", "pw", true)
		if err == nil || !strings.Contains(err.Error(), "failed to write password pipe") {
			t.Fatalf("err = %v", err)
		}
		if cmd != nil || cleanup != nil {
			t.Fatal("no command or cleanup may be returned on failure")
		}
		if _, err := w.Write([]byte("x")); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("write end not closed after failure: %v", err)
		}
	})
}

// ─── bannerExec ───────────────────────────────────────────────────────────────

// Verifies: REQ-SSH-018, REQ-SSH-019
func TestDO178CBannerExec(t *testing.T) {
	dir := useFakeBins(t, fakeBins{ssh: true})
	t.Setenv("FAKE_SSH_OUT", "remote-output")

	t.Run("banner then command output, wiring through setters", func(t *testing.T) {
		b := &bannerExec{banner: "BANNER\n", cmd: exec.Command("ssh", "x")}
		var out, errw bytes.Buffer
		b.SetStdin(strings.NewReader("in"))
		b.SetStdout(&out)
		b.SetStderr(&errw)
		if b.cmd.Stdin == nil || b.cmd.Stdout != &out || b.cmd.Stderr != &errw || b.stdout != &out {
			t.Fatal("setters did not wire the process streams")
		}
		if err := b.Run(); err != nil {
			t.Fatal(err)
		}
		if out.String() != "BANNER\nremote-output\n" {
			t.Fatalf("output = %q", out.String())
		}
		if got := fakeArgv(t, dir, "ssh.argv"); len(got) != 1 || got[0] != "x" {
			t.Fatalf("argv = %q", got)
		}
	})
	t.Run("empty banner prints nothing extra", func(t *testing.T) {
		b := &bannerExec{cmd: exec.Command("ssh", "x")}
		var out bytes.Buffer
		b.SetStdout(&out)
		if err := b.Run(); err != nil {
			t.Fatal(err)
		}
		if out.String() != "remote-output\n" {
			t.Fatalf("output = %q", out.String())
		}
	})
	t.Run("no stdout set prints no banner and does not panic", func(t *testing.T) {
		b := &bannerExec{banner: "BANNER\n", cmd: exec.Command("ssh", "x")}
		if err := b.Run(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("process error is returned unchanged", func(t *testing.T) {
		t.Setenv("FAKE_SSH_EXIT", "255")
		b := &bannerExec{cmd: exec.Command("ssh", "x")}
		if got := classifySSHError(b.Run()); got != sshFailureConnection {
			t.Fatalf("failure = %v", got)
		}
	})
}

// ─── sshToDevice ──────────────────────────────────────────────────────────────

func runCmdMsg(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("nil cmd")
	}
	return cmd()
}

// Verifies: REQ-SSH-020, REQ-SSH-021, REQ-SSH-022
func TestDO178CSSHToDeviceRejectsBadInputWithoutStartingAnything(t *testing.T) {
	useFakeBins(t, fakeBins{ssh: true, sshpass: true})
	cap := captureTeaExec(t)
	m := model{}

	cases := []struct {
		name     string
		dev      client.Device
		user, pw string
		pipeFail bool
	}{
		{name: "no addresses", dev: client.Device{Name: "x"}},
		{name: "nil address list with username", dev: client.Device{}, user: "root"},
		{name: "hostname address", dev: client.Device{Addresses: []string{"evil.example.com"}}},
		{name: "address injection", dev: client.Device{Addresses: []string{"1.2.3.4;rm"}}, user: "root"},
		{name: "option-injection username", dev: testDevice(), user: "-oProxyCommand=x"},
		{name: "password pipe cannot be created", dev: testDevice(), user: "root", pw: "pw", pipeFail: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.pipeFail {
				withOSPipe(t, func() (*os.File, *os.File, error) { return nil, nil, errors.New("nope") })
			}
			msg := runCmdMsg(t, m.sshToDevice(c.dev, c.user, c.pw))
			got, ok := msg.(sshMsg)
			if !ok || got.err == nil || got.failure != sshFailureExec {
				t.Fatalf("msg = %#v, want sshMsg with sshFailureExec", msg)
			}
			if cap.called {
				t.Fatal("no process may be handed to the terminal for invalid input")
			}
		})
	}
	// first address only: the device's second address is irrelevant
	t.Run("only the first address is used", func(t *testing.T) {
		d := testDevice()
		d.Addresses = []string{"100.64.0.9", "bad;addr"}
		m.sshToDevice(d, "root", "")
		if !cap.called || !strings.Contains(strings.Join(cap.cmd.(*bannerExec).cmd.Args, " "), "root@100.64.0.9") {
			t.Fatal("expected the first address to be used")
		}
	})
}

// Verifies: REQ-SSH-023, REQ-SSH-024, REQ-SSH-025, REQ-SSH-026
func TestDO178CSSHToDeviceBuildsSessionAndClassifiesOutcome(t *testing.T) {
	// exit errors need a real sh, so create them before PATH is replaced
	exitErrs := map[int]error{5: exitError(t, 5), 255: exitError(t, 255), 1: exitError(t, 1)}
	dir := useFakeBins(t, fakeBins{ssh: true, sshpass: true})
	cap := captureTeaExec(t)
	m := model{}
	dev := testDevice()

	t.Run("saved password goes through fd 3 and banner names the target", func(t *testing.T) {
		cmd := m.sshToDevice(dev, "root", "hunter2")
		if _, ok := runCmdMsg(t, cmd).(capturedExecMsg); !ok {
			t.Fatal("expected the process to be handed to tea.Exec")
		}
		b := cap.cmd.(*bannerExec)
		if b.banner != "\nConnection to laptop.example.com (root@100.64.0.1)\n" {
			t.Fatalf("banner = %q", b.banner)
		}
		var out bytes.Buffer
		b.SetStdout(&out)
		if err := b.Run(); err != nil {
			t.Fatal(err)
		}
		if got := fakeFile(t, dir, "sshpass.fd3"); got != "hunter2" {
			t.Fatalf("fd3 = %q", got)
		}
		if strings.Contains(strings.Join(b.cmd.Args, " "), "hunter2") {
			t.Fatal("password in argv")
		}
		if msg := cap.fn(nil); msg != (sshMsg{}) {
			t.Fatalf("clean exit must give an empty sshMsg, got %#v", msg)
		}
		// cleanup released the pipe
		if _, err := b.cmd.ExtraFiles[0].Read(make([]byte, 1)); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("pipe not released by the completion callback: %v", err)
		}
	})
	t.Run("banner falls back to hostname", func(t *testing.T) {
		d := dev
		d.Name = ""
		m.sshToDevice(d, "", "")
		if got := cap.cmd.(*bannerExec).banner; got != "\nConnection to laptop (100.64.0.1)\n" {
			t.Fatalf("banner = %q", got)
		}
	})
	t.Run("no password and sshpass installed adds BatchMode", func(t *testing.T) {
		m.sshToDevice(dev, "root", "")
		args := cap.cmd.(*bannerExec).cmd.Args
		if args[0] != "ssh" || idx(args, "BatchMode=yes") == -1 {
			t.Fatalf("args = %q", args)
		}
	})
	t.Run("callback classifies each exit code", func(t *testing.T) {
		m.sshToDevice(dev, "root", "")
		for code, want := range map[int]sshFailure{5: sshFailureBadPassword, 255: sshFailureConnection, 1: sshFailureRemoteExit} {
			msg := cap.fn(exitErrs[code]).(sshMsg)
			if msg.failure != want || msg.err == nil {
				t.Fatalf("exit %d -> %#v, want %v", code, msg, want)
			}
		}
		if msg := cap.fn(errors.New("start failed")).(sshMsg); msg.failure != sshFailureExec {
			t.Fatalf("plain error -> %#v", msg)
		}
	})
}

// Verifies: REQ-SSH-023
func TestDO178CSSHToDeviceBatchModeNeedsSSHPassAndNoPassword(t *testing.T) {
	cap := captureTeaExec(t)
	m := model{}
	dev := testDevice()

	cases := []struct {
		name      string
		sshpass   bool
		password  string
		wantBatch bool
	}{
		{"empty password, sshpass present", true, "", true},
		{"empty password, sshpass absent", false, "", false},
		{"password, sshpass present", true, "pw", false},
		{"password, sshpass absent", false, "pw", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			useFakeBins(t, fakeBins{ssh: true, sshpass: c.sshpass})
			m.sshToDevice(dev, "root", c.password)
			args := cap.cmd.(*bannerExec).cmd.Args
			if got := idx(args, "BatchMode=yes") != -1; got != c.wantBatch {
				t.Fatalf("BatchMode present = %v, want %v (args %q)", got, c.wantBatch, args)
			}
		})
	}
}

// ─── probeSSH ─────────────────────────────────────────────────────────────────

// Verifies: REQ-SSH-027, REQ-SSH-028
func TestDO178CProbeSSHInvalidInput(t *testing.T) {
	useFakeBins(t, fakeBins{ssh: true, sshpass: true})
	cases := []struct {
		name     string
		dev      client.Device
		user     string
		pw       string
		pipeFail bool
	}{
		{name: "no addresses", dev: client.Device{}},
		{name: "bad address", dev: client.Device{Addresses: []string{"1.2.3.4;rm"}}},
		{name: "bad username", dev: testDevice(), user: "-oProxyCommand=x", pw: "pw"},
		{name: "pipe failure", dev: testDevice(), user: "root", pw: "pw", pipeFail: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.pipeFail {
				withOSPipe(t, func() (*os.File, *os.File, error) { return nil, nil, errors.New("nope") })
			}
			msg := runCmdMsg(t, probeSSH(c.dev, c.user, c.pw)).(probeMsg)
			if msg.err == nil || msg.failure != sshFailureExec {
				t.Fatalf("msg = %#v", msg)
			}
		})
	}
}

// Verifies: REQ-SSH-029, REQ-SSH-030, REQ-SSH-031
func TestDO178CProbeSSHRunsTrueNonInteractively(t *testing.T) {
	dir := useFakeBins(t, fakeBins{ssh: true, sshpass: true})

	t.Run("success with password via fd 3", func(t *testing.T) {
		msg := runCmdMsg(t, probeSSH(testDevice(), "root", "pw-1")).(probeMsg)
		if msg.err != nil || msg.failure != sshFailureNone {
			t.Fatalf("msg = %#v", msg)
		}
		if got := fakeFile(t, dir, "sshpass.fd3"); got != "pw-1" {
			t.Fatalf("fd3 = %q", got)
		}
		args := fakeArgv(t, dir, "ssh.argv")
		if args[len(args)-2] != "root@100.64.0.1" || args[len(args)-1] != "true" {
			t.Fatalf("ssh args = %q", args)
		}
		if strings.Contains(strings.Join(args, "\x00"), "pw-1") {
			t.Fatal("password in argv")
		}
	})
	t.Run("without password uses BatchMode", func(t *testing.T) {
		msg := runCmdMsg(t, probeSSH(testDevice(), "", "")).(probeMsg)
		if msg.err != nil {
			t.Fatalf("msg = %#v", msg)
		}
		args := fakeArgv(t, dir, "ssh.argv")
		if idx(args, "BatchMode=yes") == -1 || args[len(args)-2] != "100.64.0.1" || args[len(args)-1] != "true" {
			t.Fatalf("ssh args = %q", args)
		}
	})
	t.Run("exit codes are classified", func(t *testing.T) {
		for code, want := range map[string]sshFailure{"255": sshFailureConnection, "1": sshFailureRemoteExit} {
			t.Setenv("FAKE_SSH_EXIT", code)
			msg := runCmdMsg(t, probeSSH(testDevice(), "root", "")).(probeMsg)
			if msg.failure != want || msg.err == nil {
				t.Fatalf("exit %s -> %#v", code, msg)
			}
		}
		t.Setenv("FAKE_SSH_EXIT", "")
		t.Setenv("FAKE_SSHPASS_EXIT", "5")
		msg := runCmdMsg(t, probeSSH(testDevice(), "root", "bad")).(probeMsg)
		if msg.failure != sshFailureBadPassword {
			t.Fatalf("sshpass exit 5 -> %#v", msg)
		}
	})
	t.Run("pipe is released after the probe", func(t *testing.T) {
		var r *os.File
		withOSPipe(t, func() (*os.File, *os.File, error) {
			var w *os.File
			var err error
			r, w, err = os.Pipe()
			return r, w, err
		})
		t.Setenv("FAKE_SSHPASS_EXIT", "")
		runCmdMsg(t, probeSSH(testDevice(), "root", "pw"))
		if _, err := r.Read(make([]byte, 1)); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("pipe still open: %v", err)
		}
	})
	t.Run("ssh binary missing is an exec failure", func(t *testing.T) {
		useFakeBins(t, fakeBins{})
		msg := runCmdMsg(t, probeSSH(testDevice(), "root", "")).(probeMsg)
		if msg.failure != sshFailureExec || msg.err == nil {
			t.Fatalf("msg = %#v", msg)
		}
	})
}

// ─── startRemoteJob ───────────────────────────────────────────────────────────

func drainJob(t *testing.T, job *runJob) (lines []string, res runResult) {
	t.Helper()
	for {
		switch msg := waitRunEvent(job)().(type) {
		case runLinesMsg:
			lines = append(lines, msg.lines...)
		case runDoneMsg:
			return lines, msg.res
		}
	}
}

// Verifies: REQ-SSH-032, REQ-SSH-033, REQ-SSH-034, REQ-SSH-035
func TestDO178CStartRemoteJobRejectsInvalidRequests(t *testing.T) {
	useFakeBins(t, fakeBins{ssh: true, sshpass: true})
	cmdSpec := sshJob{action: sshActionCommand, command: "uptime"}
	cases := []struct {
		name     string
		dev      client.Device
		user     string
		pw       string
		spec     sshJob
		wantErr  string
		pipeFail bool
	}{
		{name: "no addresses", dev: client.Device{}, spec: cmdSpec, wantErr: "no IP addresses"},
		{name: "bad address", dev: client.Device{Addresses: []string{"1.2.3.4;rm"}}, spec: cmdSpec, wantErr: "not a valid IP"},
		{name: "bad username", dev: testDevice(), user: "-oProxyCommand=x", spec: cmdSpec, wantErr: "invalid SSH username"},
		{name: "script action without script", dev: testDevice(), spec: sshJob{action: sshActionScript}, wantErr: "no script selected"},
		{name: "none action", dev: testDevice(), spec: sshJob{action: sshActionNone}, wantErr: "nothing to run"},
		{name: "interactive session is not a streamed job", dev: testDevice(), spec: sshJob{action: sshActionSession}, wantErr: "nothing to run"},
		{name: "pipe failure", dev: testDevice(), pw: "pw", spec: cmdSpec, wantErr: "failed to create password pipe", pipeFail: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.pipeFail {
				withOSPipe(t, func() (*os.File, *os.File, error) { return nil, nil, errors.New("nope") })
			}
			job, err := startRemoteJob(c.dev, c.user, c.pw, c.spec)
			if err == nil || !strings.Contains(err.Error(), c.wantErr) || job != nil {
				t.Fatalf("job=%v err=%v, want error containing %q", job, err, c.wantErr)
			}
		})
	}
	t.Run("ssh cannot be started", func(t *testing.T) {
		useFakeBins(t, fakeBins{})
		job, err := startRemoteJob(testDevice(), "root", "", cmdSpec)
		if err == nil || !strings.Contains(err.Error(), "could not start ssh") || job != nil {
			t.Fatalf("job=%v err=%v", job, err)
		}
	})
}

// Verifies: REQ-SSH-036, REQ-SSH-037
func TestDO178CStartRemoteJobCommandAndScript(t *testing.T) {
	dir := useFakeBins(t, fakeBins{ssh: true, sshpass: true})
	t.Setenv("FAKE_SSH_OUT", "result-line")

	t.Run("command is one remote argument, kept literal", func(t *testing.T) {
		hostile := "echo 'a b'; $(id) `id` \"q\" \n -oProxyCommand=x"
		job, err := startRemoteJob(testDevice(), "root", "", sshJob{action: sshActionCommand, command: hostile})
		if err != nil {
			t.Fatal(err)
		}
		lines, res := drainJob(t, job)
		if len(lines) != 1 || lines[0] != "result-line" || res.err != nil {
			t.Fatalf("lines=%v res=%+v", lines, res)
		}
		args := fakeArgv(t, dir, "ssh.argv")
		if args[len(args)-2] != "root@100.64.0.1" || args[len(args)-1] != hostile {
			t.Fatalf("command was not passed as exactly one trailing argument: %q", args)
		}
		if idx(args, "BatchMode=yes") == -1 {
			t.Fatalf("jobs must be non-interactive: %q", args)
		}
	})
	t.Run("script content goes to stdin and arguments are quoted in the snippet", func(t *testing.T) {
		t.Setenv("FAKE_READ_STDIN", "1")
		sc := scripts.Parse("a.sh", []byte("#!/bin/sh\necho \"$1\"\n"))
		job, err := startRemoteJob(testDevice(), "root", "pw-9", sshJob{action: sshActionScript, script: &sc, args: []string{"it's", "$(id)"}})
		if err != nil {
			t.Fatal(err)
		}
		_, res := drainJob(t, job)
		if res.err != nil {
			t.Fatalf("res = %+v", res)
		}
		args := fakeArgv(t, dir, "ssh.argv")
		if want := sc.RemoteCommand([]string{"it's", "$(id)"}); args[len(args)-1] != want {
			t.Fatalf("remote = %q, want %q", args[len(args)-1], want)
		}
		if got := fakeFile(t, dir, "ssh.stdin"); got != "#!/bin/sh\necho \"$1\"\n" {
			t.Fatalf("stdin = %q", got)
		}
		if got := fakeFile(t, dir, "sshpass.fd3"); got != "pw-9" {
			t.Fatalf("fd3 = %q", got)
		}
		if strings.Contains(strings.Join(args, "\x00"), "pw-9") {
			t.Fatal("password in argv")
		}
	})
}

// ─── resolveRemoteOutputPath ──────────────────────────────────────────────────

// Verifies: REQ-SSH-038, REQ-SSH-039, REQ-SSH-040
func TestDO178CResolveRemoteOutputPath(t *testing.T) {
	flow := flowModel(t, nil, "root")
	dir := useFakeBins(t, fakeBins{ssh: true, sshpass: true})
	dev := testDevice()

	t.Run("joins entry onto remote cwd and cleans", func(t *testing.T) {
		t.Setenv("FAKE_SSH_OUT", "  /home/root\n")
		cases := map[string]string{
			"file.txt":    "/home/root/file.txt",
			"./a//b/../c": "/home/root/a/c",
			"/etc/passwd": "/home/root/etc/passwd", // entries are relative by contract
			"../x":        "/home/x",
			"":            "/home/root",
		}
		for entry, want := range cases {
			got, err := flow.resolveRemoteOutputPath(dev, entry)
			if err != nil || got != want {
				t.Fatalf("entry %q -> %q, %v; want %q", entry, got, err, want)
			}
		}
		args := fakeArgv(t, dir, "ssh.argv")
		if args[len(args)-2] != "root@100.64.0.1" || args[len(args)-1] != "pwd" || idx(args, "BatchMode=yes") == -1 {
			t.Fatalf("ssh args = %q", args)
		}
	})
	t.Run("uses the saved password through fd 3", func(t *testing.T) {
		fm := flowModel(t, nil, "") // sets the isolated config dir (and so the key)
		fm.ssh.machineCreds["dev-1"] = machineCredential{username: "pi", passwordEncrypted: mustEncrypt(t, "saved-pw")}
		t.Setenv("FAKE_SSH_OUT", "/srv")
		if got, err := fm.resolveRemoteOutputPath(dev, "f"); err != nil || got != "/srv/f" {
			t.Fatalf("got %q, %v", got, err)
		}
		if got := fakeFile(t, dir, "sshpass.fd3"); got != "saved-pw" {
			t.Fatalf("fd3 = %q", got)
		}
		args := fakeArgv(t, dir, "sshpass.argv")
		if !strings.Contains(strings.Join(args, " "), "pi@100.64.0.1") {
			t.Fatalf("saved username not used: %q", args)
		}
	})
	t.Run("empty cwd is an error", func(t *testing.T) {
		t.Setenv("FAKE_SSH_OUT", "   ")
		if got, err := flow.resolveRemoteOutputPath(dev, "f"); err == nil || got != "" {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	t.Run("ssh failure is an error", func(t *testing.T) {
		t.Setenv("FAKE_SSH_OUT", "/x")
		t.Setenv("FAKE_SSH_EXIT", "255")
		if got, err := flow.resolveRemoteOutputPath(dev, "f"); err == nil || got != "" {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	t.Run("invalid device or username", func(t *testing.T) {
		if _, err := flow.resolveRemoteOutputPath(client.Device{}, "f"); err == nil {
			t.Fatal("device without address accepted")
		}
		if _, err := flow.resolveRemoteOutputPath(client.Device{Addresses: []string{"a;b"}}, "f"); err == nil {
			t.Fatal("hostile address accepted")
		}
		bad := flowModel(t, nil, "-oProxyCommand=x")
		if _, err := bad.resolveRemoteOutputPath(dev, "f"); err == nil {
			t.Fatal("hostile username accepted")
		}
	})
	t.Run("pipe failure is an error", func(t *testing.T) {
		withOSPipe(t, func() (*os.File, *os.File, error) { return nil, nil, errors.New("nope") })
		fm := flowModel(t, nil, "root")
		fm.ssh.pending = pendingSSH{machineID: "dev-1", password: "typed"}
		if _, err := fm.resolveRemoteOutputPath(dev, "f"); err == nil {
			t.Fatal("expected error")
		}
	})
}

// ─── copySSHCommand ───────────────────────────────────────────────────────────

// Verifies: REQ-SSH-041, REQ-SSH-042, REQ-SSH-043
func TestDO178CCopySSHCommand(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("uses a fake xclip")
	}
	dir := useFakeBins(t, fakeBins{xclip: true})

	mk := func(user string, addrs ...string) model {
		d := client.Device{ID: "dev-1", Name: "n", Addresses: addrs}
		m := flowModel(t, nil, user)
		m.list = deviceList{devices: []client.Device{d}, filteredDevices: []client.Device{d}}
		return m
	}
	t.Run("copies ssh user@addr and nothing secret", func(t *testing.T) {
		m := mk("root", "100.64.0.1")
		m.ssh.pending = pendingSSH{machineID: "dev-1", password: "typed-secret"}
		msg := m.copySSHCommand(0)().(copiedMsg)
		if !msg.success || msg.text != "ssh root@100.64.0.1" {
			t.Fatalf("msg = %#v", msg)
		}
		if got := fakeFile(t, dir, "xclip.stdin"); got != "ssh root@100.64.0.1" || strings.Contains(got, "typed-secret") {
			t.Fatalf("clipboard = %q", got)
		}
	})
	t.Run("no username copies bare address", func(t *testing.T) {
		msg := mk("", "100.64.0.1").copySSHCommand(0)().(copiedMsg)
		if !msg.success || msg.text != "ssh 100.64.0.1" {
			t.Fatalf("msg = %#v", msg)
		}
	})
	t.Run("no address", func(t *testing.T) {
		if msg := mk("root").copySSHCommand(0)().(copiedMsg); msg.success || msg.text != "" {
			t.Fatalf("msg = %#v", msg)
		}
	})
	t.Run("hostile address or username is never copied", func(t *testing.T) {
		if msg := mk("root", "1.2.3.4;rm").copySSHCommand(0)().(copiedMsg); msg.success || msg.text != "" {
			t.Fatalf("msg = %#v", msg)
		}
		if msg := mk("-oProxyCommand=x", "100.64.0.1").copySSHCommand(0)().(copiedMsg); msg.success || msg.text != "" {
			t.Fatalf("msg = %#v", msg)
		}
	})
}

// helper shared by the other DO-178C test files in this package
func mustEncrypt(t *testing.T, pw string) string {
	t.Helper()
	enc, err := util.EncryptPassword(pw)
	if err != nil {
		t.Fatal(err)
	}
	return enc
}
