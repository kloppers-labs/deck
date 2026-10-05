package ai

import (
	"reflect"
	"testing"
)

func TestBuildArgs(t *testing.T) {
	tests := []struct {
		backend  string
		wantName string
		wantArgs []string
	}{
		{"claude", "claude", []string{"-p", "hello", "--model", "sonnet", "--output-format", "text"}},
		{"", "claude", []string{"-p", "hello", "--model", "sonnet", "--output-format", "text"}},
		{"codex", "codex", []string{"exec", "hello"}},
		{"crush", "crush", []string{"run", "--quiet", "-m", "sonnet", "hello"}},
		{"mods", "crush", []string{"run", "--quiet", "-m", "sonnet", "hello"}},
	}
	for _, tt := range tests {
		name, args := BuildArgs(tt.backend, "sonnet", "hello")
		if name != tt.wantName {
			t.Errorf("backend %q: name = %q, want %q", tt.backend, name, tt.wantName)
		}
		if !reflect.DeepEqual(args, tt.wantArgs) {
			t.Errorf("backend %q: args = %v, want %v", tt.backend, args, tt.wantArgs)
		}
	}
}

func TestBuildArgsPreservesMultilinePrompt(t *testing.T) {
	prompt := "line one\nline two\n```go\nfmt.Println()\n```"
	_, args := BuildArgs("claude", "sonnet", prompt)
	if args[1] != prompt {
		t.Errorf("prompt argument was altered:\n got %q\nwant %q", args[1], prompt)
	}
}
