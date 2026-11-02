package files

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func touch(t *testing.T, root string, names ...string) {
	t.Helper()
	for _, name := range names {
		p := filepath.Join(root, filepath.FromSlash(name))
		if strings.HasSuffix(name, "/") {
			if err := os.MkdirAll(p, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func rowNames(rows []*Node) []string {
	out := make([]string, len(rows))
	for i, n := range rows {
		out[i] = strings.Repeat("  ", n.Depth) + n.Name
	}
	return out
}

func defaultHide() HidePolicy {
	return NewHidePolicy([]string{"bin", "obj", ".git", "node_modules", ".vs", ".idea", "TestResults"}, false)
}

func newTestTree(t *testing.T, root string) Tree {
	t.Helper()
	tr, cmd := NewTree(root, defaultHide(), SortName)
	if cmd != nil {
		t.Fatal("a small root must load synchronously")
	}
	tr.SetHeight(50)
	return tr
}

func TestFlattenRespectsExpandAndHide(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "src/a.go", "src/inner/b.go", "bin/x.dll", ".env", "z.txt")
	tr := newTestTree(t, dir)
	if got, want := rowNames(tr.Rows()), []string{"src", "z.txt"}; !slices.Equal(got, want) {
		t.Fatalf("collapsed rows = %v, want %v", got, want)
	}
	src := tr.Rows()[0]
	tr.Expand(src)
	tr.Expand(src.Children[0])
	want := []string{"src", "  inner", "    b.go", "  a.go", "z.txt"}
	if got := rowNames(tr.Rows()); !slices.Equal(got, want) {
		t.Errorf("expanded rows = %v, want %v", got, want)
	}
	tr.Collapse(src)
	if got := rowNames(Flatten(tr.Root(), tr.hide)); !slices.Equal(got, []string{"src", "z.txt"}) {
		t.Errorf("collapsing hides the subtree, got %v", got)
	}
}

func TestLazyLoadOnExpand(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "src/a.go")
	tr := newTestTree(t, dir)
	src := tr.Rows()[0]
	if src.Loaded || src.Children != nil {
		t.Fatal("children must not be read before the first expand")
	}
	if cmd := tr.Expand(src); cmd != nil {
		t.Error("a small directory loads in place")
	}
	if !src.Loaded || len(src.Children) != 1 || src.Children[0].Name != "a.go" {
		t.Errorf("after expand: loaded=%v children=%v", src.Loaded, src.Children)
	}

	defer func(n int) { bigDirEntries = n }(bigDirEntries)
	bigDirEntries = 3
	touch(t, dir, "big/1", "big/2", "big/3", "big/4")
	tr.Reload()
	big := tr.find(filepath.Join(dir, "big"))
	cmd := tr.Expand(big)
	if cmd == nil || big.Loaded || !big.loading {
		t.Fatalf("a directory over the cap loads as a command: cmd=%v loaded=%v", cmd != nil, big.Loaded)
	}
	msg, ok := cmd().(dirLoadedMsg)
	if !ok || len(msg.Nodes) != 4 {
		t.Fatalf("load command returned %#v", msg)
	}
	tr.Loaded(msg)
	if !big.Loaded || big.loading || len(big.Children) != 4 {
		t.Errorf("after the async load: loaded=%v children=%d", big.Loaded, len(big.Children))
	}
}

func TestRevealExpandsAncestorsAndSelects(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "a/b/c/target.go", "a/b/other.go", "z.go")
	tr := newTestTree(t, dir)
	target := filepath.Join(dir, "a", "b", "c", "target.go")
	tr.Reveal(target)
	if sel := tr.Selected(); sel == nil || sel.Path != target {
		t.Fatalf("selected %v, want %s", sel, target)
	}
	want := []string{"a", "  b", "    c", "      target.go", "    other.go", "z.go"}
	if got := rowNames(tr.Rows()); !slices.Equal(got, want) {
		t.Errorf("rows = %v, want %v", got, want)
	}

	touch(t, dir, "bin/Debug/out.dll")
	hidden := filepath.Join(dir, "bin", "Debug", "out.dll")
	tr.Reveal(hidden)
	if sel := tr.Selected(); sel == nil || sel.Path != hidden {
		t.Errorf("revealing a hidden file should still show and select it, got %v", sel)
	}
}

func TestRerootUpSelectsChild(t *testing.T) {
	parent := t.TempDir()
	touch(t, parent, "aaa/x.go", "proj/src/a.go", "zzz.go")
	proj := filepath.Join(parent, "proj")
	tr := newTestTree(t, proj)
	tr.Expand(tr.Rows()[0])
	tr.RerootUp()
	if tr.Root().Path != parent {
		t.Fatalf("root = %s, want %s", tr.Root().Path, parent)
	}
	if sel := tr.Selected(); sel == nil || sel.Path != proj {
		t.Errorf("after moving up the old root should be selected, got %v", sel)
	}
	want := []string{"aaa", "proj", "  src", "    a.go", "zzz.go"}
	if got := rowNames(tr.Rows()); !slices.Equal(got, want) {
		t.Errorf("the old root keeps its expansion: rows = %v, want %v", got, want)
	}
}

func TestTildeReturnsToLaunchRoot(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "src/inner/a.go", "z.go")
	tr := newTestTree(t, dir)
	tr.Reveal(filepath.Join(dir, "src", "inner", "a.go"))
	inner := tr.find(filepath.Join(dir, "src", "inner"))
	tr.RerootAt(inner)
	if tr.Root().Path != inner.Path {
		t.Fatalf("= should make the directory the root, root = %s", tr.Root().Path)
	}
	tr.RerootLaunch()
	if tr.Root().Path != dir {
		t.Fatalf("~ should return to %s, root = %s", dir, tr.Root().Path)
	}
	if sel := tr.Selected(); sel == nil || sel.Path != filepath.Join(dir, "src", "inner", "a.go") {
		t.Errorf("~ keeps the selection, got %v", sel)
	}
	tr.RerootUp()
	tr.RerootUp()
	tr.RerootLaunch()
	if tr.Root().Path != dir {
		t.Errorf("~ from above the launch root: root = %s", tr.Root().Path)
	}
}

