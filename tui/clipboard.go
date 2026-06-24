package tui

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
)

// Clipboard functions for copying text to the system clipboard

// copyTextToClipboard copies text to the system clipboard using the appropriate OS command
func copyTextToClipboard(text string) tea.Cmd {
	return func() tea.Msg {
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("pbcopy")
		case "linux":
			cmd = exec.Command("xclip", "-selection", "clipboard")
		case "windows":
			cmd = exec.Command("clip")
		default:
			return copiedMsg{success: false, text: ""}
		}

		stdin, err := cmd.StdinPipe()
		if err != nil {
			return copiedMsg{success: false, text: ""}
		}

		if err := cmd.Start(); err != nil {
			return copiedMsg{success: false, text: ""}
		}

		if _, err := stdin.Write([]byte(text)); err != nil {
			return copiedMsg{success: false, text: ""}
		}

		if err := stdin.Close(); err != nil {
			return copiedMsg{success: false, text: ""}
		}

		if err := cmd.Wait(); err != nil {
			return copiedMsg{success: false, text: ""}
		}

		return copiedMsg{success: true, text: text}
	}
}

// selectedOutputItem returns the currently selected line from command output
func (m model) selectedOutputItem() (string, bool) {
	lines := splitOutputLines(m.hist.commandOutput)
	if len(lines) == 0 {
		return "", false
	}

	cursor := m.hist.outputCursor
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= len(lines) {
		cursor = len(lines) - 1
	}

	entry := strings.TrimSpace(lines[cursor])
	if entry == "" {
		return "", false
	}

	return entry, true
}

// copySelectedOutputItem copies the selected output line to the clipboard
func (m model) copySelectedOutputItem(filenameOnly bool) tea.Cmd {
	entry, ok := m.selectedOutputItem()
	if !ok {
		return func() tea.Msg {
			return copiedMsg{success: false, text: ""}
		}
	}

	textToCopy := entry
	if filenameOnly {
		trimmed := strings.TrimRight(entry, "/")
		base := filepath.Base(trimmed)
		if base == "." || base == "/" || base == "" {
			base = trimmed
		}
		textToCopy = base
	} else if !strings.HasPrefix(entry, "/") && !strings.HasPrefix(entry, "~") {
		// Resolve relative output items to absolute remote paths
		if resolved, err := m.resolveRemoteOutputPath(entry); err == nil && resolved != "" {
			textToCopy = resolved
		}
	}

	return copyTextToClipboard(textToCopy)
}

// readClipboard reads text from the system clipboard and returns a pasteMsg.
// The target parameter is forwarded verbatim so the Update handler knows which
// input field to append the text to.
func readClipboard(target pasteTarget) tea.Cmd {
	return func() tea.Msg {
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("pbpaste")
		case "linux":
			// Prefer wl-paste (Wayland) then xclip (X11), then xsel.
			if _, err := exec.LookPath("wl-paste"); err == nil {
				cmd = exec.Command("wl-paste", "--no-newline")
			} else if _, err := exec.LookPath("xclip"); err == nil {
				cmd = exec.Command("xclip", "-selection", "clipboard", "-o")
			} else if _, err := exec.LookPath("xsel"); err == nil {
				cmd = exec.Command("xsel", "--clipboard", "--output")
			} else {
				return pasteMsg{target: target, err: fmt.Errorf("no clipboard tool found (install wl-paste, xclip or xsel)")}
			}
		case "windows":
			cmd = exec.Command("powershell", "-noprofile", "-command", "Get-Clipboard")
		default:
			return pasteMsg{target: target, err: fmt.Errorf("clipboard paste not supported on %s", runtime.GOOS)}
		}

		out, err := cmd.Output()
		if err != nil {
			return pasteMsg{target: target, err: err}
		}

		return pasteMsg{
			text:   sanitizePastedText(string(out)),
			target: target,
		}
	}
}

// sanitizePastedText strips newlines and non-printable control characters from
// clipboard text, making it safe to insert into any single-line input field.
// Tabs are converted to a single space; other whitespace is preserved.
func sanitizePastedText(s string) string {
	// Normalize line endings then discard them
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")

	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\t':
			b.WriteRune(' ')
		case unicode.IsControl(r):
			// drop all remaining control characters
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}
