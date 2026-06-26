package tui

import (
	"fmt"
	"os/exec"
	"path"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ihor/ts-cli/util"
)

// SSH operation functions for connecting to and executing commands on remote devices

// sshToDevice initiates an interactive SSH connection to a device
func (m model) sshToDevice(index int) tea.Cmd {
	device := m.list.filteredDevices[index]

	// Get the primary IP address
	if len(device.Addresses) == 0 {
		return func() tea.Msg {
			return sshMsg{err: fmt.Errorf("device has no IP addresses")}
		}
	}

	address := device.Addresses[0]

	// Build SSH command with username if available
	var sshTarget string
	if m.ssh.username != "" {
		sshTarget = fmt.Sprintf("%s@%s", m.ssh.username, address)
	} else {
		sshTarget = address
	}

	// Execute SSH with connection info printed to terminal
	return tea.ExecProcess(m.buildSSHCommand(sshTarget), func(err error) tea.Msg {
		if err != nil {
			return sshMsg{err: err}
		}
		return sshMsg{}
	})
}

// buildSSHCommand creates the SSH exec.Cmd with sshpass if password is available.
// Wraps in shell to print connection info after TUI releases the terminal.
func (m model) buildSSHCommand(sshTarget string) *exec.Cmd {
	name := m.getTargetDeviceName()
	ip := m.getTargetDeviceIP()

	var sshCmdStr string
	if m.ssh.passwordEncrypted != "" {
		decrypted, err := util.DecryptPassword(m.ssh.passwordEncrypted)
		if err == nil {
			if _, lookErr := exec.LookPath("sshpass"); lookErr == nil {
				sshCmdStr = fmt.Sprintf("sshpass -p %s ssh -o StrictHostKeyChecking=accept-new %s",
					shellEscape(decrypted), sshTarget)
			}
		}
	}
	if sshCmdStr == "" {
		sshCmdStr = fmt.Sprintf("ssh %s", sshTarget)
	}

	// Print connection info then exec SSH
	script := fmt.Sprintf("printf '\\nConnection to %s (%s)\\n' && %s",
		shellEscape(name), shellEscape(ip), sshCmdStr)
	return exec.Command("sh", "-c", script)
}

// shellEscape escapes a string for safe use in shell commands
func shellEscape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

// getTargetDeviceName returns the name of the currently targeted device
func (m model) getTargetDeviceName() string {
	target := m.getTargetDevice()
	if target < 0 || target >= len(m.list.filteredDevices) {
		return "unknown"
	}
	d := m.list.filteredDevices[target]
	if d.Name != "" {
		return d.Name
	}
	return d.Hostname
}

// getTargetDeviceIP returns the IP of the currently targeted device
func (m model) getTargetDeviceIP() string {
	target := m.getTargetDevice()
	if target < 0 || target >= len(m.list.filteredDevices) {
		return "unknown"
	}
	d := m.list.filteredDevices[target]
	if len(d.Addresses) > 0 {
		return d.Addresses[0]
	}
	return "no-ip"
}

// executeRemoteCommand executes a command on a remote device via SSH
func (m model) executeRemoteCommand(command string) tea.Cmd {
	target := m.getTargetDevice()
	if target < 0 || target >= len(m.list.filteredDevices) {
		return func() tea.Msg {
			return commandExecutedMsg{err: fmt.Errorf("no device selected")}
		}
	}

	device := m.list.filteredDevices[target]

	// Get the primary IP address
	if len(device.Addresses) == 0 {
		return func() tea.Msg {
			return commandExecutedMsg{err: fmt.Errorf("device has no IP addresses")}
		}
	}

	address := device.Addresses[0]
	name := device.Name
	if name == "" {
		name = device.Hostname
	}

	// Build SSH target
	var sshTarget string
	if m.ssh.username != "" {
		sshTarget = fmt.Sprintf("%s@%s", m.ssh.username, address)
	} else {
		sshTarget = address
	}

	// Get machine ID for history
	machineID := device.ID
	if machineID == "" {
		machineID = device.Hostname
	}

	return func() tea.Msg {
		// Execute command via SSH, using sshpass if password is saved
		var cmd *exec.Cmd
		if m.ssh.passwordEncrypted != "" {
			if decrypted, err := util.DecryptPassword(m.ssh.passwordEncrypted); err == nil {
				if _, lookErr := exec.LookPath("sshpass"); lookErr == nil {
					cmd = exec.Command("sshpass", "-p", decrypted, "ssh",
						"-o", "StrictHostKeyChecking=accept-new", sshTarget, command)
				}
			}
		}
		if cmd == nil {
			cmd = exec.Command("ssh", sshTarget, command)
		}
		output, err := cmd.CombinedOutput()

		exitCode := 0
		if err != nil {
			// Try to get exit code
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			}
		}

		// Save to history if history store is available
		if m.hist.history != nil {
			m.hist.history.AddCommand(machineID, name, command, exitCode, string(output))
			_ = m.hist.history.Save() // Ignore save errors
		}

		return commandExecutedMsg{
			output:   string(output),
			exitCode: exitCode,
			err:      err,
		}
	}
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

	address := device.Addresses[0]
	sshTarget := address
	if m.ssh.username != "" {
		sshTarget = fmt.Sprintf("%s@%s", m.ssh.username, address)
	}

	// Use sshpass if password is saved
	var pwdCmd *exec.Cmd
	if m.ssh.passwordEncrypted != "" {
		if decrypted, err := util.DecryptPassword(m.ssh.passwordEncrypted); err == nil {
			if _, lookErr := exec.LookPath("sshpass"); lookErr == nil {
				pwdCmd = exec.Command("sshpass", "-p", decrypted, "ssh",
					"-o", "StrictHostKeyChecking=accept-new", sshTarget, "pwd")
			}
		}
	}
	if pwdCmd == nil {
		pwdCmd = exec.Command("ssh", sshTarget, "pwd")
	}
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

	// Get the primary IP address
	if len(device.Addresses) == 0 {
		return func() tea.Msg {
			return copiedMsg{success: false, text: ""}
		}
	}

	address := device.Addresses[0]

	// Build SSH command with username if available
	var sshCommand string
	if m.ssh.username != "" {
		sshCommand = fmt.Sprintf("ssh %s@%s", m.ssh.username, address)
	} else {
		sshCommand = fmt.Sprintf("ssh %s", address)
	}

	return copyTextToClipboard(sshCommand)
}
