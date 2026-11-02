package viewer

import "testing"

func TestFindSmartCase(t *testing.T) {
	lines := []string{"Foo foo FOO", "bar"}
	ms, err := Find(lines, "foo", false)
	if err != nil || len(ms) != 3 {
		t.Fatalf("lowercase query is case-insensitive: got %v, %v", ms, err)
	}
	ms, _ = Find(lines, "Foo", false)
	if len(ms) != 1 || ms[0] != (Match{Line: 0, Start: 0, End: 3}) {
		t.Errorf("a capital makes it case-sensitive: got %v", ms)
	}
}

func TestFindLiteralAndRegex(t *testing.T) {
	lines := []string{"a.b axb", "x+y"}
	ms, _ := Find(lines, "a.b", false)
	if len(ms) != 1 {
		t.Errorf("literal a.b matched %d times, want 1", len(ms))
	}
	ms, _ = Find(lines, "a.b", true)
	if len(ms) != 2 {
		t.Errorf("regex a.b matched %d times, want 2", len(ms))
	}
	if _, err := Find(lines, "(", true); err == nil {
		t.Error("an invalid regex should report an error")
	}
	if ms, _ := Find(lines, "x*", true); len(ms) != 2 {
		t.Errorf("empty regex matches are skipped, got %v", ms)
	}
}

func TestFindDisplayColumns(t *testing.T) {
	ms, _ := Find([]string{"日本 key"}, "key", false)
	if len(ms) != 1 || ms[0].Start != 5 || ms[0].End != 8 {
		t.Errorf("match columns are display columns, got %v", ms)
	}
}

func TestNextPrevWrap(t *testing.T) {
	ms := []Match{{0, 2, 3}, {3, 0, 1}, {7, 4, 5}}
	cases := []struct {
		at         Pos
		next, prev int
	}{
		{Pos{0, 0}, 0, 2},
		{Pos{0, 2}, 1, 2},
		{Pos{3, 0}, 2, 0},
		{Pos{5, 0}, 2, 1},
		{Pos{9, 0}, 0, 2},
	}
	for _, c := range cases {
		if got := NextMatch(ms, c.at); got != c.next {
			t.Errorf("NextMatch(%v) = %d, want %d", c.at, got, c.next)
		}
		if got := PrevMatch(ms, c.at); got != c.prev {
			t.Errorf("PrevMatch(%v) = %d, want %d", c.at, got, c.prev)
		}
	}
	if NextMatch(nil, Pos{}) != -1 || PrevMatch(nil, Pos{}) != -1 {
		t.Error("no matches gives -1")
	}
}
