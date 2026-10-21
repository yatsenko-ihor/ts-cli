package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Requirements-based tests for choosing a local path (verification/requirements/pathpick.md).

// tree builds:  root/{alpha/ (inner.txt), alphabet.txt, Beta/, beta.log, .hidden, .dotdir/, zeta.bin}
func tree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"alpha", "Beta", ".dotdir"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for path, size := range map[string]int{"alpha/inner.txt": 3, "alphabet.txt": 10, "beta.log": 2048, ".hidden": 1, "zeta.bin": 5} {
		if err := os.WriteFile(filepath.Join(root, path), make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(root, "Beta"), filepath.Join(root, "link-to-beta")); err != nil {
		t.Fatal(err)
	}
	return root
}

// Verifies: REQ-PPK-001
func TestCompletePath(t *testing.T) {
	root := tree(t)
	p := func(s string) string { return root + "/" + s }
	cases := []struct {
		name, typed string
		dirsOnly    bool
		want        string
		wantNames   []string
	}{
		{"unique file", p("zet"), false, p("zeta.bin"), []string{"zeta.bin"}},
		{"unique directory", p("Be"), false, p("Beta/"), []string{"Beta/"}},
		{"no match leaves the text", p("nope"), false, p("nope"), nil},
		{"empty prefix lists the directory, text unchanged", root + "/", false, root + "/", []string{"Beta/", "alpha/", "alphabet.txt", "beta.log", "link-to-beta/", "zeta.bin"}},
		{"directories only", p("al"), true, p("alpha/"), []string{"alpha/"}},
		{"symlink to a directory counts as a directory", p("link"), true, p("link-to-beta/"), []string{"link-to-beta/"}},
		{"missing directory leaves the text", "/no/such/dir/x", false, "/no/such/dir/x", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, names := completePath(c.typed, c.dirsOnly)
			if got != c.want {
				t.Fatalf("completed = %q, want %q", got, c.want)
			}
			if c.wantNames != nil && strings.Join(names, ",") != strings.Join(c.wantNames, ",") {
				t.Fatalf("names = %v, want %v", names, c.wantNames)
			}
			if c.wantNames == nil && len(names) != 0 {
				t.Fatalf("unexpected names %v", names)
			}
		})
	}

	// several matches complete to their common start and are all listed
	if got, names := completePath(p("alp"), false); got != p("alpha") || strings.Join(names, ",") != "alpha/,alphabet.txt" {
		t.Fatalf("common prefix: %q %v", got, names)
	}
	// a dot prefix offers hidden entries
	if got, names := completePath(p("."), false); got != p(".") || strings.Join(names, ",") != ".dotdir/,.hidden" {
		t.Fatalf("hidden entries: %q %v", got, names)
	}
	// hidden entries stay out without a dot
	if _, names := completePath(root+"/", false); strings.Contains(strings.Join(names, ","), ".hidden") {
		t.Fatal("hidden files must not be offered without a dot")
	}
	// the typed ~ is kept
	t.Setenv("HOME", root)
	if got, _ := completePath("~/zet", false); got != "~/zeta.bin" {
		t.Fatalf("~ must stay as typed: %q", got)
	}
	// no directory part: the working directory
	wd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	if got, _ := completePath("zet", false); got != "zeta.bin" {
		t.Fatalf("relative completion: %q", got)
	}
}

// Verifies: REQ-PPK-002
func TestCompletionNote(t *testing.T) {
	if got := completionNote([]string{"a/", "b"}); got != "a/  b" {
		t.Fatalf("note = %q", got)
	}
	many := []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"}
	if got := completionNote(many); got != "1  2  3  4  5  6  7  8  … (+2)" {
		t.Fatalf("note = %q", got)
	}
	if got := completionNote(many[:8]); strings.Contains(got, "+") {
		t.Fatal("exactly eight need no ellipsis")
	}
}

func pathPrompt(t *testing.T, kind pathKind, value string) model {
	t.Helper()
	return model{input: textInput{mode: inputText, label: "x", value: value, cursor: len(value), path: kind}}
}