func TestSortModes(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, size int, age time.Duration) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(strings.Repeat("x", size)), 0o644); err != nil {
			t.Fatal(err)
		}
		at := time.Now().Add(-age)
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
	}
	write("b.txt", 10, 3*time.Hour)
	write("A.go", 300, 2*time.Hour)
	write("c.md", 50, time.Hour)
	touch(t, dir, "zdir/", "adir/")
	cases := []struct {
		mode SortMode
		want []string
	}{
		{SortName, []string{"adir", "zdir", "A.go", "b.txt", "c.md"}},
		{SortModified, []string{"c.md", "A.go", "b.txt"}},
		{SortSize, []string{"A.go", "c.md", "b.txt"}},
		{SortExt, []string{"A.go", "c.md", "b.txt"}},
	}
	for _, c := range cases {
		tr, _ := NewTree(dir, defaultHide(), c.mode)
		got := rowNames(tr.Rows())
		if got[0] != "adir" && got[0] != "zdir" || got[1] != "adir" && got[1] != "zdir" {
			t.Errorf("%v: directories must come first, got %v", c.mode, got)
		}
		files := got[2:]
		if c.mode == SortName {
			files = got
		}
		if !slices.Equal(files, c.want) {
			t.Errorf("%v: order = %v, want %v", c.mode, files, c.want)
		}
	}
	if SortName.Next() != SortModified || SortExt.Next() != SortName {
		t.Error("s cycles name → modified → size → extension → name")
	}
}

func TestHidePolicy(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "bin/a", "obj/b", ".git/c", "node_modules/d", ".env", "dist/e", "keep.go")
	tr := newTestTree(t, dir)
	if got, want := rowNames(tr.Rows()), []string{"dist", "keep.go"}; !slices.Equal(got, want) {
		t.Fatalf("defaults: %v, want %v", got, want)
	}
	tr.SetIgnored(map[string]bool{filepath.Join(dir, "dist"): true})
	if got, want := rowNames(tr.Rows()), []string{"keep.go"}; !slices.Equal(got, want) {
		t.Errorf("gitignored entries hide: %v, want %v", got, want)
	}
	tr.ToggleDotfiles()
	if got, want := rowNames(tr.Rows()), []string{".env", "keep.go"}; !slices.Equal(got, want) {
		t.Errorf(". shows dotfiles but not hidden names: %v, want %v", got, want)
	}
	tr.ToggleDotfiles()
	tr.ToggleIgnored()
	if got, want := rowNames(tr.Rows()), []string{"bin", "dist", "node_modules", "obj", "keep.go"}; !slices.Equal(got, want) {
		t.Errorf("I shows ignored and hidden names: %v, want %v", got, want)
	}
	if !tr.hide.IsIgnored(filepath.Join(dir, "dist", "e")) {
		t.Error("a child of an ignored directory counts as ignored")
	}
}

