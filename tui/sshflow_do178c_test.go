package tui

// Requirements-based tests for tui/sshflow.go (see
// verification/requirements/sshflow.md). The state machine decides when
// credentials are saved or forgotten, so every decision has an independence
// pair here (listed in the MC/DC section of the requirement file).

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yatsenko-ihor/ts-cli/client"
	"github.com/yatsenko-ihor/ts-cli/config"
	"github.com/yatsenko-ihor/ts-cli/scripts"
	"github.com/yatsenko-ihor/ts-cli/util"
)

// runAllCmds executes cmd (flattening tea.Batch) and returns every message.
// Only used for persistence commands; never pass process-starting commands.
func runAllCmds(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, runAllCmds(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func loadCfg(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func encFor(t *testing.T, pw string) string {
	t.Helper()
	enc, err := util.EncryptPassword(pw)
	if err != nil {
		t.Fatal(err)
	}
	return enc
}

// pendingFlow builds the standard single-device model with a pending attempt.
func pendingFlow(t *testing.T, defaultUser string, p pendingSSH) model {
	t.Helper()
	m := flowModel(t, nil, defaultUser)
	if p.machineID == "" {
		p.machineID = "dev-1"
	}
	if p.machineName == "" {
		p.machineName = "laptop.example.com"
	}
	m.ssh.pending = p
	return m
}

// ─── small helpers ────────────────────────────────────────────────────────────

// Verifies: REQ-FLW-001, REQ-FLW-002
func TestDO178CMachineKeyAndDisplayName(t *testing.T) {
	cases := []struct {
		name         string
		d            client.Device
		key, display string
	}{
		{"id wins for key, name wins for display", client.Device{ID: "42", Hostname: "h", Name: "n"}, "42", "n"},
		{"hostname fallback", client.Device{Hostname: "h", Name: "n"}, "h", "n"},
		{"hostname used for display when name empty", client.Device{ID: "42", Hostname: "h"}, "42", "h"},
		{"everything empty", client.Device{}, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := machineKey(c.d); got != c.key {
				t.Fatalf("machineKey = %q, want %q", got, c.key)
			}
			if got := displayName(c.d); got != c.display {
				t.Fatalf("displayName = %q, want %q", got, c.display)
			}
		})
	}
}

// Verifies: REQ-FLW-003
func TestDO178CSSHPassAvailable(t *testing.T) {
	useFakeBins(t, fakeBins{ssh: true})
	if sshpassAvailable() {
		t.Fatal("sshpass reported available with none on PATH")
	}
	useFakeBins(t, fakeBins{sshpass: true})
	if !sshpassAvailable() {
		t.Fatal("sshpass on PATH not detected")
	}
}

// Verifies: REQ-FLW-004
func TestDO178CSSHJobLabel(t *testing.T) {
	sc := &scripts.Script{Name: "Disk usage"}
	cases := []struct {
		name string
		job  sshJob
		want string
	}{
		{"script", sshJob{action: sshActionScript, script: sc, command: "ignored"}, "Disk usage"},
		{"script action without script", sshJob{action: sshActionScript, command: "ignored"}, ""},
		{"command", sshJob{action: sshActionCommand, command: "uptime", script: sc}, "uptime"},
		{"empty command", sshJob{action: sshActionCommand}, ""},
		{"session", sshJob{action: sshActionSession, command: "x", script: sc}, ""},
		{"none", sshJob{}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.job.label(); got != c.want {
				t.Fatalf("label = %q, want %q", got, c.want)
			}
		})
	}
}

// ─── beginSSH / needsAccountSwitch / continueSSH ─────────────────────────────

// Verifies: REQ-FLW-005
func TestDO178CBeginSSHIndexBounds(t *testing.T) {
	for _, idx := range []int{-1, 1, 2, 1000} { // one device: only 0 is valid
		m := flowModel(t, nil, "admin")
		m.notify.sshError = errors.New("keep")
		updated, cmd := m.beginSSH(idx, sshJob{action: sshActionSession})
		m2 := asModel(t, updated)
		if cmd != nil || m2.ssh.pending.action != sshActionNone || m2.notify.sshError == nil || m2.input.mode != inputNone {
			t.Fatalf("index %d must be a no-op: cmd=%v pending=%+v", idx, cmd != nil, m2.ssh.pending)
		}
	}
	m := flowModel(t, nil, "")
	if _, cmd := m.beginSSH(0, sshJob{action: sshActionSession}); cmd != nil {
		t.Fatal("index 0 with no username must only open a prompt")
	}
}

// Verifies: REQ-FLW-006
func TestDO178CBeginSSHInitialisesFreshPending(t *testing.T) {
	m := flowModel(t, nil, "")
	m.ssh.pending = pendingSSH{action: sshActionSession, password: "left-over", passwordTried: true, usedSavedPass: true,
		verified: true, askPasswordNext: true, username: "old"}
	m.notify.sshError = errors.New("old error")
	sc := &scripts.Script{Name: "s"}

	updated, _ := m.beginSSH(0, sshJob{action: sshActionScript, command: "c", script: sc, args: []string{"a"}})
	p := asModel(t, updated).ssh.pending
	if p.action != sshActionScript || p.deviceIndex != 0 || p.machineID != "dev-1" || p.machineName != "laptop.example.com" ||
		p.command != "c" || p.script != sc || len(p.args) != 1 {
		t.Fatalf("pending = %+v", p)
	}
	if p.password != "" || p.passwordTried || p.usedSavedPass || p.verified || p.askPasswordNext || p.username != "" {
		t.Fatalf("state from the previous attempt leaked into the new one: %+v", p)
	}
	if asModel(t, updated).notify.sshError != nil {
		t.Fatal("previous error must be cleared")
	}
}

// Verifies: REQ-FLW-007, REQ-FLW-008
func TestDO178CAccountSwitch(t *testing.T) {
	cases := []struct {
		name          string
		deviceTailnet string
		activeTailnet string
		wantSwitch    bool
	}{
		{"device tailnet unknown", "", "me@example.com", false},
		{"daemon login unknown", "other@example.org", "", false},
		{"both unknown", "", "", false},
		{"same tailnet", "me@example.com", "me@example.com", false},
		{"same tailnet other case", "ME@Example.com", "me@example.com", false},
		{"daemon login truncated to user@", "me@example.com", "me@", false},
		{"different tailnet", "other@example.org", "me@example.com", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := flowModel(t, nil, "") // no username: a non-switch ends in the username prompt
			m.list.filteredDevices[0].AccountTailnet = c.deviceTailnet
			m.acct.tailscaleActiveAccount = c.activeTailnet
			if got := m.needsAccountSwitch(m.list.filteredDevices[0]); got != c.wantSwitch {
				t.Fatalf("needsAccountSwitch = %v, want %v", got, c.wantSwitch)
			}
			updated, cmd := m.beginSSH(0, sshJob{action: sshActionSession})
			m2 := asModel(t, updated)
			if c.wantSwitch {
				if cmd == nil || m2.input.mode != inputNone {
					t.Fatalf("expected the account switch command and no prompt, mode=%v", m2.input.mode)
				}
				if m2.ssh.pending.action != sshActionSession || m2.ssh.pending.machineID != "dev-1" {
					t.Fatalf("pending must survive the switch: %+v", m2.ssh.pending)
				}
			} else if m2.input.mode != inputUsername {
				t.Fatalf("expected the flow to continue (username prompt), mode=%v", m2.input.mode)
			}
		})
	}
}

