package ui

import (
	"bytes"
	"strings"
)

// IsBinary reports whether data looks like a binary file rather than text, so
// callers can show a placeholder instead of feeding raw bytes to a syntax
// highlighter (which passes control bytes through verbatim and corrupts the
// terminal). It uses git's heuristic — a NUL byte in the first 8 KB means
// binary — plus a fallback for binary without NULs: a high ratio of C0 control
// bytes that aren't ordinary whitespace.
func IsBinary(data []byte) bool {
	const sniff = 8000
	if len(data) > sniff {
		data = data[:sniff]
	}
	if len(data) == 0 {
		return false
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return true
	}
	control := 0
	for _, b := range data {
		switch b {
		case '\n', '\t', '\r', '\f', '\v':
			// ordinary whitespace, not a binary signal
		default:
			if b < 0x20 || b == 0x7f {
				control++
			}
		}
	}
	return control*100/len(data) > 30
}

// SanitizeTerminal makes s safe to embed in a Bubble Tea View. A View string is
// meant to be printable text plus SGR colour styling — Bubble Tea's renderer
// owns cursor positioning — so anything that can move the cursor, clear the
// screen, ring the bell, or otherwise escape its line is a corruption risk.
//
// It keeps newlines, tabs, and SGR colour sequences (ESC[…m) and drops
// everything else in the control range: bare carriage returns, NUL and other C0
// controls, DEL, and every non-SGR escape sequence (cursor movement, erase, OSC,
// charset selection, …). This is deck's last line of defence against binary or
// terminal-control content — e.g. previewing a .DS_Store plist or streaming a
// command that paints with escape codes — scribbling over the tab bar.
//
// Visible characters and SGR codes have their bytes preserved verbatim, so line
// count and display width are unchanged; only zero-width control bytes are
// removed.
func SanitizeTerminal(s string) string {
	if !needsSanitize(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == esc:
			n, keep := scanEscape(s[i:])
			if keep {
				b.WriteString(s[i : i+n])
			}
			i += n
		case c == '\n' || c == '\t':
			b.WriteByte(c)
			i++
		case c < 0x20 || c == 0x7f:
			// Drop bare control byte (CR, NUL, BEL, backspace, …).
			i++
		default:
			// Printable ASCII or a UTF-8 multi-byte rune's bytes; pass through.
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

const esc = 0x1b

// needsSanitize is a cheap pre-scan: if s has no control bytes at all it is
// already safe and SanitizeTerminal can return it untouched. ESC counts as a
// control byte because a sequence still has to be classified.
func needsSanitize(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < 0x20 && c != '\n' && c != '\t') || c == 0x7f {
			return true
		}
	}
	return false
}

// scanEscape measures the escape sequence beginning at s[0] (which must be ESC)
// and reports its byte length and whether it is an SGR colour sequence worth
// keeping. Only CSI sequences ending in 'm' are kept; every other escape —
// cursor movement, erase, OSC, two-byte charset escapes, a lone trailing ESC —
// is consumed and discarded.
func scanEscape(s string) (length int, keep bool) {
	if len(s) < 2 {
		return 1, false // lone ESC at end of string
	}
	switch s[1] {
	case '[': // CSI: ESC [ params/intermediates final
		i := 2
		for i < len(s) && s[i] >= 0x20 && s[i] <= 0x3f {
			i++
		}
		if i >= len(s) {
			return i, false // truncated, no final byte
		}
		final := s[i]
		i++
		return i, final == 'm'
	case ']', 'P', 'X', '^', '_': // string escapes (OSC/DCS/SOS/PM/APC): ESC <id> … ST
		i := 2
		for i < len(s) {
			if s[i] == 0x07 { // BEL terminates OSC
				return i + 1, false
			}
			if s[i] == esc && i+1 < len(s) && s[i+1] == '\\' { // ST (ESC \)
				return i + 2, false
			}
			i++
		}
		return i, false // unterminated
	default:
		// nF/Fp/Fe/Fs escape: optional intermediate bytes (0x20-0x2f) then one
		// final byte. Covers charset selection (ESC ( 0) and ESC = / ESC > etc.
		i := 1
		for i < len(s) && s[i] >= 0x20 && s[i] <= 0x2f {
			i++
		}
		if i < len(s) {
			i++ // consume the final byte
		}
		return i, false
	}
}
