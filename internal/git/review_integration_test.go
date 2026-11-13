package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func commitAll(t *testing.T, dir, msg string) {
	t.Helper()
	gitT(t, dir, "add", "-A")
	gitT(t, dir, "commit", "-qm", msg)
}

func reviewRepo(t *testing.T) string {
	t.Helper()
	dir := newRepo(t)
	writeFile(t, dir, "keep.txt", "keep\n")
	writeFile(t, dir, "staged.txt", "one\n")
	writeFile(t, dir, "unstaged.txt", "one\n")
	writeFile(t, dir, "old.txt", strings.Repeat("rename me please\n", 20))
	commitAll(t, dir, "base")
	gitT(t, dir, "switch", "-qc", "feature")
	writeFile(t, dir, "feature.txt", "a\nb\n")
	commitAll(t, dir, "feature one")
	writeFile(t, dir, "keep.txt", "keep\nmore\n")
	commitAll(t, dir, "feature two")
	writeFile(t, dir, "staged.txt", "one\ntwo\n")
	gitT(t, dir, "add", "staged.txt")
	writeFile(t, dir, "unstaged.txt", "changed\n")
	writeFile(t, dir, "untracked.txt", "u1\nu2\nu3\n")
	gitT(t, dir, "mv", "old.txt", "new.txt")
	return dir
}

func loadReview(t *testing.T, r *Repo, base string, uncommitted bool) Review {
	t.Helper()
	rv, err := r.LoadReview(context.Background(), base, uncommitted)
	if err != nil {
		t.Fatal(err)
	}
	return rv
}

func fileByPath(rv Review, path string) *ReviewFile {
	for i := range rv.Files {
		if rv.Files[i].Path == path {
			return &rv.Files[i]
		}
	}
	return nil
}

func TestResolveBaseOrder(t *testing.T) {
	ctx := context.Background()
	origin := newRepo(t)
	writeFile(t, origin, "x.txt", "x\n")
	commitAll(t, origin, "init")
	clone := filepath.Join(t.TempDir(), "clone")
	gitT(t, origin, "clone", "-q", origin, clone)
	r := openRepo(t, clone)
	if got, err := r.ResolveBase(ctx, "", ""); err != nil || got != "origin/main" {
		t.Errorf("clone base = %q, %v; want origin/main", got, err)
	}
	if got, _ := r.ResolveBase(ctx, "main", "origin/main"); got != "main" {
		t.Errorf("explicit base = %q, want main", got)
	}
	if got, _ := r.ResolveBase(ctx, "", "main"); got != "main" {
		t.Errorf("configured base = %q, want main", got)
	}
	if _, err := r.ResolveBase(ctx, "nope", ""); err == nil {
		t.Error("an unknown explicit base should fail")
	}

	local := newRepo(t)
	writeFile(t, local, "x.txt", "x\n")
	commitAll(t, local, "init")
	if got, _ := openRepo(t, local).ResolveBase(ctx, "", ""); got != "main" {
		t.Errorf("local-only base = %q, want main", got)
	}
	gitT(t, local, "branch", "-m", "main", "trunk")
	if got, err := openRepo(t, local).ResolveBase(ctx, "", ""); err != nil || got != "" {
		t.Errorf("no candidate base = %q, %v; want empty", got, err)
	}
}

func TestLoadReviewAllChangeKinds(t *testing.T) {
	dir := reviewRepo(t)
	r := openRepo(t, dir)
	rv := loadReview(t, r, "main", false)
	if rv.Base != "main" || len(rv.MergeBase) != 40 || rv.Uncommitted || rv.Fallback != "" {
		t.Errorf("review header = %+v", rv)
	}
	if len(rv.Commits) != 2 || rv.Commits[0].Subject != "feature two" || rv.Commits[1].Subject != "feature one" {
		t.Errorf("commits = %+v", rv.Commits)
	}
	if len(rv.Files) != 6 {
		t.Fatalf("files = %+v, want 6", rv.Files)
	}
	checks := []struct {
		path        string
		status      byte
		added, dels int
		untracked   bool
	}{
		{"feature.txt", 'A', 2, 0, false},
		{"keep.txt", 'M', 1, 0, false},
		{"staged.txt", 'M', 1, 0, false},
		{"unstaged.txt", 'M', 1, 1, false},
		{"untracked.txt", 'A', 3, 0, true},
	}
	for _, c := range checks {
		f := fileByPath(rv, c.path)
		if f == nil || f.Status != c.status || f.Added != c.added || f.Deleted != c.dels || f.Untracked != c.untracked {
			t.Errorf("%s = %+v, want status %c +%d -%d untracked=%v", c.path, f, c.status, c.added, c.dels, c.untracked)
		}
	}
	if f := fileByPath(rv, "new.txt"); f == nil || f.Status != 'R' || f.OldPath != "old.txt" {
		t.Errorf("rename = %+v", f)
	}
}

