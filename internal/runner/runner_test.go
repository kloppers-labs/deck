package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kloppers-labs/deck/internal/config"
)

func write(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
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

func TestDetectGo(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "go.mod")
	got := detect(dir).tasks
	if len(got) == 0 || got[0].Name != "go test" {
		t.Fatalf("expected go tasks first, got %+v", got)
	}
}

func TestDetectAngularOverNpm(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "package.json")
	write(t, dir, "angular.json")
	got := detect(dir).tasks
	if len(got) != 1 || got[0].Cmd != "ng serve" {
		t.Fatalf("angular.json should yield exactly 'ng serve', got %+v", got)
	}
}

func TestDetectDotnet(t *testing.T) {
	dir := t.TempDir()
	writeContent(t, dir, "Api.csproj", webProj)
	got := detect(dir).tasks
	if len(got) != 3 || got[0].Cmd != "dotnet build Api.csproj" {
		t.Fatalf("expected dotnet build/watch/run, got %+v", cmds(got))
	}
}

func TestDetectEmpty(t *testing.T) {
	if got := detect(t.TempDir()); len(got.tasks) != 0 || got.more != 0 {
		t.Errorf("empty project should detect no tasks, got %+v", got)
	}
}

func TestDetectNpmScriptsEnumerated(t *testing.T) {
	dir := t.TempDir()
	writeContent(t, dir, "package.json",
		`{"scripts":{"start":"ng serve","test":"ng test","build":"ng build"}}`)
	got := detect(dir).tasks
	want := []string{"npm build", "npm start", "npm test"}
	if !slices.Equal(names(got), want) {
		t.Fatalf("names = %v, want %v", names(got), want)
	}
	if got[1].Cmd != "npm run start" {
		t.Errorf("cmd = %q, want npm run start", got[1].Cmd)
	}
}

func TestDetectPackageManagerFromLockfile(t *testing.T) {
	cases := []struct{ lock, want string }{
		{"pnpm-lock.yaml", "pnpm run dev"},
		{"yarn.lock", "yarn run dev"},
		{"bun.lockb", "bun run dev"},
		{"bun.lock", "bun run dev"},
		{"package-lock.json", "npm run dev"},
	}
	for _, c := range cases {
		dir := t.TempDir()
		writeContent(t, dir, "package.json", `{"scripts":{"dev":"vite"}}`)
		write(t, dir, c.lock)
		if got := detect(dir).tasks; len(got) != 1 || got[0].Cmd != c.want {
			t.Errorf("%s: tasks = %v, want %q", c.lock, cmds(got), c.want)
		}
	}
}

func TestDetectNpmCapReportsMore(t *testing.T) {
	dir := t.TempDir()
	var scripts []string
	for i := range maxNpmScripts + 3 {
		scripts = append(scripts, fmt.Sprintf(`"s%02d":"x"`, i))
	}
	writeContent(t, dir, "package.json", `{"scripts":{`+strings.Join(scripts, ",")+`}}`)
	got := detect(dir)
	if len(got.tasks) != maxNpmScripts || got.more != 3 {
		t.Errorf("got %d tasks +%d more, want %d +3", len(got.tasks), got.more, maxNpmScripts)
	}
}

func TestDetectAngularSkipsNgServeWhenStartScriptExists(t *testing.T) {
	dir := t.TempDir()
	writeContent(t, dir, "package.json", `{"scripts":{"start":"ng serve"}}`)
	write(t, dir, "angular.json")
	for _, task := range detect(dir).tasks {
		if task.Name == "ng serve" {
			t.Errorf("ng serve should not be duplicated when a start script exists")
		}
	}
}

func TestDetectMakefileTargets(t *testing.T) {
	dir := t.TempDir()
	writeContent(t, dir, "Makefile",
		"VAR := 1\n.PHONY: build test\nbuild:\n\tgo build ./...\n\ninstall_cmd\ntest: build\n\tgo test ./...\n%.o: %.c\n\tcc $<\n")
	got := detect(dir).tasks
	want := []string{"make build", "make test"}
	if !slices.Equal(names(got), want) {
		t.Errorf("detected %v, want %v (no .PHONY, no := vars, no pattern rules)", names(got), want)
	}
}

func TestDetectMakefileCapReportsMore(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	for i := range maxFileTargets + 4 {
		fmt.Fprintf(&b, "t%02d:\n\techo\n", i)
	}
	writeContent(t, dir, "Makefile", b.String())
	if got := detect(dir); len(got.tasks) != maxFileTargets || got.more != 4 {
		t.Errorf("got %d tasks +%d more, want %d +4", len(got.tasks), got.more, maxFileTargets)
	}
}

func TestDetectJustfile(t *testing.T) {
	dir := t.TempDir()
	writeContent(t, dir, "justfile", "deploy:\n\techo hi\n")
	got := detect(dir).tasks
	if len(got) != 1 || got[0].Cmd != "just deploy" {
		t.Errorf("detected %v, want just deploy", names(got))
	}
}

func TestDetectCargo(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "Cargo.toml")
	got := detect(dir).tasks
	if len(got) != 3 || got[0].Cmd != "cargo build" {
		t.Errorf("expected cargo build/test/run, got %v", names(got))
	}
}

func TestDetectGemfile(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "Gemfile")
	got := detect(dir).tasks
	if len(got) != 2 || got[1].Cmd != "bundle exec rake" {
		t.Errorf("expected bundle install + rake, got %v", names(got))
	}
}

func TestDetectedTasksAreAuto(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "go.mod")
	for _, task := range detect(dir).tasks {
		if task.Kind == "" {
			t.Errorf("detected task %q has no kind", task.Name)
		}
	}
}

func TestMergeTasks(t *testing.T) {
	configured := []config.Task{{Name: "api", Cmd: "dotnet watch --project src/Api"}, {Name: "go test", Cmd: "go test -race ./..."}}
	detected := []config.Task{{Name: "go test", Cmd: "go test ./..."}, {Name: "watch Api", Cmd: "dotnet watch --project src/Api"}, {Name: "go build", Cmd: "go build ./..."}}
	got := mergeDetected(configured, detected)
	if want := []string{"go build"}; !slices.Equal(names(got), want) {
		t.Errorf("merged = %v, want %v: duplicates by name or command are dropped", names(got), want)
	}
}
