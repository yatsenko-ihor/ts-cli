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

	// Note: Account switching is handled by handleSSHRequest before calling this function

	// Get the primary IP address
	if len(device.Addresses) == 0 {
		return func() tea.Msg {
			return sshMsg{err: fmt.Errorf("device has no IP addresses")}
		}
	}

	address := device.Addresses[0]
	name := device.Name
	if name == "" {
		name = device.Hostname
	}

	// Build SSH command with username if available
	var sshTarget string
	if m.ssh.username != "" {
		sshTarget = fmt.Sprintf("%s@%s", m.ssh.username, address)
	} else {
		sshTarget = address
	}

	// Log SSH connection details with account information
	accountLabel := "default"
	if device.AccountName != "" {
		accountLabel = device.AccountName
	}

	// Use tea.Sequence to print logs then execute SSH
	return tea.Sequence(
		tea.Println(fmt.Sprintf("\n🔌 Connecting to %s : %s", name, accountLabel)),
		tea.Println(fmt.Sprintf("📡 SSH command: ssh %s\n", sshTarget)),
		func() tea.Msg {
			// Check if we have a saved password and sshpass is available
			var sshCmd *exec.Cmd
			if m.ssh.passwordEncrypted != "" {
				decrypted, err := util.DecryptPassword(m.ssh.passwordEncrypted)
				if err == nil {
					// Check if sshpass is available
					if _, lookErr := exec.LookPath("sshpass"); lookErr == nil {
						sshCmd = exec.Command("sshpass", "-p", decrypted, "ssh",
							"-o", "StrictHostKeyChecking=accept-new", sshTarget)
					}
				}
			}
			if sshCmd == nil {
				sshCmd = exec.Command("ssh", sshTarget)
			}
			return tea.ExecProcess(sshCmd, func(err error) tea.Msg {
				if err != nil {
					return sshMsg{err: err}
				}
				return sshMsg{}
			})()
		},
	)
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
