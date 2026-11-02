package files

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"deck/internal/ui"
	"deck/internal/ui/picker"
)

const (
	maxGrepHits     = 2000
	grepBatch       = 128
	grepFlushEvery  = 30 * time.Millisecond
	walkMaxFileSize = 2 << 20
	walkMaxFiles    = 20_000
	binarySniff     = 8000
)

var grepDebounce = 200 * time.Millisecond

// GrepHit is one matching line: Path relative to the search root, a 1-based
// Line, a 0-based rune Col of the first match, and rune Spans of every match.
type GrepHit struct {
	Path  string
	Line  int
	Col   int
	Text  string
	Spans [][2]int
}

// GrepChunkMsg carries the next batch of hits for search Seq.
type GrepChunkMsg struct {
	Seq  int
	Hits []GrepHit
}

// GrepDoneMsg ends search Seq.
type GrepDoneMsg struct {
	Seq    int
	Count  int
	Engine string
	Capped bool
	Err    error
}

type grepDueMsg struct{ seq int }

func (GrepChunkMsg) RouteTo() ui.Dest { return ui.DestFiles }
func (GrepDoneMsg) RouteTo() ui.Dest  { return ui.DestFiles }
func (grepDueMsg) RouteTo() ui.Dest   { return ui.DestFiles }

type grepRequest struct {
	Root    string
	Pattern string
	Glob    string
	Regex   bool
	Case    bool
	Hide    HidePolicy
}

// runGrep searches with the best engine available, calling emit per hit until
// it returns false, and reports which engine ran.
var runGrep = defaultRunGrep

var grepLookPath = exec.LookPath

var errNotRepo = errors.New("not a git repository")

func defaultRunGrep(ctx context.Context, req grepRequest, emit func(GrepHit) bool) (string, error) {
	if _, err := grepLookPath("rg"); err == nil {
		return "rg", rgGrep(ctx, req, emit)
	}
	if _, err := grepLookPath("git"); err == nil {
		if err := gitGrep(ctx, req, emit); !errors.Is(err, errNotRepo) {
			return "git grep", err
		}
	}
	return "walk", walkGrep(ctx, req, emit)
}

func rgArgs(req grepRequest) []string {
	args := []string{"--json", "--max-columns", "300", "--max-columns-preview", "-m", "200"}
	if req.Case {
		args = append(args, "-s")
	} else {
		args = append(args, "--smart-case")
	}
	if !req.Regex {
		args = append(args, "-F")
	}
	if req.Glob != "" {
		args = append(args, "-g", req.Glob)
	}
	return append(args, "-e", req.Pattern, ".")
}

func rgGrep(ctx context.Context, req grepRequest, emit func(GrepHit) bool) error {
	return scanCommand(ctx, req.Root, bufio.ScanLines, func(line []byte) bool {
		h, ok := parseRGJSON(line)
		return !ok || emit(h)
	}, func(code int) error {
		if code == 1 {
			return nil
		}
		return errors.New("rg exited " + strconv.Itoa(code))
	}, "rg", rgArgs(req)...)
}

func gitGrep(ctx context.Context, req grepRequest, emit func(GrepHit) bool) error {
	match, err := compileMatcher(req)
	if err != nil {
		return err
	}
	args := []string{"grep", "-n", "--column", "-I", "-z", "--untracked", "--no-color"}
	if req.Regex {
		args = append(args, "-E")
	} else {
		args = append(args, "-F")
	}
	if !caseSensitive(req) {
		args = append(args, "-i")
	}
	args = append(args, "-e", req.Pattern, "--")
	if req.Glob != "" {
		args = append(args, req.Glob)
	}
	return scanCommand(ctx, req.Root, bufio.ScanLines, func(line []byte) bool {
		h, ok := parseGitGrep(string(line), match)
		return !ok || emit(h)
	}, func(code int) error {
		switch code {
		case 1:
			return nil
		case 128:
			return errNotRepo
		}
		return errors.New("git grep exited " + strconv.Itoa(code))
	}, "git", args...)
}

