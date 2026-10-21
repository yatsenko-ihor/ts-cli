//go:build !windows

package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yatsenko-ihor/ts-cli/client"
)

// Requirements-based tests for Tailscale SSH (verification/requirements/tailscalessh.md).

func tsshModel(t *testing.T) model {
	t.Helper()
	m := flowModel(t, nil, "")
	m.set.tailscaleSSH = true
	m.peers = map[string]peerInfo{"100.64.0.1": {ssh: true, online: true}}
	old := tailscaleAvailable
	tailscaleAvailable = func() bool { return true }
	t.Cleanup(func() { tailscaleAvailable = old })
	return m
}

// Verifies: REQ-TSS-001
func TestTailscaleSSHActiveConditions(t *testing.T) {
	dev := client.Device{ID: "dev-1", Addresses: []string{"100.64.0.1"}, AccountTailnet: "work.com"}
	cases := []struct {
		name   string
		mutate func(m *model)
		want   bool
	}{
		{"all conditions met", func(m *model) {}, true},
		{"option off", func(m *model) { m.set.tailscaleSSH = false }, false},
		{"tailscale CLI missing", func(m *model) { tailscaleAvailable = func() bool { return false } }, false},
		{"another account is active", func(m *model) { m.acct.tailscaleActiveAccount = "other@example.org" }, false},
		{"same account is active", func(m *model) { m.acct.tailscaleActiveAccount = "work.com" }, true},
		{"peer does not offer ssh", func(m *model) { m.peers = map[string]peerInfo{"100.64.0.1": {online: true}} }, false},
		{"peer unknown", func(m *model) { m.peers = nil }, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := tsshModel(t)
			c.mutate(&m)
			if got := m.tailscaleSSHActive(dev); got != c.want {
				t.Fatalf("tailscaleSSHActive = %v, want %v", got, c.want)
			}
		})
	}
}

// Verifies: REQ-TSS-002
func TestSessionToATailscaleSSHMachineAsksNothingAndSavesNothing(t *testing.T) {
	m := tsshModel(t) // no default username, no saved credentials
	cap := captureTeaExec(t)
	m.ssh.pending = pendingSSH{action: sshActionSession, deviceIndex: 0, machineID: "dev-1", machineName: "laptop"}

	updated, cmd := m.continueSSH()
	got := updated.(model)
	if got.input.mode != inputNone || got.ssh.pending.action != sshActionNone {
		t.Fatalf("no username prompt, attempt complete: input=%v pending=%+v", got.input.mode, got.ssh.pending)
	}
	if got.lastMachine != "dev-1" {
		t.Fatal("the machine is remembered for the next start")
	}
	runAllCmds(cmd)
	if !cap.called {
		t.Fatal("tailscale ssh must run in this terminal")
	}
	args := cap.cmd.(*bannerExec).cmd.Args
	if strings.Join(args, " ") != "tailscale ssh 100.64.0.1" {
		t.Fatalf("args = %q (no username known: just the address)", args)
	}

	// a known username is passed on
	cap = captureTeaExec(t)
	m.ssh.username = "pi"
	m.ssh.pending = pendingSSH{action: sshActionSession, deviceIndex: 0, machineID: "dev-1", machineName: "laptop"}
	_, cmd = m.continueSSH()
	runAllCmds(cmd)
	if args := cap.cmd.(*bannerExec).cmd.Args; args[len(args)-1] != "pi@100.64.0.1" {
		t.Fatalf("args = %q", args)
	}
	if !strings.Contains(cap.cmd.(*bannerExec).banner, "Tailscale SSH") {
		t.Fatal("banner")
	}
	if done, ok := cap.fn(errors.New("exit status 1")).(tailscaleSSHDoneMsg); !ok || done.err == nil || done.name != "laptop.example.com" {
		t.Fatalf("the exec callback must report the end of the session: %#v", done)
	}

	// commands and scripts are not affected
	m.ssh.pending = pendingSSH{action: sshActionCommand, deviceIndex: 0, machineID: "dev-1", command: "uptime"}
	cap = captureTeaExec(t)
	m.ssh.username = ""
	updated, _ = m.continueSSH()
	if updated.(model).input.mode != inputUsername {
		t.Fatal("only interactive sessions use Tailscale SSH; a command still asks for the username")
	}
}

