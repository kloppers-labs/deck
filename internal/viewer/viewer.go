// Package viewer is deck's read-only code viewer: async loading and highlighting,
// a cursor, scrolling, search and goto, rendered in time proportional to the visible rows.
package viewer

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/kloppers-labs/deck/internal/editor"
	"github.com/kloppers-labs/deck/internal/ui"
)

const (
	wheelBurst     = 40 * time.Millisecond
	maxWheelAccel  = 3
	bigJump        = 10
	cacheSize      = 16
	bracketScanCap = 10000
	wheelHoverRows = 3
)

var clipboardOut io.Writer = os.Stdout

// Options configures a viewer. Zero values fall back to the defaults.
type Options struct {
	Root              string
	Theme             string
	TabWidth          int
	ScrollLines       int
	MaxHighlightBytes int
	LineNumbers       bool
	Wrap              bool
	Navigator         Navigator
	HoverDelay        time.Duration
	GotoClick         []string
	RequestTimeout    time.Duration
}

// Spot is a cursor line, rune column and top line, all 0-based. A negative Top
// centres the cursor line.
type Spot struct{ Line, Col, Top int }

type prompt struct {
	kind      rune
	text      string
	regex     bool
	origin    Pos
	originTop int
}

type searchState struct {
	query   string
	regex   bool
	matches []Match
	idx     int
	err     error
}

type rendered struct {
	width        int
	lines, plain []string
}

type row struct{ line, sub int }

// Model is the viewer state. Documents sit behind pointers so copying the
// model stays cheap.
type Model struct {
	opts    Options
	cache   *Cache
	latest  *atomic.Int64
	gen     int
	path    string
	doc     *Document
	err     error
	note    string
	pending *Spot
	rawMD   bool
	md      *rendered
	cur     Pos
	want    int
	top     int
	left    int
	numbers bool
	wrap    bool
	focused bool
	search  searchState
	prompt  prompt
	chord   Chord
	wheelAt time.Time
	accel   int
	now     func() time.Time
	width   int
	height  int
	rows    []row

	nav       Navigator
	reqs      [numNavKinds]int64
	hoverStop context.CancelFunc
	hover     hoverState
	dwellAt   dwellState
	symbols   []Symbol
	symGen    int
	outline   bool
	diags     *diagStore
	hints     map[string]bool
	flash     string
	clicks    ui.ClickTracker
}

func New(o Options) Model {
	if o.TabWidth <= 0 {
		o.TabWidth = 4
	}
	if o.ScrollLines <= 0 {
		o.ScrollLines = 3
	}
	if o.Theme == "" {
		o.Theme = "dracula"
	}
	return Model{
		opts:    o,
		cache:   NewCache(cacheSize),
		latest:  new(atomic.Int64),
		numbers: o.LineNumbers,
		wrap:    o.Wrap,
		now:     time.Now,
		nav:     o.Navigator,
		diags:   &diagStore{byPath: map[string][]Diagnostic{}},
		hints:   map[string]bool{},
	}
}

func (m Model) loadOptions() LoadOptions {
	return LoadOptions{TabWidth: m.opts.TabWidth, Theme: m.opts.Theme, Cache: m.cache}
}

func (m *Model) nextGen() {
	m.gen = int(m.latest.Add(1))
}

// Open starts loading path. at, when set, is where the cursor goes once the
// document arrives.
func (m Model) Open(path string, at *Spot) (Model, tea.Cmd) {
	m.nextGen()
	m.path, m.pending, m.note, m.err = path, at, "", nil
	m.doc, m.md = nil, nil
	m.cur, m.want, m.top, m.left = Pos{}, 0, 0, 0
	m.prompt, m.chord = prompt{}, Chord{}
	m.search.matches, m.search.idx = nil, 0
	m.resetNav()
	m.relayout()
	return m, LoadCmd(path, m.gen, m.loadOptions())
}

func (m *Model) resetNav() {
	m.closeHover()
	m.dwellAt = dwellState{}
	m.symbols, m.symGen, m.outline = nil, 0, false
	for k := range m.reqs {
		m.reqs[k] = 0
	}
}