func scanCommand(ctx context.Context, dir string, split bufio.SplitFunc, each func([]byte) bool, exit func(int) error, name string, args ...string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	sc.Split(split)
	stopped := false
	for sc.Scan() {
		if !each(sc.Bytes()) {
			stopped = true
			cancel()
			break
		}
	}
	err = cmd.Wait()
	if stopped || ctx.Err() != nil {
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exit(exitErr.ExitCode())
	}
	return err
}

type rgText struct {
	Text  *string `json:"text"`
	Bytes string  `json:"bytes"`
}

func (t rgText) String() string {
	if t.Text != nil {
		return *t.Text
	}
	b, _ := base64.StdEncoding.DecodeString(t.Bytes)
	return string(b)
}

type rgEvent struct {
	Type string `json:"type"`
	Data struct {
		Path       rgText `json:"path"`
		Lines      rgText `json:"lines"`
		LineNumber int    `json:"line_number"`
		Submatches []struct {
			Start int `json:"start"`
			End   int `json:"end"`
		} `json:"submatches"`
	} `json:"data"`
}

// parseRGJSON turns one `rg --json` match line into a hit, converting byte
// offsets to rune columns.
func parseRGJSON(line []byte) (GrepHit, bool) {
	if !bytes.Contains(line, []byte(`"type":"match"`)) {
		return GrepHit{}, false
	}
	var ev rgEvent
	if err := json.Unmarshal(line, &ev); err != nil || ev.Type != "match" {
		return GrepHit{}, false
	}
	text := strings.TrimRight(ev.Data.Lines.String(), "\r\n")
	h := GrepHit{
		Path: filepath.ToSlash(strings.TrimPrefix(ev.Data.Path.String(), "./")),
		Line: ev.Data.LineNumber,
		Text: text,
	}
	for _, sm := range ev.Data.Submatches {
		h.Spans = append(h.Spans, [2]int{runeCol(text, sm.Start), runeCol(text, sm.End)})
	}
	if len(h.Spans) > 0 {
		h.Col = h.Spans[0][0]
	}
	return h, true
}

func runeCol(s string, b int) int {
	return utf8.RuneCountInString(s[:min(max(b, 0), len(s))])
}

// parseGitGrep reads one `git grep -n --column -z` line (path NUL line NUL
// column NUL text); spans come from match since git reports only the first.
func parseGitGrep(line string, match func(string) [][2]int) (GrepHit, bool) {
	parts := strings.SplitN(line, "\x00", 4)
	if len(parts) != 4 {
		return GrepHit{}, false
	}
	n, err1 := strconv.Atoi(parts[1])
	col, err2 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil {
		return GrepHit{}, false
	}
	h := GrepHit{Path: parts[0], Line: n, Text: parts[3], Spans: match(parts[3])}
	h.Col = runeCol(h.Text, col-1)
	if len(h.Spans) > 0 {
		h.Col = h.Spans[0][0]
	}
	return h, true
}

func caseSensitive(req grepRequest) bool {
	return req.Case || strings.IndexFunc(req.Pattern, unicode.IsUpper) >= 0
}

// compileMatcher returns rune spans of every match of req's pattern in a line,
// with rg's smart-case and literal defaults.
func compileMatcher(req grepRequest) (func(string) [][2]int, error) {
	expr := req.Pattern
	if !req.Regex {
		expr = regexp.QuoteMeta(expr)
	}
	if !caseSensitive(req) {
		expr = "(?i)" + expr
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		return nil, err
	}
	return func(line string) [][2]int {
		var out [][2]int
		for _, loc := range re.FindAllStringIndex(line, -1) {
			if loc[1] > loc[0] {
				out = append(out, [2]int{runeCol(line, loc[0]), runeCol(line, loc[1])})
			}
		}
		return out
	}, nil
}

var errStop = errors.New("stop")

