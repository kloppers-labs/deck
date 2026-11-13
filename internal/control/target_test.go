package control

import "testing"

func TestParseTarget(t *testing.T) {
	cases := []struct {
		in                 string
		path               string
		line, col, endLine int
	}{
		{"x.go", "x.go", 0, 0, 0},
		{"x.go:12", "x.go", 12, 0, 0},
		{"x.go:12:3", "x.go", 12, 3, 0},
		{"x.go:10-20", "x.go", 10, 0, 20},
		{"notes:v2.txt", "notes:v2.txt", 0, 0, 0},
		{"build-2024", "build-2024", 0, 0, 0},
		{"a:b:7", "a:b", 7, 0, 0},
		{"x.go:20-10", "x.go", 20, 0, 0},
		{"dir/", "dir/", 0, 0, 0},
		{":12", ":12", 0, 0, 0},
	}
	for _, tc := range cases {
		path, line, col, end := ParseTarget(tc.in)
		if path != tc.path || line != tc.line || col != tc.col || end != tc.endLine {
			t.Errorf("ParseTarget(%q) = %q,%d,%d,%d; want %q,%d,%d,%d",
				tc.in, path, line, col, end, tc.path, tc.line, tc.col, tc.endLine)
		}
	}
}

func TestDirsListsEveryCandidate(t *testing.T) {
	got := Dirs(env("XDG_RUNTIME_DIR", "/run/u", "XDG_STATE_HOME", "/s"), "/h")
	want := []string{"/run/u/deck", "/s/deck/run", "/h/.local/state/deck/run"}
	if len(got) != len(want) {
		t.Fatalf("Dirs = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Dirs[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if got := Dirs(env(), "/h"); len(got) != 1 || got[0] != "/h/.local/state/deck/run" {
		t.Errorf("Dirs without XDG = %q", got)
	}
}
