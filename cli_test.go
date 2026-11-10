package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLI(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name       string
		args       []string
		code       int
		launch     bool
		stdout     string
		stderr     string
		wantCwdDir bool
	}{
		{name: "no args launches", args: nil, launch: true},
		{name: "version", args: []string{"--version"}, stdout: "deck dev\n"},
		{name: "short version", args: []string{"-v"}, stdout: "deck dev\n"},
		{name: "help", args: []string{"--help"}, stdout: "Usage:"},
		{name: "unknown flag", args: []string{"--nope"}, code: 2, stderr: `unexpected argument "--nope"`},
		{name: "two dirs", args: []string{dir, dir}, code: 2, stderr: "unexpected argument"},
		{name: "missing dir", args: []string{filepath.Join(dir, "missing")}, code: 1, stderr: "no such file"},
		{name: "dir launches there", args: []string{dir}, launch: true, wantCwdDir: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			var out, errOut bytes.Buffer
			code, launch := cli(tc.args, &out, &errOut)
			if code != tc.code || launch != tc.launch {
				t.Fatalf("cli(%q) = (%d, %v), want (%d, %v)", tc.args, code, launch, tc.code, tc.launch)
			}
			if !strings.Contains(out.String(), tc.stdout) || (tc.stdout == "" && out.Len() > 0) {
				t.Errorf("stdout = %q, want it to contain %q", out.String(), tc.stdout)
			}
			if !strings.Contains(errOut.String(), tc.stderr) || (tc.stderr == "" && errOut.Len() > 0) {
				t.Errorf("stderr = %q, want it to contain %q", errOut.String(), tc.stderr)
			}
			if tc.wantCwdDir {
				cwd, _ := os.Getwd()
				want, _ := filepath.EvalSymlinks(dir)
				if got, _ := filepath.EvalSymlinks(cwd); got != want {
					t.Errorf("cwd = %s, want %s", got, want)
				}
			}
		})
	}
}

func TestVersionPrefersLinkerValue(t *testing.T) {
	old := version
	t.Cleanup(func() { version = old })
	version = "1.2.3"
	if got := versionString(); got != "1.2.3" {
		t.Errorf("versionString() = %q, want 1.2.3", got)
	}
}
