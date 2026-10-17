package tui

import (
	"github.com/yatsenko-ihor/ts-cli/client"
	"github.com/yatsenko-ihor/ts-cli/util"
)

// Messages for Bubbletea event handling
// All message types used in the Update method are defined here

// sshMsg is returned when an SSH operation completes
type sshMsg struct {
	err     error
	failure sshFailure // classification of err (see classifySSHError)
}

// copiedMsg is returned when text is copied to clipboard
type copiedMsg struct {
	success bool
	text    string
}

// clearCopiedMsg is sent to clear the "copied" notification
type clearCopiedMsg struct{}

// clearReloadMsg is sent to clear the "reloaded" notification
type clearReloadMsg struct{}

// usernameStoredMsg is returned when username storage completes
type usernameStoredMsg struct {
	err error
}

// tailscaleUpMsg is returned when `tailscale up` completes
type tailscaleUpMsg struct {
	err error
}

// addAccountMsg is returned when adding a new account completes
type addAccountMsg struct {
	err error
}

// accountSwitchedMsg is returned when account switching completes
type accountSwitchedMsg struct {
	accountName    string
	deviceIndex    int
	err            error
	proceedWithSSH bool
}

// reloadMsg is returned when device list reload completes
type reloadMsg struct {
	devices  []client.Device
	warnings []error // per-account fetch failures (non-fatal)
	err      error
}

// commandExecutedMsg is returned when a remote command completes
type commandExecutedMsg struct {
	machineID   string // history key for the device the command ran on
	machineName string
	command     string
	output      string
	exitCode    int
	err         error
	failure     sshFailure // classification of err (see classifySSHError)
}

// pasteTarget identifies which input field a clipboard paste is destined for.
// Because clipboard reads are asynchronous, the target is embedded in the
// message so the correct field is updated even if the user has switched modes
// before the OS command returns.
type pasteTarget uint8

const (
	pasteTargetUsername pasteTarget = iota // usernameInput
	pasteTargetSearch                      // searchQuery
	pasteTargetCommand                     // commandInput
	pasteTargetPassword                    // passwordInput
)

// pasteMsg is returned when a clipboard read completes.
type pasteMsg struct {
	text   string
	target pasteTarget
	err    error
}

// machineCredsSavedMsg is returned when per-machine credentials are saved
type machineCredsSavedMsg struct {
	err error
}

// secretStoreChangedMsg is returned when the master key has been moved
// between the key file and the OS keychain (or the move failed).
type secretStoreChangedMsg struct {
	store  util.SecretStore // store in effect after the attempt
	target util.SecretStore // store the user asked for
	err    error
}

// panelFocus represents which panel currently has focus
type panelFocus int

const (
	focusList    panelFocus = iota
	focusHistory            // Focus on history panel
	focusOutput             // Focus on output panel
)
