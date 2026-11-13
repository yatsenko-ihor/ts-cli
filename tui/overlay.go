package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// overlayCenter draws box over bg, centred in a width x height terminal (the
// size of bg when unknown): the screen stays visible around the popup. Both
// are ANSI-styled; lines are cut by display cells, never in the middle of an
// escape sequence.
func overlayCenter(bg, box string, width, height int) string {
	lines := strings.Split(bg, "\n")
	boxLines := strings.Split(box, "\n")
	boxW := 0
	for _, l := range boxLines {
		boxW = max(boxW, lipgloss.Width(l))
	}
	if width <= 0 {
		for _, l := range lines {
			width = max(width, lipgloss.Width(l))
		}
	}
	if height <= 0 {
		height = len(lines)
	}
	x := max(0, (width-boxW)/2)
	y := max(0, (height-len(boxLines))/2)
	for len(lines) < y+len(boxLines) {
		lines = append(lines, "")
	}
	for i, bl := range boxLines {
		row := y + i
		left := ansi.Truncate(lines[row], x, "")
		if pad := x - lipgloss.Width(left); pad > 0 {
			left += strings.Repeat(" ", pad)
		}
		right := ansi.TruncateLeft(lines[row], x+boxW, "")
		lines[row] = left + "\x1b[0m" + bl + "\x1b[0m" + right
	}
	return strings.Join(lines, "\n")
}

// moveLine moves the cursor one logical line up (-1) or down (+1), keeping
// the column where the line is long enough.
func moveLine(value string, cursor, delta int) int {
	r := []rune(value)
	cursor = max(0, min(cursor, len(r)))
	start := cursor
	for start > 0 && r[start-1] != '\n' {
		start--
	}
	col := cursor - start
	if delta < 0 {
		if start == 0 {
			return cursor
		}
		prevEnd := start - 1
		prevStart := prevEnd
		for prevStart > 0 && r[prevStart-1] != '\n' {
			prevStart--
		}
		return prevStart + min(col, prevEnd-prevStart)
	}
	end := cursor
	for end < len(r) && r[end] != '\n' {
		end++
	}
	if end == len(r) {
		return cursor
	}
	nextStart := end + 1
	nextEnd := nextStart
	for nextEnd < len(r) && r[nextEnd] != '\n' {
		nextEnd++
	}
	return nextStart + min(col, nextEnd-nextStart)
}

// renderMultiline shows value wrapped to width with the cursor mark, as at
// most rows lines around the cursor.
func renderMultiline(value string, cursor int, mark string, width, rows int) string {
	r := []rune(value)
	cursor = max(0, min(cursor, len(r)))
	withCursor := append(append(append([]rune{}, r[:cursor]...), []rune(mark)...), r[cursor:]...)
	var visual []string
	curLine := 0
	for _, line := range strings.Split(string(withCursor), "\n") {
		cells := []rune(line)
		if len(cells) == 0 {
			visual = append(visual, "")
			continue
		}
		cur := ""
		for _, c := range cells {
			if lipgloss.Width(cur+string(c)) > width {
				visual = append(visual, cur)
				cur = ""
			}
			cur += string(c)
		}
		visual = append(visual, cur)
	}
	for i, l := range visual {
		if strings.Contains(l, mark) {
			curLine = i
			break
		}
	}
	start := 0
	if curLine >= rows {
		start = curLine - rows + 1
	}
	end := min(len(visual), start+rows)
	out := visual[start:end]
	for len(out) < rows {
		out = append(out, "")
	}
	return strings.Join(out, "\n")
}
