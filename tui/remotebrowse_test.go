//go:build !windows

package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yatsenko-ihor/ts-cli/client"
)

// Requirements-based tests for browsing a machine (verification/requirements/remotebrowse.md).

// fakeListSSH answers like a machine: the last argument is the remote command.
const fakeListSSH = `#!/bin/sh
d="$FAKE_DIR"
: > "$d/ssh.argv"
for a in "$@"; do printf '%s\0' "$a" >> "$d/ssh.argv"; last="$a"; done
case "$last" in
  *"/missing"*) exit 3 ;;
  *"/slow"*) sleep 5 ;;
esac
[ -n "$FAKE_SSH_EXIT" ] && exit "$FAKE_SSH_EXIT"
if [ -n "$FAKE_LIST_OUT" ]; then printf '%b' "$FAKE_LIST_OUT"; exit 0; fi
printf '/home/pi/proj\nsub/\nREADME.md\n.hidden\n.config/\nlink-dir/\nz.txt\n'
`

func listEnv(t *testing.T) string {
	t.Helper()
	dir := useFakeBins(t, fakeBins{ssh: true, sshpass: true})
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(fakeListSSH), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"sleep"} {
		if err := os.Symlink("/bin/"+tool, filepath.Join(dir, tool)); err != nil {
			t.Skipf("no /bin/%s", tool)
		}
	}
	t.Setenv("FAKE_LIST_OUT", "")
	return dir
}

func rdev() client.Device {
	return client.Device{ID: "d1", Name: "box-050.tail813b1c.ts.net", Addresses: []string{"100.64.0.50"}}
}

// Verifies: REQ-RBR-001
func TestRemoteCdAndListScript(t *testing.T) {
	for in, want := range map[string]string{
		"":            "cd",
		"~":           "cd",
		"~/docs":      `cd -- "$HOME"'/docs'`,
		"/srv/data":   `cd -- '/srv/data'`,
		"it's here":   `cd -- 'it'"'"'s here'`,
		"-rf":         `cd -- '-rf'`,
		"$(rm -rf /)": `cd -- '$(rm -rf /)'`,
		"a b/c;d`e`":  "cd -- 'a b/c;d`e`'",
		"~user/x":     `cd -- '~user/x'`,
	} {
		if got := remoteCd(in); got != want {
			t.Errorf("remoteCd(%q) = %q, want %q", in, got, want)
		}
	}
	if got := remoteListScript("/x"); got != `cd -- '/x' 2>/dev/null || exit 3; pwd -P; ls -1ApL 2>/dev/null; exit 0` {
		t.Fatalf("script = %q", got)
	}
}

// Verifies: REQ-RBR-002
func TestRemoteStartAndListingParsing(t *testing.T) {
	for in, want := range map[string]string{
		"": "", " ": "", ".": "", "~": "", "docs": "", "file.txt": "",
		"/srv/data/": "/srv/data/", "docs/": "docs/", "/srv/data/file": "/srv/data", "~/a/b": "~/a", "a/b": "a",
	} {
		if got := remoteStart(in); got != want {
			t.Errorf("remoteStart(%q) = %q, want %q", in, got, want)
		}
	}
	dir, lines, err := parseRemoteListing("/home/pi\r\nsub/\r\n\r\nfile\n")
	if err != nil || dir != "/home/pi" || strings.Join(lines, ",") != "sub/,file" {
		t.Fatalf("%q %v %v", dir, lines, err)
	}
	for _, bad := range []string{"", "\n\n", "relative/path\nx", "Welcome to the machine\n/home/pi\n"} {
		if _, _, err := parseRemoteListing(bad); err == nil {
			t.Errorf("parseRemoteListing(%q) must fail", bad)
		}
	}
	if dir, lines, err := parseRemoteListing("/empty\n"); err != nil || dir != "/empty" || len(lines) != 0 {
		t.Fatalf("an empty directory is valid: %q %v %v", dir, lines, err)
	}
}

