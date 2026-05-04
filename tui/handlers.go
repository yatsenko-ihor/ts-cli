package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ihor/ts-cli/util"
)

// ─── Constants ────────────────────────────────────────────────────────────────

const (
	// keyFocusList / keyFocusHistory / keyFocusOutput are the keyboard shortcuts
	// that directly switch focus to the corresponding panel from any mode.
	keyFocusList    = "1"
	keyFocusHistory = "2"
	keyFocusOutput  = "3"

	// keyPasteAlt is the macOS-conventional paste shortcut.
	// On macOS most terminal emulators intercept cmd+v and deliver the clipboard
	// text as synthetic keystrokes, but some (WezTerm, Alacritty, etc.) forward
	// the raw key event so we register it here as well.
	keyPasteAlt = "cmd+v"
)

// ─── Command type ─────────────────────────────────────────────────────────────

// keyHandler is an alias for the action Command type defined in commands.go.
// It names the intent: a function that handles a specific key-press event.
type keyHandler = action

// dispatchKey looks up the handler registered for key in handlers and executes
// it. Returns (newModel, cmd, true) on a match, or (m, nil, false) when no
// handler is registered for the key.
func dispatchKey(key string, m model, handlers map[string]keyHandler) (tea.Model, tea.Cmd, bool) {
	if h, ok := handlers[key]; ok {
		newM, cmd := h(m)
		return newM, cmd, true
	}
	return m, nil, false
}

// ─── Shared commands ──────────────────────────────────────────────────────────

var (
	cmdQuit = keyHandler(func(m model) (tea.Model, tea.Cmd) {
		return m, tea.Quit
	})

	cmdMoveCursorUp = keyHandler(func(m model) (tea.Model, tea.Cmd) {
		m.moveCursorUp()
		return m, nil
	})

	cmdMoveCursorDown = keyHandler(func(m model) (tea.Model, tea.Cmd) {
		m.moveCursorDown()
		return m, nil
	})

	cmdTabForward = keyHandler(func(m model) (tea.Model, tea.Cmd) {
		if !m.showHistoryPanel {
			m.showHistoryPanel = true
			m.activeFocus = focusHistory
			m.historyCursor = 0
			m.outputScroll = 0
			m.outputCursor = 0
		} else {
			switch m.activeFocus {
			case focusList:
				m.activeFocus = focusHistory
				m.historyCursor = 0
			case focusHistory:
				m.activeFocus = focusOutput
				m.outputScroll = 0
				m.outputCursor = 0
			case focusOutput:
				m.activeFocus = focusList
			default:
				m.activeFocus = focusList
			}
		}
		return m, nil
	})

	cmdTabBackward = keyHandler(func(m model) (tea.Model, tea.Cmd) {
		if !m.showHistoryPanel {
			m.showHistoryPanel = true
			m.activeFocus = focusOutput
			m.historyCursor = 0
			m.outputScroll = 0
			m.outputCursor = 0
		} else {
			switch m.activeFocus {
			case focusList:
				m.activeFocus = focusOutput
				m.outputScroll = 0
				m.outputCursor = 0
			case focusHistory:
				m.activeFocus = focusList
			case focusOutput:
				m.activeFocus = focusHistory
				m.historyCursor = 0
			default:
				m.activeFocus = focusList
			}
		}
		return m, nil
	})
)

// ─── Username prompt mode ─────────────────────────────────────────────────────

