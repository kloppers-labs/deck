package git

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestEnvHasNoPromptAndNoLocks(t *testing.T) {
	env := gitEnv([]string{"HOME=/h", "LC_ALL=fr_FR.UTF-8", "GIT_TERMINAL_PROMPT=1"})
	for _, want := range []string{"HOME=/h", "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C"} {
		if !slices.Contains(env, want) {
			t.Errorf("env %v is missing %q", env, want)
		}
	}
	for _, bad := range []string{"LC_ALL=fr_FR.UTF-8", "GIT_TERMINAL_PROMPT=1"} {
		if slices.Contains(env, bad) {
			t.Errorf("env %v still carries %q", env, bad)
		}
	}
}

func TestRunSeparatesStderr(t *testing.T) {
	dir := newRepo(t)
	gitT(t, dir, "config", "core.autocrlf", "true")
	writeFile(t, dir, "crlf.txt", "one\ntwo\n")
	gitT(t, dir, "add", "crlf.txt")
	gitT(t, dir, "commit", "-qm", "init")
	writeFile(t, dir, "crlf.txt", "one\ntwo\nthree\n")
	repo, err := OpenRepo(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := runGit(context.Background(), repo.Top, "diff", "--", "crlf.txt")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout, "warning:") {
		t.Errorf("stdout carries git's warning:\n%s", stdout)
	}
	if !strings.Contains(stdout, "+three") {
		t.Errorf("stdout lost the diff:\n%s", stdout)
	}
	if !strings.Contains(stderr, "warning:") {
		t.Logf("git printed no CRLF warning to stderr (%q); separation still holds", stderr)
	}
	diff, err := repo.Diff(context.Background(), Item{Section: SectionUnstaged, Entry: Entry{Kind: '1', X: '.', Y: 'M', Path: "crlf.txt"}})
	if err != nil || strings.Contains(diff, "warning:") {
		t.Errorf("Repo.Diff = %q, %v; want the diff without warnings", diff, err)
	}
}

func TestRepoFromSubdirStageAndDiff(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, dir, "README.md", "hi\n")
	gitT(t, dir, "add", ".")
	gitT(t, dir, "commit", "-qm", "init")
	sub := filepath.Join(dir, "src", "Api")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "README.md", "hi\nthere\n")
	t.Chdir(sub)
	repo, err := OpenRepo(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := canon(t, repo.Top), canon(t, dir); got != want {
		t.Errorf("Top = %q, want %q", got, want)
	}
	it := Item{Section: SectionUnstaged, Entry: Entry{Kind: '1', X: '.', Y: 'M', Path: "README.md"}}
	diff, err := repo.Diff(context.Background(), it)
	if err != nil || !strings.Contains(diff, "+there") {
		t.Fatalf("Diff from a subdirectory = %q, %v", diff, err)
	}
	if err := repo.Stage(context.Background(), "README.md"); err != nil {
		t.Fatalf("Stage from a subdirectory: %v", err)
	}
	snap, err := repo.StatusSnapshot(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Entries) != 1 || !snap.Entries[0].Staged() || snap.Entries[0].Unstaged() {
		t.Errorf("after Stage: %+v", snap.Entries)
	}
}

func TestNotARepo(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	if _, err := OpenRepo(context.Background(), dir); err != ErrNotRepo {
		t.Errorf("OpenRepo outside a repository = %v, want ErrNotRepo", err)
	}
}

func TestExecErrorPrefersStderr(t *testing.T) {
	err := &ExecError{Args: []string{"push"}, Stderr: "fatal: no upstream\nmore\n"}
	if got := err.Error(); got != "fatal: no upstream" {
		t.Errorf("Error() = %q, want stderr's first line", got)
	}
	err = &ExecError{Args: []string{"commit"}, Stdout: "nothing to commit, working tree clean\n"}
	if got := err.Error(); !strings.Contains(got, "nothing to commit") {
		t.Errorf("Error() = %q, want stdout when stderr is empty", got)
	}
}

func canon(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
