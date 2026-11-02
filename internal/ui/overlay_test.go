package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestOverlayPreservesWidth(t *testing.T) {
	bgRows := []string{
		blue("abcdefghij") + green("klmnopqrst"),
		blue("ABCDEFGHIJ") + green("KLMNOPQRST"),
		"日本語のテキストです",
		blue("0123456789") + "0123456789",
		"....................",
	}
	bg := strings.Join(bgRows, "\n")
	fg := strings.Join([]string{"XXXX", "YYYY", "ZZZZ"}, "\n")

	out := Overlay(bg, green(fg), 3, 1)
	rows := strings.Split(out, "\n")
	if len(rows) != len(bgRows) {
		t.Fatalf("overlay changed the row count: %d, want %d", len(rows), len(bgRows))
	}
	for i, row := range rows {
		if got, want := ansi.StringWidth(row), ansi.StringWidth(bgRows[i]); got != want {
			t.Errorf("row %d width = %d, want %d", i, got, want)
		}
		assertWellFormedEscapes(t, row)
	}
	for _, i := range []int{0, 4} {
		if rows[i] != bgRows[i] {
			t.Errorf("row %d outside the overlay changed: %q", i, rows[i])
		}
	}
	for i, mark := range map[int]string{1: "XXXX", 2: "YYYY", 3: "ZZZZ"} {
		got := ansi.Strip(rows[i])
		orig := ansi.Strip(bgRows[i])
		if !strings.Contains(got, mark) {
			t.Errorf("row %d = %q, want the overlay %q in it", i, got, mark)
		}
		if i == 2 {
			continue
		}
		if got[:3] != orig[:3] || got[7:] != orig[7:] {
			t.Errorf("row %d untouched regions changed: %q vs %q", i, got, orig)
		}
	}
}

func TestOverlayClipsToBackground(t *testing.T) {
	out := Overlay("0123456789\n0123456789", "ABCDEFGH", 6, 0)
	rows := strings.Split(out, "\n")
	if got := ansi.Strip(rows[0]); got != "012345ABCD" {
		t.Errorf("row 0 = %q, want the overlay clipped at the right edge", got)
	}
	if rows[1] != "0123456789" {
		t.Errorf("row 1 changed: %q", rows[1])
	}
}

func TestOverlayBeyondBottomIgnored(t *testing.T) {
	out := Overlay("aaaa\nbbbb", "X\nY\nZ", 1, 1)
	if got := ansi.Strip(out); got != "aaaa\nbXbb" {
		t.Errorf("overlay = %q", got)
	}
}
