package tui

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yatsenko-ihor/ts-cli/client"
	"github.com/yatsenko-ihor/ts-cli/scripts"
)

// Move (F6 in the file manager): an scp copy, and only when that copy has
// finished successfully the original is deleted. The pending move is bound to
// the exact copy; anything else that starts or cancels cancels the move, so a
// later, identical copy can never delete a source by surprise.

// moveJob is a move waiting for its copy to finish.
type moveJob struct {
	name       string
	src        string // absolute path of the original (local or on the machine)
	fromRemote bool
	spec       copySpec
	device     client.Device
}

// moveDoneMsg is the result of deleting the original.
type moveDoneMsg struct {
	name string
	err  error
}

// unsafeToDelete refuses paths that must never be removed by a move.
func unsafeToDelete(path string) bool {
	return path == "" || path == "/" || path == "." || path == ".."
}

// settleMove runs after a copy job ended: it deletes the original when this
// was the copy of the pending move and it succeeded, and forgets the move in
// every case once the copy is over.
func (m model) settleMove(res runResult) (model, tea.Cmd) {
	mv := m.afterCopy
	if mv == nil || m.run.running() {
		return m, nil
	}
	retry := res.failure == sshFailureConnection || res.failure == sshFailureBadPassword
	if retry && !res.killed {
		return m, nil // the credential flow asks again; the same copy runs once more
	}
	m.afterCopy = nil
	if m.run.spec.action != sshActionCopy || m.run.spec.copy != mv.spec || res.killed || res.exitCode != 0 || res.failure != sshFailureNone {
		m.notify.info = "Move of " + mv.name + " not finished: the original was kept"
		return m, nil
	}
	if unsafeToDelete(mv.src) {
		m.notify.sshError = fmt.Errorf("refusing to delete %q after the move", mv.src)
		return m, nil
	}
	m.notify.info = "Copied " + mv.name + "; deleting the original…"
	src, remote, dev := mv.src, mv.fromRemote, mv.device
	username, password := m.usernameForDevice(dev), m.savedPassword(machineKey(dev))
	name := mv.name
	return m, func() tea.Msg {
		if !remote {
			return moveDoneMsg{name: name, err: os.RemoveAll(src)}
		}
		out, err := fmRemoteExec(remoteSource{device: dev, username: username, password: password}, "rm -rf -- "+scripts.ShellQuote(src))
		if err != nil {
			err = fmt.Errorf("%s", firstLine(out, err))
		}
		return moveDoneMsg{name: name, err: err}
	}
}

func (m model) handleMoveDone(msg moveDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.notify.info = ""
		m.notify.sshError = fmt.Errorf("copied %s, but could not delete the original: %w", msg.name, msg.err)
		return m, nil
	}
	m.notify.info = "✓ Moved " + msg.name + " (the original is deleted)"
	return m, nil
}
