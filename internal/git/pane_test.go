package git

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/kloppers-labs/deck/internal/editor"
	"github.com/kloppers-labs/deck/internal/ui"
)

func keys(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}
	case "ctrl+s":
		return tea.KeyMsg{Type: tea.KeyCtrlS}
	case "ctrl+g":
		return tea.KeyMsg{Type: tea.KeyCtrlG}
	case "ctrl+d":
		return tea.KeyMsg{Type: tea.KeyCtrlD}
	}
	if rest, ok := strings.CutPrefix(s, "alt+"); ok {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(rest), Alt: true}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func press(p Pane, ks ...string) (Pane, tea.Cmd) {
	var cmd tea.Cmd
	for _, k := range ks {
		p, cmd = p.Update(keys(k))
	}
	return p, cmd
}

func runAll(cmds []tea.Cmd) []tea.Msg {
	var (
		mu   sync.Mutex
		wg   sync.WaitGroup
		msgs []tea.Msg
	)
	for _, c := range cmds {
		if c == nil {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			msg := c()
			if batch, ok := msg.(tea.BatchMsg); ok {
				sub := runAll(batch)
				mu.Lock()
				msgs = append(msgs, sub...)
				mu.Unlock()
				return
			}
			mu.Lock()
			msgs = append(msgs, msg)
			mu.Unlock()
		}()
	}
	wg.Wait()
	return msgs
}

func collect(cmd tea.Cmd) []tea.Msg { return runAll([]tea.Cmd{cmd}) }

func settle(t *testing.T, p Pane, cmd tea.Cmd) Pane {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for round := 0; len(queue) > 0; round++ {
		if round > 50 {
			t.Fatal("settle: commands never quiesced")
		}
		msgs := runAll(queue)
		queue = nil
		for _, msg := range msgs {
			r, ok := msg.(ui.Routed)
			if !ok || r.RouteTo() != ui.DestGit {
				continue
			}
			var next tea.Cmd
			p, next = p.Update(msg)
			queue = append(queue, next)
		}
	}
	return p
}

func newTestPane(opts Options) Pane {
	if opts.Theme == "" {
		opts.Theme = "dracula"
	}
	p := NewPane(opts)
	p.SetSize(120, 30)
	return p
}

func loaded(t *testing.T, p Pane, snap Snapshot) Pane {
	t.Helper()
	p, _ = p.Update(RefreshMsg{})
	p, _ = p.Update(StatusLoadedMsg{Seq: p.statusSeq, Repo: &Repo{Top: "/repo"}, Snapshot: snap, IndexStamp: 1})
	return p
}

func entries(paths ...string) []Entry {
	var out []Entry
	for _, path := range paths {
		out = append(out, Entry{Kind: '1', X: '.', Y: 'M', Path: path})
	}
	return out
}

func manyEntries(n int, prefix string) []Entry {
	var paths []string
	for i := range n {
		paths = append(paths, fmt.Sprintf("%s%03d.go", prefix, i))
	}
	return entries(paths...)
}

func longDiff(n int) string {
	var b strings.Builder
	b.WriteString("@@ -1,1 +1,200 @@\n")
	for i := range n {
		fmt.Fprintf(&b, "+line %d\n", i)
	}
	return b.String()
}

func withDiff(p Pane, text string) Pane {
	it, _ := p.selected()
	p, _ = p.Update(DiffLoadedMsg{Seq: p.diffSeq, Key: p.keyFor(it), Text: text})
	return p
}

func TestNewPaneRunsNoGit(t *testing.T) {
	before := execCount.Load()
	p := newTestPane(Options{CommitAI: true})
	_ = p.View()
	p, cmd := p.Update(RefreshMsg{})
	if cmd == nil {
		t.Fatal("a refresh should return a load command")
	}
	_ = p.View()
	if got := execCount.Load(); got != before {
		t.Errorf("building, viewing and refreshing the pane ran git %d times synchronously", got-before)
	}
}

func TestReloadReturnsCmdAndKeepsState(t *testing.T) {
	p := loaded(t, newTestPane(Options{}), Snapshot{Branch: "main", Entries: entries("a.go", "b.go")})
	p, cmd := p.Update(RefreshMsg{})
	if cmd == nil {
		t.Fatal("reload should be a command")
	}
	if !p.loading {
		t.Error("the pane should show it is loading")
	}
	if len(p.items) != 2 || !strings.Contains(ansi.Strip(p.View()), "b.go") {
		t.Error("reload must keep showing the last known state")
	}
}

