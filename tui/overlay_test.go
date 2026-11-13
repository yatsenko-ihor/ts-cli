package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestOverlayKeepsTheScreenAroundThePopup(t *testing.T) {
	bg := strings.Repeat(strings.Repeat(".", 20)+"\n", 5) + strings.Repeat(".", 20)
	got := overlayCenter(bg, "AB\nCD", 20, 6)
	lines := strings.Split(got, "\n")
	if len(lines) != 6 {
		t.Fatalf("%d lines", len(lines))
	}
	// 2x2 box centred in 20x6: columns 9-10, rows 2-3; everything else untouched
	for i, l := range lines {
		plain := strings.ReplaceAll(l, "\x1b[0m", "")
		switch i {
		case 2:
			if plain != strings.Repeat(".", 9)+"AB"+strings.Repeat(".", 9) {
				t.Errorf("row %d: %q", i, plain)
			}
		case 3:
			if plain != strings.Repeat(".", 9)+"CD"+strings.Repeat(".", 9) {
				t.Errorf("row %d: %q", i, plain)
			}
		default:
			if plain != strings.Repeat(".", 20) {
				t.Errorf("row %d: %q", i, plain)
			}
		}
	}
	// a short background is padded, a missing size is taken from the background
	short := overlayCenter("ab", "XYZ", 11, 3)
	if !strings.Contains(strings.ReplaceAll(short, "\x1b[0m", ""), "ab     XYZ") && lipgloss.Width(short) < 7 {
		t.Fatalf("short: %q", short)
	}
	if got := overlayCenter("....\n....\n....", "X", 0, 0); !strings.Contains(strings.ReplaceAll(got, "\x1b[0m", ""), ".X.") {
		t.Fatalf("size from background: %q", got)
	}
	// the box is taller than the background: rows are added
	if got := overlayCenter(".", "A\nB\nC", 1, 0); len(strings.Split(got, "\n")) != 3 {
		t.Fatalf("grows: %q", got)
	}
}

func TestMoveLineAndMultilineRendering(t *testing.T) {
	v := "first\nab\nthird line"
	// cursor at column 3 of "first" (index 3): down lands at the end of the short line "ab"
	if got := moveLine(v, 3, 1); got != 8 {
		t.Fatalf("down short: %d", got)
	}
	// from the end of "ab" down to column 2 of "third line"
	if got := moveLine(v, 8, 1); got != 11 {
		t.Fatalf("down long: %d", got)
	}
	if got := moveLine(v, 13, -1); got != 8 {
		t.Fatalf("up: %d", got)
	}
	if got := moveLine(v, 8, -1); got != 2 {
		t.Fatalf("up to column 2: %d", got)
	}
	if moveLine(v, 2, -1) != 2 || moveLine(v, len([]rune(v)), 1) != len([]rune(v)) {
		t.Fatal("no line above the first or below the last")
	}
	if moveLine("a\nb", 99, 1) != 1+1 && moveLine("a\nb", 99, 1) != 3 {
		t.Fatal("cursor clamped")
	}

	out := renderMultiline("one\ntwo", 4, "▌", 20, 4)
	lines := strings.Split(out, "\n")
	if len(lines) != 4 || lines[0] != "one" || lines[1] != "▌two" {
		t.Fatalf("%q", lines)
	}
	// wrapping and a window that follows the cursor
	long := strings.Repeat("x", 25) + "\n" + strings.Join([]string{"a", "b", "c", "d", "e", "f", "g", "h"}, "\n")
	out = renderMultiline(long, len([]rune(long)), "▌", 10, 3)
	if got := strings.Split(out, "\n"); len(got) != 3 || !strings.Contains(got[2], "h▌") {
		t.Fatalf("window: %q", got)
	}
	empty := renderMultiline("", 0, "▌", 10, 2)
	if empty != "▌\n" {
		t.Fatalf("empty: %q", empty)
	}
	if got := renderMultiline("a\n\nb", 99, "▌", 10, 5); !strings.Contains(got, "b▌") {
		t.Fatalf("blank line: %q", got)
	}
}

