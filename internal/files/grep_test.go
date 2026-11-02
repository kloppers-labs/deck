package files

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

const rgSample = `{"type":"begin","data":{"path":{"text":"./a.txt"}}}
{"type":"match","data":{"path":{"text":"./a.txt"},"lines":{"text":"héllo wörld foo\n"},"line_number":1,"absolute_offset":0,"submatches":[{"match":{"text":"foo"},"start":14,"end":17}]}}
{"type":"match","data":{"path":{"text":"./a.txt"},"lines":{"text":"\tbar foo\n"},"line_number":2,"absolute_offset":18,"submatches":[{"match":{"text":"foo"},"start":5,"end":8}]}}
{"type":"end","data":{"path":{"text":"./a.txt"},"binary_offset":null,"stats":{"elapsed":{"secs":0,"nanos":89625,"human":"0.000090s"},"searches":1,"searches_with_match":1,"bytes_searched":27,"bytes_printed":417,"matched_lines":2,"matches":2}}}
{"type":"begin","data":{"path":{"text":"./sub/b c.go"}}}
{"type":"match","data":{"path":{"text":"./sub/b c.go"},"lines":{"text":"last foo:x\n"},"line_number":2,"absolute_offset":8,"submatches":[{"match":{"text":"foo"},"start":5,"end":8}]}}
{"type":"match","data":{"path":{"bytes":"Ynl0ZXMuZ28="},"lines":{"bytes":"Zm9vIGZvbw=="},"line_number":7,"absolute_offset":0,"submatches":[{"match":{"text":"foo"},"start":0,"end":3},{"match":{"text":"foo"},"start":4,"end":7}]}}
{"data":{"elapsed_total":{"human":"0.003712s","nanos":3711750,"secs":0},"stats":{"bytes_printed":657,"bytes_searched":46,"elapsed":{"human":"0.000093s","nanos":93458,"secs":0},"matched_lines":3,"matches":3,"searches":2,"searches_with_match":2}},"type":"summary"}`

func TestParseRGJSON(t *testing.T) {
	var hits []GrepHit
	for _, line := range strings.Split(rgSample, "\n") {
		if h, ok := parseRGJSON([]byte(line)); ok {
			hits = append(hits, h)
		}
	}
	want := []GrepHit{
		{Path: "a.txt", Line: 1, Col: 12, Text: "héllo wörld foo", Spans: [][2]int{{12, 15}}},
		{Path: "a.txt", Line: 2, Col: 5, Text: "\tbar foo", Spans: [][2]int{{5, 8}}},
		{Path: "sub/b c.go", Line: 2, Col: 5, Text: "last foo:x", Spans: [][2]int{{5, 8}}},
		{Path: "bytes.go", Line: 7, Col: 0, Text: "foo foo", Spans: [][2]int{{0, 3}, {4, 7}}},
	}
	if len(hits) != len(want) {
		t.Fatalf("got %d hits, want %d: %+v", len(hits), len(want), hits)
	}
	for i := range want {
		if !hitEqual(hits[i], want[i]) {
			t.Errorf("hit %d = %+v, want %+v", i, hits[i], want[i])
		}
	}
}

func hitEqual(a, b GrepHit) bool {
	return a.Path == b.Path && a.Line == b.Line && a.Col == b.Col && a.Text == b.Text && slices.Equal(a.Spans, b.Spans)
}

func TestGitGrepFallbackParse(t *testing.T) {
	match, err := compileMatcher(grepRequest{Pattern: "foo"})
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]GrepHit{
		"sub/b c.go\x002\x006\x00last foo:x":  {Path: "sub/b c.go", Line: 2, Col: 5, Text: "last foo:x", Spans: [][2]int{{5, 8}}},
		"a.txt\x001\x0015\x00héllo wörld foo": {Path: "a.txt", Line: 1, Col: 12, Text: "héllo wörld foo", Spans: [][2]int{{12, 15}}},
		"x:y.go\x003\x001\x00Foo bar":         {Path: "x:y.go", Line: 3, Col: 0, Text: "Foo bar", Spans: [][2]int{{0, 3}}},
	}
	for line, want := range cases {
		got, ok := parseGitGrep(line, match)
		if !ok || !hitEqual(got, want) {
			t.Errorf("parseGitGrep(%q) = %+v %v, want %+v", line, got, ok, want)
		}
	}
	if _, ok := parseGitGrep("garbage", match); ok {
		t.Error("a malformed line is skipped")
	}
}