// Reload reopens the current file where the cursor is now.
func (m Model) Reload() (Model, tea.Cmd) {
	if m.path == "" {
		return m, nil
	}
	s := m.Spot()
	if IsVirtual(m.path) {
		return m.OpenURI(m.path, &s)
	}
	return m.Open(m.path, &s)
}

// ShowNote replaces the document with a one-line note, e.g. for a directory.
func (m Model) ShowNote(text string) Model {
	m.nextGen()
	m.path, m.doc, m.md, m.err, m.pending = "", nil, nil, nil, nil
	m.note = text
	m.prompt, m.chord = prompt{}, Chord{}
	m.resetNav()
	m.relayout()
	return m
}

func (m Model) Path() string { return m.path }

func (m Model) Loc() Loc { return Loc{Path: m.path, Line: m.cur.Line, Col: m.cur.Col} }

func (m Model) Spot() Spot { return Spot{Line: m.cur.Line, Col: m.cur.Col, Top: m.top} }

func (m Model) Loaded() bool { return m.doc != nil }

// RawMarkdown reports whether markdown shows as source rather than rendered.
func (m Model) RawMarkdown() bool { return m.rawMD }

// SetRawMarkdown switches markdown between source and rendered, from the top.
func (m Model) SetRawMarkdown(raw bool) Model {
	if m.rawMD == raw {
		return m
	}
	m.rawMD = raw
	m.cur, m.want, m.top, m.left = Pos{}, 0, 0, 0
	m.search.matches = nil
	if m.search.query != "" && m.doc != nil {
		m.runSearch()
	}
	m.closeHover()
	m.relayout()
	return m
}

// Highlighted reports whether every line of the current document is styled.
func (m Model) Highlighted() bool {
	d := m.doc
	if d == nil {
		return false
	}
	if d.highlighted {
		return true
	}
	if len(d.chunkDone) == 0 {
		return false
	}
	for _, done := range d.chunkDone {
		if !done {
			return false
		}
	}
	return true
}

func (m *Model) SetFocused(f bool) { m.focused = f }

// Capturing is true while a prompt is open or a chord is pending.
func (m Model) Capturing() bool { return m.prompt.kind != 0 || m.chord.Pending() }

// WantsEsc reports whether esc has something to close in the viewer.
func (m Model) WantsEsc() bool { return m.Capturing() || m.search.query != "" || m.hover.open }

func (m *Model) SetSize(w, h int) {
	m.width, m.height = max(w, 0), max(h, 0)
	if m.doc != nil {
		m.ensureVisible()
	}
	m.relayout()
}

// ResizeCmd re-renders width-dependent content (markdown) after SetSize.
func (m Model) ResizeCmd() tea.Cmd { return m.ensureContent() }

func (m Model) showingMD() bool {
	return m.doc != nil && m.doc.Markdown && !m.rawMD && m.md != nil
}

func (m Model) wantMD() bool { return m.doc != nil && m.doc.Markdown && !m.rawMD }

func (m Model) lines() (raw, plain []string) {
	if m.showingMD() {
		return m.md.plain, m.md.plain
	}
	if m.doc == nil {
		return nil, nil
	}
	return m.doc.Raw, m.doc.Plain
}

func (m Model) styledLine(i int) string {
	if m.showingMD() {
		return m.md.lines[i]
	}
	if s := m.doc.Styled[i]; s != "" {
		return s
	}
	return m.doc.Plain[i]
}

func (m Model) tabWidth() int {
	if m.doc != nil {
		return m.doc.TabWidth
	}
	return m.opts.TabWidth
}

func (m Model) bodyHeight() int { return max(m.height-2, 0) }

func (m Model) gutterWidth() int {
	raw, _ := m.lines()
	if !m.numbers || m.showingMD() || len(raw) == 0 || m.doc.virtual {
		return 0
	}
	return len(strconv.Itoa(len(raw))) + 3
}

func (m Model) layout() Layout {
	g := m.gutterWidth()
	return Layout{
		Gutter:   g,
		TextW:    max(m.width-g, 1),
		Height:   m.bodyHeight(),
		Top:      m.top,
		Left:     m.left,
		TabWidth: m.tabWidth(),
		Wrap:     m.wrap || m.showingMD(),
	}
}