func TestStaleStatusSeqDropped(t *testing.T) {
	p := newTestPane(Options{})
	p, _ = p.Update(RefreshMsg{})
	stale := p.statusSeq
	p, _ = p.Update(RefreshMsg{})
	p, _ = p.Update(StatusLoadedMsg{Seq: stale, Repo: &Repo{Top: "/r"}, Snapshot: Snapshot{Entries: entries("old.go")}})
	if len(p.items) != 0 {
		t.Fatalf("a stale status was applied: %+v", p.items)
	}
	p, _ = p.Update(StatusLoadedMsg{Seq: p.statusSeq, Repo: &Repo{Top: "/r"}, Snapshot: Snapshot{Entries: entries("new.go")}})
	if len(p.items) != 1 || p.items[0].Entry.Path != "new.go" || p.loading {
		t.Errorf("the current status was not applied: %+v", p.items)
	}
}

func TestNotARepoMessage(t *testing.T) {
	p := newTestPane(Options{})
	p, _ = p.Update(RefreshMsg{})
	p, _ = p.Update(StatusLoadedMsg{Seq: p.statusSeq, Err: ErrNotRepo})
	if !strings.Contains(ansi.Strip(p.View()), "not a git repository") {
		t.Error("the pane should say it is not in a repository")
	}
}

func TestMMInBothSections(t *testing.T) {
	p := loaded(t, newTestPane(Options{}), Snapshot{Entries: []Entry{
		{Kind: '1', X: 'M', Y: 'M', Path: "both.go"},
		{Kind: '?', X: '?', Y: '?', Path: "new.go"},
		{Kind: 'u', X: 'U', Y: 'U', Path: "clash.go"},
	}})
	v := ansi.Strip(p.View())
	for _, want := range []string{"Staged (1)", "Unstaged (1)", "Untracked (1)", "Conflicts (1)"} {
		if !strings.Contains(v, want) {
			t.Errorf("view is missing section %q:\n%s", want, v)
		}
	}
	if n := strings.Count(v, "both.go"); n != 2 {
		t.Errorf("an MM file should appear in both sections, found %d times", n)
	}
}

func TestSpaceOnUnstagedEntryStages(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, "a.txt", "a\n")
	writeFile(t, dir, "b.txt", "b\n")
	gitT(t, dir, "add", ".")
	gitT(t, dir, "commit", "-qm", "init")
	writeFile(t, dir, "a.txt", "a2\n")
	writeFile(t, dir, "b.txt", "b2\n")
	p := newTestPane(Options{Dir: dir})
	p, cmd := p.Update(RefreshMsg{})
	p = settle(t, p, cmd)
	if len(p.items) != 2 || p.items[0].Section != SectionUnstaged {
		t.Fatalf("items = %+v", p.items)
	}
	p, cmd = press(p, "space")
	p = settle(t, p, cmd)
	var got []string
	for _, it := range p.items {
		got = append(got, it.Section.String()+":"+it.Entry.Path)
	}
	if !slices.Equal(got, []string{"Staged:a.txt", "Unstaged:b.txt"}) {
		t.Fatalf("after space, items = %v", got)
	}
	if it, _ := p.selected(); it.Section != SectionUnstaged || it.Entry.Path != "b.txt" {
		t.Errorf("after staging, the cursor should stay in Unstaged on the next entry, got %+v", it)
	}
	p, cmd = press(p, "a")
	p = settle(t, p, cmd)
	if len(p.items) != 2 || p.items[0].Section != SectionStaged || p.items[1].Section != SectionStaged {
		t.Fatalf("after a, items = %+v", p.items)
	}
	p, cmd = press(p, "A")
	p = settle(t, p, cmd)
	if len(p.items) != 2 || p.items[0].Section != SectionUnstaged || p.items[1].Section != SectionUnstaged {
		t.Fatalf("after A, items = %+v", p.items)
	}
}

