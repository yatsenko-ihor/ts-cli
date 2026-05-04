package tui

import "github.com/ihor/ts-cli/client"

// Messages for Bubbletea event handling
// All message types used in the Update method are defined here

// sshMsg is returned when an SSH operation completes
type sshMsg struct {
	err error
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
	devices []client.Device
	err     error
}

// commandExecutedMsg is returned when a remote command completes
type commandExecutedMsg struct {
	output   string
	exitCode int
	err      error
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
)

// pasteMsg is returned when a clipboard read completes.
type pasteMsg struct {
	text   string
	target pasteTarget
	err    error
}

// panelFocus represents which panel currently has focus
type panelFocus int

const (
	focusList    panelFocus = iota
	focusHistory            // Focus on history panel
	focusOutput             // Focus on output panel
)