var usernamePromptHandlers = map[string]keyHandler{
	"esc": func(m model) (tea.Model, tea.Cmd) {
		m.usernamePrompt = false
		m.usernameInput = ""
		return m, nil
	},
	"ctrl+c": func(m model) (tea.Model, tea.Cmd) {
		m.usernamePrompt = false
		m.usernameInput = ""
		return m, nil
	},
	"enter": func(m model) (tea.Model, tea.Cmd) {
		if m.usernameInput == "" {
			return m, nil
		}
		sanitized := util.SanitizeInput(m.usernameInput)
		if err := util.ValidateSSHUsername(sanitized); err != nil {
			m.usernameInput = ""
			return m, nil
		}
		m.sshUsername = sanitized
		m.usernamePrompt = false
		m.usernameInput = ""
		cmd := m.storeUsername(m.sshUsername)
		target := m.getTargetDevice()
		if target >= 0 && target < len(m.filteredDevices) {
			return m, tea.Batch(cmd, m.sshToDevice(target))
		}
		return m, cmd
	},
	"backspace": func(m model) (tea.Model, tea.Cmd) {
		if len(m.usernameInput) > 0 {
			m.usernameInput = m.usernameInput[:len(m.usernameInput)-1]
		}
		return m, nil
	},
	"ctrl+v": func(m model) (tea.Model, tea.Cmd) {
		return m, readClipboard(pasteTargetUsername)
	},
	keyPasteAlt: func(m model) (tea.Model, tea.Cmd) {
		return m, readClipboard(pasteTargetUsername)
	},
}

func (m model) handleUsernamePrompt(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if newM, cmd, handled := dispatchKey(msg.String(), m, usernamePromptHandlers); handled {
		return newM, cmd
	}
	// Bracketed paste: terminal wraps Cmd+V in escape sequences; bubbletea
	// delivers the full text as a single KeyMsg with Paste:true.
	if msg.Paste {
		m.usernameInput += sanitizePastedText(string(msg.Runes))
		return m, nil
	}
	// Append printable single-character input
	if len(msg.String()) == 1 {
		m.usernameInput += msg.String()
	}
	return m, nil
}

// ─── Search mode ──────────────────────────────────────────────────────────────

var searchModeHandlers = map[string]keyHandler{
	"esc": func(m model) (tea.Model, tea.Cmd) {
		m.searchMode = false
		m.searchQuery = ""
		m.filterDevices()
		return m, nil
	},
	"ctrl+c": func(m model) (tea.Model, tea.Cmd) {
		m.searchMode = false
		m.searchQuery = ""
		m.filterDevices()
		return m, nil
	},
	"enter": func(m model) (tea.Model, tea.Cmd) {
		m.searchMode = false
		return m, nil
	},
	"backspace": func(m model) (tea.Model, tea.Cmd) {
		if len(m.searchQuery) > 0 {
			m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
			m.filterDevices()
		}
		return m, nil
	},
	"ctrl+v": func(m model) (tea.Model, tea.Cmd) {
		return m, readClipboard(pasteTargetSearch)
	},
	keyPasteAlt: func(m model) (tea.Model, tea.Cmd) {
		return m, readClipboard(pasteTargetSearch)
	},
}

func (m model) handleSearchMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if newM, cmd, handled := dispatchKey(msg.String(), m, searchModeHandlers); handled {
		return newM, cmd
	}
	// Bracketed paste: terminal wraps Cmd+V in escape sequences; bubbletea
	// delivers the full text as a single KeyMsg with Paste:true.
	if msg.Paste {
		m.searchQuery += sanitizePastedText(string(msg.Runes))
		m.filterDevices()
		return m, nil
	}
	// Append printable single-character input
	if len(msg.String()) == 1 {
		m.searchQuery += msg.String()
		m.filterDevices()
	}
	return m, nil
}

// ─── Command input mode ───────────────────────────────────────────────────────

var commandModeHandlers = map[string]keyHandler{
	"esc": func(m model) (tea.Model, tea.Cmd) {
		m.commandMode = false
		m.commandInput = ""
		return m, nil
	},
	"ctrl+c": func(m model) (tea.Model, tea.Cmd) {
		m.commandMode = false
		m.commandInput = ""
		return m, nil
	},
	"enter": func(m model) (tea.Model, tea.Cmd) {
		if m.commandInput == "" {
			return m, nil
		}
		command := util.SanitizeInput(m.commandInput)
		m.commandMode = false
		m.commandInput = ""
		return m, m.executeRemoteCommand(command)
	},
	"backspace": func(m model) (tea.Model, tea.Cmd) {
		if len(m.commandInput) > 0 {
			m.commandInput = m.commandInput[:len(m.commandInput)-1]
		}
		return m, nil
	},
	"ctrl+v": func(m model) (tea.Model, tea.Cmd) {
		return m, readClipboard(pasteTargetCommand)
	},
	keyPasteAlt: func(m model) (tea.Model, tea.Cmd) {
		return m, readClipboard(pasteTargetCommand)
	},
}

