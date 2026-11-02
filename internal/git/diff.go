package git

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	tea "github.com/charmbracelet/bubbletea"

	"deck/internal/ui"
)

const diffCap = 200 * 1024

// diffKey identifies a cached diff: it is stale once the index or the
// work-tree file changes.
type diffKey struct {
	path  string
	sec   Section
	index int64
	file  int64
}

// capDiff cuts a diff at a line boundary near limit bytes and says how much is left.
func capDiff(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	cut := strings.LastIndexByte(text[:limit], '\n') + 1
	rest := text[cut:]
	n := strings.Count(rest, "\n")
	if !strings.HasSuffix(rest, "\n") {
		n++
	}
	return text[:cut] + fmt.Sprintf("… %d more lines · e open", n)
}

// firstHunkLine is the new-side line of the first change in the first hunk.
func firstHunkLine(diff string) int {
	line, inHunk := 0, false
	for l := range strings.SplitSeq(diff, "\n") {
		if strings.HasPrefix(l, "@@") {
			if inHunk {
				break
			}
			line, inHunk = hunkStart(l), true
			continue
		}
		if !inHunk {
			continue
		}
		switch {
		case strings.HasPrefix(l, "+"), strings.HasPrefix(l, "-"):
			return max(line, 1)
		case strings.HasPrefix(l, " "):
			line++
		default:
			return max(line, 1)
		}
	}
	return max(line, 1)
}

func hunkStart(header string) int {
	for _, f := range strings.Fields(header) {
		if rest, ok := strings.CutPrefix(f, "+"); ok {
			n, _, _ := strings.Cut(rest, ",")
			v, _ := strconv.Atoi(n)
			return v
		}
	}
	return 0
}

func highlightDiff(theme, diff string) string {
	lexer := lexers.Get("diff")
	if lexer == nil {
		return diff
	}
	it, err := lexer.Tokenise(nil, diff)
	if err != nil {
		return diff
	}
	style := styles.Get(theme)
	if style == nil {
		style = styles.Fallback
	}
	var buf strings.Builder
	if err := formatters.Get("terminal256").Format(&buf, style, it); err != nil {
		return diff
	}
	return buf.String()
}

func fileStamps(r *Repo, entries []Entry) map[string]int64 {
	stamps := map[string]int64{}
	for _, e := range entries {
		if !e.worktreeOn() {
			continue
		}
		if fi, err := os.Lstat(r.Abs(e.Path)); err == nil {
			stamps[e.Path] = fi.ModTime().UnixNano() ^ fi.Size()
		}
	}
	return stamps
}

func (p Pane) keyFor(it Item) diffKey {
	k := diffKey{path: it.Entry.Path, sec: it.Section, index: p.indexStamp}
	if it.Section != SectionStaged {
		k.file = p.stamps[it.Entry.Path]
	}
	return k
}

func (p Pane) requestDiff() (Pane, tea.Cmd) {
	p.diffSeq++
	return p.fetchDiff()
}

func (p Pane) scheduleDiff() (Pane, tea.Cmd) {
	p.diffSeq++
	seq := p.diffSeq
	return p, tea.Tick(diffDebounce, func(time.Time) tea.Msg { return diffDueMsg{seq: seq} })
}

func (p Pane) fetchDiff() (Pane, tea.Cmd) {
	it, ok := p.selected()
	if !ok || p.repo == nil {
		p.setDiff(diffKey{}, cachedDiff{})
		return p, nil
	}
	k := p.keyFor(it)
	if d, ok := p.cache[k]; ok {
		p.setDiff(k, d)
		return p, nil
	}
	seq, repo, theme := p.diffSeq, p.repo, p.opts.Theme
	return p, func() tea.Msg {
		text, err := repo.Diff(context.Background(), it)
		if err != nil {
			return DiffLoadedMsg{Seq: seq, Key: k, Err: err}
		}
		return DiffLoadedMsg{Seq: seq, Key: k, Text: text, Rendered: renderDiff(theme, text)}
	}
}

func renderDiff(theme, text string) string {
	if strings.TrimSpace(text) == "" {
		return ui.Muted.Render("(no textual diff: binary, empty or mode change)")
	}
	return highlightDiff(theme, capDiff(text, diffCap))
}

func (p Pane) diffLoaded(msg DiffLoadedMsg) Pane {
	if msg.Err != nil {
		if msg.Seq == p.diffSeq {
			p.rawDiff, p.shownKey = "", diffKey{}
			p.diff.SetContent(ui.Err.Render(msg.Err.Error()))
		}
		return p
	}
	d := cachedDiff{raw: msg.Text, rendered: msg.Rendered}
	if d.rendered == "" {
		d.rendered = renderDiff(p.opts.Theme, d.raw)
	}
	if len(p.cache) >= maxCachedDiff {
		clear(p.cache)
	}
	p.cache[msg.Key] = d
	if msg.Seq == p.diffSeq {
		p.setDiff(msg.Key, d)
	}
	return p
}

func (p *Pane) setDiff(k diffKey, d cachedDiff) {
	p.shownKey, p.rawDiff = k, d.raw
	p.diff.SetContent(d.rendered)
	p.diff.GotoTop()
}
