package tui

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yatsenko-ihor/ts-cli/client"
	"github.com/yatsenko-ihor/ts-cli/config"
	"github.com/yatsenko-ihor/ts-cli/scripts"
	"github.com/yatsenko-ihor/ts-cli/util"
)

// The Run panel: a launcher for things to execute on the selected machine.
//
//	Scripts   local files from ~/.ts-cli/scripts, streamed over ssh and run
//	Commands  saved named commands with {{placeholders}}
//	Recent    what ran on this machine before
//	:         ad-hoc command, runs immediately
//
// Output streams into the panel below while the job runs.

// ─── Tab contents ─────────────────────────────────────────────────────────────

// visibleScripts filters scripts by the selected device's OS.
func (m model) visibleScripts() []scripts.Script {
	dev, ok := m.targetDevice()
	var out []scripts.Script
	for _, s := range m.run.scripts {
		if !ok || s.SupportsOS(dev.OS) {
			out = append(out, s)
		}
	}
	return out
}

// recentEntries returns history for the selected machine, newest first.
func (m model) recentEntries() []util.CommandHistory {
	dev, ok := m.targetDevice()
	if !ok || m.run.history == nil {
		return nil
	}
	return m.run.history.Recent(machineKey(dev), 50)
}

// tabLength is the number of rows in the active tab.
func (m model) tabLength() int {
	switch m.run.tab {
	case tabScripts:
		return len(m.visibleScripts())
	case tabCommands:
		return len(m.run.commands)
	case tabRecent:
		return len(m.recentEntries())
	}
	return 0
}

func (m model) tabCursor() int { return m.run.cursors[m.run.tab] }

func (m *model) clampTabCursor() {
	n := m.tabLength()
	c := &m.run.cursors[m.run.tab]
	if *c >= n {
		*c = n - 1
	}
	if *c < 0 {
		*c = 0
	}
}

// targetDevice returns the highlighted device.
func (m model) targetDevice() (client.Device, bool) {
	i := m.getTargetDevice()
	if i < 0 || i >= len(m.list.filteredDevices) {
		return client.Device{}, false
	}
	return m.list.filteredDevices[i], true
}

// ─── Opening / focus ──────────────────────────────────────────────────────────

// openRunPanel shows the right column and focuses the Run panel.
func (m model) openRunPanel(focus panelFocus) model {
	m.run.visible = true
	m.activeFocus = focus
	m.clampTabCursor()
	return m
}

func (m model) closeRunPanel() model {
	m.run.visible = false
	m.activeFocus = focusList
	return m
}

// ─── Run panel key handling ───────────────────────────────────────────────────

func (m model) handleRunPanel(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "tab":
		m.activeFocus = focusOutput
		return m, nil
	case "shift+tab", "backtab":
		m.activeFocus = focusList
		return m, nil
	case "esc":
		return m.closeRunPanel(), nil
	case "ctrl+c", "q":
		return m, tea.Quit
	case "]", "l", "right":
		m.run.tab = (m.run.tab + 1) % tabCount
		m.clampTabCursor()
		return m, nil
	case "[", "h", "left":
		m.run.tab = (m.run.tab + tabCount - 1) % tabCount
		m.clampTabCursor()
		return m, nil
	case "up", "k":
		if m.run.cursors[m.run.tab] > 0 {
			m.run.cursors[m.run.tab]--
		}
		return m, nil
	case "down", "j":
		if m.run.cursors[m.run.tab] < m.tabLength()-1 {
			m.run.cursors[m.run.tab]++
		}
		return m, nil
	case ":":
		return m.openAdHocPrompt()
	case "enter":
		return m.runSelected()
	}

	switch m.run.tab {
	case tabScripts:
		if key == "r" {
			return m.reloadScripts(), nil
		}
	case tabCommands:
		switch key {
		case "n":
			return m.promptNewCommand(config.SavedCommand{}, "")
		case "e":
			if c, ok := m.selectedCommand(); ok {
				return m.promptNewCommand(c, c.Name)
			}
		case "d":
			if c, ok := m.selectedCommand(); ok {
				return m, deleteSavedCommand(c.Name)
			}
		}
	case tabRecent:
		switch key {
		case "d":
			if e, ok := m.selectedRecent(); ok && m.run.history != nil {
				if _, err := m.run.history.DeleteEntries(e.MachineID, e.Key()); err != nil {
					m.notify.sshError = fmt.Errorf("failed to delete history entry: %w", err)
				}
				m.clampTabCursor()
			}
			return m, nil
		case "s":
			if e, ok := m.selectedRecent(); ok && e.Kind != util.HistoryKindScript {
				return m.promptNewCommand(config.SavedCommand{Command: e.Command}, "")
			}
		}
	}
	return m, nil
}

