package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func runes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func TestEditLineWordMovement(t *testing.T) {
	const text = "sudo systemctl restart nginx"
	end := len([]rune(text))

	cases := []struct {
		name string
		key  tea.KeyMsg
		from int
		want int
	}{
		// Option+Left/Right as iTerm2 "Natural Text Editing" sends them: ESC b / ESC f
		{"alt+b back one word", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b"), Alt: true}, end, 23},
		{"alt+f forward one word", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f"), Alt: true}, 0, 4},
		// Terminals that send modified arrows: ESC [1;3D / ESC [1;3C
		{"alt+left", tea.KeyMsg{Type: tea.KeyLeft, Alt: true}, end, 23},
		{"alt+right", tea.KeyMsg{Type: tea.KeyRight, Alt: true}, 5, 14},
		{"ctrl+left", tea.KeyMsg{Type: tea.KeyCtrlLeft}, 14, 5},
		{"ctrl+right", tea.KeyMsg{Type: tea.KeyCtrlRight}, 4, 14},
		{"word back from mid-word goes to its start", tea.KeyMsg{Type: tea.KeyLeft, Alt: true}, 8, 5},
		{"back at start stays", tea.KeyMsg{Type: tea.KeyLeft, Alt: true}, 0, 0},
		{"forward at end stays", tea.KeyMsg{Type: tea.KeyRight, Alt: true}, end, end},
		{"left", tea.KeyMsg{Type: tea.KeyLeft}, 3, 2},
		{"right", tea.KeyMsg{Type: tea.KeyRight}, 3, 4},
		{"home", tea.KeyMsg{Type: tea.KeyHome}, 9, 0},
		{"ctrl+a", tea.KeyMsg{Type: tea.KeyCtrlA}, 9, 0},
		{"end", tea.KeyMsg{Type: tea.KeyEnd}, 3, end},
		{"ctrl+e", tea.KeyMsg{Type: tea.KeyCtrlE}, 3, end},
	}
	for _, c := range cases {
		v, cur, handled := editLine(text, c.from, c.key)
		if !handled || v != text || cur != c.want {
			t.Errorf("%s: cursor %d, want %d (handled=%v value=%q)", c.name, cur, c.want, handled, v)
		}
	}
}

func TestEditLineDeletions(t *testing.T) {
	const text = "tail -n 50 /var/log/syslog"
	cases := []struct {
		name       string
		key        tea.KeyMsg
		from       int
		wantV      string
		wantCursor int
	}{
		{"backspace", tea.KeyMsg{Type: tea.KeyBackspace}, 4, "tai -n 50 /var/log/syslog", 3},
		{"backspace at start", tea.KeyMsg{Type: tea.KeyBackspace}, 0, text, 0},
		{"delete", tea.KeyMsg{Type: tea.KeyDelete}, 0, "ail -n 50 /var/log/syslog", 0},
		{"ctrl+d", tea.KeyMsg{Type: tea.KeyCtrlD}, 0, "ail -n 50 /var/log/syslog", 0},
		{"option+backspace deletes previous word", tea.KeyMsg{Type: tea.KeyBackspace, Alt: true}, 4, "-n 50 /var/log/syslog"[0:0] + " -n 50 /var/log/syslog", 0},
		{"ctrl+w deletes previous word", tea.KeyMsg{Type: tea.KeyCtrlW}, len([]rune(text)), "tail -n 50 /var/log/", len([]rune("tail -n 50 /var/log/"))},
		{"alt+d deletes next word", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d"), Alt: true}, 5, "tail -n 50 /var/log/syslog"[:5] + " 50 /var/log/syslog", 5},
		{"ctrl+u kills to start", tea.KeyMsg{Type: tea.KeyCtrlU}, 11, "/var/log/syslog", 0},
		{"ctrl+k kills to end", tea.KeyMsg{Type: tea.KeyCtrlK}, 10, "tail -n 50", 10},
	}
	for _, c := range cases {
		v, cur, handled := editLine(text, c.from, c.key)
		if !handled || v != c.wantV || cur != c.wantCursor {
			t.Errorf("%s: got %q cursor %d, want %q cursor %d", c.name, v, cur, c.wantV, c.wantCursor)
		}
	}
}

func TestEditLineInsertsAtCursorIncludingUnicodeAndPaste(t *testing.T) {
	v, c, ok := editLine("ac", 1, runes("b"))
	if !ok || v != "abc" || c != 2 {
		t.Fatalf("insert: %q %d", v, c)
	}
	// Non-ASCII input (previously dropped because the length check was in bytes).
	v, c, _ = editLine("", 0, runes("їжак"))
	if v != "їжак" || c != 4 {
		t.Fatalf("unicode: %q %d", v, c)
	}
	v, c, _ = editLine("ab", 1, tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")})
	if v != "a b" || c != 2 {
		t.Fatalf("space: %q %d", v, c)
	}
	v, c, ok = editLine("ab", 1, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("X\nY\tZ"), Paste: true})
	if !ok || v != "aX Y Zb" || c != 6 {
		t.Fatalf("paste is sanitised and inserted at the cursor: %q %d", v, c)
	}
	// Unknown keys fall through to other handlers.
	if _, _, ok := editLine("x", 0, tea.KeyMsg{Type: tea.KeyF5}); ok {
		t.Fatal("F5 must not be handled")
	}
	if _, _, ok := editLine("x", 0, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q"), Alt: true}); ok {
		t.Fatal("unknown alt combos must not insert text")
	}
}

func TestRenderEditable(t *testing.T) {
	if got := renderEditable("abc", 1, false, "|", 0); got != "a|bc" {
		t.Fatalf("got %q", got)
	}
	if got := renderEditable("secret", 3, true, "|", 0); got != "•••|•••" {
		t.Fatalf("masked: %q", got)
	}
	long := strings.Repeat("x", 50)
	got := renderEditable(long, 50, false, "|", 10)
	if !strings.HasSuffix(got, "|") || !strings.HasPrefix(got, "…") || len([]rune(got)) > 10 {
		t.Fatalf("cursor must stay visible when the text is too wide: %q", got)
	}
}

func TestPromptEditingEndToEnd(t *testing.T) {
	m := model{input: textInput{mode: inputCommand}}
	for _, k := range []tea.KeyMsg{runes("ls "), runes("-la"), runes(" /tmp")} {
		updated, _ := m.handleCommandMode(k)
		m = updated.(model)
	}
	if m.input.value != "ls -la /tmp" {
		t.Fatalf("typed value: %q", m.input.value)
	}
	// Option+Left twice lands before the word "la" ("-" is not a word character, as in bash)
	for i := 0; i < 2; i++ {
		updated, _ := m.handleCommandMode(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b"), Alt: true})
		m = updated.(model)
	}
	updated, _ := m.handleCommandMode(runes("X"))
	m = updated.(model)
	if m.input.value != "ls -Xla /tmp" {
		t.Fatalf("after word-back and insert: %q", m.input.value)
	}

	// Search box uses the same editing.
	s := model{input: textInput{mode: inputSearch}, list: deviceList{searchQuery: "web server", searchCursor: 10}}
	updated, _ = s.handleSearchMode(tea.KeyMsg{Type: tea.KeyBackspace, Alt: true})
	s = updated.(model)
	if s.list.searchQuery != "web " || s.list.searchCursor != 4 {
		t.Fatalf("search word delete: %q %d", s.list.searchQuery, s.list.searchCursor)
	}
}
