package git

import (
	"cmp"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kloppers-labs/deck/internal/editor"
	"github.com/kloppers-labs/deck/internal/ui"
)

type reviewRowKind int

const (
	rowCommits reviewRowKind = iota
	rowCommit
	rowFile
)

type reviewRow struct {
	kind reviewRowKind
	idx  int
}

type reviewState struct {
	rv          Review
	loaded      bool
	seq         int
	base        string
	uncommitted bool
	viewed      Viewed
	hashes      map[string]string
	rows        []reviewRow
	cursor      int
	offset      int
	expanded    bool
	pendingPath string
	pendingLine int
	diffSeq     int
	err         string
}

type reviewLoadedMsg struct {
	seq    int
	repo   *Repo
	rv     Review
	viewed Viewed
	hashes map[string]string
	err    error
}

type reviewDiffDueMsg struct{ seq int }

type reviewDiffMsg struct {
	seq  int
	path string
	text string
	err  error
}

type viewedSavedMsg struct{ err error }

func (reviewLoadedMsg) RouteTo() ui.Dest  { return ui.DestGit }
func (reviewDiffDueMsg) RouteTo() ui.Dest { return ui.DestGit }
func (reviewDiffMsg) RouteTo() ui.Dest    { return ui.DestGit }
func (viewedSavedMsg) RouteTo() ui.Dest   { return ui.DestGit }

func (p Pane) openReview(msg ReviewMsg) (Pane, tea.Cmd) {
	p.mode, p.focus = modeReview, focusList
	p.review.base, p.review.uncommitted = msg.Base, msg.Uncommitted
	p.review.pendingPath, p.review.pendingLine = msg.Path, msg.Line
	p.setDiffContent(ui.Muted.Render("loading review…"))
	return p.loadReview()
}

func (p Pane) loadReview() (Pane, tea.Cmd) {
	was := p.spinning()
	p.review.seq++
	p.loading = true
	seq, repo, dir := p.review.seq, p.repo, p.opts.Dir
	explicit, configured, uncommitted := p.review.base, p.opts.ReviewBase, p.review.uncommitted
	return p, p.withSpin(was, func() tea.Msg {
		ctx := context.Background()
		if repo == nil {
			r, err := OpenRepo(ctx, dir)
			if err != nil {
				return reviewLoadedMsg{seq: seq, err: err}
			}
			repo = r
		}
		base, err := repo.ResolveBase(ctx, explicit, configured)
		if err != nil {
			return reviewLoadedMsg{seq: seq, repo: repo, err: err}
		}
		rv, err := repo.LoadReview(ctx, base, uncommitted)
		if err != nil {
			return reviewLoadedMsg{seq: seq, repo: repo, err: err}
		}
		hashes := make(map[string]string, len(rv.Files))
		for _, f := range rv.Files {
			hashes[f.Path] = FileHash(repo.Top, f.Path)
		}
		return reviewLoadedMsg{seq: seq, repo: repo, rv: rv, viewed: LoadViewed(repo.GitDir).ForBase(rv.Base), hashes: hashes}
	})
}

func (p Pane) reviewLoaded(msg reviewLoadedMsg) (Pane, tea.Cmd) {
	if msg.seq != p.review.seq {
		return p, nil
	}
	p.loading = false
	if msg.repo != nil {
		p.repo = msg.repo
	}
	if msg.err != nil {
		p.review.err = msg.err.Error()
		p.setStatus("review: "+msg.err.Error(), true)
		return p, nil
	}
	prev, _ := p.reviewFile()
	p.review.err = ""
	p.review.rv, p.review.viewed, p.review.hashes, p.review.loaded = msg.rv, msg.viewed, msg.hashes, true
	p.layoutReview()
	switch {
	case p.review.pendingPath != "":
		rel := p.relToRepo(p.review.pendingPath)
		p.review.pendingPath = ""
		if p.selectReviewPath(rel) {
			p.focus = focusDiff
		} else {
			p.review.pendingLine, p.focus = 0, focusList
			p.setStatus(rel+" has no changes vs "+cmp.Or(p.review.rv.Base, "HEAD"), false)
		}
	case prev.Path != "":
		p.selectReviewPath(prev.Path)
	default:
		p.review.cursor = p.firstFileRow()
	}
	return p.requestReviewDiff()
}