func (m model) handleCommandMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if newM, cmd, handled := dispatchKey(msg.String(), m, commandModeHandlers); handled {
		return newM, cmd
	}
	// Bracketed paste: terminal wraps Cmd+V in escape sequences; bubbletea
	// delivers the full text as a single KeyMsg with Paste:true.
	if msg.Paste {
		m.commandInput += sanitizePastedText(string(msg.Runes))
		return m, nil
	}
	// Append printable single-character input
	if len(msg.String()) == 1 {
		m.commandInput += msg.String()
	}
	return m, nil
}

// ─── History navigation mode ──────────────────────────────────────────────────

// historyDeviceContext returns the machine ID and history commands for the
// currently-targeted device. valid is false when no device can be resolved.
func historyDeviceContext(m model) (machineID string, commands []string, valid bool) {
	target := m.getTargetDevice()
	if target < 0 || target >= len(m.filteredDevices) {
		return "", nil, false
	}
	device := m.filteredDevices[target]
	machineID = device.ID
	if machineID == "" {
		machineID = device.Hostname
	}
	if m.history != nil {
		commands = m.history.GetUniqueCommands(machineID)
	}
	return machineID, commands, true
}

var (
	cmdHistoryCursorUp = keyHandler(func(m model) (tea.Model, tea.Cmd) {
		if m.historyCursor > 0 {
			m.historyCursor--
		}
		return m, nil
	})

	cmdHistoryCursorDown = keyHandler(func(m model) (tea.Model, tea.Cmd) {
		_, cmds, ok := historyDeviceContext(m)
		if !ok {
			return m, nil
		}
		if m.historyCursor < len(cmds)-1 {
			m.historyCursor++
		}
		return m, nil
	})

	cmdHistoryDelete = keyHandler(func(m model) (tea.Model, tea.Cmd) {
		machineID, cmds, ok := historyDeviceContext(m)
		if !ok || m.history == nil || len(cmds) == 0 || m.historyCursor >= len(cmds) {
			return m, nil
		}
		selected := cmds[m.historyCursor]
		removed, err := m.history.DeleteCommandForMachine(machineID, selected)
		if err != nil {
			m.sshError = fmt.Errorf("failed to delete command from history: %w", err)
			return m, nil
		}
		if removed > 0 {
			updated := m.history.GetUniqueCommands(machineID)
			if len(updated) == 0 {
				m.historyCursor = 0
			} else if m.historyCursor >= len(updated) {
				m.historyCursor = len(updated) - 1
			}
		}
		return m, nil
	})

	cmdHistoryEnterCommandMode = keyHandler(func(m model) (tea.Model, tea.Cmd) {
		target := m.getTargetDevice()
		if target < 0 || target >= len(m.filteredDevices) {
			return m, nil
		}
		device := m.filteredDevices[target]
		if !isDeviceOnline(device) {
			deviceName := device.Name
			if deviceName == "" {
				deviceName = device.Hostname
			}
			m.sshError = fmt.Errorf("Machine \"%s\" is offline", deviceName)
			return m, nil
		}
		m.commandMode = true
		m.commandInput = ""
		m.sshError = nil
		return m, nil
	})

	cmdHistoryExecute = keyHandler(func(m model) (tea.Model, tea.Cmd) {
		_, cmds, ok := historyDeviceContext(m)
		if !ok || len(cmds) == 0 || m.historyCursor >= len(cmds) {
			return m, nil
		}
		return m, m.executeRemoteCommand(cmds[m.historyCursor])
	})
)

