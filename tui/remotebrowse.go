package tui

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"path"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yatsenko-ihor/ts-cli/client"
	"github.com/yatsenko-ihor/ts-cli/scripts"
)

// Browsing a directory on a machine from an upload or download prompt.
//
// A listing is one ssh command through the same credential handling as every
// other job: the machine's saved username and password, or key
// authentication. Nothing is asked inside the browser; when the credentials
// do not work the browser says to connect once with s. Names containing a
// newline are not supported (the listing is line based).

// remoteSource is the machine being browsed.
type remoteSource struct {
	device   client.Device
	username string
	password string
}

// remoteListMsg is the answer to a listing request.
type remoteListMsg struct {
	id    int
	dir   string   // the absolute directory that was listed
	lines []string // ls -1Ap lines: "name" or "name/"
	err   error
}

// remoteListTimeout bounds one listing.
var remoteListTimeout = 30 * time.Second

// remoteList runs the listing on the machine. It is a variable so tests can
// replace it.
var remoteList = runRemoteList

// remoteCd is the shell command that enters dir; "" and "~" mean the home
// directory and a leading "~/" is expanded by the remote shell.
func remoteCd(dir string) string {
	switch {
	case dir == "" || dir == "~":
		return "cd"
	case strings.HasPrefix(dir, "~/"):
		return `cd -- "$HOME"` + scripts.ShellQuote(dir[1:])
	}
	return "cd -- " + scripts.ShellQuote(dir)
}

// remoteListScript changes into dir, prints its real path, then its entries
// (directories end in "/", symlinks to directories count as directories).
// Exit status 3 means the directory cannot be entered.
func remoteListScript(dir string) string {
	return remoteCd(dir) + " 2>/dev/null || exit 3; pwd -P; ls -1ApL 2>/dev/null; exit 0"
}

// parseRemoteListing splits the output of remoteListScript.
func parseRemoteListing(out string) (string, []string, error) {
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSuffix(l, "\r"); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "/") {
		return "", nil, fmt.Errorf("unexpected reply from the machine")
	}
	return lines[0], lines[1:], nil
}

func runRemoteList(src remoteSource, dir string) (string, []string, error) {
	if len(src.device.Addresses) == 0 {
		return "", nil, fmt.Errorf("device has no IP addresses")
	}
	if strings.ContainsAny(dir, "\x00\n\r") {
		return "", nil, fmt.Errorf("the path contains control characters")
	}
	target, err := buildSSHTarget(src.username, src.device.Addresses[0])
	if err != nil {
		return "", nil, err
	}
	cmd, cleanup, err := newSSHCommand(target, src.password, true, remoteListScript(dir))
	if err != nil {
		return "", nil, err
	}
	defer cleanup()

	var out bytes.Buffer
	cmd.Stdout = &out
	setProcessGroup(cmd) // so a timeout kills ssh together with anything it started
	if err := cmd.Start(); err != nil {
		return "", nil, fmt.Errorf("could not start ssh: %w", err)
	}
	timer := time.AfterFunc(remoteListTimeout, func() { _ = killProcessTree(cmd) })
	err = cmd.Wait()
	timedOut := !timer.Stop()
	switch {
	case timedOut:
		return "", nil, fmt.Errorf("the machine did not answer in %s", remoteListTimeout)
	case err == nil:
		return parseRemoteListing(out.String())
	}
	switch classifySSHError(err) {
	case sshFailureBadPassword:
		return "", nil, fmt.Errorf("the saved password was rejected; connect once with s to fix it")
	case sshFailureConnection:
		return "", nil, fmt.Errorf("could not connect or log in; connect once with s to save credentials")
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 3 {
		return "", nil, fmt.Errorf("cannot open the directory")
	}
	return "", nil, err
}

// remoteListCmd asks for a listing in the background.
func remoteListCmd(id int, src remoteSource, dir string) tea.Cmd {
	return func() tea.Msg {
		d, lines, err := remoteList(src, dir)
		return remoteListMsg{id: id, dir: d, lines: lines, err: err}
	}
}

// remoteStart is the directory the first listing asks for, from the typed
// text: "" (the home directory) for empty text, "." and "~"; the text itself
// when it ends in "/"; otherwise the directory part.
func remoteStart(typed string) string {
	t := strings.TrimSpace(typed)
	switch {
	case t == "" || t == "." || t == "~":
		return ""
	case strings.HasSuffix(t, "/"):
		return t
	}
	if d := path.Dir(t); d != "." {
		return d
	}
	return ""
}

// applyRemote shows lines as the content of dir.
func (b *fileBrowser) applyRemote(dir string, lines []string) {
	b.dir, b.raw, b.err = dir, lines, ""
	b.entries = append(b.entries[:0], browserEntry{name: ".", dir: true, special: true}, browserEntry{name: "..", dir: true, special: true})
	for _, l := range lines {
		isDir := strings.HasSuffix(l, "/")
		name := strings.TrimSuffix(l, "/")
		if name == "" || (strings.HasPrefix(name, ".") && !b.hidden) || (b.dirsOnly && !isDir) {
			continue
		}
		b.entries = append(b.entries, browserEntry{name: name, dir: isDir, size: -1})
	}
	real := b.entries[2:]
	sort.SliceStable(real, func(i, j int) bool {
		a, c := real[i], real[j]
		if a.dir != c.dir {
			return a.dir
		}
		return strings.ToLower(a.name) < strings.ToLower(c.name)
	})
	b.cursor = min(2, len(b.entries)-1)
}

// openRemoteBrowser starts browsing the machine of the current path prompt.
func (m model) openRemoteBrowser() (tea.Model, tea.Cmd) {
	d := m.input.device
	switch {
	case m.needsAccountSwitch(d):
		m.notify.sshError = fmt.Errorf("%s belongs to another account than the one this computer uses; switch with m first", displayName(d))
		return m, nil
	case m.usernameForDevice(d) == "":
		m.notify.sshError = fmt.Errorf("no username saved for %s; connect once with s to set one", displayName(d))
		return m, nil
	}
	src := remoteSource{device: d, username: m.usernameForDevice(d), password: m.savedPassword(machineKey(d))}
	start := remoteStart(m.input.value)
	m.notify.sshError = nil
	m.browse = &fileBrowser{
		remote: &src, dirsOnly: m.input.path.dirsOnly(),
		loading: true, reqID: 1, first: true, startDir: start,
	}
	return m, remoteListCmd(1, src, start)
}

// handleRemoteList applies the answer to the newest request; older answers
// (the user moved on) are dropped.
func (m model) handleRemoteList(msg remoteListMsg) (tea.Model, tea.Cmd) {
	if m.browse == nil || m.browse.remote == nil || msg.id != m.browse.reqID {
		return m, nil
	}
	b := *m.browse
	b.entries = append([]browserEntry(nil), b.entries...)
	b.loading = false
	if msg.err != nil {
		if b.first && b.startDir != "" { // the typed directory is not there: start at home
			b.first, b.startDir, b.loading = false, "", true
			b.reqID++
			m.browse = &b
			return m, remoteListCmd(b.reqID, *b.remote, "")
		}
		b.first = false
		b.err = msg.err.Error()
		m.browse = &b
		return m, nil
	}
	b.first = false
	b.applyRemote(msg.dir, msg.lines)
	m.browse = &b
	return m, nil
}