// Verifies: REQ-FLW-009
func TestDO178CContinueSSHWithoutActionIsNoOp(t *testing.T) {
	m := flowModel(t, nil, "admin")
	m.input = textInput{mode: inputSearch, value: "q"}
	updated, cmd := m.continueSSH()
	m2 := asModel(t, updated)
	if cmd != nil || m2.input.mode != inputSearch || m2.ssh.pending.username != "" {
		t.Fatalf("no-op expected: cmd=%v input=%+v pending=%+v", cmd != nil, m2.input, m2.ssh.pending)
	}
}

// Verifies: REQ-FLW-010
func TestDO178CContinueSSHUsernameResolution(t *testing.T) {
	useFakeBins(t, fakeBins{ssh: true})
	captureTeaExec(t)
	cases := []struct {
		name        string
		pendingUser string
		creds       map[string]machineCredential
		defaultUser string
		wantUser    string
		wantPrompt  bool
	}{
		{"pending username kept over everything", "chosen", map[string]machineCredential{"dev-1": {username: "saved"}}, "dflt", "chosen", false},
		{"machine credential beats default", "", map[string]machineCredential{"dev-1": {username: "saved"}}, "dflt", "saved", false},
		{"default when machine has no credential", "", nil, "dflt", "dflt", false},
		{"default when credential for another machine only", "", map[string]machineCredential{"other": {username: "saved"}}, "dflt", "dflt", false},
		{"credential with empty username falls back to default", "", map[string]machineCredential{"dev-1": {passwordEncrypted: "x"}}, "dflt", "dflt", false},
		{"nothing known prompts for username", "", nil, "", "", true},
		{"empty credential and no default prompts", "", map[string]machineCredential{"dev-1": {name: "n"}}, "", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := flowModel(t, c.creds, c.defaultUser)
			m.ssh.pending = pendingSSH{action: sshActionSession, machineID: "dev-1", machineName: "n", username: c.pendingUser}
			updated, cmd := m.continueSSH()
			m2 := asModel(t, updated)
			if m2.ssh.pending.username != c.wantUser {
				t.Fatalf("username = %q, want %q", m2.ssh.pending.username, c.wantUser)
			}
			if c.wantPrompt {
				if m2.input.mode != inputUsername || cmd != nil {
					t.Fatalf("expected username prompt, mode=%v cmd=%v", m2.input.mode, cmd != nil)
				}
			} else if m2.input.mode != inputNone || cmd == nil {
				t.Fatalf("expected the attempt to run, mode=%v cmd=%v", m2.input.mode, cmd != nil)
			}
		})
	}
}

// Verifies: REQ-FLW-011
func TestDO178CContinueSSHAskPasswordNext(t *testing.T) {
	t.Run("username known: password prompt, flag consumed, nothing started", func(t *testing.T) {
		m := pendingFlow(t, "", pendingSSH{action: sshActionSession, username: "root", askPasswordNext: true})
		updated, cmd := m.continueSSH()
		m2 := asModel(t, updated)
		if m2.input.mode != inputPassword || cmd != nil || m2.ssh.pending.askPasswordNext {
			t.Fatalf("mode=%v cmd=%v flag=%v", m2.input.mode, cmd != nil, m2.ssh.pending.askPasswordNext)
		}
	})
	t.Run("username still unknown: username is asked first and the flag is kept", func(t *testing.T) {
		m := pendingFlow(t, "", pendingSSH{action: sshActionSession, askPasswordNext: true})
		updated, _ := m.continueSSH()
		m2 := asModel(t, updated)
		if m2.input.mode != inputUsername || !m2.ssh.pending.askPasswordNext {
			t.Fatalf("mode=%v flag=%v", m2.input.mode, m2.ssh.pending.askPasswordNext)
		}
	})
}

// ─── runPendingAttempt ────────────────────────────────────────────────────────

// Verifies: REQ-FLW-012
func TestDO178CRunPendingAttemptStaleDeviceIndex(t *testing.T) {
	for _, idx := range []int{-1, 1, 7} {
		m := pendingFlow(t, "admin", pendingSSH{action: sshActionSession, username: "admin", password: "typed", deviceIndex: idx})
		updated, cmd := m.runPendingAttempt()
		m2 := asModel(t, updated)
		if cmd != nil || !pendingZero(m2.ssh.pending) || m2.notify.sshError == nil {
			t.Fatalf("idx %d: cmd=%v pending=%+v err=%v", idx, cmd != nil, m2.ssh.pending, m2.notify.sshError)
		}
		if !strings.Contains(m2.notify.sshError.Error(), "select the device again") {
			t.Fatalf("err = %v", m2.notify.sshError)
		}
	}
}

// Verifies: REQ-FLW-013, REQ-FLW-014
func TestDO178CRunPendingAttemptSessionRouting(t *testing.T) {
	cases := []struct {
		name      string
		sshpass   bool
		typed     string
		saved     string
		verified  bool
		wantProbe bool
		wantPass  string // password the process must receive on fd 3 ("" = no sshpass)
	}{
		{"typed, unverified, sshpass: probe first", true, "typed", "", false, true, "typed"},
		{"typed, verified: straight to session", true, "typed", "", true, false, "typed"},
		{"typed, unverified, no sshpass: straight to session", false, "typed", "", false, false, ""},
		{"no typed password, none saved: session", true, "", "", false, false, ""},
		{"saved password only: session without probe", true, "", "saved", false, false, "saved"},
		{"typed password beats saved one in the probe", true, "typed", "saved", false, true, "typed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := useFakeBins(t, fakeBins{ssh: true, sshpass: c.sshpass})
			cap := captureTeaExec(t)
			m := flowModel(t, nil, "admin")
			if c.saved != "" {
				m.ssh.machineCreds["dev-1"] = machineCredential{username: "admin", passwordEncrypted: encFor(t, c.saved)}
			}
			m.ssh.pending = pendingSSH{action: sshActionSession, machineID: "dev-1", machineName: "n", username: "admin",
				password: c.typed, verified: c.verified, usedSavedPass: true /* must be recomputed */}

			updated, cmd := m.runPendingAttempt()
			p := asModel(t, updated).ssh.pending
			if p.usedSavedPass != (c.typed == "" && c.saved != "") {
				t.Fatalf("usedSavedPass = %v", p.usedSavedPass)
			}
			msg := runCmdMsg(t, cmd)
			if c.wantProbe {
				if _, ok := msg.(probeMsg); !ok || cap.called {
					t.Fatalf("expected a probe, got %#v (exec called=%v)", msg, cap.called)
				}
			} else {
				if _, ok := msg.(capturedExecMsg); !ok || !cap.called {
					t.Fatalf("expected an interactive session, got %#v", msg)
				}
			}
			// What reached the process: password only via fd 3.
			if c.wantPass != "" {
				if c.wantProbe {
					if got := fakeFile(t, dir, "sshpass.fd3"); got != c.wantPass {
						t.Fatalf("fd3 = %q, want %q", got, c.wantPass)
					}
				} else {
					args := cap.cmd.(*bannerExec).cmd.Args
					if filepath := args[0]; !strings.HasSuffix(filepath, "sshpass") {
						t.Fatalf("session must run through sshpass, args %q", args)
					}
					for _, a := range args[1:] { // [0] is the temp path, which carries the test name
						if strings.Contains(a, c.wantPass) {
							t.Fatalf("password in argv: %q", args)
						}
					}
				}
			}
		})
	}
}