// Verifies: REQ-RBR-003
func TestApplyRemoteListing(t *testing.T) {
	b := &fileBrowser{}
	lines := []string{"sub/", "README.md", ".hidden", ".config/", "link-dir/", "z.txt", "Alpha/", "/"}
	b.applyRemote("/home/pi", lines)
	var got []string
	for _, e := range b.entries {
		got = append(got, e.label())
	}
	if strings.Join(got, ",") != ". (choose this directory),.. (up),Alpha/,link-dir/,sub/,README.md,z.txt" {
		t.Fatalf("entries = %v", got)
	}
	if b.cursor != 2 || b.dir != "/home/pi" || b.entries[5].size != -1 {
		t.Fatalf("cursor %d dir %s size %d", b.cursor, b.dir, b.entries[5].size)
	}
	b.hidden = true
	b.applyRemote("/home/pi", lines)
	if len(b.entries) != 2+8-1 || b.entries[2].name != ".config" {
		t.Fatalf("hidden entries shown: %d %v", len(b.entries), b.entries[2])
	}
	b.hidden, b.dirsOnly = false, true
	b.applyRemote("/home/pi", lines)
	if len(b.entries) != 2+3 {
		t.Fatalf("dirs only: %d", len(b.entries))
	}
	b.applyRemote("/x", nil)
	if b.cursor != 1 || len(b.entries) != 2 {
		t.Fatalf("an empty listing keeps the cursor on ..: %d", b.cursor)
	}
}

// Verifies: REQ-RBR-004
func TestRunRemoteListOverSSH(t *testing.T) {
	dir := listEnv(t)
	src := remoteSource{device: rdev(), username: "pi"}

	d, lines, err := runRemoteList(src, "~/proj")
	if err != nil || d != "/home/pi/proj" || len(lines) != 6 {
		t.Fatalf("%q %v %v", d, lines, err)
	}
	argv := fakeArgv(t, dir, "ssh.argv")
	if argv[len(argv)-2] != "pi@100.64.0.50" || !strings.HasPrefix(argv[len(argv)-1], `cd -- "$HOME"'/proj'`) {
		t.Fatalf("argv = %q", argv)
	}
	if !strings.Contains(strings.Join(argv, " "), "BatchMode=yes") {
		t.Fatal("a listing must never prompt: BatchMode")
	}

	// a saved password goes through sshpass fd 3, never argv
	src.password = "S3cret"
	if _, _, err := runRemoteList(src, ""); err != nil {
		t.Fatal(err)
	}
	if got := fakeFile(t, dir, "sshpass.fd3"); got != "S3cret" {
		t.Fatalf("fd3 = %q", got)
	}
	if strings.Contains(strings.Join(fakeArgv(t, dir, "sshpass.argv"), " "), "S3cret") {
		t.Fatal("the password must not appear in argv")
	}
	src.password = ""

	for name, tc := range map[string]struct{ dir, exit, want string }{
		"missing directory": {"/missing", "", "cannot open the directory"},
		"auth failure":      {"/x", "255", "connect once with s"},
		"bad password":      {"/x", "5", "password was rejected"},
		"other exit":        {"/x", "7", "exit status 7"},
	} {
		t.Setenv("FAKE_SSH_EXIT", tc.exit)
		if _, _, err := runRemoteList(src, tc.dir); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	t.Setenv("FAKE_SSH_EXIT", "")

	t.Setenv("FAKE_LIST_OUT", "motd only\\n")
	if _, _, err := runRemoteList(src, "/x"); err == nil || !strings.Contains(err.Error(), "unexpected reply") {
		t.Fatalf("garbage reply: %v", err)
	}
	t.Setenv("FAKE_LIST_OUT", "")

	// bad input is refused before anything runs
	os.Remove(filepath.Join(dir, "ssh.argv"))
	for name, call := range map[string]func() error{
		"no address": func() error {
			_, _, e := runRemoteList(remoteSource{device: client.Device{}, username: "pi"}, "")
			return e
		},
		"control char": func() error { _, _, e := runRemoteList(src, "a\nb"); return e },
		"bad username": func() error {
			_, _, e := runRemoteList(remoteSource{device: rdev(), username: "-oProxyCommand=x"}, "")
			return e
		},
		"bad address": func() error {
			_, _, e := runRemoteList(remoteSource{device: client.Device{Addresses: []string{"1.2.3.4;x"}}, username: "pi"}, "")
			return e
		},
	} {
		if err := call(); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "ssh.argv")); err == nil {
		t.Fatal("ssh must not run for refused input")
	}

	// a machine that does not answer is cut off
	old := remoteListTimeout
	remoteListTimeout = 200 * time.Millisecond
	t.Cleanup(func() { remoteListTimeout = old })
	started := time.Now()
	if _, _, err := runRemoteList(src, "/slow"); err == nil || !strings.Contains(err.Error(), "did not answer") || time.Since(started) > 3*time.Second {
		t.Fatalf("timeout: %v after %v", err, time.Since(started))
	}
}