func (m *Model) relayout() {
	raw, _ := m.lines()
	l := m.layout()
	rows := make([]row, 0, l.Height)
	for i := m.top; i < len(raw) && len(rows) < l.Height; i++ {
		n := l.rowsOf(raw[i])
		for s := 0; s < n && len(rows) < l.Height; s++ {
			rows = append(rows, row{i, s})
		}
	}
	m.rows = rows
}

func (m Model) lastLine() int {
	raw, _ := m.lines()
	return max(len(raw)-1, 0)
}

func (m Model) clampPos(p Pos) Pos {
	raw, _ := m.lines()
	if len(raw) == 0 {
		return Pos{}
	}
	p.Line = min(max(p.Line, 0), len(raw)-1)
	n := utf8.RuneCountInString(raw[p.Line])
	p.Col = min(max(p.Col, 0), max(n-1, 0))
	return p
}

func (m *Model) ensureVisible() {
	raw, _ := m.lines()
	l := EnsureVisible(raw, m.layout(), m.cur)
	m.top, m.left = l.Top, l.Left
}

func (m *Model) moveTo(p Pos) {
	m.cur = m.clampPos(p)
	raw, _ := m.lines()
	if len(raw) > 0 {
		m.want = DispCol(raw[m.cur.Line], m.cur.Col, m.tabWidth())
	}
	m.ensureVisible()
}

func (m *Model) moveLine(line int) {
	raw, _ := m.lines()
	if len(raw) == 0 {
		return
	}
	line = min(max(line, 0), len(raw)-1)
	m.cur = m.clampPos(Pos{line, RuneCol(raw[line], m.want, m.tabWidth())})
	m.ensureVisible()
}

func (m *Model) setTop(top int) {
	raw, _ := m.lines()
	m.top = min(max(top, 0), MaxTop(raw, m.layout()))
}

// scrollBy moves the view and drags the cursor along only as far as needed to
// keep it on screen.
func (m *Model) scrollBy(dy int) {
	m.setTop(m.top + dy)
	m.relayout()
	if len(m.rows) == 0 {
		return
	}
	off := min(scrollOffRows, (m.bodyHeight()-1)/2)
	lo, hi := m.rows[0].line, m.rows[len(m.rows)-1].line
	if lo > 0 {
		lo = min(lo+off, hi)
	}
	if hi < m.lastLine() {
		hi = max(hi-off, lo)
	}
	if m.cur.Line < lo || m.cur.Line > hi {
		raw, _ := m.lines()
		line := min(max(m.cur.Line, lo), hi)
		m.cur = m.clampPos(Pos{line, RuneCol(raw[line], m.want, m.tabWidth())})
	}
}

// page moves the cursor and the view together, then lets the scroll-off pull
// the cursor back inside the view.
func (m *Model) page(n int) {
	raw, _ := m.lines()
	line := min(max(m.cur.Line+n, 0), len(raw)-1)
	m.cur = m.clampPos(Pos{line, RuneCol(raw[line], m.want, m.tabWidth())})
	m.scrollBy(n)
}

func (m *Model) scrollLeft(dx int) {
	if m.wrap || m.showingMD() {
		return
	}
	m.left = max(m.left+dx, 0)
}

func (m *Model) jumpTo(p Pos) {
	m.moveTo(p)
	m.center()
}

func (m *Model) center() { m.setTop(m.cur.Line - m.bodyHeight()/2) }

// Update handles keys, mouse and the viewer's own async messages.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case DocLoadedMsg:
		cmd = m.loaded(msg)
	case DocHighlightedMsg:
		cmd = m.highlighted(msg)
	case MarkdownRenderedMsg:
		if msg.Doc == m.doc && msg.Width == m.width {
			plain := make([]string, len(msg.Lines))
			for i, l := range msg.Lines {
				plain[i] = ansi.Strip(l)
			}
			m.md = &rendered{width: msg.Width, lines: msg.Lines, plain: plain}
			m.cur = m.clampPos(m.cur)
			m.ensureVisible()
		}
	case LocationsMsg:
		cmd = m.located(msg)
	case HoverMsg:
		cmd = m.gotHover(msg)
	case SymbolsMsg:
		cmd = m.gotSymbols(msg)
	case hoverDwellMsg:
		cmd = m.dwellDue(msg)
	case hoverLeaveMsg:
		m.leaveDue(msg)
	case tea.KeyMsg:
		cmd = m.key(msg)
	case tea.MouseMsg:
		cmd = m.mouse(msg)
	}
	m.relayout()
	return m, tea.Batch(cmd, m.ensureContent())
}

