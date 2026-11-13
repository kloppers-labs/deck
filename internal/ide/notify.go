package ide

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/kloppers-labs/deck/internal/presence"
)

type lineCache struct {
	mu    sync.Mutex
	path  string
	mod   time.Time
	size  int64
	lines []string
}

func (c *lineCache) line(path string, n int) string {
	fi, err := os.Stat(path)
	if err != nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.path != path || !c.mod.Equal(fi.ModTime()) || c.size != fi.Size() {
		data, err := os.ReadFile(path)
		if err != nil {
			return ""
		}
		c.path, c.mod, c.size = path, fi.ModTime(), fi.Size()
		c.lines = strings.Split(strings.ReplaceAll(string(data), "\r", ""), "\n")
	}
	if n < 1 || n > len(c.lines) {
		return ""
	}
	return c.lines[n-1]
}

// utf16Col converts a rune column to UTF-16 code units, the unit VS Code reports.
func utf16Col(line string, runeCol int) int {
	units, n := 0, 0
	for _, r := range line {
		if n == runeCol {
			return units
		}
		units++
		if r >= 0x10000 {
			units++
		}
		n++
	}
	return units + max(runeCol-n, 0)
}

func (b *Bridge) pos(path string, line, col int) map[string]any {
	return map[string]any{"line": max(line-1, 0), "character": utf16Col(b.lines.line(path, line), max(col-1, 0))}
}

// selectionPayload is the selection_changed params for s, plus a key that
// changes only when what Claude would see changes.
func (b *Bridge) selectionPayload(s presence.Snapshot) (map[string]any, string, bool) {
	if s.File == "" || s.Line <= 0 {
		return nil, "", false
	}
	var start, end map[string]any
	text, empty := "", true
	if s.Sel != nil {
		start = b.pos(s.File, s.Sel.StartLine, s.Sel.StartCol)
		end = b.pos(s.File, s.Sel.EndLine, s.Sel.EndCol)
		text, empty = s.SelText, false
	} else {
		start = b.pos(s.File, s.Line, s.Col)
		end = start
	}
	payload := map[string]any{
		"text": text, "filePath": s.File, "fileUrl": fileURL(s.File),
		"selection": map[string]any{"start": start, "end": end, "isEmpty": empty},
	}
	key, _ := json.Marshal(payload)
	return payload, string(key), true
}

func (b *Bridge) notifyLoop() {
	ch, stop := b.opts.Store.Subscribe()
	defer stop()
	var fire <-chan time.Time
	for {
		select {
		case <-b.ctx.Done():
			return
		case _, ok := <-ch:
			if !ok {
				return
			}
			fire = time.After(b.opts.Debounce)
		case <-b.pokeCh():
			fire = time.After(b.opts.Debounce)
		case <-fire:
			fire = nil
			b.sendSelection()
		}
	}
}

func (b *Bridge) sendSelection() {
	if b.Clients() == 0 {
		return
	}
	payload, key, ok := b.selectionPayload(b.opts.Store.Get())
	if !ok {
		return
	}
	b.mu.Lock()
	if key == b.lastSent {
		b.mu.Unlock()
		return
	}
	b.lastSent = key
	b.mu.Unlock()
	b.notify("selection_changed", payload)
}