func TestNoteEditorIsMultilineAndSavesWithCtrlS(t *testing.T) {
	m := personalModel(t)
	for i, d := range m.list.filteredDevices {
		if machineKey(d) == "b" {
			m.list.cursor = i
		}
	}
	u, _ := m.promptNote()
	m = u.(model)
	if !m.input.multiline {
		t.Fatal("note prompt is multi-line")
	}
	send := func(k tea.KeyMsg) {
		u, _ := m.Update(k)
		m = u.(model)
	}
	for _, r := range "line one" {
		send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	send(tea.KeyMsg{Type: tea.KeyEnter}) // a new line, not a save
	if m.input.mode != inputText || !strings.Contains(m.input.value, "\n") {
		t.Fatalf("enter adds a line: %q", m.input.value)
	}
	for _, r := range "line two" {
		send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	send(tea.KeyMsg{Type: tea.KeyUp})
	if m.input.cursor > len("line one") {
		t.Fatalf("up moves to the first line: %d", m.input.cursor)
	}
	send(tea.KeyMsg{Type: tea.KeyDown})
	if !strings.Contains(m.View(), "ctrl+s save") {
		t.Fatal("the popup names the save key")
	}
	t.Setenv("TS_CLI_CONFIG_DIR", t.TempDir())
	u, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = u.(model)
	if m.input.mode != inputNone || m.notes["b"] != "line one\nline two" || cmd == nil {
		t.Fatalf("saved: %q %v", m.notes["b"], m.input.mode)
	}
	// the details panel shows every line, the Machine tab the first plus a count
	m.run.visible, m.run.tab, m.width, m.height = true, tabMachine, 140, 60
	if tab := m.renderRunPanel(); !strings.Contains(tab, "Note: line one (+1 more line(s))") {
		t.Fatalf("machine tab: %s", tab)
	}
	m.run.visible = false
	panel := m.renderDetailsPanel(70, 0)
	if !strings.Contains(panel, "line one") || !strings.Contains(panel, "line two") {
		t.Fatalf("details:\n%s", panel)
	}
	m.notes["b"] = strings.Repeat("l\n", 10)
	if !strings.Contains(m.renderDetailsPanel(70, 0), "…") {
		t.Fatal("a long note is cut")
	}
	// alt+enter also saves; a huge note is capped
	u, _ = m.promptNote()
	m = u.(model)
	m.input.value = strings.Repeat("x", maxNoteRunes+50)
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	if got := u.(model).notes["b"]; len([]rune(got)) != maxNoteRunes {
		t.Fatalf("capped at %d, got %d", maxNoteRunes, len([]rune(got)))
	}
}

func TestPopupsAreDrawnOverTheScreen(t *testing.T) {
	m := personalModel(t)
	m.width, m.height = 120, 40
	base := m.View()
	if !strings.Contains(base, "alpha") {
		t.Fatalf("the list lists alpha:\n%s", base)
	}
	for _, mode := range []inputMode{inputUsername, inputPassword} {
		m2 := m
		m2.input = textInput{mode: mode}
		view := m2.View()
		if !strings.Contains(view, "alpha") || !strings.Contains(view, "Tailscale Devices") {
			t.Errorf("mode %v: the machine list must stay visible around the popup", mode)
		}
		want := "SSH Username"
		if mode == inputPassword {
			want = "SSH Password"
		}
		if !strings.Contains(view, want) {
			t.Errorf("mode %v lacks %q", mode, want)
		}
	}
	m3 := m
	m3.input = textInput{mode: inputText, label: "A question", onSubmit: nil}
	if view := m3.View(); !strings.Contains(view, "A question") || !strings.Contains(view, "alpha") {
		t.Fatal("text prompts are popups too")
	}
}

func TestOptionsAndAboutArePopups(t *testing.T) {
	m := personalModel(t)
	m.width, m.height = 120, 40
	m.version = "9.9.9"

	m2 := m
	m2.opts.active = true
	view := m2.View()
	for _, want := range []string{"Options (ts-cli v9.9.9)", "Machines with saved passwords", "Encryption key", "alpha", "Tailscale Devices"} {
		if !strings.Contains(view, want) {
			t.Errorf("options popup: %q missing", want)
		}
	}
	m3 := m2
	m3.opts.showMachineCreds = true
	if view := m3.View(); !strings.Contains(view, "Machines with saved passwords") || !strings.Contains(view, "No machines with saved passwords") || !strings.Contains(view, "alpha") {
		t.Fatal("the credentials list is a popup too")
	}
	m4 := m
	m4.aboutMode = true
	view = m4.View()
	for _, want := range []string{"About ts-cli", "v9.9.9", "Ihor Yatsenko", "alpha", "any key to close"} {
		if !strings.Contains(view, want) {
			t.Errorf("about popup: %q missing", want)
		}
	}
}

func TestProfileSelectionIsAPopup(t *testing.T) {
	m := personalModel(t)
	m.width, m.height = 120, 40
	m.version = "9.9.9"
	m.acct.profileSelectMode = true
	view := m.View()
	for _, want := range []string{"Select Profile (ts-cli v9.9.9)", "All Accounts", "esc/q cancel", "alpha"} {
		if !strings.Contains(view, want) {
			t.Errorf("profile popup: %q missing", want)
		}
	}
}