func (m model) selectedCommand() (config.SavedCommand, bool) {
	i := m.tabCursor()
	if i < 0 || i >= len(m.run.commands) {
		return config.SavedCommand{}, false
	}
	return m.run.commands[i], true
}

func (m model) selectedRecent() (util.CommandHistory, bool) {
	entries := m.recentEntries()
	i := m.tabCursor()
	if i < 0 || i >= len(entries) {
		return util.CommandHistory{}, false
	}
	return entries[i], true
}

func (m model) reloadScripts() model {
	m.run.scripts, m.run.scriptsErr = scripts.Load()
	m.clampTabCursor()
	return m
}

// ─── Running things ───────────────────────────────────────────────────────────

// runSelected executes the highlighted row of the active tab.
func (m model) runSelected() (tea.Model, tea.Cmd) {
	if m.run.running {
		m.notify.sshError = fmt.Errorf("a job is still running; press x in the Output panel to stop it")
		return m, nil
	}
	if _, ok := m.targetDevice(); !ok {
		return m, nil
	}
	if err := m.checkTargetReady(); err != nil {
		m.notify.sshError = err
		return m, nil
	}

	switch m.run.tab {
	case tabScripts:
		list := m.visibleScripts()
		i := m.tabCursor()
		if i < 0 || i >= len(list) {
			return m, nil
		}
		return m.runScriptWithArgs(list[i])
	case tabCommands:
		c, ok := m.selectedCommand()
		if !ok {
			return m, nil
		}
		return m.runCommandWithPlaceholders(c.Command)
	case tabRecent:
		e, ok := m.selectedRecent()
		if !ok {
			return m, nil
		}
		if e.Kind == util.HistoryKindScript {
			for _, s := range m.run.scripts {
				if s.File == e.Script {
					if len(e.Args) > 0 {
						return m.startScript(s, e.Args)
					}
					return m.runScriptWithArgs(s)
				}
			}
			m.notify.sshError = fmt.Errorf("script %q no longer exists in ~/.ts-cli/%s", e.Script, scripts.DirName)
			return m, nil
		}
		return m.startCommand(e.Command)
	}
	return m, nil
}

// checkTargetReady mirrors the pre-flight checks of the interactive session.
func (m model) checkTargetReady() error {
	dev, ok := m.targetDevice()
	if !ok {
		return fmt.Errorf("no device selected")
	}
	if !isDeviceOnline(dev) {
		return fmt.Errorf("machine %q is offline", displayName(dev))
	}
	if isRunning, message := checkLocalTailscaleStatus(); !isRunning {
		return fmt.Errorf("tailscale is not running locally: %s", message)
	}
	return nil
}

// openAdHocPrompt opens the ":" command line.
func (m model) openAdHocPrompt() (tea.Model, tea.Cmd) {
	if err := m.checkTargetReady(); err != nil {
		m.notify.sshError = err
		return m, nil
	}
	m = m.openRunPanel(focusRun)
	m.input = textInput{mode: inputCommand}
	m.notify.sshError = nil
	return m, nil
}

// startCommand launches a shell command on the selected device.
func (m model) startCommand(command string) (tea.Model, tea.Cmd) {
	command = util.SanitizeInput(command)
	if command == "" {
		return m, nil
	}
	return m.beginSSH(m.getTargetDevice(), sshJob{action: sshActionCommand, command: command})
}

// startScript launches a script with resolved arguments.
func (m model) startScript(s scripts.Script, args []string) (tea.Model, tea.Cmd) {
	sc := s // copy; pending holds a pointer
	return m.beginSSH(m.getTargetDevice(), sshJob{action: sshActionScript, script: &sc, args: args})
}

// placeholderRe matches {{name}} tokens in saved commands.
var placeholderRe = regexp.MustCompile(`\{\{\s*([A-Za-z_][A-Za-z0-9_-]*)\s*\}\}`)

// placeholders returns the distinct placeholder names in order of appearance.
func placeholders(command string) []string {
	seen := map[string]bool{}
	var out []string
	for _, mm := range placeholderRe.FindAllStringSubmatch(command, -1) {
		if !seen[mm[1]] {
			seen[mm[1]] = true
			out = append(out, mm[1])
		}
	}
	return out
}

