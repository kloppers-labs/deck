package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// withRepo creates an isolated git repo in a temp dir, chdirs into it for the
// duration of the test, and returns once cwd is restored. Skips if git is
// unavailable.
func withRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })

	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "deck@test.local"},
		{"config", "user.name", "deck test"},
		{"config", "commit.gpgsign", "false"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func TestRepoLifecycle(t *testing.T) {
	dir := withRepo(t)

	if !IsRepo() {
		t.Fatal("IsRepo() = false inside a fresh git repo")
	}

	path := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	changes, err := Status()
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || !changes[0].IsUntracked() {
		t.Fatalf("expected one untracked change, got %+v", changes)
	}

	if err := Stage("hello.txt"); err != nil {
		t.Fatal(err)
	}
	changes, _ = Status()
	if len(changes) != 1 || !changes[0].IsStaged() {
		t.Fatalf("after Stage, expected staged change, got %+v", changes)
	}

	if diff, err := StagedDiff(); err != nil || diff == "" {
		t.Fatalf("StagedDiff() = %q, err %v; want non-empty", diff, err)
	}

	if err := Unstage("hello.txt"); err != nil {
		t.Fatal(err)
	}
	changes, _ = Status()
	if len(changes) != 1 || changes[0].IsStaged() {
		t.Fatalf("after Unstage, expected unstaged change, got %+v", changes)
	}

	_ = Stage("hello.txt")
	if out, err := Commit("add hello"); err != nil {
		t.Fatalf("Commit failed: %v\n%s", err, out)
	}
	changes, _ = Status()
	if len(changes) != 0 {
		t.Errorf("after commit, working tree should be clean, got %+v", changes)
	}

	if b, err := CurrentBranch(); err != nil || b == "" {
		t.Errorf("CurrentBranch() = %q, err %v; want a branch name", b, err)
	}
}