func (p Pane) relToRepo(path string) string {
	if p.repo == nil || !filepath.IsAbs(path) {
		return filepath.ToSlash(path)
	}
	top, _ := filepath.EvalSymlinks(p.repo.Top)
	abs := path
	if dir, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
		abs = filepath.Join(dir, filepath.Base(path))
	}
	if rel, err := filepath.Rel(top, abs); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(path)
}

func (p *Pane) layoutReview() {
	var rows []reviewRow
	if n := len(p.review.rv.Commits); n > 0 {
		rows = append(rows, reviewRow{kind: rowCommits})
		if p.review.expanded {
			for i := range n {
				rows = append(rows, reviewRow{kind: rowCommit, idx: i})
			}
		}
	}
	for i := range p.review.rv.Files {
		rows = append(rows, reviewRow{kind: rowFile, idx: i})
	}
	p.review.rows = rows
	p.review.cursor = max(min(p.review.cursor, len(rows)-1), 0)
}

func (p Pane) firstFileRow() int {
	for i, r := range p.review.rows {
		if r.kind == rowFile {
			return i
		}
	}
	return 0
}

func (p *Pane) selectReviewPath(path string) bool {
	for i, r := range p.review.rows {
		if r.kind == rowFile && p.review.rv.Files[r.idx].Path == path {
			p.review.cursor = i
			return true
		}
	}
	p.review.cursor = p.firstFileRow()
	return false
}

func (p Pane) reviewRowAt(i int) (reviewRow, bool) {
	if i < 0 || i >= len(p.review.rows) {
		return reviewRow{}, false
	}
	return p.review.rows[i], true
}

func (p Pane) reviewFile() (ReviewFile, bool) {
	r, ok := p.reviewRowAt(p.review.cursor)
	if !ok || r.kind != rowFile {
		return ReviewFile{}, false
	}
	return p.review.rv.Files[r.idx], true
}

func (p Pane) isViewed(path string) bool {
	return p.review.viewed.Is(path, p.review.hashes[path])
}

func (p Pane) requestReviewDiff() (Pane, tea.Cmd) {
	p.review.diffSeq++
	return p.fetchReviewDiff()
}

func (p Pane) fetchReviewDiff() (Pane, tea.Cmd) {
	f, ok := p.reviewFile()
	if !ok || p.repo == nil {
		if r, ok := p.reviewRowAt(p.review.cursor); !ok || r.kind == rowCommits {
			p.rawDiff, p.shownKey = "", diffKey{}
			p.setDiffContent(ui.Muted.Render("enter on a commit shows it"))
		}
		return p, nil
	}
	seq, repo, rv := p.review.diffSeq, p.repo, p.review.rv
	return p, func() tea.Msg {
		text, err := repo.ReviewDiff(context.Background(), rv, f)
		return reviewDiffMsg{seq: seq, path: f.Path, text: text, err: err}
	}
}

func (p Pane) scheduleReviewDiff() (Pane, tea.Cmd) {
	p.review.diffSeq++
	seq := p.review.diffSeq
	return p, tea.Tick(diffDebounce, func(time.Time) tea.Msg { return reviewDiffDueMsg{seq: seq} })
}

func (p Pane) reviewDiffLoaded(msg reviewDiffMsg) Pane {
	if msg.seq != p.review.diffSeq || p.mode != modeReview {
		return p
	}
	p.shownKey = diffKey{}
	if msg.err != nil {
		p.rawDiff = ""
		p.setDiffContent(ui.Err.Render(msg.err.Error()))
		return p
	}
	p.rawDiff = msg.text
	p.setDiffContent(renderDiff(p.opts.Theme, msg.text))
	p.diff.GotoTop()
	if strings.TrimSpace(p.rawDiff) != "" {
		p.armDiffCursor()
		p.placePendingLine()
	}
	return p
}

func (p Pane) reviewMove(delta int) (Pane, tea.Cmd) {
	return p.reviewMoveTo(p.review.cursor + delta)
}

func (p Pane) reviewMoveTo(i int) (Pane, tea.Cmd) {
	i = max(min(i, len(p.review.rows)-1), 0)
	if i == p.review.cursor || len(p.review.rows) == 0 {
		return p, nil
	}
	p.review.cursor = i
	return p.scheduleReviewDiff()
}

