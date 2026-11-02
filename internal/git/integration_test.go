package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
}

func newRepo(t *testing.T) string {
	t.Helper()
	requireGit(t)
	dir := t.TempDir()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	gitT(t, dir, "init", "-q", "-b", "main")
	gitT(t, dir, "config", "user.email", "deck@test.local")
	gitT(t, dir, "config", "user.name", "deck test")
	gitT(t, dir, "config", "commit.gpgsign", "false")
	return dir
}

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func openRepo(t *testing.T, dir string) *Repo {
	t.Helper()
	r, err := OpenRepo(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func snapshot(t *testing.T, r *Repo) Snapshot {
	t.Helper()
	s, err := r.StatusSnapshot(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRepoLifecycle(t *testing.T) {
	dir := newRepo(t)
	r := openRepo(t, dir)
	ctx := context.Background()
	writeFile(t, dir, "hello.txt", "hello\n")

	s := snapshot(t, r)
	if !s.Initial || s.Branch != "main" {
		t.Errorf("fresh repo snapshot = %+v, want initial on main", s)
	}
	if len(s.Entries) != 1 || !s.Entries[0].Untracked() {
		t.Fatalf("want one untracked entry, got %+v", s.Entries)
	}
	if err := r.Stage(ctx, "hello.txt"); err != nil {
		t.Fatal(err)
	}
	if s = snapshot(t, r); len(s.Entries) != 1 || !s.Entries[0].Staged() {
		t.Fatalf("after Stage: %+v", s.Entries)
	}
	if diff, err := r.StagedDiff(ctx); err != nil || !strings.Contains(diff, "+hello") {
		t.Fatalf("StagedDiff = %q, %v", diff, err)
	}
	if err := r.Unstage(ctx, s.Initial, Entry{Kind: '1', X: 'A', Y: '.', Path: "hello.txt"}); err != nil {
		t.Fatal(err)
	}
	if s = snapshot(t, r); len(s.Entries) != 1 || !s.Entries[0].Untracked() {
		t.Fatalf("after Unstage on an unborn branch: %+v", s.Entries)
	}
	if err := r.StageAll(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Commit(ctx, "add hello\n\nbody line", false); err != nil {
		t.Fatal(err)
	}
	if s = snapshot(t, r); len(s.Entries) != 0 || s.Initial {
		t.Errorf("after commit: %+v", s)
	}
	if msg, err := r.HeadMessage(ctx); err != nil || !strings.Contains(msg, "body line") {
		t.Errorf("HeadMessage = %q, %v", msg, err)
	}
	if _, err := r.Commit(ctx, "add hello, amended", true); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(gitT(t, dir, "log", "--oneline"), "\n"); n != 1 {
		t.Errorf("amend created a new commit: %d commits", n)
	}
}

func TestStatusQuotedPathStages(t *testing.T) {
	dir := newRepo(t)
	r := openRepo(t, dir)
	ctx := context.Background()
	writeFile(t, dir, "My File.cs", "class A {}\n")
	writeFile(t, dir, "naïve ünïcode.txt", "x\n")
	s := snapshot(t, r)
	var paths []string
	for _, e := range s.Entries {
		paths = append(paths, e.Path)
	}
	if !slices.Contains(paths, "My File.cs") || !slices.Contains(paths, "naïve ünïcode.txt") {
		t.Fatalf("paths = %q, want them unquoted", paths)
	}
	for _, p := range paths {
		if err := r.Stage(ctx, p); err != nil {
			t.Fatalf("Stage(%q): %v", p, err)
		}
	}
	for _, e := range snapshot(t, r).Entries {
		if !e.Staged() {
			t.Errorf("%q not staged: %+v", e.Path, e)
		}
	}
}

func TestStatusRenameAndAheadBehind(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, "old -> name.txt", "content\n")
	gitT(t, dir, "add", ".")
	gitT(t, dir, "commit", "-qm", "init")
	remote := t.TempDir()
	gitT(t, remote, "init", "-q", "--bare")
	gitT(t, dir, "remote", "add", "origin", remote)
	gitT(t, dir, "push", "-q", "-u", "origin", "main")
	gitT(t, dir, "mv", "old -> name.txt", "new name.txt")
	gitT(t, dir, "commit", "-qm", "rename")
	gitT(t, dir, "mv", "new name.txt", "newer name.txt")
	s := snapshot(t, openRepo(t, dir))
	if s.Upstream != "origin/main" || s.Ahead != 1 || s.Behind != 0 {
		t.Errorf("branch headers = %+v", s)
	}
	if len(s.Entries) != 1 || s.Entries[0].Kind != '2' || s.Entries[0].Orig != "new name.txt" || s.Entries[0].Path != "newer name.txt" {
		t.Errorf("rename entry = %+v", s.Entries)
	}
}

func TestBranchCreateDelete(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, "a.txt", "a\n")
	gitT(t, dir, "add", ".")
	gitT(t, dir, "commit", "-qm", "init")
	r := openRepo(t, dir)
	ctx := context.Background()
	if err := r.CreateBranch(ctx, "feature/x"); err != nil {
		t.Fatal(err)
	}
	if s := snapshot(t, r); s.Branch != "feature/x" {
		t.Errorf("after create, branch = %q", s.Branch)
	}
	if err := r.Switch(ctx, "main"); err != nil {
		t.Fatal(err)
	}
	branches, err := r.Branches(ctx)
	if err != nil || !slices.Equal(branches, []string{"feature/x", "main"}) {
		t.Fatalf("Branches = %v, %v", branches, err)
	}
	if err := r.DeleteBranch(ctx, "feature/x"); err != nil {
		t.Fatal(err)
	}
	if branches, _ = r.Branches(ctx); !slices.Equal(branches, []string{"main"}) {
		t.Errorf("after delete, branches = %v", branches)
	}
	if err := r.DeleteBranch(ctx, "main"); err == nil {
		t.Error("deleting the checked-out branch should fail")
	}
}

func TestDiscardAndUntrackedDiff(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, "a.txt", "a\n")
	gitT(t, dir, "add", ".")
	gitT(t, dir, "commit", "-qm", "init")
	r := openRepo(t, dir)
	ctx := context.Background()
	writeFile(t, dir, "a.txt", "changed\n")
	writeFile(t, dir, "new.txt", "brand new\n")
	untracked := Item{Section: SectionUntracked, Entry: Entry{Kind: '?', Path: "new.txt"}}
	if diff, err := r.Diff(ctx, untracked); err != nil || !strings.Contains(diff, "+brand new") {
		t.Fatalf("untracked diff = %q, %v", diff, err)
	}
	if err := r.Discard(ctx, false, Item{Section: SectionUnstaged, Entry: Entry{Kind: '1', X: '.', Y: 'M', Path: "a.txt"}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Discard(ctx, false, untracked); err != nil {
		t.Fatal(err)
	}
	if s := snapshot(t, r); len(s.Entries) != 0 {
		t.Errorf("after discard: %+v", s.Entries)
	}
}

func TestLogAndShow(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, "a.txt", "a\n")
	gitT(t, dir, "add", ".")
	gitT(t, dir, "commit", "-qm", "first commit")
	r := openRepo(t, dir)
	entries, err := r.Log(context.Background(), 200)
	if err != nil || len(entries) != 1 || entries[0].Subject != "first commit" || entries[0].Author != "deck test" {
		t.Fatalf("Log = %+v, %v", entries, err)
	}
	show, err := r.Show(context.Background(), entries[0].Hash)
	if err != nil || !strings.Contains(show, "first commit") || !strings.Contains(show, "+a") {
		t.Errorf("Show = %q, %v", show, err)
	}
}