func (m *Model) loaded(msg DocLoadedMsg) tea.Cmd {
	if msg.Gen != m.gen {
		return nil
	}
	if msg.Err != nil {
		m.err, m.doc = msg.Err, nil
		return nil
	}
	m.doc, m.err, m.md = msg.Doc, nil, nil
	if m.search.query != "" {
		m.runSearch()
	}
	if p := m.pending; p != nil {
		m.pending = nil
		m.moveTo(Pos{p.Line, p.Col})
		if p.Top < 0 {
			m.center()
		} else {
			m.setTop(p.Top)
		}
		m.ensureVisible()
	}
	return m.requestSymbols()
}

func (m *Model) highlighted(msg DocHighlightedMsg) tea.Cmd {
	d := msg.Doc
	if d == nil {
		return nil
	}
	c := msg.From / chunkLines
	whole := !d.big(m.opts.MaxHighlightBytes)
	if msg.Skipped {
		if whole && d.hlGen == msg.Gen {
			d.hlGen = 0
		} else if !whole && c < len(d.chunkGen) && d.chunkGen[c] == msg.Gen {
			d.chunkGen[c] = 0
		}
		return nil
	}
	copy(d.Styled[msg.From:], msg.Lines)
	if whole {
		d.highlighted = true
	} else if c < len(d.chunkDone) {
		d.chunkDone[c] = true
	}
	return nil
}

// ensureContent requests what the visible part still needs: markdown at this
// width, the whole-file highlight, or the chunks around the viewport for big files.
func (m Model) ensureContent() tea.Cmd {
	d := m.doc
	if d == nil || d.Binary || m.width <= 0 {
		return nil
	}
	if m.wantMD() {
		if req := [2]int{m.gen, m.width}; (m.md == nil || m.md.width != m.width) && d.mdReq != req {
			d.mdReq = req
			return m.markdownCmd(d, m.gen, m.width)
		}
		return nil
	}
	if !d.big(m.opts.MaxHighlightBytes) {
		if d.highlighted || d.hlGen == m.gen {
			return nil
		}
		d.hlGen = m.gen
		return m.highlightCmd(d, m.gen, 0, len(d.Raw))
	}
	if d.chunkGen == nil {
		d.chunkGen, d.chunkDone = make([]int, d.chunks()), make([]bool, d.chunks())
	}
	var cmds []tea.Cmd
	first := m.top / chunkLines
	last := min((m.top+m.bodyHeight())/chunkLines, d.chunks()-1)
	for c := first; c <= last; c++ {
		if d.chunkDone[c] || d.chunkGen[c] == m.gen {
			continue
		}
		d.chunkGen[c] = m.gen
		cmds = append(cmds, m.highlightCmd(d, m.gen, c*chunkLines, min((c+1)*chunkLines, len(d.Raw))))
	}
	return tea.Batch(cmds...)
}

