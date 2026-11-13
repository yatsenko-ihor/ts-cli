package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yatsenko-ihor/ts-cli/client"
)

func TestParseForward(t *testing.T) {
	ok := map[string]forwardSpec{
		"8080":                    {8080, "localhost", 8080},
		"8080:80":                 {8080, "localhost", 80},
		" 5432:db.internal:5432 ": {5432, "db.internal", 5432},
		"1:a:65535":               {1, "a", 65535},
	}
	for in, want := range ok {
		got, err := parseForward(in)
		if err != nil || got != want {
			t.Errorf("parseForward(%q) = %+v, %v; want %+v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "x", "0", "65536", "-1", "80:x", "80:-evil:80", "80:a b:80", "80:host:", "1:2:3:4", "80:host:99999", "80:;rm:80"} {
		if _, err := parseForward(bad); err == nil {
			t.Errorf("parseForward(%q) must fail", bad)
		}
	}
	f := forwardSpec{8080, "localhost", 80}
	if f.arg() != "127.0.0.1:8080:localhost:80" || !strings.Contains(f.label(), "127.0.0.1:8080 → localhost:80") {
		t.Fatalf("%q %q", f.arg(), f.label())
	}
}

func TestForwardFailureReasons(t *testing.T) {
	cases := map[string]string{
		"bind [127.0.0.1]:8080: Address already in use":       "already in use",
		"channel_setup_fwd_listener_tcpip: cannot listen to":  "already in use",
		"channel 2: open failed: administratively prohibited": "refused",
		"Could not request local forwarding.":                 "refused",
		"Warning: forwarding failed":                          "refused",
		"Permission denied (publickey).":                      "",
	}
	for line, want := range cases {
		got := forwardFailure([]string{"x", line})
		if (want == "" && got != "") || !strings.Contains(got, want) {
			t.Errorf("%q -> %q, want %q", line, got, want)
		}
	}
	f := forwardSpec{8080, "localhost", 80}
	if !strings.Contains(forwardResultLine(f, runResult{killed: true}, time.Second, ""), "stopped") ||
		forwardResultLine(f, runResult{exitCode: 255}, time.Second, "the local port is already in use") != "✗ the local port is already in use" ||
		!strings.Contains(forwardResultLine(f, runResult{}, time.Second, ""), "ended") ||
		!strings.Contains(forwardResultLine(f, runResult{exitCode: 2}, time.Second, ""), "exit 2") {
		t.Fatal("result lines")
	}
}

func TestForwardRunsAsAJobAndStopsLikeOne(t *testing.T) {
	dir := useFakeBins(t, fakeBins{ssh: true})
	t.Setenv("FAKE_SSH_EXIT", "0")
	m := flowModel(t, map[string]machineCredential{"dev-1": {username: "pi"}}, "pi")
	spec := forwardSpec{8080, "localhost", 80}

	updated, cmd := m.beginSSH(0, sshJob{action: sshActionForward, forward: spec})
	m = asModel(t, updated)
	if cmd == nil || len(m.run.runs) != 1 || !strings.HasPrefix(m.run.runs[0].lines[0], "Forwarding 127.0.0.1:8080 → localhost:80") {
		t.Fatalf("runs = %+v", m.run.runs)
	}
	if !strings.Contains(m.run.label, "forward 127.0.0.1:8080") || m.ssh.pending.forward != spec {
		t.Fatalf("label %q pending %+v", m.run.label, m.ssh.pending.forward)
	}
	job := m.run.runs[0].job
	var done runDoneMsg
	for {
		switch msg := waitRunEvent(job)().(type) {
		case runDoneMsg:
			done = msg
		default:
			continue
		}
		break
	}
	argv := strings.Join(fakeArgv(t, dir, "ssh.argv"), " ")
	if !strings.Contains(argv, "-N") || !strings.Contains(argv, "-L 127.0.0.1:8080:localhost:80") ||
		!strings.Contains(argv, "ExitOnForwardFailure=yes") || !strings.HasSuffix(argv, "pi@100.64.0.1") {
		t.Fatalf("argv = %q", argv)
	}
	updated, _ = m.Update(done)
	m = asModel(t, updated)
	last := m.run.runs[0].lines[len(m.run.runs[0].lines)-1]
	if !strings.Contains(last, "Forward ended") {
		t.Fatalf("last line %q", last)
	}
}

func TestBusyPortDoesNotOpenTheCredentialMenu(t *testing.T) {
	m := flowModel(t, map[string]machineCredential{"dev-1": {username: "pi"}}, "pi")
	m.run.spec = sshJob{action: sshActionForward, forward: forwardSpec{8080, "localhost", 80}}
	job := &runJob{}
	m.run.runs = []*machineRun{{device: m.list.devices[0], job: job, status: runRunning, lines: []string{"bind [127.0.0.1]:8080: Address already in use"}}}
	m.ssh.pending = pendingSSH{action: sshActionForward, deviceIndex: 0, machineID: "dev-1", machineName: "laptop.example.com", username: "pi"}

	res := runResult{err: errors.New("exit status 255"), failure: sshFailureConnection, exitCode: 255}
	adjusted := m.adjustForwardResult(runDoneMsg{job: job, res: res})
	if adjusted.res.failure != sshFailureRemoteExit {
		t.Fatal("a busy port is not a login failure")
	}
	updated, _ := m.Update(runDoneMsg{job: job, res: res})
	m = asModel(t, updated)
	if m.ssh.retryMode || m.input.mode != inputNone {
		t.Fatal("no credential menu or prompt")
	}
	if last := m.run.runs[0].lines; !strings.Contains(last[len(last)-1], "already in use") {
		t.Fatalf("lines = %v", last)
	}

	// Other actions and other failures are left alone.
	m2 := m
	m2.run.spec.action = sshActionCommand
	if m2.adjustForwardResult(runDoneMsg{job: job, res: res}).res.failure != sshFailureConnection {
		t.Fatal("only forwards are adjusted")
	}
	m3 := flowModel(t, nil, "pi")
	m3.run.spec.action = sshActionForward
	m3.run.runs = []*machineRun{{job: job, lines: []string{"Permission denied"}}}
	if m3.adjustForwardResult(runDoneMsg{job: job, res: res}).res.failure != sshFailureConnection {
		t.Fatal("a refused login stays a connection failure")
	}
	if m3.adjustForwardResult(runDoneMsg{job: &runJob{}, res: res}).res.failure != sshFailureConnection {
		t.Fatal("an unknown job is left alone")
	}
}

func TestPromptForward(t *testing.T) {
	m := flowModel(t, nil, "pi")
	d := m.list.devices[0]
	updated, _ := m.promptForward(d)
	m = asModel(t, updated)
	if m.input.mode != inputText || !strings.Contains(m.input.label, "Forward a local port") {
		t.Fatal("prompt")
	}
	// an invalid value shows the error and starts nothing
	u, cmd := m.input.onSubmit(m, "abc")
	if cmd != nil || u.(model).act.err == nil || u.(model).input.mode != inputNone {
		t.Fatal("invalid")
	}
	// several marked machines: refused
	m2 := m
	m2.input = textInput{}
	second := d
	second.ID, second.Hostname, second.Name = "dev-2", "two", "two.example.com"
	m2.list.filteredDevices = append(append([]client.Device{}, m2.list.filteredDevices...), second)
	m2.list.marked = map[string]bool{"dev-1": true, "dev-2": true}
	u, _ = m2.promptForward(d)
	if u.(model).act.err == nil || u.(model).input.mode != inputNone {
		t.Fatal("multi")
	}
	// the Machine tab offers the row
	found := false
	for _, a := range machineActions(d) {
		found = found || strings.HasPrefix(a.title, "Forward a port")
	}
	if !found {
		t.Fatal("row missing")
	}
}
