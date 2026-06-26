package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ihor/ts-cli/util"
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

// storePassword encrypts and saves the SSH password to config.json
func (m model) storePassword(password string) tea.Cmd {
	return func() tea.Msg {
		encrypted, err := util.EncryptPassword(password)
		if err != nil {
			return passwordStoredMsg{err: fmt.Errorf("failed to encrypt password: %w", err)}
		}

		config, err := loadConfigJSON()
		if err != nil {
			return passwordStoredMsg{err: err}
		}

		config.SSHPassword = encrypted
		config.SavePasswordEnabled = true

		if err := saveConfigJSON(config); err != nil {
			return passwordStoredMsg{err: err}
		}

		return passwordStoredMsg{err: nil, encrypted: encrypted}
	}
}

// clearSavedPassword removes the saved password from config.json
func (m model) clearSavedPassword() tea.Cmd {
	return func() tea.Msg {
		config, err := loadConfigJSON()
		if err != nil {
			return passwordStoredMsg{err: err}
		}

		config.SSHPassword = ""

		if err := saveConfigJSON(config); err != nil {
			return passwordStoredMsg{err: err}
		}

		return passwordStoredMsg{err: nil}
	}
}

// toggleSavePassword enables/disables the password saving feature
func (m model) toggleSavePassword(enabled bool) tea.Cmd {
	return func() tea.Msg {
		config, err := loadConfigJSON()
		if err != nil {
			return optionToggledMsg{err: err}
		}

		config.SavePasswordEnabled = enabled
		if !enabled {
			config.SSHPassword = ""
		}

		if err := saveConfigJSON(config); err != nil {
			return optionToggledMsg{err: err}
		}

		return optionToggledMsg{err: nil}
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
	Accounts            json.RawMessage `json:"accounts,omitempty"`
	SSHUsername         string          `json:"ssh_username,omitempty"`
	SSHPassword         string          `json:"ssh_password,omitempty"`
	SavePasswordEnabled bool            `json:"save_password_enabled"`
	ConfigVersion       string          `json:"config_version,omitempty"`
}
