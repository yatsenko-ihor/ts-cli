package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yatsenko-ihor/ts-cli/client"
	"github.com/yatsenko-ihor/ts-cli/config"
	"github.com/yatsenko-ihor/ts-cli/scripts"
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
	inputCommand            // Typing an ad-hoc remote command (":")
	inputText               // Generic one-line prompt driven by textInput.onSubmit
)

// textInput holds the transient state for any single-line text input prompt.
type textInput struct {
	mode  inputMode // Which input is active (inputNone = no input active)
	value string    // Current text being typed
	label string    // Prompt title for inputText popups
	hint  string    // Optional helper line under the label
	// onSubmit receives the entered value for inputText prompts and returns
	// the next state; it may open another prompt (chained questions).
	onSubmit func(m model, value string) (tea.Model, tea.Cmd)
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

// runTab selects what the Run panel lists.
type runTab int

const (
	tabScripts  runTab = iota // Local scripts from ~/.ts-cli/scripts
	tabCommands               // Saved named commands (config.json)
	tabRecent                 // Recently run things for this machine
	tabCount
)

func (t runTab) String() string {
	switch t {
	case tabScripts:
		return "Scripts"
	case tabCommands:
		return "Commands"
	case tabRecent:
		return "Recent"
	}
	return "?"
}

// runPanel groups state for the Run panel (right-top) and Output panel (right-bottom).
type runPanel struct {
	visible bool
	tab     runTab
	cursors [tabCount]int // One cursor per tab

	scripts    []scripts.Script
	scriptsErr error
	commands   []config.SavedCommand
	history    *util.HistoryStore

	// Output of the current/last job
	lines     []string
	partial   string // trailing line without newline yet
	label     string // what is running / ran, for the Output title
	running   bool
	cancelled bool
	finished  bool
	exitCode  int
	startedAt time.Time
	elapsed   time.Duration
	job       *runJob // active job; nil when idle

	outputScroll int
	outputCursor int
	follow       bool // keep the cursor on the last line while output streams in
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
	sshActionCommand           // Run pending.command and stream output
	sshActionScript            // Stream pending.script to the machine and run it
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
	script          *scripts.Script
	args            []string // Script arguments for sshActionScript
	usedSavedPass   bool     // The attempt used a password loaded from config
	passwordTried   bool     // A TUI-entered password has already been tried
	askPasswordNext bool     // After the username prompt, go straight to the password prompt
}

// label describes the pending job for the Output title and history.
func (p pendingSSH) label() string {
	switch p.action {
	case sshActionScript:
		if p.script != nil {
			return p.script.Name
		}
	case sshActionCommand:
		return p.command
	}
	return ""
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
	secretStoreBusy    bool   // A keychain/file switch is in progress
	secretStoreMsg     string // Result of the last switch attempt
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
