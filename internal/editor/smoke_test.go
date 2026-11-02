//go:build microsmoke

package editor

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"deck/internal/config"
)

func runMicro(t *testing.T, dir, file string, target Target) State {
	t.Helper()
	state := filepath.Join(t.TempDir(), "state")
	c, _, err := Launch("micro", Target{Path: file, Line: target.Line, Col: target.Col, Top: target.Top}, Opts{ConfigDir: dir, StateFile: state})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	inner := "stty rows 30 cols 100; exec " + shellJoin(c.Args)
	script := exec.CommandContext(ctx, "script", "-q", "/dev/null", "sh", "-c", inner)
	script.Env = append(c.Env, "TERM=xterm-256color")
	script.Dir = c.Dir
	stdin, err := script.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	script.Stdout = io.Discard
	launched := time.Now()
	if err := script.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Second)
	if _, err := io.WriteString(stdin, "\x1bq"); err != nil {
		t.Fatal(err)
	}
	if err := script.Wait(); err != nil {
		t.Fatalf("micro did not exit cleanly: %v", err)
	}
	st, ok := ReadState(state, launched)
	if !ok {
		data, _ := os.ReadFile(state)
		t.Fatalf("no usable state written: %q", data)
	}
	return st
}

func shellJoin(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
	}
	return strings.Join(quoted, " ")
}

func TestMicroSmoke(t *testing.T) {
	bin, err := exec.LookPath("micro")
	if err != nil {
		t.Skip("micro not on PATH")
	}
	dir := filepath.Join(t.TempDir(), "micro")
	if _, err := Sync(dir, MicroVersion(bin), SyncOptions{LSP: false}); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for i := 1; i <= 200; i++ {
		fmt.Fprintf(&b, "line %03d of the smoke test file\n", i)
	}
	file := filepath.Join(t.TempDir(), "smoke.txt")
	if err := os.WriteFile(file, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	st := runMicro(t, dir, file, Target{Line: 3, Col: 5})
	if st.Path != file || st.Line != 3 || st.Col != 5 {
		t.Errorf("state = %+v, want %s at 3:5", st, file)
	}

	st = runMicro(t, dir, file, Target{Line: 120, Col: 1, Top: 110})
	if st.Line != 120 || st.Top != 110 {
		t.Errorf("state = %+v, want line 120 with top 110 restored", st)
	}
}

func TestMicroSmokeFullBundle(t *testing.T) {
	if _, err := exec.LookPath("micro"); err != nil {
		t.Skip("micro not on PATH")
	}
	dir := filepath.Join(t.TempDir(), "micro")
	msg := RunSync(BundleConfig{Editor: "micro", Dir: dir, LSP: true, Specs: SpecsFromConfig(config.LSP{})})
	if msg.Err != nil {
		t.Fatal(msg.Err)
	}
	t.Logf("features %+v, issues %q", msg.Features, msg.Issues)
	work := t.TempDir()
	file := filepath.Join(work, "main.go")
	if err := os.WriteFile(filepath.Join(work, "go.mod"), []byte("module smoke\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := runMicro(t, dir, file, Target{Line: 4, Col: 2})
	if st.Path != file || st.Line != 4 || st.Col != 2 {
		t.Errorf("state = %+v, want %s at 4:2", st, file)
	}
}