// Verifies: REQ-TSS-003
func TestTailscaleSSHInATmuxPaneAndErrors(t *testing.T) {
	m := tsshModel(t)
	dev := client.Device{ID: "d", Name: "box", Addresses: []string{"100.64.0.1"}}

	// no address / bad username
	if msg := m.launchTailscaleSSH(client.Device{Name: "x"}, "")().(tmuxOpenedMsg); msg.err == nil {
		t.Fatal("no address must fail")
	}
	if msg := m.launchTailscaleSSH(dev, "-oProxyCommand=x")().(tmuxOpenedMsg); msg.err == nil {
		t.Fatal("a bad username must fail")
	}

	// inside tmux: a pane, no in-terminal exec
	useFakeBins(t, fakeBins{})
	dir := t.TempDir()
	_ = dir
	tmuxBin := useFakeTmux(t, false)
	_ = tmuxBin
	cap := captureTeaExec(t)
	var args []string
	old := runTmux
	runTmux = func(a ...string) error { args = a; return nil }
	t.Cleanup(func() { runTmux = old })
	m = tsshModel(t)
	m.set.tmuxSplit = true
	t.Setenv("TMUX_PANE", "%3")
	msg := m.launchTailscaleSSH(dev, "root")().(tmuxOpenedMsg)
	if msg.err != nil || cap.called {
		t.Fatalf("msg %+v, in-terminal exec=%v", msg, cap.called)
	}
	joined := strings.Join(args, " ")
	if !strings.HasPrefix(joined, "split-window -h -t %3 sh -c ") || !strings.Contains(joined, "tailscale ssh") || !strings.Contains(joined, "root@100.64.0.1") {
		t.Fatalf("tmux args = %q", args)
	}

	runTmux = func(...string) error { return errors.New("no server") }
	if msg := m.launchTailscaleSSH(dev, "root")().(tmuxOpenedMsg); msg.err == nil || !strings.Contains(msg.err.Error(), "split-window failed") {
		t.Fatalf("msg = %+v", msg)
	}
}

// Verifies: REQ-TSS-004
func TestTailscaleSSHEndIsInformationNotAnError(t *testing.T) {
	m := model{}
	m.notify.sshError = errors.New("old")
	updated, _ := m.Update(tailscaleSSHDoneMsg{name: "box"})
	got := updated.(model)
	if got.notify.sshError != nil || got.notify.info != "" {
		t.Fatalf("a clean end shows nothing: %+v", got.notify)
	}
	updated, _ = m.Update(tailscaleSSHDoneMsg{name: "box", err: errors.New("exit status 1")})
	got = updated.(model)
	if got.notify.sshError != nil || !strings.Contains(got.notify.info, "box") || !strings.Contains(got.notify.info, "exit status 1") {
		t.Fatalf("info = %q err = %v", got.notify.info, got.notify.sshError)
	}
	var _ tea.Msg = tailscaleSSHDoneMsg{}
}

// Verifies: REQ-TSS-005
func TestTailscaleSSHSettingIsOffByDefaultAndToggles(t *testing.T) {
	t.Setenv("TS_CLI_CONFIG_DIR", t.TempDir())
	if loadUISettings(nil).tailscaleSSH {
		t.Fatal("off by default")
	}
	if !loadUISettings(map[string]string{uiTailscaleSSH: "on"}).tailscaleSSH {
		t.Fatal("stored on")
	}
	s := loadUISettings(nil).toggled(uiTailscaleSSH)
	if !s.tailscaleSSH || s.value(uiTailscaleSSH) != "on" || s.toggled(uiTailscaleSSH).value(uiTailscaleSSH) != "off" {
		t.Fatal("toggle")
	}
}
