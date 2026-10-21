package tui

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yatsenko-ihor/ts-cli/client"
	"github.com/yatsenko-ihor/ts-cli/scripts"
)

// SSH sessions in a tmux split.
//
// When ts-cli runs inside tmux, `s` opens the ssh session in a new pane next
// to the list (tmux split-window -h) instead of taking over ts-cli's own
// terminal. The pane is an ordinary terminal, so vim, htop, the mouse,
// scrolling and copying all work with no terminal emulation in ts-cli.
//
// ts-cli cannot see how a session in another pane ends, so the credentials
// are always proven first with the same non-interactive probe the normal flow
// uses for a typed password; only then is the pane opened.
//
// A password never appears in tmux's or ssh's argument list: it is written to
// a private named pipe that the pane's shell opens as file descriptor 3 for
// `sshpass -d 3`; the pipe holds no data on disk and is removed at once.

// Seams for tests.
var (
	inTmux  = func() bool { return os.Getenv("TMUX") != "" }
	runTmux = func(args ...string) error { return exec.Command("tmux", args...).Run() }
)

// fifoFeedTimeout is how long the password writer waits for the pane's shell
// to open the pipe before giving up.
const fifoFeedTimeout = 15 * time.Second

// tmuxSplitActive reports whether a session with this password should open in
// a tmux pane: the option is on, ts-cli runs inside tmux, tmux is installed,
// and a needed password can be passed (sshpass).
func (m model) tmuxSplitActive(password string) bool {
	if !m.set.tmuxSplit || runtime.GOOS == "windows" || !inTmux() {
		return false
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		return false
	}
	return password == "" || sshpassAvailable()
}

// tmuxOpenedMsg is returned when the pane was (or could not be) created.
type tmuxOpenedMsg struct {
	name string
	err  error
}

// tmuxSSHScript is the POSIX script run by the pane. fifo and dir are empty
// when no password is passed. The pane stays open after a failure so the
// error can be read.
func tmuxSSHScript(target, fifo, dir string) string {
	args := []string{"-o", "StrictHostKeyChecking=accept-new", "-o", "ConnectTimeout=15", target}
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = scripts.ShellQuote(a)
	}
	run := "ssh " + strings.Join(quoted, " ")
	if fifo != "" {
		run = fmt.Sprintf("exec 3<%s; rm -rf %s; sshpass -d 3 %s", scripts.ShellQuote(fifo), scripts.ShellQuote(dir), run)
	}
	return paneScript(run)
}

// paneScript runs the command line and, after a failure, keeps the pane open
// until Enter so the error can be read.
func paneScript(run string) string {
	return run + `; rc=$?; if [ $rc -ne 0 ]; then printf '\nssh exited with status %s. Press Enter to close. ' "$rc"; read _; fi`
}

// tmuxSplitArgs are the tmux arguments that open script in a pane to the
// right of ts-cli's own pane. The script is wrapped in `sh -c` so it does not
// depend on the user's default shell.
func tmuxSplitArgs(script, pane string) []string {
	args := []string{"split-window", "-h"}
	if pane != "" {
		args = append(args, "-t", pane)
	}
	return append(args, "sh -c "+scripts.ShellQuote(script))
}

// openInTmux opens the ssh session to device in a tmux pane.
func openInTmux(device client.Device, username, password string) tea.Cmd {
	return func() tea.Msg {
		name := displayName(device)
		if len(device.Addresses) == 0 {
			return tmuxOpenedMsg{name: name, err: fmt.Errorf("device has no IP addresses")}
		}
		target, err := buildSSHTarget(username, device.Addresses[0])
		if err != nil {
			return tmuxOpenedMsg{name: name, err: err}
		}

		var fifo, dir string
		if password != "" {
			if fifo, dir, err = makePasswordFIFO(); err != nil {
				return tmuxOpenedMsg{name: name, err: err}
			}
		}
		if err := runTmux(tmuxSplitArgs(tmuxSSHScript(target, fifo, dir), os.Getenv("TMUX_PANE"))...); err != nil {
			if dir != "" {
				_ = os.RemoveAll(dir)
			}
			return tmuxOpenedMsg{name: name, err: fmt.Errorf("tmux split-window failed: %w", err)}
		}
		if password != "" {
			go feedPasswordFIFO(fifo, dir, password, fifoFeedTimeout)
		}
		return tmuxOpenedMsg{name: name}
	}
}