func (m *Model) key(msg tea.KeyMsg) tea.Cmd {
	m.flash = ""
	k := msg.String()
	if m.hover.open {
		switch k {
		case "ctrl+j":
			m.scrollHover(1)
			return nil
		case "ctrl+k":
			m.scrollHover(-1)
			return nil
		case "esc":
			m.closeHover()
			return nil
		}
	}
	m.closeHover()
	if m.prompt.kind != 0 {
		return m.promptKey(msg)
	}
	if action, consumed := m.chord.Feed(k); consumed {
		return m.chordAction(action)
	}
	if m.doc == nil {
		return nil
	}
	raw, _ := m.lines()
	if len(raw) == 0 {
		return nil
	}
	half := max(m.bodyHeight()/2, 1)
	page := max(m.bodyHeight(), 1)
	switch k {
	case "j", "down":
		m.moveLine(m.cur.Line + 1)
	case "k", "up":
		m.moveLine(m.cur.Line - 1)
	case "h", "left":
		m.moveTo(Pos{m.cur.Line, m.cur.Col - 1})
	case "l", "right":
		m.moveTo(Pos{m.cur.Line, m.cur.Col + 1})
	case "w":
		m.moveTo(m.nextWord())
	case "b":
		m.moveTo(m.prevWord())
	case "0", "home":
		m.moveTo(Pos{m.cur.Line, 0})
	case "^":
		m.moveTo(Pos{m.cur.Line, firstNonBlank(raw[m.cur.Line])})
	case "$", "end":
		m.moveTo(Pos{m.cur.Line, utf8.RuneCountInString(raw[m.cur.Line])})
	case "G":
		from := m.Loc()
		m.moveLine(len(raw) - 1)
		return m.jumpedFrom(from)
	case "H", "M", "L":
		m.screenLine(k)
	case "%":
		if p, ok := matchBracket(raw, m.cur); ok {
			m.moveTo(p)
		}
	case "ctrl+d":
		m.page(half)
	case "ctrl+u":
		m.page(-half)
	case "pgdown", " ":
		m.page(page)
	case "pgup":
		m.page(-page)
	case "J":
		m.page(bigJump)
	case "K":
		if m.nav != nil && m.Status().State != ServerNone && m.lspDoc() {
			return m.requestHover(m.cur, false)
		}
		m.page(-bigJump)
	case "o":
		return m.openOutline()
	case "f12":
		return m.navigate(NavDefinition, m.cur, false)
	case ":":
		m.prompt = prompt{kind: ':', origin: m.cur}
	case "/":
		m.prompt = prompt{kind: '/', origin: m.cur, originTop: m.top, regex: m.search.regex}
	case "n":
		m.stepMatch(true)
	case "N":
		m.stepMatch(false)
	case "*":
		m.searchWord()
	case "esc":
		m.search = searchState{regex: m.search.regex}
	case "e":
		return m.openEditor()
	case "y":
		label := m.relPath() + ":" + strconv.Itoa(m.cur.Line+1)
		return copyCmd(label, label)
	case "Y":
		return copyCmd(raw[m.cur.Line], "line "+strconv.Itoa(m.cur.Line+1))
	case "W":
		m.wrap = !m.wrap
		m.left = 0
		m.ensureVisible()
	case "#":
		m.numbers = !m.numbers
		m.ensureVisible()
	case "m":
		if m.doc.Markdown {
			*m = m.SetRawMarkdown(!m.rawMD)
		}
	}
	return nil
}

func (m *Model) chordAction(action string) tea.Cmd {
	raw, _ := m.lines()
	if len(raw) == 0 {
		return nil
	}
	switch action {
	case "gg":
		from := m.Loc()
		m.moveLine(0)
		return m.jumpedFrom(from)
	case "gd":
		return m.navigate(NavDefinition, m.cur, false)
	case "gy":
		return m.navigate(NavTypeDefinition, m.cur, false)
	case "gi":
		return m.navigate(NavImplementation, m.cur, false)
	case "gr":
		return m.navigate(NavReferences, m.cur, false)
	case "]d", "[d":
		if p, ok := NextDiagnostic(m.diagnostics(), m.cur, action == "]d"); ok {
			m.moveTo(p)
		} else {
			m.flash = "no diagnostics"
		}
	case "zz":
		m.center()
	case "zt":
		m.setTop(m.cur.Line)
	case "zb":
		m.setTop(m.cur.Line - m.bodyHeight() + 1)
	}
	return nil
}

func (m *Model) screenLine(k string) {
	if len(m.rows) == 0 {
		return
	}
	off := min(scrollOffRows, (m.bodyHeight()-1)/2)
	first, last := m.rows[0].line, m.rows[len(m.rows)-1].line
	switch k {
	case "H":
		if first > 0 {
			first = min(first+off, last)
		}
		m.moveLine(first)
	case "M":
		m.moveLine(m.rows[len(m.rows)/2].line)
	case "L":
		if last < m.lastLine() {
			last = max(last-off, first)
		}
		m.moveLine(last)
	}
}

