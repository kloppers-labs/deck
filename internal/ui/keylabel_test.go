package ui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestAltPrefixFor(t *testing.T) {
	cases := map[string]string{"darwin": "⌥", "linux": "alt+", "windows": "alt+"}
	for goos, want := range cases {
		if got := altPrefixFor(goos); got != want {
			t.Errorf("altPrefixFor(%q) = %q, want %q", goos, got, want)
		}
	}
}

func TestOptionGlyphIsOneCell(t *testing.T) {
	if w := lipgloss.Width("⌥m"); w != 2 {
		t.Errorf("width of ⌥m = %d, want 2", w)
	}
}
