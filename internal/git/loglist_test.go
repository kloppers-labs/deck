package git

import (
	"reflect"
	"testing"
)

func TestParseLog(t *testing.T) {
	out := "abc1234\x002 hours ago\x00Ada\x00HEAD -> main, origin/main\x00fix: things\n" +
		"def5678\x003 days ago\x00Bob\x00\x00feat: a subject with \x00 inside\n" +
		"broken line\n\n"
	want := []LogEntry{
		{Hash: "abc1234", Age: "2 hours ago", Author: "Ada", Refs: "HEAD -> main, origin/main", Subject: "fix: things"},
		{Hash: "def5678", Age: "3 days ago", Author: "Bob", Subject: "feat: a subject with \x00 inside"},
	}
	if got := parseLog(out); !reflect.DeepEqual(got, want) {
		t.Errorf("parseLog:\n got %+v\nwant %+v", got, want)
	}
}

func TestFirstHunkLine(t *testing.T) {
	cases := []struct {
		name string
		diff string
		want int
	}{
		{"none", "", 1},
		{"binary", "Binary files a/x and b/x differ\n", 1},
		{"addition after context", "diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -10,7 +10,8 @@ func f() {\n ctx\n ctx\n ctx\n+added\n ctx\n@@ -40 +41 @@\n-x\n+y\n", 13},
		{"new file", "--- /dev/null\n+++ b/n\n@@ -0,0 +1,3 @@\n+a\n+b\n+c\n", 1},
		{"deletion", "@@ -5,3 +5,2 @@\n a\n-b\n c\n", 6},
		{"single-line header", "@@ -1 +7 @@\n-x\n+y\n", 7},
	}
	for _, c := range cases {
		if got := firstHunkLine(c.diff); got != c.want {
			t.Errorf("%s: firstHunkLine = %d, want %d", c.name, got, c.want)
		}
	}
}
