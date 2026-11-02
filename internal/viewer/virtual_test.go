package viewer

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"deck/internal/ui"
)

func TestOpenVirtualShowsPrebuiltDocument(t *testing.T) {
	m := sized(New(Options{}), 40, 8)
	plain := []string{"card/", "2 files", "needle here"}
	styled := []string{"\x1b[1mcard/\x1b[0m", "2 files", "needle here"}
	built := 0
	m, cmd := m.OpenVirtual("/tmp/card", nil, func() (*Document, error) {
		built++
		return NewVirtualDocument("/tmp/card", plain, styled), nil
	})
	if built != 0 || m.Loaded() {
		t.Fatal("the card must be built by the command, not by OpenVirtual")
	}
	m, follow := m.Update(cmd())
	if !m.Loaded() || !m.Highlighted() {
		t.Fatalf("a virtual document arrives styled: loaded=%v highlighted=%v", m.Loaded(), m.Highlighted())
	}
	if follow != nil {
		if _, ok := follow().(DocHighlightedMsg); ok {
			t.Error("a virtual document must not be re-highlighted")
		}
	}
	rows := strings.Split(ansi.Strip(m.View()), "\n")
	if !strings.HasPrefix(rows[1], "card/") || !strings.HasPrefix(rows[2], "2 files") {
		t.Errorf("a virtual document has no line-number gutter: %q", rows)
	}
	m = typeText(t, press(t, m, "/"), "needle")
	m = press(t, m, "enter")
	if m.Loc().Line != 2 {
		t.Errorf("search works on the card, cursor line = %d", m.Loc().Line)
	}

	m, stale := m.OpenVirtual("/tmp/other", nil, func() (*Document, error) { return NewVirtualDocument("/tmp/other", nil, nil), nil })
	old := stale()
	m, _ = m.Open("/tmp/third", nil)
	m, _ = m.Update(old)
	if m.Loaded() {
		t.Error("a superseded virtual load is dropped like any stale load")
	}
}

func TestPlacePopupFlipsAndClamps(t *testing.T) {
	bounds := ui.Rect{X: 0, Y: 0, W: 80, H: 20}
	cases := []struct {
		name   string
		anchor ui.Rect
		w, h   int
		want   ui.Rect
	}{
		{"below", ui.Rect{X: 10, Y: 2, W: 4, H: 1}, 20, 5, ui.Rect{X: 10, Y: 3, W: 20, H: 5}},
		{"flips above", ui.Rect{X: 10, Y: 17, W: 4, H: 1}, 20, 5, ui.Rect{X: 10, Y: 12, W: 20, H: 5}},
		{"clamped right", ui.Rect{X: 70, Y: 2, W: 1, H: 1}, 20, 5, ui.Rect{X: 60, Y: 3, W: 20, H: 5}},
		{"beside, zero-height anchor", ui.Rect{X: 30, Y: 4}, 20, 5, ui.Rect{X: 30, Y: 4, W: 20, H: 5}},
		{"no room either side", ui.Rect{X: 0, Y: 9, W: 1, H: 1}, 10, 15, ui.Rect{X: 0, Y: 5, W: 10, H: 15}},
		{"bigger than bounds", ui.Rect{X: 0, Y: 0, W: 1, H: 1}, 100, 30, ui.Rect{X: 0, Y: 0, W: 80, H: 20}},
	}
	for _, c := range cases {
		if got := PlacePopup(c.anchor, c.w, c.h, bounds); got != c.want {
			t.Errorf("%s: PlacePopup = %+v, want %+v", c.name, got, c.want)
		}
	}
}
