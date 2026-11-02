package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, body string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func warningsText(ws []Warning) string {
	var parts []string
	for _, w := range ws {
		parts = append(parts, w.String())
	}
	return strings.Join(parts, "\n")
}

func TestDefaultTheme(t *testing.T) {
	if got := Default().Theme; got != "dracula" {
		t.Errorf("Default().Theme = %q, want %q", got, "dracula")
	}
}

func TestLoadMissingFileUsesDefaults(t *testing.T) {
	c, ws := load(filepath.Join(t.TempDir(), "does-not-exist.toml"))
	if len(ws) != 0 {
		t.Errorf("missing file produced warnings: %v", ws)
	}
	if !reflect.DeepEqual(c, Default()) {
		t.Errorf("missing-file load = %+v, want Default()", c)
	}
}

func TestDefaultsFilledOnce(t *testing.T) {
	path := writeFile(t, filepath.Join(t.TempDir(), "config.toml"), `
theme = ""
editor = ""
[viewer]
scroll_lines = 0
wrap = true
[files]
hide = []
`)
	c, ws := load(path)
	if len(ws) != 0 {
		t.Fatalf("unexpected warnings: %v", ws)
	}
	d := Default()
	if c.Theme != d.Theme || c.Editor != d.Editor {
		t.Errorf("empty strings not filled from Default(): theme=%q editor=%q", c.Theme, c.Editor)
	}
	if c.Viewer.ScrollLines != d.Viewer.ScrollLines {
		t.Errorf("viewer.scroll_lines = %d, want default %d", c.Viewer.ScrollLines, d.Viewer.ScrollLines)
	}
	if !c.Viewer.Wrap {
		t.Error("an explicit viewer.wrap = true was lost")
	}
	if !c.Viewer.LineNumbers {
		t.Error("viewer.line_numbers should keep its true default when the file omits it")
	}
	if c.Files.Hide == nil || len(c.Files.Hide) != 0 {
		t.Errorf("an explicit empty files.hide should stay empty, got %v", c.Files.Hide)
	}
}

func TestExplicitFalseOverridesTrueDefault(t *testing.T) {
	path := writeFile(t, filepath.Join(t.TempDir(), "config.toml"), `
micro_bundle = false
[git]
confirm_commit = false
[lsp]
prestart = false
`)
	c, _ := load(path)
	if c.MicroBundle || c.Git.ConfirmCommit || c.LSP.Prestart {
		t.Errorf("explicit false values were overwritten by defaults: %+v", c)
	}
	if !c.LSP.Enabled {
		t.Error("lsp.enabled should keep its default")
	}
}

func TestLoadOverridesTheme(t *testing.T) {
	path := writeFile(t, filepath.Join(t.TempDir(), "config.toml"), "theme = \"monokai\"\n")
	if c, _ := load(path); c.Theme != "monokai" {
		t.Errorf("Theme = %q, want %q", c.Theme, "monokai")
	}
}

func TestLoadUnknownThemeIsKeptVerbatim(t *testing.T) {
	path := writeFile(t, filepath.Join(t.TempDir(), "config.toml"), "theme = \"not-a-real-theme\"\n")
	if c, _ := load(path); c.Theme != "not-a-real-theme" {
		t.Errorf("Theme = %q, want it preserved verbatim", c.Theme)
	}
}

func TestDecodeErrorBecomesWarning(t *testing.T) {
	path := writeFile(t, filepath.Join(t.TempDir(), "config.toml"), "theme = \"monokai\"\nthis is not toml = =\n")
	c, ws := load(path)
	if len(ws) != 1 {
		t.Fatalf("warnings = %v, want exactly one", ws)
	}
	if !strings.Contains(ws[0].String(), "config.toml") {
		t.Errorf("warning %q should name the file", ws[0])
	}
	if !reflect.DeepEqual(c, Default()) {
		t.Errorf("a broken file should leave the defaults, got %+v", c)
	}
}

func TestUnknownKeyWarningSuggests(t *testing.T) {
	path := writeFile(t, filepath.Join(t.TempDir(), "config.toml"), `
[[task]]
name = "api"
cmd = "dotnet run"

[viewer]
scrol_lines = 5
`)
	c, ws := load(path)
	text := warningsText(ws)
	if len(ws) != 2 {
		t.Fatalf("warnings = %q, want one for [[task]] and one for scrol_lines", text)
	}
	if !strings.Contains(text, `"task"`) || !strings.Contains(text, `"tasks"`) {
		t.Errorf("warning %q should name task and suggest tasks", text)
	}
	if !strings.Contains(text, `"viewer.scroll_lines"`) {
		t.Errorf("warning %q should suggest viewer.scroll_lines", text)
	}
	if len(c.Tasks) != 0 {
		t.Errorf("tasks = %v, want none from a misspelt table", c.Tasks)
	}
}

func TestUnknownServerFieldSuggestsLeaf(t *testing.T) {
	path := writeFile(t, filepath.Join(t.TempDir(), "config.toml"), `
[lsp.servers.csharp]
comand = ["csharp-ls"]
`)
	_, ws := load(path)
	if text := warningsText(ws); !strings.Contains(text, `"command"`) {
		t.Errorf("warning %q should suggest command", text)
	}
}

