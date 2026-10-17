package tui

import (
	"fmt"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yatsenko-ihor/ts-cli/client"
	"github.com/yatsenko-ihor/ts-cli/util"
)

// SSH credential flow.
//
//  1. Saved password for the machine        → connect with it, no questions.
//  2. No saved password                      → try once without a password
//     (key-based auth). Interactive sessions and remote commands both use
//     BatchMode so a missing key fails fast instead of ssh prompting on the
//     TUI's terminal.
//  3. That attempt fails with an auth error  → ask for the password in the
//     TUI and retry with sshpass.
//  4. The attempt succeeds                   → persist username (+ password
//     if one was entered). Nothing is written before success.
//  5. A saved password is rejected           → it is deleted and the user is
//     asked for a new one.

// machineKey returns the stable credential/history key for a device.
func machineKey(d client.Device) string {
	if d.ID != "" {
		return d.ID
	}
	return d.Hostname
}

// displayName returns the human-readable name of a device.
func displayName(d client.Device) string {
	if d.Name != "" {
		return d.Name
	}
	return d.Hostname
}

// sshpassAvailable reports whether password authentication can be driven
// from the TUI at all.
func sshpassAvailable() bool {
	_, err := exec.LookPath("sshpass")
	return err == nil
}

// beginSSH starts a new attempt for the device at index. It performs the
// account switch first when the device belongs to another tailnet.
func (m model) beginSSH(index int, action sshAction, command string) (tea.Model, tea.Cmd) {
	if index < 0 || index >= len(m.list.filteredDevices) {
		return m, nil
	}
	device := m.list.filteredDevices[index]

	m.notify.sshError = nil
	m.ssh.pending = pendingSSH{
		action:      action,
		deviceIndex: index,
		machineID:   machineKey(device),
		machineName: displayName(device),
		command:     command,
	}

	if m.needsAccountSwitch(device) {
		return m, m.switchAccountForSSH(index, device.AccountTailnet)
	}
	return m.continueSSH()
}

// needsAccountSwitch compares the device's tailnet with the daemon's login.
func (m model) needsAccountSwitch(device client.Device) bool {
	if device.AccountTailnet == "" || m.acct.tailscaleActiveAccount == "" {
		return false
	}
	deviceAccount := strings.ToLower(device.AccountTailnet)
	activeAccount := strings.ToLower(m.acct.tailscaleActiveAccount)
	// The status output may truncate the login ("user@"), so accept a prefix match either way.
	return !strings.HasPrefix(deviceAccount, strings.TrimSuffix(activeAccount, "@")) &&
		!strings.HasPrefix(activeAccount, strings.TrimSuffix(deviceAccount, "@"))
}

// continueSSH resolves the username (prompting if unknown) and runs the attempt.
func (m model) continueSSH() (tea.Model, tea.Cmd) {
	p := &m.ssh.pending
	if p.action == sshActionNone {
		return m, nil
	}

	if p.username == "" {
		if cred, ok := m.ssh.machineCreds[p.machineID]; ok && cred.username != "" {
			p.username = cred.username
		} else {
			p.username = m.ssh.username
		}
	}
	if p.username == "" {
		m.input = textInput{mode: inputUsername}
		return m, nil
	}
	if p.askPasswordNext {
		p.askPasswordNext = false
		m.input = textInput{mode: inputPassword}
		return m, nil
	}
	return m.runPendingAttempt()
}

// runPendingAttempt executes the pending action with the best password we have.
func (m model) runPendingAttempt() (tea.Model, tea.Cmd) {
	p := &m.ssh.pending
	if p.deviceIndex < 0 || p.deviceIndex >= len(m.list.filteredDevices) {
		m.ssh.pending = pendingSSH{}
		m.notify.sshError = fmt.Errorf("device list changed; please select the device again")
		return m, nil
	}
	device := m.list.filteredDevices[p.deviceIndex]

	password := p.password
	p.usedSavedPass = false
	if password == "" {
		if saved := m.savedPassword(p.machineID); saved != "" {
			password = saved
			p.usedSavedPass = true
		}
	}

	switch p.action {
	case sshActionSession:
		return m, m.sshToDevice(device, p.username, password)
	case sshActionCommand:
		return m, m.executeRemoteCommand(device, p.username, password, p.command)
	}
	return m, nil
}

// savedPassword decrypts the stored password for a machine key, if any.
func (m model) savedPassword(key string) string {
	cred, ok := m.ssh.machineCreds[key]
	if !ok || cred.passwordEncrypted == "" {
		return ""
	}
	plain, err := util.DecryptPassword(cred.passwordEncrypted)
	if err != nil {
		return ""
	}
	return plain
}

// passwordForDevice is used by code paths outside the pending flow (e.g.
// resolving a remote path for the clipboard): saved password, or the one
// entered for the attempt in progress on the same machine.
func (m model) passwordForDevice(device client.Device) string {
	key := machineKey(device)
	if m.ssh.pending.machineID == key && m.ssh.pending.password != "" {
		return m.ssh.pending.password
	}
	return m.savedPassword(key)
}

