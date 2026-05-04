// Package services provides business logic layer for configuration management
package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ihor/ts-cli/internal/constants"
)

// Account represents a Tailscale account configuration
type Account struct {
	Name    string `json:"name"`
	APIKey  string `json:"api_key"`
	Tailnet string `json:"tailnet"`
	Active  bool   `json:"active"`
}

// Config represents the full application configuration
type Config struct {
	Accounts      []Account `json:"accounts"`
	SSHUsername   string    `json:"ssh_username,omitempty"`
	ConfigVersion string    `json:"config_version"`
}

// ConfigService handles configuration file management
type ConfigService struct {
	configPath string
}

// NewConfigService creates a new config service
func NewConfigService() *ConfigService {
	return &ConfigService{
		configPath: getConfigPath(),
	}
}

// getConfigPath returns the path to the configuration file
func getConfigPath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return constants.CONFIG_FILE_NAME
	}
	return filepath.Join(homeDir, constants.CONFIG_DIR_NAME, constants.CONFIG_FILE_NAME)
}

// Load reads the configuration from disk
func (cs *ConfigService) Load() (*Config, error) {
	data, err := os.ReadFile(cs.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{ConfigVersion: constants.CONFIG_VERSION}, nil
		}
		return nil, fmt.Errorf("failed to read config: %w", err)
	}
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		// Try migrating from old format
		migratedConfig, migrateErr := migrateOldConfig(data)
		if migrateErr != nil {
			return nil, fmt.Errorf("failed to parse config: %w", err)
		}
		return migratedConfig, nil
	}
	if config.ConfigVersion == "" {
		config.ConfigVersion = constants.CONFIG_VERSION
	}
	return &config, nil
}

// Save writes the configuration to disk
func (cs *ConfigService) Save(config *Config) error {
	dir := filepath.Dir(cs.configPath)
	if err := os.MkdirAll(dir, constants.DIR_PERMISSION); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	if err := os.WriteFile(cs.configPath, data, constants.FILE_PERMISSION); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}
	return nil
}

// AddOrUpdateAccount adds a new account or updates an existing one
func (cs *ConfigService) AddOrUpdateAccount(config *Config, account Account) {
	for i, acc := range config.Accounts {
		if acc.Name == account.Name {
			config.Accounts[i] = account
			return
		}
	}
	config.Accounts = append(config.Accounts, account)
}

// GetActiveAccount returns the currently active account
func (cs *ConfigService) GetActiveAccount(config *Config) *Account {
	for i := range config.Accounts {
		if config.Accounts[i].Active {
			return &config.Accounts[i]
		}
	}
	if len(config.Accounts) > 0 {
		return &config.Accounts[0]
	}
	return nil
}

// migrateOldConfig attempts to convert old-format config to new format
func migrateOldConfig(data []byte) (*Config, error) {
	lines := splitLines(string(data))
	var apiKey, tailnet, sshUsername string
	for _, line := range lines {
		key, val, ok := splitOnce(line, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case constants.OLD_CONFIG_KEY_API_KEY:
			apiKey = strings.TrimSpace(val)
		case constants.OLD_CONFIG_KEY_TAILNET:
			tailnet = strings.TrimSpace(val)
		case constants.OLD_CONFIG_KEY_SSH_USERNAME:
			sshUsername = strings.TrimSpace(val)
		}
	}
	if apiKey == "" {
		return nil, fmt.Errorf("no api_key found in old config format")
	}
	return &Config{
		Accounts: []Account{
			{
				Name:    "default",
				APIKey:  apiKey,
				Tailnet: tailnet,
				Active:  true,
			},
		},
		SSHUsername:   sshUsername,
		ConfigVersion: constants.CONFIG_VERSION,
	}, nil
}

// splitLines splits a string into non-empty lines
func splitLines(s string) []string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	return lines
}

// splitOnce splits a string on the first occurrence of sep
func splitOnce(s, sep string) (string, string, bool) {
	idx := strings.Index(s, sep)
	if idx < 0 {
		return "", "", false
	}
	return s[:idx], s[idx+len(sep):], true
}





