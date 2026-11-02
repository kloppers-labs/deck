package editor

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestCheckHealthy(t *testing.T) {
	issues := Check(CheckEnv{MicroPath: "/bin/micro", MicroVersion: "2.0.15", LookPath: allTools})
	if len(issues) != 0 {
		t.Errorf("a complete setup has no issues, got %q", issues)
	}
}

func TestCheckReportsEachProblem(t *testing.T) {
	issues := Check(CheckEnv{
		MicroPath:    "/bin/micro",
		MicroVersion: "2.0.12",
		LookPath:     fakeLookup(map[string]string{"rg": "/bin/rg"}),
		Missing:      []MissingServer{{Server: "csharp", Binary: "csharp-ls", Hint: "dotnet tool install --global csharp-ls"}},
	})
	joined := strings.Join(issues, "\n")
	for _, want := range []string{"2.0.12", "2.0.14", "fzf", "bat", "csharp-ls", "dotnet tool install --global csharp-ls"} {
		if !strings.Contains(joined, want) {
			t.Errorf("issues should mention %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "rg") {
		t.Errorf("rg is installed and should not be reported:\n%s", joined)
	}
}

func TestCheckMicroMissing(t *testing.T) {
	issues := Check(CheckEnv{LookPath: allTools})
	if len(issues) != 1 || !strings.Contains(issues[0], "micro not found") {
		t.Errorf("issues = %q, want only micro not found", issues)
	}
}

func TestNotifyOncePerDigest(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "micro")
	issues := []string{"fzf not found"}
	if !NotifyOnce(dir, "m1", issues) {
		t.Fatal("the first report should notify")
	}
	if NotifyOnce(dir, "m1", issues) {
		t.Error("the same report should not notify twice")
	}
	if !NotifyOnce(dir, "m2", issues) {
		t.Error("a manifest change should notify again")
	}
	if !NotifyOnce(dir, "m2", slices.Concat(issues, []string{"bat not found"})) {
		t.Error("a new issue should notify again")
	}
}
