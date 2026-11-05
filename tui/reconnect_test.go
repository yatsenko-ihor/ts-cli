package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Verifies: REQ-FLW-050
func TestClassifySSHStderr(t *testing.T) {
	cases := []struct {
		name    string
		failure sshFailure
		stderr  string
		want    sshFailure
	}{
		{"timed out", sshFailureConnection, "ssh: connect to host 100.64.0.1 port 22: Connection timed out", sshFailureUnreachable},
		{"refused while booting", sshFailureConnection, "ssh: connect to host x port 22: Connection refused", sshFailureUnreachable},
		{"closed by sshd shutting down", sshFailureConnection, "Connection closed by 100.64.0.1 port 22", sshFailureUnreachable},
		{"login refused stays a login problem", sshFailureConnection, "pi@x: Permission denied (publickey,password).", sshFailureConnection},
		{"permission denied wins over a closing line", sshFailureConnection, "Permission denied\nConnection closed by x", sshFailureConnection},
		{"unknown 255 stays", sshFailureConnection, "something else", sshFailureConnection},
		{"other failures untouched", sshFailureBadPassword, "Connection timed out", sshFailureBadPassword},
	}
	for _, c := range cases {
		if got := classifySSHStderr(c.failure, c.stderr); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestTailBufferKeepsTheEnd(t *testing.T) {
	var b tailBuffer
	b.Write([]byte(strings.Repeat("a", tailBufferSize)))
	n, _ := b.Write([]byte("END"))
	if n != 3 || len(b.String()) != tailBufferSize || !strings.HasSuffix(b.String(), "END") {
		t.Fatalf("tail = %d bytes ...%q", len(b.String()), b.String()[len(b.String())-5:])
	}
}

func TestReconnectReason(t *testing.T) {
	if reconnectReason(nil) != "" || reconnectReason(errors.New("first\nsecond")) != "first" {
		t.Fatal("reason is the first line, empty for nil")
	}
}

func TestProbeSSHClassifiesUnreachableFromStderr(t *testing.T) {
	useFakeBins(t, fakeBins{ssh: true})
	t.Setenv("FAKE_SSH_EXIT", "255")
	dev := flowModel(t, nil, "pi").list.devices[0]

	t.Setenv("FAKE_SSH_ERR", "ssh: connect to host 100.64.0.1 port 22: No route to host")
	if msg := probeSSH(dev, "pi", "")().(probeMsg); msg.failure != sshFailureUnreachable {
		t.Fatalf("failure = %v", msg.failure)
	}
	t.Setenv("FAKE_SSH_ERR", "pi@100.64.0.1: Permission denied (publickey).")
	if msg := probeSSH(dev, "pi", "")().(probeMsg); msg.failure != sshFailureConnection {
		t.Fatalf("failure = %v", msg.failure)
	}
}

func TestSessionStderrDecidesUnreachable(t *testing.T) {
	useFakeBins(t, fakeBins{ssh: true})
	t.Setenv("FAKE_SSH_EXIT", "255")
	t.Setenv("FAKE_SSH_ERR", "ssh: connect to host 100.64.0.1 port 22: Connection refused")
	c := captureTeaExec(t)
	m := flowModel(t, nil, "pi")
	m.sshToDevice(m.list.devices[0], "pi", "")()
	var out strings.Builder
	c.cmd.SetStdout(&out)
	c.cmd.SetStderr(&out)
	err := c.cmd.Run()
	if err == nil {
		t.Fatal("fake ssh exits 255")
	}
	if msg := c.fn(err).(sshMsg); msg.failure != sshFailureUnreachable {
		t.Fatalf("failure = %v", msg.failure)
	}
	if !strings.Contains(out.String(), "Connection refused") {
		t.Fatal("stderr must still reach the terminal")
	}
}

func reconnectFlow(t *testing.T) model {
	t.Helper()
	m := flowModel(t, nil, "pi")
	m.ssh.pending = pendingSSH{action: sshActionSession, deviceIndex: 0, machineID: "dev-1", machineName: "laptop.example.com", username: "pi", password: "pw"}
	return m
}

// Verifies: REQ-FLW-051
func TestUnreachableConnectCountsDownAndRetries(t *testing.T) {
	useFakeBins(t, fakeBins{ssh: true})
	t.Setenv("FAKE_SSH_EXIT", "255")
	t.Setenv("FAKE_SSH_ERR", "Connection timed out")
	m := reconnectFlow(t)

	updated, cmd := m.finishSSHAttempt(sshFailureUnreachable, errors.New("exit status 255"))
	m = asModel(t, updated)
	if m.ssh.retryMode || m.input.mode != inputNone || m.ssh.reconnect == nil || cmd == nil {
		t.Fatal("an unreachable machine must wait and retry, not ask for credentials")
	}
	if !strings.Contains(m.View(), "Attempt 2 in 5 s") {
		t.Fatalf("view: %q", m.View())
	}
	seq := m.ssh.reconnect.seq

	// A tick of another loop is ignored; ours counts down.
	if _, c := m.handleReconnectTick(reconnectTickMsg{seq: seq + 9}); c != nil {
		t.Fatal("stale tick must be ignored")
	}
	for i := 0; i < reconnectDelay-1; i++ {
		updated, cmd = m.handleReconnectTick(reconnectTickMsg{seq: seq})
		m = asModel(t, updated)
		if cmd == nil || m.ssh.reconnect.probing {
			t.Fatalf("tick %d: still waiting", i)
		}
	}
	updated, cmd = m.handleReconnectTick(reconnectTickMsg{seq: seq})
	m = asModel(t, updated)
	if !m.ssh.reconnect.probing || m.ssh.reconnect.attempt != 2 || cmd == nil {
		t.Fatal("the wait is over: probe again")
	}
	if !strings.Contains(m.View(), "Attempt 2: connecting") {
		t.Fatalf("view: %q", m.View())
	}
	if _, c := m.handleReconnectTick(reconnectTickMsg{seq: seq}); c != nil {
		t.Fatal("no ticks while an attempt runs")
	}

	// Still down: wait again, keeping the loop.
	updated, _ = m.handleProbe(probeMsg{failure: sshFailureUnreachable, err: errors.New("down\nmore")})
	m = asModel(t, updated)
	if m.ssh.reconnect == nil || m.ssh.reconnect.probing || m.ssh.reconnect.attempt != 2 || m.ssh.reconnect.reason != "down" {
		t.Fatalf("state = %+v", m.ssh.reconnect)
	}
	if !strings.Contains(m.View(), "Last error: down") {
		t.Fatal("the reason must be shown")
	}

	// It answers: the loop ends and the session starts.
	c := captureTeaExec(t)
	updated, _ = m.handleProbe(probeMsg{failure: sshFailureNone})
	m = asModel(t, updated)
	if m.ssh.reconnect != nil || !c.called {
		t.Fatal("an answering machine ends the loop and opens the session")
	}
}

func TestReconnectProbeUsesSavedPasswordAndHandlesMissingDevice(t *testing.T) {
	useFakeBins(t, fakeBins{ssh: true})
	m := reconnectFlow(t)
	m.ssh.pending.password = ""
	if m.reconnectProbe() == nil {
		t.Fatal("probe command expected")
	}
	m.ssh.pending.deviceIndex = 7
	msg := m.reconnectProbe()().(probeMsg)
	if msg.failure != sshFailureExec {
		t.Fatalf("failure = %v", msg.failure)
	}
	updated, _ := m.handleProbe(msg)
	if m2 := asModel(t, updated); m2.ssh.reconnect != nil {
		t.Fatal("a vanished device cancels the attempt")
	}
}

// Verifies: REQ-FLW-052
func TestEscStopsReconnecting(t *testing.T) {
	m := reconnectFlow(t)
	updated, _ := m.finishSSHAttempt(sshFailureUnreachable, errors.New("x"))
	m = asModel(t, updated)

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if asModel(t, updated).ssh.reconnect == nil {
		t.Fatal("other keys are ignored")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = asModel(t, updated)
	if m.ssh.reconnect != nil || m.ssh.pending.action != sshActionNone || !strings.Contains(m.notify.info, "stopped") {
		t.Fatalf("esc must stop: %+v %q", m.ssh.reconnect, m.notify.info)
	}
	updated, _ = m.Update(reconnectTickMsg{seq: 1})
	if asModel(t, updated).ssh.reconnect != nil {
		t.Fatal("a late tick must not revive the loop")
	}
}

// Verifies: REQ-FLW-053
func TestDroppedSessionAsksThenReconnects(t *testing.T) {
	useFakeBins(t, fakeBins{ssh: true})
	m := reconnectFlow(t)
	m.ssh.pending.verified = true
	m.ping = map[string]pingState{"dev-1": pingOK}
	updated, _ := m.finishSSHAttempt(sshFailureUnreachable, errors.New("exit status 255"))
	m = asModel(t, updated)
	if !m.ssh.askReconnect || m.ping["dev-1"] != pingUnknown || m.ssh.retryMode {
		t.Fatal("must ask, and forget the old ping result")
	}
	if !strings.Contains(m.View(), "Reconnect automatically?") {
		t.Fatalf("view: %q", m.View())
	}

	key := func(m model, k tea.KeyMsg) model {
		u, _ := m.Update(k)
		return asModel(t, u)
	}
	// Unknown keys change nothing; down/up toggle the choice.
	if m2 := key(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("z")}); !m2.ssh.askReconnect {
		t.Fatal("unknown key")
	}
	m2 := key(m, tea.KeyMsg{Type: tea.KeyDown})
	if m2.ssh.askCursor != 1 {
		t.Fatal("down moves to No")
	}
	if m2 = key(m2, tea.KeyMsg{Type: tea.KeyEnter}); m2.ssh.askReconnect || m2.ssh.pending.action != sshActionNone ||
		m2.notify.sshError == nil || !strings.Contains(m2.notify.sshError.Error(), "press s to reconnect") {
		t.Fatalf("No must leave a hint: %v", m2.notify.sshError)
	}
	if m3 := key(m, tea.KeyMsg{Type: tea.KeyEsc}); m3.ssh.askReconnect {
		t.Fatal("esc declines")
	}

	// Yes (enter on the default, or y) starts probing at once.
	for _, k := range []tea.KeyMsg{{Type: tea.KeyEnter}, {Type: tea.KeyRunes, Runes: []rune("y")}} {
		u, cmd := m.Update(k)
		m3 := asModel(t, u)
		if m3.ssh.askReconnect || m3.ssh.reconnect == nil || !m3.ssh.reconnect.probing || cmd == nil {
			t.Fatalf("yes must start reconnecting: %+v", m3.ssh.reconnect)
		}
	}
}

func TestReconnectLoopEndsOnLoginRefusal(t *testing.T) {
	m := reconnectFlow(t)
	m.ssh.reconnect = &reconnectState{seq: 1, attempt: 3, probing: true}
	updated, _ := m.handleProbe(probeMsg{failure: sshFailureBadPassword, err: errors.New("exit status 5")})
	m = asModel(t, updated)
	if m.ssh.reconnect != nil || m.input.mode != inputPassword {
		t.Fatal("once the machine answers, a refused login is handled as usual")
	}
}