func (p Pane) reviewKey(msg tea.KeyMsg) (Pane, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		return p.reviewMove(1)
	case "k", "up":
		return p.reviewMove(-1)
	case "g", "home":
		return p.reviewMoveTo(0)
	case "G", "end":
		return p.reviewMoveTo(len(p.review.rows) - 1)
	case "enter", "right", "l":
		return p.reviewEnter()
	case " ", "s":
		return p.toggleViewed()
	case "n":
		return p.nextUnviewed()
	case "u":
		p.review.uncommitted = !p.review.uncommitted
		return p.loadReview()
	case "e":
		return p.reviewEdit()
	case "r":
		p, status := p.reload()
		p, review := p.loadReview()
		return p, tea.Batch(status, review)
	case "v", "esc", "h", "left":
		p.mode, p.focus = modeChanges, focusList
		return p.requestDiff()
	}
	return p, nil
}

func (p Pane) reviewEnter() (Pane, tea.Cmd) {
	r, ok := p.reviewRowAt(p.review.cursor)
	if !ok {
		return p, nil
	}
	switch r.kind {
	case rowCommits:
		p.review.expanded = !p.review.expanded
		p.layoutReview()
		return p, nil
	case rowCommit:
		if p.repo == nil {
			return p, nil
		}
		c := p.review.rv.Commits[r.idx]
		p.focus = focusDiff
		p.rawDiff, p.shownKey = "", diffKey{}
		p.review.diffSeq++
		p.setDiffContent(ui.Muted.Render("loading " + c.Hash[:min(7, len(c.Hash))] + "…"))
		repo, hash := p.repo, c.Hash
		return p, func() tea.Msg {
			text, err := repo.Show(context.Background(), hash)
			return showLoadedMsg{hash: hash, text: text, err: err}
		}
	}
	p.focus = focusDiff
	return p, nil
}

func (p Pane) toggleViewed() (Pane, tea.Cmd) {
	f, ok := p.reviewFile()
	if !ok || p.repo == nil {
		return p, nil
	}
	files := make(map[string]string, len(p.review.viewed.Files)+1)
	for k, v := range p.review.viewed.Files {
		files[k] = v
	}
	if p.isViewed(f.Path) {
		delete(files, f.Path)
	} else {
		files[f.Path] = p.review.hashes[f.Path]
	}
	p.review.viewed = Viewed{Base: p.review.rv.Base, Files: files}
	v, gitDir := p.review.viewed, p.repo.GitDir
	return p, func() tea.Msg { return viewedSavedMsg{err: v.Save(gitDir)} }
}

func (p Pane) nextUnviewed() (Pane, tea.Cmd) {
	n := len(p.review.rows)
	for step := 1; step <= n; step++ {
		i := (p.review.cursor + step) % n
		if r := p.review.rows[i]; r.kind == rowFile && !p.isViewed(p.review.rv.Files[r.idx].Path) {
			return p.reviewMoveTo(i)
		}
	}
	p.setStatus("every file is viewed", false)
	return p, nil
}

func (p Pane) reviewEdit() (Pane, tea.Cmd) {
	f, ok := p.reviewFile()
	if !ok || p.repo == nil {
		return p, nil
	}
	if f.Status == 'D' {
		p.setStatus(f.Path+" is deleted", true)
		return p, nil
	}
	msg := editor.OpenMsg{Target: editor.Target{Path: p.repo.Abs(f.Path), Line: firstHunkLine(p.rawDiff)}}
	return p, func() tea.Msg { return msg }
}

func (p Pane) reviewMouse(msg tea.MouseMsg) (Pane, tea.Cmd) {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		return p.reviewMove(-1)
	case tea.MouseButtonWheelDown:
		return p.reviewMove(1)
	case tea.MouseButtonLeft:
		if msg.Action != tea.MouseActionPress {
			return p, nil
		}
		p.focus = focusList
		if r := p.review.offset + msg.Y - p.listTop(); r >= 0 && r < len(p.review.rows) {
			return p.reviewMoveTo(r)
		}
	}
	return p, nil
}

func (p Pane) reviewScrollOffset() int {
	vis, off, cur := p.visibleRows(), p.review.offset, p.review.cursor
	if cur < off {
		off = cur
	}
	if cur >= off+vis {
		off = cur - vis + 1
	}
	return max(min(off, len(p.review.rows)-vis), 0)
}

