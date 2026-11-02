package runner

const (
	maxLogBytes = 256 * 1024
	maxLogLines = 5000
)

// logBuf is a capped line buffer addressed by absolute line numbers that stay
// stable when old lines are trimmed from the front.
type logBuf struct {
	lines   []string
	bytes   int
	dropped int
	open    bool
}

// Append adds a chunk, overwriting the last line after a bare carriage return,
// and returns the line's absolute index and whether it overwrote one.
func (b *logBuf) Append(c chunk) (int, bool) {
	if b.open && len(b.lines) > 0 {
		b.open = c.cr
		last := len(b.lines) - 1
		if c.text == "" {
			return b.dropped + last, false
		}
		b.bytes += len(c.text) - len(b.lines[last])
		b.lines[last] = c.text
		return b.dropped + last, true
	}
	b.open = c.cr
	b.lines = append(b.lines, c.text)
	b.bytes += len(c.text) + 1
	b.trim()
	return b.End() - 1, false
}

func (b *logBuf) trim() {
	if b.bytes <= maxLogBytes && len(b.lines) <= maxLogLines {
		return
	}
	drop := max(1, len(b.lines)/10)
	for _, l := range b.lines[:drop] {
		b.bytes -= len(l) + 1
	}
	b.lines = append(b.lines[:0], b.lines[drop:]...)
	b.dropped += drop
}

func (b *logBuf) Len() int   { return len(b.lines) }
func (b *logBuf) First() int { return b.dropped }
func (b *logBuf) End() int   { return b.dropped + len(b.lines) }

// Line returns the line at absolute index i, or "" when it is out of range.
func (b *logBuf) Line(i int) string {
	i -= b.dropped
	if i < 0 || i >= len(b.lines) {
		return ""
	}
	return b.lines[i]
}

func (b *logBuf) Reset() { *b = logBuf{} }
