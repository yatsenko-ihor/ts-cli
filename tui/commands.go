package tui

import tea "github.com/charmbracelet/bubbletea"

// action is the Command type — a key-triggered update to the model state.
// Each action receives the current model and returns the updated state
// together with any asynchronous side-effect command (bubbletea Cmd).
type action func(m model) (tea.Model, tea.Cmd)

// keyDispatcher implements the Command pattern: it maps key strings to
// actions and routes incoming key events to the appropriate command.
type keyDispatcher struct {
	bindings  map[string]action
	charInput func(m model, char string) (tea.Model, tea.Cmd)
}

// newDispatcher creates a keyDispatcher with the given key→action bindings.
func newDispatcher(bindings map[string]action) *keyDispatcher {
	return &keyDispatcher{bindings: bindings}
}

// withCharInput attaches a fallback handler for single-character key presses,
// used for text-entry modes (username prompt, search, command input).
func (d *keyDispatcher) withCharInput(fn func(m model, char string) (tea.Model, tea.Cmd)) *keyDispatcher {
	d.charInput = fn
	return d
}

// dispatch routes key to its registered action.
// If no exact binding matches and the key is a single printable character,
// the charInput fallback is called instead.
func (d *keyDispatcher) dispatch(m model, key string) (tea.Model, tea.Cmd) {
	if a, ok := d.bindings[key]; ok {
		return a(m)
	}
	if d.charInput != nil && len([]rune(key)) == 1 {
		return d.charInput(m, key)
	}
	return m, nil
}

// Profile selector menu constants.
const (
	// profileAllAccountsIndex is the cursor index for the "All Accounts" entry.
	profileAllAccountsIndex = 0
	// profileAccountOffset is added to an account's slice index to get its
	// profile-cursor index (shifted by one to make room for "All Accounts").
	profileAccountOffset = 1
)

// Account management menu constants.
const (
	// accountManageOptionCount is the number of items in the account management menu.
	accountManageOptionCount = 1
	// addAccountMenuIndex is the cursor index for the "Add Account" option.
	addAccountMenuIndex = 0
)
