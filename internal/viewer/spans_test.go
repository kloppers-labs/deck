package viewer

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

const (
	red = "\x1b[31m"
	inv = "\x1b[7m"
	bg  = "\x1b[44m"
)

func TestPaintPlain(t *testing.T) {
	got := Paint("hello", []Span{{Start: 1, End: 3, SGR: inv}})
	want := "h" + inv + "el" + sgrReset + "lo"
	if got != want {
		t.Errorf("Paint = %q, want %q", got, want)
	}
}

func TestPaintRestoresUnderlyingStyle(t *testing.T) {
	styled := red + "abcd" + sgrReset
	got := Paint(styled, []Span{{Start: 1, End: 2, SGR: inv}})
	want := red + "a" + inv + "b" + sgrReset + red + "cd" + sgrReset
	if got != want {
		t.Errorf("Paint = %q, want %q", got, want)
	}
	if ansi.Strip(got) != "abcd" {
		t.Errorf("visible text changed: %q", ansi.Strip(got))
	}
}

func TestPaintReappliesSpanAfterTokenChange(t *testing.T) {
	styled := red + "ab" + sgrReset + "\x1b[32m" + "cd" + sgrReset
	got := Paint(styled, []Span{{Start: 0, End: 4, SGR: bg}})
	if !strings.Contains(got, "\x1b[32m"+bg+"c") {
		t.Errorf("span should be re-applied after the token's own SGR: %q", got)
	}
}

func TestPaintLaterSpanWins(t *testing.T) {
	got := Paint("abcdef", []Span{{Start: 0, End: 6, SGR: bg}, {Start: 2, End: 3, SGR: inv}})
	want := bg + "ab" + sgrReset + inv + "c" + sgrReset + bg + "def" + sgrReset
	if got != want {
		t.Errorf("Paint = %q, want %q", got, want)
	}
}

func TestPaintWideRunes(t *testing.T) {
	got := Paint("日本x", []Span{{Start: 2, End: 4, SGR: inv}})
	want := "日" + inv + "本" + sgrReset + "x"
	if got != want {
		t.Errorf("Paint = %q, want %q", got, want)
	}
}

func TestPaintNoSpans(t *testing.T) {
	if got := Paint(red+"x"+sgrReset, nil); got != red+"x"+sgrReset {
		t.Errorf("no spans should return the input, got %q", got)
	}
}