func (p Pane) reviewHeader() string {
	rv := p.review.rv
	added, deleted, viewed := 0, 0, 0
	for _, f := range rv.Files {
		added += max(f.Added, 0)
		deleted += max(f.Deleted, 0)
		if p.isViewed(f.Path) {
			viewed++
		}
	}
	parts := []string{"Review"}
	switch {
	case rv.Base != "" && rv.MergeBase != "":
		parts = append(parts, "vs "+rv.Base+" @ "+rv.MergeBase[:min(7, len(rv.MergeBase))])
	case rv.Base != "":
		parts = append(parts, "vs "+rv.Base)
	}
	if rv.Uncommitted {
		parts = append(parts, "uncommitted")
	}
	parts = append(parts,
		plural(len(rv.Commits), "commit"),
		fmt.Sprintf("%s +%d −%d", plural(len(rv.Files), "file"), added, deleted),
		fmt.Sprintf("%d/%d viewed", viewed, len(rv.Files)))
	return lipgloss.NewStyle().Bold(true).Render(strings.Join(parts, " · "))
}

func (p Pane) reviewLines(w int) []string {
	header := ui.Muted.Render("Review …")
	if p.review.loaded {
		header = p.reviewHeader()
	}
	info := p.infoLine()
	if info == "" && p.review.rv.Fallback != "" {
		info = ui.Muted.Render(p.review.rv.Fallback)
	}
	lines := []string{ui.FitWidth(header, w, "…"), ui.FitWidth(info, w, "…")}
	switch {
	case p.review.err != "" && !p.review.loaded:
		return append(lines, ui.Err.Render(p.review.err))
	case !p.review.loaded:
		return append(lines, ui.Muted.Render("loading review…"))
	case len(p.review.rows) == 0:
		return append(lines, ui.Muted.Render("no changes"))
	}
	end := min(p.review.offset+p.visibleRows(), len(p.review.rows))
	for i := p.review.offset; i < end; i++ {
		lines = append(lines, p.reviewRowView(i, w))
	}
	return lines
}

var statusColors = map[byte]lipgloss.TerminalColor{
	'A': ui.ColorString, 'M': ui.ColorWarn, 'D': ui.ColorError, 'R': ui.ColorInfo, 'C': ui.ColorInfo,
}

func (p Pane) reviewRowView(i, w int) string {
	r := p.review.rows[i]
	cursor := i == p.review.cursor
	prefix := "  "
	if cursor {
		prefix = "› "
	}
	var text string
	switch r.kind {
	case rowCommits:
		marker := "▸"
		if p.review.expanded {
			marker = "▾"
		}
		text = lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("%s Commits (%d)", marker, len(p.review.rv.Commits)))
	case rowCommit:
		c := p.review.rv.Commits[r.idx]
		text = "  " + ui.Muted.Render(c.Hash[:min(7, len(c.Hash))]) + " " + c.Subject
	case rowFile:
		text = p.fileRowText(p.review.rv.Files[r.idx], w-len(prefix))
	}
	switch {
	case cursor && p.focus == focusList:
		return ui.FitWidth(ui.Active.Render(prefix)+text, w, "…")
	case cursor:
		return ui.FitWidth(ui.Muted.Render(prefix)+text, w, "…")
	}
	return ui.FitWidth(prefix+text, w, "…")
}

func (p Pane) fileRowText(f ReviewFile, w int) string {
	counts := fmt.Sprintf(" +%d −%d", f.Added, f.Deleted)
	if f.Binary {
		counts = " bin"
	}
	tick := ""
	if p.isViewed(f.Path) {
		tick = " " + lipgloss.NewStyle().Foreground(ui.ColorString).Render("✓")
	}
	label := f.Path
	if f.OldPath != "" {
		label = f.OldPath + " → " + f.Path
	}
	room := w - 2 - len(counts) - lipgloss.Width(tick)
	color := lipgloss.NewStyle().Foreground(statusColors[f.Status])
	return color.Render(string(f.Status)) + " " + TruncatePath(label, max(room, 4)) + ui.Muted.Render(counts) + tick
}

var (
	bindViewed      = key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "viewed"))
	bindNextUnseen  = key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "next unviewed"))
	bindUncommitted = key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "uncommitted only"))
	bindReview      = key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "review branch"))
	bindLeaveReview = key.NewBinding(key.WithKeys("v", "esc"), key.WithHelp("v/esc", "changes"))
)

func (p Pane) reviewBindings() []key.Binding {
	return []key.Binding{bindMove, bindDiff, bindViewed, bindNextUnseen, bindUncommitted, bindEdit, bindRefresh, bindLeaveReview}
}
