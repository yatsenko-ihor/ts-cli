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
	m.ssh.username = "root"
	m.width = 300
	if !strings.Contains(m.getHelpText(), "d clear-user") {
		t.Error("clear-user hint when a default user is set")
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
