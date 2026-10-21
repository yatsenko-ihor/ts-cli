//go:build !windows

package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yatsenko-ihor/ts-cli/client"
)

// Requirements-based tests for file copy (verification/requirements/copy.md).

const fakeSCPScript = `#!/bin/sh
d="$FAKE_DIR"
# Several machines run in parallel: write a private file, then rename it into
# place so the recorded argv is always one invocation's, never a mixture.
: > "$d/scp.$$"
for a in "$@"; do printf '%s\0' "$a" >> "$d/scp.$$"; done
mv "$d/scp.$$" "$d/scp.argv"
exit ${FAKE_SCP_EXIT:-0}
`

// copyEnv gives a private PATH with fake scp, ssh, sshpass and tailscale, and
// stubs the "is tailscale installed" probe so launches go straight through.
func copyEnv(t *testing.T) string {
	t.Helper()
	dir := useFakeBins(t, fakeBins{ssh: true, sshpass: true})
	for name, body := range map[string]string{
		"scp":       fakeSCPScript,
		"tailscale": "#!/bin/sh\nexit 0\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("/bin/mv", filepath.Join(dir, "mv")); err != nil { // the fake scp renames its record
		t.Fatal(err)
	}
	old := tailscaleAvailable
	tailscaleAvailable = func() bool { return false } // no pre-flight ping
	t.Cleanup(func() { tailscaleAvailable = old })
	t.Setenv("FAKE_SCP_EXIT", "")
	return dir
}

func copyModel(t *testing.T, marked ...string) model {
	t.Helper()
	now := time.Now()
	devs := []client.Device{
		{ID: "a", Name: "alpha.ts.net", Addresses: []string{"100.64.0.1"}, LastSeen: now},
		{ID: "b", Name: "bravo.ts.net", Addresses: []string{"100.64.0.2"}, LastSeen: now},
	}
	m := model{
		set:  loadUISettings(nil),
		list: deviceList{devices: devs, filteredDevices: devs},
		ssh: ssh{username: "root", machineCreds: map[string]machineCredential{
			"a": {username: "root"}, "b": {username: "root"},
		}},
	}
	for _, k := range marked {
		m.list.marked = map[string]bool{}
		for _, kk := range marked {
			m.list.marked[kk] = true
		}
		_ = k
	}
	return m
}

// Verifies: REQ-CPY-001
func TestCopySpecLabelAndIPv6Brackets(t *testing.T) {
	if got := (copySpec{local: "/tmp/data/report.pdf", remote: "docs/"}).label(); got != "upload report.pdf → docs/" {
		t.Fatalf("label = %q", got)
	}
	if got := (copySpec{local: "/tmp/out", remote: "/var/log/syslog", download: true}).label(); got != "download /var/log/syslog → /tmp/out" {
		t.Fatalf("label = %q", got)
	}
	for in, want := range map[string]string{
		"root@100.64.0.5":     "root@100.64.0.5",
		"root@fd7a:115c::1":   "root@[fd7a:115c::1]",
		"fd7a:115c::1":        "[fd7a:115c::1]",
		"root@[fd7a:115c::1]": "root@[fd7a:115c::1]",
		"100.64.0.5":          "100.64.0.5",
	} {
		if got := bracketIPv6(in); got != want {
			t.Errorf("bracketIPv6(%q) = %q, want %q", in, got, want)
		}
	}
}

