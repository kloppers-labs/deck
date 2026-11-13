package git

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func TestParseNameStatusZ(t *testing.T) {
	out := "M\x00a.go\x00A\x00new.go\x00D\x00gone.go\x00R100\x00old.go\x00moved.go\x00"
	want := []ReviewFile{
		{Path: "a.go", Status: 'M'},
		{Path: "new.go", Status: 'A'},
		{Path: "gone.go", Status: 'D'},
		{Path: "moved.go", OldPath: "old.go", Status: 'R'},
	}
	if got := parseNameStatusZ(out); !reflect.DeepEqual(got, want) {
		t.Errorf("parseNameStatusZ = %+v\nwant %+v", got, want)
	}
	if got := parseNameStatusZ(""); len(got) != 0 {
		t.Errorf("empty output = %+v", got)
	}
}

func TestParseNumstatZ(t *testing.T) {
	out := "3\t1\ta.go\x00-\t-\tlogo.png\x002\t0\t\x00old.go\x00moved.go\x00"
	want := map[string][2]int{"a.go": {3, 1}, "logo.png": {-1, -1}, "moved.go": {2, 0}}
	if got := parseNumstatZ(out); !reflect.DeepEqual(got, want) {
		t.Errorf("parseNumstatZ = %v, want %v", got, want)
	}
}

func TestBlobHashMatchesGit(t *testing.T) {
	requireGit(t)
	for _, body := range []string{"", "x\n", strings.Repeat("abcdefg\n", 9000)} {
		cmd := exec.Command("git", "hash-object", "--stdin")
		cmd.Stdin = strings.NewReader(body)
		out, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		if got, want := BlobHash([]byte(body)), strings.TrimSpace(string(out)); got != want {
			t.Errorf("BlobHash(%d bytes) = %s, want %s", len(body), got, want)
		}
	}
}

func TestViewedIs(t *testing.T) {
	v := Viewed{Base: "main", Files: map[string]string{"a.go": "h1"}}
	if !v.Is("a.go", "h1") || v.Is("a.go", "h2") || v.Is("b.go", "") {
		t.Errorf("Is gave wrong answers for %+v", v)
	}
	if fresh := v.ForBase("origin/main"); len(fresh.Files) != 0 || fresh.Base != "origin/main" {
		t.Errorf("ForBase(new base) = %+v, want an empty set", fresh)
	}
	if same := v.ForBase("main"); !same.Is("a.go", "h1") {
		t.Error("ForBase(same base) should keep the ticks")
	}
}