func TestDiffYOffsetZeroAfterJAndSpace(t *testing.T) {
	p := loaded(t, newTestPane(Options{}), Snapshot{Entries: entries("a.go", "b.go")})
	p = withDiff(p, longDiff(200))
	p, _ = press(p, "j")
	p = withDiff(p, longDiff(200))
	p, _ = press(p, "space")
	if p.diff.YOffset != 0 {
		t.Errorf("list keys scrolled the diff to %d", p.diff.YOffset)
	}
	if p.cursor != 1 {
		t.Errorf("cursor = %d, want 1", p.cursor)
	}
}

func TestDiffFocusScrollsOnly(t *testing.T) {
	p := loaded(t, newTestPane(Options{}), Snapshot{Entries: entries("a.go", "b.go")})
	p = withDiff(p, longDiff(200))
	p, _ = press(p, "enter")
	if p.focus != focusDiff {
		t.Fatal("enter should focus the diff")
	}
	p, _ = press(p, "j", "j")
	if p.diff.YOffset != 2 || p.cursor != 0 {
		t.Errorf("in the diff, j should scroll only: offset %d cursor %d", p.diff.YOffset, p.cursor)
	}
	p, _ = press(p, "G")
	if p.diff.YOffset == 2 {
		t.Error("G should jump to the bottom of the diff")
	}
	p, _ = press(p, "g")
	if p.diff.YOffset != 0 {
		t.Error("g should jump to the top of the diff")
	}
	p, _ = press(p, "esc")
	if p.focus != focusList {
		t.Error("esc should return to the list")
	}
	p, _ = press(p, "enter", "h")
	if p.focus != focusList {
		t.Error("h should return to the list")
	}
}

func TestLogModeJMovesLogCursor(t *testing.T) {
	p := loaded(t, newTestPane(Options{}), Snapshot{Entries: entries("a.go", "b.go")})
	p = withDiff(p, longDiff(200))
	p, cmd := press(p, "l")
	if p.mode != modeLog || cmd == nil {
		t.Fatal("l should enter log mode and load the log")
	}
	p, _ = p.Update(logLoadedMsg{entries: []LogEntry{{Hash: "a1", Subject: "one"}, {Hash: "b2", Subject: "two"}, {Hash: "c3", Subject: "three"}}})
	p, _ = press(p, "j")
	if p.logList.Index() != 1 {
		t.Errorf("log cursor = %d, want 1", p.logList.Index())
	}
	if p.cursor != 0 || p.diff.YOffset != 0 {
		t.Errorf("j in log mode moved the change cursor (%d) or the diff (%d)", p.cursor, p.diff.YOffset)
	}
	p, _ = press(p, "esc")
	if p.mode != modeChanges {
		t.Error("esc should leave log mode")
	}
}

func TestLogListEnterShowsCommit(t *testing.T) {
	p := loaded(t, newTestPane(Options{}), Snapshot{})
	p, _ = press(p, "l")
	p, _ = p.Update(logLoadedMsg{entries: []LogEntry{{Hash: "a1", Subject: "one"}, {Hash: "b2", Subject: "two"}}})
	p, _ = press(p, "j")
	p, cmd := press(p, "enter")
	if cmd == nil || p.focus != focusDiff {
		t.Fatal("enter on a commit should load it and focus the diff")
	}
	p, _ = p.Update(showLoadedMsg{hash: "b2", text: "commit b2\n\n    two\n\n file | 1 +\n"})
	if v := ansi.Strip(p.View()); !strings.Contains(v, "commit b2") {
		t.Errorf("the diff pane should show the commit:\n%s", v)
	}
	p, _ = press(p, "esc")
	if p.mode != modeLog || p.focus != focusList {
		t.Error("esc from the commit should return to the log list")
	}
}

func TestWindowedListKeepsCursorVisible(t *testing.T) {
	p := loaded(t, newTestPane(Options{}), Snapshot{Entries: manyEntries(60, "f-")})
	p.SetSize(100, 14)
	for range 40 {
		p, _ = press(p, "j")
	}
	if p.cursor != 40 {
		t.Fatalf("cursor = %d, want 40", p.cursor)
	}
	if v := ansi.Strip(p.View()); !strings.Contains(v, "f-040.go") {
		t.Errorf("the cursor row scrolled out of view:\n%s", v)
	}
	for range 40 {
		p, _ = press(p, "k")
	}
	v := ansi.Strip(p.View())
	if !strings.Contains(v, "f-000.go") || !strings.Contains(v, "Unstaged (60)") {
		t.Errorf("moving back to the top should show the section header and first row:\n%s", v)
	}
}

