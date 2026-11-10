package files

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kloppers-labs/deck/internal/ui/picker"
)

type fakeLister struct {
	calls   []string
	outputs map[string][]string
	fail    map[string]bool
}

func (f *fakeLister) list(_ context.Context, _ string, limit int, name string, _ ...string) ([]string, bool, error) {
	f.calls = append(f.calls, name)
	if f.fail[name] {
		return nil, false, errors.New(name + " failed")
	}
	out := f.outputs[name]
	if limit > 0 && len(out) > limit {
		return out[:limit], true, nil
	}
	return out, false, nil
}

func lookPathFor(found ...string) func(string) (string, error) {
	return func(name string) (string, error) {
		if slices.Contains(found, name) {
			return "/bin/" + name, nil
		}
		return "", errors.New("not found")
	}
}

func TestIndexSourceSelection(t *testing.T) {
	dir := fixture(t, map[string]int{"walked.go": 1, "bin/skip.dll": 1})
	hide := NewHidePolicy([]string{"bin"}, false)
	cases := []struct {
		name, want string
		found      []string
		fail       []string
	}{
		{name: "fd first", want: "fd", found: []string{"fd", "git"}},
		{name: "fdfind counts as fd", want: "fd", found: []string{"fdfind", "git"}},
		{name: "git without fd", want: "git", found: []string{"git"}},
		{name: "git outside a repo falls back to walking", want: "walk", found: []string{"git"}, fail: []string{"git"}},
		{name: "walk with no tools", want: "walk"},
	}
	for _, c := range cases {
		f := &fakeLister{outputs: map[string][]string{
			"fd": {"from-fd.go"}, "fdfind": {"from-fd.go"}, "git": {"from-git.go", "bin/hidden.go"},
		}, fail: map[string]bool{}}
		for _, n := range c.fail {
			f.fail[n] = true
		}
		idx := buildIndex(context.Background(), indexEnv{lookPath: lookPathFor(c.found...), list: f.list}, dir, hide, 100)
		if idx.Source != c.want {
			t.Errorf("%s: source %q, want %q (calls %v)", c.name, idx.Source, c.want, f.calls)
		}
		switch c.want {
		case "git":
			if strings.Join(idx.Paths, ",") != "from-git.go" {
				t.Errorf("%s: git paths are filtered by the hide policy: %v", c.name, idx.Paths)
			}
		case "walk":
			if strings.Join(idx.Paths, ",") != "walked.go" {
				t.Errorf("%s: the walk skips hidden names: %v", c.name, idx.Paths)
			}
		}
	}
}

func TestIndexCapped(t *testing.T) {
	var many []string
	for i := range 10 {
		many = append(many, fmt.Sprintf("f%d.go", i))
	}
	f := &fakeLister{outputs: map[string][]string{"fd": many}}
	idx := buildIndex(context.Background(), indexEnv{lookPath: lookPathFor("fd"), list: f.list}, t.TempDir(), HidePolicy{}, 3)
	if len(idx.Paths) != 3 || !idx.Capped {
		t.Errorf("the index stops at the cap: %d paths, capped %v", len(idx.Paths), idx.Capped)
	}
	dir := fixture(t, map[string]int{"a.go": 1, "b.go": 1, "c.go": 1, "d.go": 1})
	idx = buildIndex(context.Background(), indexEnv{lookPath: lookPathFor(), list: f.list}, dir, HidePolicy{}, 2)
	if len(idx.Paths) != 2 || !idx.Capped {
		t.Errorf("the walk honours the cap too: %v capped %v", idx.Paths, idx.Capped)
	}
}

func TestListCommandSplitsNULs(t *testing.T) {
	out, capped, err := listCommand(context.Background(), t.TempDir(), 2, "printf", `./a.go\000b c.go\000d.go\000`)
	if err != nil || capped != true || strings.Join(out, "|") != "a.go|b c.go" {
		t.Errorf("listCommand = %q capped %v err %v", out, capped, err)
	}
}

func TestFilenameBeatsPathScore(t *testing.T) {
	items := []picker.Item{
		{Title: "src/LeadSnap/Other.cs"},
		{Title: "src/Api/Services/LeadSnap.cs"},
		{Title: "docs/notes.md"},
	}
	got := matchFiles("leadsnap", items)
	if len(got) != 2 || items[got[0].Index].Title != "src/Api/Services/LeadSnap.cs" {
		t.Fatalf("a file-name match ranks above a directory match: %+v", got)
	}
	if fmt.Sprint(got[0].Positions) != "[17 18 19 20 21 22 23 24]" {
		t.Errorf("positions point into the file name within the path: %v", got[0].Positions)
	}
	if got := matchFiles("leadsnap:120", items); len(got) != 2 {
		t.Errorf("a :line suffix is not part of the match, got %d", len(got))
	}
}

