package viewer

import (
	"strings"
	"unicode/utf8"
)

const sgrReset = "\x1b[0m"

// Span paints display columns [Start, End) of a rendered row with SGR.
type Span struct {
	Start, End int
	SGR        string
}

func spanAt(spans []Span, col int) int {
	for i := len(spans) - 1; i >= 0; i-- {
		if col >= spans[i].Start && col < spans[i].End {
			return i
		}
	}
	return -1
}

// Paint overlays spans on a styled row without changing its visible text. Later
// spans win where they overlap, and the row's own styling resumes after each.
func Paint(styled string, spans []Span) string {
	if len(spans) == 0 {
		return styled
	}
	var b strings.Builder
	b.Grow(len(styled) + 16*len(spans))
	cur := ""
	active := -1
	col := 0
	for i := 0; i < len(styled); {
		if styled[i] == 0x1b {
			n := escapeLen(styled[i:])
			seq := styled[i : i+n]
			b.WriteString(seq)
			if strings.HasSuffix(seq, "m") {
				if seq == sgrReset || seq == "\x1b[m" {
					cur = ""
				} else {
					cur += seq
				}
				if active >= 0 {
					b.WriteString(spans[active].SGR)
				}
			}
			i += n
			continue
		}
		r, size := utf8.DecodeRuneInString(styled[i:])
		if next := spanAt(spans, col); next != active {
			if active >= 0 {
				b.WriteString(sgrReset + cur)
			}
			if next >= 0 {
				b.WriteString(spans[next].SGR)
			}
			active = next
		}
		b.WriteString(styled[i : i+size])
		col += runeWidth(r)
		i += size
	}
	if active >= 0 {
		b.WriteString(sgrReset + cur)
	}
	return b.String()
}

func escapeLen(s string) int {
	if len(s) >= 2 && s[1] == ']' {
		for j := 2; j < len(s); j++ {
			if s[j] == 0x07 {
				return j + 1
			}
			if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
				return j + 2
			}
		}
		return len(s)
	}
	if len(s) < 2 || s[1] != '[' {
		return min(2, len(s))
	}
	for j := 2; j < len(s); j++ {
		if s[j] >= 0x40 && s[j] <= 0x7e {
			return j + 1
		}
	}
	return len(s)
}
