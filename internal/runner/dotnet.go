package runner

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/kloppers-labs/deck/internal/config"
)

type projectKind int

const (
	projLib projectKind = iota
	projExe
	projWeb
	projTest
)

const (
	maxDotnetDepth    = 3
	maxDotnetRunnable = 6
	maxProjectBytes   = 256 * 1024
)

var skipDirs = map[string]bool{"bin": true, "obj": true, "node_modules": true, ".git": true}

var (
	sdkWebRe      = regexp.MustCompile(`(?i)Sdk\s*=\s*"Microsoft\.NET\.Sdk\.(Web|BlazorWebAssembly|Razor)"`)
	outputExeRe   = regexp.MustCompile(`(?i)<OutputType>\s*(Win)?Exe\s*</OutputType>`)
	isTestRe      = regexp.MustCompile(`(?i)<IsTestProject>\s*true\s*</IsTestProject>`)
	testPackageRe = regexp.MustCompile(`(?i)<PackageReference\s+Include\s*=\s*"(Microsoft\.NET\.Test\.Sdk|xunit[^"]*|nunit[^"]*|MSTest[^"]*)"`)
)

func classifyProject(xml string) projectKind {
	switch {
	case isTestRe.MatchString(xml) || testPackageRe.MatchString(xml):
		return projTest
	case sdkWebRe.MatchString(xml):
		return projWeb
	case outputExeRe.MatchString(xml):
		return projExe
	}
	return projLib
}

type dotnetProject struct {
	rel  string
	kind projectKind
}

// findDotnet walks at most maxDotnetDepth levels below root, skipping build
// and dependency folders; paths are relative to root.
func findDotnet(root string) (solutions []string, projects []dotnetProject) {
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() {
			if path != root && (skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".") || strings.Count(rel, string(filepath.Separator)) >= maxDotnetDepth) {
				return filepath.SkipDir
			}
			return nil
		}
		switch filepath.Ext(d.Name()) {
		case ".sln", ".slnx":
			if !strings.Contains(rel, string(filepath.Separator)) {
				solutions = append(solutions, rel)
			}
		case ".csproj", ".fsproj":
			projects = append(projects, dotnetProject{rel: rel, kind: classifyProject(readCapped(path))})
		}
		return nil
	})
	slices.Sort(solutions)
	return solutions, projects
}

func readCapped(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	data, _ := io.ReadAll(io.LimitReader(f, maxProjectBytes))
	return string(data)
}

// dotnetTasks builds and tests the root solutions (or each project when there
// is none) and offers run and watch for every web or console project.
func dotnetTasks(root string) ([]config.Task, int) {
	solutions, projects := findDotnet(root)
	if len(solutions) == 0 && len(projects) == 0 {
		return nil, 0
	}
	hasTests := slices.ContainsFunc(projects, func(p dotnetProject) bool { return p.kind == projTest })
	var tasks []config.Task
	for _, sln := range solutions {
		tasks = append(tasks, config.Task{Name: "build " + sln, Cmd: "dotnet build " + sln, Kind: "build"})
		if hasTests {
			tasks = append(tasks, config.Task{Name: "test " + sln, Cmd: "dotnet test " + sln, Kind: "test"})
		}
	}
	if len(solutions) == 0 {
		for _, p := range projects {
			name := projectName(p.rel)
			if p.kind == projTest {
				tasks = append(tasks, config.Task{Name: "test " + name, Cmd: "dotnet test " + p.rel, Kind: "test"})
			} else {
				tasks = append(tasks, config.Task{Name: "build " + name, Cmd: "dotnet build " + p.rel, Kind: "build"})
			}
		}
	}
	more := 0
	runnable := 0
	for _, p := range projects {
		if p.kind != projWeb && p.kind != projExe {
			continue
		}
		if runnable == maxDotnetRunnable {
			more += 2
			continue
		}
		runnable++
		name, dir := projectName(p.rel), projectDir(p.rel)
		run := config.Task{Name: "run " + name, Cmd: "dotnet run --project " + dir, Kind: "run"}
		watch := config.Task{Name: "watch " + name, Cmd: "dotnet watch --project " + dir, Kind: "serve"}
		if p.kind == projWeb {
			tasks = append(tasks, watch, run)
		} else {
			tasks = append(tasks, run, watch)
		}
	}
	return tasks, more
}

func projectName(rel string) string {
	return strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
}

func projectDir(rel string) string {
	return filepath.ToSlash(filepath.Dir(rel))
}
