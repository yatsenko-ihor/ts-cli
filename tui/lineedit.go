package tui

import (
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Single-line editing shared by every text prompt (username, password,
// ad-hoc command, generic prompts and the machine search).
//
// Terminals report Option/Alt+arrows in two ways and both are supported:
//
//	ESC b / ESC f          iTerm2 "Natural Text Editing" / Esc+ as Option, Terminal.app
//	ESC [1;3D / ESC [1;3C  terminals that send modified arrow sequences
//
// Bubble Tea turns these into "alt+b"/"alt+f" and "alt+left"/"alt+right";
// Ctrl+←/→ are accepted too. Whatever the terminal does not send (for
// example Option typing special characters when it is not configured as
// Meta) simply never reaches the app.

// isWordRune reports whether r belongs to a word for word-wise movement.
func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

// prevWordStart returns the start of the word before pos.
func prevWordStart(r []rune, pos int) int {
	for pos > 0 && !isWordRune(r[pos-1]) {
		pos--
	}
	for pos > 0 && isWordRune(r[pos-1]) {
		pos--
	}
	return pos
}

// nextWordEnd returns the end of the word after pos.
func nextWordEnd(r []rune, pos int) int {
	for pos < len(r) && !isWordRune(r[pos]) {
		pos++
	}
	for pos < len(r) && isWordRune(r[pos]) {
		pos++
	}
	return pos
}

func deleteRange(r []rune, from, to int) []rune {
	out := make([]rune, 0, len(r)-(to-from))
	out = append(out, r[:from]...)
	return append(out, r[to:]...)
}

// insertText inserts text at the rune position cursor and returns the new
// value and cursor (placed after the inserted text).
func insertText(value string, cursor int, text string) (string, int) {
	r := []rune(value)
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(r) {
		cursor = len(r)
	}
	ins := []rune(text)
	out := make([]rune, 0, len(r)+len(ins))
	out = append(out, r[:cursor]...)
	out = append(out, ins...)
	out = append(out, r[cursor:]...)
	return string(out), cursor + len(ins)
}

// editLine applies an editing key to value/cursor. handled is false when the
// key is not an editing key, so callers can fall through to other handlers.
func editLine(value string, cursor int, msg tea.KeyMsg) (newValue string, newCursor int, handled bool) {
	r := []rune(value)
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(r) {
		cursor = len(r)
	}

	// Bracketed paste arrives as one KeyMsg with the whole text.
	if msg.Paste {
		v, c := insertText(value, cursor, sanitizePastedText(string(msg.Runes)))
		return v, c, true
	}

	switch msg.String() {
	case "left", "ctrl+b":
		if cursor > 0 {
			cursor--
		}
	case "right", "ctrl+f":
		if cursor < len(r) {
			cursor++
		}
	case "alt+left", "alt+b", "ctrl+left":
		cursor = prevWordStart(r, cursor)
	case "alt+right", "alt+f", "ctrl+right":
		cursor = nextWordEnd(r, cursor)
	case "home", "ctrl+a":
		cursor = 0
	case "end", "ctrl+e":
		cursor = len(r)
	case "backspace":
		if cursor > 0 {
			r = deleteRange(r, cursor-1, cursor)
			cursor--
		}
	case "delete", "ctrl+d":
		if cursor < len(r) {
			r = deleteRange(r, cursor, cursor+1)
		}
	case "alt+backspace", "ctrl+w":
		start := prevWordStart(r, cursor)
		r = deleteRange(r, start, cursor)
		cursor = start
	case "alt+d", "alt+delete":
		r = deleteRange(r, cursor, nextWordEnd(r, cursor))
	case "ctrl+u":
		r = deleteRange(r, 0, cursor)
		cursor = 0
	case "ctrl+k":
		r = deleteRange(r, cursor, len(r))
	default:
		switch {
		case msg.Type == tea.KeySpace && !msg.Alt:
			v, c := insertText(value, cursor, " ")
			return v, c, true
		case msg.Type == tea.KeyRunes && !msg.Alt && len(msg.Runes) > 0:
			v, c := insertText(value, cursor, string(msg.Runes))
			return v, c, true
		}
		return value, cursor, false
	}
	return string(r), cursor, true
}

// editInput applies an editing key to the active text prompt.
func (m *model) editInput(msg tea.KeyMsg) bool {
	v, c, handled := editLine(m.input.value, m.input.cursor, msg)
	if handled {
		m.input.value, m.input.cursor = v, c
	}
	return handled
}

// renderEditable renders value with the cursor mark inserted at cursor.
// masked replaces every character with a bullet. When maxWidth > 0 and the
// text is too wide, the start is cut (with an ellipsis) so the cursor stays
// visible, then the end if still necessary.
func renderEditable(value string, cursor int, masked bool, mark string, maxWidth int) string {
	r := []rune(value)
	if masked {
		for i := range r {
			r[i] = '•'
		}
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(r) {
		cursor = len(r)
	}
	left, right := r[:cursor], r[cursor:]
	if maxWidth <= 0 {
		return string(left) + mark + string(right)
	}
	prefix := ""
	for len(left) > 0 && lipgloss.Width(prefix+string(left)+mark+string(right)) > maxWidth {
		left = left[1:]
		prefix = "…"
	}
	for len(right) > 0 && lipgloss.Width(prefix+string(left)+mark+string(right)) > maxWidth {
		right = right[:len(right)-1]
	}
	return prefix + string(left) + mark + string(right)
}
