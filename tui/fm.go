package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/yatsenko-ihor/ts-cli/client"
	"github.com/yatsenko-ihor/ts-cli/scripts"
)

// Two-pane file manager (key b): this computer on the left, the highlighted
// machine on the right. The panes are the same fileBrowser the path prompts
// use. Copying leaves the manager and runs as an ordinary scp job (so the
// credential flow, the Output panel and the summary line work as usual); b
// opens the manager again in the same directories. Creating, renaming and
// deleting on the machine use the saved login only and never prompt.

const (
	fmLocal  = 0
	fmRemote = 1
)

// fileManager is the state of the open manager.
type fileManager struct {
	panes  [2]fileBrowser
	focus  int
	device client.Device
	status string // last result line
	err    string
}

// fmListMsg is the answer to a listing of the remote pane.
type fmListMsg struct{ msg remoteListMsg }

// fmOpMsg is the result of a create/rename/delete.
type fmOpMsg struct {
	what string
	err  error
}

// fmRemoteExec runs a command on the machine; replaced in tests.
var fmRemoteExec = func(src remoteSource, command string) (string, error) {
	return runSSHOnce(src.device, src.username, src.password, "sh -c "+scripts.ShellQuote(command), 30*time.Second)
}

func fmListCmd(id int, src remoteSource, dir string) tea.Cmd {
	return func() tea.Msg {
		d, lines, err := remoteList(src, dir)
		return fmListMsg{msg: remoteListMsg{id: id, dir: d, lines: lines, err: err}}
	}
}

// openFileManager opens the manager for the highlighted (or the single marked) machine.
func (m model) openFileManager() (tea.Model, tea.Cmd) {
	targets := m.targets()
	if len(targets) != 1 {
		m.notify.sshError = fmt.Errorf("the file manager works on one machine; clear the marks (esc) first")
		return m, nil
	}
	d := targets[0]
	return m.ensureAccount(d, func(m model) (tea.Model, tea.Cmd) { return m.showFileManager(d) })
}

// showFileManager opens the manager once the account is right.
func (m model) showFileManager(d client.Device) (tea.Model, tea.Cmd) {
	switch {
	case m.usernameForDevice(d) == "":
		m.notify.sshError = fmt.Errorf("no username saved for %s; connect once with s to set one", displayName(d))
		return m, nil
	case len(d.Addresses) == 0:
		m.notify.sshError = fmt.Errorf("%s has no IP address", displayName(d))
		return m, nil
	}
	src := remoteSource{device: d, username: m.usernameForDevice(d), password: m.savedPassword(machineKey(d))}
	last := m.fmDirs[machineKey(d)]
	remoteDir := last[fmRemote]
	if remoteDir != "" {
		remoteDir = strings.TrimSuffix(remoteDir, "/") + "/" // "/srv" would start in its parent
	}
	f := &fileManager{device: d}
	f.panes[fmLocal] = *newBrowser(last[fmLocal], false)
	f.panes[fmRemote] = fileBrowser{remote: &src, loading: true, reqID: 1, first: true, startDir: remoteStart(remoteDir)}
	m.fm = f
	m.notify.sshError = nil
	return m, fmListCmd(1, src, f.panes[fmRemote].startDir)
}

// handleFMList applies the remote pane's listing; like the path browser, a
// failing first listing falls back to the home directory.
func (m model) handleFMList(msg fmListMsg) (tea.Model, tea.Cmd) {
	if m.fm == nil || msg.msg.id != m.fm.panes[fmRemote].reqID {
		return m, nil
	}
	f := *m.fm
	b := f.panes[fmRemote]
	b.entries = append([]browserEntry(nil), b.entries...)
	b.loading = false
	if err := msg.msg.err; err != nil {
		if b.first && b.startDir != "" {
			b.first, b.startDir, b.loading = false, "", true
			b.reqID++
			f.panes[fmRemote] = b
			m.fm = &f
			return m, fmListCmd(b.reqID, *b.remote, "")
		}
		b.first = false
		f.err = err.Error()
	} else {
		b.first, f.err = false, ""
		b.applyRemote(msg.msg.dir, msg.msg.lines)
	}
	f.panes[fmRemote] = b
	m.fm = &f
	return m, nil
}

// fmSelected is the entry under the cursor of pane i (nil when none).
func (f *fileManager) fmSelected(i int) *browserEntry {
	list := f.panes[i].shown()
	if c := f.panes[i].cursor; c >= 0 && c < len(list) {
		e := list[c]
		return &e
	}
	return nil
}

