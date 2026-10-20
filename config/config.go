// Package config is the single owner of ~/.ts-cli/config.json. Both the
// cobra commands and the TUI read and write the file through it, so the
// schema lives in one place and no writer can drop fields another writer
// relies on.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/yatsenko-ihor/ts-cli/util"
)

const (
	fileName       = "config.json"
	oldFileName    = "config" // pre-1.0 KEY=VALUE format
	CurrentVersion = "1.0"
)

// Account represents a Tailscale account configuration.
//
// APIKey is the usable, cleartext key in memory. On disk it is stored only
// as APIKeyEncrypted (AES-256-GCM with the master key, see util.Encrypt);
// the cleartext api_key field is still accepted when reading so files
// written by older versions keep working and are re-encrypted on the next
// save.
type Account struct {
	Name            string `json:"name"`                  // User-friendly name for the account
	APIKey          string `json:"api_key,omitempty"`     // Tailscale API key (cleartext; legacy on disk)
	APIKeyEncrypted string `json:"api_key_enc,omitempty"` // Tailscale API key, encrypted (on disk)
	Tailnet         string `json:"tailnet"`               // Tailnet name
	Active          bool   `json:"active"`                // Whether this is the active account
}

// MachineAuth stores per-machine SSH credentials. The map key is the stable
// Tailscale device ID (hostname as a fallback for devices without one);
// Name is kept for display only.
type MachineAuth struct {
	Name              string `json:"name,omitempty"`
	Username          string `json:"username,omitempty"`
	PasswordEncrypted string `json:"password,omitempty"`
}

// SavedCommand is a named remote command shown in the TUI's Run panel.
// Command may contain {{placeholders}} that are asked for before running.
type SavedCommand struct {
	Name    string `json:"name"`
	Command string `json:"command"`
}

// Config represents the application configuration
type Config struct {
	Accounts           []Account              `json:"accounts"`
	SSHUsername        string                 `json:"ssh_username,omitempty"`
	MachineCredentials map[string]MachineAuth `json:"machine_credentials,omitempty"`
	Commands           []SavedCommand         `json:"commands,omitempty"`
	UI                 map[string]string      `json:"ui,omitempty"`           // display settings (F12)
	LastMachine        string                 `json:"last_machine,omitempty"` // key of the machine connected to most recently
	ConfigVersion      string                 `json:"config_version"`
}

// mu serialises every read-modify-write of the file within this process.
var mu sync.Mutex

// Path returns the config file path, creating the directory (0700) if needed.
func Path() (string, error) {
	dir, err := util.EnsureConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fileName), nil
}

// Load reads the configuration from disk. A missing file yields an empty
// config; any other failure is returned so callers never overwrite a file
// they could not read.
func Load() (*Config, error) {
	mu.Lock()
	defer mu.Unlock()
	return load()
}

func load() (*Config, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if cfg, ok := migrateOldConfig(); ok {
			return cfg, nil
		}
		return &Config{Accounts: []Account{}, ConfigVersion: CurrentVersion}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", path, err)
	}
	if cfg.Accounts == nil {
		cfg.Accounts = []Account{}
	}
	if cfg.ConfigVersion == "" {
		cfg.ConfigVersion = CurrentVersion
	}

	// Decrypt API keys; migrate cleartext ones written by older versions.
	needsRewrite := false
	for i := range cfg.Accounts {
		acc := &cfg.Accounts[i]
		switch {
		case acc.APIKeyEncrypted != "":
			plain, err := util.Decrypt(acc.APIKeyEncrypted)
			if err != nil {
				return nil, fmt.Errorf("failed to decrypt API key for account %q (secret store: %s): %w",
					acc.Name, util.CurrentSecretStore(), err)
			}
			acc.APIKey = plain
		case acc.APIKey != "":
			needsRewrite = true
		}
	}
	if needsRewrite {
		// Best effort: a failure here must not block reading the config.
		_ = save(&cfg)
	}
	return &cfg, nil
}

// forDisk returns a copy of cfg with every API key encrypted and the
// cleartext field cleared.
func forDisk(cfg *Config) (*Config, error) {
	out := *cfg
	out.Accounts = make([]Account, len(cfg.Accounts))
	for i, acc := range cfg.Accounts {
		if acc.APIKey != "" {
			enc, err := util.Encrypt(acc.APIKey)
			if err != nil {
				return nil, fmt.Errorf("failed to encrypt API key for account %q: %w", acc.Name, err)
			}
			acc.APIKeyEncrypted = enc
		}
		acc.APIKey = ""
		out.Accounts[i] = acc
	}
	return &out, nil
}

// Save writes the configuration atomically (temp file + rename) with mode 0600.
func Save(cfg *Config) error {
	mu.Lock()
	defer mu.Unlock()
	return save(cfg)
}

