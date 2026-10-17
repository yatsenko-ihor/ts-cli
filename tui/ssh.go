package tui

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yatsenko-ihor/ts-cli/client"
	"github.com/yatsenko-ihor/ts-cli/util"
)

// SSH operation functions for connecting to and executing commands on remote devices

// Exit codes used to tell a credential problem apart from an ordinary
// non-zero exit of the remote shell.
const (
	exitSSHPassWrongPassword = 5   // sshpass: "invalid/incorrect password"
	exitSSHConnectionFailed  = 255 // ssh: connection or authentication error
)

// sshFailure classifies why an SSH process ended with an error.
type sshFailure int

const (
	sshFailureNone        sshFailure = iota
	sshFailureBadPassword            // sshpass rejected the saved password
	sshFailureConnection             // ssh could not connect or authenticate
	sshFailureRemoteExit             // session worked; remote shell exited non-zero
	sshFailureExec                   // process could not be started at all
)

// classifySSHError maps the error returned by exec/tea.Exec to an sshFailure.
func classifySSHError(err error) sshFailure {
	if err == nil {
		return sshFailureNone
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return sshFailureExec
	}
	switch exitErr.ExitCode() {
	case exitSSHPassWrongPassword:
		return sshFailureBadPassword
	case exitSSHConnectionFailed:
		return sshFailureConnection
	default:
		return sshFailureRemoteExit
	}
}

// buildSSHTarget validates username and address and returns "user@addr" or "addr".
// Both values can come from disk (config.json) or the API, so they are checked
// here rather than trusted.
func buildSSHTarget(username, address string) (string, error) {
	if net.ParseIP(address) == nil {
		return "", fmt.Errorf("device address %q is not a valid IP", address)
	}
	if username == "" {
		return address, nil
	}
	if err := util.ValidateSSHUsername(username); err != nil {
		return "", fmt.Errorf("invalid SSH username: %w", err)
	}
	return username + "@" + address, nil
}

// newSSHCommand builds the ssh (or sshpass+ssh) process for target.
//
// When a password is available and sshpass is installed, the password is
// written to a private pipe and handed to sshpass via `-d <fd>`, so it never
// appears in the process argument list. The returned cleanup func must be
// called once the process has finished to release the pipe.
//
// batch requests `-o BatchMode=yes` for non-interactive runs without a
// password, so ssh fails fast instead of trying to prompt on the TUI's tty.
func newSSHCommand(target, password string, batch bool, remoteArgs ...string) (*exec.Cmd, func(), error) {
	sshArgs := []string{"-o", "StrictHostKeyChecking=accept-new"}

	if password != "" {
		if sshpassPath, err := exec.LookPath("sshpass"); err == nil {
			r, w, err := os.Pipe()
			if err != nil {
				return nil, nil, fmt.Errorf("failed to create password pipe: %w", err)
			}
			if _, err := io.WriteString(w, password+"\n"); err != nil {
				w.Close()
				r.Close()
				return nil, nil, fmt.Errorf("failed to write password pipe: %w", err)
			}
			w.Close()

			// ExtraFiles[0] becomes fd 3 in the child.
			args := append([]string{"-d", "3", "ssh"}, sshArgs...)
			args = append(args, target)
			args = append(args, remoteArgs...)
			cmd := exec.Command(sshpassPath, args...)
			cmd.ExtraFiles = []*os.File{r}
			return cmd, func() { r.Close() }, nil
		}
	}

	if batch {
		sshArgs = append(sshArgs, "-o", "BatchMode=yes")
	}
	args := append(sshArgs, target)
	args = append(args, remoteArgs...)
	return exec.Command("ssh", args...), func() {}, nil
}

// bannerExec implements tea.ExecCommand: it prints a connection banner on the
// freed terminal and then runs the wrapped ssh command. This replaces the
// previous `sh -c "printf ... && ssh ..."` wrapper.
type bannerExec struct {
	banner string
	cmd    *exec.Cmd
	stdout io.Writer
}

func (b *bannerExec) Run() error {
	if b.stdout != nil && b.banner != "" {
		fmt.Fprint(b.stdout, b.banner)
	}
	return b.cmd.Run()
}
func (b *bannerExec) SetStdin(r io.Reader)  { b.cmd.Stdin = r }
func (b *bannerExec) SetStdout(w io.Writer) { b.stdout = w; b.cmd.Stdout = w }
func (b *bannerExec) SetStderr(w io.Writer) { b.cmd.Stderr = w }

