package lsp

import "testing"

func TestRuneToUnits(t *testing.T) {
	const line = "a😀b中c"
	for _, tc := range []struct {
		name string
		enc  Encoding
		rune int
		want int
	}{
		{"utf16 before emoji", UTF16, 1, 1},
		{"utf16 after surrogate pair", UTF16, 2, 3},
		{"utf16 after cjk", UTF16, 4, 5},
		{"utf16 past end clamps", UTF16, 99, 6},
		{"utf16 negative clamps", UTF16, -3, 0},
		{"utf8 after emoji", UTF8, 2, 5},
		{"utf8 after cjk", UTF8, 4, 9},
		{"utf8 past end clamps", UTF8, 99, 10},
		{"utf32 is runes", UTF32, 4, 4},
		{"unknown encoding is utf16", "", 2, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := RuneToUnits(line, tc.rune, tc.enc); got != tc.want {
				t.Fatalf("RuneToUnits(%d, %s) = %d, want %d", tc.rune, tc.enc, got, tc.want)
			}
		})
	}
}

func TestUnitsToRune(t *testing.T) {
	const line = "a😀b中c"
	for _, tc := range []struct {
		name  string
		enc   Encoding
		units int
		want  int
	}{
		{"utf16 start", UTF16, 0, 0},
		{"utf16 inside surrogate pair snaps to the emoji", UTF16, 2, 1},
		{"utf16 after pair", UTF16, 3, 2},
		{"utf16 after cjk", UTF16, 5, 4},
		{"utf16 past end clamps", UTF16, 50, 5},
		{"utf8 inside multibyte snaps back", UTF8, 3, 1},
		{"utf8 after cjk", UTF8, 9, 4},
		{"utf8 past end clamps", UTF8, 50, 5},
		{"utf32", UTF32, 3, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := UnitsToRune(line, tc.units, tc.enc); got != tc.want {
				t.Fatalf("UnitsToRune(%d, %s) = %d, want %d", tc.units, tc.enc, got, tc.want)
			}
		})
	}
}

func TestByteUnitsRoundTrip(t *testing.T) {
	const line = "x := \"héllo 😀 世界\""
	for _, enc := range []Encoding{UTF8, UTF16, UTF32} {
		for b := range len(line) + 1 {
			if b < len(line) && !isRuneStart(line[b]) {
				continue
			}
			u := ByteToUnits(line, b, enc)
			if got := UnitsToByte(line, u, enc); got != b {
				t.Fatalf("%s: byte %d -> %d units -> byte %d", enc, b, u, got)
			}
		}
	}
	if got := ByteToUnits(line, 999, UTF16); got != RuneToUnits(line, 999, UTF16) {
		t.Fatalf("past-end byte clamp = %d", got)
	}
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

func TestNegotiateEncoding(t *testing.T) {
	for raw, want := range map[string]Encoding{"": UTF16, "utf-8": UTF8, "utf-16": UTF16, "utf-32": UTF32, "weird": UTF16} {
		if got := negotiatedEncoding(raw); got != want {
			t.Fatalf("negotiatedEncoding(%q) = %q, want %q", raw, got, want)
		}
	}
}