// fillPlaceholders substitutes values into command.
func fillPlaceholders(command string, values map[string]string) string {
	return placeholderRe.ReplaceAllStringFunc(command, func(tok string) string {
		name := placeholderRe.FindStringSubmatch(tok)[1]
		if v, ok := values[name]; ok {
			return v
		}
		return tok
	})
}

// runCommandWithPlaceholders asks for each {{placeholder}} then runs.
func (m model) runCommandWithPlaceholders(command string) (tea.Model, tea.Cmd) {
	names := placeholders(command)
	if len(names) == 0 {
		return m.startCommand(command)
	}
	values := map[string]string{}
	return m.askSequence(names, "Command: "+command, values, func(m model) (tea.Model, tea.Cmd) {
		return m.startCommand(fillPlaceholders(command, values))
	})
}

// runScriptWithArgs asks for each declared argument then runs the script.
func (m model) runScriptWithArgs(s scripts.Script) (tea.Model, tea.Cmd) {
	if len(s.Args) == 0 {
		return m.startScript(s, nil)
	}
	values := map[string]string{}
	return m.askSequence(s.Args, "Script: "+s.Name, values, func(m model) (tea.Model, tea.Cmd) {
		args := make([]string, len(s.Args))
		for i, a := range s.Args {
			args[i] = values[a]
		}
		return m.startScript(s, args)
	})
}

// askSequence opens one text prompt per name, storing answers in values, then
// calls done.
func (m model) askSequence(names []string, hint string, values map[string]string, done func(model) (tea.Model, tea.Cmd)) (tea.Model, tea.Cmd) {
	if len(names) == 0 {
		return done(m)
	}
	name := names[0]
	m.input = textInput{
		mode:  inputText,
		label: fmt.Sprintf("Value for {{%s}}", name),
		hint:  hint,
		onSubmit: func(m model, value string) (tea.Model, tea.Cmd) {
			values[name] = value
			return m.askSequence(names[1:], hint, values, done)
		},
	}
	return m, nil
}

// ─── Saved commands ───────────────────────────────────────────────────────────

// promptNewCommand asks for a name and a command, then saves. oldName is set
// when editing an existing entry.
func (m model) promptNewCommand(initial config.SavedCommand, oldName string) (tea.Model, tea.Cmd) {
	m = m.openRunPanel(focusRun)
	m.input = textInput{
		mode:  inputText,
		label: "Command name",
		value: initial.Name,
		onSubmit: func(m model, name string) (tea.Model, tea.Cmd) {
			name = strings.TrimSpace(name)
			if name == "" {
				return m, nil
			}
			m.input = textInput{
				mode:  inputText,
				label: "Command for \"" + name + "\"",
				hint:  "Use {{name}} placeholders to be asked for values at run time",
				value: initial.Command,
				onSubmit: func(m model, command string) (tea.Model, tea.Cmd) {
					command = util.SanitizeInput(command)
					if command == "" {
						return m, nil
					}
					m.input = textInput{mode: inputNone}
					m.run.tab = tabCommands
					return m, saveSavedCommand(oldName, config.SavedCommand{Name: name, Command: command})
				},
			}
			return m, nil
		},
	}
	return m, nil
}

// saveSavedCommand persists a saved command and returns the fresh list.
func saveSavedCommand(oldName string, cmd config.SavedCommand) tea.Cmd {
	return func() tea.Msg {
		var list []config.SavedCommand
		err := config.Update(func(cfg *config.Config) error {
			cfg.UpsertCommand(oldName, cmd)
			list = append([]config.SavedCommand(nil), cfg.Commands...)
			return nil
		})
		return commandsChangedMsg{commands: list, err: err}
	}
}

// deleteSavedCommand removes a saved command and returns the fresh list.
func deleteSavedCommand(name string) tea.Cmd {
	return func() tea.Msg {
		var list []config.SavedCommand
		err := config.Update(func(cfg *config.Config) error {
			cfg.DeleteCommand(name)
			list = append([]config.SavedCommand(nil), cfg.Commands...)
			return nil
		})
		return commandsChangedMsg{commands: list, err: err}
	}
}

// ─── Output panel ─────────────────────────────────────────────────────────────