func save(cfg *Config) error {
	path, err := Path()
	if err != nil {
		return err
	}
	if cfg.ConfigVersion == "" {
		cfg.ConfigVersion = CurrentVersion
	}

	disk, err := forDisk(cfg)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(disk, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), fileName+".*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp config: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }

	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		cleanup()
		return fmt.Errorf("failed to set config permissions: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		cleanup()
		return fmt.Errorf("failed to write config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("failed to write config: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("failed to replace config: %w", err)
	}
	return nil
}

// Update loads the config, applies fn and saves the result, all under the
// package lock. Use it for every small mutation (username, one credential)
// so concurrent writers cannot lose each other's changes.
func Update(fn func(*Config) error) error {
	mu.Lock()
	defer mu.Unlock()

	cfg, err := load()
	if err != nil {
		return err
	}
	if err := fn(cfg); err != nil {
		return err
	}
	return save(cfg)
}

// ─── Accounts ────────────────────────────────────────────────────────────────

// FindAccount returns the account whose Name or Tailnet equals nameOrTailnet.
func (c *Config) FindAccount(nameOrTailnet string) *Account {
	for i := range c.Accounts {
		if c.Accounts[i].Name == nameOrTailnet || c.Accounts[i].Tailnet == nameOrTailnet {
			return &c.Accounts[i]
		}
	}
	return nil
}

// AddAccount adds a new account or updates the one with the same tailnet.
func (c *Config) AddAccount(name, apiKey, tailnet string, setActive bool) {
	for i := range c.Accounts {
		if c.Accounts[i].Tailnet == tailnet {
			c.Accounts[i].Name = name
			c.Accounts[i].APIKey = apiKey
			if setActive {
				c.SetActiveAccount(tailnet)
			}
			return
		}
	}

	if setActive {
		for i := range c.Accounts {
			c.Accounts[i].Active = false
		}
	}
	c.Accounts = append(c.Accounts, Account{Name: name, APIKey: apiKey, Tailnet: tailnet, Active: setActive})
}

// RemoveAccount removes the account matching nameOrTailnet. If the active
// account was removed, the first remaining one becomes active. It reports
// whether anything was removed.
func (c *Config) RemoveAccount(nameOrTailnet string) bool {
	kept := make([]Account, 0, len(c.Accounts))
	removed := false
	for _, acc := range c.Accounts {
		if acc.Name == nameOrTailnet || acc.Tailnet == nameOrTailnet {
			removed = true
			continue
		}
		kept = append(kept, acc)
	}
	if !removed {
		return false
	}
	c.Accounts = kept
	if c.GetActiveAccount() == nil && len(c.Accounts) > 0 {
		c.Accounts[0].Active = true
	}
	return true
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

// SetActiveAccount marks the account matching nameOrTailnet as active and all
// others inactive. Nothing changes when no account matches.
func (c *Config) SetActiveAccount(nameOrTailnet string) bool {
	target := c.FindAccount(nameOrTailnet)
	if target == nil {
		return false
	}
	for i := range c.Accounts {
		c.Accounts[i].Active = &c.Accounts[i] == target
	}
	return true
}

// GetAllAccounts returns all configured accounts
func (c *Config) GetAllAccounts() []Account {
	return c.Accounts
}

// ─── Machine credentials ─────────────────────────────────────────────────────

// SetMachineCredential stores auth under key, creating the map if needed.
func (c *Config) SetMachineCredential(key string, auth MachineAuth) {
	if c.MachineCredentials == nil {
		c.MachineCredentials = make(map[string]MachineAuth)
	}
	c.MachineCredentials[key] = auth
}

// DeleteMachineCredential removes the credential stored under key.
func (c *Config) DeleteMachineCredential(key string) {
	delete(c.MachineCredentials, key)
}

// ─── Saved commands ──────────────────────────────────────────────────────────

// UpsertCommand adds a saved command or replaces the one with the same name.
// When oldName is non-empty, the command previously stored under that name is
// renamed/replaced instead (edit flow).
func (c *Config) UpsertCommand(oldName string, cmd SavedCommand) {
	target := oldName
	if target == "" {
		target = cmd.Name
	}
	for i := range c.Commands {
		if c.Commands[i].Name == target {
			c.Commands[i] = cmd
			return
		}
	}
	c.Commands = append(c.Commands, cmd)
}

// DeleteCommand removes the saved command with the given name.
func (c *Config) DeleteCommand(name string) bool {
	for i := range c.Commands {
		if c.Commands[i].Name == name {
			c.Commands = append(c.Commands[:i], c.Commands[i+1:]...)
			return true
		}
	}
	return false
}

// ─── Legacy migration ────────────────────────────────────────────────────────

// migrateOldConfig converts the pre-1.0 KEY=VALUE file into a Config, saves
// it, and renames the old file to *.backup.
func migrateOldConfig() (*Config, bool) {
	dir, err := util.ConfigDir()
	if err != nil {
		return nil, false
	}
	oldPath := filepath.Join(dir, oldFileName)
	data, err := os.ReadFile(oldPath)
	if err != nil {
		return nil, false
	}

	var apiKey, tailnet, sshUsername string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "TAILSCALE_API_KEY":
			apiKey = strings.TrimSpace(value)
		case "TAILNET":
			tailnet = strings.TrimSpace(value)
		case "SSH_USERNAME":
			sshUsername = strings.TrimSpace(value)
		}
	}
	if apiKey == "" || tailnet == "" {
		return nil, false
	}

	cfg := &Config{
		Accounts:      []Account{{Name: tailnet, APIKey: apiKey, Tailnet: tailnet, Active: true}},
		SSHUsername:   sshUsername,
		ConfigVersion: CurrentVersion,
	}
	if err := save(cfg); err != nil {
		return nil, false
	}
	if err := os.Rename(oldPath, oldPath+".backup"); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not rename old config %s: %v\n", oldPath, err)
	}
	return cfg, true
}