func TestMatcherSmartCaseAndRegex(t *testing.T) {
	cases := []struct {
		req  grepRequest
		line string
		want int
	}{
		{grepRequest{Pattern: "foo"}, "FOO foo", 2},
		{grepRequest{Pattern: "Foo"}, "FOO foo Foo", 1},
		{grepRequest{Pattern: "foo", Case: true}, "FOO foo", 1},
		{grepRequest{Pattern: "a.c"}, "abc a.c", 1},
		{grepRequest{Pattern: "a.c", Regex: true}, "abc a.c", 2},
	}
	for _, c := range cases {
		m, err := compileMatcher(c.req)
		if err != nil {
			t.Fatal(err)
		}
		if got := len(m(c.line)); got != c.want {
			t.Errorf("%+v on %q: %d matches, want %d", c.req, c.line, got, c.want)
		}
	}
}

func TestRGArgs(t *testing.T) {
	args := strings.Join(rgArgs(grepRequest{Pattern: "-x", Glob: "*.cs"}), " ")
	for _, want := range []string{"--json", "--smart-case", "--max-columns 300", "-m 200", "-F", "-g *.cs", "-e -x"} {
		if !strings.Contains(args, want) {
			t.Errorf("rg args %q lack %q", args, want)
		}
	}
	args = strings.Join(rgArgs(grepRequest{Pattern: "a", Regex: true, Case: true}), " ")
	if strings.Contains(args, "-F") || strings.Contains(args, "--smart-case") || !strings.Contains(args, "-s") {
		t.Errorf("regex and case toggles: %q", args)
	}
}

func TestParseGrepQuery(t *testing.T) {
	if p, g := parseGrepQuery("todo fix *.cs", true); p != "todo fix" || g != "*.cs" {
		t.Errorf("glob mode takes the last word as the glob: %q %q", p, g)
	}
	if p, g := parseGrepQuery("todo fix *.cs", false); p != "todo fix *.cs" || g != "" {
		t.Errorf("without glob mode the whole query is the pattern: %q %q", p, g)
	}
	if p, g := parseGrepQuery("todo", true); p != "todo" || g != "" {
		t.Errorf("a single word is the pattern: %q %q", p, g)
	}
}

type fakeGrep struct {
	mu      sync.Mutex
	reqs    []grepRequest
	block   bool
	started chan context.Context
}

func (f *fakeGrep) run(ctx context.Context, req grepRequest, emit func(GrepHit) bool) (string, error) {
	f.mu.Lock()
	f.reqs = append(f.reqs, req)
	f.mu.Unlock()
	if f.started != nil {
		f.started <- ctx
	}
	if f.block {
		<-ctx.Done()
		return "fake", ctx.Err()
	}
	emit(GrepHit{Path: "a.go", Line: 2, Col: 1, Text: " " + req.Pattern + " here", Spans: [][2]int{{1, 1 + len(req.Pattern)}}})
	return "fake", nil
}

func (f *fakeGrep) patterns() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, r := range f.reqs {
		out = append(out, r.Pattern)
	}
	return out
}

func useGrep(t *testing.T, f *fakeGrep) {
	t.Helper()
	oldRun, oldDelay := runGrep, grepDebounce
	t.Cleanup(func() { runGrep, grepDebounce = oldRun, oldDelay })
	runGrep, grepDebounce = f.run, time.Millisecond
}

func TestGrepStreamsResultsGroupedByFile(t *testing.T) {
	f := &fakeGrep{}
	useGrep(t, f)
	dir := fixture(t, map[string]int{"a.go": 20})
	m := started(t, dir, 120, 30)
	m, _ = press(t, m, append([]string{"ctrl+f"}, typeKeys("needle")...)...)
	if !m.pick.Active() || m.pick.Len() != 1 {
		t.Fatalf("ctrl+f searches as you type, shown %d", m.pick.Len())
	}
	it, _ := m.pick.Selected()
	if it.Group != "a.go" || it.Title != "needle here" || it.Line != 2 || it.Col != 1 || it.Path != filepath.Join(dir, "a.go") {
		t.Errorf("hits become grouped items with trimmed text: %+v", it)
	}
	if slices.Compare(it.Highlight, []int{0, 1, 2, 3, 4, 5}) != 0 {
		t.Errorf("highlights shift with the trimmed indent: %v", it.Highlight)
	}
	m, _ = press(t, m, "enter")
	if loc := m.viewer.Loc(); loc.Path != filepath.Join(dir, "a.go") || loc.Line != 1 || loc.Col != 1 || m.history.Len() == 0 {
		t.Errorf("enter jumps to the hit and records history: %+v", loc)
	}
}

