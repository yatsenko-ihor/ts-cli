package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ihor/ts-cli/util"
)

// Config functions for persisting TUI user preferences
// These operate directly on the config file to avoid import cycles with the commands package

// storeUsername saves the SSH username to the ts-cli config file
func (m model) storeUsername(username string) tea.Cmd {
	return func() tea.Msg {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return usernameStoredMsg{err: err}
		}

		configDir := filepath.Join(homeDir, ".ts-cli")
		if err := os.MkdirAll(configDir, 0700); err != nil {
			return usernameStoredMsg{err: err}
		}

		// Read existing config
		configFile := filepath.Join(configDir, "config")
		content, err := os.ReadFile(configFile)
		if err != nil && !os.IsNotExist(err) {
			return usernameStoredMsg{err: err}
		}

		// Parse existing config and update SSH_USERNAME
		lines := []string{}
		found := false
		for _, line := range strings.Split(string(content), "\n") {
			if strings.HasPrefix(line, "SSH_USERNAME=") {
				lines = append(lines, fmt.Sprintf("SSH_USERNAME=%s", username))
				found = true
			} else if line != "" {
				lines = append(lines, line)
			}
		}

		// Add SSH_USERNAME if not found
		if !found {
			lines = append(lines, fmt.Sprintf("SSH_USERNAME=%s", username))
		}

		// Write back
		newContent := strings.Join(lines, "\n") + "\n"
		if err := os.WriteFile(configFile, []byte(newContent), 0600); err != nil {
			return usernameStoredMsg{err: err}
		}

		return usernameStoredMsg{err: nil}
	}
}

// clearUsername removes the stored SSH username from the config file
func (m model) clearUsername() tea.Cmd {
	return func() tea.Msg {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return usernameStoredMsg{err: err}
		}

		configDir := filepath.Join(homeDir, ".ts-cli")
		configFile := filepath.Join(configDir, "config")

		// Read existing config
		content, err := os.ReadFile(configFile)
		if err != nil {
			if os.IsNotExist(err) {
				// No config file, nothing to clear
				return usernameStoredMsg{err: nil}
			}
			return usernameStoredMsg{err: err}
		}

		// Parse existing config and remove SSH_USERNAME
		lines := []string{}
		for _, line := range strings.Split(string(content), "\n") {
			if !strings.HasPrefix(line, "SSH_USERNAME=") && line != "" {
				lines = append(lines, line)
			}
		}

		// Write back
		newContent := strings.Join(lines, "\n")
		if newContent != "" {
			newContent += "\n"
		}
		if err := os.WriteFile(configFile, []byte(newContent), 0600); err != nil {
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

		return passwordStoredMsg{err: nil}
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
