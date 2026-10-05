package git

import (
	"strings"
	"testing"
)

func TestStyleLogKeepsContent(t *testing.T) {
	in := "abc1234 (HEAD -> main, origin/main) fix things\ndef5678 add stuff\n"
	out := styleLog(in)
	for _, want := range []string{"abc1234", "fix things", "def5678", "add stuff", "(HEAD -> main, origin/main)"} {
		if !strings.Contains(out, want) {
			t.Errorf("styled log lost %q in %q", want, out)
		}
	}
	if got := strings.Count(out, "\n"); got != 2 {
		t.Errorf("styled log has %d newlines, want 2", got)
	}
}

func TestBranchPickerCaptures(t *testing.T) {
	p := NewPane("dracula")
	if p.Capturing() {
		t.Fatal("fresh pane should not capture")
	}
	p.branching = true
	if !p.Capturing() {
		t.Error("open branch picker should capture the keyboard")
	}
}
