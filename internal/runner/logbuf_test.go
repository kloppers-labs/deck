package runner

import (
	"strings"
	"testing"
)

func TestLogBufPreservesContent(t *testing.T) {
	var b logBuf
	b.Append("a\n")
	b.Append("b\n")
	if b.String() != "a\nb\n" {
		t.Errorf("String = %q, want lines joined unchanged", b.String())
	}
}

func TestLogBufCapsBytes(t *testing.T) {
	var b logBuf
	line := strings.Repeat("x", 1024) + "\n"
	for range 400 { // ~400 KiB, well past the 256 KiB cap
		b.Append(line)
	}
	if b.bytes > maxLogBytes {
		t.Errorf("bytes = %d, want <= %d", b.bytes, maxLogBytes)
	}
	if !strings.HasPrefix(b.String(), "\x1b[0m") {
		t.Error("trimmed buffer should start with an ANSI reset")
	}
	if !strings.HasSuffix(b.String(), line) {
		t.Error("trimming should drop oldest lines, keeping the newest")
	}
}

func TestLogBufCapsLines(t *testing.T) {
	var b logBuf
	for range maxLogLines + 500 {
		b.Append("l\n")
	}
	if len(b.lines) > maxLogLines {
		t.Errorf("lines = %d, want <= %d", len(b.lines), maxLogLines)
	}
}

func TestLogBufReset(t *testing.T) {
	var b logBuf
	b.Append("hello\n")
	b.Reset()
	if b.String() != "" || b.bytes != 0 {
		t.Errorf("reset buffer not empty: %q (%d bytes)", b.String(), b.bytes)
	}
}