func TestRowTruncatedToWidth(t *testing.T) {
	n := &Node{Name: strings.Repeat("n", 80) + ".go", Depth: 2}
	for _, w := range []int{10, 20, 31} {
		row := renderRow(n, rowStyle{width: w, icons: "unicode", badge: BadgeModified})
		if got := ansi.StringWidth(row); got != w {
			t.Errorf("width %d: row is %d wide: %q", w, got, ansi.Strip(row))
		}
		if !strings.Contains(ansi.Strip(row), "…") || !strings.HasSuffix(strings.TrimRight(ansi.Strip(row), " "), "M") {
			t.Errorf("width %d: want a truncated name and the badge kept: %q", w, ansi.Strip(row))
		}
	}
}

func TestSymlinkBrokenMarked(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "real/x.go")
	if err := os.Symlink(filepath.Join(dir, "real"), filepath.Join(dir, "link")); err != nil {
		t.Skip("symlinks unsupported:", err)
	}
	if err := os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, "broken")); err != nil {
		t.Fatal(err)
	}
	tr := newTestTree(t, dir)
	byName := map[string]*Node{}
	for _, n := range tr.Rows() {
		byName[n.Name] = n
	}
	if l := byName["link"]; l == nil || !l.IsDir || !l.IsSymlink || l.Broken {
		t.Errorf("a symlinked directory is a directory: %+v", l)
	}
	b := byName["broken"]
	if b == nil || !b.Broken || b.IsDir || b.Target != filepath.Join(dir, "missing") {
		t.Fatalf("broken link: %+v", b)
	}
	row := ansi.Strip(renderRow(b, rowStyle{width: 300, icons: "unicode"}))
	if !strings.Contains(row, "broken → "+filepath.Join(dir, "missing")) {
		t.Errorf("a broken link shows its target: %q", row)
	}
}

func TestIconModes(t *testing.T) {
	dir, file := &Node{Name: "src", IsDir: true}, &Node{Name: "main.go"}
	open := &Node{Name: "src", IsDir: true, Expanded: true}
	cases := []struct {
		mode            string
		dir, open, file string
	}{
		{"unicode", "▸ ", "▾ ", "  "},
		{"none", "+ ", "- ", "  "},
	}
	for _, c := range cases {
		if got := icon(dir, c.mode); got != c.dir {
			t.Errorf("%s dir = %q, want %q", c.mode, got, c.dir)
		}
		if got := icon(open, c.mode); got != c.open {
			t.Errorf("%s open dir = %q, want %q", c.mode, got, c.open)
		}
		if got := icon(file, c.mode); got != c.file {
			t.Errorf("%s file = %q, want %q", c.mode, got, c.file)
		}
	}
	if got := icon(file, "nerd"); ansi.StringWidth(got) != 2 || got == "  " {
		t.Errorf("nerd file icon = %q, want a glyph and a space", got)
	}
}

func BenchmarkTreeFlatten10k(b *testing.B) {
	root := &Node{Path: "/r", IsDir: true, Expanded: true, Loaded: true}
	for i := range 100 {
		d := &Node{Path: fmt.Sprintf("/r/d%03d", i), Name: fmt.Sprintf("d%03d", i), IsDir: true, Expanded: true, Loaded: true}
		for j := range 100 {
			d.Children = append(d.Children, &Node{Path: fmt.Sprintf("%s/f%03d.go", d.Path, j), Name: fmt.Sprintf("f%03d.go", j)})
		}
		root.Children = append(root.Children, d)
	}
	hide := defaultHide()
	b.ResetTimer()
	for b.Loop() {
		if rows := Flatten(root, hide); len(rows) != 10100 {
			b.Fatal(len(rows))
		}
	}
}
