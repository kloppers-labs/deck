package ui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

var sgr = regexp.MustCompile(`\x1b\[[0-9;:]*m`)

func red(s string) string   { return "\x1b[31m" + s + "\x1b[0m" }
func green(s string) string { return "\x1b[32m" + s + "\x1b[0m" }
func blue(s string) string  { return "\x1b[34m" + s + "\x1b[0m" }

func assertWellFormedEscapes(t *testing.T, s string) {
	t.Helper()
	if rest := sgr.ReplaceAllString(s, ""); strings.Contains(rest, "\x1b") {
		t.Errorf("escape sequence was split: %q", s)
	}
}

func TestFitPadsAndClips(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		w, h  int
		strip []string
	}{
		{"pads rows and columns", "abc", 5, 3, []string{"abc  ", "     ", "     "}},
		{"clips extra rows", "1\n2\n3\n4", 3, 2, []string{"1  ", "2  "}},
		{"clips styled wide line", red("hello world"), 5, 1, []string{"hello"}},
		{"clips double-width runes", "日本語テキスト", 5, 1, []string{"日本 "}},
		{"expands tabs", "\tx", 6, 1, []string{"    x "}},
		{"zero height", "abc", 5, 0, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := Fit(c.in, c.w, c.h)
			if c.h == 0 {
				if out != "" {
					t.Fatalf("Fit with h=0 = %q, want empty", out)
				}
				return
			}
			lines := strings.Split(out, "\n")
			if len(lines) != c.h {
				t.Fatalf("got %d rows, want %d", len(lines), c.h)
			}
			for i, line := range lines {
				if got := ansi.StringWidth(line); got != c.w {
					t.Errorf("row %d width = %d, want %d", i, got, c.w)
				}
				if got := ansi.Strip(line); got != c.strip[i] {
					t.Errorf("row %d = %q, want %q", i, got, c.strip[i])
				}
				assertWellFormedEscapes(t, line)
			}
		})
	}
}

func TestFitKeepsTrailingReset(t *testing.T) {
	out := Fit(red("hello world"), 5, 1)
	if !strings.HasSuffix(out, "\x1b[0m") {
		t.Errorf("clipped styled line lost its reset, colour would bleed: %q", out)
	}
}
