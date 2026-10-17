package tui

import (
	"sort"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yatsenko-ihor/ts-cli/config"
)

// Persistence helpers. All writes go through config.Update, which performs a
// locked read-modify-write of the whole file, so the TUI can never drop
// fields written by the CLI commands.

// storeUsername saves the default SSH username to config.json
func storeUsername(username string) tea.Cmd {
	return func() tea.Msg {
		err := config.Update(func(cfg *config.Config) error {
			cfg.SSHUsername = username
			return nil
		})
		return usernameStoredMsg{err: err}
	}
}

// clearUsername removes the default SSH username from config.json
func clearUsername() tea.Cmd {
	return storeUsername("")
}

// storeMachineCredential saves one machine's SSH credentials to config.json
func storeMachineCredential(key string, cred machineCredential) tea.Cmd {
	return func() tea.Msg {
		err := config.Update(func(cfg *config.Config) error {
			cfg.SetMachineCredential(key, config.MachineAuth{
				Name:              cred.name,
				Username:          cred.username,
				PasswordEncrypted: cred.passwordEncrypted,
			})
			return nil
		})
		return machineCredsSavedMsg{err: err}
	}
}

// deleteMachineCredential removes one machine's SSH credentials from config.json
func deleteMachineCredential(key string) tea.Cmd {
	return func() tea.Msg {
		err := config.Update(func(cfg *config.Config) error {
			cfg.DeleteMachineCredential(key)
			return nil
		})
		return machineCredsSavedMsg{err: err}
	}
}

// replaceMachineCredentials overwrites the whole credential map on disk.
// Used once after a key migration (Name → device ID).
func replaceMachineCredentials(creds map[string]machineCredential) tea.Cmd {
	snapshot := make(map[string]config.MachineAuth, len(creds))
	for k, c := range creds {
		snapshot[k] = config.MachineAuth{Name: c.name, Username: c.username, PasswordEncrypted: c.passwordEncrypted}
	}
	return func() tea.Msg {
		err := config.Update(func(cfg *config.Config) error {
			cfg.MachineCredentials = snapshot
			return nil
		})
		return machineCredsSavedMsg{err: err}
	}
}

// getSortedMachineCredIDs returns machine keys sorted by display name
func (m model) getSortedMachineCredIDs() []string {
	ids := make([]string, 0, len(m.ssh.machineCreds))
	for id := range m.ssh.machineCreds {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		return m.credDisplayName(ids[i]) < m.credDisplayName(ids[j])
	})
	return ids
}

// credDisplayName returns the human-readable label for a stored credential.
func (m model) credDisplayName(key string) string {
	if c, ok := m.ssh.machineCreds[key]; ok && c.name != "" {
		return c.name
	}
	return key
}
