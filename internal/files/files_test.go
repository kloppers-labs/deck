package files

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSortEntriesDirsFirst(t *testing.T) {
	in := []entry{
		{name: "zebra.go", isDir: false},
		{name: "src", isDir: true},
		{name: "Apple.txt", isDir: false},
		{name: "build", isDir: true},
	}
	sortEntries(in)
	want := []string{"build", "src", "Apple.txt", "zebra.go"}
	for i, w := range want {
		if in[i].name != w {
			t.Errorf("position %d = %q, want %q (order: %+v)", i, in[i].name, w, in)
		}
	}
}

func TestIsMarkdown(t *testing.T) {
	cases := map[string]bool{
		"README.md":      true,
		"notes.MARKDOWN": true,
		"main.go":        false,
		"style.css":      false,
		"plain":          false,
	}
	for path, want := range cases {
		if got := isMarkdown(path); got != want {
			t.Errorf("isMarkdown(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestPreviewHighlightsCode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snippet.go")
	if err := os.WriteFile(path, []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := Preview(path, 80, "dracula")
	if out == "" {
		t.Fatal("expected non-empty preview for a Go file")
	}
	if len(out) <= len("package main") {
		t.Errorf("expected highlighted output longer than raw source, got %q", out)
	}
}

func TestPreviewMissingFile(t *testing.T) {
	out := Preview(filepath.Join(t.TempDir(), "nope.go"), 80, "dracula")
	if out == "" {
		t.Fatal("expected an error message, got empty string")
	}
}

func TestPreviewWithUnknownTheme(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snippet.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := Preview(path, 80, "not-a-real-theme"); out == "" {
		t.Fatal("expected non-empty preview even with an unknown theme")
	}
}

func TestFocusToggle(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(dir, "dracula")
	m.SetSize(80, 24) // give the list a usable page size for cursor movement
	if m.Capturing() {
		t.Fatal("new pane should start in list focus, not capturing")
	}

	m, _ = m.Update(keyMsg("f"))
	if !m.Capturing() {
		t.Fatal("pressing f should focus the preview (capturing)")
	}

	startCursor := m.list.Index()
	m, _ = m.Update(keyMsg("j"))
	if m.list.Index() != startCursor {
		t.Errorf("j in preview focus moved cursor from %d to %d", startCursor, m.list.Index())
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.Capturing() {
		t.Fatal("esc should return focus to the list")
	}

	m, _ = m.Update(keyMsg("j"))
	if m.list.Index() == startCursor {
		t.Error("j in list focus should advance the cursor")
	}
}

func TestCapturingWhileFiltering(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(dir, "dracula")
	m.SetSize(80, 24)
	if m.Capturing() {
		t.Fatal("list focus without an active filter should not capture")
	}
	m, _ = m.Update(keyMsg("/"))
	if !m.Capturing() {
		t.Error("'/' should start the filter and capture the keyboard")
	}
}

func keyMsg(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}
