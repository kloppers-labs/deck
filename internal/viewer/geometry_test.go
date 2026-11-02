package viewer

import (
	"math/rand"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestExpandTabs(t *testing.T) {
	cases := []struct {
		in   string
		tw   int
		want string
	}{
		{"abc", 4, "abc"},
		{"\tx", 4, "    x"},
		{"a\tb", 4, "a   b"},
		{"abcd\te", 4, "abcd    e"},
		{"日\tx", 4, "日  x"},
		{"\t\t", 2, "    "},
		{"a\tb", 0, "a b"},
	}
	for _, c := range cases {
		if got := ExpandTabs(c.in, c.tw); got != c.want {
			t.Errorf("ExpandTabs(%q, %d) = %q, want %q", c.in, c.tw, got, c.want)
		}
	}
}

func TestDispColAndRuneCol(t *testing.T) {
	cases := []struct {
		line    string
		runeCol int
		disp    int
	}{
		{"hello", 0, 0},
		{"hello", 3, 3},
		{"hello", 5, 5},
		{"\tx", 1, 4},
		{"a\tb", 2, 4},
		{"日本語x", 1, 2},
		{"日本語x", 3, 6},
		{"😀a", 1, 2},
		{"a😀b", 2, 3},
	}
	for _, c := range cases {
		if got := DispCol(c.line, c.runeCol, 4); got != c.disp {
			t.Errorf("DispCol(%q, %d) = %d, want %d", c.line, c.runeCol, got, c.disp)
		}
		if got := RuneCol(c.line, c.disp, 4); got != c.runeCol {
			t.Errorf("RuneCol(%q, %d) = %d, want %d", c.line, c.disp, got, c.runeCol)
		}
	}
	if got := RuneCol("\tx", 2, 4); got != 0 {
		t.Errorf("a column inside a tab maps to the tab, got %d", got)
	}
	if got := RuneCol("日x", 1, 4); got != 0 {
		t.Errorf("the second cell of a wide rune maps to that rune, got %d", got)
	}
	if got := RuneCol("ab", 40, 4); got != 2 {
		t.Errorf("a column past the end clamps to the line end, got %d", got)
	}
}

func TestDocToScreenAndBack(t *testing.T) {
	lines := []string{"package main", "\tfunc x() {}", "日本語 text", ""}
	cases := []struct {
		name string
		l    Layout
		p    Pos
		x, y int
		ok   bool
	}{
		{"origin", Layout{Gutter: 5, TextW: 20, Height: 10, TabWidth: 4}, Pos{0, 0}, 5, 0, true},
		{"after tab", Layout{Gutter: 5, TextW: 20, Height: 10, TabWidth: 4}, Pos{1, 1}, 9, 1, true},
		{"wide", Layout{Gutter: 0, TextW: 20, Height: 10, TabWidth: 4}, Pos{2, 2}, 4, 2, true},
		{"scrolled top", Layout{Gutter: 0, TextW: 20, Height: 10, TabWidth: 4, Top: 1}, Pos{2, 0}, 0, 1, true},
		{"above top", Layout{Gutter: 0, TextW: 20, Height: 10, TabWidth: 4, Top: 1}, Pos{0, 0}, 0, 0, false},
		{"left offset", Layout{Gutter: 2, TextW: 5, Height: 10, TabWidth: 4, Left: 4}, Pos{0, 6}, 4, 0, true},
		{"left of offset", Layout{Gutter: 2, TextW: 5, Height: 10, TabWidth: 4, Left: 4}, Pos{0, 1}, 0, 0, false},
		{"wrapped second row", Layout{Gutter: 1, TextW: 5, Height: 10, TabWidth: 4, Wrap: true}, Pos{0, 7}, 3, 1, true},
		{"after wrapped line", Layout{Gutter: 1, TextW: 5, Height: 10, TabWidth: 4, Wrap: true}, Pos{1, 0}, 1, 3, true},
		{"below height", Layout{Gutter: 0, TextW: 20, Height: 2, TabWidth: 4}, Pos{3, 0}, 0, 0, false},
	}
	for _, c := range cases {
		x, y, ok := DocToScreen(lines, c.l, c.p)
		if ok != c.ok || (ok && (x != c.x || y != c.y)) {
			t.Errorf("%s: DocToScreen = (%d, %d, %v), want (%d, %d, %v)", c.name, x, y, ok, c.x, c.y, c.ok)
			continue
		}
		if !ok {
			continue
		}
		back, ok := ScreenToDoc(lines, c.l, x, y)
		if !ok || back != c.p {
			t.Errorf("%s: ScreenToDoc(%d, %d) = %v (%v), want %v", c.name, x, y, back, ok, c.p)
		}
	}
}

func TestScreenToDocClamps(t *testing.T) {
	lines := []string{"abc", "de"}
	l := Layout{Gutter: 4, TextW: 20, Height: 10, TabWidth: 4}
	if p, ok := ScreenToDoc(lines, l, 15, 1); !ok || p != (Pos{1, 2}) {
		t.Errorf("click past the line end = %v %v, want {1 2}", p, ok)
	}
	if p, ok := ScreenToDoc(lines, l, 1, 0); !ok || p != (Pos{0, 0}) {
		t.Errorf("click in the gutter = %v %v, want {0 0}", p, ok)
	}
	if _, ok := ScreenToDoc(lines, l, 5, 5); ok {
		t.Error("a click below the last line should report !ok")
	}
}

func TestGeometryRoundTrip(t *testing.T) {
	alphabet := []rune("abcxyz \t日本語😀🎉_{}")
	rng := rand.New(rand.NewSource(7))
	for iter := range 400 {
		var lines []string
		for range 1 + rng.Intn(6) {
			var b strings.Builder
			for range rng.Intn(30) {
				b.WriteRune(alphabet[rng.Intn(len(alphabet))])
			}
			lines = append(lines, b.String())
		}
		l := Layout{
			Gutter:   rng.Intn(6),
			TextW:    3 + rng.Intn(20),
			Height:   200,
			TabWidth: 1 + rng.Intn(8),
			Wrap:     iter%2 == 0,
		}
		for li, line := range lines {
			for col := 0; col <= utf8.RuneCountInString(line); col++ {
				p := Pos{li, col}
				view := l
				if !view.Wrap {
					view.Left = max(DispCol(line, col, l.TabWidth)-rng.Intn(l.TextW), 0)
				}
				x, y, ok := DocToScreen(lines, view, p)
				if !ok {
					t.Fatalf("%v not visible in %+v for %q", p, view, line)
				}
				back, ok := ScreenToDoc(lines, view, x, y)
				if !ok || back != p {
					t.Fatalf("round trip %v -> (%d,%d) -> %v in %+v for %q", p, x, y, back, view, line)
				}
			}
		}
	}
}

func TestEnsureVisible(t *testing.T) {
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = strings.Repeat("x", 50)
	}
	l := Layout{TextW: 20, Height: 10, TabWidth: 4}
	if got := EnsureVisible(lines, l, Pos{8, 0}); got.Top != 2 {
		t.Errorf("cursor near the bottom: top = %d, want 2 (3-row scroll-off)", got.Top)
	}
	l.Top = 50
	if got := EnsureVisible(lines, l, Pos{51, 0}); got.Top != 48 {
		t.Errorf("cursor near the top: top = %d, want 48", got.Top)
	}
	if got := EnsureVisible(lines, l, Pos{99, 0}); got.Top != 90 {
		t.Errorf("cursor at the end: top = %d, want 90", got.Top)
	}
	l.Top = 0
	if got := EnsureVisible(lines, l, Pos{0, 30}); got.Left != 19 {
		t.Errorf("cursor right of the view: left = %d, want 19 (8-col scroll-off)", got.Left)
	}
	l.Left = 30
	if got := EnsureVisible(lines, l, Pos{0, 32}); got.Left != 24 {
		t.Errorf("cursor near the left edge: left = %d, want 24", got.Left)
	}
	l.Wrap, l.Left = true, 30
	if got := EnsureVisible(lines, l, Pos{0, 40}); got.Left != 0 {
		t.Errorf("wrap mode never scrolls horizontally, left = %d", got.Left)
	}
}