func TestViewFits(t *testing.T) {
	long := strings.Repeat("very-long-directory-name/", 5) + "file.cs"
	for _, size := range [][2]int{{60, 11}, {80, 19}, {120, 35}, {200, 45}} {
		modes := map[string]func(Pane) Pane{
			"list":    func(p Pane) Pane { return p },
			"diff":    func(p Pane) Pane { p, _ = press(p, "enter"); return p },
			"commit":  func(p Pane) Pane { p, _ = press(p, "c"); return p },
			"confirm": func(p Pane) Pane { p, _ = press(p, "c"); p.box.SetValue("x"); p, _ = press(p, "ctrl+s"); return p },
			"branch": func(p Pane) Pane {
				p, _ = press(p, "b")
				p, _ = p.Update(branchesLoadedMsg{names: []string{"main", long}})
				return p
			},
			"log": func(p Pane) Pane {
				p, _ = press(p, "l")
				p, _ = p.Update(logLoadedMsg{entries: []LogEntry{{Hash: "abc", Age: "2 days ago", Author: long, Subject: long, Refs: long}}})
				return p
			},
			"discard": func(p Pane) Pane { p, _ = press(p, "X"); return p },
			"busy":    func(p Pane) Pane { p, _ = press(p, "f"); return p },
		}
		for name, mode := range modes {
			p := loaded(t, newTestPane(Options{ConfirmCommit: true}), Snapshot{
				Branch: long, Upstream: "origin/" + long, Ahead: 3,
				Entries: append(manyEntries(80, long), Entry{Kind: '2', X: 'R', Y: 'M', Path: long + ".new", Orig: long}),
			})
			p.status = strings.Repeat("a very long status message ", 10)
			p.SetSize(size[0], size[1])
			p = withDiff(p, longDiff(300))
			p = mode(p)
			v := p.View()
			if got := lipgloss.Height(v); got != size[1] {
				t.Errorf("%v %s: height = %d, want %d", size, name, got, size[1])
			}
			if got := lipgloss.Width(v); got > size[0] {
				t.Errorf("%v %s: width = %d, want <= %d", size, name, got, size[0])
			}
		}
	}
}

func TestDiffDebounced(t *testing.T) {
	p := loaded(t, newTestPane(Options{}), Snapshot{Entries: entries("a.go", "b.go", "c.go")})
	p, first := press(p, "j")
	p, second := press(p, "j")
	if first == nil || second == nil {
		t.Fatal("moving should schedule a diff load")
	}
	msgs := collect(first)
	if len(msgs) != 1 {
		t.Fatalf("first move produced %v", msgs)
	}
	due, ok := msgs[0].(diffDueMsg)
	if !ok {
		t.Fatalf("a move should only schedule a debounced load, got %T", msgs[0])
	}
	if _, cmd := p.Update(due); cmd != nil {
		t.Error("a superseded debounce must not load a diff")
	}
	latest := collect(second)[0].(diffDueMsg)
	if _, cmd := p.Update(latest); cmd == nil {
		t.Error("the latest debounce should load the diff")
	}
}

func TestDiffCachedPerIndexMtime(t *testing.T) {
	snap := Snapshot{Entries: entries("a.go", "b.go")}
	p := loaded(t, newTestPane(Options{}), snap)
	p = withDiff(p, "@@ -1 +1 @@\n+cached A\n")
	p, cmd := press(p, "j")
	p, cmd = p.Update(collect(cmd)[0])
	if cmd == nil {
		t.Fatal("an uncached diff should load")
	}
	p, cmd = press(p, "k")
	p, cmd = p.Update(collect(cmd)[0])
	if cmd != nil {
		t.Error("returning to a cached entry should not reload its diff")
	}
	if !strings.Contains(ansi.Strip(p.diff.View()), "cached A") {
		t.Error("the cached diff should be shown")
	}
	p, _ = p.Update(RefreshMsg{})
	if _, cmd = p.Update(StatusLoadedMsg{Seq: p.statusSeq, Repo: &Repo{Top: "/repo"}, Snapshot: snap, IndexStamp: 1}); cmd != nil {
		t.Error("an unchanged index should keep the cached diff")
	}
	p, _ = p.Update(RefreshMsg{})
	if _, cmd = p.Update(StatusLoadedMsg{Seq: p.statusSeq, Repo: &Repo{Top: "/repo"}, Snapshot: snap, IndexStamp: 2}); cmd == nil {
		t.Error("a new index mtime should invalidate the cached diff")
	}
}