// walkGrep is the last-resort engine: a capped walk honouring the hide policy
// that skips large and binary files.
func walkGrep(ctx context.Context, req grepRequest, emit func(GrepHit) bool) error {
	match, err := compileMatcher(req)
	if err != nil {
		return err
	}
	files := 0
	err = filepath.WalkDir(req.Root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == req.Root {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			if d.Name() == ".git" || req.Hide.Names[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(req.Root, p)
		rel = filepath.ToSlash(rel)
		if req.Glob != "" {
			if ok, _ := filepath.Match(req.Glob, d.Name()); !ok {
				if ok, _ := filepath.Match(req.Glob, rel); !ok {
					return nil
				}
			}
		}
		if files++; files > walkMaxFiles {
			return errStop
		}
		if !grepFile(p, rel, match, emit) {
			return errStop
		}
		return nil
	})
	if errors.Is(err, errStop) {
		return nil
	}
	return err
}

func grepFile(path, rel string, match func(string) [][2]int, emit func(GrepHit) bool) bool {
	f, err := os.Open(path)
	if err != nil {
		return true
	}
	defer f.Close()
	if fi, err := f.Stat(); err != nil || fi.Size() > walkMaxFileSize {
		return true
	}
	data, err := io.ReadAll(f)
	if err != nil || bytes.IndexByte(data[:min(len(data), binarySniff)], 0) >= 0 {
		return true
	}
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if spans := match(line); len(spans) > 0 {
			if !emit(GrepHit{Path: rel, Line: i + 1, Col: spans[0][0], Text: line, Spans: spans}) {
				return false
			}
		}
	}
	return true
}

// parseGrepQuery splits the query into a pattern and, in glob mode, a file
// glob taken from its last word.
func parseGrepQuery(q string, globOn bool) (pattern, glob string) {
	if !globOn {
		return q, ""
	}
	i := strings.LastIndexByte(strings.TrimRight(q, " "), ' ')
	if i < 0 {
		return q, ""
	}
	return strings.TrimRight(q[:i], " "), strings.TrimSpace(q[i+1:])
}

// grepStream runs one search in the background and hands its batches to the
// UI one Cmd at a time, the way runner streams task output.
type grepStream struct {
	ch chan tea.Msg
}

