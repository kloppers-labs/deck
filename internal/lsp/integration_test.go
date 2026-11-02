package lsp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const goplsSrc = `package main

import "fmt"

func greet(name string) string {
	return "hello " + name
}

func main() {
	s := "😀 world"; fmt.Println(greet(s))
}
`

func TestGoplsHoverAndDefinition(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	if _, err := NewRegistry(DefaultSpecs(), DefaultEnv()).LookupBinary("gopls"); err != nil {
		t.Skip("gopls not found")
	}
	dir := realTemp(t)
	main := filepath.Join(dir, "main.go")
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/hello\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(main, []byte(goplsSrc), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig()
	cfg.RequestTimeout = time.Minute
	mgr := NewManager(cfg)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = mgr.Shutdown(ctx)
	})
	ctx := context.Background()
	c, err := mgr.ClientFor(ctx, main)
	if err != nil {
		t.Fatal(err)
	}
	if c.Root() != dir {
		t.Fatalf("root = %q, want %q", c.Root(), dir)
	}

	lines := strings.Split(goplsSrc, "\n")
	const callLine = 9
	at := func(word string) Position {
		return Position{Line: callLine, Character: ByteToUnits(lines[callLine], strings.Index(lines[callLine], word), c.Encoding())}
	}

	hv, err := c.Hover(ctx, main, at("Println"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(hv.Markdown, "Println") {
		t.Fatalf("hover = %q", hv.Markdown)
	}

	defs, err := c.Definition(ctx, main, at("greet"))
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 {
		t.Fatalf("definitions = %+v", defs)
	}
	path, ok := URIToPath(defs[0].URI)
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	if !ok || path != main || defs[0].Range.Start.Line != 4 {
		t.Fatalf("definition = %s %+v", path, defs[0].Range)
	}
	defLine := lines[4]
	if col := UnitsToByte(defLine, defs[0].Range.Start.Character, c.Encoding()); !strings.HasPrefix(defLine[col:], "greet") {
		t.Fatalf("definition column %d points at %q", col, defLine[col:])
	}
}
