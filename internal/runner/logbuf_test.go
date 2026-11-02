package runner

import (
	"strings"
	"testing"
)

func appendText(b *logBuf, lines ...string) {
	for _, l := range lines {
		b.Append(chunk{text: l})
	}
}

func bufLines(b *logBuf) []string {
	out := make([]string, 0, b.Len())
	for i := b.First(); i < b.End(); i++ {
		out = append(out, b.Line(i))
	}
	return out
}

func TestLogBufPreservesContent(t *testing.T) {
	var b logBuf
	appendText(&b, "a", "b")
	if got := strings.Join(bufLines(&b), ","); got != "a,b" {
		t.Errorf("lines = %q, want a,b", got)
	}
}

func TestCarriageReturnReplacesLine(t *testing.T) {
	var b logBuf
	b.Append(chunk{text: "start"})
	b.Append(chunk{text: "10%", cr: true})
	b.Append(chunk{text: "55%", cr: true})
	b.Append(chunk{text: "100%"})
	b.Append(chunk{text: "next"})
	if got := strings.Join(bufLines(&b), ","); got != "start,100%,next" {
		t.Errorf("lines = %q, want start,100%%,next", got)
	}
}

func TestCarriageReturnThenNewlineKeepsLine(t *testing.T) {
	var b logBuf
	b.Append(chunk{text: "Downloading", cr: true})
	b.Append(chunk{text: ""})
	b.Append(chunk{text: "done"})
	if got := strings.Join(bufLines(&b), ","); got != "Downloading,done" {
		t.Errorf("lines = %q, want Downloading,done", got)
	}
}

func TestAppendReportsReplacement(t *testing.T) {
	var b logBuf
	if idx, replaced := b.Append(chunk{text: "a", cr: true}); idx != 0 || replaced {
		t.Errorf("first append = (%d, %v), want (0, false)", idx, replaced)
	}
	if idx, replaced := b.Append(chunk{text: "b"}); idx != 0 || !replaced {
		t.Errorf("overwrite = (%d, %v), want (0, true)", idx, replaced)
	}
}

func TestLogBufCapsBytes(t *testing.T) {
	var b logBuf
	line := strings.Repeat("x", 1024)
	for range 400 {
		b.Append(chunk{text: line})
	}
	if b.bytes > maxLogBytes {
		t.Errorf("bytes = %d, want <= %d", b.bytes, maxLogBytes)
	}
	if b.First() == 0 {
		t.Error("trimming should advance the first absolute index")
	}
	if b.Line(b.End()-1) != line {
		t.Error("trimming should keep the newest lines")
	}
}

func TestLogBufCapsLines(t *testing.T) {
	var b logBuf
	for range maxLogLines + 500 {
		b.Append(chunk{text: "l"})
	}
	if b.Len() > maxLogLines {
		t.Errorf("lines = %d, want <= %d", b.Len(), maxLogLines)
	}
	if b.End() != maxLogLines+500 {
		t.Errorf("End = %d, want absolute numbering to survive trims", b.End())
	}
}

func TestLogBufAppendIncremental(t *testing.T) {
	var b logBuf
	for range maxLogLines - 10 {
		b.Append(chunk{text: "warm"})
	}
	allocs := testing.AllocsPerRun(1000, func() { b.Append(chunk{text: "steady"}) })
	if allocs > 1 {
		t.Errorf("Append allocates %.1f times per line; it should not rebuild the log", allocs)
	}
}

func TestLogBufReset(t *testing.T) {
	var b logBuf
	appendText(&b, "hello")
	b.Reset()
	if b.Len() != 0 || b.bytes != 0 || b.First() != 0 {
		t.Errorf("reset buffer not empty: %d lines, %d bytes", b.Len(), b.bytes)
	}
}
