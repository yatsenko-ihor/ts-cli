package tui

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Choosing a local path in a prompt: Tab completes what is typed (like a
// shell), Ctrl+O opens a small file browser. Both only read the local disk.

// pathKind says whether a text prompt asks for a local path.
type pathKind int

const (
	pathNone       pathKind = iota // an ordinary prompt
	pathAny                        // a local file or directory
	pathDirs                       // a local directory
	pathRemoteAny                  // a file or directory on the machine (browse only, no Tab)
	pathRemoteDirs                 // a directory on the machine
)

// remote reports whether the path is on the machine rather than on this computer.
func (k pathKind) remote() bool { return k == pathRemoteAny || k == pathRemoteDirs }

// dirsOnly reports whether only directories can be chosen.
func (k pathKind) dirsOnly() bool { return k == pathDirs || k == pathRemoteDirs }

const pathSeparators = "/" + string(os.PathSeparator)

// entryName is a directory entry as shown: directories end in "/".
func entryName(name string, dir bool) string {
	if dir {
		return name + "/"
	}
	return name
}

// completePath completes typed against the local disk. It returns the new
// text (the longest common start of all matches, a directory ending in "/")
// and the matching names for display. Hidden files are offered only when the
// typed name starts with a dot. A leading ~ is kept as typed.
func completePath(typed string, dirsOnly bool) (string, []string) {
	i := strings.LastIndexAny(typed, pathSeparators)
	dirTyped, prefix := typed[:i+1], typed[i+1:]
	dirReal := expandHome(dirTyped)
	if dirReal == "" {
		dirReal = "."
	}
	entries, err := os.ReadDir(dirReal)
	if err != nil {
		return typed, nil
	}

	type match struct {
		name string
		dir  bool
	}
	var matches []match
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, prefix) || (strings.HasPrefix(name, ".") && !strings.HasPrefix(prefix, ".")) {
			continue
		}
		isDir := e.IsDir()
		if !isDir && e.Type()&os.ModeSymlink != 0 {
			if st, err := os.Stat(filepath.Join(dirReal, name)); err == nil && st.IsDir() {
				isDir = true // a symlink to a directory behaves as one
			}
		}
		if dirsOnly && !isDir {
			continue
		}
		matches = append(matches, match{name, isDir})
	}
	switch len(matches) {
	case 0:
		return typed, nil
	case 1:
		return dirTyped + entryName(matches[0].name, matches[0].dir), []string{entryName(matches[0].name, matches[0].dir)}
	}

	common := matches[0].name
	shown := make([]string, 0, len(matches))
	for _, m := range matches {
		for !strings.HasPrefix(m.name, common) {
			common = common[:len(common)-1]
		}
		shown = append(shown, entryName(m.name, m.dir))
	}
	return dirTyped + common, shown
}

// completionNote is the line shown under the prompt after Tab.
func completionNote(names []string) string {
	const max = 8
	if len(names) <= max {
		return strings.Join(names, "  ")
	}
	return strings.Join(names[:max], "  ") + fmt.Sprintf("  … (+%d)", len(names)-max)
}

// ─── Browser ─────────────────────────────────────────────────────────────────

type browserEntry struct {
	name    string
	dir     bool
	size    int64
	special bool // "." (choose this directory) or ".." (go up)
}

// label is how the entry is shown in the list.
func (e browserEntry) label() string {
	switch {
	case e.special && e.name == ".":
		return ". (choose this directory)"
	case e.special:
		return ".. (up)"
	}
	return entryName(e.name, e.dir)
}

// fileBrowser lists one local directory at a time.
type fileBrowser struct {
	dir      string
	entries  []browserEntry
	cursor   int
	dirsOnly bool
	hidden   bool
	err      string

	// filter narrows the listing to names containing it (typed after "/").
	filter    string
	filtering bool // keys are typed into the filter

	// Browsing a directory on a machine (remotebrowse.go): listings arrive
	// asynchronously, so keys are ignored while one is loading.
	remote   *remoteSource
	loading  bool
	reqID    int      // the request whose answer is wanted; older answers are dropped
	first    bool     // the first listing is pending (a failing start falls back to home)
	startDir string   // where the first listing was asked for
	raw      []string // the last remote listing, to re-filter without asking again
}

// shown is the listing after the filter: "." and ".." always stay, other
// entries must contain the filter (case-insensitively).
func (b *fileBrowser) shown() []browserEntry {
	if b.filter == "" {
		return b.entries
	}
	needle := strings.ToLower(b.filter)
	out := make([]browserEntry, 0, len(b.entries))
	for _, e := range b.entries {
		if e.special || strings.Contains(strings.ToLower(e.name), needle) {
			out = append(out, e)
		}
	}
	return out
}

// goTo shows dir: at once for the local disk, through a request for a machine.
func (b *fileBrowser) goTo(dir string) tea.Cmd {
	if b.remote == nil {
		b.load(dir)
		return nil
	}
	b.loading = true
	b.reqID++
	return remoteListCmd(b.reqID, *b.remote, dir)
}

