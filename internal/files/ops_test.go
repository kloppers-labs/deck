package files

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/kloppers-labs/deck/internal/ui"
)

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func TestCreateFileAndDirWithParents(t *testing.T) {
	dir := t.TempDir()
	p, err := CreatePath(dir, "deep/er/new.go")
	if err != nil || p != filepath.Join(dir, "deep", "er", "new.go") {
		t.Fatalf("CreatePath = %q, %v", p, err)
	}
	if fi, err := os.Stat(p); err != nil || fi.IsDir() {
		t.Errorf("a name without a trailing slash makes a file: %v", err)
	}
	d, err := CreatePath(dir, "pkg/sub/")
	if err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(d); err != nil || !fi.IsDir() {
		t.Errorf("a trailing slash makes a directory: %v", err)
	}
	for _, bad := range []string{"", "  ", "../escape.go", "/abs.go", "deep/er/new.go"} {
		if _, err := CreatePath(dir, bad); err == nil {
			t.Errorf("CreatePath(%q) should fail", bad)
		}
	}
}

func TestRenamePath(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "a.go", "b.go")
	p, err := RenamePath(filepath.Join(dir, "a.go"), "c.go")
	if err != nil || p != filepath.Join(dir, "c.go") || !exists(p) || exists(filepath.Join(dir, "a.go")) {
		t.Fatalf("RenamePath = %q, %v", p, err)
	}
	if _, err := RenamePath(p, "b.go"); err == nil {
		t.Error("renaming onto an existing entry must fail")
	}
	if _, err := RenamePath(p, "../x.go"); err == nil {
		t.Error("a rename stays inside the directory")
	}
}

func TestRenameCursorBeforeExtension(t *testing.T) {
	for name, want := range map[string]int{"Program.cs": 7, "Makefile": 8, ".env": 4, "a.test.ts": 6} {
		if got := renameCursor(name, false); got != want {
			t.Errorf("renameCursor(%q) = %d, want %d", name, got, want)
		}
	}
	if got := renameCursor("pkg.d", true); got != 5 {
		t.Errorf("directories put the cursor at the end, got %d", got)
	}
}

func TestRenameKeepsSelection(t *testing.T) {
	dir := fixture(t, map[string]int{"a.go": 3, "b.go": 5, "c.go": 1})
	m := started(t, dir, 100, 24)
	m, _ = press(t, m, "j", "r")
	if !m.Capturing() || !strings.Contains(ansi.Strip(m.View()), "rename") {
		t.Fatalf("r opens an inline rename prompt:\n%s", ansi.Strip(m.View()))
	}
	m, _ = press(t, m, "x", "enter")
	want := filepath.Join(dir, "bx.go")
	if !exists(want) || m.SelectedPath() != want {
		t.Fatalf("rename typed before the extension; selected %q", m.SelectedPath())
	}
	if m.Capturing() || m.viewer.Path() != want {
		t.Errorf("after the rename the prompt closes and the viewer shows %q, got %q", want, m.viewer.Path())
	}
	m, _ = press(t, m, "r", "esc")
	if m.Capturing() || !exists(want) {
		t.Error("esc cancels the rename")
	}
}

func TestNewFileFromTree(t *testing.T) {
	dir := fixture(t, map[string]int{"sub/a.go": 1, "z.go": 1})
	m := started(t, dir, 100, 24)
	m, _ = press(t, m, "a")
	if !m.Capturing() {
		t.Fatal("a opens the new-file prompt")
	}
	m, _ = press(t, m, "n", "e", "w", ".", "g", "o", "enter")
	want := filepath.Join(dir, "sub", "new.go")
	if !exists(want) || m.SelectedPath() != want {
		t.Errorf("a on a directory creates inside it and selects the new file; selected %q", m.SelectedPath())
	}
	m, _ = press(t, m, "G", "a", "d", "/", "enter")
	if fi, err := os.Stat(filepath.Join(dir, "d")); err != nil || !fi.IsDir() {
		t.Errorf("a on a top-level file creates beside it; a trailing / makes a directory: %v", err)
	}
}

func TestTrashMovesToTrashDir(t *testing.T) {
	trash := t.TempDir()
	dir := t.TempDir()
	touch(t, dir, "gone.go")
	src := filepath.Join(dir, "gone.go")
	if err := (Trash{Dir: trash}).Move(src); err != nil {
		t.Fatal(err)
	}
	if exists(src) || !exists(filepath.Join(trash, "gone.go")) {
		t.Fatal("Move puts the file in the trash directory")
	}
	touch(t, dir, "gone.go")
	if err := (Trash{Dir: trash}).Move(src); err != nil || !exists(filepath.Join(trash, "gone 2.go")) {
		t.Errorf("a name clash in the trash gets a numbered name: %v", err)
	}
}

