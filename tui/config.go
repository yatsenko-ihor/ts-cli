package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
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