func startGrep(ctx context.Context, seq int, req grepRequest) *grepStream {
	s := &grepStream{ch: make(chan tea.Msg, 4)}
	hits := make(chan GrepHit, grepBatch)
	type result struct {
		engine string
		err    error
	}
	done := make(chan result, 1)
	run := runGrep
	go func() {
		defer close(hits)
		count := 0
		engine, err := run(ctx, req, func(h GrepHit) bool {
			if count >= maxGrepHits {
				return false
			}
			select {
			case hits <- h:
				count++
				return true
			case <-ctx.Done():
				return false
			}
		})
		done <- result{engine, err}
	}()
	go func() {
		defer close(s.ch)
		send := func(msg tea.Msg) bool {
			select {
			case s.ch <- msg:
				return true
			case <-ctx.Done():
				return false
			}
		}
		tick := time.NewTicker(grepFlushEvery)
		defer tick.Stop()
		var batch []GrepHit
		total := 0
		flush := func() bool {
			if len(batch) == 0 {
				return true
			}
			ok := send(GrepChunkMsg{Seq: seq, Hits: batch})
			batch = nil
			return ok
		}
		for {
			select {
			case h, ok := <-hits:
				if !ok {
					r := <-done
					if !flush() {
						return
					}
					send(GrepDoneMsg{Seq: seq, Count: total, Engine: r.engine, Capped: total >= maxGrepHits, Err: r.err})
					return
				}
				batch = append(batch, h)
				total++
				if len(batch) >= grepBatch && !flush() {
					return
				}
			case <-tick.C:
				if !flush() {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return s
}

func (s *grepStream) next() tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-s.ch
		if !ok {
			return nil
		}
		return msg
	}
}

// grepItems turns hits into picker rows grouped by file, the line text trimmed
// and made single-width safe so highlight positions stay aligned.
func grepItems(root string, hits []GrepHit) []picker.Item {
	items := make([]picker.Item, len(hits))
	for i, h := range hits {
		runes := []rune(h.Text)
		for j, r := range runes {
			if r == '\t' || unicode.IsControl(r) {
				runes[j] = ' '
			}
		}
		lead := 0
		for lead < len(runes) && runes[lead] == ' ' {
			lead++
		}
		var hl []int
		for _, sp := range h.Spans {
			for p := max(sp[0], lead); p < sp[1]; p++ {
				hl = append(hl, p-lead)
			}
		}
		items[i] = picker.Item{
			Title:     strings.TrimRight(string(runes[lead:]), " "),
			Detail:    ":" + strconv.Itoa(h.Line),
			Group:     h.Path,
			Path:      filepath.Join(root, filepath.FromSlash(h.Path)),
			Line:      h.Line,
			Col:       h.Col,
			Highlight: hl,
		}
	}
	return items
}

type grepState struct {
	qseq   int
	seq    int
	cancel context.CancelFunc
	stream *grepStream
	regex  bool
	cased  bool
	glob   bool
	count  int
	state  string
}

func (g *grepState) toggle(k string) bool {
	switch k {
	case "alt+r":
		g.regex = !g.regex
	case "alt+c":
		g.cased = !g.cased
	case "alt+g":
		g.glob = !g.glob
	default:
		return false
	}
	return true
}

func (g grepState) status() string {
	parts := []string{"literal", "smart-case"}
	if g.regex {
		parts[0] = "regex"
	}
	if g.cased {
		parts[1] = "case"
	}
	if g.glob {
		parts = append(parts, "glob")
	}
	if g.state != "" {
		parts = append(parts, g.state)
	}
	return strings.Join(parts, " · ")
}

func (m *Model) openGrep() tea.Cmd {
	m.openPicker(picker.Options{
		ID: pickGrep, Dest: ui.DestFiles, Title: "Grep", Live: true,
		Placeholder: "text to find (alt+r regex · alt+c case · alt+g glob)",
	})
	m.grep.state = ""
	m.pick.SetStatus(m.grep.status())
	return nil
}

func (m Model) grepQuery() (Model, tea.Cmd) {
	m.grep.qseq++
	seq := m.grep.qseq
	return m, tea.Tick(grepDebounce, func(time.Time) tea.Msg { return grepDueMsg{seq: seq} })
}

func (m *Model) stopGrep() {
	if m.grep.cancel != nil {
		m.grep.cancel()
	}
	m.grep.cancel, m.grep.stream = nil, nil
}

func (m Model) startSearch() (Model, tea.Cmd) {
	m.stopGrep()
	m.grep.seq++
	m.grep.count = 0
	pattern, glob := parseGrepQuery(m.pick.Query(), m.grep.glob)
	cmd := m.pick.SetItems(nil)
	if strings.TrimSpace(pattern) == "" {
		m.grep.state = ""
		m.pick.SetStatus(m.grep.status())
		return m, cmd
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.grep.cancel = cancel
	m.grep.stream = startGrep(ctx, m.grep.seq, grepRequest{
		Root: m.tree.Launch(), Pattern: pattern, Glob: glob,
		Regex: m.grep.regex, Case: m.grep.cased, Hide: m.tree.Hide(),
	})
	m.grep.state = "searching…"
	m.pick.SetStatus(m.grep.status())
	return m, tea.Batch(cmd, m.grep.stream.next())
}

func (m Model) grepEvent(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case grepDueMsg:
		if msg.seq != m.grep.qseq || !m.pick.Active() || m.pick.ID() != pickGrep {
			return m, nil
		}
		return m.startSearch()
	case GrepChunkMsg:
		if msg.Seq != m.grep.seq || m.grep.stream == nil {
			return m, nil
		}
		m.grep.count += len(msg.Hits)
		cmd := m.pick.Append(grepItems(m.tree.Launch(), msg.Hits))
		m.grep.state = strconv.Itoa(m.grep.count) + " hits…"
		m.pick.SetStatus(m.grep.status())
		return m, tea.Batch(cmd, m.grep.stream.next())
	case GrepDoneMsg:
		if msg.Seq != m.grep.seq {
			return m, nil
		}
		m.grep.cancel, m.grep.stream = nil, nil
		switch {
		case msg.Err != nil:
			m.grep.state = msg.Engine + ": " + msg.Err.Error()
		case msg.Capped:
			m.grep.state = strconv.Itoa(msg.Count) + "+ hits · " + msg.Engine
		default:
			m.grep.state = strconv.Itoa(msg.Count) + " hits · " + msg.Engine
		}
		m.pick.SetStatus(m.grep.status())
	}
	return m, nil
}
