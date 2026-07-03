package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	tea "github.com/charmbracelet/bubbletea"
)

// Config functions for persisting TUI user preferences
// These operate directly on the config file to avoid import cycles with the commands package

// storeUsername saves the SSH username to config.json
func (m model) storeUsername(username string) tea.Cmd {
	return func() tea.Msg {
		config, err := loadConfigJSON()
		if err != nil {
			return usernameStoredMsg{err: err}
		}

		config.SSHUsername = username

		if err := saveConfigJSON(config); err != nil {
			return usernameStoredMsg{err: err}
		}

		return usernameStoredMsg{err: nil}
	}
}

// clearUsername removes the stored SSH username from config.json
func (m model) clearUsername() tea.Cmd {
	return func() tea.Msg {
		config, err := loadConfigJSON()
		if err != nil {
			return usernameStoredMsg{err: err}
		}

		config.SSHUsername = ""

		if err := saveConfigJSON(config); err != nil {
			return usernameStoredMsg{err: err}
		}

		return usernameStoredMsg{err: nil}
	}
}

// loadConfigJSON loads config.json (used by TUI to avoid import cycle with commands)
func loadConfigJSON() (*configJSON, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	configFile := filepath.Join(homeDir, ".ts-cli", "config.json")
	data, err := os.ReadFile(configFile)
	if err != nil {
		if os.IsNotExist(err) {
			return &configJSON{}, nil
		}
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var config configJSON
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return &config, nil
}

// saveConfigJSON saves config.json
func saveConfigJSON(config *configJSON) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	configDir := filepath.Join(homeDir, ".ts-cli")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	return os.WriteFile(filepath.Join(configDir, "config.json"), data, 0600)
}

// configJSON mirrors the relevant fields from commands.Config to avoid import cycles
type configJSON struct {
	Accounts           json.RawMessage        `json:"accounts,omitempty"`
	SSHUsername        string                 `json:"ssh_username,omitempty"`
	MachineCredentials map[string]machineAuth `json:"machine_credentials,omitempty"`
	ConfigVersion      string                 `json:"config_version,omitempty"`
}

// machineAuth stores per-machine SSH credentials
type machineAuth struct {
	Username          string `json:"username,omitempty"`
	PasswordEncrypted string `json:"password,omitempty"`
}

// storeMachineCredentials saves per-machine SSH credentials to config
func storeMachineCredentials(machineID, username, passwordEncrypted string) tea.Cmd {
	return func() tea.Msg {
		cfg, err := loadConfigJSON()
		if err != nil {
			return machineCredsSavedMsg{err: err}
		}
		if cfg.MachineCredentials == nil {
			cfg.MachineCredentials = make(map[string]machineAuth)
		}
		cfg.MachineCredentials[machineID] = machineAuth{
			Username:          username,
			PasswordEncrypted: passwordEncrypted,
		}
		err = saveConfigJSON(cfg)
		return machineCredsSavedMsg{err: err}
	}
}

// deleteMachineCredentials removes per-machine SSH credentials from config
func (m model) deleteMachineCredentials(machineID string) tea.Cmd {
	return func() tea.Msg {
		cfg, err := loadConfigJSON()
		if err != nil {
			return machineCredsSavedMsg{err: err}
		}
		delete(cfg.MachineCredentials, machineID)
		err = saveConfigJSON(cfg)
		return machineCredsSavedMsg{err: err}
	}
}

// getSortedMachineCredIDs returns machine IDs sorted alphabetically
func (m model) getSortedMachineCredIDs() []string {
	ids := make([]string, 0, len(m.ssh.machineCreds))
	for id := range m.ssh.machineCreds {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