func TestInvalidMouseWarns(t *testing.T) {
	path := writeFile(t, filepath.Join(t.TempDir(), "config.toml"), "mouse = \"sometimes\"\n")
	c, ws := load(path)
	if c.Mouse != Default().Mouse {
		t.Errorf("mouse = %q, want the default after an invalid value", c.Mouse)
	}
	if text := warningsText(ws); !strings.Contains(text, "mouse") {
		t.Errorf("warnings %q should mention mouse", text)
	}
}

func TestProjectLocalMerges(t *testing.T) {
	dir := t.TempDir()
	global := writeFile(t, filepath.Join(dir, "global", "config.toml"), `
theme = "monokai"
editor = "nvim"
[[tasks]]
name = "global"
cmd = "echo g"
dir = "src"
`)
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repo, ProjectFile), `
theme = "github"
[[tasks]]
name = "local"
cmd = "echo l"
`)
	sub := filepath.Join(repo, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	c, ws := load(global, projectPath(sub))
	if len(ws) != 0 {
		t.Fatalf("unexpected warnings: %v", ws)
	}
	if c.Theme != "github" {
		t.Errorf("theme = %q, want the project-local override", c.Theme)
	}
	if c.Editor != "nvim" {
		t.Errorf("editor = %q, want the global value kept", c.Editor)
	}
	if len(c.Tasks) != 2 || c.Tasks[0].Name != "global" || c.Tasks[1].Name != "local" {
		t.Fatalf("tasks = %+v, want global then local", c.Tasks)
	}
	if c.Tasks[1].Dir != "" {
		t.Errorf("local task inherited dir %q from the global task", c.Tasks[1].Dir)
	}
}

func TestProjectPathWithoutRepoUsesCwd(t *testing.T) {
	dir := t.TempDir()
	if got := projectPath(dir); got != filepath.Join(dir, ProjectFile) {
		t.Errorf("projectPath = %q, want the cwd's %s", got, ProjectFile)
	}
}

func TestXDGConfigHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if got, want := Path(), filepath.Join(dir, "deck", "config.toml"); got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	if got, want := Path(), filepath.Join(home, ".config", "deck", "config.toml"); got != want {
		t.Errorf("Path() without XDG = %q, want %q", got, want)
	}
}

func TestNestedDefaults(t *testing.T) {
	d := Default()
	if d.Viewer.ScrollLines != 3 {
		t.Errorf("viewer.scroll_lines = %d, want 3", d.Viewer.ScrollLines)
	}
	if len(d.LSP.Servers) != 0 {
		t.Errorf("lsp.servers = %v, want empty: server defaults live in the lsp package", d.LSP.Servers)
	}
	if d.Mouse != "hover" || d.EditorReturn != "follow" || !d.MicroBundle {
		t.Errorf("top-level defaults wrong: %+v", d)
	}
	if !reflect.DeepEqual(d.Viewer.GotoClick, []string{"ctrl", "double", "right"}) {
		t.Errorf("viewer.goto_click = %v", d.Viewer.GotoClick)
	}
	if !d.Git.ConfirmCommit || !d.LSP.Enabled || !d.LSP.Prestart || d.LSP.RequestTimeoutMS != 8000 {
		t.Errorf("git/lsp defaults wrong: %+v %+v", d.Git, d.LSP)
	}
}

func TestDefaultReturnsFreshSlices(t *testing.T) {
	a := Default()
	a.Files.Hide[0] = "mutated"
	if Default().Files.Hide[0] == "mutated" {
		t.Error("Default() shares slice storage between calls")
	}
}

func TestServerEnabledDefaultsTrue(t *testing.T) {
	path := writeFile(t, filepath.Join(t.TempDir(), "config.toml"), `
[lsp.servers.go]
command = ["gopls"]
[lsp.servers.ts]
command = ["typescript-language-server", "--stdio"]
enabled = false
`)
	c, ws := load(path)
	if len(ws) != 0 {
		t.Fatalf("unexpected warnings: %v", ws)
	}
	if !c.LSP.Servers["go"].IsEnabled() {
		t.Error("a server without enabled should be enabled")
	}
	if c.LSP.Servers["ts"].IsEnabled() {
		t.Error("enabled = false should disable the server")
	}
}

func TestTaskFields(t *testing.T) {
	path := writeFile(t, filepath.Join(t.TempDir(), "config.toml"), `
[[tasks]]
name = "api"
cmd = "dotnet watch"
dir = "src/Api"
kind = "serve"
env = { ASPNETCORE_ENVIRONMENT = "Development" }
`)
	c, ws := load(path)
	if len(ws) != 0 {
		t.Fatalf("unexpected warnings: %v", ws)
	}
	want := Task{Name: "api", Cmd: "dotnet watch", Dir: "src/Api", Kind: "serve",
		Env: map[string]string{"ASPNETCORE_ENVIRONMENT": "Development"}}
	if len(c.Tasks) != 1 || !reflect.DeepEqual(c.Tasks[0], want) {
		t.Errorf("tasks = %+v, want %+v", c.Tasks, want)
	}
}