// Verifies: REQ-FLW-015
func TestDO178CRunPendingAttemptStreamedJobs(t *testing.T) {
	dir := useFakeBins(t, fakeBins{ssh: true, sshpass: true})
	t.Setenv("FAKE_SSH_OUT", "out")
	sc := scripts.Parse("s.sh", []byte("#!/bin/sh\necho hi\n"))

	for _, spec := range []sshJob{
		{action: sshActionCommand, command: "uptime"},
		{action: sshActionScript, script: &sc, args: []string{"x"}},
	} {
		m := flowModel(t, nil, "admin")
		m.ssh.machineCreds["dev-1"] = machineCredential{username: "admin", passwordEncrypted: encFor(t, "saved-pw")}
		m.ssh.pending = pendingSSH{action: spec.action, machineID: "dev-1", machineName: "n", username: "admin",
			command: spec.command, script: spec.script, args: spec.args}
		updated, cmd := m.runPendingAttempt()
		m2 := asModel(t, updated)
		if cmd == nil || len(m2.run.runs) != 1 || m2.run.runs[0].job == nil || m2.run.runs[0].status != runRunning ||
			!m2.run.visible || m2.run.multi || m2.run.label != spec.label() {
			t.Fatalf("run not started: cmd=%v run=%+v", cmd != nil, m2.run)
		}
		if !m2.ssh.pending.usedSavedPass || m2.ssh.pending.action != spec.action {
			t.Fatalf("pending = %+v", m2.ssh.pending)
		}
		_, res := drainJobTimeout(t, m2.run.runs[0].job)
		if res.err != nil {
			t.Fatalf("res = %+v", res)
		}
		if got := fakeFile(t, dir, "sshpass.fd3"); got != "saved-pw" {
			t.Fatalf("fd3 = %q", got)
		}
	}
}

// Verifies: REQ-FLW-016
func TestDO178CRunPendingAttemptJobStartFailure(t *testing.T) {
	useFakeBins(t, fakeBins{}) // no ssh at all
	m := pendingFlow(t, "admin", pendingSSH{action: sshActionCommand, username: "admin", command: "uptime", password: "typed"})
	updated, cmd := m.runPendingAttempt()
	m2 := asModel(t, updated)
	if cmd != nil || m2.notify.sshError == nil || !strings.Contains(m2.notify.sshError.Error(), "could not start ssh") {
		t.Fatalf("cmd=%v err=%v", cmd != nil, m2.notify.sshError)
	}
	if !pendingZero(m2.ssh.pending) || len(m2.run.runs) != 0 {
		t.Fatalf("nothing may be started or remembered: %+v", m2.ssh.pending)
	}
	if _, ok := m2.ssh.machineCreds["dev-1"]; ok {
		t.Fatal("nothing may be saved when ssh could not even be started")
	}
}

// Verifies: REQ-FLW-017
func TestDO178CRunPendingAttemptUnknownActionDoesNothing(t *testing.T) {
	m := pendingFlow(t, "admin", pendingSSH{action: sshActionNone, username: "admin"})
	updated, cmd := m.runPendingAttempt()
	if cmd != nil || asModel(t, updated).ssh.pending.username != "admin" {
		t.Fatal("expected a no-op")
	}
	m = pendingFlow(t, "admin", pendingSSH{action: sshAction(99), username: "admin"})
	if _, cmd := m.runPendingAttempt(); cmd != nil {
		t.Fatal("unknown action must not start anything")
	}
}

// ─── password / username lookups ──────────────────────────────────────────────

// Verifies: REQ-FLW-018
func TestDO178CSavedPassword(t *testing.T) {
	m := flowModel(t, nil, "")
	m.ssh.machineCreds["good"] = machineCredential{passwordEncrypted: encFor(t, "it's a \"secret\" $(x)")}
	m.ssh.machineCreds["empty"] = machineCredential{username: "u"}
	m.ssh.machineCreds["garbage"] = machineCredential{passwordEncrypted: "not-base64!!"}
	m.ssh.machineCreds["tampered"] = machineCredential{passwordEncrypted: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}
	cases := map[string]string{
		"good":     "it's a \"secret\" $(x)",
		"empty":    "",
		"garbage":  "",
		"tampered": "",
		"missing":  "",
	}
	for key, want := range cases {
		if got := m.savedPassword(key); got != want {
			t.Errorf("savedPassword(%q) = %q, want %q", key, got, want)
		}
	}
	var nilMap model
	if got := nilMap.savedPassword("x"); got != "" {
		t.Errorf("nil map -> %q", got)
	}
}