// fmGo moves pane i to dir.
func (f *fileManager) fmGo(i int, dir string) tea.Cmd {
	b := &f.panes[i]
	if i == fmLocal {
		b.load(dir)
		if b.err != "" {
			f.err, b.err = b.err, ""
		} else {
			f.err = ""
		}
		return nil
	}
	b.loading = true
	b.reqID++
	return fmListCmd(b.reqID, *b.remote, dir)
}

// closeFM leaves the manager and remembers where both panes were.
func (m model) closeFM() model {
	if m.fm != nil {
		if m.fmDirs == nil {
			m.fmDirs = map[string][2]string{}
		}
		m.fmDirs[machineKey(m.fm.device)] = [2]string{m.fm.panes[fmLocal].dir, m.fm.panes[fmRemote].dir}
	}
	m.fm = nil
	return m
}

// handleFM processes keys of the manager.
func (m model) handleFM(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := *m.fm
	for i := range f.panes {
		f.panes[i].entries = append([]browserEntry(nil), f.panes[i].entries...)
	}
	m.fm = &f
	b := &f.panes[f.focus]
	key := msg.String()

	if b.loading && key != "esc" && key != "q" && key != "ctrl+c" && key != "tab" {
		return m, nil // a listing is on its way
	}
	if b.filtering && key != "ctrl+c" {
		switch key {
		case "up", "down", "pgup", "pgdown", "home", "end", "tab":
		default:
			b.editFilter(msg)
			return m, nil
		}
	}
	list := b.shown()
	sel := f.fmSelected(f.focus)
	rows := m.fmRows()
	var cmd tea.Cmd
	f.status = ""

	switch key {
	case "ctrl+c", "q":
		return m.closeFM(), nil
	case "esc":
		if b.filter != "" {
			b.filter = ""
			b.cursor = min(2, len(b.entries)-1)
			return m, nil
		}
		return m.closeFM(), nil
	case "tab":
		f.focus = 1 - f.focus
	case "/":
		b.filtering = true
	case "up", "k":
		if b.cursor > 0 {
			b.cursor--
		}
	case "down", "j":
		if b.cursor < len(list)-1 {
			b.cursor++
		}
	case "pgup":
		b.cursor = max(0, b.cursor-rows)
	case "pgdown":
		b.cursor = max(0, min(len(list)-1, b.cursor+rows))
	case "home", "g":
		b.cursor = 0
	case "end", "G":
		b.cursor = max(0, len(list)-1)
	case "left", "h", "backspace":
		cmd = f.fmGo(f.focus, b.parent())
	case "~":
		if f.focus == fmRemote {
			cmd = f.fmGo(fmRemote, "")
		} else if home, err := os.UserHomeDir(); err == nil {
			cmd = f.fmGo(fmLocal, home)
		}
	case ".":
		b.hidden = !b.hidden
		if f.focus == fmRemote {
			b.applyRemote(b.dir, b.raw)
		} else {
			b.load(b.dir)
		}
	case "r":
		cmd = f.fmGo(f.focus, b.dir)
	case "right", "l", "enter":
		switch {
		case sel == nil:
		case sel.special && sel.name == ".":
		case sel.special:
			cmd = f.fmGo(f.focus, b.parent())
		case sel.dir:
			cmd = f.fmGo(f.focus, b.join(sel.name))
		}
	case "c", "f5":
		return m.fmCopy()
	case "n", "f7":
		return m.fmPromptMkdir()
	case "R", "f6":
		return m.fmPromptRename()
	case "d", "delete", "f8":
		return m.fmPromptDelete()
	}
	return m, cmd
}

// fmCopy runs an scp job for the selected entry into the other pane's directory.
func (m model) fmCopy() (tea.Model, tea.Cmd) {
	f := m.fm
	sel := f.fmSelected(f.focus)
	if sel == nil || sel.special {
		f.status = "Select a file or directory to copy"
		return m, nil
	}
	local, remote := f.panes[fmLocal], f.panes[fmRemote]
	if remote.dir == "" || remote.loading {
		f.status = "The machine's directory is not loaded yet"
		return m, nil
	}
	spec := copySpec{local: local.dir, remote: remote.dir, download: true}
	if f.focus == fmLocal {
		spec = copySpec{local: local.join(sel.name), remote: remote.dir}
	} else {
		spec.remote = remote.join(sel.name)
	}
	m = m.closeFM()
	m.list.marked = nil // the copy goes to the machine of the manager only
	for i, d := range m.list.filteredDevices {
		if machineKey(d) == machineKey(f.device) {
			m.list.cursor = i
			break
		}
	}
	return m.launch(sshJob{action: sshActionCopy, copy: spec})
}