func TestDiffCappedAt200KB(t *testing.T) {
	var b strings.Builder
	for i := 0; b.Len() < 300*1024; i++ {
		fmt.Fprintf(&b, "+%s %d\n", strings.Repeat("x", 90), i)
	}
	full := b.String()
	got := capDiff(full, diffCap)
	if len(got) > diffCap+200 {
		t.Errorf("capped diff is %d bytes", len(got))
	}
	total := strings.Count(full, "\n")
	shown := strings.Count(got, "\n")
	want := fmt.Sprintf("… %d more lines · e open", total-shown)
	if !strings.HasSuffix(got, want) {
		t.Errorf("capped diff should end with %q, ends with %q", want, got[len(got)-60:])
	}
	if small := "+a\n+b\n"; capDiff(small, diffCap) != small {
		t.Error("a small diff should be untouched")
	}
}

func committingPane(t *testing.T, opts Options) Pane {
	t.Helper()
	p := loaded(t, newTestPane(opts), Snapshot{Branch: "main", Entries: []Entry{{Kind: '1', X: 'A', Y: '.', Path: "a.go"}}})
	p, _ = press(p, "c")
	if !p.committing {
		t.Fatal("c should open the commit box")
	}
	return p
}

func TestTextareaSubjectCounterRedOver72(t *testing.T) {
	if n, over := subjectLen(strings.Repeat("a", 72) + "\nbody that is very long " + strings.Repeat("b", 100)); n != 72 || over {
		t.Errorf("72-char subject = %d, over %v", n, over)
	}
	if n, over := subjectLen(strings.Repeat("é", 73)); n != 73 || !over {
		t.Errorf("73-char subject = %d, over %v", n, over)
	}
	p := committingPane(t, Options{CommitAI: true})
	p.box.SetValue(strings.Repeat("a", 73))
	if v := ansi.Strip(p.View()); !strings.Contains(v, "73/72") {
		t.Errorf("the commit box should show the subject counter:\n%s", v)
	}
}

func TestCtrlSCommits(t *testing.T) {
	p := committingPane(t, Options{})
	p, cmd := press(p, "ctrl+s")
	if cmd != nil || p.status == "" {
		t.Error("ctrl+s on an empty box should only report it")
	}
	p.box.SetValue("feat: x\n\nbody")
	p, cmd = press(p, "ctrl+s")
	if cmd == nil || !strings.Contains(p.busy, "commit") {
		t.Fatalf("ctrl+s should start an async commit, busy %q", p.busy)
	}
	p, _ = p.Update(commitDoneMsg{err: errors.New("hook failed")})
	if !p.committing || p.box.Value() != "feat: x\n\nbody" || !strings.Contains(p.status, "hook failed") {
		t.Errorf("a failed commit should keep the box and text, status %q", p.status)
	}
	p, cmd = p.Update(commitDoneMsg{subject: "feat: x"})
	if p.committing || cmd == nil || !strings.Contains(p.status, "committed") {
		t.Errorf("a successful commit should close the box and reload, status %q", p.status)
	}
}

func TestConfirmOptionalByConfig(t *testing.T) {
	p := committingPane(t, Options{ConfirmCommit: true})
	p.box.SetValue("msg")
	p, cmd := press(p, "ctrl+s")
	if !p.confirming || cmd != nil {
		t.Fatal("with confirm_commit the commit should wait for a confirm")
	}
	p, cmd = press(p, "y")
	if cmd == nil || p.busy == "" {
		t.Error("y should commit")
	}
	p = committingPane(t, Options{ConfirmCommit: false})
	p.box.SetValue("msg")
	p, cmd = press(p, "ctrl+s")
	if p.confirming || cmd == nil {
		t.Error("without confirm_commit ctrl+s should commit straight away")
	}
}

