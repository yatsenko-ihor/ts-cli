package tui

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yatsenko-ihor/ts-cli/client"
	"github.com/yatsenko-ihor/ts-cli/config"
	"github.com/yatsenko-ihor/ts-cli/scripts"
	"github.com/yatsenko-ihor/ts-cli/util"
)

// The Run panel: a launcher for things to execute on the selected machine(s).
//
//	Scripts   local files from ~/.ts-cli/scripts, streamed over ssh and run
//	Commands  saved named commands with {{placeholders}}
//	Recent    what ran on this machine before
//	:         ad-hoc command, runs immediately
//
// Targets: the highlighted machine, or every machine marked with Space.
// A single target goes through the interactive credential flow (prompts);
// several targets run in parallel without prompts using saved credentials,
// and the Output panel shows one section per machine.

// maxParallelRuns bounds how many ssh processes a multi-machine run keeps alive.
const maxParallelRuns = 8

// ─── Targets / marks ──────────────────────────────────────────────────────────

// targetDevice returns the highlighted device.
func (m model) targetDevice() (client.Device, bool) {
	i := m.getTargetDevice()
	if i < 0 || i >= len(m.list.filteredDevices) {
		return client.Device{}, false
	}
	return m.list.filteredDevices[i], true
}

// markedDevices returns the marked devices in list order (only those visible).
func (m model) markedDevices() []client.Device {
	if len(m.list.marked) == 0 {
		return nil
	}
	var out []client.Device
	for _, d := range m.list.filteredDevices {
		if m.list.marked[machineKey(d)] {
			out = append(out, d)
		}
	}
	return out
}

// targets returns the machines a run applies to: marked ones, else the highlighted one.
func (m model) targets() []client.Device {
	if marked := m.markedDevices(); len(marked) > 0 {
		return marked
	}
	if d, ok := m.targetDevice(); ok {
		return []client.Device{d}
	}
	return nil
}

// isMulti reports whether a run would address several machines.
func (m model) isMulti() bool { return len(m.markedDevices()) > 1 }

func (m *model) toggleMark(d client.Device) {
	if m.list.marked == nil {
		m.list.marked = map[string]bool{}
	}
	k := machineKey(d)
	if m.list.marked[k] {
		delete(m.list.marked, k)
	} else {
		m.list.marked[k] = true
	}
}

// toggleAllMarks marks every visible machine, or clears all marks when every
// visible machine is already marked.
func (m *model) toggleAllMarks() {
	all := len(m.list.filteredDevices) > 0
	for _, d := range m.list.filteredDevices {
		if !m.list.marked[machineKey(d)] {
			all = false
			break
		}
	}
	if all {
		m.list.marked = nil
		return
	}
	if m.list.marked == nil {
		m.list.marked = map[string]bool{}
	}
	for _, d := range m.list.filteredDevices {
		m.list.marked[machineKey(d)] = true
	}
}

func (m *model) clearMarks() { m.list.marked = nil }

// targetsLabel describes the targets for headers.
func (m model) targetsLabel() string {
	t := m.targets()
	switch len(t) {
	case 0:
		return "No machine selected"
	case 1:
		return fmt.Sprintf("%s %s", getStatusIcon(t[0]), displayName(t[0]))
	}
	names := make([]string, 0, len(t))
	for _, d := range t {
		names = append(names, displayName(d))
	}
	return fmt.Sprintf("%d machines: %s", len(t), strings.Join(names, ", "))
}

// ─── Tab contents ─────────────────────────────────────────────────────────────

// visibleScripts filters scripts by the targets' OS: a script is listed when
// at least one target can run it.
func (m model) visibleScripts() []scripts.Script {
	targets := m.targets()
	var out []scripts.Script
	for _, s := range m.run.scripts {
		if len(targets) == 0 {
			out = append(out, s)
			continue
		}
		for _, d := range targets {
			if s.SupportsOS(d.OS) {
				out = append(out, s)
				break
			}
		}
	}
	return out
}

// recentEntries returns history for the highlighted machine, newest first.
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

// ─── Opening / focus ──────────────────────────────────────────────────────────

