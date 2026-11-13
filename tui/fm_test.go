package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yatsenko-ihor/ts-cli/client"
)

func fmKey(k string) tea.KeyMsg {
	switch k {
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

type fmFixture struct {
	m     model
	local string
	ran   *[]string
}

// fmOpen opens the manager on a machine whose listing is /srv with a dir and two files.
func fmOpen(t *testing.T) fmFixture {
	t.Helper()
	local := t.TempDir()
	os.Mkdir(filepath.Join(local, "docs"), 0o755)
	os.WriteFile(filepath.Join(local, "a.txt"), []byte("hello"), 0o644)
	os.WriteFile(filepath.Join(local, ".hidden"), []byte("x"), 0o644)
	t.Chdir(local)

	oldList, oldExec := remoteList, fmRemoteExec
	var ran []string
	remoteList = func(src remoteSource, dir string) (string, []string, error) {
		switch dir {
		case "", "/srv":
			return "/srv", []string{"data/", "notes.txt", ".dot"}, nil
		case "/srv/data":
			return "/srv/data", []string{"x.bin"}, nil
		case "/":
			return "/", []string{"srv/"}, nil
		}
		return "", nil, errors.New("cannot open the directory")
	}
	fmRemoteExec = func(src remoteSource, command string) (string, error) {
		ran = append(ran, command)
		if strings.Contains(command, "fail") {
			return "boom\nmore", errors.New("exit status 1")
		}
		return "", nil
	}
	t.Cleanup(func() { remoteList, fmRemoteExec = oldList, oldExec })

	m := flowModel(t, map[string]machineCredential{"dev-1": {username: "pi"}}, "pi")
	u, cmd := m.openFileManager()
	m = u.(model)
	if m.fm == nil || cmd == nil {
		t.Fatalf("not opened: %v", m.notify.sshError)
	}
	u, _ = m.handleFMList(cmd().(fmListMsg))
	return fmFixture{m: u.(model), local: local, ran: &ran}
}

func (f *fmFixture) key(k string) tea.Cmd {
	u, cmd := f.m.Update(fmKey(k))
	f.m = u.(model)
	return cmd
}

func TestFileManagerOpensWithTwoPanes(t *testing.T) {
	f := fmOpen(t)
	remote := f.m.fm.panes[fmRemote]
	if remote.dir != "/srv" || remote.loading || len(remote.entries) != 4 { // ., .., data/, notes.txt
		t.Fatalf("remote: %+v", remote)
	}
	local := f.m.fm.panes[fmLocal]
	if local.dir != f.local || len(local.entries) != 4 { // ., .., docs/, a.txt
		t.Fatalf("local: %q %d", local.dir, len(local.entries))
	}
	view := f.m.View()
	for _, want := range []string{"This computer", "laptop.example.com", "docs/", "a.txt", "data/", "notes.txt", "tab switch pane"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q", want)
		}
	}
}

func TestFileManagerNavigation(t *testing.T) {
	f := fmOpen(t)
	// local pane: down to a.txt... enter on the directory docs goes in, backspace comes back
	f.key("enter") // cursor starts at docs/
	if filepath.Base(f.m.fm.panes[fmLocal].dir) != "docs" {
		t.Fatalf("dir = %q", f.m.fm.panes[fmLocal].dir)
	}
	f.key("backspace")
	if f.m.fm.panes[fmLocal].dir != f.local {
		t.Fatal("up")
	}
	f.key(".")
	if len(f.m.fm.panes[fmLocal].entries) != 5 {
		t.Fatal("hidden files shown")
	}
	f.key(".")
	f.key("down")
	f.key("up")
	f.key("end")
	f.key("home")
	f.key("g")
	f.key("G")
	f.key("k")
	f.key("j")

	// switch to the machine and enter a directory (asynchronously)
	f.key("tab")
	if f.m.fm.focus != fmRemote {
		t.Fatal("tab")
	}
	cmd := f.key("enter") // data/
	if cmd == nil || !f.m.fm.panes[fmRemote].loading {
		t.Fatal("listing requested")
	}
	if f.key("down") != nil || f.m.fm.panes[fmRemote].cursor != 2 {
		t.Fatal("keys wait while loading") // cursor unchanged
	}
	u, _ := f.m.handleFMList(cmd().(fmListMsg))
	f.m = u.(model)
	if f.m.fm.panes[fmRemote].dir != "/srv/data" || f.m.fm.panes[fmRemote].loading {
		t.Fatalf("dir = %q", f.m.fm.panes[fmRemote].dir)
	}
	cmd = f.key("left")
	u, _ = f.m.handleFMList(cmd().(fmListMsg))
	f.m = u.(model)
	if f.m.fm.panes[fmRemote].dir != "/srv" {
		t.Fatal("up on the machine")
	}
	// ".." entry goes up too; "~" goes home; "r" reloads; "." on the machine
	f.key("home")
	f.key("down")
	cmd = f.key("enter")
	u, _ = f.m.handleFMList(cmd().(fmListMsg))
	f.m = u.(model)
	if f.m.fm.panes[fmRemote].dir != "/" {
		t.Fatalf("dir = %q", f.m.fm.panes[fmRemote].dir)
	}
	cmd = f.key("~")
	u, _ = f.m.handleFMList(cmd().(fmListMsg))
	f.m = u.(model)
	cmd = f.key("r")
	u, _ = f.m.handleFMList(cmd().(fmListMsg))
	f.m = u.(model)
	f.key(".")
	if len(f.m.fm.panes[fmRemote].entries) != 5 {
		t.Fatal("hidden on the machine")
	}
	// "~" on the local pane
	f.key("tab")
	f.key("~")
	if f.m.fm.panes[fmLocal].dir == f.local {
		t.Fatal("home")
	}
	// "." (choose) does nothing in the manager
	f.key("home")
	f.key("enter")
	// a failing local directory keeps the old listing and says why
	cmd = f.m.fm.fmGo(fmLocal, "/definitely/not/here")
	if cmd != nil || f.m.fm.err == "" {
		t.Fatal("error shown")
	}
	// pgup/pgdown
	f.key("pgdown")
	f.key("pgup")
}

func TestFileManagerFilterAndEscape(t *testing.T) {
	f := fmOpen(t)
	f.key("/")
	if !f.m.fm.panes[fmLocal].filtering {
		t.Fatal("filtering")
	}
	for _, r := range "a.t" {
		f.key(string(r))
	}
	if shown := f.m.fm.panes[fmLocal].shown(); len(shown) != 3 { // ., .., a.txt
		t.Fatalf("shown %d", len(shown))
	}
	f.key("enter")
	if f.m.fm.panes[fmLocal].filtering || f.m.fm.panes[fmLocal].filter == "" {
		t.Fatal("enter keeps the filter")
	}
	f.key("esc") // clears the filter
	if f.m.fm.panes[fmLocal].filter != "" || f.m.fm == nil {
		t.Fatal("first esc clears the filter")
	}
	f.key("esc")
	if f.m.fm != nil || f.m.fmDirs["dev-1"][fmRemote] != "/srv" {
		t.Fatal("second esc closes and remembers the directories")
	}
	// reopening goes back to the same directories
	u, cmd := f.m.openFileManager()
	m := u.(model)
	if m.fm.panes[fmRemote].startDir != "/srv/" || cmd == nil {
		t.Fatalf("start = %q", m.fm.panes[fmRemote].startDir)
	}
	f2 := fmFixture{m: m}
	f2.key("q")
	if f2.m.fm != nil {
		t.Fatal("q closes")
	}
}

func TestFileManagerCopyUploadsAndDownloads(t *testing.T) {
	useFakeBins(t, fakeBins{ssh: true})
	f := fmOpen(t)
	f.key("down") // a.txt in the local pane
	f.m.list.marked = map[string]bool{"dev-1": true}
	u, cmd := f.m.fmCopy()
	m := u.(model)
	if m.fm == nil || cmd == nil && m.fm.err == "" {
		t.Fatalf("the manager stays open during a copy: %v", m.fm)
	}
	if len(m.run.runs) == 1 {
		spec := m.run.spec.copy
		if spec.download || filepath.Base(spec.local) != "a.txt" || spec.remote != "/srv" {
			t.Fatalf("upload spec %+v", spec)
		}
	}

	// download: remote pane, notes.txt
	g := fmOpen(t)
	g.key("tab")
	g.key("down") // notes.txt
	u, _ = g.m.fmCopy()
	m = u.(model)
	if len(m.run.runs) == 1 {
		spec := m.run.spec.copy
		if !spec.download || spec.remote != "/srv/notes.txt" || spec.local != g.local {
			t.Fatalf("download spec %+v", spec)
		}
	}
}

func TestFileManagerCopyRefusals(t *testing.T) {
	f := fmOpen(t)
	f.key("up")
	f.key("up") // "." is special
	f.key("c")
	if !strings.Contains(f.m.fm.status, "Select a file") {
		t.Fatalf("status = %q", f.m.fm.status)
	}
	f.key("down")
	f.key("down")
	f.key("down") // a real entry
	f.m.fm.panes[fmRemote].loading = true
	f.m.fm.panes[fmRemote].dir = ""
	f.key("c")
	if !strings.Contains(f.m.fm.status, "not loaded") {
		t.Fatalf("status = %q", f.m.fm.status)
	}
}

func TestFileManagerOperations(t *testing.T) {
	f := fmOpen(t)
	run := func(cmd tea.Cmd) {
		t.Helper()
		if cmd == nil {
			t.Fatal("expected a command")
		}
		msg, ok := cmd().(fmOpMsg)
		if !ok {
			t.Fatal("not an op result")
		}
		u, c := f.m.handleFMOp(msg)
		f.m = u.(model)
		if c != nil {
			if l, ok := c().(fmListMsg); ok { // only the machine's pane needs a request
				u, _ := f.m.handleFMList(l)
				f.m = u.(model)
			}
		}
	}
	answer := func(text string) tea.Cmd {
		t.Helper()
		if f.m.input.onSubmit == nil {
			t.Fatal("no prompt open")
		}
		u, cmd := f.m.input.onSubmit(f.m, text)
		f.m = u.(model)
		return cmd
	}

	// mkdir locally
	f.key("n")
	run(answer("newdir"))
	if st, err := os.Stat(filepath.Join(f.local, "newdir")); err != nil || !st.IsDir() {
		t.Fatal("local mkdir")
	}
	if f.m.fm.status != "✓ Created newdir" {
		t.Fatalf("status = %q", f.m.fm.status)
	}
	f.key("n")
	if answer("  ") != nil {
		t.Fatal("empty name does nothing")
	}

	// rename locally (cursor is on a directory; pick a.txt)
	for f.m.fm.fmSelected(fmLocal).name != "a.txt" {
		f.key("down")
	}
	f.key("R")
	if cmd := answer("a/b"); cmd != nil || !strings.Contains(f.m.fm.err, "separator") {
		t.Fatalf("separator: %q", f.m.fm.err)
	}
	f.key("R")
	if answer("a.txt") != nil {
		t.Fatal("same name does nothing")
	}
	f.key("R")
	run(answer("b.txt"))
	if _, err := os.Stat(filepath.Join(f.local, "b.txt")); err != nil {
		t.Fatal("local rename")
	}

	// delete locally needs "yes"
	for f.m.fm.fmSelected(fmLocal).name != "b.txt" {
		f.key("down")
	}
	f.key("d")
	if answer("no") != nil || f.m.fm.status != "Not deleted" {
		t.Fatal("not confirmed")
	}
	f.key("d")
	run(answer("yes"))
	if _, err := os.Stat(filepath.Join(f.local, "b.txt")); err == nil {
		t.Fatal("deleted")
	}

	// the machine: mkdir, rename, delete through ssh; absolute names stay absolute
	f.key("tab")
	f.key("n")
	run(answer("/srv/made"))
	f.key("n")
	run(answer("rel dir"))
	f.key("down")
	f.key("R")
	run(answer("renamed"))
	f.key("d")
	run(answer("yes"))
	joined := strings.Join(*f.ran, "\n")
	for _, want := range []string{"mkdir -p -- '/srv/made'", "mkdir -p -- '/srv/rel dir'", "mv -n -- '/srv/", "rm -rf -- '/srv/"} {
		if !strings.Contains(joined, want) {
			t.Errorf("remote commands lack %q:\n%s", want, joined)
		}
	}

	// a failing remote command shows its first line and keeps the listing
	f.key("n")
	run(answer("fail-dir"))
	if !strings.Contains(f.m.fm.err, "boom") {
		t.Fatalf("err = %q", f.m.fm.err)
	}
	// nothing selected: refusals
	f.key("up")
	f.key("up")
	f.key("up")
	for _, k := range []string{"R", "d"} {
		f.key(k)
		if !strings.Contains(f.m.fm.status, "Select a file") {
			t.Fatalf("%s: %q", k, f.m.fm.status)
		}
	}
	// a stale op result after closing is ignored
	f.m.fm = nil
	if _, c := f.m.handleFMOp(fmOpMsg{what: "x"}); c != nil {
		t.Fatal("closed")
	}
	if _, c := f.m.handleFMList(fmListMsg{}); c != nil {
		t.Fatal("closed")
	}
}

func TestFileManagerRefusalsAndListErrors(t *testing.T) {
	m := flowModel(t, nil, "")
	if u, _ := m.openFileManager(); u.(model).fm != nil || !strings.Contains(u.(model).notify.sshError.Error(), "username") {
		t.Fatal("no username")
	}
	m = flowModel(t, map[string]machineCredential{"dev-1": {username: "pi"}}, "pi")
	other := m
	other.list.devices[0].AccountTailnet = "other.example.com"
	other.list.filteredDevices = other.list.devices
	other.acct.tailscaleActiveAccount = "mine.example.com"
	if u, c := other.openFileManager(); u.(model).fm != nil || c == nil || u.(model).pendingSwitch == nil {
		t.Fatal("another account is switched to by itself, not refused")
	}
	noAddr := flowModel(t, map[string]machineCredential{"dev-1": {username: "pi"}}, "pi")
	noAddr.list.devices[0].Addresses = nil
	noAddr.list.filteredDevices = noAddr.list.devices
	if u, _ := noAddr.openFileManager(); u.(model).fm != nil || !strings.Contains(u.(model).notify.sshError.Error(), "no IP") {
		t.Fatal("no address")
	}
	second := m.list.devices[0]
	second.ID, second.Hostname, second.Name = "dev-2", "two", "two.example.com"
	m.list.filteredDevices = append(append([]client.Device{}, m.list.filteredDevices...), second)
	m.list.marked = map[string]bool{"dev-1": true, "dev-2": true}
	if u, _ := m.openFileManager(); u.(model).fm != nil || !strings.Contains(u.(model).notify.sshError.Error(), "one machine") {
		t.Fatal("several machines")
	}

	// the first listing fails: fall back to the home directory; a second failure shows the error
	oldList := remoteList
	t.Cleanup(func() { remoteList = oldList })
	calls := 0
	remoteList = func(src remoteSource, dir string) (string, []string, error) {
		calls++
		return "", nil, errors.New("cannot open the directory")
	}
	m = flowModel(t, map[string]machineCredential{"dev-1": {username: "pi"}}, "pi")
	m.fmDirs = map[string][2]string{"dev-1": {"", "/gone"}}
	u, cmd := m.openFileManager()
	m = u.(model)
	u, cmd = m.handleFMList(cmd().(fmListMsg)) // falls back
	m = u.(model)
	if cmd == nil || !m.fm.panes[fmRemote].loading {
		t.Fatal("fallback to home")
	}
	u, cmd = m.handleFMList(cmd().(fmListMsg))
	m = u.(model)
	if cmd != nil || m.fm.err == "" || m.fm.panes[fmRemote].loading {
		t.Fatal("second failure shows the error")
	}
	// an old answer is dropped
	if _, c := m.handleFMList(fmListMsg{msg: remoteListMsg{id: 99}}); c != nil {
		t.Fatal("stale")
	}
	if !strings.Contains(m.View(), "✗ cannot open") {
		t.Fatal("the error is shown")
	}
}

func TestFileManagerReachableFromTheListAndTheMachineTab(t *testing.T) {
	m := flowModel(t, map[string]machineCredential{"dev-1": {username: "pi"}}, "pi")
	old := remoteList
	remoteList = func(remoteSource, string) (string, []string, error) { return "/", nil, nil }
	t.Cleanup(func() { remoteList = old })
	u, cmd := m.Update(fmKey("b"))
	if u.(model).fm == nil || cmd == nil {
		t.Fatal("b opens it")
	}
	found := false
	for _, a := range machineActions(m.list.devices[0]) {
		if strings.HasPrefix(a.title, "File manager") {
			u, _ := a.run(m, m.list.devices[0])
			found = u.(model).fm != nil
		}
	}
	if !found {
		t.Fatal("the Machine tab opens it")
	}
}
