// Package runner is deck's "Run" tab: a list of dev tasks (from config or
// auto-detected from the project) that can be started and stopped, with their
// combined stdout/stderr streamed live into a viewport. Each task runs via
// "sh -c" so commands work exactly as typed.
package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"deck/internal/config"
	"deck/internal/ui"
)

// OutputMsg carries one streamed line from a task. Exported so the root model
// can route it to this pane even when another tab is focused, keeping a running
// task's output pumping in the background. gen identifies which run of the job
// produced the line, so output from a superseded run can be dropped.
type OutputMsg struct {
	idx  int
	gen  int
	text string
}

// ExitMsg signals a task finished (err non-nil on failure). Exported for the
// same background-routing reason as OutputMsg.
type ExitMsg struct {
	idx int
	gen int
	err error
}

func (OutputMsg) RouteTo() ui.Dest { return ui.DestRun }

func (ExitMsg) RouteTo() ui.Dest { return ui.DestRun }

// proc is a running task's output channel and cancel handle.
type proc struct {
	ch     chan tea.Msg
	cancel context.CancelFunc
}

// startProc is an indirection over start so pane tests can inject a fake.
var startProc = start

// start launches command under "sh -c", merging stdout+stderr, and streams each
// line as an OutputMsg tagged with idx and gen, then a final ExitMsg. Output
// lines are guaranteed to arrive before the ExitMsg.
func start(idx, gen int, command string) *proc {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan tea.Msg, 256)
	p := &proc{ch: ch, cancel: cancel}

	go func() {
		defer close(ch)
		cmd := exec.CommandContext(ctx, "sh", "-c", command)
		pr, pw := io.Pipe()
		cmd.Stdout = pw
		cmd.Stderr = pw
		if err := cmd.Start(); err != nil {
			ch <- ExitMsg{idx: idx, gen: gen, err: err}
			return
		}
		errc := make(chan error, 1)
		go func() {
			errc <- cmd.Wait()
			_ = pw.Close()
		}()
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			ch <- OutputMsg{idx: idx, gen: gen, text: sc.Text() + "\n"}
		}
		ch <- ExitMsg{idx: idx, gen: gen, err: <-errc}
	}()

	return p
}

// next blocks for this proc's next message, for feeding back into Bubble Tea.
func (p *proc) next() tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-p.ch
		if !ok {
			return nil
		}
		return msg
	}
}

func (p *proc) stop() {
	if p != nil && p.cancel != nil {
		p.cancel()
	}
}

// detect returns sensible default tasks based on marker files in dir, used when
// the user hasn't configured any in ~/.config/deck/config.toml.
func detect(dir string) []config.Task {
	if dir == "" {
		dir = "."
	}
	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	}
	hasGlob := func(pattern string) bool {
		m, _ := globIn(dir, pattern)
		return m
	}

	var tasks []config.Task
	if exists("go.mod") {
		tasks = append(tasks,
			config.Task{Name: "go test", Cmd: "go test ./..."},
			config.Task{Name: "go build", Cmd: "go build ./..."},
		)
	}
	npm := npmScripts(dir)
	tasks = append(tasks, npm...)
	if exists("angular.json") && !hasTask(npm, "npm start", "npm serve") {
		tasks = append(tasks, config.Task{Name: "ng serve", Cmd: "ng serve"})
	}
	if len(npm) == 0 && exists("package.json") && !exists("angular.json") {
		// package.json without a parseable scripts map: fall back to guesses.
		tasks = append(tasks,
			config.Task{Name: "npm dev", Cmd: "npm run dev"},
			config.Task{Name: "npm test", Cmd: "npm test"},
		)
	}
	if hasGlob("*.sln") || hasGlob("*.csproj") {
		tasks = append(tasks,
			config.Task{Name: "dotnet build", Cmd: "dotnet build"},
			config.Task{Name: "dotnet run", Cmd: "dotnet run"},
		)
	}
	if exists("pyproject.toml") || exists("requirements.txt") || exists("pytest.ini") {
		tasks = append(tasks, config.Task{Name: "pytest", Cmd: "pytest"})
	}
	if exists("Cargo.toml") {
		tasks = append(tasks,
			config.Task{Name: "cargo build", Cmd: "cargo build"},
			config.Task{Name: "cargo test", Cmd: "cargo test"},
			config.Task{Name: "cargo run", Cmd: "cargo run"},
		)
	}
	if exists("Gemfile") {
		tasks = append(tasks,
			config.Task{Name: "bundle install", Cmd: "bundle install"},
			config.Task{Name: "rake", Cmd: "bundle exec rake"},
		)
	}
	if exists("Makefile") {
		for _, t := range fileTargets(filepath.Join(dir, "Makefile")) {
			tasks = append(tasks, config.Task{Name: "make " + t, Cmd: "make " + t})
		}
	}
	for _, jf := range []string{"justfile", "Justfile"} {
		if exists(jf) {
			for _, t := range fileTargets(filepath.Join(dir, jf)) {
				tasks = append(tasks, config.Task{Name: "just " + t, Cmd: "just " + t})
			}
			break
		}
	}
	return tasks
}

func hasTask(tasks []config.Task, names ...string) bool {
	for _, t := range tasks {
		if slices.Contains(names, t.Name) {
			return true
		}
	}
	return false
}

// maxNpmScripts caps how many package.json scripts become tasks.
const maxNpmScripts = 8

// npmScripts enumerates package.json's scripts map as "npm run X" tasks,
// sorted by name for a stable list.
func npmScripts(dir string) []config.Task {
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return nil
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal(data, &pkg) != nil || len(pkg.Scripts) == 0 {
		return nil
	}
	names := slices.Sorted(maps.Keys(pkg.Scripts))
	if len(names) > maxNpmScripts {
		names = names[:maxNpmScripts]
	}
	tasks := make([]config.Task, 0, len(names))
	for _, n := range names {
		tasks = append(tasks, config.Task{Name: "npm " + n, Cmd: "npm run " + n})
	}
	return tasks
}

// maxFileTargets caps how many Makefile/justfile targets become tasks.
const maxFileTargets = 10

// targetRe matches a plain top-level rule "name:" at the start of a line. The
// leading alphanumeric excludes special targets (.PHONY), pattern rules (%.o),
// and indented recipe lines; "([^=]|$)" excludes ":=" assignments.
var targetRe = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9_.-]*)\s*:([^=]|$)`)

// fileTargets extracts rule names from a Makefile or justfile, in file order.
func fileTargets(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var targets []string
	seen := map[string]bool{}
	for line := range strings.SplitSeq(string(data), "\n") {
		m := targetRe.FindStringSubmatch(line)
		if m == nil || seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		targets = append(targets, m[1])
		if len(targets) == maxFileTargets {
			break
		}
	}
	return targets
}

func globIn(dir, pattern string) (bool, error) {
	matches, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	for _, m := range matches {
		if ok, _ := matchExt(pattern, m.Name()); ok {
			return true, nil
		}
	}
	return false, nil
}

// matchExt matches simple "*.ext" patterns against a filename.
func matchExt(pattern, name string) (bool, error) {
	if len(pattern) > 1 && pattern[0] == '*' {
		ext := pattern[1:]
		if len(name) >= len(ext) && name[len(name)-len(ext):] == ext {
			return true, nil
		}
		return false, nil
	}
	return pattern == name, nil
}