// fmPane returns the name of the focused pane's side.
func (f *fileManager) fmName() string {
	if f.focus == fmRemote {
		return shortHost(displayName(f.device))
	}
	return "this computer"
}

func (m model) fmPromptMkdir() (tea.Model, tea.Cmd) {
	f := m.fm
	focus, dir := f.focus, f.panes[f.focus].dir
	m.input = textInput{
		mode: inputText, label: "New directory in " + f.fmName(), hint: "a name, or a path relative to " + dir,
		onSubmit: func(m model, v string) (tea.Model, tea.Cmd) {
			m.input = textInput{}
			name := strings.TrimSpace(v)
			if name == "" {
				return m, nil
			}
			return m.fmRunOp(focus, "Created "+name, func(path func(string) string, run func(string) error) error {
				if focus == fmLocal {
					return os.MkdirAll(path(name), 0o755)
				}
				return run("mkdir -p -- " + scripts.ShellQuote(path(name)))
			})
		},
	}
	return m, nil
}

func (m model) fmPromptRename() (tea.Model, tea.Cmd) {
	f := m.fm
	sel := f.fmSelected(f.focus)
	if sel == nil || sel.special {
		f.status = "Select a file or directory to rename"
		return m, nil
	}
	focus, old := f.focus, sel.name
	m.input = textInput{
		mode: inputText, label: "Rename " + old, value: old, cursor: len([]rune(old)), hint: "the new name (in the same directory)",
		onSubmit: func(m model, v string) (tea.Model, tea.Cmd) {
			m.input = textInput{}
			name := strings.TrimSpace(v)
			if name == "" || name == old || strings.ContainsAny(name, "/"+string(os.PathSeparator)) {
				if name != "" && name != old {
					m.fm.err = "the new name must not contain a path separator"
				}
				return m, nil
			}
			return m.fmRunOp(focus, "Renamed "+old+" to "+name, func(path func(string) string, run func(string) error) error {
				if focus == fmLocal {
					return os.Rename(path(old), path(name))
				}
				return run("mv -n -- " + scripts.ShellQuote(path(old)) + " " + scripts.ShellQuote(path(name)))
			})
		},
	}
	return m, nil
}

func (m model) fmPromptDelete() (tea.Model, tea.Cmd) {
	f := m.fm
	sel := f.fmSelected(f.focus)
	if sel == nil || sel.special {
		f.status = "Select a file or directory to delete"
		return m, nil
	}
	focus, name := f.focus, sel.name
	m.input = textInput{
		mode: inputText, label: "Delete " + name + " on " + f.fmName() + "?", hint: "Type yes to delete it for good",
		onSubmit: func(m model, v string) (tea.Model, tea.Cmd) {
			m.input = textInput{}
			if strings.TrimSpace(v) != "yes" {
				m.fm.status = "Not deleted"
				return m, nil
			}
			return m.fmRunOp(focus, "Deleted "+name, func(path func(string) string, run func(string) error) error {
				if focus == fmLocal {
					return os.RemoveAll(path(name))
				}
				return run("rm -rf -- " + scripts.ShellQuote(path(name)))
			})
		},
	}
	return m, nil
}

// fmRunOp runs a create/rename/delete in pane i's directory in the background.
func (m model) fmRunOp(i int, done string, op func(path func(string) string, run func(string) error) error) (tea.Model, tea.Cmd) {
	f := *m.fm
	b := f.panes[i]
	if i == fmRemote && b.remote == nil {
		return m, nil
	}
	src := b.remote
	path := func(name string) string {
		if filepath.IsAbs(name) || (i == fmRemote && strings.HasPrefix(name, "/")) {
			return name
		}
		return b.join(name)
	}
	f.status = "Working…"
	m.fm = &f
	return m, func() tea.Msg {
		run := func(command string) error {
			out, err := fmRemoteExec(*src, command)
			if err != nil {
				return fmt.Errorf("%s", firstLine(out, err))
			}
			return nil
		}
		return fmOpMsg{what: done, err: op(path, run)}
	}
}