var historyNavHandlers = map[string]keyHandler{
	"tab": func(m model) (tea.Model, tea.Cmd) {
		m.activeFocus = focusOutput
		m.outputScroll = 0
		m.outputCursor = 0
		return m, nil
	},
	"shift+tab": func(m model) (tea.Model, tea.Cmd) {
		m.activeFocus = focusList
		return m, nil
	},
	"backtab": func(m model) (tea.Model, tea.Cmd) {
		m.activeFocus = focusList
		return m, nil
	},
	"esc": func(m model) (tea.Model, tea.Cmd) {
		m.showHistoryPanel = false
		m.activeFocus = focusList
		return m, nil
	},
	"ctrl+c": cmdQuit,
	"q":      cmdQuit,
	"up":     cmdHistoryCursorUp,
	"k":      cmdHistoryCursorUp,
	"down":   cmdHistoryCursorDown,
	"j":      cmdHistoryCursorDown,
	"d":      cmdHistoryDelete,
	"e":      cmdHistoryEnterCommandMode,
	"enter":  cmdHistoryExecute,
}

func (m model) handleHistoryNavigation(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if newM, cmd, handled := dispatchKey(msg.String(), m, historyNavHandlers); handled {
		return newM, cmd
	}
	return m, nil
}

// ─── Profile selection mode ───────────────────────────────────────────────────

var profileSelectHandlers = map[string]keyHandler{
	"esc": func(m model) (tea.Model, tea.Cmd) {
		m.profileSelectMode = false
		return m, nil
	},
	"ctrl+c": func(m model) (tea.Model, tea.Cmd) {
		m.profileSelectMode = false
		return m, nil
	},
	"q": func(m model) (tea.Model, tea.Cmd) {
		m.profileSelectMode = false
		return m, nil
	},
	"enter": func(m model) (tea.Model, tea.Cmd) {
		if m.profileCursor == profileAllAccountsIndex {
			m.selectedProfile = ""
		} else if m.profileCursor <= len(m.accounts) {
			m.selectedProfile = m.accounts[m.profileCursor-profileAccountOffset].Name
		}
		m.profileSelectMode = false
		m.filterDevices()
		return m, nil
	},
	"up": func(m model) (tea.Model, tea.Cmd) {
		if m.profileCursor > profileAllAccountsIndex {
			m.profileCursor--
		}
		return m, nil
	},
	"k": func(m model) (tea.Model, tea.Cmd) {
		if m.profileCursor > profileAllAccountsIndex {
			m.profileCursor--
		}
		return m, nil
	},
	"down": func(m model) (tea.Model, tea.Cmd) {
		// len(m.accounts) is the maximum valid cursor: accounts occupy slots 1..n
		if m.profileCursor < len(m.accounts) {
			m.profileCursor++
		}
		return m, nil
	},
	"j": func(m model) (tea.Model, tea.Cmd) {
		if m.profileCursor < len(m.accounts) {
			m.profileCursor++
		}
		return m, nil
	},
}

func (m model) handleProfileSelection(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if newM, cmd, handled := dispatchKey(msg.String(), m, profileSelectHandlers); handled {
		return newM, cmd
	}
	return m, nil
}

// ─── Account management mode ──────────────────────────────────────────────────

var accountManageHandlers = map[string]keyHandler{
	"esc": func(m model) (tea.Model, tea.Cmd) {
		m.accountManageMode = false
		return m, nil
	},
	"ctrl+c": func(m model) (tea.Model, tea.Cmd) {
		m.accountManageMode = false
		return m, nil
	},
	"q": func(m model) (tea.Model, tea.Cmd) {
		m.accountManageMode = false
		return m, nil
	},
	"enter": func(m model) (tea.Model, tea.Cmd) {
		m.accountManageMode = false
		if m.manageCursor == addAccountMenuIndex {
			return m, m.runAddAccount()
		}
		return m, nil
	},
	"up": func(m model) (tea.Model, tea.Cmd) {
		if m.manageCursor > addAccountMenuIndex {
			m.manageCursor--
		}
		return m, nil
	},
	"k": func(m model) (tea.Model, tea.Cmd) {
		if m.manageCursor > addAccountMenuIndex {
			m.manageCursor--
		}
		return m, nil
	},
	"down": func(m model) (tea.Model, tea.Cmd) {
		if m.manageCursor < accountManageOptionCount-1 {
			m.manageCursor++
		}
		return m, nil
	},
	"j": func(m model) (tea.Model, tea.Cmd) {
		if m.manageCursor < accountManageOptionCount-1 {
			m.manageCursor++
		}
		return m, nil
	},
}