// Verifies: REQ-FLW-019
func TestDO178CPasswordForDevice(t *testing.T) {
	dev := client.Device{ID: "dev-1"}
	mk := func(pending pendingSSH, savedFor string) model {
		m := flowModel(t, nil, "")
		if savedFor != "" {
			m.ssh.machineCreds[savedFor] = machineCredential{passwordEncrypted: encFor(t, "saved")}
		}
		m.ssh.pending = pending
		return m
	}
	cases := []struct {
		name string
		m    model
		want string
	}{
		{"typed password for the same machine wins", mk(pendingSSH{machineID: "dev-1", password: "typed"}, "dev-1"), "typed"},
		{"typed password for another machine is ignored", mk(pendingSSH{machineID: "other", password: "typed"}, "dev-1"), "saved"},
		{"no typed password: saved one", mk(pendingSSH{machineID: "dev-1"}, "dev-1"), "saved"},
		{"typed password but nothing saved", mk(pendingSSH{machineID: "dev-1", password: "typed"}, ""), "typed"},
		{"nothing at all", mk(pendingSSH{}, ""), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.m.passwordForDevice(dev); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

// Verifies: REQ-FLW-020
func TestDO178CUsernameForDevice(t *testing.T) {
	dev := client.Device{ID: "dev-1"}
	cases := []struct {
		name  string
		creds map[string]machineCredential
		dflt  string
		want  string
	}{
		{"machine credential wins", map[string]machineCredential{"dev-1": {username: "pi"}}, "admin", "pi"},
		{"credential without username falls back", map[string]machineCredential{"dev-1": {name: "n"}}, "admin", "admin"},
		{"no credential falls back", nil, "admin", "admin"},
		{"credential of another machine ignored", map[string]machineCredential{"x": {username: "pi"}}, "admin", "admin"},
		{"nothing known", nil, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := flowModel(t, c.creds, c.dflt)
			if got := m.usernameForDevice(dev); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

// ─── finishSSHAttempt ─────────────────────────────────────────────────────────

// Verifies: REQ-FLW-021
func TestDO178CFinishWithoutPendingIsNoOp(t *testing.T) {
	for _, f := range []sshFailure{sshFailureNone, sshFailureBadPassword, sshFailureConnection, sshFailureRemoteExit, sshFailureExec} {
		m := flowModel(t, nil, "admin")
		updated, cmd := m.finishSSHAttempt(f, errors.New("x"))
		m2 := asModel(t, updated)
		if cmd != nil || len(m2.ssh.machineCreds) != 0 || m2.notify.sshError != nil || m2.lastMachine != "" || m2.input.mode != inputNone {
			t.Fatalf("failure %v changed state", f)
		}
	}
}

// Verifies: REQ-FLW-022
func TestDO178CVerifiedSessionDroppedWith255(t *testing.T) {
	useFakeBins(t, fakeBins{sshpass: true})
	base := pendingSSH{action: sshActionSession, username: "pi", password: "typed", verified: true}
	err255 := errors.New("exit status 255")

	t.Run("verified + session + 255: remote exit, credentials kept", func(t *testing.T) {
		m := pendingFlow(t, "pi", base)
		updated, _ := m.finishSSHAttempt(sshFailureConnection, err255)
		m2 := asModel(t, updated)
		if m2.ssh.retryMode || m2.input.mode != inputNone || m2.ssh.pending.action != sshActionNone {
			t.Fatalf("must end quietly: retry=%v mode=%v", m2.ssh.retryMode, m2.input.mode)
		}
		if m2.notify.sshError == nil || !strings.Contains(m2.notify.sshError.Error(), "connection closed") ||
			!strings.Contains(m2.notify.sshError.Error(), "exit status 255") {
			t.Fatalf("err = %v", m2.notify.sshError)
		}
		if m2.ssh.machineCreds["dev-1"].passwordEncrypted == "" {
			t.Fatal("password must be (still) saved")
		}
	})
	t.Run("not verified: a 255 is a login failure", func(t *testing.T) {
		p := base
		p.verified = false
		p.passwordTried = true // so the outcome is the retry menu, not another prompt
		m := pendingFlow(t, "pi", p)
		updated, _ := m.finishSSHAttempt(sshFailureConnection, err255)
		m2 := asModel(t, updated)
		if !m2.ssh.retryMode || m2.ssh.pending.action == sshActionNone {
			t.Fatal("unverified 255 must open the retry menu and keep the attempt")
		}
		if _, ok := m2.ssh.machineCreds["dev-1"]; ok {
			t.Fatal("nothing may be saved after a login failure")
		}
	})
	t.Run("verified but a command, not a session: a 255 is a connection failure", func(t *testing.T) {
		p := base
		p.action = sshActionCommand
		p.passwordTried = true
		m := pendingFlow(t, "pi", p)
		updated, _ := m.finishSSHAttempt(sshFailureConnection, err255)
		if m2 := asModel(t, updated); !m2.ssh.retryMode {
			t.Fatal("expected the retry menu")
		}
	})
	t.Run("verified session but bad password stays a bad password", func(t *testing.T) {
		m := pendingFlow(t, "pi", base)
		updated, _ := m.finishSSHAttempt(sshFailureBadPassword, errors.New("exit status 5"))
		m2 := asModel(t, updated)
		if m2.input.mode != inputPassword || m2.ssh.pending.password != "" {
			t.Fatalf("mode=%v pw=%q", m2.input.mode, m2.ssh.pending.password)
		}
	})
}

// Verifies: REQ-FLW-023
func TestDO178CFinishSuccessPersistsAndClears(t *testing.T) {
	cases := []struct {
		name    string
		failure sshFailure
		action  sshAction
		wantErr bool
	}{
		{"none, session", sshFailureNone, sshActionSession, false},
		{"none, command", sshFailureNone, sshActionCommand, false},
		{"remote exit, session reports the error", sshFailureRemoteExit, sshActionSession, true},
		{"remote exit, command stays quiet", sshFailureRemoteExit, sshActionCommand, false},
		{"remote exit, script stays quiet", sshFailureRemoteExit, sshActionScript, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := pendingFlow(t, "", pendingSSH{action: c.action, username: "root", password: "typed-pw"})
			updated, cmd := m.finishSSHAttempt(c.failure, errors.New("exit status 3"))
			m2 := asModel(t, updated)
			if cmd == nil {
				t.Fatal("expected persistence commands")
			}
			if !pendingZero(m2.ssh.pending) || m2.ssh.retryMode || m2.input.mode != inputNone {
				t.Fatalf("pending must be cleared without prompts: %+v", m2.ssh.pending)
			}
			if (m2.notify.sshError != nil) != c.wantErr {
				t.Fatalf("sshError = %v, wantErr %v", m2.notify.sshError, c.wantErr)
			}
			if c.wantErr && !strings.Contains(m2.notify.sshError.Error(), "remote session ended with an error: exit status 3") {
				t.Fatalf("err = %v", m2.notify.sshError)
			}
			cred := m2.ssh.machineCreds["dev-1"]
			if cred.username != "root" || cred.name != "laptop.example.com" || cred.passwordEncrypted == "" {
				t.Fatalf("cred = %+v", cred)
			}
			if m2.lastMachine != "dev-1" || m2.ssh.username != "root" {
				t.Fatalf("lastMachine=%q default=%q", m2.lastMachine, m2.ssh.username)
			}
		})
	}
}

// Verifies: REQ-FLW-024
func TestDO178CFinishBadSavedPasswordIsForgotten(t *testing.T) {
	t.Run("saved password rejected: forgotten on disk too, username and name kept", func(t *testing.T) {
		m := flowModel(t, map[string]machineCredential{"dev-1": {name: "laptop.example.com", username: "root", passwordEncrypted: "will-be-replaced"}}, "")
		m.ssh.pending = pendingSSH{action: sshActionSession, machineID: "dev-1", machineName: "laptop.example.com",
			username: "root", usedSavedPass: true, password: ""}
		// the stored value must really be a password so the file write is meaningful
		cred := m.ssh.machineCreds["dev-1"]
		cred.passwordEncrypted = encFor(t, "old")
		m.ssh.machineCreds["dev-1"] = cred
		if err := storeMsgErr(runAllCmds(storeMachineCredential("dev-1", cred))); err != nil {
			t.Fatal(err)
		}

		updated, cmd := m.finishSSHAttempt(sshFailureBadPassword, errors.New("exit status 5"))
		m2 := asModel(t, updated)
		got := m2.ssh.machineCreds["dev-1"]
		if got.passwordEncrypted != "" || got.username != "root" || got.name != "laptop.example.com" {
			t.Fatalf("cred = %+v", got)
		}
		if cmd == nil {
			t.Fatal("the forgotten password must be persisted")
		}
		if err := storeMsgErr(runAllCmds(cmd)); err != nil {
			t.Fatal(err)
		}
		disk := loadCfg(t).MachineCredentials["dev-1"]
		if disk.PasswordEncrypted != "" || disk.Username != "root" {
			t.Fatalf("on disk: %+v", disk)
		}
		if m2.input.mode != inputPassword || !m2.ssh.pending.passwordTried || m2.ssh.pending.password != "" {
			t.Fatalf("mode=%v pending=%+v", m2.input.mode, m2.ssh.pending)
		}
		if m2.notify.sshError == nil || !strings.Contains(m2.notify.sshError.Error(), "saved password for laptop.example.com was rejected") {
			t.Fatalf("err = %v", m2.notify.sshError)
		}
		if m2.ssh.pending.action != sshActionSession {
			t.Fatal("the attempt must stay pending so it can be retried")
		}
	})
	t.Run("used saved password but no stored credential: message only", func(t *testing.T) {
		m := pendingFlow(t, "", pendingSSH{action: sshActionSession, username: "root", usedSavedPass: true})
		updated, cmd := m.finishSSHAttempt(sshFailureBadPassword, errors.New("x"))
		m2 := asModel(t, updated)
		if cmd != nil || len(m2.ssh.machineCreds) != 0 || m2.input.mode != inputPassword {
			t.Fatalf("cmd=%v creds=%v mode=%v", cmd != nil, m2.ssh.machineCreds, m2.input.mode)
		}
		if !strings.Contains(m2.notify.sshError.Error(), "saved password") {
			t.Fatalf("err = %v", m2.notify.sshError)
		}
	})
}

func storeMsgErr(msgs []tea.Msg) error {
	for _, m := range msgs {
		switch v := m.(type) {
		case machineCredsSavedMsg:
			if v.err != nil {
				return v.err
			}
		case usernameStoredMsg:
			if v.err != nil {
				return v.err
			}
		}
	}
	return nil
}

// Verifies: REQ-FLW-025
func TestDO178CFinishBadTypedPasswordIsNotSaved(t *testing.T) {
	m := flowModel(t, map[string]machineCredential{"dev-1": {name: "n", username: "root", passwordEncrypted: encFor(t, "keep-me")}}, "")
	m.ssh.pending = pendingSSH{action: sshActionSession, machineID: "dev-1", machineName: "laptop", username: "root", password: "wrong"}
	updated, cmd := m.finishSSHAttempt(sshFailureBadPassword, errors.New("x"))
	m2 := asModel(t, updated)
	if cmd != nil {
		t.Fatal("nothing may be persisted for a rejected typed password")
	}
	if m2.ssh.machineCreds["dev-1"].passwordEncrypted == "" {
		t.Fatal("an unrelated saved password must not be touched when a typed one fails")
	}
	if m2.ssh.pending.password != "" || !m2.ssh.pending.passwordTried || m2.input.mode != inputPassword {
		t.Fatalf("pending = %+v mode=%v", m2.ssh.pending, m2.input.mode)
	}
	if m2.notify.sshError == nil || m2.notify.sshError.Error() != "password rejected for root@laptop; try again" {
		t.Fatalf("err = %v", m2.notify.sshError)
	}
}

// Verifies: REQ-FLW-026, REQ-FLW-027
func TestDO178CFinishConnectionFailure(t *testing.T) {
	err255 := errors.New("exit status 255")
	base := pendingSSH{action: sshActionSession, username: "admin"}
	cases := []struct {
		name       string
		sshpass    bool
		mutate     func(*pendingSSH)
		wantPrompt bool
	}{
		{"first key failure with sshpass: ask for a password", true, func(*pendingSSH) {}, true},
		{"password already tried: retry menu", true, func(p *pendingSSH) { p.passwordTried = true }, false},
		{"a typed password is present: retry menu", true, func(p *pendingSSH) { p.password = "typed" }, false},
		{"a saved password was used: retry menu", true, func(p *pendingSSH) { p.usedSavedPass = true }, false},
		{"no sshpass: retry menu", false, func(*pendingSSH) {}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			useFakeBins(t, fakeBins{sshpass: c.sshpass})
			p := base
			c.mutate(&p)
			m := pendingFlow(t, "admin", p)
			m.ssh.retryCursor = 3
			updated, cmd := m.finishSSHAttempt(sshFailureConnection, err255)
			m2 := asModel(t, updated)
			if cmd != nil {
				t.Fatal("no persistence on a connection failure")
			}
			if _, ok := m2.ssh.machineCreds["dev-1"]; ok {
				t.Fatal("nothing may be saved on a connection failure")
			}
			if m2.ssh.pending.action != sshActionSession {
				t.Fatal("attempt must stay pending")
			}
			if c.wantPrompt {
				if m2.input.mode != inputPassword || m2.ssh.retryMode {
					t.Fatalf("mode=%v retry=%v", m2.input.mode, m2.ssh.retryMode)
				}
				want := "could not log in as admin without a password: exit status 255"
				if m2.notify.sshError == nil || m2.notify.sshError.Error() != want {
					t.Fatalf("err = %v", m2.notify.sshError)
				}
			} else {
				if !m2.ssh.retryMode || m2.ssh.retryCursor != 0 || m2.input.mode != inputNone {
					t.Fatalf("retry=%v cursor=%d mode=%v", m2.ssh.retryMode, m2.ssh.retryCursor, m2.input.mode)
				}
				if m2.notify.sshError != err255 {
					t.Fatalf("err = %v", m2.notify.sshError)
				}
			}
		})
	}
}

// Verifies: REQ-FLW-028
func TestDO178CFinishExecFailure(t *testing.T) {
	for _, f := range []sshFailure{sshFailureExec, sshFailure(99)} {
		m := pendingFlow(t, "admin", pendingSSH{action: sshActionSession, username: "admin", password: "typed"})
		updated, cmd := m.finishSSHAttempt(f, errors.New("no such file"))
		m2 := asModel(t, updated)
		if cmd != nil || !pendingZero(m2.ssh.pending) || m2.input.mode != inputNone || m2.ssh.retryMode {
			t.Fatalf("failure %v: pending=%+v", f, m2.ssh.pending)
		}
		if m2.notify.sshError == nil || m2.notify.sshError.Error() != "could not start ssh: no such file" {
			t.Fatalf("err = %v", m2.notify.sshError)
		}
		if len(m2.ssh.machineCreds) != 0 {
			t.Fatal("nothing may be saved")
		}
	}
}

// ─── persistSuccessfulCredentials ─────────────────────────────────────────────

// Verifies: REQ-FLW-029, REQ-FLW-030, REQ-FLW-031
func TestDO178CPersistSuccessfulCredentials(t *testing.T) {
	t.Run("new machine: encrypted password, username, default user, last machine all reach the disk", func(t *testing.T) {
		m := pendingFlow(t, "", pendingSSH{action: sshActionSession, username: "pi", password: "pa$$ 'w\"ord"})
		m.ssh.machineCreds = nil // nil map must be tolerated
		cmds := m.persistSuccessfulCredentials()
		if len(cmds) != 3 {
			t.Fatalf("expected credential, default-username and last-machine commands, got %d", len(cmds))
		}
		for _, c := range cmds {
			if err := storeMsgErr(runAllCmds(c)); err != nil {
				t.Fatal(err)
			}
		}
		cfg := loadCfg(t)
		auth := cfg.MachineCredentials["dev-1"]
		if auth.Username != "pi" || auth.Name != "laptop.example.com" || auth.PasswordEncrypted == "" {
			t.Fatalf("auth = %+v", auth)
		}
		if cfg.SSHUsername != "pi" || cfg.LastMachine != "dev-1" || m.ssh.username != "pi" || m.lastMachine != "dev-1" {
			t.Fatalf("cfg=%+v model default=%q last=%q", cfg, m.ssh.username, m.lastMachine)
		}
		if plain, err := util.DecryptPassword(auth.PasswordEncrypted); err != nil || plain != "pa$$ 'w\"ord" {
			t.Fatalf("round trip: %q %v", plain, err)
		}
		path, _ := config.Path()
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "pa$$") || strings.Contains(string(raw), "w\\\"ord") {
			t.Fatal("plaintext password found in config.json")
		}
		if m.ssh.machineCreds["dev-1"].passwordEncrypted != auth.PasswordEncrypted {
			t.Fatal("in-memory state must match what was written")
		}
	})
	t.Run("nothing new learned: only the last-machine hint is written", func(t *testing.T) {
		m := flowModel(t, map[string]machineCredential{"dev-1": {name: "laptop.example.com", username: "pi", passwordEncrypted: "keep"}}, "pi")
		m.ssh.pending = pendingSSH{action: sshActionSession, machineID: "dev-1", machineName: "laptop.example.com", username: "pi"}
		cmds := m.persistSuccessfulCredentials()
		if len(cmds) != 1 {
			t.Fatalf("got %d commands", len(cmds))
		}
		if m.ssh.machineCreds["dev-1"].passwordEncrypted != "keep" {
			t.Fatal("stored password must not change when none was typed")
		}
	})
	t.Run("only the password changed", func(t *testing.T) {
		m := flowModel(t, map[string]machineCredential{"dev-1": {name: "laptop.example.com", username: "pi"}}, "pi")
		m.ssh.pending = pendingSSH{action: sshActionSession, machineID: "dev-1", machineName: "laptop.example.com", username: "pi", password: "new"}
		if cmds := m.persistSuccessfulCredentials(); len(cmds) != 2 {
			t.Fatalf("got %d commands", len(cmds))
		}
		if m.ssh.machineCreds["dev-1"].passwordEncrypted == "" {
			t.Fatal("password not stored")
		}
	})
	t.Run("only the name changed", func(t *testing.T) {
		m := flowModel(t, map[string]machineCredential{"dev-1": {name: "old name", username: "pi"}}, "pi")
		m.ssh.pending = pendingSSH{action: sshActionSession, machineID: "dev-1", machineName: "laptop.example.com", username: "pi"}
		if cmds := m.persistSuccessfulCredentials(); len(cmds) != 2 || m.ssh.machineCreds["dev-1"].name != "laptop.example.com" {
			t.Fatalf("got %d commands, cred %+v", len(cmds), m.ssh.machineCreds["dev-1"])
		}
	})
	t.Run("only the username changed", func(t *testing.T) {
		m := flowModel(t, map[string]machineCredential{"dev-1": {name: "laptop.example.com", username: "old"}}, "old")
		m.ssh.pending = pendingSSH{action: sshActionSession, machineID: "dev-1", machineName: "laptop.example.com", username: "pi"}
		if cmds := m.persistSuccessfulCredentials(); len(cmds) != 2 || m.ssh.machineCreds["dev-1"].username != "pi" {
			t.Fatalf("got %d commands, cred %+v", len(cmds), m.ssh.machineCreds["dev-1"])
		}
		if m.ssh.username != "old" {
			t.Fatal("an existing default username must not be overwritten")
		}
	})
	t.Run("default username is only set when empty and the attempt had one", func(t *testing.T) {
		m := flowModel(t, map[string]machineCredential{"dev-1": {name: "laptop.example.com"}}, "")
		m.ssh.pending = pendingSSH{action: sshActionSession, machineID: "dev-1", machineName: "laptop.example.com"} // empty username
		cmds := m.persistSuccessfulCredentials()
		if len(cmds) != 1 || m.ssh.username != "" {
			t.Fatalf("empty attempt username must not become the default: cmds=%d default=%q", len(cmds), m.ssh.username)
		}
	})
	t.Run("encryption failure: warn, never store plaintext, keep the rest", func(t *testing.T) {
		old := encryptPassword
		encryptPassword = func(string) (string, error) { return "", errors.New("keystore locked") }
		t.Cleanup(func() { encryptPassword = old })
		m := pendingFlow(t, "pi", pendingSSH{action: sshActionSession, username: "pi", password: "typed"})
		cmds := m.persistSuccessfulCredentials()
		cred := m.ssh.machineCreds["dev-1"]
		if cred.passwordEncrypted != "" || strings.Contains(cred.passwordEncrypted, "typed") {
			t.Fatalf("cred = %+v", cred)
		}
		if cred.username != "pi" || len(cmds) != 2 {
			t.Fatalf("username/last machine must still be saved: %+v, %d cmds", cred, len(cmds))
		}
		if m.notify.sshError == nil || !strings.Contains(m.notify.sshError.Error(), "could not encrypt password") ||
			!strings.Contains(m.notify.sshError.Error(), "keystore locked") {
			t.Fatalf("err = %v", m.notify.sshError)
		}
	})
}

// ─── handleProbe ──────────────────────────────────────────────────────────────

// Verifies: REQ-FLW-032
func TestDO178CHandleProbeIgnoredWhenNotSession(t *testing.T) {
	for _, a := range []sshAction{sshActionNone, sshActionCommand, sshActionScript} {
		m := pendingFlow(t, "pi", pendingSSH{action: a, username: "pi", password: "typed"})
		if a == sshActionNone {
			m.ssh.pending = pendingSSH{}
		}
		updated, cmd := m.handleProbe(probeMsg{failure: sshFailureNone})
		m2 := asModel(t, updated)
		if cmd != nil || len(m2.ssh.machineCreds) != 0 || m2.lastMachine != "" || m2.ssh.pending.action != a {
			t.Fatalf("action %v: late probe result must be ignored", a)
		}
	}
}

// Verifies: REQ-FLW-033
func TestDO178CHandleProbeOutcomes(t *testing.T) {
	useFakeBins(t, fakeBins{ssh: true, sshpass: true})
	cap := captureTeaExec(t)
	pending := pendingSSH{action: sshActionSession, username: "pi", password: "typed-pw"}

	for _, f := range []sshFailure{sshFailureNone, sshFailureRemoteExit} {
		t.Run("success "+itoaN(int(f)), func(t *testing.T) {
			*cap = teaExecCapture{}
			m := pendingFlow(t, "pi", pending)
			updated, cmd := m.handleProbe(probeMsg{failure: f})
			m2 := asModel(t, updated)
			if cmd == nil || !cap.called || !m2.ssh.pending.verified {
				t.Fatalf("session must start: cmd=%v called=%v verified=%v", cmd != nil, cap.called, m2.ssh.pending.verified)
			}
			cred := m2.ssh.machineCreds["dev-1"]
			if cred.username != "pi" || cred.passwordEncrypted == "" {
				t.Fatalf("credentials must be saved before the session starts: %+v", cred)
			}
			if args := cap.cmd.(*bannerExec).cmd.Args; strings.Contains(strings.Join(args, "\x00"), "typed-pw") {
				t.Fatalf("password in argv: %q", args)
			}
		})
	}
	for _, f := range []sshFailure{sshFailureBadPassword, sshFailureConnection, sshFailureExec} {
		t.Run("failure "+itoaN(int(f)), func(t *testing.T) {
			*cap = teaExecCapture{}
			m := pendingFlow(t, "pi", pending)
			updated, _ := m.handleProbe(probeMsg{failure: f, err: errors.New("probe failed")})
			m2 := asModel(t, updated)
			if cap.called || len(m2.ssh.machineCreds) != 0 || m2.lastMachine != "" {
				t.Fatalf("a failed probe must start nothing and save nothing: creds=%v", m2.ssh.machineCreds)
			}
			if m2.ssh.pending.verified {
				t.Fatal("must not be marked verified")
			}
			switch f {
			case sshFailureBadPassword:
				if m2.input.mode != inputPassword || m2.ssh.pending.password != "" {
					t.Fatalf("mode=%v pw=%q", m2.input.mode, m2.ssh.pending.password)
				}
			case sshFailureConnection:
				if !m2.ssh.retryMode { // password was already typed, so no new prompt
					t.Fatal("expected retry menu")
				}
			default:
				if m2.ssh.pending.action != sshActionNone || m2.notify.sshError == nil {
					t.Fatal("exec failure must clear the attempt and report")
				}
			}
		})
	}
	t.Run("device list changed while probing: cancel, save nothing", func(t *testing.T) {
		*cap = teaExecCapture{}
		p := pending
		p.deviceIndex = 5
		m := pendingFlow(t, "pi", p)
		updated, cmd := m.handleProbe(probeMsg{failure: sshFailureNone})
		m2 := asModel(t, updated)
		if cmd != nil || cap.called || len(m2.ssh.machineCreds) != 0 || !pendingZero(m2.ssh.pending) {
			t.Fatalf("pending=%+v creds=%v", m2.ssh.pending, m2.ssh.machineCreds)
		}
		p.deviceIndex = -1
		m = pendingFlow(t, "pi", p)
		if updated, _ := m.handleProbe(probeMsg{failure: sshFailureNone}); !pendingZero(asModel(t, updated).ssh.pending) {
			t.Fatal("negative index must also cancel")
		}
	})
}

// ─── cancelSSHAttempt ─────────────────────────────────────────────────────────

// Verifies: REQ-FLW-034
func TestDO178CCancelClearsEverySecret(t *testing.T) {
	m := pendingFlow(t, "admin", pendingSSH{action: sshActionSession, username: "u", password: "typed-secret",
		passwordTried: true, usedSavedPass: true, verified: true, askPasswordNext: true})
	m.input = textInput{mode: inputPassword, value: "half-typed-secret", cursor: 5, masked: true}
	m.ssh.retryMode = true
	m.ssh.retryCursor = 2
	m.notify.sshError = errors.New("boom")

	m2 := m.cancelSSHAttempt()
	if !pendingZero(m2.ssh.pending) {
		t.Fatalf("pending not cleared: %+v", m2.ssh.pending)
	}
	if m2.ssh.retryMode || m2.notify.sshError != nil {
		t.Fatal("retry menu and error must be cleared")
	}
	if m2.input.mode != inputNone || m2.input.value != "" || m2.input.cursor != 0 || m2.input.masked {
		t.Fatalf("input not reset: %+v", m2.input)
	}
	// the receiver copy is not mutated through shared state
	if m.ssh.pending.password != "typed-secret" {
		t.Fatal("cancel must work on a copy")
	}
	// already idle: still fine
	_ = m2.cancelSSHAttempt()
}

// ─── handleRunDone ────────────────────────────────────────────────────────────

func runDoneModel(t *testing.T, multi bool) (model, *runJob, *util.HistoryStore) {
	t.Helper()
	m := flowModel(t, nil, "")
	hist, err := util.NewHistoryStore()
	if err != nil {
		t.Fatal(err)
	}
	job := &runJob{}
	dev := m.list.filteredDevices[0]
	m.run = runPanel{history: hist, label: "uptime", startedAt: time.Now(), multi: multi,
		spec: sshJob{action: sshActionCommand, command: "uptime"},
		runs: []*machineRun{{device: dev, username: "root", job: job, status: runRunning, lines: []string{"ok"}, startedAt: time.Now()}}}
	m.ssh.pending = pendingSSH{action: sshActionCommand, machineID: "dev-1", machineName: "laptop.example.com",
		username: "root", command: "uptime", password: "typed"}
	return m, job, hist
}

// Verifies: REQ-FLW-035
func TestDO178CHandleRunDoneStaleJob(t *testing.T) {
	m, _, hist := runDoneModel(t, false)
	for _, job := range []*runJob{nil, {}} {
		updated, cmd := m.handleRunDone(runDoneMsg{job: job, res: runResult{exitCode: 9}})
		m2 := asModel(t, updated)
		if cmd != nil || m2.run.runs[0].status != runRunning || m2.ssh.pending.action != sshActionCommand || len(m2.ssh.machineCreds) != 0 {
			t.Fatal("a stale completion must change nothing")
		}
	}
	if len(hist.Recent("dev-1", 5)) != 0 {
		t.Fatal("stale completion recorded history")
	}
}

// Verifies: REQ-FLW-036, REQ-FLW-037
func TestDO178CHandleRunDoneSingleMachine(t *testing.T) {
	useFakeBins(t, fakeBins{sshpass: true})
	cases := []struct {
		name      string
		res       runResult
		connected bool
	}{
		{"clean exit", runResult{failure: sshFailureNone}, true},
		{"remote exit non-zero", runResult{failure: sshFailureRemoteExit, exitCode: 2, err: errors.New("exit status 2")}, true},
		{"killed by the user, even though flagged as connection failure", runResult{failure: sshFailureConnection, killed: true, exitCode: -1}, true},
		{"connection failure", runResult{failure: sshFailureConnection, exitCode: 255, err: errors.New("exit status 255")}, false},
		{"bad password", runResult{failure: sshFailureBadPassword, exitCode: 5, err: errors.New("exit status 5")}, false},
		{"could not start", runResult{failure: sshFailureExec, err: errors.New("exec failed")}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, job, hist := runDoneModel(t, false)
			m.notify.sshError = errors.New("previous")
			updated, _ := m.handleRunDone(runDoneMsg{job: job, res: c.res})
			m2 := asModel(t, updated)
			mr := m2.run.runs[0]
			if mr.status != runDone || mr.job != nil || mr.exitCode != c.res.exitCode || mr.killed != c.res.killed {
				t.Fatalf("machine run not settled: %+v", mr)
			}
			recent := hist.Recent("dev-1", 5)
			if c.connected {
				if len(recent) != 1 || recent[0].Command != "uptime" {
					t.Fatalf("history = %+v", recent)
				}
				if m2.notify.sshError != nil || m2.ssh.pending.action != sshActionNone {
					t.Fatalf("err=%v pending=%+v", m2.notify.sshError, m2.ssh.pending)
				}
				if cred := m2.ssh.machineCreds["dev-1"]; cred.username != "root" || cred.passwordEncrypted == "" {
					t.Fatalf("credentials must be saved after a connected run: %+v", cred)
				}
			} else {
				if len(recent) != 0 {
					t.Fatalf("history recorded for a failed connection: %+v", recent)
				}
				if _, ok := m2.ssh.machineCreds["dev-1"]; ok {
					t.Fatal("credentials saved after a failed connection")
				}
				if m2.ssh.pending.action != sshActionCommand && c.res.failure != sshFailureExec {
					t.Fatal("the attempt must stay pending for the retry")
				}
				if m2.notify.sshError == nil {
					t.Fatal("failure must be reported")
				}
			}
		})
	}
}

// Verifies: REQ-FLW-038
func TestDO178CHandleRunDoneMultiBypassesCredentialFlow(t *testing.T) {
	m, job, _ := runDoneModel(t, true)
	updated, _ := m.handleRunDone(runDoneMsg{job: job, res: runResult{failure: sshFailureBadPassword, exitCode: 5, err: errors.New("x")}})
	m2 := asModel(t, updated)
	if m2.input.mode != inputNone || m2.ssh.pending.password != "typed" || m2.ssh.pending.action != sshActionCommand {
		t.Fatalf("multi runs must not touch the interactive flow: mode=%v pending=%+v", m2.input.mode, m2.ssh.pending)
	}
	mr := m2.run.runs[0]
	if mr.status != runFailed || !strings.Contains(mr.note, "saved password rejected") {
		t.Fatalf("machine run = %+v", mr)
	}
	if len(m2.ssh.machineCreds) != 0 {
		t.Fatal("a failed multi run must not save or forget credentials")
	}
}

// ─── migrateCredentialKeys ────────────────────────────────────────────────────

// Verifies: REQ-FLW-039, REQ-FLW-040
func TestDO178CMigrateCredentialKeys(t *testing.T) {
	type creds = map[string]machineCredential
	cases := []struct {
		name        string
		creds       creds
		devices     []client.Device
		wantChanged bool
		want        creds
	}{
		{"no devices", creds{"a": {username: "x"}}, nil, false, creds{"a": {username: "x"}}},
		{"no credentials", creds{}, []client.Device{{ID: "1", Name: "n"}}, false, creds{}},
		{"legacy name moved to id with display name",
			creds{"n": {username: "root", passwordEncrypted: "enc"}},
			[]client.Device{{ID: "1", Name: "n", Hostname: "h"}}, true,
			creds{"1": {name: "n", username: "root", passwordEncrypted: "enc"}}},
		{"legacy hostname moved to id",
			creds{"h": {username: "root"}},
			[]client.Device{{ID: "1", Name: "n", Hostname: "h"}}, true,
			creds{"1": {name: "n", username: "root"}}},
		{"existing id entry wins; legacy entry dropped",
			creds{"n": {username: "legacy", passwordEncrypted: "legacy-enc"}, "1": {name: "n", username: "current"}},
			[]client.Device{{ID: "1", Name: "n"}}, true,
			creds{"1": {name: "n", username: "current"}}},
		{"both legacy names present: first moved, second dropped",
			creds{"n": {username: "byname"}, "h": {username: "byhost"}},
			[]client.Device{{ID: "1", Name: "n", Hostname: "h"}}, true,
			creds{"1": {name: "n", username: "byname"}}},
		{"no id: key is hostname, legacy name moves onto it",
			creds{"n": {username: "root"}},
			[]client.Device{{Name: "n", Hostname: "h"}}, true,
			creds{"h": {name: "n", username: "root"}}},
		{"legacy equal to key is not touched",
			creds{"h": {name: "n", username: "root"}},
			[]client.Device{{Hostname: "h", Name: "h"}}, false,
			creds{"h": {name: "n", username: "root"}}},
		{"device without any key is skipped",
			creds{"n": {username: "root"}},
			[]client.Device{{Name: "n"}}, false,
			creds{"n": {username: "root"}}},
		{"empty legacy names are skipped",
			creds{"": {username: "ghost"}},
			[]client.Device{{ID: "1"}}, false,
			creds{"": {username: "ghost"}}},
		{"missing display name is filled in",
			creds{"1": {username: "root"}},
			[]client.Device{{ID: "1", Name: "n"}}, true,
			creds{"1": {name: "n", username: "root"}}},
		{"existing display name is kept",
			creds{"1": {name: "custom", username: "root"}},
			[]client.Device{{ID: "1", Name: "n"}}, false,
			creds{"1": {name: "custom", username: "root"}}},
		{"unrelated credentials are kept",
			creds{"other": {username: "x"}},
			[]client.Device{{ID: "1", Name: "n"}}, false,
			creds{"other": {username: "x"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := migrateCredentialKeys(c.creds, c.devices); got != c.wantChanged {
				t.Fatalf("changed = %v, want %v", got, c.wantChanged)
			}
			if len(c.creds) != len(c.want) {
				t.Fatalf("creds = %+v, want %+v", c.creds, c.want)
			}
			for k, w := range c.want {
				if c.creds[k] != w {
					t.Fatalf("creds[%q] = %+v, want %+v", k, c.creds[k], w)
				}
			}
			// idempotent
			if migrateCredentialKeys(c.creds, c.devices) {
				t.Fatal("second run must be a no-op")
			}
		})
	}
}

func pendingZero(p pendingSSH) bool { return reflect.DeepEqual(p, pendingSSH{}) }