// parent is the directory above the one shown (the root is its own parent).
func (b *fileBrowser) parent() string {
	if b.remote != nil {
		return path.Dir(b.dir)
	}
	return filepath.Dir(b.dir)
}

// join is the path of the entry name in the directory shown.
func (b *fileBrowser) join(name string) string {
	if b.remote != nil {
		return path.Join(b.dir, name)
	}
	return filepath.Join(b.dir, name)
}

// load lists dir (directories first, then files, each case-insensitively).
// On failure the previous listing stays and err says why.
func (b *fileBrowser) load(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		b.err = err.Error()
		return
	}
	b.err = ""
	b.dir = dir
	b.filter, b.filtering = "", false
	b.entries = append(b.entries[:0], browserEntry{name: ".", dir: true, special: true}, browserEntry{name: "..", dir: true, special: true})
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") && !b.hidden {
			continue
		}
		isDir, size := e.IsDir(), int64(0)
		if info, err := os.Stat(filepath.Join(dir, name)); err == nil { // follows symlinks
			isDir, size = info.IsDir(), info.Size()
		}
		if b.dirsOnly && !isDir {
			continue
		}
		b.entries = append(b.entries, browserEntry{name: name, dir: isDir, size: size})
	}
	real := b.entries[2:] // "." and ".." stay on top
	sort.SliceStable(real, func(i, j int) bool {
		a, c := real[i], real[j]
		if a.dir != c.dir {
			return a.dir
		}
		return strings.ToLower(a.name) < strings.ToLower(c.name)
	})
	b.cursor = min(2, len(b.entries)-1) // the first real entry
}

// newBrowser starts in the directory the typed text points into: the text
// itself when it is a directory, else its parent, else the working directory.
func newBrowser(typed string, dirsOnly bool) *fileBrowser {
	b := &fileBrowser{dirsOnly: dirsOnly}
	start := expandHome(typed)
	if start != "" {
		if abs, err := filepath.Abs(start); err == nil {
			start = abs
		}
		if st, err := os.Stat(start); err != nil || !st.IsDir() {
			start = filepath.Dir(start)
		}
	}
	if start == "" {
		start, _ = os.Getwd()
	}
	if b.load(start); b.err != "" {
		wd, _ := os.Getwd()
		b.load(wd)
	}
	return b
}

// editFilter types into the filter: runes and space add, Backspace deletes,
// Enter keeps the filter and returns to the keys, Esc clears it.
func (b *fileBrowser) editFilter(msg tea.KeyMsg) {
	switch msg.String() {
	case "esc":
		b.filter, b.filtering = "", false
	case "enter":
		b.filtering = false
		return
	case "backspace":
		if r := []rune(b.filter); len(r) > 0 {
			b.filter = string(r[:len(r)-1])
		}
	default:
		switch msg.Type {
		case tea.KeyRunes:
			b.filter += string(msg.Runes)
		case tea.KeySpace:
			b.filter += " "
		}
	}
	b.cursor = min(2, len(b.shown())-1) // the first match
}

// openBrowser opens the browser for the current path prompt: the local disk
// at once, a machine through a listing request.
func (m model) openBrowser() (tea.Model, tea.Cmd) {
	if m.input.path.remote() {
		return m.openRemoteBrowser()
	}
	m.browse = newBrowser(m.input.value, m.input.path.dirsOnly())
	return m, nil
}

// handleBrowser processes keys while the browser is open.
func (m model) handleBrowser(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	b := *m.browse
	b.entries = append([]browserEntry(nil), b.entries...)
	choose := func(chosen string) (tea.Model, tea.Cmd) {
		m.browse = nil
		m.input.value, m.input.cursor, m.input.note = chosen, len([]rune(chosen)), ""
		return m, nil
	}
	list := b.shown()
	var selected *browserEntry
	if b.cursor >= 0 && b.cursor < len(list) {
		selected = &list[b.cursor]
	}
	var cmd tea.Cmd

	key := msg.String()
	if b.loading && key != "esc" && key != "ctrl+c" && key != "q" {
		return m, nil // a listing is on its way
	}
	if b.filtering && key != "ctrl+c" {
		switch key {
		case "up", "down", "pgup", "pgdown", "home", "end":
			// moving through the matches while typing
		default:
			b.editFilter(msg)
			m.browse = &b
			return m, nil
		}
	}
	switch key {
	case "ctrl+c", "q":
		m.browse = nil
		return m, nil
	case "esc":
		if b.filter == "" {
			m.browse = nil
			return m, nil
		}
		b.filter = "" // the first Esc clears the filter
		b.cursor = min(2, len(b.entries)-1)
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
		b.cursor = max(0, b.cursor-m.browserRows())
	case "pgdown":
		b.cursor = max(0, min(len(list)-1, b.cursor+m.browserRows()))
	case "home", "g":
		b.cursor = 0
	case "end", "G":
		b.cursor = max(0, len(list)-1)
	case "left", "h", "backspace":
		cmd = b.goTo(b.parent())
	case "~":
		if b.remote != nil {
			cmd = b.goTo("")
		} else if home, err := os.UserHomeDir(); err == nil {
			b.load(home)
		}
	case ".":
		b.hidden = !b.hidden
		if b.remote != nil {
			b.applyRemote(b.dir, b.raw)
		} else {
			b.load(b.dir)
		}
	case "right", "l", "enter":
		switch {
		case selected == nil:
		case selected.special && selected.name == ".":
			if key == "enter" {
				return choose(b.dir)
			}
		case selected.special:
			cmd = b.goTo(b.parent())
		case selected.dir:
			cmd = b.goTo(b.join(selected.name))
		case key == "enter":
			return choose(b.join(selected.name))
		}
	case " ", "s":
		return choose(b.dir)
	}
	m.browse = &b
	return m, cmd
}

