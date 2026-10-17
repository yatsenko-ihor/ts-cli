package tui

import (
	"github.com/yatsenko-ihor/ts-cli/client"
	"github.com/yatsenko-ihor/ts-cli/util"
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
	searchQuery     string   // Persisted search filter
	selectedProfile string   // Active profile filter (empty = all)
	sort            sortMode // Current sort mode
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
	username     string                       // Default username (from config)
	machineCreds map[string]machineCredential // Per-machine saved credentials, keyed by device ID
	credsDirty   bool                         // machineCreds were migrated and must be persisted
	pending      pendingSSH                   // The connection attempt in progress
	retryMode    bool                         // Show retry menu after SSH failure
	retryCursor  int                          // Cursor position in retry menu
}

// sshAction says what a pending SSH attempt should do once credentials are known.
type sshAction int

const (
	sshActionNone    sshAction = iota
	sshActionSession           // Interactive ssh session
	sshActionCommand           // Run pending.command and capture output
)

// pendingSSH tracks a single connection attempt from key press to outcome.
// Nothing in it is persisted until the attempt succeeds.
type pendingSSH struct {
	action          sshAction
	deviceIndex     int
	machineID       string // Credential/history key (device ID)
	machineName     string // Display name
	username        string // Username used for this attempt
	password        string // Password entered in the TUI for this attempt (plaintext, memory only)
	command         string // Remote command for sshActionCommand
	usedSavedPass   bool   // The attempt used a password loaded from config
	passwordTried   bool   // A TUI-entered password has already been tried
	askPasswordNext bool   // After the username prompt, go straight to the password prompt
}

// machineCredential holds per-machine SSH credentials in memory
type machineCredential struct {
	name              string // Display name of the device
	username          string
	passwordEncrypted string
}

// options groups the options menu state.
type options struct {
	active             bool
	cursor             int
	showMachineCreds   bool // Sub-view: list of machines with saved credentials
	machineCredsCursor int
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