// openRunPanel shows the right column and focuses the given panel.
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

// ─── Launching ────────────────────────────────────────────────────────────────

// runSelected executes the highlighted row of the active tab on the targets.
func (m model) runSelected() (tea.Model, tea.Cmd) {
	if m.run.running() {
		m.notify.sshError = fmt.Errorf("a run is still in progress; press x in the Output panel to stop it")
		return m, nil
	}
	if len(m.targets()) == 0 {
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
						return m.launch(sshJob{action: sshActionScript, script: &s, args: e.Args})
					}
					return m.runScriptWithArgs(s)
				}
			}
			m.notify.sshError = fmt.Errorf("script %q no longer exists in ~/.ts-cli/%s", e.Script, scripts.DirName)
			return m, nil
		}
		return m.launch(sshJob{action: sshActionCommand, command: e.Command})
	}
	return m, nil
}

// openAdHocPrompt opens the ":" command line.
func (m model) openAdHocPrompt() (tea.Model, tea.Cmd) {
	if len(m.targets()) == 0 {
		return m, nil
	}
	if isRunning, message := checkLocalTailscaleStatus(); !isRunning {
		m.notify.sshError = fmt.Errorf("tailscale is not running locally: %s", message)
		return m, nil
	}
	m = m.openRunPanel(focusRun)
	m.input = textInput{mode: inputCommand}
	m.notify.sshError = nil
	return m, nil
}

// startCommand launches a shell command on the targets.
func (m model) startCommand(command string) (tea.Model, tea.Cmd) {
	command = util.SanitizeInput(command)
	if command == "" {
		return m, nil
	}
	return m.launch(sshJob{action: sshActionCommand, command: command})
}

// launch dispatches a job to one machine (interactive flow) or many (batch).
func (m model) launch(spec sshJob) (tea.Model, tea.Cmd) {
	if spec.action == sshActionScript && spec.script != nil {
		sc := *spec.script // pending holds a pointer; keep our own copy
		spec.script = &sc
	}
	if isRunning, message := checkLocalTailscaleStatus(); !isRunning {
		m.notify.sshError = fmt.Errorf("tailscale is not running locally: %s", message)
		return m, nil
	}
	if m.isMulti() {
		return m.startMultiRun(spec)
	}
	dev, ok := m.targetDevice()
	if marked := m.markedDevices(); len(marked) == 1 {
		dev, ok = marked[0], true
	}
	if !ok {
		return m, nil
	}
	if !isDeviceOnline(dev) {
		m.notify.sshError = fmt.Errorf("machine %q is offline", displayName(dev))
		return m, nil
	}
	return m.beginSSH(m.indexOf(dev), spec)
}