func (m Model) nextWord() Pos {
	raw, _ := m.lines()
	line := raw[m.cur.Line]
	if c := nextWordStart(line, m.cur.Col); c < utf8.RuneCountInString(line) {
		return Pos{m.cur.Line, c}
	}
	for l := m.cur.Line + 1; l < len(raw); l++ {
		if fb := firstNonBlank(raw[l]); fb < utf8.RuneCountInString(raw[l]) {
			return Pos{l, fb}
		}
	}
	return m.cur
}

func (m Model) prevWord() Pos {
	raw, _ := m.lines()
	if m.cur.Col > 0 && firstNonBlank(raw[m.cur.Line]) < m.cur.Col {
		return Pos{m.cur.Line, prevWordStart(raw[m.cur.Line], m.cur.Col)}
	}
	for l := m.cur.Line - 1; l >= 0; l-- {
		n := utf8.RuneCountInString(raw[l])
		if firstNonBlank(raw[l]) < n {
			return Pos{l, prevWordStart(raw[l], n)}
		}
	}
	return Pos{m.cur.Line, 0}
}

func firstNonBlank(line string) int {
	i := 0
	for _, r := range line {
		if r != ' ' && r != '\t' {
			return i
		}
		i++
	}
	return i
}

var bracketPairs = map[rune]rune{'(': ')', '[': ']', '{': '}', ')': '(', ']': '[', '}': '{'}

func matchBracket(lines []string, p Pos) (Pos, bool) {
	rs := []rune(lines[p.Line])
	start := p.Col
	for start < len(rs) && bracketPairs[rs[start]] == 0 {
		start++
	}
	if start >= len(rs) {
		return p, false
	}
	open := rs[start]
	want := bracketPairs[open]
	forward := open == '(' || open == '[' || open == '{'
	depth, scanned := 0, 0
	line, col := p.Line, start
	for line >= 0 && line < len(lines) && scanned < bracketScanCap {
		rs = []rune(lines[line])
		for col >= 0 && col < len(rs) {
			switch rs[col] {
			case open:
				depth++
			case want:
				depth--
				if depth == 0 {
					return Pos{line, col}, true
				}
			}
			if forward {
				col++
			} else {
				col--
			}
		}
		scanned++
		if forward {
			line++
			col = 0
		} else {
			line--
			if line >= 0 {
				col = utf8.RuneCountInString(lines[line]) - 1
			}
		}
	}
	return p, false
}

func (m *Model) promptKey(msg tea.KeyMsg) tea.Cmd {
	p := &m.prompt
	switch msg.Type {
	case tea.KeyEsc:
		if p.kind == '/' {
			m.cur, m.top = p.origin, p.originTop
			m.search = searchState{regex: m.search.regex}
		}
		m.prompt = prompt{}
		return nil
	case tea.KeyEnter:
		return m.acceptPrompt()
	case tea.KeyBackspace:
		if p.text == "" {
			return m.promptKey(tea.KeyMsg{Type: tea.KeyEsc})
		}
		_, size := utf8.DecodeLastRuneInString(p.text)
		p.text = p.text[:len(p.text)-size]
	case tea.KeyCtrlU:
		p.text = ""
	case tea.KeyCtrlR:
		if p.kind == '/' {
			p.regex = !p.regex
		}
	case tea.KeySpace:
		p.text += " "
	case tea.KeyRunes:
		p.text += string(msg.Runes)
	default:
		return nil
	}
	if p.kind == '/' {
		m.search.query, m.search.regex = p.text, p.regex
		m.runSearch()
		m.cur, m.top = p.origin, p.originTop
		if len(m.search.matches) > 0 {
			o := p.origin
			raw, _ := m.lines()
			at := Pos{o.Line, DispCol(raw[o.Line], o.Col, m.tabWidth()) - 1}
			m.gotoMatch(NextMatch(m.search.matches, at))
		}
	}
	return nil
}