// browserRows is how many entries are visible at once: the browser grows with
// the terminal's height (12 when the height is unknown).
func (m model) browserRows() int {
	if m.height <= 0 {
		return 12
	}
	return max(5, min(40, m.height-16))
}

// browserWidth is the popup's width: it stretches with the terminal's width.
func (m model) browserWidth() int {
	if m.width <= 0 {
		return 72
	}
	return max(40, min(160, m.width-8))
}

// renderBrowser renders the browser as a popup.
func (m model) renderBrowser() string {
	b := m.browse
	var s strings.Builder
	s.WriteString(titleStyle.Render(fmt.Sprintf("Tailscale Devices (ts-cli v%s)", m.version)))
	s.WriteString("\n\n")

	width := m.browserWidth()
	inner := width - 6
	rows := m.browserRows()

	var c strings.Builder
	what := "file or directory"
	if b.dirsOnly {
		what = "directory"
	}
	title := "Choose a " + what
	if b.remote != nil {
		title += " on " + shortHost(displayName(b.remote.device))
	}
	c.WriteString(lipgloss.NewStyle().Bold(true).Render(truncateForWidth(title, inner)))
	c.WriteString("\n")
	shown := b.dir
	if b.loading {
		shown = "loading…"
		if b.dir != "" {
			shown = b.dir + "  (loading…)"
		}
	}
	c.WriteString(grayItalicStyle.Render(truncateLeft(shown, inner)))
	c.WriteString("\n\n")

	list := b.shown()
	if b.filtering || b.filter != "" {
		prompt := "/" + b.filter
		if b.filtering {
			prompt += "▌"
		}
		c.WriteString(lipgloss.NewStyle().Foreground(cur.info).Render(truncateForWidth(prompt, inner-12)))
		c.WriteString(grayItalicStyle.Render(fmt.Sprintf("  %d match(es)", len(list)-2)))
		c.WriteString("\n\n")
	}
	if len(list) == 2 && !b.loading {
		empty := "  (empty)"
		if b.filter != "" {
			empty = "  (no match)"
		}
		c.WriteString(grayItalicStyle.Render(empty))
		c.WriteString("\n")
	}
	start := 0
	if b.cursor >= rows {
		start = b.cursor - rows + 1
	}
	end := min(len(list), start+rows)
	for i := start; i < end; i++ {
		e := list[i]
		name := e.label()
		size := ""
		if !e.dir && e.size >= 0 { // a machine's listing has no sizes
			size = humanBytes(e.size)
		}
		line := padRight(truncateForWidth(name, inner-14), inner-14) + fmt.Sprintf("%10s", size)
		if i == b.cursor {
			c.WriteString(selectedStyle.Render("▸ " + line))
		} else {
			c.WriteString(normalStyle.Render("  " + line))
		}
		c.WriteString("\n")
	}
	if end < len(list) {
		c.WriteString(grayItalicStyle.Render(fmt.Sprintf("  … %d more", len(list)-end)))
		c.WriteString("\n")
	}
	if b.err != "" {
		c.WriteString("\n" + lipgloss.NewStyle().Foreground(cur.errc).Render(truncateForWidth(b.err, inner)))
		c.WriteString("\n")
	}
	c.WriteString("\n")
	help := "↑/↓ move • →/enter open • ←/⌫ up • enter on . or space chooses this directory • / filter • . hidden • ~ home • esc back"
	if !b.dirsOnly {
		help = "↑/↓ move • →/enter open or choose a file • ←/⌫ up • enter on . or space chooses this directory • / filter • . hidden • ~ home • esc back"
	}
	c.WriteString(grayItalicStyle.Width(inner).Render(help)) // wraps; nothing is cut off

	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cur.borderFocus).Padding(1, 2).Width(width)
	s.WriteString(box.Render(c.String()))
	return s.String()
}

// truncateLeft keeps the end of a long path, which is the part that matters.
func truncateLeft(s string, width int) string {
	r := []rune(s)
	if width <= 1 || lipgloss.Width(s) <= width {
		return s
	}
	for len(r) > 0 && lipgloss.Width("…"+string(r)) > width {
		r = r[1:]
	}
	return "…" + string(r)
}
