package ui

// Rect is a screen region in cells, used for mouse hit-testing.
type Rect struct{ X, Y, W, H int }

func (r Rect) Contains(x, y int) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

func (r Rect) Local(x, y int) (int, int) { return x - r.X, y - r.Y }