func TestLoadReviewOnBaseFallsBack(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, "a.txt", "a\n")
	commitAll(t, dir, "init")
	writeFile(t, dir, "a.txt", "b\n")
	r := openRepo(t, dir)
	rv := loadReview(t, r, "main", false)
	if !rv.Uncommitted || rv.Fallback != "no commits ahead of main; showing uncommitted changes" || len(rv.Files) != 1 {
		t.Errorf("review on the base = %+v", rv)
	}
	rv = loadReview(t, r, "", false)
	if !rv.Uncommitted || rv.Fallback != "no base branch found; showing uncommitted changes" || len(rv.Files) != 1 {
		t.Errorf("review without a base = %+v", rv)
	}
}

func TestUncommittedOnly(t *testing.T) {
	dir := reviewRepo(t)
	rv := loadReview(t, openRepo(t, dir), "main", true)
	if fileByPath(rv, "feature.txt") != nil || fileByPath(rv, "keep.txt") != nil {
		t.Errorf("committed-only changes should be hidden: %+v", rv.Files)
	}
	if fileByPath(rv, "unstaged.txt") == nil || fileByPath(rv, "untracked.txt") == nil || fileByPath(rv, "staged.txt") == nil {
		t.Errorf("uncommitted changes missing: %+v", rv.Files)
	}
	if len(rv.Commits) != 2 {
		t.Errorf("commits still listed for context, got %+v", rv.Commits)
	}
}

func TestReviewDiffUntracked(t *testing.T) {
	dir := reviewRepo(t)
	r := openRepo(t, dir)
	rv := loadReview(t, r, "main", false)
	out, err := r.ReviewDiff(context.Background(), rv, *fileByPath(rv, "untracked.txt"))
	if err != nil || !strings.Contains(out, "+++ b/untracked.txt") || !strings.Contains(out, "+u3") {
		t.Errorf("untracked diff = %q, %v", out, err)
	}
	out, err = r.ReviewDiff(context.Background(), rv, *fileByPath(rv, "keep.txt"))
	if err != nil || !strings.Contains(out, "+more") {
		t.Errorf("committed change diff = %q, %v", out, err)
	}
	out, err = r.ReviewDiff(context.Background(), rv, *fileByPath(rv, "new.txt"))
	if err != nil || !strings.Contains(out, "rename from old.txt") {
		t.Errorf("rename diff = %q, %v", out, err)
	}
}

func TestViewedClearsWhenFileChanges(t *testing.T) {
	dir := reviewRepo(t)
	r := openRepo(t, dir)
	v := LoadViewed(r.GitDir).ForBase("main")
	v.Files["keep.txt"] = FileHash(r.Top, "keep.txt")
	if err := v.Save(r.GitDir); err != nil {
		t.Fatal(err)
	}
	v = LoadViewed(r.GitDir)
	if !v.Is("keep.txt", FileHash(r.Top, "keep.txt")) {
		t.Fatal("a saved tick should survive a reload")
	}
	writeFile(t, dir, "keep.txt", "keep\nmore\nagain\n")
	if v.Is("keep.txt", FileHash(r.Top, "keep.txt")) {
		t.Error("a changed file should lose its tick")
	}
}

func TestViewedFreshOnBaseChange(t *testing.T) {
	dir := reviewRepo(t)
	r := openRepo(t, dir)
	v := LoadViewed(r.GitDir).ForBase("main")
	v.Files["keep.txt"] = FileHash(r.Top, "keep.txt")
	v.Save(r.GitDir)
	if got := LoadViewed(r.GitDir).ForBase("origin/main"); len(got.Files) != 0 {
		t.Errorf("a new base should start a fresh set, got %+v", got)
	}
}

func TestDeletedFileViewedHash(t *testing.T) {
	dir := reviewRepo(t)
	r := openRepo(t, dir)
	os.Remove(filepath.Join(dir, "unstaged.txt"))
	if got := FileHash(r.Top, "unstaged.txt"); got != "deleted" {
		t.Errorf("FileHash(missing) = %q, want deleted", got)
	}
}

func TestViewedPerWorktree(t *testing.T) {
	dir := reviewRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	gitT(t, dir, "worktree", "add", "-q", wt, "main")
	main, other := openRepo(t, dir), openRepo(t, wt)
	if main.GitDir == other.GitDir {
		t.Fatal("worktrees should have their own git dirs")
	}
	v := LoadViewed(other.GitDir).ForBase("main")
	v.Files["keep.txt"] = "h"
	v.Save(other.GitDir)
	if len(LoadViewed(main.GitDir).Files) != 0 {
		t.Error("ticks in one worktree leaked into another")
	}
	if _, err := os.Stat(filepath.Join(other.GitDir, "deck", "review.json")); err != nil {
		t.Errorf("ticks should live in the worktree's git dir: %v", err)
	}
}
