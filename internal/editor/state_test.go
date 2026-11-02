package editor

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func writeState(t *testing.T, body string, mtime time.Time) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "state")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if !mtime.IsZero() {
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func TestReadStateGood(t *testing.T) {
	launched := time.Now().Add(-time.Minute)
	p := writeState(t, "/src/a.go\n40 7 31\n/src/a.go\n/src/b.go\n/src/a.go\n", time.Time{})
	st, ok := ReadState(p, launched)
	if !ok {
		t.Fatal("a fresh, well-formed state file should be read")
	}
	if st.Path != "/src/a.go" || st.Line != 40 || st.Col != 7 || st.Top != 31 {
		t.Errorf("state = %+v", st)
	}
	if want := []string{"/src/a.go", "/src/b.go"}; !slices.Equal(st.Saved, want) {
		t.Errorf("saved = %q, want %q (deduplicated)", st.Saved, want)
	}
}

func TestReadStateMissing(t *testing.T) {
	if _, ok := ReadState(filepath.Join(t.TempDir(), "nope"), time.Now()); ok {
		t.Error("a missing state file should be ignored")
	}
	if _, ok := ReadState("", time.Now()); ok {
		t.Error("no state file at all should be ignored")
	}
}

func TestReadStateStaleIgnored(t *testing.T) {
	launched := time.Now()
	p := writeState(t, "/src/a.go\n1 1 1\n", launched.Add(-time.Hour))
	if _, ok := ReadState(p, launched); ok {
		t.Error("a state file older than the launch should be ignored")
	}
}

func TestReadStateMalformedIgnored(t *testing.T) {
	for _, body := range []string{"", "/src/a.go\n", "/src/a.go\nx y z\n", "relative.go\n1 1 1\n", "/src/a.go\n0 1 1\n"} {
		p := writeState(t, body, time.Time{})
		if _, ok := ReadState(p, time.Now().Add(-time.Minute)); ok {
			t.Errorf("malformed state %q should be ignored", body)
		}
	}
}

func TestReadStateOutsideRootFallsBack(t *testing.T) {
	origin := Target{Path: "/work/proj/a.go", Line: 10, Col: 2, Top: 5}
	st := State{Path: "/etc/hosts", Line: 3, Col: 1, Top: 1}
	if got := ReturnTarget(origin, st, true, "follow", "/work/proj"); got != origin {
		t.Errorf("a path outside the root returns to the origin, got %+v", got)
	}
	st.Path = "/work/project-two/a.go"
	if got := ReturnTarget(origin, st, true, "follow", "/work/proj"); got != origin {
		t.Errorf("a sibling directory sharing a prefix is outside the root, got %+v", got)
	}
}

func TestFollowVsOrigin(t *testing.T) {
	origin := Target{Path: "/work/proj/a.go", Line: 10, Col: 2, Top: 5}
	st := State{Path: "/work/proj/sub/b.go", Line: 80, Col: 4, Top: 70}
	follow := Target{Path: st.Path, Line: 80, Col: 4, Top: 70}
	cases := []struct {
		mode string
		ok   bool
		want Target
	}{
		{"follow", true, follow},
		{"", true, follow},
		{"origin", true, origin},
		{"follow", false, origin},
	}
	for _, c := range cases {
		if got := ReturnTarget(origin, st, c.ok, c.mode, "/work/proj"); got != c.want {
			t.Errorf("mode %q ok=%v: got %+v, want %+v", c.mode, c.ok, got, c.want)
		}
	}
}

func TestStatePathsAreUnique(t *testing.T) {
	a, b := StatePath(), StatePath()
	if a == b || !filepath.IsAbs(a) {
		t.Errorf("StatePath should give distinct absolute paths, got %q and %q", a, b)
	}
}
