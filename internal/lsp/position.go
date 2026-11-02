package lsp

import (
	"unicode/utf16"
	"unicode/utf8"
)

type Encoding string

const (
	UTF8  Encoding = "utf-8"
	UTF16 Encoding = "utf-16"
	UTF32 Encoding = "utf-32"
)

func negotiatedEncoding(s string) Encoding {
	switch e := Encoding(s); e {
	case UTF8, UTF32:
		return e
	}
	return UTF16
}

func unitsOf(r rune, size int, enc Encoding) int {
	switch enc {
	case UTF8:
		return size
	case UTF32:
		return 1
	}
	if n := utf16.RuneLen(r); n > 0 {
		return n
	}
	return 1
}

// RuneToUnits converts a rune column to the server's column units, clamping to the line.
func RuneToUnits(line string, runeCol int, enc Encoding) int {
	units := 0
	for i, n := 0, 0; i < len(line) && n < runeCol; n++ {
		r, size := utf8.DecodeRuneInString(line[i:])
		units += unitsOf(r, size, enc)
		i += size
	}
	return units
}

// UnitsToRune converts server column units to a rune column; a unit inside a rune maps to that rune.
func UnitsToRune(line string, units int, enc Encoding) int {
	runes, acc := 0, 0
	for i := 0; i < len(line); {
		r, size := utf8.DecodeRuneInString(line[i:])
		acc += unitsOf(r, size, enc)
		if acc > units {
			return runes
		}
		runes++
		i += size
	}
	return runes
}

func ByteToUnits(line string, byteCol int, enc Encoding) int {
	if byteCol > len(line) {
		byteCol = len(line)
	}
	return RuneToUnits(line, utf8.RuneCountInString(line[:max(byteCol, 0)]), enc)
}

func UnitsToByte(line string, units int, enc Encoding) int {
	runes := UnitsToRune(line, units, enc)
	i := 0
	for n := 0; n < runes && i < len(line); n++ {
		_, size := utf8.DecodeRuneInString(line[i:])
		i += size
	}
	return i
}
