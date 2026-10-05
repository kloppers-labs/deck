package runner

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"deck/internal/config"
)

func write(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDetectGo(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "go.mod")
	got := detect(dir)
	if len(got) == 0 || got[0].Name != "go test" {
		t.Fatalf("expected go tasks first, got %+v", got)
	}
}

func TestDetectAngularOverNpm(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "package.json")
	write(t, dir, "angular.json")
	got := detect(dir)
	if len(got) != 1 || got[0].Cmd != "ng serve" {
		t.Fatalf("angular.json should yield exactly 'ng serve', got %+v", got)
	}
}

func TestDetectDotnet(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "Api.csproj")
	got := detect(dir)
	if len(got) != 2 || got[0].Cmd != "dotnet build" {
		t.Fatalf("expected dotnet build+run, got %+v", got)
	}
}

func TestDetectEmpty(t *testing.T) {
	if got := detect(t.TempDir()); len(got) != 0 {
		t.Errorf("empty project should detect no tasks, got %+v", got)
	}
}

func writeContent(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func names(tasks []config.Task) []string {
	out := make([]string, len(tasks))
	for i, t := range tasks {
		out[i] = t.Name
	}
	return out
}

func TestDetectNpmScriptsEnumerated(t *testing.T) {
	dir := t.TempDir()
	writeContent(t, dir, "package.json",
		`{"scripts":{"start":"ng serve","test":"ng test","build":"ng build"}}`)
	got := detect(dir)
	want := []string{"npm build", "npm start", "npm test"} // sorted
	if len(got) != 3 {
		t.Fatalf("expected 3 script tasks, got %v", names(got))
	}
	for i, w := range want {
		if got[i].Name != w {
			t.Errorf("task %d = %q, want %q", i, got[i].Name, w)
		}
	}
}

func TestDetectAngularSkipsNgServeWhenStartScriptExists(t *testing.T) {
	dir := t.TempDir()
	writeContent(t, dir, "package.json", `{"scripts":{"start":"ng serve"}}`)
	write(t, dir, "angular.json")
	got := detect(dir)
	for _, task := range got {
		if task.Name == "ng serve" {
			t.Errorf("ng serve should not be duplicated when npm start exists: %v", names(got))
		}
	}
}

func TestDetectMakefileTargets(t *testing.T) {
	dir := t.TempDir()
	writeContent(t, dir, "Makefile",
		"VAR := 1\n.PHONY: build test\nbuild:\n\tgo build ./...\n\ninstall_cmd\ntest: build\n\tgo test ./...\n%.o: %.c\n\tcc $<\n")
	got := detect(dir)
	want := []string{"make build", "make test"}
	if len(got) != 2 || got[0].Name != want[0] || got[1].Name != want[1] {
		t.Errorf("detected %v, want %v (no .PHONY, no := vars, no pattern rules)", names(got), want)
	}
}

func TestDetectJustfile(t *testing.T) {
	dir := t.TempDir()
	writeContent(t, dir, "justfile", "deploy:\n\techo hi\n")
	got := detect(dir)
	if len(got) != 1 || got[0].Cmd != "just deploy" {
		t.Errorf("detected %v, want just deploy", names(got))
	}
}

func TestDetectCargo(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "Cargo.toml")
	got := detect(dir)
	if len(got) != 3 || got[0].Cmd != "cargo build" {
		t.Errorf("expected cargo build/test/run, got %v", names(got))
	}
}

func TestDetectGemfile(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "Gemfile")
	got := detect(dir)
	if len(got) != 2 || got[1].Cmd != "bundle exec rake" {
		t.Errorf("expected bundle install + rake, got %v", names(got))
	}
}

func TestMatchExt(t *testing.T) {
	cases := []struct {
		pattern, name string
		want          bool
	}{
		{"*.csproj", "Api.csproj", true},
		{"*.csproj", "Api.cs", false},
		{"*.sln", "deck.sln", true},
		{"go.mod", "go.mod", true},
	}
	for _, c := range cases {
		if got, _ := matchExt(c.pattern, c.name); got != c.want {
			t.Errorf("matchExt(%q,%q) = %v, want %v", c.pattern, c.name, got, c.want)
		}
	}
}

func TestCapturingWhileFiltering(t *testing.T) {
	p := NewPane([]config.Task{{Name: "build", Cmd: "go build"}}, "")
	p.SetSize(80, 24)
	if p.Capturing() {
		t.Fatal("Run pane should not capture before the filter is active")
	}
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	if !p.Capturing() {
		t.Error("'/' should start the filter and capture the keyboard")
	}
}

// drain reads all messages from a proc until its channel closes.
func drain(p *proc) []tea.Msg {
	var msgs []tea.Msg
	for m := range p.ch {
		msgs = append(msgs, m)
	}
	return msgs
}

func TestStartStreamsStdoutThenExit(t *testing.T) {
	p := start(0, 1, "echo hello")
	msgs := drain(p)
	if len(msgs) != 2 {
		t.Fatalf("expected output + exit, got %d: %+v", len(msgs), msgs)
	}
	out, ok := msgs[0].(OutputMsg)
	if !ok || out.text != "hello\n" {
		t.Errorf("first msg = %+v, want OutputMsg 'hello\\n'", msgs[0])
	}
	ex, ok := msgs[1].(ExitMsg)
	if !ok || ex.err != nil {
		t.Errorf("last msg = %+v, want clean ExitMsg", msgs[1])
	}
}

func TestStartCapturesStderrAndFailure(t *testing.T) {
	p := start(2, 1, "echo oops >&2; exit 1")
	msgs := drain(p)
	if len(msgs) < 2 {
		t.Fatalf("expected stderr line + exit, got %+v", msgs)
	}
	if out, ok := msgs[0].(OutputMsg); !ok || out.text != "oops\n" || out.idx != 2 {
		t.Errorf("expected stderr captured as OutputMsg{idx:2,'oops\\n'}, got %+v", msgs[0])
	}
	if ex, ok := msgs[len(msgs)-1].(ExitMsg); !ok || ex.err == nil {
		t.Errorf("expected failing ExitMsg, got %+v", msgs[len(msgs)-1])
	}
}

func TestStopCancelsRunningProcess(t *testing.T) {
	p := start(0, 1, "sleep 30")
	done := make(chan []tea.Msg, 1)
	go func() { done <- drain(p) }()
	time.Sleep(100 * time.Millisecond)
	p.stop()
	select {
	case msgs := <-done:
		if ex, ok := msgs[len(msgs)-1].(ExitMsg); !ok || ex.err == nil {
			t.Errorf("cancelled process should exit with error, got %+v", msgs)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stop() did not terminate the process within 3s")
	}
}