func TestAmendToggle(t *testing.T) {
	p := committingPane(t, Options{})
	p, cmd := press(p, "alt+a")
	if !p.amend || cmd == nil {
		t.Fatal("alt+a should switch to amend and load the last message")
	}
	p, _ = p.Update(headMessageMsg{text: "previous message\n"})
	if p.box.Value() != "previous message" {
		t.Errorf("amend should prefill the last message, got %q", p.box.Value())
	}
	if v := ansi.Strip(p.View()); !strings.Contains(v, "amend") {
		t.Error("the commit box should show amend mode")
	}
	p, _ = press(p, "alt+a")
	if p.amend {
		t.Error("alt+a again should turn amend off")
	}
	if got := commitArgs(true); !slices.Equal(got, []string{"commit", "--amend", "-F", "-"}) {
		t.Errorf("commitArgs(amend) = %q", got)
	}
	if got := commitArgs(false); !slices.Equal(got, []string{"commit", "-F", "-"}) {
		t.Errorf("commitArgs = %q", got)
	}
}

func TestCommitAsync(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, "a.txt", "a\n")
	gitT(t, dir, "add", ".")
	p := newTestPane(Options{Dir: dir})
	p, cmd := p.Update(RefreshMsg{})
	p = settle(t, p, cmd)
	p, _ = press(p, "c")
	p.box.SetValue("first: commit\n\nwith a body")
	p, cmd = press(p, "ctrl+s")
	if cmd == nil {
		t.Fatal("ctrl+s should return the commit command")
	}
	if out, err := runGitT(dir, "log", "--oneline"); err == nil {
		t.Fatalf("the commit ran synchronously: %s", out)
	}
	p = settle(t, p, cmd)
	if p.committing || len(p.items) != 0 {
		t.Errorf("after the commit: committing %v, items %+v, status %q", p.committing, p.items, p.status)
	}
	if body := gitT(t, dir, "log", "-1", "--format=%B"); !strings.Contains(body, "with a body") {
		t.Errorf("commit body = %q", body)
	}
}

func runGitT(dir string, args ...string) (string, error) {
	out, _, err := runGit(context.Background(), dir, args...)
	return out, err
}

func TestCtrlGKeepsTextOnError(t *testing.T) {
	p := committingPane(t, Options{CommitAI: true})
	p.box.SetValue("wip")
	p, cmd := press(p, "ctrl+g")
	var req *GenCommitMsgRequest
	for _, msg := range collect(cmd) {
		if r, ok := msg.(GenCommitMsgRequest); ok {
			req = &r
		}
	}
	if req == nil || req.Ctx == nil || req.Repo == nil {
		t.Fatalf("ctrl+g should request a message with a context and the repo, got %+v", req)
	}
	p, cmd = p.Update(CommitMsgReady{Err: errors.New("cli missing")})
	if p.box.Value() != "wip" || !strings.Contains(p.status, "cli missing") {
		t.Errorf("box %q status %q", p.box.Value(), p.status)
	}
	var toast *ui.ToastMsg
	for _, msg := range collect(cmd) {
		if tm, ok := msg.(ui.ToastMsg); ok {
			toast = &tm
		}
	}
	if toast == nil || toast.Level != ui.LevelError {
		t.Errorf("a failed commit message should raise an error toast, got %+v", toast)
	}
}

func TestCommitMsgSuccessSetsText(t *testing.T) {
	p := committingPane(t, Options{CommitAI: true})
	p.box.SetValue("wip")
	p, _ = p.Update(CommitMsgReady{Text: "  feat: add widget\n\nbody\n"})
	if got := p.box.Value(); got != "feat: add widget\n\nbody" {
		t.Errorf("commit box = %q, want the generated message", got)
	}
}

func TestGenerateShowsSpinnerUntilReady(t *testing.T) {
	p := committingPane(t, Options{CommitAI: true})
	p, _ = press(p, "ctrl+g")
	if !p.generating {
		t.Fatal("ctrl+g should start generating")
	}
	if !strings.Contains(p.View(), p.spin.View()) {
		t.Error("the commit box should show the spinner while generating")
	}
	p, _ = p.Update(CommitMsgReady{Text: "feat: x"})
	if p.generating || strings.Contains(p.View(), p.spin.View()) {
		t.Error("generating should stop once the message is ready")
	}
}

