package viewer

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"deck/internal/ui"
)

const (
	defaultLoadLimit = 20 << 20
	bigDocLines      = 50_000
	chunkLines       = 2000
	maxStyledCols    = 4000
)

// Document is a loaded file: Raw has "\r" stripped (editor and LSP columns), Plain
// is tab-expanded and sanitised, and Styled is per-line highlighting ("" until styled).
type Document struct {
	Path      string
	Raw       []string
	Plain     []string
	Styled    []string
	Lang      string
	Size      int64
	Loaded    int64
	ModTime   time.Time
	Truncated bool
	Binary    bool
	Markdown  bool
	TabWidth  int

	highlighted bool
	hlGen       int
	mdReq       [2]int
	chunkGen    []int
	chunkDone   []bool
}

// LoadOptions configures Load. A nil Cache disables caching and a zero Limit
// uses 20 MB.
type LoadOptions struct {
	TabWidth int
	Theme    string
	Cache    *Cache
	Limit    int64
}

var errIsDir = errors.New("is a directory")

// Load reads path whole (up to the limit), splitting, expanding tabs and
// sanitising once. Cached documents are returned as they are.
func Load(path string, o LoadOptions) (*Document, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if fi.IsDir() {
		return nil, errIsDir
	}
	key := cacheKey(path, fi.ModTime(), fi.Size(), o.Theme, o.TabWidth)
	if d, ok := o.Cache.Get(key); ok {
		return d, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	limit := o.Limit
	if limit <= 0 {
		limit = defaultLoadLimit
	}
	data, truncated, err := readFile(f, limit)
	if err != nil {
		return nil, err
	}
	d := newDocument(path, data, o.TabWidth)
	d.Size, d.ModTime, d.Truncated = fi.Size(), fi.ModTime(), truncated
	o.Cache.Put(key, d)
	return d, nil
}

// LoadCmd runs Load off the update loop.
func LoadCmd(path string, gen int, o LoadOptions) tea.Cmd {
	return func() tea.Msg {
		d, err := Load(path, o)
		return DocLoadedMsg{Path: path, Gen: gen, Doc: d, Err: err}
	}
}

func readFile(r io.Reader, limit int64) ([]byte, bool, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(data)) <= limit {
		return data, false, nil
	}
	data = data[:limit]
	if i := bytes.LastIndexByte(data, '\n'); i >= 0 {
		data = data[:i+1]
	}
	return data, true, nil
}

func newDocument(path string, data []byte, tabWidth int) *Document {
	d := &Document{
		Path:     path,
		TabWidth: max(tabWidth, 1),
		Markdown: isMarkdownPath(path),
		Size:     int64(len(data)),
		Loaded:   int64(len(data)),
	}
	if ui.IsBinary(data) {
		d.Binary = true
		return d
	}
	text := strings.TrimSuffix(string(data), "\n")
	raw := strings.Split(text, "\n")
	plain := make([]string, len(raw))
	for i, l := range raw {
		l = strings.TrimSuffix(l, "\r")
		raw[i] = l
		plain[i] = ExpandTabs(sanitizeLine(l), d.TabWidth)
	}
	d.Raw, d.Plain, d.Styled = raw, plain, make([]string, len(raw))
	d.Lang = LexerFor(path, text[:min(len(text), 4096)]).Config().Name
	return d
}

func (d *Document) big(maxBytes int) bool {
	return (maxBytes > 0 && d.Loaded > int64(maxBytes)) || len(d.Raw) > bigDocLines
}

func (d *Document) chunks() int { return (len(d.Raw) + chunkLines - 1) / chunkLines }

func isControl(r rune) bool {
	return (r < 0x20 && r != '\t') || r == 0x7f || (r >= 0x80 && r < 0xa0)
}

// sanitizeLine replaces control characters and invalid bytes with U+FFFD, one
// rune for one rune, so columns still line up with Raw.
func sanitizeLine(s string) string {
	dirty := false
	for _, r := range s {
		if r == utf8.RuneError || isControl(r) {
			dirty = true
			break
		}
	}
	if !dirty {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if isControl(r) {
			r = utf8.RuneError
		}
		b.WriteRune(r)
	}
	return b.String()
}