func TestTrashFromTreeConfirms(t *testing.T) {
	trash := t.TempDir()
	defer func(f func() Trash) { trashFor = f }(trashFor)
	trashFor = func() Trash { return Trash{Dir: trash} }
	dir := fixture(t, map[string]int{"a.go": 1, "b.go": 1, "c.go": 1})
	m := started(t, dir, 100, 24)
	m, _ = press(t, m, "j", "D")
	if !strings.Contains(ansi.Strip(m.View()), "y/N") {
		t.Fatalf("D asks for confirmation:\n%s", ansi.Strip(m.View()))
	}
	m, _ = press(t, m, "n")
	if !exists(filepath.Join(dir, "b.go")) || m.Capturing() {
		t.Fatal("anything but y cancels")
	}
	m, _ = press(t, m, "D", "y")
	if exists(filepath.Join(dir, "b.go")) {
		t.Fatal("y moves the file to the trash")
	}
	if m.SelectedPath() != filepath.Join(dir, "c.go") {
		t.Errorf("the selection stays in place on the next entry, got %q", m.SelectedPath())
	}
}

func TestRefuseTrashWithoutTarget(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "keep.go")
	src := filepath.Join(dir, "keep.go")
	if err := (Trash{}).Move(src); err == nil {
		t.Fatal("with no trash directory and no trash command, Move must refuse")
	}
	if !exists(src) {
		t.Error("a refused trash leaves the file alone")
	}
}

func TestCopyPathRelAbs(t *testing.T) {
	var buf bytes.Buffer
	defer func(w io.Writer) { clipboardOut = w }(clipboardOut)
	clipboardOut = &buf
	dir := fixture(t, map[string]int{"sub/a.go": 1})
	m := started(t, dir, 100, 24)
	m, _ = press(t, m, "l", "j")
	_, out := press(t, m, "y", "y")
	if buf.String() != ansi.SetSystemClipboard(filepath.Join("sub", "a.go")) {
		t.Errorf("yy copies the path relative to the launch root over OSC52, wrote %q", buf.String())
	}
	if !hasToast(out, "copied") {
		t.Errorf("copying raises a toast, got %v", out)
	}
	buf.Reset()
	press(t, m, "y", "Y")
	if buf.String() != ansi.SetSystemClipboard(filepath.Join(dir, "sub", "a.go")) {
		t.Errorf("yY copies the absolute path, wrote %q", buf.String())
	}
}

func TestYYChord(t *testing.T) {
	var buf bytes.Buffer
	defer func(w io.Writer) { clipboardOut = w }(clipboardOut)
	clipboardOut = &buf
	dir := fixture(t, map[string]int{"a.go": 1, "b.go": 1})
	m := started(t, dir, 100, 24)
	m, _ = press(t, m, "y")
	if !m.Capturing() {
		t.Fatal("a pending y chord captures so q and digits do not leak")
	}
	m, _ = press(t, m, "esc")
	if m.Capturing() || buf.Len() != 0 {
		t.Error("esc drops the chord without copying")
	}
	m, _ = press(t, m, "y", "j")
	if m.Capturing() || buf.Len() != 0 || m.tree.sel != 1 {
		t.Errorf("another key drops the chord and still acts: sel %d", m.tree.sel)
	}
}

func TestRevealInFinderAndShell(t *testing.T) {
	var revealed string
	defer func(f func(string) error) { revealInOS = f }(revealInOS)
	revealInOS = func(p string) error { revealed = p; return nil }
	dir := fixture(t, map[string]int{"a.go": 1})
	m := started(t, dir, 100, 24)
	press(t, m, "o")
	if revealed != filepath.Join(dir, "a.go") {
		t.Errorf("o reveals the selection, got %q", revealed)
	}
	_, cmd := m.Update(keyMsg("!"))
	if cmd == nil {
		t.Fatal("! suspends deck for a shell")
	}
	sh := shellCommand(filepath.Join(dir, "a.go"))
	if sh.Dir != dir {
		t.Errorf("the shell starts in the selection's directory, got %q", sh.Dir)
	}
}

func hasToast(msgs []tea.Msg, text string) bool {
	for _, msg := range msgs {
		if t, ok := msg.(ui.ToastMsg); ok && strings.Contains(t.Text, text) {
			return true
		}
	}
	return false
}
