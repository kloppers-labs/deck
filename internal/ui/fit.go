package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Fit pads or clips a rendered block to exactly h rows of exactly w columns,
// without splitting ANSI escape sequences.
func Fit(s string, w, h int) string {
	if h <= 0 {
		return ""
	}
	w = max(w, 0)
	lines := strings.Split(s, "\n")
	out := make([]string, h)
	for i := range out {
		var line string
		if i < len(lines) {
			line = strings.ReplaceAll(lines[i], "\t", "    ")
		}
		line = ansi.Truncate(line, w, "")
		if pad := w - ansi.StringWidth(line); pad > 0 {
			line += strings.Repeat(" ", pad)
		}
		out[i] = line
	}
	return strings.Join(out, "\n")
}

// FitWidth clips a single line to at most w columns, ending in tail when cut.
func FitWidth(s string, w int, tail string) string {
	return ansi.Truncate(s, max(w, 0), tail)
}
