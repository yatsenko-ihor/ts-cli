package tui

import (
	"github.com/ihor/ts-cli/client"
	"github.com/ihor/ts-cli/util"
)

// ─── Sub-state structs ────────────────────────────────────────────────────────
// The main model composes these focused state groups to keep related fields
// together and reduce cognitive load. Fields are accessed via embedding
// (e.g., m.list.cursor) so existing handler code uses clear, scoped names.

// inputMode identifies which text input is currently active.
type inputMode int

const (
	inputNone     inputMode = iota
	inputSearch             // Typing a search query
	inputUsername           // Typing SSH username
	inputPassword           // Typing SSH password
	inputCommand            // Typing a remote command
)

// textInput holds the transient state for any single-line text input prompt.
type textInput struct {
	mode  inputMode // Which input is active (inputNone = no input active)
	value string    // Current text being typed
}

// deviceList groups all state related to the device list panel.
type deviceList struct {
	devices         []client.Device
	filteredDevices []client.Device
	cursor          int
	selected        int
	viewportTop     int
	searchQuery     string // Persisted search filter
	selectedProfile string // Active profile filter (empty = all)
}

// historyPanel groups state for the command history + output panels.
type historyPanel struct {
	visible       bool
	cursor        int
	history       *util.HistoryStore
	commandOutput string
	outputScroll  int
	outputCursor  int
}

// accounts groups account and profile selection state.
type accounts struct {
	list                   []client.AccountInfo
	activeAccount          string // From config
	tailscaleActiveAccount string // From Tailscale daemon
	profileSelectMode      bool
	profileCursor          int
	accountManageMode      bool
	manageCursor           int
}

// ssh groups SSH connection related state.
type ssh struct {
	username            string
	passwordEncrypted   string
	savePasswordEnabled bool
	pendingPassword     string // Plaintext password awaiting successful SSH before saving
}

// options groups the options menu state.
type options struct {
	active bool
	cursor int
}

// notifications groups transient UI messages.
type notifications struct {
	copiedText    string
	reloadSuccess string
	sshError      error
}

// install groups PATH installation suggestion state.
type install struct {
	showSuggestion bool
	broken         bool
}
