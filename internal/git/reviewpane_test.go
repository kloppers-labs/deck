package git

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/kloppers-labs/deck/internal/editor"
)

func reviewPane(t *testing.T, dir string) Pane {
	t.Helper()
	p := newTestPane(Options{Dir: dir})
	p.SetSize(240, 40)
	p = settle(t, p, func() tea.Msg { return RefreshMsg{} })
	p, cmd := press(p, "v")
	return settle(t, p, cmd)
}

func plainView(p Pane) string { return ansi.Strip(p.View()) }

func mergeBase(t *testing.T, dir string) string {
	t.Helper()
	return strings.TrimSpace(gitT(t, dir, "merge-base", "main", "HEAD"))
}

func selectedReviewFile(p Pane) string {
	if f, ok := p.reviewFile(); ok {
		return f.Path
	}
	return ""
}

func selectFile(t *testing.T, p Pane, path string) Pane {
	t.Helper()
	for range 20 {
		if selectedReviewFile(p) == path {
			return p
		}
		var cmd tea.Cmd
		p, cmd = press(p, "j")
		p = settle(t, p, cmd)
	}
	t.Fatalf("never reached %s", path)
	return p
}

func TestVEntersReviewAndBack(t *testing.T) {
	dir := reviewRepo(t)
	p := reviewPane(t, dir)
	if p.mode != modeReview || !strings.Contains(plainView(p), "Review · vs main") {
		t.Fatalf("v should open the review:\n%s", plainView(p))
	}
	p, _ = press(p, "v")
	if p.mode != modeChanges {
		t.Error("v again should return to changes")
	}
	p = reviewPane(t, dir)
	p, _ = press(p, "esc")
	if p.mode != modeChanges {
		t.Error("esc in the review list should return to changes")
	}
}

func TestReviewHeaderText(t *testing.T) {
	dir := reviewRepo(t)
	p := reviewPane(t, dir)
	want := fmt.Sprintf("Review · vs main @ %s · 2 commits · 6 files +8 −1 · 0/6 viewed", mergeBase(t, dir)[:7])
	if got := strings.Split(plainView(p), "\n")[0]; !strings.Contains(got, want) {
		t.Errorf("header = %q, want %q", got, want)
	}
}

func TestReviewMsgSelectsPath(t *testing.T) {
	dir := reviewRepo(t)
	p := newTestPane(Options{Dir: dir})
	p, cmd := p.Update(ReviewMsg{Path: filepath.Join(dir, "keep.txt"), Line: 2})
	p = settle(t, p, cmd)
	if p.mode != modeReview || selectedReviewFile(p) != "keep.txt" || p.focus != focusDiff {
		t.Fatalf("mode %d, selected %q, focus %d", p.mode, selectedReviewFile(p), p.focus)
	}
	if !strings.Contains(ansi.Strip(p.diff.View()), "+more") {
		t.Errorf("the selected file's diff should show:\n%s", p.diff.View())
	}
}

func TestReviewMsgBaseAndUncommitted(t *testing.T) {
	dir := reviewRepo(t)
	p := newTestPane(Options{Dir: dir})
	p, cmd := p.Update(ReviewMsg{Uncommitted: true})
	p = settle(t, p, cmd)
	if !p.review.rv.Uncommitted || strings.Contains(plainView(p), "feature.txt") {
		t.Errorf("ReviewMsg{Uncommitted} should narrow to uncommitted work:\n%s", plainView(p))
	}
}

func TestSpaceTogglesViewedAndPersists(t *testing.T) {
	dir := reviewRepo(t)
	p := selectFile(t, reviewPane(t, dir), "keep.txt")
	p, cmd := press(p, "space")
	p = settle(t, p, cmd)
	if !strings.Contains(plainView(p), "1/6 viewed") {
		t.Fatalf("space should tick the file:\n%s", plainView(p))
	}
	if again := reviewPane(t, dir); !strings.Contains(plainView(again), "1/6 viewed") {
		t.Errorf("ticks should persist:\n%s", plainView(again))
	}
	p, cmd = press(p, "space")
	p = settle(t, p, cmd)
	if !strings.Contains(plainView(p), "0/6 viewed") {
		t.Error("space again should untick")
	}
}

