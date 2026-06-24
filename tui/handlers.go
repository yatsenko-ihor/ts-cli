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
		if !m.hist.visible {
			m.hist.visible = true
			m.activeFocus = focusHistory
			m.hist.cursor = 0
			m.hist.outputScroll = 0
			m.hist.outputCursor = 0
		} else {
			switch m.activeFocus {
			case focusList:
				m.activeFocus = focusHistory
				m.hist.cursor = 0
			case focusHistory:
				m.activeFocus = focusOutput
				m.hist.outputScroll = 0
				m.hist.outputCursor = 0
			case focusOutput:
				m.activeFocus = focusList
			default:
				m.activeFocus = focusList
			}
		}
		return m, nil
	})

	cmdTabBackward = keyHandler(func(m model) (tea.Model, tea.Cmd) {
		if !m.hist.visible {
			m.hist.visible = true
			m.activeFocus = focusOutput
			m.hist.cursor = 0
			m.hist.outputScroll = 0
			m.hist.outputCursor = 0
		} else {
			switch m.activeFocus {
			case focusList:
				m.activeFocus = focusOutput
				m.hist.outputScroll = 0
				m.hist.outputCursor = 0
			case focusHistory:
				m.activeFocus = focusList
			case focusOutput:
				m.activeFocus = focusHistory
				m.hist.cursor = 0
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
		m.input = textInput{mode: inputNone}
		m.input.value = ""
		return m, nil
	},
	"ctrl+c": func(m model) (tea.Model, tea.Cmd) {
		m.input = textInput{mode: inputNone}
		m.input.value = ""
		return m, nil
	},
	"enter": func(m model) (tea.Model, tea.Cmd) {
		if m.input.value == "" {
			return m, nil
		}
		sanitized := util.SanitizeInput(m.input.value)
		if err := util.ValidateSSHUsername(sanitized); err != nil {
			m.input.value = ""
			return m, nil
		}
		m.ssh.username = sanitized
		m.input = textInput{mode: inputNone}
		m.input.value = ""
		cmd := m.storeUsername(m.ssh.username)
		target := m.getTargetDevice()
		if target >= 0 && target < len(m.list.filteredDevices) {
			// If password saving is enabled and no password saved yet, prompt for it
			if m.ssh.savePasswordEnabled && m.ssh.passwordEncrypted == "" {
				m.input = textInput{mode: inputPassword}
				m.input.value = ""
				return m, cmd
			}
			return m, tea.Batch(cmd, m.sshToDevice(target))
		}
		return m, cmd
	},
	"backspace": func(m model) (tea.Model, tea.Cmd) {
		if len(m.input.value) > 0 {
			m.input.value = m.input.value[:len(m.input.value)-1]
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
		m.input.value += sanitizePastedText(string(msg.Runes))
		return m, nil
	}
	// Append printable single-character input
	if len(msg.String()) == 1 {
		m.input.value += msg.String()
	}
	return m, nil
}

// ─── Search mode ──────────────────────────────────────────────────────────────

var searchModeHandlers = map[string]keyHandler{
	"esc": func(m model) (tea.Model, tea.Cmd) {
		m.input = textInput{mode: inputNone}
		m.list.searchQuery = ""
		m.filterDevices()
		return m, nil
	},
	"ctrl+c": func(m model) (tea.Model, tea.Cmd) {
		m.input = textInput{mode: inputNone}
		m.list.searchQuery = ""
		m.filterDevices()
		return m, nil
	},
	"enter": func(m model) (tea.Model, tea.Cmd) {
		m.input = textInput{mode: inputNone}
		return m, nil
	},
	"backspace": func(m model) (tea.Model, tea.Cmd) {
		if len(m.list.searchQuery) > 0 {
			m.list.searchQuery = m.list.searchQuery[:len(m.list.searchQuery)-1]
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
		m.list.searchQuery += sanitizePastedText(string(msg.Runes))
		m.filterDevices()
		return m, nil
	}
	// Append printable single-character input
	if len(msg.String()) == 1 {
		m.list.searchQuery += msg.String()
		m.filterDevices()
	}
	return m, nil
}

// ─── Command input mode ───────────────────────────────────────────────────────

var commandModeHandlers = map[string]keyHandler{
	"esc": func(m model) (tea.Model, tea.Cmd) {
		m.input = textInput{mode: inputNone}
		m.input.value = ""
		return m, nil
	},
	"ctrl+c": func(m model) (tea.Model, tea.Cmd) {
		m.input = textInput{mode: inputNone}
		m.input.value = ""
		return m, nil
	},
	"enter": func(m model) (tea.Model, tea.Cmd) {
		if m.input.value == "" {
			return m, nil
		}
		command := util.SanitizeInput(m.input.value)
		m.input = textInput{mode: inputNone}
		m.input.value = ""
		return m, m.executeRemoteCommand(command)
	},
	"backspace": func(m model) (tea.Model, tea.Cmd) {
		if len(m.input.value) > 0 {
			m.input.value = m.input.value[:len(m.input.value)-1]
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
		m.input.value += sanitizePastedText(string(msg.Runes))
		return m, nil
	}
	// Append printable single-character input
	if len(msg.String()) == 1 {
		m.input.value += msg.String()
	}
	return m, nil
}

// ─── History navigation mode ──────────────────────────────────────────────────

// historyDeviceContext returns the machine ID and history commands for the
// currently-targeted device. valid is false when no device can be resolved.
func historyDeviceContext(m model) (machineID string, commands []string, valid bool) {
	target := m.getTargetDevice()
	if target < 0 || target >= len(m.list.filteredDevices) {
		return "", nil, false
	}
	device := m.list.filteredDevices[target]
	machineID = device.ID
	if machineID == "" {
		machineID = device.Hostname
	}
	if m.hist.history != nil {
		commands = m.hist.history.GetUniqueCommands(machineID)
	}
	return machineID, commands, true
}

var (
	cmdHistoryCursorUp = keyHandler(func(m model) (tea.Model, tea.Cmd) {
		if m.hist.cursor > 0 {
			m.hist.cursor--
		}
		return m, nil
	})

	cmdHistoryCursorDown = keyHandler(func(m model) (tea.Model, tea.Cmd) {
		_, cmds, ok := historyDeviceContext(m)
		if !ok {
			return m, nil
		}
		if m.hist.cursor < len(cmds)-1 {
			m.hist.cursor++
		}
		return m, nil
	})

	cmdHistoryDelete = keyHandler(func(m model) (tea.Model, tea.Cmd) {
		machineID, cmds, ok := historyDeviceContext(m)
		if !ok || m.hist.history == nil || len(cmds) == 0 || m.hist.cursor >= len(cmds) {
			return m, nil
		}
		selected := cmds[m.hist.cursor]
		removed, err := m.hist.history.DeleteCommandForMachine(machineID, selected)
		if err != nil {
			m.notify.sshError = fmt.Errorf("failed to delete command from history: %w", err)
			return m, nil
		}
		if removed > 0 {
			updated := m.hist.history.GetUniqueCommands(machineID)
			if len(updated) == 0 {
				m.hist.cursor = 0
			} else if m.hist.cursor >= len(updated) {
				m.hist.cursor = len(updated) - 1
			}
		}
		return m, nil
	})

	cmdHistoryEnterCommandMode = keyHandler(func(m model) (tea.Model, tea.Cmd) {
		target := m.getTargetDevice()
		if target < 0 || target >= len(m.list.filteredDevices) {
			return m, nil
		}
		device := m.list.filteredDevices[target]
		if !isDeviceOnline(device) {
			deviceName := device.Name
			if deviceName == "" {
				deviceName = device.Hostname
			}
			m.notify.sshError = fmt.Errorf("Machine \"%s\" is offline", deviceName)
			return m, nil
		}
		m.input = textInput{mode: inputCommand}
		m.input.value = ""
		m.notify.sshError = nil
		return m, nil
	})

	cmdHistoryExecute = keyHandler(func(m model) (tea.Model, tea.Cmd) {
		_, cmds, ok := historyDeviceContext(m)
		if !ok || len(cmds) == 0 || m.hist.cursor >= len(cmds) {
			return m, nil
		}
		return m, m.executeRemoteCommand(cmds[m.hist.cursor])
	})
)

var historyNavHandlers = map[string]keyHandler{
	"tab": func(m model) (tea.Model, tea.Cmd) {
		m.activeFocus = focusOutput
		m.hist.outputScroll = 0
		m.hist.outputCursor = 0
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
		m.hist.visible = false
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
		m.acct.profileSelectMode = false
		return m, nil
	},
	"ctrl+c": func(m model) (tea.Model, tea.Cmd) {
		m.acct.profileSelectMode = false
		return m, nil
	},
	"q": func(m model) (tea.Model, tea.Cmd) {
		m.acct.profileSelectMode = false
		return m, nil
	},
	"enter": func(m model) (tea.Model, tea.Cmd) {
		if m.acct.profileCursor == profileAllAccountsIndex {
			m.list.selectedProfile = ""
		} else if m.acct.profileCursor <= len(m.acct.list) {
			m.list.selectedProfile = m.acct.list[m.acct.profileCursor-profileAccountOffset].Name
		}
		m.acct.profileSelectMode = false
		m.filterDevices()
		return m, nil
	},
	"up": func(m model) (tea.Model, tea.Cmd) {
		if m.acct.profileCursor > profileAllAccountsIndex {
			m.acct.profileCursor--
		}
		return m, nil
	},
	"k": func(m model) (tea.Model, tea.Cmd) {
		if m.acct.profileCursor > profileAllAccountsIndex {
			m.acct.profileCursor--
		}
		return m, nil
	},
	"down": func(m model) (tea.Model, tea.Cmd) {
		// len(m.acct.list) is the maximum valid cursor: accounts occupy slots 1..n
		if m.acct.profileCursor < len(m.acct.list) {
			m.acct.profileCursor++
		}
		return m, nil
	},
	"j": func(m model) (tea.Model, tea.Cmd) {
		if m.acct.profileCursor < len(m.acct.list) {
			m.acct.profileCursor++
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
		m.acct.accountManageMode = false
		return m, nil
	},
	"ctrl+c": func(m model) (tea.Model, tea.Cmd) {
		m.acct.accountManageMode = false
		return m, nil
	},
	"q": func(m model) (tea.Model, tea.Cmd) {
		m.acct.accountManageMode = false
		return m, nil
	},
	"enter": func(m model) (tea.Model, tea.Cmd) {
		m.acct.accountManageMode = false
		if m.acct.manageCursor == addAccountMenuIndex {
			return m, m.runAddAccount()
		}
		return m, nil
	},
	"up": func(m model) (tea.Model, tea.Cmd) {
		if m.acct.manageCursor > addAccountMenuIndex {
			m.acct.manageCursor--
		}
		return m, nil
	},
	"k": func(m model) (tea.Model, tea.Cmd) {
		if m.acct.manageCursor > addAccountMenuIndex {
			m.acct.manageCursor--
		}
		return m, nil
	},
	"down": func(m model) (tea.Model, tea.Cmd) {
		if m.acct.manageCursor < accountManageOptionCount-1 {
			m.acct.manageCursor++
		}
		return m, nil
	},
	"j": func(m model) (tea.Model, tea.Cmd) {
		if m.acct.manageCursor < accountManageOptionCount-1 {
			m.acct.manageCursor++
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
		m.input = textInput{mode: inputSearch}
		m.list.searchQuery = ""
		return m, nil
	},
	"c": func(m model) (tea.Model, tea.Cmd) {
		if m.hist.visible && m.activeFocus == focusOutput {
			return m, m.copySelectedOutputItem(false)
		}
		target := m.getTargetDevice()
		if target >= 0 && target < len(m.list.filteredDevices) {
			return m, m.copySSHCommand(target)
		}
		return m, nil
	},
	"n": func(m model) (tea.Model, tea.Cmd) {
		if m.hist.visible && m.activeFocus == focusOutput {
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
		m.acct.accountManageMode = true
		m.acct.manageCursor = addAccountMenuIndex
		return m, nil
	},
	"r": func(m model) (tea.Model, tea.Cmd) {
		if m.reloading {
			return m, nil
		}
		m.reloading = true
		m.notify.sshError = nil
		return m, m.reloadDevices()
	},
	"p": func(m model) (tea.Model, tea.Cmd) {
		m.acct.profileSelectMode = true
		m.acct.profileCursor = profileAllAccountsIndex
		for i, acc := range m.acct.list {
			if acc.Name == m.list.selectedProfile {
				m.acct.profileCursor = i + profileAccountOffset
				break
			}
		}
		return m, nil
	},
	"x": func(m model) (tea.Model, tea.Cmd) {
		m.inst.showSuggestion = false
		return m, nil
	},
	"d": func(m model) (tea.Model, tea.Cmd) {
		if m.ssh.username != "" {
			m.ssh.username = ""
			return m, m.clearUsername()
		}
		return m, nil
	},
	"o": func(m model) (tea.Model, tea.Cmd) {
		m.opts.active = true
		m.opts.cursor = 0
		return m, nil
	},
}

func (m model) handleNormalMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.handleFrameShortcut(msg.String()) {
		return m, nil
	}
	if m.hist.visible && m.activeFocus == focusHistory {
		return m.handleHistoryNavigation(msg)
	}
	if newM, cmd, handled := dispatchKey(msg.String(), m, normalModeHandlers); handled {
		return newM, cmd
	}
	return m, nil
}

// getTargetDevice returns the index of the currently-highlighted device.
func (m model) getTargetDevice() int {
	return m.list.cursor
}

// moveCursorUp moves the cursor up and adjusts viewport if needed
func (m *model) moveCursorUp() {
	// Handle different panels based on focus
	if m.hist.visible {
		if m.activeFocus == focusHistory {
			// Scroll history list
			if m.hist.cursor > 0 {
				m.hist.cursor--
			}
			return
		} else if m.activeFocus == focusOutput {
			// Move selection in output panel
			lines := splitOutputLines(m.hist.commandOutput)
			if len(lines) > 0 && m.hist.outputCursor > 0 {
				m.hist.outputCursor--
				m.ensureOutputCursorVisible()
			}
			return
		}
	}

	// Default: scroll device list
	if m.list.cursor > 0 {
		m.list.cursor--
		// Scroll up if cursor goes above viewport
		if m.list.cursor < m.list.viewportTop {
			m.list.viewportTop = m.list.cursor
		}
		// Clear SSH error when moving cursor
		m.notify.sshError = nil
	}
}

// moveCursorDown moves the cursor down and adjusts viewport if needed
func (m *model) moveCursorDown() {
	// Handle different panels based on focus
	if m.hist.visible {
		if m.activeFocus == focusHistory {
			// Scroll history list
			if m.hist.history != nil && m.list.cursor >= 0 && m.list.cursor < len(m.list.filteredDevices) {
				device := m.list.filteredDevices[m.list.cursor]
				machineID := device.ID
				if machineID == "" {
					machineID = device.Hostname
				}
				historyCommands := m.hist.history.GetUniqueCommands(machineID)
				if m.hist.cursor < len(historyCommands)-1 {
					m.hist.cursor++
				}
			}
			return
		} else if m.activeFocus == focusOutput {
			// Move selection in output panel
			lines := splitOutputLines(m.hist.commandOutput)
			if len(lines) > 0 && m.hist.outputCursor < len(lines)-1 {
				m.hist.outputCursor++
				m.ensureOutputCursorVisible()
			}
			return
		}
	}

	// Default: scroll device list
	if m.list.cursor < len(m.list.filteredDevices)-1 {
		m.list.cursor++
		// Scroll down if cursor goes below viewport
		maxVisible := m.getMaxVisibleItems()
		if m.list.cursor >= m.list.viewportTop+maxVisible {
			m.list.viewportTop = m.list.cursor - maxVisible + 1
		}
		// Clear SSH error when moving cursor
		m.notify.sshError = nil
	}
}

// handleSSHRequest handles the SSH request logic
func (m model) handleSSHRequest() (tea.Model, tea.Cmd) {
	target := m.getTargetDevice()
	if target < 0 || target >= len(m.list.filteredDevices) {
		return m, nil
	}

	device := m.list.filteredDevices[target]

	// Check if device is offline
	if !isDeviceOnline(device) {
		deviceName := device.Name
		if deviceName == "" {
			deviceName = device.Hostname
		}
		m.notify.sshError = fmt.Errorf("Machine \"%s\" is offline", deviceName)
		return m, nil
	}

	// Check if Tailscale is running locally before attempting SSH
	if isRunning, message := checkLocalTailscaleStatus(); !isRunning {
		m.notify.sshError = fmt.Errorf("Tailscale is not running locally: %s\nPress 'u' to run 'tailscale up' or start Tailscale manually", message)
		return m, nil
	}

	// Clear any previous SSH errors
	m.notify.sshError = nil

	// Check if we need to switch accounts first
	// Compare device's account tailnet against the real Tailscale active account
	if device.AccountTailnet != "" && m.acct.tailscaleActiveAccount != "" {
		// Normalize both for comparison (handle truncated accounts like "user@" vs "user@domain.com")
		deviceAccount := strings.ToLower(device.AccountTailnet)
		activeAccount := strings.ToLower(m.acct.tailscaleActiveAccount)

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
	if m.ssh.username == "" {
		// Prompt for username
		m.input = textInput{mode: inputUsername}
		m.input.value = ""
		return m, nil
	}
	// Username exists, start SSH session
	return m, m.sshToDevice(target)
}

// ─── Options menu constants ───────────────────────────────────────────────────

const (
	optionSavePassword  = 0 // Toggle save-password feature
	optionClearPassword = 1 // Clear saved password
	optionCount         = 2 // Total number of options
)

// ─── Options menu mode ────────────────────────────────────────────────────────

var optionsMenuHandlers = map[string]keyHandler{
	"esc": func(m model) (tea.Model, tea.Cmd) {
		m.opts.active = false
		return m, nil
	},
	"ctrl+c": func(m model) (tea.Model, tea.Cmd) {
		m.opts.active = false
		return m, nil
	},
	"q": func(m model) (tea.Model, tea.Cmd) {
		m.opts.active = false
		return m, nil
	},
	"up": func(m model) (tea.Model, tea.Cmd) {
		if m.opts.cursor > 0 {
			m.opts.cursor--
		}
		return m, nil
	},
	"k": func(m model) (tea.Model, tea.Cmd) {
		if m.opts.cursor > 0 {
			m.opts.cursor--
		}
		return m, nil
	},
	"down": func(m model) (tea.Model, tea.Cmd) {
		if m.opts.cursor < optionCount-1 {
			m.opts.cursor++
		}
		return m, nil
	},
	"j": func(m model) (tea.Model, tea.Cmd) {
		if m.opts.cursor < optionCount-1 {
			m.opts.cursor++
		}
		return m, nil
	},
	"enter": func(m model) (tea.Model, tea.Cmd) {
		switch m.opts.cursor {
		case optionSavePassword:
			m.ssh.savePasswordEnabled = !m.ssh.savePasswordEnabled
			if !m.ssh.savePasswordEnabled {
				// When disabling, also clear saved password
				m.ssh.passwordEncrypted = ""
			}
			return m, m.toggleSavePassword(m.ssh.savePasswordEnabled)
		case optionClearPassword:
			if m.ssh.passwordEncrypted != "" {
				m.ssh.passwordEncrypted = ""
				return m, m.clearSavedPassword()
			}
			return m, nil
		}
		return m, nil
	},
}

func (m model) handleOptionsMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if newM, cmd, handled := dispatchKey(msg.String(), m, optionsMenuHandlers); handled {
		return newM, cmd
	}
	return m, nil
}

// ─── Password prompt mode ─────────────────────────────────────────────────────

var passwordPromptHandlers = map[string]keyHandler{
	"esc": func(m model) (tea.Model, tea.Cmd) {
		m.input = textInput{mode: inputNone}
		m.input.value = ""
		return m, nil
	},
	"ctrl+c": func(m model) (tea.Model, tea.Cmd) {
		m.input = textInput{mode: inputNone}
		m.input.value = ""
		return m, nil
	},
	"enter": func(m model) (tea.Model, tea.Cmd) {
		if m.input.value == "" {
			return m, nil
		}
		password := m.input.value
		m.input = textInput{mode: inputNone}
		m.input.value = ""
		storeCmd := m.storePassword(password)
		// Proceed with SSH after storing password
		target := m.getTargetDevice()
		if target >= 0 && target < len(m.list.filteredDevices) {
			return m, tea.Batch(storeCmd, m.sshToDevice(target))
		}
		return m, storeCmd
	},
	"backspace": func(m model) (tea.Model, tea.Cmd) {
		if len(m.input.value) > 0 {
			m.input.value = m.input.value[:len(m.input.value)-1]
		}
		return m, nil
	},
	"ctrl+v": func(m model) (tea.Model, tea.Cmd) {
		return m, readClipboard(pasteTargetPassword)
	},
	keyPasteAlt: func(m model) (tea.Model, tea.Cmd) {
		return m, readClipboard(pasteTargetPassword)
	},
}

func (m model) handlePasswordPrompt(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if newM, cmd, handled := dispatchKey(msg.String(), m, passwordPromptHandlers); handled {
		return newM, cmd
	}
	if msg.Paste {
		m.input.value += sanitizePastedText(string(msg.Runes))
		return m, nil
	}
	if len(msg.String()) == 1 {
		m.input.value += msg.String()
	}
	return m, nil
}