// Verifies: REQ-RBR-004
func TestRunRemoteListFailuresBeforeTheCommandRuns(t *testing.T) {
	listEnv(t)
	// ssh cannot be started
	t.Setenv("PATH", t.TempDir())
	if _, _, err := runRemoteList(remoteSource{device: rdev(), username: "pi"}, ""); err == nil || !strings.Contains(err.Error(), "could not start ssh") {
		t.Fatalf("err = %v", err)
	}
	// the password pipe cannot be created
	listEnv(t)
	withOSPipe(t, func() (*os.File, *os.File, error) { return nil, nil, errors.New("no fds") })
	if _, _, err := runRemoteList(remoteSource{device: rdev(), username: "pi", password: "x"}, ""); err == nil {
		t.Fatal("a pipe failure must be reported")
	}
}

func browsingModel(t *testing.T) model {
	t.Helper()
	m := flowModel(t, map[string]machineCredential{"d1": {username: "pi"}}, "")
	m.list.devices = []client.Device{rdev()}
	m.list.filteredDevices = m.list.devices
	m.input = textInput{mode: inputText, path: pathRemoteAny, device: rdev(), value: "."}
	return m
}

func stubRemoteList(t *testing.T, fn func(src remoteSource, dir string) (string, []string, error)) *[]string {
	t.Helper()
	var asked []string
	old := remoteList
	remoteList = func(src remoteSource, dir string) (string, []string, error) {
		asked = append(asked, dir)
		return fn(src, dir)
	}
	t.Cleanup(func() { remoteList = old })
	return &asked
}

func answer(t *testing.T, m model, cmd tea.Cmd) model {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a listing request")
	}
	updated, next := m.Update(cmd())
	if next != nil {
		t.Fatal("no follow-up request expected")
	}
	return updated.(model)
}

func homeListing(src remoteSource, dir string) (string, []string, error) {
	switch dir {
	case "", "~", "/home/pi":
		return "/home/pi", []string{"proj/", "notes.txt", ".dot"}, nil
	case "/home/pi/proj":
		return "/home/pi/proj", []string{"a.go", "deep/"}, nil
	case "/home":
		return "/home", []string{"pi/"}, nil
	}
	return "", nil, errors.New("cannot open the directory")
}

