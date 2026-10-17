package util

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Kinds of history entries.
const (
	HistoryKindCommand = "command" // ad-hoc or saved shell command
	HistoryKindScript  = "script"  // local script streamed to the machine
)

// CommandHistory represents a command executed on a machine
type CommandHistory struct {
	MachineID   string    `json:"machine_id"`       // Device ID or hostname
	MachineName string    `json:"machine_name"`     // Human-readable name
	Kind        string    `json:"kind,omitempty"`   // HistoryKindCommand (default) or HistoryKindScript
	Command     string    `json:"command"`          // The command executed, or the script's display name
	Script      string    `json:"script,omitempty"` // Script file name for HistoryKindScript
	Args        []string  `json:"args,omitempty"`   // Script arguments
	Timestamp   time.Time `json:"timestamp"`        // When it was executed
	ExitCode    int       `json:"exit_code"`        // Exit code of the command
	Output      string    `json:"output"`           // Command output (optional, can be limited)
}

// Key identifies "the same thing to run again" for de-duplication.
func (h CommandHistory) Key() string {
	if h.Kind == HistoryKindScript {
		return "script:" + h.Script
	}
	return "command:" + h.Command
}

// HistoryStore manages command history
type HistoryStore struct {
	filePath string
	commands []CommandHistory
}

// NewHistoryStore creates or loads a history store
func NewHistoryStore() (*HistoryStore, error) {
	configDir, err := EnsureConfigDir()
	if err != nil {
		return nil, err
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

// Add appends an entry, truncating its output and capping the store size.
func (h *HistoryStore) Add(entry CommandHistory) {
	// Limit output to 1KB to keep history file manageable
	if len(entry.Output) > 1024 {
		entry.Output = entry.Output[:1024] + "... (truncated)"
	}
	if entry.Kind == "" {
		entry.Kind = HistoryKindCommand
	}
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}

	h.commands = append(h.commands, entry)

	// Keep only last 1000 commands
	if len(h.commands) > 1000 {
		h.commands = h.commands[len(h.commands)-1000:]
	}
}

// AddCommand adds a plain command to history
func (h *HistoryStore) AddCommand(machineID, machineName, command string, exitCode int, output string) {
	h.Add(CommandHistory{
		MachineID:   machineID,
		MachineName: machineName,
		Kind:        HistoryKindCommand,
		Command:     command,
		ExitCode:    exitCode,
		Output:      output,
	})
}

// GetCommandsForMachine returns all entries for a specific machine
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

// Recent returns the most recent distinct entries for a machine, newest
// first, at most limit (0 = no limit).
func (h *HistoryStore) Recent(machineID string, limit int) []CommandHistory {
	seen := make(map[string]bool)
	var out []CommandHistory
	for i := len(h.commands) - 1; i >= 0; i-- {
		e := h.commands[i]
		if e.MachineID != machineID {
			continue
		}
		k := e.Key()
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, e)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// GetUniqueCommands returns unique command strings for a machine (most recent first).
func (h *HistoryStore) GetUniqueCommands(machineID string) []string {
	var unique []string
	for _, e := range h.Recent(machineID, 0) {
		if e.Kind == HistoryKindCommand || e.Kind == "" {
			unique = append(unique, e.Command)
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
	return h.DeleteEntries(machineID, CommandHistory{Kind: HistoryKindCommand, Command: command}.Key())
}

// DeleteEntries removes every entry for machineID whose Key() equals key.
func (h *HistoryStore) DeleteEntries(machineID, key string) (int, error) {
	filtered := make([]CommandHistory, 0, len(h.commands))
	removed := 0

	for _, entry := range h.commands {
		if entry.MachineID == machineID && entry.Key() == key {
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