func (m *Model) acceptPrompt() tea.Cmd {
	p := m.prompt
	m.prompt = prompt{}
	switch p.kind {
	case ':':
		if pos, ok := parseGoto(p.text); ok {
			from := Loc{Path: m.path, Line: p.origin.Line, Col: p.origin.Col}
			m.jumpTo(pos)
			return m.jumpedFrom(from)
		}
	case '/':
		if p.text == "" {
			m.search = searchState{regex: m.search.regex}
		}
	}
	return nil
}

func parseGoto(s string) (Pos, bool) {
	s = strings.TrimSpace(s)
	lineStr, colStr, hasCol := strings.Cut(s, ":")
	if !hasCol {
		lineStr, colStr, hasCol = strings.Cut(s, ",")
	}
	line, err := strconv.Atoi(strings.TrimSpace(lineStr))
	if err != nil {
		return Pos{}, false
	}
	col := 1
	if hasCol {
		if col, err = strconv.Atoi(strings.TrimSpace(colStr)); err != nil {
			return Pos{}, false
		}
	}
	return Pos{line - 1, col - 1}, true
}

func (m *Model) runSearch() {
	_, plain := m.lines()
	m.search.matches, m.search.err = Find(plain, m.search.query, m.search.regex)
	m.search.idx = -1
}

func (m *Model) gotoMatch(i int) {
	if i < 0 || i >= len(m.search.matches) {
		return
	}
	m.search.idx = i
	mt := m.search.matches[i]
	raw, _ := m.lines()
	m.moveTo(Pos{mt.Line, RuneCol(raw[mt.Line], mt.Start, m.tabWidth())})
}

func (m *Model) stepMatch(forward bool) {
	if len(m.search.matches) == 0 {
		return
	}
	raw, _ := m.lines()
	at := Pos{m.cur.Line, DispCol(raw[m.cur.Line], m.cur.Col, m.tabWidth())}
	if forward {
		m.gotoMatch(NextMatch(m.search.matches, at))
	} else {
		m.gotoMatch(PrevMatch(m.search.matches, at))
	}
}

func (m *Model) searchWord() {
	raw, _ := m.lines()
	w := WordAt(raw[m.cur.Line], m.cur.Col)
	if w == "" {
		return
	}
	m.search = searchState{query: `\b` + regexp.QuoteMeta(w) + `\b`, regex: true}
	m.runSearch()
	m.stepMatch(true)
}

