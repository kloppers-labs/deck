package ui

import "time"

// DoubleClickWindow is the longest gap between two presses that still counts as
// a double-click.
const DoubleClickWindow = 400 * time.Millisecond

// ClickTracker counts consecutive presses on the same cell within
// DoubleClickWindow of each other.
type ClickTracker struct {
	x, y, count int
	at          time.Time
}

// Press records a press at (x, y) and returns how many presses in a row landed
// on that cell: 1 for a single click, 2 for a double-click, and so on.
func (c *ClickTracker) Press(x, y int, at time.Time) int {
	if c.count > 0 && x == c.x && y == c.y && at.Sub(c.at) <= DoubleClickWindow {
		c.count++
	} else {
		c.count = 1
	}
	c.x, c.y, c.at = x, y, at
	return c.count
}

// IsDouble reports whether a Press count is exactly a double-click.
func IsDouble(count int) bool { return count == 2 }
