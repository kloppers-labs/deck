package runner

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"deck/internal/ui"
)

const maxProblems = 1000

var (
	matchStyle    = lipgloss.NewStyle().Foreground(ui.ColorBg).Background(ui.ColorWarn)
	curMatchStyle = lipgloss.NewStyle().Foreground(ui.ColorBg).Background(ui.ColorAccent)
	problemStyle  = lipgloss.NewStyle().Background(ui.ColorDim).Bold(true)
)

type located struct {
	abs int
	Problem
}

// logView is one task's output with its scroll position, search matches and
// recognised problems, all keyed by absolute line number.
type logView struct {
	buf      logBuf
	dir      string
	top      int
	follow   bool
	unseen   int
	query    string
	matches  []int
	match    int
	problems []located
	problem  int
}

func newLogView(dir string) logView {
	return logView{dir: dir, follow: true, match: -1, problem: -1}
}

func (v *logView) reset() {
	*v = logView{dir: v.dir, follow: true, query: v.query, match: -1, problem: -1}
}

func (v *logView) append(cs []chunk) {
	for _, c := range cs {
		end := v.buf.End()
		idx, replaced := v.buf.Append(c)
		switch {
		case replaced:
			v.forget(idx)
			v.index(idx)
		case v.buf.End() > end:
			v.index(idx)
			if !v.follow {
				v.unseen++
			}
		}
	}
	v.prune()
}

func (v *logView) index(abs int) {
	text := ansi.Strip(v.buf.Line(abs))
	if v.query != "" && containsQuery(text, v.query) {
		v.matches = append(v.matches, abs)
	}
	if p, ok := matchProblem(text); ok && len(v.problems) < maxProblems {
		if !filepath.IsAbs(p.Path) && v.dir != "" {
			p.Path = filepath.Join(v.dir, p.Path)
		}
		v.problems = append(v.problems, located{abs: abs, Problem: p})
	}
}

func (v *logView) forget(abs int) {
	if n := len(v.matches); n > 0 && v.matches[n-1] == abs {
		v.matches = v.matches[:n-1]
		v.match = min(v.match, len(v.matches)-1)
	}
	if n := len(v.problems); n > 0 && v.problems[n-1].abs == abs {
		v.problems = v.problems[:n-1]
		v.problem = min(v.problem, len(v.problems)-1)
	}
}

func (v *logView) prune() {
	first := v.buf.First()
	if n := countBelow(v.matches, func(a int) bool { return a < first }); n > 0 {
		v.matches = v.matches[n:]
		v.match = max(v.match-n, -1)
	}
	if n := countBelow(v.problems, func(p located) bool { return p.abs < first }); n > 0 {
		v.problems = v.problems[n:]
		v.problem = max(v.problem-n, -1)
	}
	v.top = max(v.top, first)
}

func countBelow[T any](s []T, below func(T) bool) int {
	n := 0
	for n < len(s) && below(s[n]) {
		n++
	}
	return n
}

// containsQuery is a smart-case substring test: an all-lowercase query
// ignores case.
func containsQuery(text, q string) bool {
	if strings.ToLower(q) == q {
		text = strings.ToLower(text)
	}
	return strings.Contains(text, q)
}

func (v *logView) setQuery(q string) {
	v.query = q
	v.matches = v.matches[:0]
	v.match = -1
	if q == "" {
		return
	}
	for i := v.buf.First(); i < v.buf.End(); i++ {
		if containsQuery(ansi.Strip(v.buf.Line(i)), q) {
			v.matches = append(v.matches, i)
		}
	}
}

func rowsFor(line string, w int, wrap bool) int {
	if !wrap || w <= 0 {
		return 1
	}
	return max(1, (ansi.StringWidth(expandTabs(line))+w-1)/w)
}

func (v *logView) bottomTop(w, h int, wrap bool) int {
	if !wrap {
		return max(v.buf.End()-h, v.buf.First())
	}
	rows := 0
	i := v.buf.End()
	for i > v.buf.First() {
		rows += rowsFor(v.buf.Line(i-1), w, wrap)
		if rows > h {
			break
		}
		i--
	}
	return i
}

func (v *logView) start(w, h int, wrap bool) int {
	if v.follow {
		return v.bottomTop(w, h, wrap)
	}
	return min(max(v.top, v.buf.First()), v.bottomTop(w, h, wrap))
}

func (v *logView) scroll(delta, w, h int, wrap bool) {
	bottom := v.bottomTop(w, h, wrap)
	v.top = min(max(v.start(w, h, wrap)+delta, v.buf.First()), bottom)
	v.follow = v.top >= bottom
	if v.follow {
		v.unseen = 0
	}
}

func (v *logView) toTop() {
	v.top = v.buf.First()
	v.follow = false
}

func (v *logView) toBottom() {
	v.follow = true
	v.unseen = 0
}

func (v *logView) reveal(abs, w, h int, wrap bool) {
	s := v.start(w, h, wrap)
	if abs >= s && abs < s+h && !wrap {
		return
	}
	v.top = max(abs-h/3, v.buf.First())
	bottom := v.bottomTop(w, h, wrap)
	if v.top >= bottom {
		v.top = bottom
		v.follow = true
		v.unseen = 0
		return
	}
	v.follow = false
}

