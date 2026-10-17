package util

import (
	"fmt"
	"os"
	"path/filepath"
)

// ConfigDirEnv overrides the configuration directory (used by tests and
// for running several isolated profiles).
const ConfigDirEnv = "TS_CLI_CONFIG_DIR"

// ConfigDir returns the directory that holds config.json, history.json and
// the encryption key: $TS_CLI_CONFIG_DIR if set, otherwise ~/.ts-cli.
// The directory is not created here.
func ConfigDir() (string, error) {
	if dir := os.Getenv(ConfigDirEnv); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(home, ".ts-cli"), nil
}

// EnsureConfigDir returns ConfigDir after creating it with mode 0700.
func EnsureConfigDir() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("failed to create config directory: %w", err)
	}
	return dir, nil
}