func TestEscWhileGeneratingCancels(t *testing.T) {
	p := committingPane(t, Options{CommitAI: true})
	p, cmd := press(p, "ctrl+g")
	var ctx context.Context
	for _, msg := range collect(cmd) {
		if r, ok := msg.(GenCommitMsgRequest); ok {
			ctx = r.Ctx
		}
	}
	p, _ = press(p, "esc")
	if ctx == nil || ctx.Err() == nil {
		t.Fatal("esc while generating should cancel the request context")
	}
	if p.generating || !p.committing {
		t.Error("the first esc should only cancel generation")
	}
	p.box.SetValue("wip")
	p, cmd = p.Update(CommitMsgReady{Err: context.Canceled})
	if p.box.Value() != "wip" || cmd != nil {
		t.Error("a cancelled reply must not touch the box or raise a toast")
	}
	p, _ = press(p, "esc")
	if p.committing {
		t.Error("the second esc should close the commit box")
	}
}

func TestCommitAIHintHiddenWhenUnavailable(t *testing.T) {
	hasHint := func(p Pane) bool {
		for _, b := range p.HelpBindings() {
			if b.Help().Key == "ctrl+g" {
				return true
			}
		}
		return false
	}
	if !hasHint(committingPane(t, Options{CommitAI: true})) {
		t.Error("ctrl+g hint should show when the AI CLI is available")
	}
	off := committingPane(t, Options{})
	if hasHint(off) || strings.Contains(off.box.Placeholder, "ctrl+g") {
		t.Error("ctrl+g should not be advertised when the AI CLI is missing")
	}
}

func TestFetchPullPushAsyncStatusLine(t *testing.T) {
	for _, c := range []struct{ key, busy, label string }{
		{"f", "fetching", "fetch"}, {"p", "pulling", "pull"}, {"P", "pushing", "push"},
	} {
		p := loaded(t, newTestPane(Options{}), Snapshot{Branch: "main"})
		p, cmd := press(p, c.key)
		if cmd == nil {
			t.Fatalf("%s should return a command", c.key)
		}
		if !strings.Contains(ansi.Strip(p.View()), c.busy) {
			t.Errorf("%s: the status line should say %q while running", c.key, c.busy)
		}
		if _, again := press(p, c.key); again != nil {
			t.Errorf("%s: a second remote op should not start while one is running", c.key)
		}
		done, cmd := p.Update(opDoneMsg{label: c.label})
		if done.busy != "" || cmd == nil || !strings.Contains(done.status, c.label) {
			t.Errorf("%s: done should clear busy and reload, status %q", c.key, done.status)
		}
		failed, cmd := p.Update(opDoneMsg{label: c.label, err: errors.New("rejected")})
		if !strings.Contains(failed.status, "rejected") || cmd == nil {
			t.Errorf("%s: a failure should be reported, status %q", c.key, failed.status)
		}
	}
}

func TestBranchPickerCursorOnCurrent(t *testing.T) {
	p := loaded(t, newTestPane(Options{}), Snapshot{Branch: "b"})
	p, cmd := press(p, "b")
	if !p.branching || cmd == nil || !p.Capturing() {
		t.Fatal("b should open the branch picker and load branches")
	}
	p, _ = p.Update(branchesLoadedMsg{names: []string{"a", "b", "c"}})
	if p.branchList.Index() != 1 {
		t.Errorf("branch cursor = %d, want the current branch", p.branchList.Index())
	}
	p, cmd = press(p, "j", "enter")
	if p.branching || cmd == nil || !strings.Contains(p.busy, "switching") {
		t.Errorf("enter should switch async: branching %v busy %q", p.branching, p.busy)
	}
}

func TestBranchCreateAndDeleteKeys(t *testing.T) {
	p := loaded(t, newTestPane(Options{}), Snapshot{Branch: "main"})
	p, _ = press(p, "b")
	p, _ = p.Update(branchesLoadedMsg{names: []string{"main", "old"}})
	p, _ = press(p, "n")
	if !p.naming {
		t.Fatal("n should prompt for a branch name")
	}
	p, _ = press(p, "f", "e", "a", "t")
	p, cmd := press(p, "enter")
	if p.naming || p.branching || cmd == nil || !strings.Contains(p.busy, "feat") {
		t.Errorf("enter should create the branch async, busy %q", p.busy)
	}
	p, _ = p.Update(opDoneMsg{label: "create feat"})
	p, _ = press(p, "b")
	p, _ = p.Update(branchesLoadedMsg{names: []string{"main", "old"}})
	p, _ = press(p, "j", "D")
	if p.pending == nil || !strings.Contains(ansi.Strip(p.View()), "old") {
		t.Fatal("D should ask before deleting")
	}
	p, cmd = press(p, "y")
	if p.pending != nil || cmd == nil {
		t.Error("y should delete the branch")
	}
}

