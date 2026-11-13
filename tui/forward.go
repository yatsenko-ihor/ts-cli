package tui

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/yatsenko-ihor/ts-cli/client"
)

// Port forwarding: `ssh -N -L 127.0.0.1:<local>:<host>:<remote>` as a job of
// the Run panel. It stays running until stopped (x), the same as any other
// job, so it shows up in the Output panel and uses the same credential flow.
// The local end is always the loopback address, never a public one.

// forwardSpec is one forwarded port.
type forwardSpec struct {
	localPort  int
	host       string // as seen from the machine; "localhost" by default
	remotePort int
}

var forwardHostRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9._-]*[A-Za-z0-9])?$`)

func parsePort(s string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 1 || n > 65535 {
		return 0, fmt.Errorf("%q is not a port number (1-65535)", strings.TrimSpace(s))
	}
	return n, nil
}

// parseForward reads "8080", "8080:80" or "8080:host:80".
func parseForward(s string) (forwardSpec, error) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	var spec forwardSpec
	var err error
	if spec.localPort, err = parsePort(parts[0]); err != nil {
		return spec, err
	}
	spec.host, spec.remotePort = "localhost", spec.localPort
	switch len(parts) {
	case 1:
	case 2:
		spec.remotePort, err = parsePort(parts[1])
	case 3:
		spec.host = parts[1]
		if !forwardHostRe.MatchString(spec.host) {
			return spec, fmt.Errorf("%q is not a host name or address", spec.host)
		}
		spec.remotePort, err = parsePort(parts[2])
	default:
		return spec, fmt.Errorf("use PORT, LOCAL:REMOTE or LOCAL:HOST:REMOTE")
	}
	return spec, err
}

// arg is the value of ssh's -L option: loopback only.
func (f forwardSpec) arg() string {
	return fmt.Sprintf("127.0.0.1:%d:%s:%d", f.localPort, f.host, f.remotePort)
}

func (f forwardSpec) label() string {
	return fmt.Sprintf("forward 127.0.0.1:%d → %s:%d", f.localPort, f.host, f.remotePort)
}

// startLine tells the user the forward is up: ssh -N prints nothing.
func (f forwardSpec) startLine(d client.Device) string {
	return fmt.Sprintf("Forwarding 127.0.0.1:%d → %s:%d on %s (x stops it)", f.localPort, f.host, f.remotePort, displayName(d))
}

func forwardResultLine(f forwardSpec, res runResult, elapsed time.Duration, reason string) string {
	took := elapsed.Round(time.Second)
	switch {
	case res.killed:
		return fmt.Sprintf("■ Forward stopped after %s: %s", took, f.label())
	case reason != "":
		return "✗ " + reason
	case res.exitCode == 0:
		return fmt.Sprintf("Forward ended after %s: %s", took, f.label())
	}
	return fmt.Sprintf("✗ Forward failed (exit %d) after %s: %s", res.exitCode, took, f.label())
}

// forwardFailure finds why ssh could not set the forward up: the local port
// is taken, or the server refuses forwarding. These are not login problems.
func forwardFailure(lines []string) string {
	for _, l := range lines {
		low := strings.ToLower(l)
		switch {
		case strings.Contains(low, "address already in use"), strings.Contains(low, "cannot listen"):
			return "the local port is already in use"
		case strings.Contains(low, "forwarding failed"), strings.Contains(low, "administratively prohibited"),
			strings.Contains(low, "could not request local forwarding"):
			return "the machine refused the forward (AllowTcpForwarding?)"
		}
	}
	return ""
}

// adjustForwardResult keeps a failed forward away from the credential flow:
// ssh exits 255 both for a refused login and for a port it could not bind.
func (m model) adjustForwardResult(msg runDoneMsg) runDoneMsg {
	if m.run.spec.action != sshActionForward || msg.res.failure != sshFailureConnection {
		return msg
	}
	if mr := m.run.runFor(msg.job); mr != nil && forwardFailure(mr.lines) != "" {
		msg.res.failure = sshFailureRemoteExit
	}
	return msg
}

// promptForward asks for the ports and starts the forward.
func (m model) promptForward(d client.Device) (tea.Model, tea.Cmd) {
	if m.isMulti() {
		m.act = actionState{err: fmt.Errorf("forwarding works on one machine; clear the marks (esc) first")}
		return m, nil
	}
	m.input = textInput{
		mode: inputText, label: "Forward a local port to " + shortHost(displayName(d)),
		hint: "PORT, LOCAL:REMOTE or LOCAL:HOST:REMOTE, e.g. 8080:80 or 5432:db.internal:5432",
		onSubmit: func(m model, v string) (tea.Model, tea.Cmd) {
			m.input = textInput{}
			spec, err := parseForward(v)
			if err != nil {
				m.act = actionState{err: err}
				return m, nil
			}
			return m.launch(sshJob{action: sshActionForward, forward: spec})
		},
	}
	return m, nil
}