func drive(t *testing.T, m Model, msgs []tea.Msg) (Model, []tea.Msg) {
	t.Helper()
	var out []tea.Msg
	for _, msg := range msgs {
		var cmd tea.Cmd
		m, cmd = m.Update(msg)
		out = append(out, collect(cmd)...)
	}
	return m, out
}

func TestDebounceAndCancel(t *testing.T) {
	f := &fakeGrep{}
	useGrep(t, f)
	dir := fixture(t, map[string]int{"a.go": 5})
	m := started(t, dir, 120, 30)
	m, _ = press(t, m, "ctrl+f")
	m, cmdA := m.Update(keyMsg("a"))
	m, cmdB := m.Update(keyMsg("b"))
	m, ticks := drive(t, m, append(collect(cmdA), collect(cmdB)...))
	m, _ = drive(t, m, ticks[:1])
	if len(f.patterns()) != 0 {
		t.Fatalf("a superseded keystroke must not search, searched %v", f.patterns())
	}
	m, _ = settle(t, m, func() tea.Msg { return ticks[1] })
	if got := f.patterns(); len(got) != 1 || got[0] != "ab" {
		t.Fatalf("only the settled query searches, got %v", got)
	}
	before := m.pick.Total()
	m, _ = m.Update(GrepChunkMsg{Seq: m.grep.seq - 1, Hits: []GrepHit{{Path: "stale.go", Line: 1}}})
	if m.pick.Total() != before {
		t.Error("chunks from an older search are dropped")
	}

	blocking := &fakeGrep{block: true, started: make(chan context.Context, 4)}
	runGrep = blocking.run
	m, _ = m.Update(grepDueMsg{seq: m.grep.qseq})
	first := <-blocking.started
	m, _ = m.Update(keyMsg("c"))
	m, _ = m.Update(grepDueMsg{seq: m.grep.qseq})
	second := <-blocking.started
	select {
	case <-first.Done():
	case <-time.After(time.Second):
		t.Fatal("a new search cancels the previous one")
	}
	m, _ = m.Update(keyMsg("esc"))
	select {
	case <-second.Done():
	case <-time.After(time.Second):
		t.Fatal("closing the picker cancels the running search")
	}
}

func TestTogglesRegexCaseGlob(t *testing.T) {
	f := &fakeGrep{}
	useGrep(t, f)
	dir := fixture(t, map[string]int{"a.go": 5})
	m := started(t, dir, 120, 30)
	m, _ = press(t, m, append([]string{"ctrl+f"}, typeKeys("foo *.go")...)...)
	last := func() grepRequest {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.reqs[len(f.reqs)-1]
	}
	if r := last(); r.Regex || r.Case || r.Glob != "" || r.Pattern != "foo *.go" {
		t.Fatalf("defaults are literal, smart-case, no glob: %+v", r)
	}
	m, _ = press(t, m, "alt+r")
	if r := last(); !r.Regex {
		t.Errorf("alt+r turns on regex and searches again: %+v", r)
	}
	m, _ = press(t, m, "alt+c")
	if r := last(); !r.Case || !r.Regex {
		t.Errorf("alt+c turns on case sensitivity: %+v", r)
	}
	m, _ = press(t, m, "alt+g")
	if r := last(); r.Pattern != "foo" || r.Glob != "*.go" {
		t.Errorf("alt+g reads the last word as a file glob: %+v", r)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "regex") || !strings.Contains(v, "case") || !strings.Contains(v, "glob") {
		t.Errorf("the picker shows the active toggles:\n%s", v)
	}
	if !m.pick.Active() || m.pick.Query() != "foo *.go" {
		t.Error("toggles keep the picker open with its query")
	}
}

func TestGrepRealEngineFindsText(t *testing.T) {
	dir := fixture(t, map[string]int{"a.go": 3})
	var hits []GrepHit
	engine, err := defaultRunGrep(context.Background(), grepRequest{Root: dir, Pattern: "line 2"}, func(h GrepHit) bool {
		hits = append(hits, h)
		return true
	})
	if err != nil || len(hits) != 1 || hits[0].Path != "a.go" || hits[0].Line != 3 {
		t.Errorf("%s found %+v (err %v)", engine, hits, err)
	}
	hits = nil
	if err := walkGrep(context.Background(), grepRequest{Root: dir, Pattern: "LINE 2"}, func(h GrepHit) bool {
		hits = append(hits, h)
		return true
	}); err != nil || len(hits) != 0 {
		t.Errorf("the walk fallback is smart-case: %+v %v", hits, err)
	}
}