// Verifies: REQ-PPK-003
func TestTabCompletesOnlyPathPrompts(t *testing.T) {
	root := tree(t)
	m := pathPrompt(t, pathAny, root+"/zet")
	updated, _ := m.handleTextPrompt(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(model)
	if m.input.value != root+"/zeta.bin" || m.input.cursor != len([]rune(m.input.value)) || m.input.note != "zeta.bin" {
		t.Fatalf("input = %+v", m.input)
	}
	// typing clears the candidate line
	updated, _ = m.handleTextPrompt(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if updated.(model).input.note != "" {
		t.Fatal("editing must clear the note")
	}
	// the dirs-only kind offers directories only
	m = pathPrompt(t, pathDirs, root+"/al")
	updated, _ = m.handleTextPrompt(tea.KeyMsg{Type: tea.KeyTab})
	if got := updated.(model).input.value; got != root+"/alpha/" {
		t.Fatalf("dirs only: %q", got)
	}
	// an ordinary prompt ignores Tab and Ctrl+O
	m = pathPrompt(t, pathNone, root+"/zet")
	updated, _ = m.handleTextPrompt(tea.KeyMsg{Type: tea.KeyTab})
	if updated.(model).input.value != root+"/zet" {
		t.Fatal("Tab does nothing in an ordinary prompt")
	}
	updated, _ = m.handleTextPrompt(tea.KeyMsg{Type: tea.KeyCtrlO})
	if updated.(model).browse != nil {
		t.Fatal("no browser in an ordinary prompt")
	}
}

// Verifies: REQ-PPK-004
func TestBrowserListsDirectoriesFirstAndStartsWhereTheTextPoints(t *testing.T) {
	root := tree(t)
	names := func(b *fileBrowser) string {
		var out []string
		for _, e := range b.entries {
			out = append(out, e.label())
		}
		return strings.Join(out, ",")
	}
	b := newBrowser(root, false)
	if b.dir != root || names(b) != ". (choose this directory),.. (up),alpha/,Beta/,link-to-beta/,alphabet.txt,beta.log,zeta.bin" {
		t.Fatalf("dir %s entries %s", b.dir, names(b))
	}
	if b.cursor != 2 {
		t.Fatalf("the cursor starts on the first real entry, got %d", b.cursor)
	}
	if b.entries[5].size != 10 || b.entries[4].dir != true {
		t.Fatalf("sizes and symlinked directories: %+v", b.entries)
	}
	if got := names(newBrowser(root, true)); got != ". (choose this directory),.. (up),alpha/,Beta/,link-to-beta/" {
		t.Fatalf("dirs only: %s", got)
	}
	// starts: a directory, a file (its parent), a missing path (its parent), empty (cwd)
	if newBrowser(filepath.Join(root, "alpha"), false).dir != filepath.Join(root, "alpha") {
		t.Fatal("a directory opens itself")
	}
	if newBrowser(filepath.Join(root, "zeta.bin"), false).dir != root {
		t.Fatal("a file opens its directory")
	}
	if newBrowser(filepath.Join(root, "missing"), false).dir != root {
		t.Fatal("a missing path opens its parent")
	}
	wd, _ := os.Getwd()
	if newBrowser("", false).dir != wd {
		t.Fatal("empty text opens the working directory")
	}
	// nothing readable up the chain: fall back to the working directory
	if got := newBrowser("/proc/does/not/exist/at/all/x", false); got.err != "" || got.dir == "" {
		t.Fatalf("fallback: %+v", got)
	}
	// relative text is resolved against the working directory
	t.Cleanup(func() { _ = os.Chdir(wd) })
	_ = os.Chdir(root)
	if newBrowser("alpha", false).dir != filepath.Join(root, "alpha") {
		t.Fatal("relative text")
	}
}

func browserKey(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "pgup":
		return tea.KeyMsg{Type: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyMsg{Type: tea.KeyPgDown}
	case "home":
		return tea.KeyMsg{Type: tea.KeyHome}
	case "end":
		return tea.KeyMsg{Type: tea.KeyEnd}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func press(t *testing.T, m model, keys ...string) model {
	t.Helper()
	for _, k := range keys {
		updated, _ := m.handleBrowser(browserKey(k))
		m = updated.(model)
	}
	return m
}

// Verifies: REQ-PPK-005
func TestBrowserNavigationAndChoice(t *testing.T) {
	root := tree(t)
	open := func(kind pathKind) model {
		m := pathPrompt(t, kind, root)
		updated, _ := m.handleTextPrompt(tea.KeyMsg{Type: tea.KeyCtrlO})
		m = updated.(model)
		if m.browse == nil || m.browse.dir != root {
			t.Fatalf("Ctrl+O must open the browser at %s: %+v", root, m.browse)
		}
		return m
	}

	// enter on a directory descends, left goes up, a file is chosen by enter
	m := press(t, open(pathAny), "enter") // cursor 0 = alpha/
	if m.browse.dir != filepath.Join(root, "alpha") {
		t.Fatalf("dir = %s", m.browse.dir)
	}
	m = press(t, m, "left")
	if m.browse.dir != root {
		t.Fatalf("left must go up: %s", m.browse.dir)
	}
	m = press(t, m, "end", "enter") // last entry: zeta.bin
	if m.browse != nil || m.input.value != filepath.Join(root, "zeta.bin") || m.input.cursor != len([]rune(m.input.value)) {
		t.Fatalf("a file is chosen by enter: browse=%v input=%+v", m.browse, m.input)
	}
	if m.input.mode != inputText {
		t.Fatal("the prompt stays open with the chosen path")
	}

	// space chooses the directory being shown; right descends; backspace goes up
	m = press(t, open(pathAny), "right", "backspace", "down", "right", " ")
	if m.browse != nil || m.input.value != filepath.Join(root, "Beta") {
		t.Fatalf("space chooses the current directory: %q", m.input.value)
	}
	m = press(t, open(pathAny), "s")
	if m.input.value != root {
		t.Fatalf("s chooses the current directory too: %q", m.input.value)
	}

	// in directory-only mode enter never chooses
	m = press(t, open(pathDirs), "enter", "enter")
	if m.browse == nil {
		t.Fatal("enter descends in directory mode")
	}

	// esc / q / ctrl+c close without changing the text
	for _, k := range []string{"esc", "q"} {
		m = open(pathAny)
		before := m.input.value
		m = press(t, m, k)
		if m.browse != nil || m.input.value != before {
			t.Fatalf("%s must close the browser and keep the text", k)
		}
	}
	updated, _ := open(pathAny).handleBrowser(tea.KeyMsg{Type: tea.KeyCtrlC})
	if updated.(model).browse != nil {
		t.Fatal("ctrl+c closes the browser")
	}

	// cursor keys, clamped
	m = open(pathAny)
	m = press(t, m, "up", "k")
	if m.browse.cursor != 0 {
		t.Fatal("cursor stops at the top")
	}
	m = press(t, m, "down", "j")
	if m.browse.cursor != 2 {
		t.Fatalf("cursor = %d", m.browse.cursor)
	}
	m = press(t, m, "up")
	if m.browse.cursor != 1 {
		t.Fatalf("up moves the cursor: %d", m.browse.cursor)
	}
	m = press(t, m, "pgdown")
	if m.browse.cursor != len(m.browse.entries)-1 {
		t.Fatalf("pgdown clamps: %d", m.browse.cursor)
	}
	m = press(t, m, "pgup")
	if m.browse.cursor != 0 {
		t.Fatalf("pgup clamps: %d", m.browse.cursor)
	}
	m = press(t, m, "G", "down")
	if m.browse.cursor != len(m.browse.entries)-1 {
		t.Fatal("cursor stops at the end")
	}
	m = press(t, m, "g")
	if m.browse.cursor != 0 {
		t.Fatal("g goes to the top")
	}
	m = press(t, m, "end", "home")
	if m.browse.cursor != 0 {
		t.Fatal("home goes to the top")
	}

	// the browser does not leak input state: nothing chosen, nothing submitted
	m = open(pathAny)
	m = press(t, m, "x")
	if m.browse == nil || m.input.value != root {
		t.Fatal("unknown keys do nothing")
	}
}

// Verifies: REQ-PPK-006
func TestBrowserHiddenHomeAndErrors(t *testing.T) {
	root := tree(t)
	m := model{browse: newBrowser(root, false)}
	if strings.Contains(strings.Join(entryNames(m.browse), ","), ".hidden") {
		t.Fatal("hidden entries start hidden")
	}
	m = press(t, m, ".")
	if got := strings.Join(entryNames(m.browse), ","); !strings.Contains(got, ".hidden") || !strings.Contains(got, ".dotdir/") {
		t.Fatalf("'.' shows hidden entries: %s", got)
	}
	m = press(t, m, ".")
	if strings.Contains(strings.Join(entryNames(m.browse), ","), ".hidden") {
		t.Fatal("'.' toggles back")
	}

	t.Setenv("HOME", filepath.Join(root, "alpha"))
	m = press(t, m, "~")
	if m.browse.dir != filepath.Join(root, "alpha") {
		t.Fatalf("~ goes home: %s", m.browse.dir)
	}
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	before := m.browse.dir
	m = press(t, m, "~")
	if m.browse.dir != before {
		t.Fatal("without a home directory ~ does nothing")
	}

	// an unreadable directory keeps the listing and says why
	m = model{browse: newBrowser(root, false)}
	broken := filepath.Join(root, "dangling")
	if err := os.Symlink(filepath.Join(root, "gone"), broken); err != nil {
		t.Fatal(err)
	}
	b := *m.browse
	b.load(broken)
	if b.err == "" || b.dir != root {
		t.Fatalf("a failing load keeps the previous directory: %+v", b)
	}
	// parent of the root is the root: the listing stays valid
	m = model{browse: newBrowser("/", false)}
	m = press(t, m, "left")
	if m.browse.dir != "/" {
		t.Fatalf("dir = %s", m.browse.dir)
	}
	// an empty directory has only "." and ".."; the cursor starts on ".."
	empty := t.TempDir()
	m = model{browse: newBrowser(empty, false)}
	if len(m.browse.entries) != 2 || m.browse.cursor != 1 {
		t.Fatalf("empty directory: %+v", m.browse)
	}
	m = press(t, m, "down", "up", "up", "down")
	if m.browse == nil || m.browse.dir != empty || m.browse.cursor != 1 {
		t.Fatal("moving in an empty directory stays within its two rows")
	}
}

func entryNames(b *fileBrowser) []string {
	var out []string
	for _, e := range b.entries {
		out = append(out, entryName(e.name, e.dir))
	}
	return out
}

// Verifies: REQ-PPK-007
func TestBrowserAndPromptRendering(t *testing.T) {
	root := tree(t)
	for i := 0; i < 20; i++ {
		_ = os.WriteFile(filepath.Join(root, "file"+string(rune('a'+i))+".txt"), nil, 0o644)
	}
	m := model{width: 100, version: "t", browse: newBrowser(root, false)}
	out := m.renderBrowser()
	for _, want := range []string{"Choose a file or directory", "alpha/", "2.0 kB", "more", "choose a file"} {
		if !strings.Contains(out, want) {
			t.Errorf("browser lacks %q", want)
		}
	}
	m.browse = newBrowser(root, true)
	if out := m.renderBrowser(); !strings.Contains(out, "Choose a directory") || strings.Contains(out, "zeta.bin") {
		t.Errorf("directory mode:\n%s", out)
	}
	// scrolling: the cursor row stays visible
	m.browse = newBrowser(root, false)
	m = press(t, m, "end")
	if out := m.renderBrowser(); !strings.Contains(out, "zeta.bin") {
		t.Error("the last entry must be visible at the end")
	}
	// empty directory, error line, narrow terminal, long path
	m.browse = newBrowser(t.TempDir(), false)
	m.browse.err = "boom"
	m.width = 60
	out = m.renderBrowser()
	if !strings.Contains(out, "(empty)") || !strings.Contains(out, "boom") {
		t.Error("empty listing and error line")
	}
	m.browse.dir = "/" + strings.Repeat("deep/", 30) + "end"
	if out := m.renderBrowser(); !strings.Contains(out, "…") || !strings.Contains(out, "end") {
		t.Error("a long path is cut at the start, keeping its end")
	}
	if truncateLeft("short", 20) != "short" || truncateLeft("abc", 1) != "abc" {
		t.Error("truncateLeft leaves short text alone")
	}

	// the View shows the browser instead of the prompt; the prompt shows its keys and candidates
	m = model{width: 100, version: "t", browse: newBrowser(root, false)}
	if !strings.Contains(m.View(), "Choose a file or directory") {
		t.Error("View must render the browser")
	}
	p := pathPrompt(t, pathAny, root+"/al")
	p.version = "t"
	p.input.note = "alpha/  alphabet.txt"
	view := p.renderInputPopup("Upload to box: local path", p.input.value, false)
	for _, want := range []string{"tab complete", "ctrl+o", "[ Browse… ]", "alpha/  alphabet.txt"} {
		if !strings.Contains(view, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
	plain := model{version: "t", input: textInput{mode: inputText, label: "x"}}
	if v := plain.renderInputPopup("x", "", false); strings.Contains(v, "ctrl+o") {
		t.Error("an ordinary prompt has no browse hint")
	}
	// browser keys reach the browser through Update
	updated, _ := model{browse: newBrowser(root, false)}.Update(tea.KeyMsg{Type: tea.KeyDown})
	if updated.(model).browse.cursor != 3 {
		t.Error("Update must route keys to the open browser")
	}
}

// Verifies: REQ-PPK-008
func TestBrowserDotAndDotDotRows(t *testing.T) {
	root := tree(t)
	open := func(dir string) model {
		m := pathPrompt(t, pathAny, dir)
		updated, _ := m.handleTextPrompt(tea.KeyMsg{Type: tea.KeyCtrlO})
		return updated.(model)
	}
	sub := filepath.Join(root, "alpha")

	// ".." goes up (enter and right), "." chooses (enter) but right does nothing
	m := press(t, open(sub), "up", "up") // cursor on "."
	if m.browse.cursor != 0 || !m.browse.entries[0].special {
		t.Fatal("the first row is .")
	}
	m = press(t, m, "right")
	if m.browse == nil || m.browse.dir != sub {
		t.Fatal("right on . does nothing")
	}
	m = press(t, m, "down", "enter") // ".."
	if m.browse.dir != root {
		t.Fatalf("enter on .. goes up: %s", m.browse.dir)
	}
	m = press(t, m, "up", "up")
	m = press(t, m, "enter") // "."
	if m.browse != nil || m.input.value != root {
		t.Fatalf("enter on . chooses the directory shown: %q", m.input.value)
	}
	m = press(t, open(sub), "up", "right") // ".." via right
	if m.browse.dir != root {
		t.Fatalf("right on .. goes up: %s", m.browse.dir)
	}
	if lines := m.renderBrowser(); !strings.Contains(lines, ". (choose this directory)") || !strings.Contains(lines, ".. (up)") {
		t.Errorf("the rows are labelled:\n%s", lines)
	}
	// the root stays the root
	m = press(t, model{browse: newBrowser("/", true)}, "up", "enter")
	if m.browse.dir != "/" {
		t.Fatalf("up from the root: %s", m.browse.dir)
	}
}

// Verifies: REQ-PPK-009
func TestBrowseButtonInPathPrompts(t *testing.T) {
	root := tree(t)
	m := pathPrompt(t, pathAny, root)
	m.version = "t"
	hasFocus := func(m model) bool { return m.input.focusBrowse }
	render := func(m model) string { return m.renderInputPopup("x", m.input.value, false) }

	if !strings.Contains(render(m), "[ Browse… ]") {
		t.Fatal("a path prompt shows the button")
	}
	if strings.Contains(render(model{version: "t", input: textInput{mode: inputText}}), "Browse") {
		t.Fatal("an ordinary prompt has no button")
	}

	// down focuses the button, up returns to the field
	updated, _ := m.handleTextPrompt(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	if !hasFocus(m) {
		t.Fatal("down must focus the button")
	}
	focused := render(m)
	updated, _ = m.handleTextPrompt(tea.KeyMsg{Type: tea.KeyUp})
	if hasFocus(updated.(model)) || render(updated.(model)) == focused {
		t.Fatal("up must return to the field and the button must look different when focused")
	}

	// enter on the button opens the browser instead of submitting
	submitted := false
	m.input.onSubmit = func(m model, v string) (tea.Model, tea.Cmd) { submitted = true; return m, nil }
	updated, _ = m.handleTextPrompt(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(model)
	if submitted || got.browse == nil || got.browse.dir != root || hasFocus(got) {
		t.Fatalf("enter on the button: submitted=%v browse=%v focus=%v", submitted, got.browse, hasFocus(got))
	}

	// typing or Tab returns the focus to the field
	for _, k := range []tea.KeyMsg{{Type: tea.KeyRunes, Runes: []rune("a")}, {Type: tea.KeyTab}} {
		m.input.focusBrowse = true
		updated, _ = m.handleTextPrompt(k)
		if hasFocus(updated.(model)) {
			t.Fatalf("%v must return the focus to the field", k)
		}
	}
	// enter without focus submits as before
	m.input.focusBrowse = false
	updated, _ = m.handleTextPrompt(tea.KeyMsg{Type: tea.KeyEnter})
	if !submitted {
		t.Fatal("enter in the field submits")
	}
	_ = updated

	// ordinary prompts ignore up and down
	plain := model{input: textInput{mode: inputText, value: "x"}}
	for _, k := range []tea.KeyType{tea.KeyDown, tea.KeyUp} {
		updated, _ = plain.handleTextPrompt(tea.KeyMsg{Type: k})
		if updated.(model).input.focusBrowse {
			t.Fatal("no button, no focus")
		}
	}
}