// usernameForDevice returns the username that would be used for a device.
func (m model) usernameForDevice(device client.Device) string {
	if cred, ok := m.ssh.machineCreds[machineKey(device)]; ok && cred.username != "" {
		return cred.username
	}
	return m.ssh.username
}

// finishSSHAttempt handles the outcome of a session or command attempt.
// It returns the updated model and any persistence commands.
func (m model) finishSSHAttempt(failure sshFailure, err error) (tea.Model, tea.Cmd) {
	p := m.ssh.pending
	if p.action == sshActionNone {
		return m, nil
	}

	switch failure {
	case sshFailureNone, sshFailureRemoteExit:
		// The connection worked. Persist what we learned, then clear.
		cmds := m.persistSuccessfulCredentials()
		if failure == sshFailureRemoteExit {
			m.notify.sshError = fmt.Errorf("remote session ended with an error: %v", err)
		}
		m.ssh.pending = pendingSSH{}
		return m, tea.Batch(cmds...)

	case sshFailureBadPassword:
		var cmds []tea.Cmd
		if p.usedSavedPass {
			// Forget the stored password (keep the username) and ask again.
			if cred, ok := m.ssh.machineCreds[p.machineID]; ok {
				cred.passwordEncrypted = ""
				m.ssh.machineCreds[p.machineID] = cred
				cmds = append(cmds, storeMachineCredential(p.machineID, cred))
			}
			m.notify.sshError = fmt.Errorf("saved password for %s was rejected; enter the current one", p.machineName)
		} else {
			m.notify.sshError = fmt.Errorf("password rejected for %s@%s; try again", p.username, p.machineName)
		}
		m.ssh.pending.password = ""
		m.ssh.pending.passwordTried = true
		m.input = textInput{mode: inputPassword}
		return m, tea.Batch(cmds...)

	case sshFailureConnection:
		// Key-based attempt failed: ask for a password, once. If that also
		// fails (or no sshpass is installed), fall back to the retry menu.
		if !p.passwordTried && p.password == "" && !p.usedSavedPass && sshpassAvailable() {
			m.notify.sshError = fmt.Errorf("could not log in as %s without a password: %v", p.username, err)
			m.input = textInput{mode: inputPassword}
			return m, nil
		}
		m.ssh.retryMode = true
		m.ssh.retryCursor = 0
		m.notify.sshError = err
		return m, nil

	default: // sshFailureExec
		m.notify.sshError = fmt.Errorf("could not start ssh: %w", err)
		m.ssh.pending = pendingSSH{}
		return m, nil
	}
}

// persistSuccessfulCredentials writes the username and, if one was entered,
// the password used by the successful attempt. It updates in-memory state
// and returns the commands that write config.json.
func (m *model) persistSuccessfulCredentials() []tea.Cmd {
	p := m.ssh.pending
	var cmds []tea.Cmd

	cred := m.ssh.machineCreds[p.machineID]
	changed := false
	if cred.name != p.machineName {
		cred.name = p.machineName
		changed = true
	}
	if cred.username != p.username {
		cred.username = p.username
		changed = true
	}
	if p.password != "" {
		if enc, err := util.EncryptPassword(p.password); err == nil {
			cred.passwordEncrypted = enc
			changed = true
		} else {
			m.notify.sshError = fmt.Errorf("connected, but could not encrypt password for saving: %w", err)
		}
	}
	if changed {
		if m.ssh.machineCreds == nil {
			m.ssh.machineCreds = make(map[string]machineCredential)
		}
		m.ssh.machineCreds[p.machineID] = cred
		cmds = append(cmds, storeMachineCredential(p.machineID, cred))
	}

	// First successful login also sets the default username for new machines.
	if m.ssh.username == "" && p.username != "" {
		m.ssh.username = p.username
		cmds = append(cmds, storeUsername(p.username))
	}
	return cmds
}

// cancelSSHAttempt aborts the pending flow (Esc in a prompt or retry menu).
func (m model) cancelSSHAttempt() model {
	m.ssh.pending = pendingSSH{}
	m.ssh.retryMode = false
	m.input = textInput{mode: inputNone}
	m.notify.sshError = nil
	return m
}

// migrateCredentialKeys moves credentials stored under a device's Name or
// Hostname (older versions) to its stable ID and fills in display names.
// It reports whether anything changed.
func migrateCredentialKeys(creds map[string]machineCredential, devices []client.Device) bool {
	changed := false
	for _, d := range devices {
		key := machineKey(d)
		if key == "" {
			continue
		}
		for _, legacy := range []string{d.Name, d.Hostname} {
			if legacy == "" || legacy == key {
				continue
			}
			old, ok := creds[legacy]
			if !ok {
				continue
			}
			if _, exists := creds[key]; !exists {
				old.name = displayName(d)
				creds[key] = old
			}
			delete(creds, legacy)
			changed = true
		}
		if c, ok := creds[key]; ok && c.name == "" {
			c.name = displayName(d)
			creds[key] = c
			changed = true
		}
	}
	return changed
}