func (m Model) relPath() string {
	if m.opts.Root != "" {
		if rel, err := filepath.Rel(m.opts.Root, m.path); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return m.path
}

func (m *Model) openEditor() tea.Cmd {
	if IsVirtual(m.path) {
		m.flash = "read-only document"
		return nil
	}
	t := editor.Target{Path: m.path}
	if !m.showingMD() {
		t.Line, t.Col, t.Top = m.cur.Line+1, m.cur.Col+1, m.top+1
	}
	return func() tea.Msg { return editor.OpenMsg{Target: t} }
}

func copyCmd(text, label string) tea.Cmd {
	return func() tea.Msg {
		if _, err := io.WriteString(clipboardOut, ansi.SetSystemClipboard(text)); err != nil {
			return ui.ToastMsg{Level: ui.LevelError, Text: "copy: " + err.Error()}
		}
		return ui.ToastMsg{Level: ui.LevelInfo, Text: "copied " + ui.FitWidth(label, 60, "…")}
	}
}

func (m *Model) mouse(msg tea.MouseMsg) tea.Cmd {
	if m.doc == nil {
		return nil
	}
	if r, ok := m.hoverRect(); ok && r.Contains(msg.X, msg.Y) {
		switch msg.Button {
		case tea.MouseButtonWheelDown:
			m.scrollHover(wheelHoverRows)
		case tea.MouseButtonWheelUp:
			m.scrollHover(-wheelHoverRows)
		}
		m.hover.leave = 0
		return nil
	}
	ev := tea.MouseEvent(msg)
	if ev.IsWheel() {
		m.closeHover()
		n := m.wheelLines()
		switch {
		case msg.Button == tea.MouseButtonWheelLeft, msg.Shift && msg.Button == tea.MouseButtonWheelUp:
			m.scrollLeft(-2 * n)
		case msg.Button == tea.MouseButtonWheelRight, msg.Shift && msg.Button == tea.MouseButtonWheelDown:
			m.scrollLeft(2 * n)
		case msg.Button == tea.MouseButtonWheelDown:
			m.scrollBy(n)
		case msg.Button == tea.MouseButtonWheelUp:
			m.scrollBy(-n)
		}
		return nil
	}
	if msg.Action == tea.MouseActionMotion && msg.Button == tea.MouseButtonNone {
		return m.dwell(msg.X, msg.Y)
	}
	if msg.Action != tea.MouseActionPress || (msg.Button != tea.MouseButtonLeft && msg.Button != tea.MouseButtonRight) {
		return nil
	}
	y := msg.Y - 1
	if y < 0 || y >= m.bodyHeight() {
		return nil
	}
	m.closeHover()
	m.flash = ""
	raw, _ := m.lines()
	l := m.layout()
	p, onText := ScreenToDoc(raw, l, msg.X, y)
	onText = onText && msg.X >= l.Gutter
	if msg.Button == tea.MouseButtonRight {
		if !onText || !m.gotoOn("right") {
			return nil
		}
		m.moveTo(p)
		return m.navigate(NavDefinition, m.cur, true)
	}
	presses := m.clicks.Press(msg.X, msg.Y, m.now())
	switch {
	case onText:
		m.moveTo(p)
	case len(raw) > 0:
		m.moveLine(len(raw) - 1)
	}
	switch {
	case !onText:
		return nil
	case msg.Alt:
		return m.navigate(NavReferences, m.cur, true)
	case msg.Ctrl && m.gotoOn("ctrl"), ui.IsDouble(presses) && m.gotoOn("double"):
		return m.navigate(NavDefinition, m.cur, true)
	}
	return nil
}

func (m Model) gotoOn(gesture string) bool {
	if m.opts.GotoClick == nil {
		return true
	}
	for _, g := range m.opts.GotoClick {
		if strings.EqualFold(g, gesture) {
			return true
		}
	}
	return false
}

func (m *Model) wheelLines() int {
	now := m.now()
	if !m.wheelAt.IsZero() && now.Sub(m.wheelAt) < wheelBurst {
		m.accel = min(m.accel+1, maxWheelAccel)
	} else {
		m.accel = 1
	}
	m.wheelAt = now
	return m.opts.ScrollLines * m.accel
}

// HelpBindings lists the viewer keys for the footer and help overlay.
func (m Model) HelpBindings() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("j", "k"), key.WithHelp("j/k", "move")),
		key.NewBinding(key.WithKeys("ctrl+d", "ctrl+u"), key.WithHelp("ctrl+d/u", "half page")),
		key.NewBinding(key.WithKeys("g", "G"), key.WithHelp("gg/G", "top/bottom")),
		key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
		key.NewBinding(key.WithKeys("n", "N"), key.WithHelp("n/N", "next/prev")),
		key.NewBinding(key.WithKeys(":"), key.WithHelp(":", "goto")),
		key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit here")),
		key.NewBinding(key.WithKeys("y", "Y"), key.WithHelp("y/Y", "copy path:line/line")),
		key.NewBinding(key.WithKeys("W"), key.WithHelp("W", "wrap")),
		key.NewBinding(key.WithKeys("#"), key.WithHelp("#", "line numbers")),
		key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "markdown raw")),
		key.NewBinding(key.WithKeys("w", "b"), key.WithHelp("w/b", "word")),
		key.NewBinding(key.WithKeys("z"), key.WithHelp("zz/zt/zb", "centre/top/bottom")),
		key.NewBinding(key.WithKeys("%"), key.WithHelp("%", "matching bracket")),
		key.NewBinding(key.WithKeys("K"), key.WithHelp("K", "hover")),
		key.NewBinding(key.WithKeys("g", "f12"), key.WithHelp("gd/F12", "definition")),
		key.NewBinding(key.WithKeys("g"), key.WithHelp("gi/gy", "implementation/type")),
		key.NewBinding(key.WithKeys("g"), key.WithHelp("gr", "references")),
		key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "outline")),
		key.NewBinding(key.WithKeys("]", "["), key.WithHelp("]d/[d", "next/prev diagnostic")),
	}
}