// handleFMOp shows the result and reloads both panes.
func (m model) handleFMOp(msg fmOpMsg) (tea.Model, tea.Cmd) {
	if m.fm == nil {
		return m, nil
	}
	f := *m.fm
	for i := range f.panes {
		f.panes[i].entries = append([]browserEntry(nil), f.panes[i].entries...)
	}
	if msg.err != nil {
		f.err, f.status = msg.err.Error(), ""
		m.fm = &f
		return m, nil
	}
	f.err, f.status = "", "✓ "+msg.what
	cmds := []tea.Cmd{f.fmGo(fmLocal, f.panes[fmLocal].dir), f.fmGo(fmRemote, f.panes[fmRemote].dir)}
	f.err, f.status = "", "✓ "+msg.what
	m.fm = &f
	return m, tea.Batch(cmds...)
}

// fmRows is how many entries each pane shows.
func (m model) fmRows() int {
	if m.height <= 0 {
		return 12
	}
	return max(5, min(60, m.height-14))
}

// renderFM draws the two panes side by side.
func (m model) renderFM() string {
	f := m.fm
	var s strings.Builder
	s.WriteString(titleStyle.Render(fmt.Sprintf("Files · %s (ts-cli v%s)", shortHost(displayName(f.device)), m.version)))
	s.WriteString("\n\n")

	total := m.width
	if total <= 0 {
		total = 100
	}
	paneW := max(30, (total-4)/2)
	rows := m.fmRows()
	titles := [2]string{"This computer", shortHost(displayName(f.device))}
	left := m.fmPane(&f.panes[fmLocal], titles[0], paneW, rows, f.focus == fmLocal)
	right := m.fmPane(&f.panes[fmRemote], titles[1], paneW, rows, f.focus == fmRemote)
	s.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right))
	s.WriteString("\n")
	switch {
	case f.err != "":
		s.WriteString(lipgloss.NewStyle().Foreground(cur.errc).Render(truncateForWidth("✗ "+f.err, total-2)))
	case f.status != "":
		s.WriteString(infoStyle.Render(truncateForWidth(f.status, total-2)))
	}
	s.WriteString("\n")
	s.WriteString(helpStyle.Render(truncateForWidth("tab switch pane • ↑/↓ move • →/enter open • ←/⌫ up • c copy to the other pane • n new dir • R rename • d delete • / filter • . hidden • r refresh • esc close", total-2)))
	return s.String()
}

func (m model) fmPane(b *fileBrowser, title string, width, rows int, focused bool) string {
	inner := width - 6
	var c strings.Builder
	dir := b.dir
	if b.loading {
		dir = "loading…"
	}
	c.WriteString(lipgloss.NewStyle().Bold(true).Render(truncateForWidth(title, inner)))
	c.WriteString("\n")
	c.WriteString(grayItalicStyle.Render(truncateLeft(dir, inner)))
	c.WriteString("\n")
	list := b.shown()
	if b.filtering || b.filter != "" {
		prompt := "/" + b.filter
		if b.filtering {
			prompt += "▌"
		}
		c.WriteString(lipgloss.NewStyle().Foreground(cur.info).Render(truncateForWidth(prompt, inner)))
	}
	c.WriteString("\n")
	start := 0
	if b.cursor >= rows {
		start = b.cursor - rows + 1
	}
	end := min(len(list), start+rows)
	for i := start; i < end; i++ {
		e := list[i]
		size := ""
		if !e.dir && e.size >= 0 {
			size = humanBytes(e.size)
		}
		label := e.label()
		if e.special && e.name == "." {
			label = "."
		}
		line := padRight(truncateForWidth(label, inner-12), inner-12) + fmt.Sprintf("%10s", size)
		switch {
		case i == b.cursor && focused:
			c.WriteString(selectedStyle.Render("▸ " + line))
		case i == b.cursor:
			c.WriteString(normalStyle.Render("› " + line))
		default:
			c.WriteString(normalStyle.Render("  " + line))
		}
		c.WriteString("\n")
	}
	for i := end - start; i < rows; i++ {
		c.WriteString("\n") // keep both panes the same height
	}
	border := cur.border
	if focused {
		border = cur.borderFocus
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).Padding(0, 1).Width(width)
	return box.Render(strings.TrimRight(c.String(), "\n"))
}