func TestSplitLineSuffix(t *testing.T) {
	cases := map[string][3]any{
		"file.cs:120":   {"file.cs", 120, 0},
		"file.cs:12:7":  {"file.cs", 12, 7},
		"file.cs":       {"file.cs", 0, 0},
		"file.cs:":      {"file.cs:", 0, 0},
		"C:thing":       {"C:thing", 0, 0},
		"a:b:3":         {"a:b", 3, 0},
		"Program.cs:x1": {"Program.cs:x1", 0, 0},
	}
	for in, want := range cases {
		q, line, col := splitLineSuffix(in)
		if q != want[0] || line != want[1] || col != want[2] {
			t.Errorf("splitLineSuffix(%q) = %q %d %d, want %v", in, q, line, col, want)
		}
	}
}

func useIndex(t *testing.T, paths ...string) {
	t.Helper()
	old := finderEnv
	t.Cleanup(func() { finderEnv = old })
	f := &fakeLister{outputs: map[string][]string{"fd": paths}}
	finderEnv = indexEnv{lookPath: lookPathFor("fd"), list: f.list}
}

func typeKeys(s string) []string {
	var out []string
	for _, r := range s {
		out = append(out, string(r))
	}
	return out
}

func TestFinderOpensAndPeeks(t *testing.T) {
	dir := fixture(t, map[string]int{"a.go": 3, "b.go": 40})
	useIndex(t, "a.go", "b.go")
	m := started(t, dir, 120, 30)
	m, _ = press(t, m, "ctrl+p")
	if !m.pick.Active() || !m.Capturing() || m.pick.Total() != 2 {
		t.Fatalf("ctrl+p opens the finder over the index: active %v total %d", m.pick.Active(), m.pick.Total())
	}
	m, _ = press(t, m, "down")
	if m.viewer.Path() != filepath.Join(dir, "b.go") || m.SelectedPath() != filepath.Join(dir, "a.go") {
		t.Errorf("moving peeks in the viewer without moving the tree: viewer %q tree %q", m.viewer.Path(), m.SelectedPath())
	}
	m, _ = press(t, m, "esc")
	if m.pick.Active() || m.viewer.Path() != filepath.Join(dir, "a.go") {
		t.Errorf("esc closes the finder and restores the viewer, got %q", m.viewer.Path())
	}
}

func TestAcceptRevealsAndPushesHistory(t *testing.T) {
	dir := fixture(t, map[string]int{"a.go": 30, "sub/deep/b.go": 40})
	useIndex(t, "a.go", "sub/deep/b.go")
	m := started(t, dir, 120, 30)
	m, _ = press(t, m, "j", "enter", "j", "j")
	m, _ = press(t, m, append([]string{"ctrl+p"}, typeKeys("bgo")...)...)
	m, _ = press(t, m, "enter")
	want := filepath.Join(dir, "sub", "deep", "b.go")
	if m.SelectedPath() != want || m.viewer.Path() != want || m.focus != focusViewer {
		t.Fatalf("enter reveals and opens the file: tree %q viewer %q", m.SelectedPath(), m.viewer.Path())
	}
	if got := rowNames(m.tree.Rows()); strings.Join(got, ",") != "sub,  deep,    b.go,a.go" {
		t.Errorf("the ancestors are expanded: %v", got)
	}
	back, ok := m.history.Back(m.viewer.Loc())
	if m.history.Len() == 0 || !ok || back.Path != filepath.Join(dir, "a.go") || back.Line != 2 {
		t.Errorf("the jump is recorded in the history: %+v %v", back, ok)
	}
}

func TestPathColonLineJumps(t *testing.T) {
	dir := fixture(t, map[string]int{"a.go": 3, "b.go": 200})
	useIndex(t, "a.go", "b.go")
	m := started(t, dir, 120, 30)
	m, _ = press(t, m, append([]string{"ctrl+p"}, typeKeys("b.go:120:4")...)...)
	if m.pick.Len() != 1 {
		t.Fatalf("the :line suffix filters by the name only, shown %d", m.pick.Len())
	}
	m, _ = press(t, m, "enter")
	if loc := m.viewer.Loc(); loc.Path != filepath.Join(dir, "b.go") || loc.Line != 119 || loc.Col != 3 {
		t.Errorf("file.go:120:4 opens at line 120 col 4, got %+v", loc)
	}
}

func TestFinderIndexBuiltOnceAndRefreshedWhenStale(t *testing.T) {
	dir := fixture(t, map[string]int{"a.go": 3})
	f := &fakeLister{outputs: map[string][]string{"fd": {"a.go"}}}
	old := finderEnv
	defer func() { finderEnv = old }()
	finderEnv = indexEnv{lookPath: lookPathFor("fd"), list: f.list}
	m := started(t, dir, 120, 30)
	if len(f.calls) != 0 {
		t.Fatal("the index is built on first use, not at startup")
	}
	m, _ = press(t, m, "ctrl+p", "esc", "ctrl+p", "esc")
	if len(f.calls) != 1 {
		t.Errorf("a fresh index is reused, built %d times", len(f.calls))
	}
	m, _ = settle(t, m, ReloadCurrent)
	press(t, m, "ctrl+p")
	if len(f.calls) != 2 {
		t.Errorf("a reload marks the index stale and the next open rebuilds it, built %d times", len(f.calls))
	}
}
