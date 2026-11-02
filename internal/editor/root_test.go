package editor

import (
	"os"
	"path/filepath"
	"testing"
)

func layout(t *testing.T, files ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, f := range files {
		p := filepath.Join(root, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if filepath.Base(f) == ".git" {
			if err := os.Mkdir(p, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestProjectRootPrecedence(t *testing.T) {
	cases := []struct {
		name  string
		files []string
		file  string
		cwd   string
		want  string
	}{
		{"solution beats nearer go.mod", []string{"App.sln", "svc/go.mod", "svc/x/main.go"}, "svc/x/main.go", "", ""},
		{"slnx counts as a solution", []string{"src/App.slnx", "src/Api/Program.cs", "src/Api/Api.csproj"}, "src/Api/Program.cs", "", "src"},
		{"go.mod beats .git", []string{".git", "svc/go.mod", "svc/x/main.go"}, "svc/x/main.go", "", "svc"},
		{"nearest of tsconfig and package.json", []string{"package.json", "web/tsconfig.json", "web/src/a.ts"}, "web/src/a.ts", "", "web"},
		{".git when nothing else", []string{"repo/.git", "repo/docs/a.md"}, "repo/docs/a.md", "", "repo"},
		{"nothing found falls back to cwd", []string{"a/b/c.txt"}, "a/b/c.txt", "", ""},
		{"never above the cwd", []string{"App.sln", "sub/go.mod", "sub/deep/main.go"}, "sub/deep/main.go", "sub/deep", "sub/deep"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := layout(t, c.files...)
			cwd := filepath.Join(root, c.cwd)
			got := ProjectRoot(filepath.Join(root, c.file), cwd)
			if want := filepath.Join(root, c.want); got != want {
				t.Errorf("ProjectRoot = %q, want %q", got, want)
			}
		})
	}
}

func TestProjectRootOutsideCwd(t *testing.T) {
	root := layout(t, "elsewhere/go.mod", "elsewhere/a.go", "here/x.txt")
	got := ProjectRoot(filepath.Join(root, "elsewhere", "a.go"), filepath.Join(root, "here"))
	if want := filepath.Join(root, "elsewhere"); got != want {
		t.Errorf("a file outside the cwd opens in its own directory, got %q want %q", got, want)
	}
}

func TestCmdDirIsProjectRoot(t *testing.T) {
	root := layout(t, "App.sln", "src/Api/Program.cs")
	file := filepath.Join(root, "src", "Api", "Program.cs")
	c, _, _ := Launch("micro", Target{Path: file, Line: 1}, Opts{Cwd: root})
	if c.Dir != root {
		t.Errorf("micro Dir = %q, want the project root %q (mlsp takes rootUri from it)", c.Dir, root)
	}
}