// Verifies: REQ-RBR-005
func TestOpeningTheRemoteBrowser(t *testing.T) {
	asked := stubRemoteList(t, homeListing)
	m := browsingModel(t)
	m.input.value = "  "
	updated, cmd := m.handleTextPrompt(tea.KeyMsg{Type: tea.KeyCtrlO})
	m = updated.(model)
	if m.browse == nil || !m.browse.loading || m.browse.remote == nil || m.browse.remote.username != "pi" || m.browse.dirsOnly {
		t.Fatalf("browse = %+v", m.browse)
	}
	if !strings.Contains(m.renderBrowser(), "loading…") || !strings.Contains(m.renderBrowser(), "on box-050") {
		t.Fatalf("loading state:\n%s", m.renderBrowser())
	}
	m = answer(t, m, cmd)
	if m.browse.loading || m.browse.dir != "/home/pi" || (*asked)[0] != "" {
		t.Fatalf("dir %q asked %v", m.browse.dir, *asked)
	}
	out := m.renderBrowser()
	if !strings.Contains(out, "proj/") || !strings.Contains(out, "notes.txt") || strings.Contains(out, ".dot") {
		t.Fatalf("listing:\n%s", out)
	}

	// the button works the same way; destination prompts browse directories only
	m = browsingModel(t)
	m.input.path = pathRemoteDirs
	m.input.focusBrowse = true
	updated, cmd = m.handleTextPrompt(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if m.browse == nil || !m.browse.dirsOnly || cmd == nil {
		t.Fatal("enter on the button opens the machine browser")
	}

	// a typed path starts there
	m = browsingModel(t)
	m.input.value = "/home/pi/proj/a.go"
	updated, _ = m.handleTextPrompt(tea.KeyMsg{Type: tea.KeyCtrlO})
	if updated.(model).browse.startDir != "/home/pi/proj" {
		t.Fatalf("start = %q", updated.(model).browse.startDir)
	}

	// Tab does not complete against the local disk in a machine prompt
	m = browsingModel(t)
	m.input.value = "/etc/pas"
	updated, _ = m.handleTextPrompt(tea.KeyMsg{Type: tea.KeyTab})
	if updated.(model).input.value != "/etc/pas" || updated.(model).input.note != "" {
		t.Fatal("no local completion for a remote path")
	}
	updated, _ = m.handleTextPrompt(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	updated, _ = m.handleTextPrompt(tea.KeyMsg{Type: tea.KeyTab})
	if updated.(model).input.focusBrowse {
		t.Fatal("Tab returns the focus to the field")
	}
	if v := m.renderInputPopup("x", m.input.value, false); !strings.Contains(v, "browse the machine") || strings.Contains(v, "tab complete") || !strings.Contains(v, "[ Browse… ]") {
		t.Fatalf("remote prompt hints:\n%s", v)
	}
}

// Verifies: REQ-RBR-006
func TestRemoteBrowserRefusesWhenItCannotLogIn(t *testing.T) {
	stubRemoteList(t, homeListing)
	m := browsingModel(t)
	m.ssh.machineCreds = map[string]machineCredential{}
	updated, cmd := m.openRemoteBrowser()
	got := updated.(model)
	if got.browse != nil || cmd != nil || got.notify.sshError == nil || !strings.Contains(got.notify.sshError.Error(), "no username saved") {
		t.Fatalf("no username: %+v", got.notify.sshError)
	}

	m = browsingModel(t)
	m.acct.tailscaleActiveAccount = "other@example.org"
	d := rdev()
	d.AccountTailnet = "work.com"
	m.input.device = d
	updated, cmd = m.openRemoteBrowser()
	got = updated.(model)
	if got.browse != nil || cmd != nil || got.notify.sshError == nil || !strings.Contains(got.notify.sshError.Error(), "another account") {
		t.Fatalf("other account: %+v", got.notify.sshError)
	}

	// a saved password is used
	m = browsingModel(t)
	m.ssh.machineCreds = map[string]machineCredential{"d1": {username: "pi", passwordEncrypted: encFor(t, "pw")}}
	updated, _ = m.openRemoteBrowser()
	if updated.(model).browse.remote.password != "pw" {
		t.Fatal("the saved password must be used")
	}
}

// Verifies: REQ-RBR-007
func TestRemoteBrowserNavigation(t *testing.T) {
	asked := stubRemoteList(t, homeListing)
	m := browsingModel(t)
	updated, cmd := m.openRemoteBrowser()
	m = answer(t, updated.(model), cmd)

	// enter on a directory asks for it; keys are ignored while it loads
	updated, cmd = m.handleBrowser(browserKey("enter")) // first real entry: proj/
	m = updated.(model)
	if !m.browse.loading || cmd == nil {
		t.Fatal("descending asks the machine")
	}
	updated, noCmd := m.handleBrowser(browserKey("down"))
	if noCmd != nil || updated.(model).browse.cursor != m.browse.cursor {
		t.Fatal("keys are ignored while loading")
	}
	if out := m.renderBrowser(); !strings.Contains(out, "/home/pi  (loading…)") {
		t.Fatalf("the directory stays visible while the next one loads:\n%s", out)
	}
	m = answer(t, m, cmd)
	if m.browse.dir != "/home/pi/proj" || (*asked)[len(*asked)-1] != "/home/pi/proj" {
		t.Fatalf("dir %q asked %v", m.browse.dir, *asked)
	}

	// left goes to the parent (path based, the root is its own parent), ~ to home
	updated, cmd = m.handleBrowser(browserKey("left"))
	m = answer(t, updated.(model), cmd)
	if m.browse.dir != "/home/pi" {
		t.Fatalf("parent: %q", m.browse.dir)
	}
	updated, cmd = m.handleBrowser(browserKey("~"))
	m = answer(t, updated.(model), cmd)
	if (*asked)[len(*asked)-1] != "" || m.browse.dir != "/home/pi" {
		t.Fatalf("~ asks for the home directory: %v", *asked)
	}
	m.browse.dir = "/"
	_, cmd = m.handleBrowser(browserKey("backspace"))
	if (*asked)[len(*asked)-1] != "" { // not asked yet: the command has not run
		t.Fatal("unexpected")
	}
	cmdMsg := cmd().(remoteListMsg)
	if len(*asked) == 0 || (*asked)[len(*asked)-1] != "/" {
		t.Fatalf("the parent of / is /: %v (%v)", *asked, cmdMsg.err)
	}

	// hidden entries toggle without asking again
	m = browsingModel(t)
	updated, cmd = m.openRemoteBrowser()
	m = answer(t, updated.(model), cmd)
	before := len(*asked)
	updated, noCmd = m.handleBrowser(browserKey("."))
	m = updated.(model)
	if noCmd != nil || len(*asked) != before || !strings.Contains(m.renderBrowser(), ".dot") {
		t.Fatal("'.' re-filters the listing already received")
	}

	// the .. row asks for the parent as well
	m.browse.cursor = 1
	updated, cmd = m.handleBrowser(browserKey("enter"))
	m = answer(t, updated.(model), cmd)
	if m.browse.dir != "/home" {
		t.Fatalf(".. row: %q", m.browse.dir)
	}

	// choosing: . gives the directory, a file its path, esc closes and keeps the text
	m.browse.cursor = 0
	updated, _ = m.handleBrowser(browserKey("enter"))
	if got := updated.(model).input.value; got != "/home" || updated.(model).browse != nil {
		t.Fatalf("chose %q", got)
	}
	m = browsingModel(t)
	updated, cmd = m.openRemoteBrowser()
	m = answer(t, updated.(model), cmd)
	m.browse.cursor = 3 // notes.txt (proj/, notes.txt after . and ..)
	updated, _ = m.handleBrowser(browserKey("enter"))
	if got := updated.(model).input.value; got != "/home/pi/notes.txt" {
		t.Fatalf("chose %q", got)
	}
	updated, _ = m.openRemoteBrowser()
	m = updated.(model)
	before2 := m.input.value
	updated, _ = m.handleBrowser(browserKey("esc"))
	if updated.(model).browse != nil || updated.(model).input.value != before2 {
		t.Fatal("esc closes the browser even while loading")
	}
}

// Verifies: REQ-RBR-008
func TestRemoteListingErrorsAndStaleAnswers(t *testing.T) {
	asked := stubRemoteList(t, homeListing)

	// the typed directory does not exist: fall back to home once
	m := browsingModel(t)
	m.input.value = "/nowhere/file"
	updated, cmd := m.openRemoteBrowser()
	m = updated.(model)
	updated, next := m.Update(cmd())
	m = updated.(model)
	if next == nil || !m.browse.loading || m.browse.startDir != "" || m.browse.err != "" {
		t.Fatalf("a failing start retries at home: %+v", m.browse)
	}
	m = answer(t, m, next)
	if m.browse.dir != "/home/pi" || strings.Join(*asked, ",") != "/nowhere,/" && strings.Join(*asked, ",") != "/nowhere," {
		t.Fatalf("dir %q asked %v", m.browse.dir, *asked)
	}

	// a later failure keeps the listing and says why
	m.browse.cursor = 2
	m.browse.entries[2].name = "gone"
	updated, cmd = m.handleBrowser(browserKey("enter"))
	m = answer(t, updated.(model), cmd)
	if m.browse.err == "" || m.browse.dir != "/home/pi" || m.browse.loading {
		t.Fatalf("failure: %+v", m.browse)
	}

	// failure of the very first listing without a typed directory: shown, nothing to retry
	stubRemoteList(t, func(remoteSource, string) (string, []string, error) { return "", nil, errors.New("connection refused") })
	m = browsingModel(t)
	updated, cmd = m.openRemoteBrowser()
	m = answer(t, updated.(model), cmd)
	if m.browse.err != "connection refused" || !strings.Contains(m.renderBrowser(), "connection refused") || m.browse.loading {
		t.Fatalf("first failure: %+v", m.browse)
	}

	// answers for older requests, a closed browser or a local browser are dropped
	m.browse.reqID = 5
	updated, _ = m.handleRemoteList(remoteListMsg{id: 4, dir: "/x"})
	if updated.(model).browse.dir == "/x" {
		t.Fatal("a stale answer must be ignored")
	}
	updated, _ = m.handleRemoteList(remoteListMsg{id: 5, dir: "/new", lines: []string{"x"}})
	if updated.(model).browse.dir != "/new" {
		t.Fatal("the newest answer is applied")
	}
	closed := m
	closed.browse = nil
	if updated, _ := closed.handleRemoteList(remoteListMsg{id: 1}); updated.(model).browse != nil {
		t.Fatal("no browser, no change")
	}
	local := m
	local.browse = newBrowser(t.TempDir(), false)
	if updated, _ := local.handleRemoteList(remoteListMsg{id: 0, dir: "/x"}); updated.(model).browse.dir == "/x" {
		t.Fatal("a local browser ignores machine listings")
	}
}

// Verifies: REQ-RBR-009
func TestCopyPromptsBrowseTheMachine(t *testing.T) {
	copyEnv(t)
	m := copyModel(t) // alpha highlighted
	src := filepath.Join(t.TempDir(), "f.txt")
	_ = os.WriteFile(src, []byte("x"), 0o600)

	updated, _ := m.promptUpload()
	m = updated.(model)
	updated, _ = m.input.onSubmit(m, src)
	m = updated.(model)
	if m.input.path != pathRemoteDirs || m.input.device.ID != "a" {
		t.Fatalf("the upload destination browses directories of the first target: %+v", m.input)
	}

	m = copyModel(t)
	updated, _ = m.promptDownload()
	m = updated.(model)
	if m.input.path != pathRemoteAny || m.input.device.ID != "a" {
		t.Fatalf("the download source browses the machine: %+v", m.input)
	}
	updated, _ = m.input.onSubmit(m, "/var/log")
	if updated.(model).input.path != pathDirs {
		t.Fatal("the destination of a download is a local directory")
	}
}