// indexOf returns the position of d in the filtered list (or the cursor).
func (m model) indexOf(d client.Device) int {
	k := machineKey(d)
	for i, x := range m.list.filteredDevices {
		if machineKey(x) == k {
			return i
		}
	}
	return m.getTargetDevice()
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
		return m.launch(sshJob{action: sshActionScript, script: &s})
	}
	values := map[string]string{}
	return m.askSequence(s.Args, "Script: "+s.Name, values, func(m model) (tea.Model, tea.Cmd) {
		args := make([]string, len(s.Args))
		for i, a := range s.Args {
			args[i] = values[a]
		}
		return m.launch(sshJob{action: sshActionScript, script: &s, args: args})
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

// ─── Single-machine run bookkeeping ───────────────────────────────────────────

// beginSingleRun resets the Output panel for a job on one machine.
func (m *model) beginSingleRun(device client.Device, username string, spec sshJob, job *runJob) {
	now := time.Now()
	m.run.spec = spec
	m.run.label = spec.label()
	m.run.multi = false
	m.run.runs = []*machineRun{{device: device, username: username, job: job, status: runRunning, startedAt: now}}
	m.run.startedAt = now
	m.run.elapsed = 0
	m.run.outputCursor = 0
	m.run.outputScroll = 0
	m.run.follow = true
	m.run.visible = true
}

// ─── Multi-machine run ────────────────────────────────────────────────────────

// startMultiRun prepares one machineRun per marked machine, skipping those
// that cannot be attempted, and starts the first maxParallelRuns of them.
func (m model) startMultiRun(spec sshJob) (tea.Model, tea.Cmd) {
	targets := m.markedDevices()
	now := time.Now()
	m.run.spec = spec
	m.run.label = spec.label()
	m.run.multi = true
	m.run.runs = nil
	m.run.startedAt = now
	m.run.elapsed = 0
	m.run.outputCursor = 0
	m.run.outputScroll = 0
	m.run.follow = true
	m.run.visible = true
	m.notify.sshError = nil

	for _, d := range targets {
		mr := &machineRun{device: d, status: runPending}
		switch {
		case !isDeviceOnline(d):
			mr.status, mr.note = runSkipped, "offline"
		case m.needsAccountSwitch(d):
			mr.status, mr.note = runSkipped, "belongs to another account (active: "+m.acct.tailscaleActiveAccount+")"
		case spec.action == sshActionScript && spec.script != nil && !spec.script.SupportsOS(d.OS):
			mr.status, mr.note = runSkipped, "script is not for "+d.OS
		default:
			mr.username = m.usernameForDevice(d)
			if mr.username == "" {
				mr.status, mr.note = runSkipped, "no username saved; connect once with s to set one"
			} else {
				mr.password = m.savedPassword(machineKey(d))
			}
		}
		m.run.runs = append(m.run.runs, mr)
	}

	cmds := m.startPendingRuns()
	if !m.run.running() {
		m.run.elapsed = time.Since(m.run.startedAt)
	}
	return m, tea.Batch(cmds...)
}

// startPendingRuns launches waiting machines while slots are free.
func (m *model) startPendingRuns() []tea.Cmd {
	active := 0
	for _, mr := range m.run.runs {
		if mr.status == runRunning {
			active++
		}
	}
	var cmds []tea.Cmd
	for _, mr := range m.run.runs {
		if active >= maxParallelRuns {
			break
		}
		if mr.status != runPending {
			continue
		}
		job, err := startRemoteJob(mr.device, mr.username, mr.password, m.run.spec)
		if err != nil {
			mr.status, mr.note = runFailed, err.Error()
			continue
		}
		mr.job = job
		mr.status = runRunning
		mr.startedAt = time.Now()
		active++
		cmds = append(cmds, waitRunEvent(job), tickRun(job))
	}
	return cmds
}

// continueMultiRun records one machine's outcome and starts the next one.
func (m model) continueMultiRun(mr *machineRun, res runResult) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch {
	case res.killed:
		mr.note = "stopped"
	case res.failure == sshFailureNone, res.failure == sshFailureRemoteExit:
		m.recordRunHistory(mr, m.run.spec.action, m.run.spec.command, m.run.spec.script, m.run.spec.args)
		if cmd := m.persistRunUsername(mr); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case res.failure == sshFailureBadPassword:
		mr.status, mr.note = runFailed, "saved password rejected; connect once with s to fix it"
	case res.failure == sshFailureConnection:
		mr.status, mr.note = runFailed, "could not connect or authenticate; connect once with s to save credentials"
	default:
		mr.status, mr.note = runFailed, res.err.Error()
	}
	cmds = append(cmds, m.startPendingRuns()...)
	if !m.run.running() {
		m.run.elapsed = time.Since(m.run.startedAt)
	}
	return m, tea.Batch(cmds...)
}

// persistRunUsername saves the username used by a successful batch run for
// machines that had none, so the next run does not fall back to the default.
func (m *model) persistRunUsername(mr *machineRun) tea.Cmd {
	key := machineKey(mr.device)
	cred := m.ssh.machineCreds[key]
	if cred.username == mr.username && cred.name != "" {
		return nil
	}
	cred.username = mr.username
	cred.name = displayName(mr.device)
	if m.ssh.machineCreds == nil {
		m.ssh.machineCreds = map[string]machineCredential{}
	}
	m.ssh.machineCreds[key] = cred
	return storeMachineCredential(key, cred)
}

// stopRun kills every running machine and marks waiting ones as stopped.
func (m *model) stopRun() {
	for _, mr := range m.run.runs {
		switch mr.status {
		case runRunning:
			mr.job.kill()
		case runPending:
			mr.status, mr.note = runSkipped, "stopped"
		}
	}
}

// ─── Shared bookkeeping ───────────────────────────────────────────────────────

// settleMachineRun stores a finished process result on its machineRun.
func (m *model) settleMachineRun(mr *machineRun, res runResult) {
	mr.status = runDone
	mr.exitCode = res.exitCode
	mr.killed = res.killed
	mr.elapsed = time.Since(mr.startedAt)
	mr.job = nil
	if !m.run.running() {
		m.run.elapsed = time.Since(m.run.startedAt)
	}
}

// appendRunLines adds streamed lines to the machine owning job.
func (m *model) appendRunLines(job *runJob, lines []string) {
	mr := m.run.runFor(job)
	if mr == nil {
		return
	}
	limit := maxOutputLines
	if m.run.multi {
		limit = maxOutputLines / 4
	}
	mr.lines = append(mr.lines, lines...)
	if over := len(mr.lines) - limit; over > 0 {
		mr.lines = mr.lines[over:]
	}
	if m.run.follow {
		m.jumpToOutputEnd()
	} else {
		total := len(m.outputLines())
		if m.run.outputCursor >= total {
			m.run.outputCursor = total - 1
		}
	}
}

// recordRunHistory appends a finished machine to history.
func (m model) recordRunHistory(mr *machineRun, action sshAction, command string, script *scripts.Script, args []string) {
	if m.run.history == nil {
		return
	}
	entry := util.CommandHistory{
		MachineID:   machineKey(mr.device),
		MachineName: displayName(mr.device),
		ExitCode:    mr.exitCode,
		Output:      strings.Join(mr.lines, "\n"),
	}
	switch action {
	case sshActionScript:
		if script == nil {
			return
		}
		entry.Kind = util.HistoryKindScript
		entry.Command = script.Name
		entry.Script = script.File
		entry.Args = args
	case sshActionCommand:
		entry.Kind = util.HistoryKindCommand
		entry.Command = command
	default:
		return
	}
	m.run.history.Add(entry)
	_ = m.run.history.Save()
}

// ─── Output flattening ────────────────────────────────────────────────────────

// outputLine is one row of the Output panel.
type outputLine struct {
	text   string
	run    *machineRun // owning machine (nil for separators)
	header bool        // section header row in multi mode
}

// outputLines flattens the current run into rows. Single runs have no headers.
func (m model) outputLines() []outputLine {
	if s := m.run.single(); s != nil {
		out := make([]outputLine, len(s.lines))
		for i, l := range s.lines {
			out[i] = outputLine{text: l, run: s}
		}
		return out
	}
	var out []outputLine
	for i, mr := range m.run.runs {
		if i > 0 {
			out = append(out, outputLine{})
		}
		out = append(out, outputLine{text: m.runHeader(mr), run: mr, header: true})
		if mr.note != "" && len(mr.lines) == 0 {
			out = append(out, outputLine{text: "  " + mr.note, run: mr})
		}
		for _, l := range mr.lines {
			out = append(out, outputLine{text: l, run: mr})
		}
	}
	return out
}

// runHeader renders a machine's section header.
func (m model) runHeader(mr *machineRun) string {
	return fmt.Sprintf("── %s %s  %s ──", getStatusIcon(mr.device), displayName(mr.device), runStatusText(mr))
}

// runStatusText renders one machine's state.
func runStatusText(mr *machineRun) string {
	switch mr.status {
	case runPending:
		return "… waiting"
	case runRunning:
		return fmt.Sprintf("● running %s", time.Since(mr.startedAt).Truncate(time.Second))
	case runSkipped:
		return "⊘ skipped"
	case runFailed:
		return "✗ failed"
	}
	if mr.killed {
		return "✗ stopped"
	}
	if mr.exitCode == 0 {
		return fmt.Sprintf("✓ exit 0 · %s", mr.elapsed.Truncate(100*time.Millisecond))
	}
	return fmt.Sprintf("✗ exit %d · %s", mr.exitCode, mr.elapsed.Truncate(100*time.Millisecond))
}

// outputStatus returns the short status shown in the Output title.
func (m model) outputStatus() string {
	if len(m.run.runs) == 0 {
		return ""
	}
	if s := m.run.single(); s != nil {
		if s.status == runRunning {
			return fmt.Sprintf("● running %s", m.run.elapsed.Truncate(time.Second))
		}
		return runStatusText(s)
	}
	done, failed, skipped := 0, 0, 0
	for _, mr := range m.run.runs {
		switch mr.status {
		case runDone:
			done++
			if mr.exitCode != 0 || mr.killed {
				failed++
			}
		case runFailed:
			done++
			failed++
		case runSkipped:
			skipped++
		}
	}
	total := len(m.run.runs) - skipped
	parts := []string{fmt.Sprintf("%d/%d done", done, total)}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", failed))
	}
	if skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d skipped", skipped))
	}
	if m.run.running() {
		parts = append(parts, fmt.Sprintf("● %s", m.run.elapsed.Truncate(time.Second)))
	} else {
		parts = append(parts, m.run.elapsed.Truncate(100*time.Millisecond).String())
	}
	return strings.Join(parts, " · ")
}

