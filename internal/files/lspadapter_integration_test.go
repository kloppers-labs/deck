package files

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kloppers-labs/deck/internal/lsp"
	"github.com/kloppers-labs/deck/internal/viewer"
)

const adapterGoSrc = `package main

import "fmt"

func greet(name string) string {
	return "hello " + name
}

func main() {
	s := "😀 world"; fmt.Println(greet(s))
}
`

func TestAdapterAgainstGopls(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	if _, err := lsp.NewRegistry(lsp.DefaultSpecs(), lsp.DefaultEnv()).LookupBinary("gopls"); err != nil {
		t.Skip("gopls not found")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(dir, "main.go")
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/hello\n\ngo 1.22\n")
	writeFile(t, main, adapterGoSrc)

	cfg := lsp.DefaultConfig()
	cfg.RequestTimeout = time.Minute
	mgr := lsp.NewManager(cfg)
	a := NewLSPAdapter(mgr)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = a.Shutdown(ctx)
	})
	ctx := context.Background()
	line := strings.Split(adapterGoSrc, "\n")[9]
	col := len([]rune(line[:strings.Index(line, "greet")]))

	defs, err := a.Definition(ctx, main, viewer.Loc{Path: main, Line: 9, Col: col})
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 {
		t.Fatalf("definitions = %+v", defs)
	}
	got := defs[0]
	if p, err := filepath.EvalSymlinks(got.Path); err == nil {
		got.Path = p
	}
	if got.Path != main || got.Range.Start != (viewer.Pos{Line: 4, Col: 5}) || !strings.HasPrefix(got.Text, "func greet") {
		t.Errorf("definition = %+v", got)
	}
	h, err := a.Hover(ctx, main, viewer.Loc{Path: main, Line: 9, Col: col})
	if err != nil || !strings.Contains(h.Markdown, "greet") {
		t.Errorf("hover = %q, %v", h.Markdown, err)
	}
	syms, err := a.DocumentSymbols(ctx, main)
	if err != nil || len(syms) < 2 || syms[0].Name != "greet" {
		t.Errorf("symbols = %+v, %v", syms, err)
	}
	if st := a.Status(main); st.Label != "Go" || !st.Usable() {
		t.Errorf("status = %+v", st)
	}
}
