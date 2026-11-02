package viewer

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/alecthomas/chroma/v2"
	"github.com/charmbracelet/x/ansi"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadReadsWholeFileNoShortRead(t *testing.T) {
	content := strings.Repeat("0123456789abcdef\n", 20000)
	data, truncated, err := readFile(iotest.OneByteReader(strings.NewReader(content)), 1<<30)
	if err != nil || truncated {
		t.Fatalf("readFile: truncated=%v err=%v", truncated, err)
	}
	if string(data) != content {
		t.Errorf("read %d bytes, want %d", len(data), len(content))
	}
	d := newDocument("big.txt", data, 4)
	if len(d.Raw) != 20000 {
		t.Errorf("lines = %d, want 20000", len(d.Raw))
	}
}

func TestLoadDetectsBinary(t *testing.T) {
	p := writeFile(t, t.TempDir(), "blob.bin", "bplist00\x00\x00\r\x1b[5A")
	d, err := Load(p, LoadOptions{TabWidth: 4})
	if err != nil {
		t.Fatal(err)
	}
	if !d.Binary || len(d.Raw) != 0 {
		t.Errorf("binary = %v, lines = %d; want a binary document with no lines", d.Binary, len(d.Raw))
	}
}

func TestLoadTruncatesOver20MB(t *testing.T) {
	content := strings.Repeat("abcdefghi\n", 100)
	p := writeFile(t, t.TempDir(), "huge.log", content)
	d, err := Load(p, LoadOptions{TabWidth: 4, Limit: 205})
	if err != nil {
		t.Fatal(err)
	}
	if !d.Truncated || len(d.Raw) != 20 || d.Size != int64(len(content)) {
		t.Fatalf("truncated=%v lines=%d size=%d", d.Truncated, len(d.Raw), d.Size)
	}
	m := sized(New(Options{}), 80, 10)
	m = deliver(t, m, d)
	if h := ansi.Strip(strings.Split(m.View(), "\n")[0]); !strings.Contains(h, "showing 200 B of 1000 B") {
		t.Errorf("header should carry the truncation banner, got %q", h)
	}
}

func TestLoadSplitsAndSanitises(t *testing.T) {
	p := writeFile(t, t.TempDir(), "a.go", "a\tb\r\nc\x07d\n\n")
	d, err := Load(p, LoadOptions{TabWidth: 4})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a\tb", "c\x07d", ""}; strings.Join(d.Raw, "|") != strings.Join(want, "|") {
		t.Errorf("Raw = %q, want %q", d.Raw, want)
	}
	if d.Plain[0] != "a   b" || d.Plain[1] != "c�d" {
		t.Errorf("Plain = %q", d.Plain)
	}
	if d.Lang != "Go" {
		t.Errorf("Lang = %q, want Go", d.Lang)
	}
}

func TestLoadEmptyFileHasOneLine(t *testing.T) {
	d, err := Load(writeFile(t, t.TempDir(), "empty.txt", ""), LoadOptions{})
	if err != nil || len(d.Raw) != 1 {
		t.Fatalf("empty file: %v lines, err %v", len(d.Raw), err)
	}
}

func TestLoadDirectoryFails(t *testing.T) {
	if _, err := Load(t.TempDir(), LoadOptions{}); !errors.Is(err, errIsDir) {
		t.Errorf("err = %v, want errIsDir", err)
	}
}

func TestHighlightLinesPreserveCount(t *testing.T) {
	src := []string{
		"class A {",
		"    /* a comment",
		"       spanning lines */",
		"    int x = 1;",
		"}",
	}
	out := Highlight("a.cs", src, "dracula", 4)
	if len(out) != len(src) {
		t.Fatalf("lines = %d, want %d", len(out), len(src))
	}
	for i, l := range out {
		if !strings.HasSuffix(l, sgrReset) {
			t.Errorf("line %d does not end with a reset: %q", i, l)
		}
		if got := ansi.Strip(l); got != ExpandTabs(src[i], 4) {
			t.Errorf("line %d text = %q, want %q", i, got, src[i])
		}
	}
	comment := paletteFor("dracula", detectProfile()).code(chroma.CommentMultiline)
	for i, word := range map[int]string{1: "/*", 2: "spanning"} {
		at := strings.Index(out[i], word)
		if at < 0 || !strings.Contains(out[i][:at], comment) {
			t.Errorf("comment line %d is not styled as a comment: %q", i, out[i])
		}
	}
}

func TestHighlightExpandsTabsAndKeepsEmptyLines(t *testing.T) {
	out := Highlight("x.go", []string{"\tx := 1", "", "}"}, "dracula", 4)
	if len(out) != 3 || ansi.Strip(out[0]) != "    x := 1" || ansi.Strip(out[1]) != "" {
		t.Errorf("Highlight = %q", out)
	}
}

func TestHighlightSanitisesControlBytes(t *testing.T) {
	out := Highlight("x.txt", []string{"a\x1b[2Jb\x07c"}, "dracula", 4)
	if strings.Contains(out[0], "\x1b[2J") || strings.ContainsRune(out[0], '\x07') {
		t.Errorf("control bytes leaked: %q", out[0])
	}
	if got := ansi.Strip(out[0]); got != "a�[2Jb�c" {
		t.Errorf("visible text = %q", got)
	}
}

func TestHighlightLongLinesStayPlain(t *testing.T) {
	long := strings.Repeat("x = 1; ", 700)
	out := Highlight("a.js", []string{long, "var y = 2;"}, "dracula", 4)
	if out[0] != long+sgrReset {
		t.Error("a line over 4000 columns should be left unhighlighted")
	}
	if out[1] == "var y = 2;"+sgrReset {
		t.Error("the next line should still be highlighted")
	}
}

