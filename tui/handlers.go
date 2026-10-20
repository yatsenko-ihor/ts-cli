package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yatsenko-ihor/ts-cli/util"
)

// ─── Constants ────────────────────────────────────────────────────────────────

const (
	// keyFocusList / keyFocusRun / keyFocusOutput are the keyboard shortcuts
	// that directly switch focus to the corresponding panel from any mode.
	keyFocusList   = "1"
	keyFocusRun    = "2"
	keyFocusOutput = "3"

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
		if !m.run.visible {
			return m.openRunPanel(focusRun), nil
		}
		switch m.activeFocus {
		case focusList:
			m.activeFocus = focusRun
		case focusRun:
			m.activeFocus = focusOutput
		default:
			m.activeFocus = focusList
		}
		return m, nil
	})

	cmdTabBackward = keyHandler(func(m model) (tea.Model, tea.Cmd) {
		if !m.run.visible {
			return m.openRunPanel(focusOutput), nil
		}
		switch m.activeFocus {
		case focusList:
			m.activeFocus = focusOutput
		case focusOutput:
			m.activeFocus = focusRun
		default:
			m.activeFocus = focusList
		}
		return m, nil
	})
)

// ─── Generic text prompt (inputText) ──────────────────────────────────────────

func (m model) handleTextPrompt(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		cancel := m.input.onCancel
		m.input = textInput{mode: inputNone}
		if cancel != nil {
			m = cancel(m)
		}
		return m, nil
	case "enter":
		submit := m.input.onSubmit
		value := m.input.value
		if submit == nil {
			m.input = textInput{mode: inputNone}
			return m, nil
		}
		return submit(m, value)
	case "ctrl+v", keyPasteAlt:
		return m, readClipboard(pasteTargetText)
	}
	m.editInput(msg)
	return m, nil
}

// ─── Username prompt mode ─────────────────────────────────────────────────────

