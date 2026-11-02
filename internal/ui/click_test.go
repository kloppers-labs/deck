package ui

import (
	"testing"
	"time"
)

func TestClickTrackerDouble(t *testing.T) {
	t0 := time.Unix(1000, 0)
	cases := []struct {
		name   string
		x2, y2 int
		gap    time.Duration
		want   int
	}{
		{"same cell quickly", 4, 2, 150 * time.Millisecond, 2},
		{"same cell at the limit", 4, 2, 400 * time.Millisecond, 2},
		{"same cell too slow", 4, 2, 401 * time.Millisecond, 1},
		{"different column", 5, 2, 100 * time.Millisecond, 1},
		{"different row", 4, 3, 100 * time.Millisecond, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var ct ClickTracker
			if got := ct.Press(4, 2, t0); got != 1 {
				t.Fatalf("first press count = %d, want 1", got)
			}
			if got := ct.Press(c.x2, c.y2, t0.Add(c.gap)); got != c.want {
				t.Errorf("second press count = %d, want %d", got, c.want)
			}
		})
	}
}

func TestClickTrackerDoubleIsDetectedOnce(t *testing.T) {
	t0 := time.Unix(1000, 0)
	var ct ClickTracker
	ct.Press(1, 1, t0)
	if !IsDouble(ct.Press(1, 1, t0.Add(100*time.Millisecond))) {
		t.Fatal("second quick press should be a double-click")
	}
	if IsDouble(ct.Press(1, 1, t0.Add(200*time.Millisecond))) {
		t.Error("a third quick press should not count as another double-click")
	}
}

func TestRectContainsAndLocal(t *testing.T) {
	r := Rect{X: 10, Y: 5, W: 4, H: 2}
	cases := []struct {
		x, y int
		in   bool
	}{
		{10, 5, true}, {13, 6, true}, {14, 5, false}, {10, 7, false}, {9, 5, false}, {10, 4, false},
	}
	for _, c := range cases {
		if got := r.Contains(c.x, c.y); got != c.in {
			t.Errorf("Contains(%d,%d) = %v, want %v", c.x, c.y, got, c.in)
		}
	}
	if x, y := r.Local(12, 6); x != 2 || y != 1 {
		t.Errorf("Local(12,6) = (%d,%d), want (2,1)", x, y)
	}
}