func TestEntrySGRProfiles(t *testing.T) {
	pal := paletteFor("dracula", profileTrue)
	if !strings.Contains(pal.code(chroma.Keyword), "38;2;") {
		t.Errorf("truecolour keyword SGR = %q", pal.code(chroma.Keyword))
	}
	pal = paletteFor("dracula", profile256)
	if !strings.Contains(pal.code(chroma.Keyword), "38;5;") {
		t.Errorf("256-colour keyword SGR = %q", pal.code(chroma.Keyword))
	}
}

func TestNearest256(t *testing.T) {
	cases := map[[3]uint8]int{
		{0, 0, 0}:          16,
		{255, 255, 255}:    231,
		{255, 0, 0}:        196,
		{128, 128, 128}:    244,
		{0x28, 0x2a, 0x36}: 235,
	}
	for c, want := range cases {
		if got := nearest256(c[0], c[1], c[2]); got != want {
			t.Errorf("nearest256(%v) = %d, want %d", c, got, want)
		}
	}
}

func TestLexMap(t *testing.T) {
	cases := map[string]string{
		"App.csproj":      "XML",
		"Directory.props": "XML",
		"build.targets":   "XML",
		"Strings.resx":    "XML",
		"MainPage.xaml":   "XML",
		"App.slnx":        "XML",
		"Index.razor":     "HTML",
		"_Layout.cshtml":  "HTML",
		"App.sln":         "INI",
		".editorconfig":   "INI",
		"api.http":        "plaintext",
		"Program.cs":      "C#",
		"main.go":         "Go",
		"web.config":      "XML",
		"Package.nuspec":  "XML",
	}
	for path, want := range cases {
		if got := LexerFor(path, "").Config().Name; got != want {
			t.Errorf("LexerFor(%q) = %q, want %q", path, got, want)
		}
	}
	if got := LexerFor("script", "#!/bin/bash\necho hi\n").Config().Name; got != "Bash" {
		t.Errorf("extensionless shebang file = %q, want Bash", got)
	}
}

func TestCacheLRU(t *testing.T) {
	c := NewCache(2)
	now := time.Unix(1000, 0)
	k1 := cacheKey("a", now, 10, "dracula", 4)
	k2 := cacheKey("b", now, 10, "dracula", 4)
	k3 := cacheKey("c", now, 10, "dracula", 4)
	c.Put(k1, &Document{Path: "a"})
	c.Put(k2, &Document{Path: "b"})
	c.Get(k1)
	c.Put(k3, &Document{Path: "c"})
	if _, ok := c.Get(k2); ok {
		t.Error("the least recently used entry should be evicted")
	}
	if _, ok := c.Get(k1); !ok {
		t.Error("a recently read entry should survive")
	}
	if c.Len() != 2 {
		t.Errorf("len = %d, want 2", c.Len())
	}
	for _, other := range []string{
		cacheKey("a", now.Add(time.Second), 10, "dracula", 4),
		cacheKey("a", now, 11, "dracula", 4),
		cacheKey("a", now, 10, "monokai", 4),
		cacheKey("a", now, 10, "dracula", 8),
	} {
		if _, ok := c.Get(other); ok {
			t.Errorf("key %q should miss: mtime, size, theme and tab width are all part of the key", other)
		}
	}
}

func TestLoadUsesCache(t *testing.T) {
	p := writeFile(t, t.TempDir(), "a.go", "package a\n")
	c := NewCache(4)
	d1, _ := Load(p, LoadOptions{TabWidth: 4, Cache: c})
	d2, _ := Load(p, LoadOptions{TabWidth: 4, Cache: c})
	if d1 != d2 {
		t.Error("an unchanged file should come from the cache")
	}
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(p, later, later); err != nil {
		t.Fatal(err)
	}
	if d3, _ := Load(p, LoadOptions{TabWidth: 4, Cache: c}); d3 == d1 {
		t.Error("a changed mtime should reload")
	}
}

func TestSanitizeStyledKeepsOnlySGR(t *testing.T) {
	in := "\x1b[1mbold\x1b[0m\x1b]0;title\x07\x1b[2Jx\ty\x00"
	if got := sanitizeStyled(in); got != "\x1b[1mbold\x1b[0mx    y" {
		t.Errorf("sanitizeStyled = %q", got)
	}
}

func TestConcurrentLoadHighlightAndRender(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "a.cs", "class A {\n  /* x */\n  int y;\n}\n")
	readme := writeFile(t, dir, "r.md", "# T\n\ntext\n")
	cache := NewCache(4)
	done := make(chan *Document, 8)
	for range 8 {
		go func() {
			d, _ := Load(src, LoadOptions{TabWidth: 4, Theme: "dracula", Cache: cache})
			Highlight(d.Path, d.Raw, "dracula", 4)
			renderMarkdown("# T\n\ntext\n", 40, "dracula")
			done <- d
		}()
	}
	md, err := Load(readme, LoadOptions{TabWidth: 4, Cache: cache})
	if err != nil {
		t.Fatal(err)
	}
	m := deliver(t, sized(New(Options{}), 60, 10), md)
	for range 8 {
		d := <-done
		m, _ = m.Update(DocHighlightedMsg{Doc: d, Gen: m.gen, Lines: Highlight(d.Path, d.Raw, "dracula", 4)})
		_ = m.View()
	}
}