func (m model) handleOutputPanel(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "tab":
		m.activeFocus = focusList
		return m, nil
	case "shift+tab", "backtab":
		m.activeFocus = focusRun
		return m, nil
	case "esc":
		return m.closeRunPanel(), nil
	case "ctrl+c":
		if m.run.running {
			m.run.job.kill()
			return m, nil
		}
		return m, tea.Quit
	case "q":
		return m, tea.Quit
	case "x":
		if m.run.running {
			m.run.job.kill()
		}
		return m, nil
	case "up", "k":
		if m.run.outputCursor > 0 {
			m.run.outputCursor--
			m.run.follow = false
			m.ensureOutputCursorVisible()
		}
		return m, nil
	case "down", "j":
		if m.run.outputCursor < len(m.run.lines)-1 {
			m.run.outputCursor++
			m.run.follow = m.run.outputCursor == len(m.run.lines)-1
			m.ensureOutputCursorVisible()
		}
		return m, nil
	case "G", "end":
		m.jumpToOutputEnd()
		return m, nil
	case "g", "home":
		m.run.outputCursor = 0
		m.run.outputScroll = 0
		m.run.follow = false
		return m, nil
	case "c":
		return m, m.copySelectedOutputItem(false)
	case "n":
		return m, m.copySelectedOutputItem(true)
	case "C":
		if !m.run.running {
			m.run.lines = nil
			m.run.partial = ""
			m.run.finished = false
			m.run.label = ""
			m.run.outputCursor = 0
			m.run.outputScroll = 0
		}
		return m, nil
	case ":":
		return m.openAdHocPrompt()
	}
	return m, nil
}

func (m *model) jumpToOutputEnd() {
	if n := len(m.run.lines); n > 0 {
		m.run.outputCursor = n - 1
	} else {
		m.run.outputCursor = 0
	}
	m.run.follow = true
	m.ensureOutputCursorVisible()
}

// ─── Job lifecycle (called from Update) ───────────────────────────────────────

// beginJobOutput resets the Output panel for a new job.
func (m *model) beginJobOutput(job *runJob, label string) {
	m.run.job = job
	m.run.label = label
	m.run.lines = nil
	m.run.partial = ""
	m.run.running = true
	m.run.cancelled = false
	m.run.finished = false
	m.run.exitCode = 0
	m.run.startedAt = time.Now()
	m.run.elapsed = 0
	m.run.outputCursor = 0
	m.run.outputScroll = 0
	m.run.follow = true
	m.run.visible = true
}

// appendJobLines adds streamed lines, keeping the buffer bounded.
func (m *model) appendJobLines(lines []string) {
	m.run.lines = append(m.run.lines, lines...)
	if over := len(m.run.lines) - maxOutputLines; over > 0 {
		m.run.lines = m.run.lines[over:]
		m.run.outputCursor -= over
		m.run.outputScroll -= over
		if m.run.outputCursor < 0 {
			m.run.outputCursor = 0
		}
		if m.run.outputScroll < 0 {
			m.run.outputScroll = 0
		}
	}
	if m.run.follow {
		m.jumpToOutputEnd()
	}
}

// finishJobOutput records the result in the Output panel state.
func (m *model) finishJobOutput(res runResult) {
	m.run.running = false
	m.run.finished = true
	m.run.cancelled = res.killed
	m.run.exitCode = res.exitCode
	m.run.elapsed = time.Since(m.run.startedAt)
	m.run.job = nil
}

// recordJobHistory appends the finished job to history.
func (m model) recordJobHistory(p pendingSSH, res runResult) {
	if m.run.history == nil || p.machineID == "" {
		return
	}
	entry := util.CommandHistory{
		MachineID:   p.machineID,
		MachineName: p.machineName,
		ExitCode:    res.exitCode,
		Output:      strings.Join(m.run.lines, "\n"),
	}
	switch p.action {
	case sshActionScript:
		if p.script == nil {
			return
		}
		entry.Kind = util.HistoryKindScript
		entry.Command = p.script.Name
		entry.Script = p.script.File
		entry.Args = p.args
	case sshActionCommand:
		entry.Kind = util.HistoryKindCommand
		entry.Command = p.command
	default:
		return
	}
	m.run.history.Add(entry)
	_ = m.run.history.Save()
}

// outputStatus returns the short status shown in the Output title.
func (m model) outputStatus() string {
	switch {
	case m.run.running:
		return fmt.Sprintf("● running %s", m.run.elapsed.Truncate(time.Second))
	case m.run.cancelled:
		return "✗ stopped"
	case m.run.finished && m.run.exitCode == 0:
		return fmt.Sprintf("✓ exit 0 · %s", m.run.elapsed.Truncate(time.Millisecond*100))
	case m.run.finished:
		return fmt.Sprintf("✗ exit %d · %s", m.run.exitCode, m.run.elapsed.Truncate(time.Millisecond*100))
	}
	return ""
}
