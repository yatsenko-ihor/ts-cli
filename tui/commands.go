package tui

import tea "github.com/charmbracelet/bubbletea"

// action is the Command type — a key-triggered update to the model state.
// Each action receives the current model and returns the updated state
// together with any asynchronous side-effect command (bubbletea Cmd).
type action func(m model) (tea.Model, tea.Cmd)

// Profile selector menu constants.
const (
	// profileAllAccountsIndex is the cursor index for the "All Accounts" entry.
	profileAllAccountsIndex = 0
	// profileAccountOffset is added to an account's slice index to get its
	// profile-cursor index (shifted by one to make room for "All Accounts").
	profileAccountOffset = 1
)

// Account management menu constants.