var usernamePromptHandlers = map[string]keyHandler{
	"esc": func(m model) (tea.Model, tea.Cmd) {
		return m.cancelSSHAttempt(), nil
	},
	"ctrl+c": func(m model) (tea.Model, tea.Cmd) {
		return m.cancelSSHAttempt(), nil
	},
	"enter": func(m model) (tea.Model, tea.Cmd) {
		if m.input.value == "" {
			return m, nil
		}
		sanitized := util.SanitizeInput(m.input.value)
		if err := util.ValidateSSHUsername(sanitized); err != nil {
			m.input.value = ""
			m.notify.sshError = err
			return m, nil
		}
		m.notify.sshError = nil
		m.input = textInput{mode: inputNone}
		// Try with this username (key-based first); the flow asks for a
		// password only if that fails.
		m.ssh.pending.username = sanitized
		return m.continueSSH()
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
	// Cursor movement, word-wise Option/Alt+arrows, deletions, typing, paste.
	m.editInput(msg)
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
	v, c, handled := editLine(m.list.searchQuery, m.list.searchCursor, msg)
	if handled {
		changed := v != m.list.searchQuery
		m.list.searchQuery, m.list.searchCursor = v, c
		if changed {
			m.filterDevices()
		}
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
		return m.startCommand(command)
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
	// Cursor movement, word-wise Option/Alt+arrows, deletions, typing, paste.
	m.editInput(msg)
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
		m.list.searchCursor = 0
		return m, nil
	},
	"c": func(m model) (tea.Model, tea.Cmd) {
		target := m.getTargetDevice()
		if target >= 0 && target < len(m.list.filteredDevices) {
			return m, m.copySSHCommand(target)
		}
		return m, nil
	},
	":": func(m model) (tea.Model, tea.Cmd) {
		return m.openAdHocPrompt()
	},
	" ": func(m model) (tea.Model, tea.Cmd) {
		if d, ok := m.targetDevice(); ok {
			if !m.list.marked[machineKey(d)] && !m.isReachable(d) {
				m.notify.sshError = fmt.Errorf("machine %q is unreachable; press p to ping it", displayName(d))
				return m, nil
			}
			m.toggleMark(d)
		}
		return m, nil
	},
	"p": func(m model) (tea.Model, tea.Cmd) {
		return m.pingTargets()
	},
	"H": func(m model) (tea.Model, tea.Cmd) {
		return m.applySetting(uiHideOffline)
	},
	"f12": func(m model) (tea.Model, tea.Cmd) {
		m.settingsMode = true
		m.settingsCursor = 0
		return m, nil
	},
	"*": func(m model) (tea.Model, tea.Cmd) {
		m.toggleAllMarks()
		return m, nil
	},
	"esc": func(m model) (tea.Model, tea.Cmd) {
		if len(m.list.marked) > 0 {
			m.clearMarks()
		} else if m.run.visible {
			m = m.closeRunPanel()
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
		m.acct.manageCursor = 0
		m.acct.confirmDelete = ""
		m.acct.status = ""
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
	"P": func(m model) (tea.Model, tea.Cmd) {
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
			return m, clearUsername()
		}
		return m, nil
	},
	"o": func(m model) (tea.Model, tea.Cmd) {
		m.opts.active = true
		m.opts.cursor = 0
		return m, nil
	},
	"a": func(m model) (tea.Model, tea.Cmd) {
		m.aboutMode = true
		return m, nil
	},
	"f1": func(m model) (tea.Model, tea.Cmd) {
		m.helpMode = true
		m.helpScroll = 0
		return m, nil
	},
	"S": func(m model) (tea.Model, tea.Cmd) {
		// Cycle sort mode
		m.list.sort = (m.list.sort + 1) % sortModeCount
		m.filterDevices()
		return m, nil
	},
}

func (m model) handleNormalMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	before := m.list.cursor
	updated, cmd := m.handleNormalModeKeys(msg)
	nm := updated.(model)
	if nm.list.cursor != before {
		if tick := nm.scheduleAutoPing(); tick != nil {
			return nm, tea.Batch(cmd, tick)
		}
	}
	return nm, cmd
}

func (m model) handleNormalModeKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.handleFrameShortcut(msg.String()) {
		return m, nil
	}
	if m.run.visible {
		switch m.activeFocus {
		case focusRun:
			return m.handleRunPanel(msg)
		case focusOutput:
			return m.handleOutputPanel(msg)
		}
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

// moveCursorUp moves the device-list cursor up.
// (Run and Output panels handle their own cursors.)
func (m *model) moveCursorUp() {
	if m.list.cursor > 0 {
		m.list.cursor--
		m.ensureListCursorVisible()
		m.notify.sshError = nil
	}
}

// moveCursorDown moves the device-list cursor down.
func (m *model) moveCursorDown() {
	if m.list.cursor < len(m.list.filteredDevices)-1 {
		m.list.cursor++
		m.ensureListCursorVisible()
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
	if !m.isReachable(device) {
		deviceName := device.Name
		if deviceName == "" {
			deviceName = device.Hostname
		}
		m.notify.sshError = fmt.Errorf("machine %q is offline", deviceName)
		return m, nil
	}

	// Check if Tailscale is running locally before attempting SSH
	if isRunning, message := checkLocalTailscaleStatus(); !isRunning {
		m.notify.sshError = fmt.Errorf("tailscale is not running locally: %s\nPress 'u' to run 'tailscale up' or start Tailscale manually", message)
		return m, nil
	}

	// Everything else (account switch, username/password prompts, persistence)
	// is handled by the credential flow in sshflow.go.
	return m.withPreflight(device, func(m model) (tea.Model, tea.Cmd) {
		return m.beginSSH(m.indexOf(device), sshJob{action: sshActionSession})
	})
}

// ─── Options menu constants ───────────────────────────────────────────────────

const (
	optionMachineCredentials = 0 // View machines with saved passwords
	optionSecretStore        = 1 // Toggle master key location: file / OS keychain
	optionCount              = 2 // Total number of options
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
		case optionMachineCredentials:
			m.opts.showMachineCreds = true
			m.opts.machineCredsCursor = 0
			return m, nil
		case optionSecretStore:
			if m.opts.secretStoreBusy {
				return m, nil
			}
			m.opts.secretStoreBusy = true
			m.opts.secretStoreMsg = "Switching secret store…"
			return m, toggleSecretStore()
		}
		return m, nil
	},
}

// toggleSecretStore moves the master key to the other store.
func toggleSecretStore() tea.Cmd {
	return func() tea.Msg {
		var err error
		target := util.SecretStoreKeychain
		if util.CurrentSecretStore() == util.SecretStoreKeychain {
			target = util.SecretStoreFile
			err = util.UseFile()
		} else {
			err = util.UseKeychain()
		}
		return secretStoreChangedMsg{store: util.CurrentSecretStore(), target: target, err: err}
	}
}

func (m model) handleOptionsMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.opts.showMachineCreds {
		return m.handleMachineCredsView(msg)
	}
	if newM, cmd, handled := dispatchKey(msg.String(), m, optionsMenuHandlers); handled {
		return newM, cmd
	}
	return m, nil
}

