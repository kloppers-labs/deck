package git

import "testing"

func TestParseStatus(t *testing.T) {
	out := " M internal/ai/ai.go\n" +
		"A  internal/git/git.go\n" +
		"?? notes.txt\n" +
		"MM main.go\n"
	got := ParseStatus(out)
	want := []Change{
		{Staged: " ", Unstaged: "M", Path: "internal/ai/ai.go"},
		{Staged: "A", Unstaged: " ", Path: "internal/git/git.go"},
		{Staged: "?", Unstaged: "?", Path: "notes.txt"},
		{Staged: "M", Unstaged: "M", Path: "main.go"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d changes, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("change %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseStatusRename(t *testing.T) {
	got := ParseStatus("R  old/name.go -> new/name.go\n")
	if len(got) != 1 {
		t.Fatalf("got %d changes, want 1", len(got))
	}
	if got[0].Path != "new/name.go" {
		t.Errorf("rename path = %q, want destination %q", got[0].Path, "new/name.go")
	}
}

func TestParseStatusIgnoresBlankLines(t *testing.T) {
	if got := ParseStatus("\n\n   \n"); len(got) != 0 {
		t.Errorf("expected no changes from blank input, got %+v", got)
	}
}

func TestChangePredicates(t *testing.T) {
	staged := Change{Staged: "A", Unstaged: " "}
	if !staged.IsStaged() {
		t.Error("expected IsStaged true for index change A")
	}
	if staged.IsUntracked() {
		t.Error("staged change should not be untracked")
	}
	untracked := Change{Staged: "?", Unstaged: "?"}
	if !untracked.IsUntracked() {
		t.Error("expected IsUntracked true for ??")
	}
	if untracked.IsStaged() {
		t.Error("untracked file should not count as staged")
	}
	none := Change{Staged: " ", Unstaged: "M"}
	if none.IsStaged() {
		t.Error("working-tree-only change should not be staged")
	}
}