func TestDiscardAsksFirst(t *testing.T) {
	p := loaded(t, newTestPane(Options{}), Snapshot{Entries: entries("a.go")})
	p, cmd := press(p, "X")
	if p.pending == nil || cmd != nil || !p.Capturing() {
		t.Fatal("X should ask for confirmation first")
	}
	if !strings.Contains(ansi.Strip(p.View()), "discard") {
		t.Error("the confirm prompt should be visible")
	}
	p, cmd = press(p, "n")
	if p.pending != nil || cmd != nil {
		t.Error("n should cancel without running anything")
	}
	p, _ = press(p, "X")
	p, cmd = press(p, "y")
	if p.pending != nil || cmd == nil {
		t.Error("y should discard")
	}
}

func TestEOpensEditorAtFirstHunk(t *testing.T) {
	p := loaded(t, newTestPane(Options{}), Snapshot{Entries: entries("src/a.go")})
	p = withDiff(p, "@@ -10,3 +12,4 @@\n ctx\n+new\n")
	_, cmd := press(p, "e")
	msgs := collect(cmd)
	if len(msgs) != 1 {
		t.Fatalf("e produced %v", msgs)
	}
	got, ok := msgs[0].(editor.OpenMsg)
	if !ok || got.Target.Path != filepath.Join("/repo", "src/a.go") || got.Target.Line != 13 {
		t.Errorf("e = %#v, want the file at line 13", msgs[0])
	}
}

func TestMouseClickFocusesDiffAndSelectsRows(t *testing.T) {
	p := loaded(t, newTestPane(Options{}), Snapshot{Entries: entries("a.go", "b.go", "c.go")})
	p, _ = p.Update(tea.MouseMsg{X: p.width - 5, Y: 5, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if p.focus != focusDiff {
		t.Error("a click on the diff should focus it")
	}
	p, _ = p.Update(tea.MouseMsg{X: 2, Y: p.listTop() + 3, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if p.focus != focusList || p.cursor != 2 {
		t.Errorf("a click on a row should select it: focus %v cursor %d", p.focus, p.cursor)
	}
	p, _ = p.Update(tea.MouseMsg{X: 2, Y: 5, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp})
	if p.cursor != 1 {
		t.Errorf("wheel up over the list should move the cursor, got %d", p.cursor)
	}
}

func TestMessagesRouteToGit(t *testing.T) {
	for _, msg := range []ui.Routed{RefreshMsg{}, CommitMsgReady{}, StatusLoadedMsg{}, DiffLoadedMsg{}, diffDueMsg{}, opDoneMsg{}} {
		if msg.RouteTo() != ui.DestGit {
			t.Errorf("%T routes to %v, want DestGit", msg, msg.RouteTo())
		}
	}
	if b, ok := any(StatusLoadedMsg{}).(ui.Broadcast); !ok || !slices.Contains(b.AlsoTo(), ui.DestFiles) {
		t.Error("StatusLoadedMsg should also reach the Files tab")
	}
}

func TestRefreshMsgReloads(t *testing.T) {
	p := newTestPane(Options{})
	before := p.Reloads()
	p, _ = p.Update(Refresh())
	if p.Reloads() != before+1 {
		t.Errorf("Reloads = %d, want %d", p.Reloads(), before+1)
	}
}

func TestBranchStatusAndTop(t *testing.T) {
	p := loaded(t, newTestPane(Options{}), Snapshot{Branch: "main", Upstream: "origin/main", Ahead: 2, Behind: 1, Entries: entries("a", "b")})
	if p.Branch() != "main ↑2 ↓1" || p.DirtyCount() != 2 || p.Top() != "/repo" {
		t.Errorf("Branch %q Dirty %d Top %q", p.Branch(), p.DirtyCount(), p.Top())
	}
}
