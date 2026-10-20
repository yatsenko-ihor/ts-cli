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
	mode   inputMode // Which input is active (inputNone = no input active)
	value  string    // Current text being typed
	cursor int       // Cursor position in runes within value
	label  string    // Prompt title for inputText popups
	hint   string    // Optional helper line under the label
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
	searchQuery     string          // Persisted search filter
	selectedProfile string          // Active profile filter (empty = all)
	sort            sortMode        // Current sort mode
	searchCursor    int             // Cursor position (runes) inside searchQuery
	marked          map[string]bool // Machines selected for a multi-machine run, by machineKey
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

// runStatus is the lifecycle of one machine inside a run.
type runStatus int

const (
	runPending runStatus = iota // waiting for a free slot
	runRunning                  // ssh process alive
	runDone                     // process exited (exitCode tells how)
	runSkipped                  // not attempted (offline, no username, other account, os mismatch, stopped)
	runFailed                   // could not connect or authenticate
)

// machineRun is one machine's part of a run.
type machineRun struct {
	device    client.Device
	username  string
	password  string // resolved for this attempt, memory only
	job       *runJob
	lines     []string
	status    runStatus
	exitCode  int
	note      string // reason for runSkipped / runFailed
	killed    bool
	startedAt time.Time
	elapsed   time.Duration
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

	// The current/last run: one or many machines
	label     string // what is running / ran, for the Output title
	spec      sshJob // what was launched
	runs      []*machineRun
	multi     bool // several machines: no prompts, sectioned output
	startedAt time.Time
	elapsed   time.Duration

	outputScroll int
	outputCursor int
	follow       bool // keep the cursor on the last line while output streams in
}

// running reports whether any machine is still running or waiting.
func (r runPanel) running() bool {
	for _, mr := range r.runs {
		if mr.status == runRunning || mr.status == runPending {
			return true
		}
	}
	return false
}

// finished reports whether a run happened and every machine is settled.
func (r runPanel) finished() bool {
	return len(r.runs) > 0 && !r.running()
}

// single returns the only machineRun of a single-machine run, or nil.
func (r runPanel) single() *machineRun {
	if r.multi || len(r.runs) != 1 {
		return nil
	}
	return r.runs[0]
}

// runFor finds the machineRun owning job.
func (r runPanel) runFor(job *runJob) *machineRun {
	if job == nil {
		return nil
	}
	for _, mr := range r.runs {
		if mr.job == job {
			return mr
		}
	}
	return nil
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
	verified        bool     // A probe proved the credentials; a later session ending with an ssh error is not an auth failure
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
	info          string // neutral status line (e.g. ping progress/result)
}

// install groups PATH installation suggestion state.
type install struct {
	showSuggestion bool
	broken         bool
}