func (m model) handleAccountManagement(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if newM, cmd, handled := dispatchKey(msg.String(), m, accountManageHandlers); handled {
		return newM, cmd
	}
	return m, nil
}

// ─── Normal mode ──────────────────────────────────────────────────────────────

var normalModeHandlers = map[string]keyHandler{
	"ctrl+c":    cmdQuit,
	"q":         cmdQuit,
	"tab":       cmdTabForward,
	"shift+tab": cmdTabBackward,
	"backtab":   cmdTabBackward,
	"up":        cmdMoveCursorUp,
	"k":         cmdMoveCursorUp,
	"down":      cmdMoveCursorDown,
	"j":         cmdMoveCursorDown,
	"/": func(m model) (tea.Model, tea.Cmd) {
		m.searchMode = true
		m.searchQuery = ""
		return m, nil
	},
	"c": func(m model) (tea.Model, tea.Cmd) {
		if m.showHistoryPanel && m.activeFocus == focusOutput {
			return m, m.copySelectedOutputItem(false)
		}
		target := m.getTargetDevice()
		if target >= 0 && target < len(m.filteredDevices) {
			return m, m.copySSHCommand(target)
		}
		return m, nil
	},
	"n": func(m model) (tea.Model, tea.Cmd) {
		if m.showHistoryPanel && m.activeFocus == focusOutput {
			return m, m.copySelectedOutputItem(true)
		}
		return m, nil
	},
	"s": func(m model) (tea.Model, tea.Cmd) {
		return m.handleSSHRequest()
	},
	"u": func(m model) (tea.Model, tea.Cmd) {
		return m, m.runTailscaleUp()
	},
	"m": func(m model) (tea.Model, tea.Cmd) {
		m.accountManageMode = true
		m.manageCursor = addAccountMenuIndex
		return m, nil
	},
	"r": func(m model) (tea.Model, tea.Cmd) {
		if m.reloading {
			return m, nil
		}
		m.reloading = true
		m.sshError = nil
		return m, m.reloadDevices()
	},
	"p": func(m model) (tea.Model, tea.Cmd) {
		m.profileSelectMode = true
		m.profileCursor = profileAllAccountsIndex
		for i, acc := range m.accounts {
			if acc.Name == m.selectedProfile {
				m.profileCursor = i + profileAccountOffset
				break
			}
		}
		return m, nil
	},
	"x": func(m model) (tea.Model, tea.Cmd) {
		m.showInstallSuggestion = false
		return m, nil
	},
	"d": func(m model) (tea.Model, tea.Cmd) {
		if m.sshUsername != "" {
			m.sshUsername = ""
			return m, m.clearUsername()
		}
		return m, nil
	},
}

func (m model) handleNormalMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.handleFrameShortcut(msg.String()) {
		return m, nil
	}
	if m.showHistoryPanel && m.activeFocus == focusHistory {
		return m.handleHistoryNavigation(msg)
	}
	if newM, cmd, handled := dispatchKey(msg.String(), m, normalModeHandlers); handled {
		return newM, cmd
	}
	return m, nil
}

// getTargetDevice returns the index of the currently-highlighted device.
func (m model) getTargetDevice() int {
	return m.cursor
}

