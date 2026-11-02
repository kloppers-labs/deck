package viewer

import "deck/internal/ui"

// PlacePopup positions a w×h popup at anchor: below it when it fits, else
// above, else as low as it can go, always clamped inside bounds.
func PlacePopup(anchor ui.Rect, w, h int, bounds ui.Rect) ui.Rect {
	w, h = min(w, bounds.W), min(h, bounds.H)
	bottom := bounds.Y + bounds.H
	y := anchor.Y + anchor.H
	if y+h > bottom {
		if above := anchor.Y - h; above >= bounds.Y {
			y = above
		} else {
			y = bottom - h
		}
	}
	x := min(max(anchor.X, bounds.X), bounds.X+bounds.W-w)
	return ui.Rect{X: x, Y: max(y, bounds.Y), W: w, H: h}
}
