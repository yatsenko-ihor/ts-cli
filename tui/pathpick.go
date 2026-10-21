package tui

import (
	"fmt"
	"os"
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
	pathNone pathKind = iota // an ordinary prompt
	pathAny                  // a file or a directory
	pathDirs                 // a directory
)

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
	name string
	dir  bool
	size int64
}

// fileBrowser lists one local directory at a time.
type fileBrowser struct {
	dir      string
	entries  []browserEntry
	cursor   int
	dirsOnly bool
	hidden   bool
	err      string
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
	b.cursor = 0
	b.entries = b.entries[:0]
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
	sort.SliceStable(b.entries, func(i, j int) bool {
		a, c := b.entries[i], b.entries[j]
		if a.dir != c.dir {
			return a.dir
		}
		return strings.ToLower(a.name) < strings.ToLower(c.name)
	})
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

// handleBrowser processes keys while the browser is open.
func (m model) handleBrowser(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	b := *m.browse
	b.entries = append([]browserEntry(nil), b.entries...)
	choose := func(path string) (tea.Model, tea.Cmd) {
		m.browse = nil
		m.input.value, m.input.cursor, m.input.note = path, len([]rune(path)), ""
		return m, nil
	}
	var selected *browserEntry
	if b.cursor >= 0 && b.cursor < len(b.entries) {
		selected = &b.entries[b.cursor]
	}

	switch msg.String() {
	case "esc", "ctrl+c", "q":
		m.browse = nil
		return m, nil
	case "up", "k":
		if b.cursor > 0 {
			b.cursor--
		}
	case "down", "j":
		if b.cursor < len(b.entries)-1 {
			b.cursor++
		}
	case "pgup":
		b.cursor = max(0, b.cursor-browserRows)
	case "pgdown":
		b.cursor = max(0, min(len(b.entries)-1, b.cursor+browserRows))
	case "home", "g":
		b.cursor = 0
	case "end", "G":
		b.cursor = max(0, len(b.entries)-1)
	case "left", "h", "backspace":
		b.load(filepath.Dir(b.dir))
	case "~":
		if home, err := os.UserHomeDir(); err == nil {
			b.load(home)
		}
	case ".":
		b.hidden = !b.hidden
		b.load(b.dir)
	case "right", "l", "enter":
		switch {
		case selected == nil:
		case selected.dir:
			b.load(filepath.Join(b.dir, selected.name))
		case msg.String() == "enter":
			return choose(filepath.Join(b.dir, selected.name))
		}
	case " ", "s":
		return choose(b.dir)
	}
	m.browse = &b
	return m, nil
}

// browserRows is how many entries are visible at once.
const browserRows = 12

// renderBrowser renders the browser as a popup.
func (m model) renderBrowser() string {
	b := m.browse
	var s strings.Builder
	s.WriteString(titleStyle.Render(fmt.Sprintf("Tailscale Devices (ts-cli v%s)", m.version)))
	s.WriteString("\n\n")

	width := 72
	if m.width > 0 && m.width < 90 {
		width = max(40, m.width-10)
	}
	inner := width - 6

	var c strings.Builder
	what := "file or directory"
	if b.dirsOnly {
		what = "directory"
	}
	c.WriteString(lipgloss.NewStyle().Bold(true).Render("Choose a " + what))
	c.WriteString("\n")
	c.WriteString(grayItalicStyle.Render(truncateLeft(b.dir, inner)))
	c.WriteString("\n\n")

	if len(b.entries) == 0 {
		c.WriteString(grayItalicStyle.Render("  (empty)"))
		c.WriteString("\n")
	}
	start := 0
	if b.cursor >= browserRows {
		start = b.cursor - browserRows + 1
	}
	end := min(len(b.entries), start+browserRows)
	for i := start; i < end; i++ {
		e := b.entries[i]
		name := entryName(e.name, e.dir)
		size := ""
		if !e.dir {
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
	if end < len(b.entries) {
		c.WriteString(grayItalicStyle.Render(fmt.Sprintf("  … %d more", len(b.entries)-end)))
		c.WriteString("\n")
	}
	if b.err != "" {
		c.WriteString("\n" + lipgloss.NewStyle().Foreground(cur.errc).Render(truncateForWidth(b.err, inner)))
		c.WriteString("\n")
	}
	c.WriteString("\n")
	help := "↑/↓ move • →/enter open • ←/⌫ up • space choose this directory • . hidden • ~ home • esc back"
	if !b.dirsOnly {
		help = "↑/↓ move • →/enter open or choose a file • ←/⌫ up • space choose this directory • . hidden • ~ home • esc back"
	}
	c.WriteString(grayItalicStyle.Render(truncateForWidth(help, inner)))

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
