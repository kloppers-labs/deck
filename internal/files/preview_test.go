package files

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPreviewBinaryFileNoControlLeak is the regression test for the tab bar
// getting scribbled over: previewing a binary file (here a .DS_Store-style
// plist with NUL, CR, and a cursor-up escape) must return a clean placeholder
// rather than the raw bytes.
func TestPreviewBinaryFileNoControlLeak(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".DS_Store")
	blob := []byte("bplist00\x00\x00\r\x1b[5A{{722, 3\x00\x07\x08commands")
	if err := os.WriteFile(path, blob, 0o644); err != nil {
		t.Fatal(err)
	}
	out := Preview(path, 80, "dracula")
	for name, seq := range map[string]string{
		"NUL":       "\x00",
		"cursor-up": "\x1b[5A",
		"BEL":       "\x07",
		"backspace": "\x08",
	} {
		if strings.Contains(out, seq) {
			t.Errorf("binary preview leaked %s: %q", name, out)
		}
	}
	if !strings.Contains(out, "binary file") {
		t.Errorf("expected a binary placeholder, got %q", out)
	}
}

func TestPreviewTextFileStillRenders(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hello.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := Preview(path, 80, "dracula")
	if !strings.Contains(out, "package") || !strings.Contains(out, "main") {
		t.Errorf("text file should still preview its contents, got %q", out)
	}
}

func TestExpandMermaidNoFence(t *testing.T) {
	src := "# Title\n\nSome text, no diagrams.\n"
	if got := expandMermaid(src); got != src {
		t.Errorf("non-mermaid source was altered:\n got %q\nwant %q", got, src)
	}
}

func TestExpandMermaidLeftAloneWhenBinaryMissing(t *testing.T) {
	prev := mermaidBin
	mermaidBin = func() (string, error) { return "", errors.New("not found") }
	t.Cleanup(func() { mermaidBin = prev })

	src := "intro\n\n```mermaid\ngraph LR\nA --> B\n```\n\noutro\n"
	if got := expandMermaid(src); got != src {
		t.Errorf("fence should be untouched when mermaid-ascii is absent:\n got %q\nwant %q", got, src)
	}
}

func TestExpandMermaidRendersWhenBinaryPresent(t *testing.T) {
	prev := mermaidBin
	// Stub the binary with `cat`, which echoes the diagram body back: this
	// exercises the replace path without depending on mermaid-ascii.
	mermaidBin = func() (string, error) { return "cat", nil }
	t.Cleanup(func() { mermaidBin = prev })

	src := "```mermaid\ngraph LR\nA --> B\n```\n"
	got := expandMermaid(src)
	if strings.Contains(got, "```mermaid") {
		t.Errorf("mermaid fence label should be replaced, got %q", got)
	}
	if !strings.Contains(got, "graph LR") {
		t.Errorf("rendered output should contain the piped diagram body, got %q", got)
	}
}