func TestNNextUnviewed(t *testing.T) {
	dir := reviewRepo(t)
	p := reviewPane(t, dir)
	first := selectedReviewFile(p)
	p, cmd := press(p, "space")
	p = settle(t, p, cmd)
	p, cmd = press(p, "n")
	p = settle(t, p, cmd)
	if got := selectedReviewFile(p); got == first || got == "" {
		t.Errorf("n went to %q, want the next unviewed file after %q", got, first)
	}
}

func TestUToggleUncommitted(t *testing.T) {
	dir := reviewRepo(t)
	p := reviewPane(t, dir)
	p, cmd := press(p, "u")
	p = settle(t, p, cmd)
	view := plainView(p)
	if !strings.Contains(view, "uncommitted") || strings.Contains(view, "feature.txt") || !strings.Contains(view, "unstaged.txt") {
		t.Errorf("u should show only uncommitted changes:\n%s", view)
	}
}

func TestReviewRefreshKeepsCursorPath(t *testing.T) {
	dir := reviewRepo(t)
	p := selectFile(t, reviewPane(t, dir), "unstaged.txt")
	writeFile(t, dir, "aaa-first.txt", "new\n")
	p = settle(t, p, func() tea.Msg { return RefreshMsg{} })
	if got := selectedReviewFile(p); got != "unstaged.txt" {
		t.Errorf("after a refresh the cursor is on %q, want unstaged.txt", got)
	}
	if !strings.Contains(plainView(p), "aaa-first.txt") {
		t.Error("a refresh should pick up the new file")
	}
}

func TestReviewCommitsToggle(t *testing.T) {
	dir := reviewRepo(t)
	p := reviewPane(t, dir)
	if strings.Contains(plainView(p), "feature two") {
		t.Fatal("commits start collapsed")
	}
	for range 3 {
		p, _ = press(p, "k")
	}
	p, cmd := press(p, "enter")
	p = settle(t, p, cmd)
	if !strings.Contains(plainView(p), "feature two") {
		t.Fatalf("enter on the commits row should expand them:\n%s", plainView(p))
	}
	p, cmd = press(p, "j", "enter")
	p = settle(t, p, cmd)
	if !strings.Contains(ansi.Strip(p.diff.View()), "feature two") {
		t.Errorf("enter on a commit should show it:\n%s", p.diff.View())
	}
}

func TestReviewFallbackLine(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, "a.txt", "a\n")
	commitAll(t, dir, "init")
	writeFile(t, dir, "a.txt", "b\n")
	p := reviewPane(t, dir)
	if !strings.Contains(plainView(p), "no commits ahead of main; showing uncommitted changes") {
		t.Errorf("the fallback should be explained:\n%s", plainView(p))
	}
}

func TestReviewEditOpensFile(t *testing.T) {
	dir := reviewRepo(t)
	p := selectFile(t, reviewPane(t, dir), "keep.txt")
	_, cmd := press(p, "e")
	var opened editor.Target
	for _, msg := range collect(cmd) {
		if o, ok := msg.(editor.OpenMsg); ok {
			opened = o.Target
		}
	}
	repo, _ := OpenRepo(context.Background(), dir)
	if opened.Path != filepath.Join(repo.Top, "keep.txt") || opened.Line != 2 {
		t.Errorf("e opened %+v, want keep.txt at its first changed line 2", opened)
	}
}

func TestReviewMsgUnchangedPath(t *testing.T) {
	dir := reviewRepo(t)
	p := newTestPane(Options{Dir: dir})
	p.SetSize(240, 40)
	p, cmd := p.Update(ReviewMsg{Path: filepath.Join(dir, "does-not-change.txt"), Line: 2})
	p = settle(t, p, cmd)
	if p.focus != focusList {
		t.Error("a path with no changes should leave focus on the list, not another file's diff")
	}
	if p.review.pendingLine != 0 {
		t.Errorf("pendingLine = %d, want it dropped", p.review.pendingLine)
	}
	if !strings.Contains(plainView(p), "does-not-change.txt has no changes vs main") {
		t.Errorf("the status should explain:\n%s", plainView(p))
	}
}
