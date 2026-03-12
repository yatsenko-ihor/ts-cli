package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Account represents a Tailscale account configuration
type Account struct {
	Name    string `json:"name"`    // User-friendly name for the account
	APIKey  string `json:"api_key"` // Tailscale API key
	Tailnet string `json:"tailnet"` // Tailnet name
	Active  bool   `json:"active"`  // Whether this is the active account
}

// Config represents the application configuration
type Config struct {
	Accounts      []Account `json:"accounts"`
	SSHUsername   string    `json:"ssh_username,omitempty"`
	ConfigVersion string    `json:"config_version"` // For future migrations
}

// GetConfigPath returns the path to the config file
func GetConfigPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	configDir := filepath.Join(homeDir, ".ts-cli")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return "", fmt.Errorf("failed to create config directory: %w", err)
	}

	return filepath.Join(configDir, "config.json"), nil
}

// LoadConfig loads the configuration from disk
func LoadConfig() (*Config, error) {
	configPath, err := GetConfigPath()
	if err != nil {
		return nil, err
	}

	// Check if config file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// Try to migrate from old config format
		if config, migrated := migrateOldConfig(); migrated {
			return config, nil
		}
		// Return empty config if no old config exists
		return &Config{
			Accounts:      []Account{},
			ConfigVersion: "1.0",
		}, nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &config, nil
}

// SaveConfig saves the configuration to disk
func SaveConfig(config *Config) error {
	configPath, err := GetConfigPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// migrateOldConfig attempts to migrate from the old config format
func migrateOldConfig() (*Config, bool) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, false
	}

	oldConfigPath := filepath.Join(homeDir, ".ts-cli", "config")
	data, err := os.ReadFile(oldConfigPath)
	if err != nil {
		return nil, false
	}

	// Parse old config format
	var apiKey, tailnet, sshUsername string
	lines := string(data)
	for _, line := range splitLines(lines) {
		if len(line) == 0 {
			continue
		}
		parts := splitOnce(line, "=")
		if len(parts) != 2 {
			continue
		}
		key := parts[0]
		value := parts[1]

		switch key {
		case "TAILSCALE_API_KEY":
			apiKey = value
		case "TAILNET":
			tailnet = value
		case "SSH_USERNAME":
			sshUsername = value
		}
	}

	if apiKey == "" || tailnet == "" {
		return nil, false
	}

	// Create new config with migrated data
	config := &Config{
		Accounts: []Account{
			{
				Name:    tailnet, // Use tailnet as name
				APIKey:  apiKey,
				Tailnet: tailnet,
				Active:  true,
			},
		},
		SSHUsername:   sshUsername,
		ConfigVersion: "1.0",
	}

	// Save the new config
	if err := SaveConfig(config); err != nil {
		return nil, false
	}

	// Rename old config to backup
	os.Rename(oldConfigPath, oldConfigPath+".backup")

	return config, true
}

// AddAccount adds a new account to the configuration
func (c *Config) AddAccount(name, apiKey, tailnet string, setActive bool) error {
	// Check if account already exists
	for i, acc := range c.Accounts {
		if acc.Tailnet == tailnet {
			// Update existing account
			c.Accounts[i].Name = name
			c.Accounts[i].APIKey = apiKey
			if setActive {
				c.SetActiveAccount(tailnet)
			}
			return nil
		}
	}

	// Add new account
	account := Account{
		Name:    name,
		APIKey:  apiKey,
		Tailnet: tailnet,
		Active:  setActive,
	}

	// If setting as active, deactivate all others
	if setActive {
		for i := range c.Accounts {
			c.Accounts[i].Active = false
		}
	}

	c.Accounts = append(c.Accounts, account)
	return nil
}

// GetActiveAccount returns the currently active account
func (c *Config) GetActiveAccount() *Account {
	for i := range c.Accounts {
		if c.Accounts[i].Active {
			return &c.Accounts[i]
		}
	}
	return nil
}

// GetAccountByTailnet returns an account by tailnet name
func (c *Config) GetAccountByTailnet(tailnet string) *Account {
	for i := range c.Accounts {
		if c.Accounts[i].Tailnet == tailnet {
			return &c.Accounts[i]
		}
	}
	return nil
}

// SetActiveAccount sets the active account by tailnet
func (c *Config) SetActiveAccount(tailnet string) bool {
	// First check if the account exists
	found := false
	for i := range c.Accounts {
		if c.Accounts[i].Tailnet == tailnet {
			found = true
			break
		}
	}
	
	// Only modify account states if the tailnet was found
	if found {
		for i := range c.Accounts {
			c.Accounts[i].Active = c.Accounts[i].Tailnet == tailnet
		}
	}
	
	return found
}

// GetAllAccounts returns all configured accounts
func (c *Config) GetAllAccounts() []Account {
	return c.Accounts
}

// Helper functions
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func splitOnce(s, sep string) []string {
	idx := 0
	for i := 0; i < len(s); i++ {
		if i+len(sep) <= len(s) && s[i:i+len(sep)] == sep {
			idx = i
			break
		}
	}
	if idx == 0 {
		return []string{s}
	}
	return []string{s[:idx], s[idx+len(sep):]}
}