// moveCursorUp moves the cursor up and adjusts viewport if needed
func (m *model) moveCursorUp() {
	// Handle different panels based on focus
	if m.showHistoryPanel {
		if m.activeFocus == focusHistory {
			// Scroll history list
			if m.historyCursor > 0 {
				m.historyCursor--
			}
			return
		} else if m.activeFocus == focusOutput {
			// Move selection in output panel
			lines := splitOutputLines(m.commandOutput)
			if len(lines) > 0 && m.outputCursor > 0 {
				m.outputCursor--
				m.ensureOutputCursorVisible()
			}
			return
		}
	}

	// Default: scroll device list
	if m.cursor > 0 {
		m.cursor--
		// Scroll up if cursor goes above viewport
		if m.cursor < m.viewportTop {
			m.viewportTop = m.cursor
		}
		// Clear SSH error when moving cursor
		m.sshError = nil
	}
}

// moveCursorDown moves the cursor down and adjusts viewport if needed
func (m *model) moveCursorDown() {
	// Handle different panels based on focus
	if m.showHistoryPanel {
		if m.activeFocus == focusHistory {
			// Scroll history list
			if m.history != nil && m.cursor >= 0 && m.cursor < len(m.filteredDevices) {
				device := m.filteredDevices[m.cursor]
				machineID := device.ID
				if machineID == "" {
					machineID = device.Hostname
				}
				historyCommands := m.history.GetUniqueCommands(machineID)
				if m.historyCursor < len(historyCommands)-1 {
					m.historyCursor++
				}
			}
			return
		} else if m.activeFocus == focusOutput {
			// Move selection in output panel
			lines := splitOutputLines(m.commandOutput)
			if len(lines) > 0 && m.outputCursor < len(lines)-1 {
				m.outputCursor++
				m.ensureOutputCursorVisible()
			}
			return
		}
	}

	// Default: scroll device list
	if m.cursor < len(m.filteredDevices)-1 {
		m.cursor++
		// Scroll down if cursor goes below viewport
		maxVisible := m.getMaxVisibleItems()
		if m.cursor >= m.viewportTop+maxVisible {
			m.viewportTop = m.cursor - maxVisible + 1
		}
		// Clear SSH error when moving cursor
		m.sshError = nil
	}
}

// handleSSHRequest handles the SSH request logic
func (m model) handleSSHRequest() (tea.Model, tea.Cmd) {
	target := m.getTargetDevice()
	if target < 0 || target >= len(m.filteredDevices) {
		return m, nil
	}

	device := m.filteredDevices[target]

	// Check if device is offline
	if !isDeviceOnline(device) {
		deviceName := device.Name
		if deviceName == "" {
			deviceName = device.Hostname
		}
		m.sshError = fmt.Errorf("Machine \"%s\" is offline", deviceName)
		return m, nil
	}

	// Check if Tailscale is running locally before attempting SSH
	if isRunning, message := checkLocalTailscaleStatus(); !isRunning {
		m.sshError = fmt.Errorf("Tailscale is not running locally: %s\nPress 'u' to run 'tailscale up' or start Tailscale manually", message)
		return m, nil
	}

	// Clear any previous SSH errors
	m.sshError = nil

	// Check if we need to switch accounts first
	// Compare device's account tailnet against the real Tailscale active account
	if device.AccountTailnet != "" && m.tailscaleActiveAccount != "" {
		// Normalize both for comparison (handle truncated accounts like "user@" vs "user@domain.com")
		deviceAccount := strings.ToLower(device.AccountTailnet)
		activeAccount := strings.ToLower(m.tailscaleActiveAccount)

		// Check if they're different accounts
		// Account in status might be truncated, so check if one starts with the other
		needsSwitch := !strings.HasPrefix(deviceAccount, strings.TrimSuffix(activeAccount, "@")) &&
			!strings.HasPrefix(activeAccount, strings.TrimSuffix(deviceAccount, "@"))

		if needsSwitch {
			// Need to switch Tailscale account before SSH
			return m, m.switchAccountForSSH(target, device.AccountTailnet)
		}
	}

	// Check if username is stored
	if m.sshUsername == "" {
		// Prompt for username
		m.usernamePrompt = true
		m.usernameInput = ""
		return m, nil
	}
	// Username exists, start SSH session
	return m, m.sshToDevice(target)
}
