package viewer

import "testing"

func TestWordAt(t *testing.T) {
	cases := []struct {
		line string
		col  int
		want string
	}{
		{"var @class = 1;", 6, "@class"},
		{"var @class = 1;", 4, "@class"},
		{"List<T> items", 0, "List"},
		{"List<T> items", 2, "List"},
		{"List<T> items", 5, "T"},
		{"this._field = x", 6, "_field"},
		{"naïve日本 = 1", 3, "naïve日本"},
		{"a + b", 1, ""},
		{"x", 5, ""},
		{"foo.Bar()", 4, "Bar"},
		{"user@example", 5, "example"},
	}
	for _, c := range cases {
		if got := WordAt(c.line, c.col); got != c.want {
			t.Errorf("WordAt(%q, %d) = %q, want %q", c.line, c.col, got, c.want)
		}
	}
}

func TestNextPrevWordStart(t *testing.T) {
	line := "  foo.bar(baz)  qux"
	if got := nextWordStart(line, 2); got != 5 {
		t.Errorf("next from foo = %d, want 5 (the dot)", got)
	}
	if got := nextWordStart(line, 5); got != 6 {
		t.Errorf("next from the dot = %d, want 6", got)
	}
	if got := nextWordStart(line, 13); got != 16 {
		t.Errorf("next across spaces = %d, want 16", got)
	}
	if got := prevWordStart(line, 16); got != 13 {
		t.Errorf("prev from qux = %d, want 13", got)
	}
	if got := prevWordStart(line, 6); got != 5 {
		t.Errorf("prev from bar = %d, want 5", got)
	}
	if got := prevWordStart(line, 2); got != 0 {
		t.Errorf("prev from the first word = %d, want 0", got)
	}
}
