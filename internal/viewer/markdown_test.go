package viewer

import (
	"errors"
	"strings"
	"testing"
)

func TestIsMarkdownPath(t *testing.T) {
	cases := map[string]bool{
		"README.md":      true,
		"notes.MARKDOWN": true,
		"main.go":        false,
		"plain":          false,
	}
	for path, want := range cases {
		if got := isMarkdownPath(path); got != want {
			t.Errorf("isMarkdownPath(%q) = %v, want %v", path, got, want)
		}
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

func TestExpandMermaidRendersAndCaches(t *testing.T) {
	prev := mermaidBin
	mermaidBin = func() (string, error) { return "cat", nil }
	t.Cleanup(func() { mermaidBin = prev })

	src := "```mermaid\ngraph LR\nA --> B\n```\n"
	got := expandMermaid(src)
	if strings.Contains(got, "```mermaid") || !strings.Contains(got, "graph LR") {
		t.Errorf("mermaid fence should be replaced by the tool output, got %q", got)
	}
	mermaidBin = func() (string, error) { return "cat", nil }
	if again := expandMermaid(src); again != got {
		t.Errorf("a second render should come from the cache: %q vs %q", again, got)
	}
}

func TestRenderMarkdownUnknownThemeFallsBack(t *testing.T) {
	out := renderMarkdown("# Hello\n\nworld\n", 60, "not-a-glamour-style")
	if !strings.Contains(out, "Hello") || strings.Contains(out, "# Hello") {
		t.Errorf("unknown theme should fall back to dracula and still render, got %q", out)
	}
}
