package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

const sgrReset = "\x1b[0m"

// Overlay composites fg over bg with fg's top-left cell at (x, y). Rows are
// treated as padded to bg's widest row, and fg is clipped to that width.
func Overlay(bg, fg string, x, y int) string {
	x, y = max(x, 0), max(y, 0)
	bgLines := strings.Split(bg, "\n")
	width := 0
	for _, l := range bgLines {
		width = max(width, ansi.StringWidth(l))
	}
	for i, fl := range strings.Split(fg, "\n") {
		row := y + i
		if row >= len(bgLines) {
			break
		}
		bgLines[row] = overlayLine(bgLines[row], fl, x, width)
	}
	return strings.Join(bgLines, "\n")
}

func overlayLine(bg, fg string, x, width int) string {
	if x >= width {
		return bg
	}
	if pad := width - ansi.StringWidth(bg); pad > 0 {
		bg += strings.Repeat(" ", pad)
	}
	fg = ansi.Truncate(fg, width-x, "")
	fw := ansi.StringWidth(fg)
	left := ansi.Truncate(bg, x, "")
	cut := x + fw
	right := ansi.TruncateLeft(bg, cut, "")
	for ansi.StringWidth(right) > width-x-fw {
		cut++
		right = ansi.TruncateLeft(bg, cut, "")
	}
	leftPad := x - ansi.StringWidth(left)
	rightPad := width - x - fw - ansi.StringWidth(right)
	return left + sgrReset + strings.Repeat(" ", max(leftPad, 0)) +
		fg + sgrReset + strings.Repeat(" ", max(rightPad, 0)) + right
}

// Dimmed strips styling from a rendered block and redraws it in the Dim style,
// row by row, so it reads as an inactive backdrop.
func Dimmed(s string) string {
	lines := strings.Split(ansi.Strip(s), "\n")
	for i, l := range lines {
		lines[i] = Dim.Render(l)
	}
	return strings.Join(lines, "\n")
}
