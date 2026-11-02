package viewer

import "unicode"

func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// WordSpan is the rune range [start, end) of the identifier under col, or ok
// false when col is not on one. A C# verbatim "@" prefix belongs to the word.
func WordSpan(line string, col int) (start, end int, ok bool) {
	rs := []rune(line)
	if col < 0 || col >= len(rs) {
		return 0, 0, false
	}
	if rs[col] == '@' && col+1 < len(rs) && isWordRune(rs[col+1]) && (col == 0 || !isWordRune(rs[col-1])) {
		col++
	}
	if !isWordRune(rs[col]) {
		return 0, 0, false
	}
	start, end = col, col
	for start > 0 && isWordRune(rs[start-1]) {
		start--
	}
	for end < len(rs) && isWordRune(rs[end]) {
		end++
	}
	if start > 0 && rs[start-1] == '@' && (start == 1 || !isWordRune(rs[start-2])) {
		start--
	}
	return start, end, true
}

// WordAt is the identifier under rune column col, or "".
func WordAt(line string, col int) string {
	start, end, ok := WordSpan(line, col)
	if !ok {
		return ""
	}
	return string([]rune(line)[start:end])
}

type runeClass int

const (
	classSpace runeClass = iota
	classWord
	classPunct
)

func classOf(r rune) runeClass {
	switch {
	case unicode.IsSpace(r):
		return classSpace
	case isWordRune(r):
		return classWord
	}
	return classPunct
}

func nextWordStart(line string, col int) int {
	rs := []rune(line)
	if col >= len(rs) {
		return len(rs)
	}
	c := classOf(rs[col])
	i := col
	for i < len(rs) && classOf(rs[i]) == c && c != classSpace {
		i++
	}
	for i < len(rs) && classOf(rs[i]) == classSpace {
		i++
	}
	return i
}

func prevWordStart(line string, col int) int {
	rs := []rune(line)
	i := min(col, len(rs)) - 1
	for i >= 0 && classOf(rs[i]) == classSpace {
		i--
	}
	if i < 0 {
		return 0
	}
	c := classOf(rs[i])
	for i > 0 && classOf(rs[i-1]) == c {
		i--
	}
	return i
}
