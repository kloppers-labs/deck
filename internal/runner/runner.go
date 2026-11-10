// Package runner is deck's "Run" tab: configured and detected tasks, each run
// in its own process group, streamed into a searchable log with problem matching.
package runner

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kloppers-labs/deck/internal/config"
	"github.com/kloppers-labs/deck/internal/ui"
)

// OutputMsg carries a batch of output from one run of a task.
type OutputMsg struct {
	idx, gen int
	lines    []chunk
}

// ExitMsg reports that a run finished; code is the exit status, -1 when the
// process did not exit normally.
type ExitMsg struct {
	idx, gen int
	err      error
	code     int
}

// DetectedMsg delivers tasks detected from the project in the background.
type DetectedMsg struct {
	tasks []config.Task
	more  int
}

func (OutputMsg) RouteTo() ui.Dest   { return ui.DestRun }
func (ExitMsg) RouteTo() ui.Dest     { return ui.DestRun }
func (DetectedMsg) RouteTo() ui.Dest { return ui.DestRun }

type detection struct {
	tasks []config.Task
	more  int
}

func detectCmd(dir string) tea.Cmd {
	return func() tea.Msg {
		d := detect(dir)
		return DetectedMsg{tasks: d.tasks, more: d.more}
	}
}

// detect infers tasks from marker files in dir. more counts tasks left out by
// the per-source caps.
func detect(dir string) detection {
	if dir == "" {
		dir = "."
	}
	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	}

	var d detection
	add := func(kind string, tasks ...config.Task) {
		for _, t := range tasks {
			if t.Kind == "" {
				t.Kind = kind
			}
			d.tasks = append(d.tasks, t)
		}
	}
	if exists("go.mod") {
		add("test", config.Task{Name: "go test", Cmd: "go test ./..."})
		add("build", config.Task{Name: "go build", Cmd: "go build ./..."})
	}
	pm := packageManager(exists)
	scripts, hidden := npmScripts(dir, pm)
	d.more += hidden
	add("run", scripts...)
	if exists("angular.json") && !slices.ContainsFunc(scripts, isServeScript) {
		add("serve", config.Task{Name: "ng serve", Cmd: "ng serve"})
	}
	if len(scripts) == 0 && exists("package.json") && !exists("angular.json") {
		add("serve", config.Task{Name: pm + " dev", Cmd: pm + " run dev"})
		add("test", config.Task{Name: pm + " test", Cmd: pm + " test"})
	}
	dotnet, more := dotnetTasks(dir)
	d.more += more
	add("build", dotnet...)
	if exists("pyproject.toml") || exists("requirements.txt") || exists("pytest.ini") {
		add("test", config.Task{Name: "pytest", Cmd: "pytest"})
	}
	if exists("Cargo.toml") {
		add("build", config.Task{Name: "cargo build", Cmd: "cargo build"})
		add("test", config.Task{Name: "cargo test", Cmd: "cargo test"})
		add("run", config.Task{Name: "cargo run", Cmd: "cargo run"})
	}
	if exists("Gemfile") {
		add("build", config.Task{Name: "bundle install", Cmd: "bundle install"})
		add("test", config.Task{Name: "rake", Cmd: "bundle exec rake"})
	}
	if exists("Makefile") {
		targets, hidden := fileTargets(filepath.Join(dir, "Makefile"))
		d.more += hidden
		for _, t := range targets {
			add("run", config.Task{Name: "make " + t, Cmd: "make " + t})
		}
	}
	for _, jf := range []string{"justfile", "Justfile"} {
		if exists(jf) {
			targets, hidden := fileTargets(filepath.Join(dir, jf))
			d.more += hidden
			for _, t := range targets {
				add("run", config.Task{Name: "just " + t, Cmd: "just " + t})
			}
			break
		}
	}
	return d
}

// mergeDetected drops detected tasks whose name or command is already
// configured.
func mergeDetected(configured, detected []config.Task) []config.Task {
	var out []config.Task
	for _, t := range detected {
		dup := slices.ContainsFunc(configured, func(c config.Task) bool {
			return c.Name == t.Name || strings.TrimSpace(c.Cmd) == strings.TrimSpace(t.Cmd)
		})
		if !dup {
			out = append(out, t)
		}
	}
	return out
}

func isServeScript(t config.Task) bool {
	return strings.HasSuffix(t.Name, " start") || strings.HasSuffix(t.Name, " serve")
}

func packageManager(exists func(string) bool) string {
	switch {
	case exists("pnpm-lock.yaml"):
		return "pnpm"
	case exists("yarn.lock"):
		return "yarn"
	case exists("bun.lockb"), exists("bun.lock"):
		return "bun"
	}
	return "npm"
}

const maxNpmScripts = 8

// npmScripts lists package.json scripts as tasks for the package manager,
// sorted by name, and how many the cap left out.
func npmScripts(dir, pm string) ([]config.Task, int) {
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return nil, 0
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal(data, &pkg) != nil || len(pkg.Scripts) == 0 {
		return nil, 0
	}
	names := slices.Sorted(maps.Keys(pkg.Scripts))
	hidden := max(len(names)-maxNpmScripts, 0)
	names = names[:len(names)-hidden]
	tasks := make([]config.Task, 0, len(names))
	for _, n := range names {
		tasks = append(tasks, config.Task{Name: pm + " " + n, Cmd: pm + " run " + n})
	}
	return tasks, hidden
}

const maxFileTargets = 10

// targetRe matches a plain top-level rule "name:"; the leading alphanumeric
// excludes .PHONY, pattern rules and recipe lines, "([^=]|$)" excludes ":=".
var targetRe = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9_.-]*)\s*:([^=]|$)`)

// fileTargets extracts rule names from a Makefile or justfile in file order,
// and how many the cap left out.
func fileTargets(path string) ([]string, int) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0
	}
	var targets []string
	hidden := 0
	seen := map[string]bool{}
	for line := range strings.SplitSeq(string(data), "\n") {
		m := targetRe.FindStringSubmatch(line)
		if m == nil || seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		if len(targets) == maxFileTargets {
			hidden++
			continue
		}
		targets = append(targets, m[1])
	}
	return targets, hidden
}
