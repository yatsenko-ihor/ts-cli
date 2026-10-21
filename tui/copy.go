package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// File copy to and from machines with scp: `f` uploads a file or directory
// to the selected (marked) machines, `F` downloads from the highlighted one.
// It is an ordinary job, so everything that applies to runs applies here: the
// same credential flow for one machine, saved credentials only for several,
// parallel runs, per-machine output and exit status, stop with x.

// copySpec describes one copy.
type copySpec struct {
	local    string // path on this computer (a directory for a download)
	remote   string // path on the machine (relative = home directory)
	download bool
}

// label is the short description used in the Output title.
func (c copySpec) label() string {
	if c.download {
		return "download " + c.remote + " → " + c.local
	}
	return "upload " + filepath.Base(c.local) + " → " + c.remote
}

// bracketIPv6 turns "user@fd7a::1" into "user@[fd7a::1]" as scp requires;
// IPv4 targets and already bracketed ones stay as they are.
func bracketIPv6(target string) string {
	at := strings.LastIndex(target, "@")
	host := target[at+1:]
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		return target[:at+1] + "[" + host + "]"
	}
	return target
}

// newSCPCommand builds the scp process for target (user@address as returned
// by buildSSHTarget). The local path is made absolute so it can never be read
// as an option or as host:path, and "--" ends the option list as well.
func newSCPCommand(target, password string, c copySpec) (*exec.Cmd, func(), error) {
	if c.local == "" || c.remote == "" {
		return nil, nil, fmt.Errorf("both a local and a remote path are required")
	}
	if strings.ContainsAny(c.remote, "\x00\n\r") {
		return nil, nil, fmt.Errorf("the remote path contains control characters")
	}
	local, err := filepath.Abs(c.local)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot resolve %q: %w", c.local, err)
	}
	remote := bracketIPv6(target) + ":" + c.remote
	tail := []string{"--", local, remote}
	if c.download {
		tail = []string{"--", remote, local}
	}
	return newSecureCommand("scp", []string{"-r"}, tail, password, true)
}

// expandHome replaces a leading ~ with the user's home directory.
func expandHome(p string) string {
	p = strings.TrimSpace(p)
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// promptUpload asks what to upload and where, then starts the copy.
func (m model) promptUpload() (tea.Model, tea.Cmd) {
	targets := m.targets()
	if len(targets) == 0 {
		return m, nil
	}
	where := shortHost(displayName(targets[0]))
	if len(targets) > 1 {
		where = fmt.Sprintf("%d machines", len(targets))
	}
	m.input = textInput{
		mode: inputText, label: "Upload to " + where + ": local path", path: pathAny,
		hint: "file or directory on this computer",
		onSubmit: func(m model, v string) (tea.Model, tea.Cmd) {
			m.input = textInput{}
			local := expandHome(v)
			if local == "" {
				return m, nil
			}
			if _, err := os.Stat(local); err != nil {
				m.notify.sshError = fmt.Errorf("cannot upload: %w", err)
				return m, nil
			}
			m.input = textInput{
				mode: inputText, label: "Upload to " + where + ": remote path", value: ".", cursor: 1,
				path: pathRemoteDirs, device: targets[0],
				hint: "a directory or file name; relative paths start in the home directory",
				onSubmit: func(m model, v string) (tea.Model, tea.Cmd) {
					m.input = textInput{}
					remote := strings.TrimSpace(v)
					if remote == "" {
						remote = "."
					}
					return m.launch(sshJob{action: sshActionCopy, copy: copySpec{local: local, remote: remote}})
				},
			}
			return m, nil
		},
	}
	return m, nil
}

// promptDownload asks what to download from the highlighted machine and
// where to put it, then starts the copy.
func (m model) promptDownload() (tea.Model, tea.Cmd) {
	targets := m.targets()
	switch {
	case len(targets) == 0:
		return m, nil
	case len(targets) > 1:
		m.notify.sshError = fmt.Errorf("download works on one machine; clear the marks (esc) first")
		return m, nil
	}
	name := shortHost(displayName(targets[0]))
	m.input = textInput{
		mode: inputText, label: "Download from " + name + ": remote path",
		path: pathRemoteAny, device: targets[0],
		hint: "a file or directory; relative paths start in the home directory",
		onSubmit: func(m model, v string) (tea.Model, tea.Cmd) {
			m.input = textInput{}
			remote := strings.TrimSpace(v)
			if remote == "" {
				return m, nil
			}
			m.input = textInput{
				mode: inputText, label: "Download to: local directory", value: ".", cursor: 1, path: pathDirs,
				hint: "an existing directory on this computer",
				onSubmit: func(m model, v string) (tea.Model, tea.Cmd) {
					m.input = textInput{}
					dir := expandHome(v)
					if dir == "" {
						dir = "."
					}
					if st, err := os.Stat(dir); err != nil || !st.IsDir() {
						m.notify.sshError = fmt.Errorf("%q is not an existing directory", dir)
						return m, nil
					}
					return m.launch(sshJob{action: sshActionCopy, copy: copySpec{local: dir, remote: remote, download: true}})
				},
			}
			return m, nil
		},
	}
	return m, nil
}