// sshToDevice starts an interactive SSH session with the given credentials.
// When no password is available and sshpass is installed, BatchMode makes a
// failed key login return quickly (exit 255) so the TUI can ask for a
// password; without sshpass, ssh is left free to prompt on the terminal.
func (m model) sshToDevice(device client.Device, username, password string) tea.Cmd {
	if len(device.Addresses) == 0 {
		return func() tea.Msg {
			return sshMsg{err: fmt.Errorf("device has no IP addresses"), failure: sshFailureExec}
		}
	}

	target, err := buildSSHTarget(username, device.Addresses[0])
	if err != nil {
		return func() tea.Msg { return sshMsg{err: err, failure: sshFailureExec} }
	}

	batch := password == "" && sshpassAvailable()
	cmd, cleanup, err := newSSHCommand(target, password, batch)
	if err != nil {
		return func() tea.Msg { return sshMsg{err: err, failure: sshFailureExec} }
	}

	banner := fmt.Sprintf("\nConnection to %s (%s)\n", displayName(device), target)
	return tea.Exec(&bannerExec{banner: banner, cmd: cmd}, func(err error) tea.Msg {
		cleanup()
		return sshMsg{err: err, failure: classifySSHError(err)}
	})
}

// startRemoteJob starts the pending command or script on device and begins
// streaming its output into the Output panel. It returns the Cmd that waits
// for the first output event, or an error when the process could not start.
func (m *model) startRemoteJob(device client.Device, username, password string) (tea.Cmd, error) {
	p := m.ssh.pending
	if len(device.Addresses) == 0 {
		return nil, fmt.Errorf("device has no IP addresses")
	}
	sshTarget, err := buildSSHTarget(username, device.Addresses[0])
	if err != nil {
		return nil, err
	}

	var remote string
	var stdin []byte
	switch p.action {
	case sshActionCommand:
		remote = p.command
	case sshActionScript:
		if p.script == nil {
			return nil, fmt.Errorf("no script selected")
		}
		remote = p.script.RemoteCommand(p.args)
		stdin = p.script.Content
	default:
		return nil, fmt.Errorf("nothing to run")
	}

	cmd, cleanup, err := newSSHCommand(sshTarget, password, true, remote)
	if err != nil {
		return nil, err
	}
	job, err := startRunJob(cmd, stdin, cleanup)
	if err != nil {
		return nil, fmt.Errorf("could not start ssh: %w", err)
	}

	m.beginJobOutput(job, p.label())
	return tea.Batch(waitRunEvent(job), tickRun(job)), nil
}

// resolveRemoteOutputPath resolves a relative path in command output to an absolute remote path
func (m model) resolveRemoteOutputPath(entry string) (string, error) {
	target := m.getTargetDevice()
	if target < 0 || target >= len(m.list.filteredDevices) {
		return "", fmt.Errorf("no device selected")
	}

	device := m.list.filteredDevices[target]
	if len(device.Addresses) == 0 {
		return "", fmt.Errorf("device has no IP addresses")
	}

	sshTarget, err := buildSSHTarget(m.usernameForDevice(device), device.Addresses[0])
	if err != nil {
		return "", err
	}

	pwdCmd, cleanup, err := newSSHCommand(sshTarget, m.passwordForDevice(device), true, "pwd")
	if err != nil {
		return "", err
	}
	defer cleanup()

	output, err := pwdCmd.Output()
	if err != nil {
		return "", err
	}

	remoteCwd := strings.TrimSpace(string(output))
	if remoteCwd == "" {
		return "", fmt.Errorf("empty remote cwd")
	}

	return path.Clean(path.Join(remoteCwd, entry)), nil
}

// copySSHCommand copies the SSH command to the clipboard
func (m model) copySSHCommand(index int) tea.Cmd {
	device := m.list.filteredDevices[index]

	if len(device.Addresses) == 0 {
		return func() tea.Msg {
			return copiedMsg{success: false, text: ""}
		}
	}

	target, err := buildSSHTarget(m.usernameForDevice(device), device.Addresses[0])
	if err != nil {
		return func() tea.Msg {
			return copiedMsg{success: false, text: ""}
		}
	}

	return copyTextToClipboard("ssh " + target)
}