// handleMachineCredsView handles keys in the "machines with saved passwords" sub-view
func (m model) handleMachineCredsView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	machineIDs := m.getSortedMachineCredIDs()
	maxCursor := len(machineIDs) - 1
	if maxCursor < 0 {
		maxCursor = 0
	}

	switch msg.String() {
	case "esc", "q":
		m.opts.showMachineCreds = false
		return m, nil
	case "up", "k":
		if m.opts.machineCredsCursor > 0 {
			m.opts.machineCredsCursor--
		}
		return m, nil
	case "down", "j":
		if m.opts.machineCredsCursor < maxCursor {
			m.opts.machineCredsCursor++
		}
		return m, nil
	case "d", "delete":
		// Delete selected machine credentials (backspace deliberately not
		// bound: it is too easy to hit by reflex after leaving a text prompt)
		if len(machineIDs) > 0 && m.opts.machineCredsCursor < len(machineIDs) {
			id := machineIDs[m.opts.machineCredsCursor]
			delete(m.ssh.machineCreds, id)
			if m.opts.machineCredsCursor > 0 && m.opts.machineCredsCursor >= len(machineIDs)-1 {
				m.opts.machineCredsCursor--
			}
			return m, deleteMachineCredential(id)
		}
		return m, nil
	}
	return m, nil
}

// ─── About mode ───────────────────────────────────────────────────────────────

func (m model) handleAboutMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "a", "enter":
		m.aboutMode = false
	case "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

func (m model) handleHelpMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "f1", "enter":
		m.helpMode = false
	case "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		if m.helpScroll > 0 {
			m.helpScroll--
		}
	case "down", "j":
		m.helpScroll++
	}
	return m, nil
}

// ─── SSH retry menu ───────────────────────────────────────────────────────────

const (
	sshRetryUsername = 0
	sshRetryPassword = 1
	sshRetryBoth     = 2
	sshRetryCancel   = 3
	sshRetryCount    = 4
)

func (m model) handleSSHRetryMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		return m.cancelSSHAttempt(), nil
	case "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		if m.ssh.retryCursor > 0 {
			m.ssh.retryCursor--
		}
		return m, nil
	case "down", "j":
		if m.ssh.retryCursor < sshRetryCount-1 {
			m.ssh.retryCursor++
		}
		return m, nil
	case "enter":
		m.ssh.retryMode = false
		m.notify.sshError = nil
		p := &m.ssh.pending
		switch m.ssh.retryCursor {
		case sshRetryUsername:
			// New username, keep whatever password we have for the retry.
			p.username = ""
			m.input = textInput{mode: inputUsername}
		case sshRetryPassword:
			// Ignore the saved password for this attempt and ask for a new one.
			p.password = ""
			p.passwordTried = true
			if cred, ok := m.ssh.machineCreds[p.machineID]; ok {
				cred.passwordEncrypted = ""
				m.ssh.machineCreds[p.machineID] = cred
			}
			m.input = textInput{mode: inputPassword}
		case sshRetryBoth:
			p.username = ""
			p.password = ""
			p.passwordTried = true
			p.askPasswordNext = true
			if cred, ok := m.ssh.machineCreds[p.machineID]; ok {
				cred.username = ""
				cred.passwordEncrypted = ""
				m.ssh.machineCreds[p.machineID] = cred
			}
			m.input = textInput{mode: inputUsername}
		case sshRetryCancel:
			return m.cancelSSHAttempt(), nil
		}
		return m, nil
	}
	return m, nil
}

// ─── Password prompt mode ─────────────────────────────────────────────────────

var passwordPromptHandlers = map[string]keyHandler{
	"esc": func(m model) (tea.Model, tea.Cmd) {
		return m.cancelSSHAttempt(), nil
	},
	"ctrl+c": func(m model) (tea.Model, tea.Cmd) {
		return m.cancelSSHAttempt(), nil
	},
	"enter": func(m model) (tea.Model, tea.Cmd) {
		if m.input.value == "" {
			return m, nil
		}
		password := m.input.value
		m.input = textInput{mode: inputNone}
		m.notify.sshError = nil
		// Keep the password in memory only; it is encrypted and written to
		// config.json by finishSSHAttempt once the connection succeeds.
		m.ssh.pending.password = password
		m.ssh.pending.passwordTried = true
		return m.runPendingAttempt()
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
	// Cursor movement, word-wise Option/Alt+arrows, deletions, typing, paste.
	m.editInput(msg)
	return m, nil
}
