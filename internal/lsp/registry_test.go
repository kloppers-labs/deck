package lsp

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func touch(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func realTemp(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestFindRootSlnBeatsCsproj(t *testing.T) {
	repo := realTemp(t)
	file := filepath.Join(repo, "src", "Foo.Api", "Controllers", "Home.cs")
	touch(t, filepath.Join(repo, "Foo.sln"), filepath.Join(repo, "src", "Foo.Api", "Foo.Api.csproj"), file)
	root, match, ok := FindRoot(file, []string{"*.sln", "*.slnx", "*.csproj"}, repo)
	if !ok || root != repo || match != filepath.Join(repo, "Foo.sln") {
		t.Fatalf("FindRoot = %q, %q, %v", root, match, ok)
	}
	root, match, ok = FindRoot(file, []string{"*.csproj"}, repo)
	if !ok || root != filepath.Join(repo, "src", "Foo.Api") || filepath.Base(match) != "Foo.Api.csproj" {
		t.Fatalf("csproj FindRoot = %q, %q, %v", root, match, ok)
	}
}

func TestGoWorkBeatsGoMod(t *testing.T) {
	repo := realTemp(t)
	file := filepath.Join(repo, "svc", "cmd", "main.go")
	touch(t, filepath.Join(repo, "go.work"), filepath.Join(repo, "svc", "go.mod"), file)
	root, match, ok := FindRoot(file, []string{"go.work", "go.mod"}, repo)
	if !ok || root != repo || filepath.Base(match) != "go.work" {
		t.Fatalf("FindRoot = %q, %q, %v", root, match, ok)
	}
}

func TestMultipleSlnTieBreak(t *testing.T) {
	base := realTemp(t)
	named := filepath.Join(base, "Shop")
	touch(t, filepath.Join(named, "Alpha.sln"), filepath.Join(named, "Shop.sln"), filepath.Join(named, "Zed.sln"), filepath.Join(named, "a.cs"))
	if _, match, _ := FindRoot(filepath.Join(named, "a.cs"), []string{"*.sln"}, base); filepath.Base(match) != "Shop.sln" {
		t.Fatalf("match = %q, want Shop.sln", match)
	}
	other := filepath.Join(base, "Other")
	touch(t, filepath.Join(other, "Zed.sln"), filepath.Join(other, "Beta.sln"), filepath.Join(other, "a.cs"))
	if _, match, _ := FindRoot(filepath.Join(other, "a.cs"), []string{"*.sln"}, base); filepath.Base(match) != "Beta.sln" {
		t.Fatalf("match = %q, want Beta.sln", match)
	}
}

func TestBoundaryStops(t *testing.T) {
	outer := realTemp(t)
	repo := filepath.Join(outer, "repo")
	file := filepath.Join(repo, "pkg", "a.go")
	touch(t, filepath.Join(outer, "go.mod"), filepath.Join(repo, ".git", "HEAD"), file)
	if _, _, ok := FindRoot(file, []string{"go.mod"}, repo); ok {
		t.Fatal("FindRoot crossed the boundary")
	}
	r := NewRegistry(DefaultSpecs(), Env{Home: outer})
	if got := r.Boundary(file); got != repo {
		t.Fatalf("Boundary = %q, want git top level %q", got, repo)
	}
	loose := filepath.Join(outer, "loose", "deep", "b.go")
	touch(t, loose)
	if got := r.Boundary(loose); got != outer {
		t.Fatalf("Boundary without git = %q, want home %q", got, outer)
	}
	if got := NewRegistry(nil, Env{Home: "/nonexistent-home"}).Boundary("/a/b/c.go"); got != "/a" {
		t.Fatalf("Boundary outside home = %q, want /a (never /)", got)
	}
}

func TestMergeOverDefaults(t *testing.T) {
	off := false
	merged := MergeSpecs(DefaultSpecs(), map[string]ServerOverride{
		"csharp":     {Command: []string{"/opt/csharp-ls", "--loglevel", "info"}, Solution: "Big.sln"},
		"typescript": {Enabled: &off},
		"go":         {RootMarkers: []string{"go.mod"}},
		"rust":       {Command: []string{"rust-analyzer"}, Extensions: []string{".rs"}, RootMarkers: []string{"Cargo.toml"}},
	})
	byName := map[string]ServerSpec{}
	var names []string
	for _, s := range merged {
		byName[s.Name] = s
		names = append(names, s.Name)
	}
	if want := []string{"csharp", "go", "typescript", "rust"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("order = %v, want %v", names, want)
	}
	cs := byName["csharp"]
	if cs.Command[0] != "/opt/csharp-ls" || cs.Solution != "Big.sln" || !cs.Enabled ||
		!reflect.DeepEqual(cs.Extensions, []string{".cs", ".csx"}) || cs.SolutionFlag != "--solution" {
		t.Fatalf("csharp = %+v", cs)
	}
	if byName["typescript"].Enabled {
		t.Fatal("typescript should be disabled")
	}
	if g := byName["go"]; !reflect.DeepEqual(g.RootMarkers, []string{"go.mod"}) || g.Command[0] != "gopls" {
		t.Fatalf("go = %+v", g)
	}
	if rs := byName["rust"]; !rs.Enabled || rs.Command[0] != "rust-analyzer" || rs.Extensions[0] != ".rs" {
		t.Fatalf("rust = %+v", rs)
	}
	if DefaultSpecs()[0].Solution != "" {
		t.Fatal("MergeSpecs mutated the defaults")
	}
}

func TestBinaryFallbackDirs(t *testing.T) {
	home := realTemp(t)
	gopath := realTemp(t)
	notFound := func(string) (string, error) { return "", exec.ErrNotFound }
	place := func(dir, name string) string {
		p := filepath.Join(dir, name)
		touch(t, p)
		if err := os.Chmod(p, 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	dotnet := place(filepath.Join(home, ".dotnet", "tools"), "deck-test-csharp-ls")
	gobin := place(filepath.Join(home, "go", "bin"), "deck-test-gopls")
	gp := place(filepath.Join(gopath, "bin"), "deck-test-other")
	touch(t, filepath.Join(home, "go", "bin", "deck-test-noexec"))

	r := NewRegistry(nil, Env{LookPath: notFound, Home: home, GOPATH: gopath})
	for name, want := range map[string]string{
		"deck-test-csharp-ls": dotnet,
		"deck-test-gopls":     gobin,
		"deck-test-other":     gp,
	} {
		if got, err := r.LookupBinary(name); err != nil || got != want {
			t.Errorf("LookupBinary(%s) = %q, %v, want %q", name, got, err, want)
		}
	}
	if _, err := r.LookupBinary("deck-test-noexec"); err == nil {
		t.Error("non-executable file accepted")
	}
	if got, err := r.LookupBinary(dotnet); err != nil || got != dotnet {
		t.Errorf("absolute path = %q, %v", got, err)
	}

	onPath := NewRegistry(nil, Env{LookPath: func(n string) (string, error) { return "/usr/bin/" + n, nil }, Home: home})
	if got, _ := onPath.LookupBinary("gopls"); got != "/usr/bin/gopls" {
		t.Errorf("PATH lookup = %q", got)
	}
}

func TestResolveLanguageID(t *testing.T) {
	repo := realTemp(t)
	touch(t, filepath.Join(repo, ".git", "HEAD"), filepath.Join(repo, "tsconfig.json"), filepath.Join(repo, "go.mod"))
	r := NewRegistry(DefaultSpecs(), Env{LookPath: func(n string) (string, error) { return "/bin/" + n, nil }, Home: repo})
	for file, want := range map[string]string{
		"app.tsx": "typescriptreact", "app.ts": "typescript", "a.jsx": "javascriptreact",
		"a.mjs": "javascript", "a.cjs": "javascript", "a.js": "javascript", "main.go": "go",
	} {
		res, err := r.Resolve(filepath.Join(repo, "src", file))
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		if res.LanguageID != want || res.Root != repo {
			t.Errorf("%s: languageID=%q root=%q, want %q %q", file, res.LanguageID, res.Root, want, repo)
		}
	}
	if got := LanguageID("/x/Program.cs"); got != "csharp" {
		t.Errorf("LanguageID(.cs) = %q", got)
	}
	var noServer *NoServerError
	if _, err := r.Resolve(filepath.Join(repo, "notes.txt")); !errors.As(err, &noServer) ||
		err.Error() != "no language server for .txt" {
		t.Fatalf("txt err = %v", err)
	}
}

func TestResolveCSharpSolutionArgs(t *testing.T) {
	repo := realTemp(t)
	file := filepath.Join(repo, "src", "Api", "Program.cs")
	touch(t, filepath.Join(repo, ".git", "HEAD"), filepath.Join(repo, "Shop.sln"), filepath.Join(repo, "src", "Api", "Api.csproj"), file)
	r := NewRegistry(DefaultSpecs(), Env{LookPath: func(n string) (string, error) { return "/bin/" + n, nil }, Home: repo})
	res, err := r.Resolve(file)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--features", "metadata-uris", "--solution", filepath.Join(repo, "Shop.sln")}
	if res.Binary != "/bin/csharp-ls" || res.Root != repo || !reflect.DeepEqual(res.Args, want) {
		t.Fatalf("resolved = %+v", res)
	}

	other := filepath.Join(repo, "other", "Big.slnx")
	touch(t, other)
	specs := MergeSpecs(DefaultSpecs(), map[string]ServerOverride{"csharp": {Solution: other}})
	res, err = NewRegistry(specs, Env{LookPath: func(n string) (string, error) { return "/bin/" + n, nil }, Home: repo}).Resolve(file)
	if err != nil {
		t.Fatal(err)
	}
	if res.Root != filepath.Dir(other) || res.Args[len(res.Args)-1] != other {
		t.Fatalf("override = %+v", res)
	}
}

func TestResolveMissingBinary(t *testing.T) {
	repo := realTemp(t)
	r := NewRegistry(DefaultSpecs(), Env{LookPath: func(string) (string, error) { return "", exec.ErrNotFound }, Home: repo})
	_, err := r.Resolve(filepath.Join(repo, "a.cs"))
	var missing *MissingBinaryError
	if !errors.As(err, &missing) || missing.Binary != "csharp-ls" || !strings.Contains(missing.Hint, "dotnet tool install") {
		t.Fatalf("err = %#v", err)
	}
	if err.Error() != "csharp-ls not found" {
		t.Fatalf("message = %q", err.Error())
	}
}
