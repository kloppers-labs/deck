package viewer

import (
	"regexp"
	"unicode"
)

const maxMatches = 10000

// Match is a search hit on Plain line Line, spanning display columns
// [Start, End).
type Match struct{ Line, Start, End int }

func (m Match) pos() Pos { return Pos{m.Line, m.Start} }

func compileQuery(query string, regex bool) (*regexp.Regexp, error) {
	expr := query
	if !regex {
		expr = regexp.QuoteMeta(query)
	}
	if !hasUpper(query) {
		expr = "(?i)" + expr
	}
	return regexp.Compile(expr)
}

func hasUpper(s string) bool {
	for _, r := range s {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

// Find searches tab-expanded lines. It is smart-case: a query without capitals
// ignores case. Without regex the query is literal.
func Find(lines []string, query string, regex bool) ([]Match, error) {
	if query == "" {
		return nil, nil
	}
	re, err := compileQuery(query, regex)
	if err != nil {
		return nil, err
	}
	var out []Match
	for i, line := range lines {
		for _, loc := range re.FindAllStringIndex(line, -1) {
			if loc[0] == loc[1] {
				continue
			}
			start := strWidth(line[:loc[0]])
			out = append(out, Match{Line: i, Start: start, End: start + strWidth(line[loc[0]:loc[1]])})
			if len(out) >= maxMatches {
				return out, nil
			}
		}
	}
	return out, nil
}

func strWidth(s string) int {
	w := 0
	for _, r := range s {
		w += runeWidth(r)
	}
	return w
}

// NextMatch is the index of the first match after at (display columns),
// wrapping to the first; -1 when there are none.
func NextMatch(ms []Match, at Pos) int {
	if len(ms) == 0 {
		return -1
	}
	for i, m := range ms {
		if at.before(m.pos()) {
			return i
		}
	}
	return 0
}

// PrevMatch is the index of the last match before at, wrapping to the last.
func PrevMatch(ms []Match, at Pos) int {
	if len(ms) == 0 {
		return -1
	}
	for i := len(ms) - 1; i >= 0; i-- {
		if ms[i].pos().before(at) {
			return i
		}
	}
	return len(ms) - 1
}