// Verifies: REQ-CPY-002, REQ-CPY-003
func TestNewSCPCommandBuildsSafeArgumentLists(t *testing.T) {
	useFakeBins(t, fakeBins{ssh: true, sshpass: true})
	wd, _ := os.Getwd()

	cmd, cleanup, err := newSCPCommand("root@100.64.0.5", "", copySpec{local: "-oProxyCommand=evil", remote: "dest dir"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	args := strings.Join(cmd.Args[1:], "\x00")
	want := strings.Join([]string{"-o", "StrictHostKeyChecking=accept-new", "-o", "ConnectTimeout=15", "-r", "-o", "BatchMode=yes", "--", filepath.Join(wd, "-oProxyCommand=evil"), "root@100.64.0.5:dest dir"}, "\x00")
	if args != want {
		t.Fatalf("args = %q\nwant   %q", cmd.Args[1:], strings.Split(want, "\x00"))
	}
	if !filepath.IsAbs(cmd.Args[len(cmd.Args)-2]) {
		t.Fatal("the local path must be absolute so it cannot be read as an option or host:path")
	}

	// download: remote first, local second; IPv6 bracketed
	cmd, cleanup, err = newSCPCommand("root@fd7a:115c::1", "", copySpec{local: "/tmp/out", remote: "/etc/hosts", download: true})
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	n := len(cmd.Args)
	if cmd.Args[n-3] != "--" || cmd.Args[n-2] != "root@[fd7a:115c::1]:/etc/hosts" || cmd.Args[n-1] != "/tmp/out" {
		t.Fatalf("download args = %q", cmd.Args)
	}

	// with a password: through sshpass fd 3, never in argv
	cmd, cleanup, err = newSCPCommand("root@100.64.0.5", "S3cret", copySpec{local: "/tmp/x", remote: "."})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if !strings.HasSuffix(cmd.Path, "sshpass") || cmd.Args[1] != "-d" || cmd.Args[2] != "3" || cmd.Args[3] != "scp" || len(cmd.ExtraFiles) != 1 {
		t.Fatalf("expected sshpass -d 3 scp, got %q", cmd.Args)
	}
	if strings.Contains(strings.Join(cmd.Args, " "), "S3cret") {
		t.Fatal("the password must not appear in argv")
	}
}

// Verifies: REQ-CPY-004
func TestNewSCPCommandRejectsBadInput(t *testing.T) {
	for name, c := range map[string]copySpec{
		"no local":     {remote: "x"},
		"no remote":    {local: "/tmp/x"},
		"newline":      {local: "/tmp/x", remote: "a\nb"},
		"carriage ret": {local: "/tmp/x", remote: "a\rb"},
		"nul":          {local: "/tmp/x", remote: "a\x00b"},
	} {
		if _, _, err := newSCPCommand("root@100.64.0.5", "", c); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}

	// a relative path cannot be resolved when the working directory is gone
	gone := t.TempDir()
	wd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(gone); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(gone); err != nil {
		t.Skip("cannot remove the working directory here")
	}
	if _, _, err := newSCPCommand("root@100.64.0.5", "", copySpec{local: "relative", remote: "x"}); err == nil || !strings.Contains(err.Error(), "cannot resolve") {
		t.Fatalf("err = %v", err)
	}
}

// Verifies: REQ-CPY-005
func TestExpandHome(t *testing.T) {
	t.Setenv("HOME", "/home/me")
	for in, want := range map[string]string{
		"~":          "/home/me",
		"~/docs/a":   "/home/me/docs/a",
		"  /abs/p  ": "/abs/p",
		"~other/x":   "~other/x",
		"rel/p":      "rel/p",
		"":           "",
	} {
		if got := expandHome(in); got != want {
			t.Errorf("expandHome(%q) = %q, want %q", in, got, want)
		}
	}
	t.Setenv("HOME", "")
	if got := expandHome("~/x"); got != "~/x" {
		t.Errorf("without a home directory the path stays: %q", got)
	}
}

// Verifies: REQ-CPY-006
func TestUploadPromptsThenCopiesToEveryMarkedMachine(t *testing.T) {
	dir := copyEnv(t)
	src := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(src, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := copyModel(t, "a", "b")

	updated, _ := m.promptUpload()
	m = updated.(model)
	if m.input.mode != inputText || !strings.Contains(m.input.label, "2 machines") {
		t.Fatalf("first prompt: %+v", m.input)
	}
	// empty and missing paths end the flow with a message, not a copy
	if u, cmd := m.input.onSubmit(m, "  "); cmd != nil || u.(model).input.mode != inputNone || u.(model).notify.sshError != nil {
		t.Fatal("empty input closes quietly")
	}
	if u, cmd := m.input.onSubmit(m, filepath.Join(t.TempDir(), "nope")); cmd != nil || u.(model).notify.sshError == nil {
		t.Fatal("a missing file must be reported")
	}

	updated, _ = m.input.onSubmit(m, src)
	m = updated.(model)
	if m.input.value != "." || !strings.Contains(m.input.label, "remote path") {
		t.Fatalf("second prompt: %+v", m.input)
	}
	updated, cmd := m.input.onSubmit(m, "  ") // empty means the home directory
	m = updated.(model)
	if cmd == nil || m.run.spec.action != sshActionCopy || m.run.spec.copy.local != src || m.run.spec.copy.remote != "." || m.run.spec.copy.download {
		t.Fatalf("the upload must start: spec %+v", m.run.spec)
	}
	if !m.run.multi || len(m.run.runs) != 2 || m.run.label != "upload report.txt → ." {
		t.Fatalf("two machines: multi=%v runs=%d label=%q", m.run.multi, len(m.run.runs), m.run.label)
	}
	waitForFile(t, filepath.Join(dir, "scp.argv"))
	argv := fakeArgv(t, dir, "scp.argv")
	if argv[len(argv)-3] != "--" || argv[len(argv)-2] != src || !strings.HasPrefix(argv[len(argv)-1], "root@100.64.0.") {
		t.Fatalf("scp argv = %q", argv)
	}
}

// Verifies: REQ-CPY-007
func TestDownloadPromptsThenCopiesFromOneMachine(t *testing.T) {
	dir := copyEnv(t)
	dest := t.TempDir()
	m := copyModel(t) // no marks: the highlighted machine

	updated, _ := m.promptDownload()
	m = updated.(model)
	if m.input.mode != inputText || !strings.Contains(m.input.label, "alpha.ts.net") {
		t.Fatalf("first prompt: %+v", m.input)
	}
	if u, cmd := m.input.onSubmit(m, " "); cmd != nil || u.(model).input.mode != inputNone {
		t.Fatal("an empty remote path closes the prompt")
	}
	updated, _ = m.input.onSubmit(m, "/var/log/syslog")
	m = updated.(model)
	if m.input.value != "." || !strings.Contains(m.input.label, "local directory") {
		t.Fatalf("second prompt: %+v", m.input)
	}
	// not a directory / missing
	notDir := filepath.Join(dest, "file")
	_ = os.WriteFile(notDir, []byte("x"), 0o600)
	for _, bad := range []string{notDir, filepath.Join(dest, "missing")} {
		if u, cmd := m.input.onSubmit(m, bad); cmd != nil || u.(model).notify.sshError == nil {
			t.Fatalf("%q must be refused", bad)
		}
	}
	// the default is the current directory
	if u, cmd := m.input.onSubmit(m, " "); cmd == nil || u.(model).run.spec.copy.local != "." {
		t.Fatal("an empty directory means the current one")
	}
	waitForFile(t, filepath.Join(dir, "scp.argv")) // let that job finish before the real one
	time.Sleep(100 * time.Millisecond)
	_ = os.Remove(filepath.Join(dir, "scp.argv"))

	updated, cmd := m.input.onSubmit(m, dest)
	m = updated.(model)
	if cmd == nil || !m.run.spec.copy.download || m.run.spec.copy.remote != "/var/log/syslog" || m.run.spec.copy.local != dest {
		t.Fatalf("the download must start: %+v", m.run.spec)
	}
	waitForFile(t, filepath.Join(dir, "scp.argv"))
	argv := fakeArgv(t, dir, "scp.argv")
	if argv[len(argv)-2] != "root@100.64.0.1:/var/log/syslog" || argv[len(argv)-1] != dest {
		t.Fatalf("scp argv = %q", argv)
	}
}

// Verifies: REQ-CPY-008
func TestCopyKeysDoNothingWithoutTargetAndDownloadRefusesSeveral(t *testing.T) {
	m := model{}
	if _, cmd := m.promptUpload(); cmd != nil {
		t.Fatal("no machine, no upload prompt")
	}
	if u, _ := m.promptUpload(); u.(model).input.mode != inputNone {
		t.Fatal("no prompt without a machine")
	}
	if u, _ := m.promptDownload(); u.(model).input.mode != inputNone {
		t.Fatal("no prompt without a machine")
	}

	m = copyModel(t, "a", "b")
	u, _ := m.promptDownload()
	got := u.(model)
	if got.input.mode != inputNone || got.notify.sshError == nil || !strings.Contains(got.notify.sshError.Error(), "one machine") {
		t.Fatalf("download from several machines must be refused: %+v", got.notify.sshError)
	}

	// the keys are wired
	m = copyModel(t)
	for _, k := range []string{"f", "F"} {
		u, _ := normalModeHandlers[k](m)
		if u.(model).input.mode != inputText {
			t.Fatalf("%s must open a prompt", k)
		}
	}
}
