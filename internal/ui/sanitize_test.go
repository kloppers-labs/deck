package ui

import (
	"strings"
	"testing"
)

func TestSanitizeTerminalKeepsPlainText(t *testing.T) {
	in := "hello world\n\ttabbed line\n"
	if got := SanitizeTerminal(in); got != in {
		t.Errorf("plain text altered:\n got %q\nwant %q", got, in)
	}
}

func TestSanitizeTerminalKeepsSGRColour(t *testing.T) {
	// Foreground colour, 256-colour, and reset — all SGR, all must survive so
	// syntax highlighting and lipgloss styling keep working.
	in := "\x1b[31mred\x1b[0m \x1b[38;5;82mgreen\x1b[0m"
	if got := SanitizeTerminal(in); got != in {
		t.Errorf("SGR sequences should be preserved:\n got %q\nwant %q", got, in)
	}
}

func TestSanitizeTerminalStripsCursorAndControlBytes(t *testing.T) {
	// The reported corruption: a cursor-up escape plus CR/NUL/BEL that scroll
	// over and overwrite the tab bar.
	in := "before\x1b[5Aafter\rmid\x00\x07\x08end"
	got := SanitizeTerminal(in)
	for name, seq := range map[string]string{
		"cursor-up":       "\x1b[5A",
		"carriage return": "\r",
		"NUL":             "\x00",
		"BEL":             "\x07",
		"backspace":       "\x08",
	} {
		if strings.Contains(got, seq) {
			t.Errorf("output leaked %s: %q", name, got)
		}
	}
	// The visible text must remain, just stitched together.
	if want := "beforeaftermidend"; got != want {
		t.Errorf("visible text wrong:\n got %q\nwant %q", got, want)
	}
}

func TestSanitizeTerminalStripsOSCAndCharsetEscapes(t *testing.T) {
	// OSC (window title) terminated by BEL, and a two-byte charset escape.
	in := "a\x1b]0;evil title\x07b\x1b(0c"
	if want, got := "abc", SanitizeTerminal(in); got != want {
		t.Errorf("non-SGR escapes should be stripped:\n got %q\nwant %q", got, want)
	}
}

func TestSanitizeTerminalKeepsUnicode(t *testing.T) {
	in := "⎇ branch · ▶ 2 running · 📁 café"
	if got := SanitizeTerminal(in); got != in {
		t.Errorf("unicode mangled:\n got %q\nwant %q", got, in)
	}
}

func TestSanitizeTerminalHandlesLoneAndTruncatedEscapes(t *testing.T) {
	// A lone trailing ESC and a truncated CSI must not panic and must vanish.
	for _, in := range []string{"text\x1b", "text\x1b[", "text\x1b[38;5"} {
		if got := SanitizeTerminal(in); got != "text" {
			t.Errorf("input %q: got %q, want %q", in, got, "text")
		}
	}
}
