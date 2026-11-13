package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Verifies: REQ-HNT-001
func TestFitHintsDropsTheLeastUsefulFirstAndKeepsTheTail(t *testing.T) {
	hints := []string{"one", "two", "three", "four"}
	if got := fitHints(hints, "? help", 0); got != "one • two • three • four • ? help" {
		t.Fatalf("no limit: %q", got)
	}
	got := fitHints(hints, "? help", 22)
	if got != "one • two • ? help" && got != "one • two • three • ? help" {
		t.Fatalf("limited: %q", got)
	}
	for w := 1; w < 40; w++ {
		got := fitHints(hints, "? help", w)
		if !strings.HasSuffix(got, "? help") {
			t.Fatalf("width %d: the tail must always be there: %q", w, got)
		}
		if lipgloss.Width(got) > w && got != "? help" {
			t.Fatalf("width %d: %q is too wide", w, got)
		}
	}
	if got := fitHints(hints, "? help", 3); got != "? help" {
		t.Fatalf("nothing fits but the tail: %q", got)
	}
	if got := fitHints(nil, "? help", 0); got != "? help" {
		t.Fatalf("no hints: %q", got)
	}
}

// Verifies: REQ-HNT-002
func TestHelpBarMentionsTheCurrentKeysAndFitsTheTerminal(t *testing.T) {
	m := model{width: 200}
	full := m.getHelpText()
	for _, want := range []string{"s ssh", "H offline", "m accounts", "F12 settings", "? help", "q quit", "machine actions"} {
		if !strings.Contains(full, want) {
			t.Errorf("list hints lack %q: %s", want, full)
		}
	}
	m.width = 80
	narrow := m.getHelpText()
	if lipgloss.Width(narrow) > 80-2-4 || !strings.HasSuffix(narrow, "? help • q quit") || !strings.Contains(narrow, "s ssh") {
		t.Fatalf("narrow bar (%d cells): %q", lipgloss.Width(narrow), narrow)
	}
	m.width = 300
	if !strings.Contains(m.getHelpText(), "d forget password") || !strings.Contains(m.getHelpText(), "b files") {
		t.Error("the wide bar names d and b")
	}

	// Run panel: Enter's meaning follows the tab
	m = model{run: runPanel{visible: true, tab: tabMachine}, activeFocus: focusRun}
	if h := m.getHelpText(); !strings.Contains(h, "enter do it") {
		t.Errorf("machine tab hint: %q", h)
	}
	m.run.tab = tabRecent
	if h := m.getHelpText(); !strings.Contains(h, "enter run again") {
		t.Errorf("recent tab hint: %q", h)
	}
	m.run.tab = tabScripts
	if h := m.getHelpText(); !strings.Contains(h, "enter run ") {
		t.Errorf("scripts tab hint: %q", h)
	}
}

// Verifies: REQ-HNT-003
func TestQuestionMarkOpensAndClosesHelp(t *testing.T) {
	m := model{}
	updated, _ := m.handleNormalMode(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = updated.(model)
	if !m.helpMode || m.helpScroll != 0 {
		t.Fatal("? must open the help screen")
	}
	updated, _ = m.handleHelpMode(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	if updated.(model).helpMode {
		t.Fatal("? must close it again")
	}
}

func TestHintBarWrapsInsteadOfDroppingKeys(t *testing.T) {
	hints := []string{"aaaa", "bbbb", "cccc", "dddd", "eeee", "ffff"}
	// everything fits on one line
	if got := fitHintLines(hints, "tail", 100, 3); got != "aaaa • bbbb • cccc • dddd • eeee • ffff • tail" {
		t.Fatalf("one line: %q", got)
	}
	// two lines: the hints wrap, the tail ends the last line, nothing is lost
	got := fitHintLines(hints, "tail", 26, 3)
	lines := strings.Split(got, "\n")
	if len(lines) < 2 || !strings.HasSuffix(got, " • tail") || strings.Count(got, "aaaa")+strings.Count(got, "bbbb")+strings.Count(got, "cccc")+strings.Count(got, "dddd")+strings.Count(got, "eeee")+strings.Count(got, "ffff") != len(hints) {
		t.Fatalf("wrapped: %q", got)
	}
	for _, l := range lines {
		if lipgloss.Width(l) > 26 {
			t.Fatalf("line %q is wider than 26", l)
		}
	}
	// too many hints for the allowed lines: the least useful ones are dropped, the tail stays
	short := fitHintLines(hints, "tail", 14, 2)
	if len(strings.Split(short, "\n")) != 2 || !strings.HasSuffix(short, "tail") || strings.Contains(short, "ffff") {
		t.Fatalf("dropped: %q", short)
	}
	// no room at all: just the tail; unknown width: one unlimited line
	if got := fitHintLines(hints, "tail", 3, 2); got != "tail" && !strings.HasSuffix(got, "tail") {
		t.Fatalf("tiny: %q", got)
	}
	if got := fitHintLines(hints, "tail", 0, 3); strings.Contains(got, "\n") {
		t.Fatalf("no width: %q", got)
	}
	// the real list hints at a normal terminal: every documented key shows
	m := model{width: 140}
	bar := m.getHelpText()
	for _, key := range []string{"y/Y copy ip/name", "b files", "* pin", "N note", "E export", "d forget password", "A mark all", "? help", "q quit"} {
		if !strings.Contains(bar, key) {
			t.Errorf("the bar lacks %q:\n%s", key, bar)
		}
	}
}
