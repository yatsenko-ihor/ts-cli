package util

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// CommandHistory represents a command executed on a machine
type CommandHistory struct {
	MachineID   string    `json:"machine_id"`   // Device ID or hostname
	MachineName string    `json:"machine_name"` // Human-readable name
	Command     string    `json:"command"`      // The command executed
	Timestamp   time.Time `json:"timestamp"`    // When it was executed
	ExitCode    int       `json:"exit_code"`    // Exit code of the command
	Output      string    `json:"output"`       // Command output (optional, can be limited)
}

// HistoryStore manages command history
type HistoryStore struct {
	filePath string
	commands []CommandHistory
}

// NewHistoryStore creates or loads a history store
func NewHistoryStore() (*HistoryStore, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	configDir := filepath.Join(homeDir, ".ts-cli")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	historyFile := filepath.Join(configDir, "history.json")

	store := &HistoryStore{
		filePath: historyFile,
		commands: []CommandHistory{},
	}

	// Load existing history if file exists
	if _, err := os.Stat(historyFile); err == nil {
		if err := store.load(); err != nil {
			return nil, err
		}
	}

	return store, nil
}

// load reads history from disk
func (h *HistoryStore) load() error {
	data, err := os.ReadFile(h.filePath)
	if err != nil {
		return fmt.Errorf("failed to read history file: %w", err)
	}

	if err := json.Unmarshal(data, &h.commands); err != nil {
		return fmt.Errorf("failed to parse history file: %w", err)
	}

	return nil
}

// Save writes history to disk
func (h *HistoryStore) Save() error {
	data, err := json.MarshalIndent(h.commands, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal history: %w", err)
	}

	if err := os.WriteFile(h.filePath, data, 0600); err != nil {
		return fmt.Errorf("failed to write history file: %w", err)
	}

	return nil
}

// AddCommand adds a new command to history
func (h *HistoryStore) AddCommand(machineID, machineName, command string, exitCode int, output string) {
	// Limit output to 1KB to keep history file manageable
	if len(output) > 1024 {
		output = output[:1024] + "... (truncated)"
	}

	entry := CommandHistory{
		MachineID:   machineID,
		MachineName: machineName,
		Command:     command,
		Timestamp:   time.Now(),
		ExitCode:    exitCode,
		Output:      output,
	}

	h.commands = append(h.commands, entry)

	// Keep only last 1000 commands
	if len(h.commands) > 1000 {
		h.commands = h.commands[len(h.commands)-1000:]
	}
}

// GetCommandsForMachine returns all commands for a specific machine
func (h *HistoryStore) GetCommandsForMachine(machineID string) []CommandHistory {
	var result []CommandHistory
	for _, cmd := range h.commands {
		if cmd.MachineID == machineID {
			result = append(result, cmd)
		}
	}
	return result
}

// GetAllCommands returns all commands in history
func (h *HistoryStore) GetAllCommands() []CommandHistory {
	return h.commands
}

// GetUniqueCommands returns unique commands for a machine (deduplicated)
func (h *HistoryStore) GetUniqueCommands(machineID string) []string {
	seen := make(map[string]bool)
	var unique []string

	// Iterate in reverse to get most recent unique commands first
	for i := len(h.commands) - 1; i >= 0; i-- {
		cmd := h.commands[i]
		if cmd.MachineID == machineID && !seen[cmd.Command] {
			seen[cmd.Command] = true
			unique = append(unique, cmd.Command)
		}
	}

	return unique
}

// DeleteCommandForMachine removes all matching command entries for a machine.
// Returns how many entries were removed.
func (h *HistoryStore) DeleteCommandForMachine(machineID, command string) (int, error) {
	if machineID == "" || command == "" {
		return 0, nil
	}

	filtered := make([]CommandHistory, 0, len(h.commands))
	removed := 0

	for _, entry := range h.commands {
		if entry.MachineID == machineID && entry.Command == command {
			removed++
			continue
		}
		filtered = append(filtered, entry)
	}

	if removed == 0 {
		return 0, nil
	}

	h.commands = filtered
	if err := h.Save(); err != nil {
		return 0, err
	}

	return removed, nil
}

// Clear removes all history
func (h *HistoryStore) Clear() error {
	h.commands = []CommandHistory{}
	return h.Save()
}
