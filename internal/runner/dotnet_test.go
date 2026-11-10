package runner

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/kloppers-labs/deck/internal/config"
)

const (
	webProj  = `<Project Sdk="Microsoft.NET.Sdk.Web"><PropertyGroup><TargetFramework>net9.0</TargetFramework></PropertyGroup></Project>`
	exeProj  = `<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><OutputType>Exe</OutputType></PropertyGroup></Project>`
	libProj  = `<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>net9.0</TargetFramework></PropertyGroup></Project>`
	testProj = `<Project Sdk="Microsoft.NET.Sdk"><ItemGroup><PackageReference Include="Microsoft.NET.Test.Sdk" Version="17.0.0" /></ItemGroup></Project>`
)

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func cmds(tasks []config.Task) []string {
	out := make([]string, len(tasks))
	for i, t := range tasks {
		out[i] = t.Cmd
	}
	return out
}

func TestClassifyProject(t *testing.T) {
	cases := []struct {
		xml  string
		want projectKind
	}{
		{webProj, projWeb},
		{exeProj, projExe},
		{libProj, projLib},
		{testProj, projTest},
		{`<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><IsTestProject>true</IsTestProject></PropertyGroup></Project>`, projTest},
		{`<Project><ItemGroup><PackageReference Include="xunit" /></ItemGroup></Project>`, projTest},
		{`<Project><ItemGroup><PackageReference Include="NUnit" /></ItemGroup></Project>`, projTest},
		{`<Project><ItemGroup><PackageReference Include="MSTest.TestFramework" /></ItemGroup></Project>`, projTest},
		{`<Project Sdk="Microsoft.NET.Sdk.Worker"><PropertyGroup><OutputType>exe</OutputType></PropertyGroup></Project>`, projExe},
	}
	for i, c := range cases {
		if got := classifyProject(c.xml); got != c.want {
			t.Errorf("case %d: classify = %v, want %v", i, got, c.want)
		}
	}
}

func TestDotnetSlnAtRootProjectsUnderSrc(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "Shop.sln", "")
	writeFile(t, root, "src/Shop.Api/Shop.Api.csproj", webProj)
	writeFile(t, root, "src/Shop.Domain/Shop.Domain.csproj", libProj)
	writeFile(t, root, "src/Shop.Worker/Shop.Worker.csproj", exeProj)
	writeFile(t, root, "tests/Shop.Tests/Shop.Tests.csproj", testProj)
	got, more := dotnetTasks(root)
	want := []string{
		"dotnet build Shop.sln",
		"dotnet test Shop.sln",
		"dotnet watch --project src/Shop.Api",
		"dotnet run --project src/Shop.Api",
		"dotnet run --project src/Shop.Worker",
		"dotnet watch --project src/Shop.Worker",
	}
	if !slices.Equal(cmds(got), want) || more != 0 {
		t.Errorf("tasks = %q (+%d), want %q", cmds(got), more, want)
	}
	if got[0].Kind != "build" || got[1].Kind != "test" || got[2].Kind != "serve" {
		t.Errorf("kinds = %q %q %q", got[0].Kind, got[1].Kind, got[2].Kind)
	}
}

func TestDotnetSlnx(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "App.slnx", "<Solution/>")
	writeFile(t, root, "App/App.csproj", libProj)
	got, _ := dotnetTasks(root)
	if len(got) == 0 || got[0].Cmd != "dotnet build App.slnx" {
		t.Errorf("tasks = %q, want a build of App.slnx", cmds(got))
	}
}

func TestDotnetWithoutSolution(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "Api.csproj", webProj)
	got, _ := dotnetTasks(root)
	want := []string{"dotnet build Api.csproj", "dotnet watch --project .", "dotnet run --project ."}
	if !slices.Equal(cmds(got), want) {
		t.Errorf("tasks = %q, want %q", cmds(got), want)
	}
}

func TestDotnetDepthLimitAndSkippedDirs(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a/b/c/Deep.csproj", exeProj)
	writeFile(t, root, "a/b/c/d/TooDeep.csproj", exeProj)
	for _, skip := range []string{"bin", "obj", "node_modules", ".git"} {
		writeFile(t, root, skip+"/Hidden.csproj", exeProj)
	}
	got, _ := dotnetTasks(root)
	for _, c := range cmds(got) {
		if c == "dotnet run --project a/b/c/d" || slices.Contains([]string{"bin", "obj", "node_modules", ".git"}, filepath.Base(c)) {
			t.Errorf("unexpected task %q", c)
		}
	}
	if !slices.Contains(cmds(got), "dotnet run --project a/b/c") {
		t.Errorf("tasks = %q, want the project three levels down", cmds(got))
	}
	if len(got) != 3 {
		t.Errorf("tasks = %q, want only Deep's build/run/watch", cmds(got))
	}
}

func TestDotnetCapReportsMore(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "Big.sln", "")
	for _, n := range []string{"A", "B", "C", "D", "E", "F", "G", "H"} {
		writeFile(t, root, "src/"+n+"/"+n+".csproj", webProj)
	}
	got, more := dotnetTasks(root)
	if len(got) != 1+2*maxDotnetRunnable || more != 2*(8-maxDotnetRunnable) {
		t.Errorf("got %d tasks +%d more, want %d +%d", len(got), more, 1+2*maxDotnetRunnable, 2*(8-maxDotnetRunnable))
	}
}