// ─── Output panel keys ────────────────────────────────────────────────────────

func (m model) handleOutputPanel(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	total := len(m.outputLines())
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
		if m.run.running() {
			m.stopRun()
			return m, nil
		}
		return m, tea.Quit
	case "q":
		return m, tea.Quit
	case "x":
		if m.run.running() {
			m.stopRun()
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
		if m.run.outputCursor < total-1 {
			m.run.outputCursor++
			m.run.follow = m.run.outputCursor == total-1
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
	case "}", "n":
		if m.run.multi {
			m.jumpToSection(+1)
			return m, nil
		}
		return m, m.copySelectedOutputItem(true)
	case "{", "p":
		if m.run.multi {
			m.jumpToSection(-1)
		}
		return m, nil
	case "c":
		return m, m.copySelectedOutputItem(false)
	case "N":
		return m, m.copySelectedOutputItem(true)
	case "C":
		if !m.run.running() {
			m.run.runs = nil
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

// jumpToSection moves the cursor to the next/previous machine header.
func (m *model) jumpToSection(dir int) {
	lines := m.outputLines()
	headers := []int{}
	for i, l := range lines {
		if l.header {
			headers = append(headers, i)
		}
	}
	if len(headers) == 0 {
		return
	}
	cur := m.run.outputCursor
	if dir > 0 {
		i := sort.SearchInts(headers, cur+1)
		if i < len(headers) {
			m.run.outputCursor = headers[i]
		}
	} else {
		i := sort.SearchInts(headers, cur) - 1
		if i >= 0 {
			m.run.outputCursor = headers[i]
		}
	}
	m.run.follow = false
	m.ensureOutputCursorVisible()
}

func (m *model) jumpToOutputEnd() {
	if n := len(m.outputLines()); n > 0 {
		m.run.outputCursor = n - 1
	} else {
		m.run.outputCursor = 0
	}
	m.run.follow = true
	m.ensureOutputCursorVisible()
}

// ─── Saved commands ───────────────────────────────────────────────────────────

// promptNewCommand asks for a name and a command, then saves. oldName is set
// when editing an existing entry.
func (m model) promptNewCommand(initial config.SavedCommand, oldName string) (tea.Model, tea.Cmd) {
	m = m.openRunPanel(focusRun)
	m.input = textInput{
		mode:   inputText,
		label:  "Command name",
		value:  initial.Name,
		cursor: len([]rune(initial.Name)),
		onSubmit: func(m model, name string) (tea.Model, tea.Cmd) {
			name = strings.TrimSpace(name)
			if name == "" {
				return m, nil
			}
			m.input = textInput{
				mode:   inputText,
				label:  "Command for \"" + name + "\"",
				hint:   "Use {{name}} placeholders to be asked for values at run time",
				value:  initial.Command,
				cursor: len([]rune(initial.Command)),
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