func (v *logView) nextMatch(dir, w, h int, wrap bool) {
	if len(v.matches) == 0 {
		return
	}
	v.match = step(v.match, dir, len(v.matches), func(i int) int { return v.matches[i] }, v.start(w, h, wrap))
	v.reveal(v.matches[v.match], w, h, wrap)
}

func (v *logView) nextProblem(dir, w, h int, wrap bool) {
	if len(v.problems) == 0 {
		return
	}
	v.problem = step(v.problem, dir, len(v.problems), func(i int) int { return v.problems[i].abs }, v.start(w, h, wrap))
	v.reveal(v.problems[v.problem].abs, w, h, wrap)
}

// step moves cur by dir with wrap-around; with no current item it picks the
// first item at or after from (or the last before it, going backwards).
func step(cur, dir, n int, at func(int) int, from int) int {
	if cur >= 0 {
		return (cur + dir + n) % n
	}
	if dir > 0 {
		for i := range n {
			if at(i) >= from {
				return i
			}
		}
		return 0
	}
	for i := n - 1; i >= 0; i-- {
		if at(i) < from {
			return i
		}
	}
	return n - 1
}

func (v *logView) selectedProblem() (located, bool) {
	if v.problem < 0 || v.problem >= len(v.problems) {
		return located{}, false
	}
	return v.problems[v.problem], true
}

func (v *logView) selectProblemAt(abs int) bool {
	for i, p := range v.problems {
		if p.abs == abs {
			v.problem = i
			return true
		}
	}
	return false
}

// render returns exactly h rows of at most w columns and the absolute line
// each row shows (-1 for blank rows).
func (v *logView) render(w, h int, wrap bool) ([]string, []int) {
	rows := make([]string, 0, h)
	abs := make([]int, 0, h)
	cur := -1
	if v.match >= 0 && v.match < len(v.matches) {
		cur = v.matches[v.match]
	}
	sel := -1
	if p, ok := v.selectedProblem(); ok {
		sel = p.abs
	}
	for i := v.start(w, h, wrap); i < v.buf.End() && len(rows) < h; i++ {
		line := v.decorate(v.buf.Line(i), i == cur, i == sel)
		parts := []string{ansi.Truncate(line, w, "")}
		if wrap {
			parts = strings.Split(ansi.Hardwrap(line, w, true), "\n")
		}
		for _, part := range parts {
			if len(rows) == h {
				break
			}
			rows = append(rows, part+resetIfStyled(part))
			abs = append(abs, i)
		}
	}
	for len(rows) < h {
		rows = append(rows, "")
		abs = append(abs, -1)
	}
	return rows, abs
}

func (v *logView) decorate(line string, current, selected bool) string {
	line = expandTabs(line)
	if selected {
		return problemStyle.Render(ansi.Strip(line))
	}
	if v.query == "" || !containsQuery(ansi.Strip(line), v.query) {
		return line
	}
	style := matchStyle
	if current {
		style = curMatchStyle
	}
	return highlight(ansi.Strip(line), v.query, style)
}

func highlight(text, q string, style lipgloss.Style) string {
	hay := text
	if strings.ToLower(q) == q {
		hay = strings.ToLower(text)
	}
	var b strings.Builder
	for {
		i := strings.Index(hay, q)
		if i < 0 || q == "" {
			b.WriteString(text)
			return b.String()
		}
		b.WriteString(text[:i])
		b.WriteString(style.Render(text[i : i+len(q)]))
		text, hay = text[i+len(q):], hay[i+len(q):]
	}
}

func expandTabs(s string) string { return strings.ReplaceAll(s, "\t", "    ") }

func resetIfStyled(s string) string {
	if strings.Contains(s, "\x1b") {
		return "\x1b[0m"
	}
	return ""
}

// status summarises search, follow and problem state for the log footer.
func (v *logView) status() (left, right string) {
	switch {
	case v.query != "" && len(v.matches) == 0:
		left = "/" + v.query + " no matches"
	case v.query != "":
		left = fmt.Sprintf("/%s %d/%d", v.query, v.match+1, len(v.matches))
	case v.unseen > 0:
		left = fmt.Sprintf("↓ %d new · F follow", v.unseen)
	case !v.follow:
		left = "paused · F follow"
	}
	errs, warns := v.problemCounts()
	var parts []string
	if errs > 0 {
		parts = append(parts, plural(errs, "error"))
	}
	if warns > 0 {
		parts = append(parts, plural(warns, "warning"))
	}
	if len(parts) > 0 {
		right = strings.Join(parts, " ") + " · e next"
	}
	return left, right
}

func (v *logView) problemCounts() (errs, warns int) {
	seen := map[string]bool{}
	for _, p := range v.problems {
		if seen[p.key()] {
			continue
		}
		seen[p.key()] = true
		if p.Severity == "warning" {
			warns++
		} else {
			errs++
		}
	}
	return errs, warns
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}
