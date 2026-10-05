package runner

import "strings"

// Caps for a job's retained output. Long-running dev servers stream
// indefinitely, so the buffer keeps a recent window rather than growing
// without bound.
const (
	maxLogBytes = 256 * 1024
	maxLogLines = 5000
)

// logBuf is a chunk-oriented capped buffer. The runner's scanner appends whole
// lines, so trimming never cuts an ANSI escape mid-sequence; String prepends a
// reset once trimmed so a dropped line's unclosed colour can't bleed into what
// remains.
type logBuf struct {
	lines   []string
	bytes   int
	trimmed bool
}

func (b *logBuf) Append(chunk string) {
	b.lines = append(b.lines, chunk)
	b.bytes += len(chunk)
	if b.bytes <= maxLogBytes && len(b.lines) <= maxLogLines {
		return
	}
	// Drop the oldest ~10% in one copy so trims stay amortized.
	drop := max(1, len(b.lines)/10)
	for _, l := range b.lines[:drop] {
		b.bytes -= len(l)
	}
	b.lines = append(b.lines[:0], b.lines[drop:]...)
	b.trimmed = true
}

func (b *logBuf) String() string {
	s := strings.Join(b.lines, "")
	if b.trimmed {
		return "\x1b[0m" + s
	}
	return s
}

func (b *logBuf) Reset() {
	b.lines = nil
	b.bytes = 0
	b.trimmed = false
}
