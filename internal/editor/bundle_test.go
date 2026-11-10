package editor

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kloppers-labs/deck/internal/lsp"
	"github.com/kloppers-labs/deck/internal/ui"
)

func bundleConfig(dir string) BundleConfig {
	return BundleConfig{
		Editor:       "micro",
		Dir:          dir,
		LSP:          true,
		Specs:        lsp.DefaultSpecs(),
		LookPath:     fakeLookup(map[string]string{"micro": "/bin/micro", "fzf": "x", "rg": "x"}),
		LookupServer: fakeLookup(map[string]string{"gopls": "/go/bin/gopls", "csharp-ls": "/x/csharp-ls"}),
		Version:      func(string) string { return "2.0.15" },
	}
}

func TestRunSyncReportsIssuesOnce(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "micro")
	msg := RunSync(bundleConfig(dir))
	if msg.Err != nil {
		t.Fatal(msg.Err)
	}
	if msg.RouteTo() != ui.DestApp {
		t.Error("SyncedMsg belongs to the app")
	}
	joined := strings.Join(msg.Issues, "\n")
	if !msg.Notify || !strings.Contains(joined, "bat") || !strings.Contains(joined, "typescript-language-server") {
		t.Errorf("first sync should notify about bat and typescript: %+v", msg)
	}
	if !msg.Features.LSP || msg.Features.Omni {
		t.Errorf("features = %+v, want LSP without Omni (bat missing)", msg.Features)
	}
	if again := RunSync(bundleConfig(dir)); again.Notify {
		t.Error("the same issues should not be shown twice")
	}
	if !fileExists(filepath.Join(dir, "plug", "mlsp", "z_deck.lua")) {
		t.Error("z_deck.lua should be installed")
	}
}

func TestRunSyncWithoutMicro(t *testing.T) {
	c := bundleConfig(filepath.Join(t.TempDir(), "micro"))
	c.LookPath = fakeLookup(nil)
	msg := RunSync(c)
	if len(msg.Issues) != 1 || !strings.Contains(msg.Issues[0], "micro not found") || !msg.Notify {
		t.Errorf("missing micro should be reported once: %+v", msg)
	}
	if fileExists(filepath.Join(c.Dir, "settings.json")) {
		t.Error("nothing should be installed without micro")
	}
}

func TestRunSyncLSPOffIgnoresMissingServers(t *testing.T) {
	c := bundleConfig(filepath.Join(t.TempDir(), "micro"))
	c.LSP = false
	msg := RunSync(c)
	if strings.Contains(strings.Join(msg.Issues, "\n"), "LSP") {
		t.Errorf("with micro_lsp off missing servers are not an issue: %q", msg.Issues)
	}
}

func TestQuietResyncRecordsWithoutNotifying(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "micro")
	c := bundleConfig(dir)
	c.Quiet = true
	if msg := RunSync(c); msg.Err != nil || msg.Notify || len(msg.Issues) == 0 {
		t.Fatalf("a quiet sync reports issues without notifying: %+v", msg)
	}
	if again := RunSync(bundleConfig(dir)); again.Notify {
		t.Error("issues seen by a quiet sync should not be shown later")
	}
	if (ResyncMsg{}).RouteTo() != ui.DestApp {
		t.Error("ResyncMsg belongs to the app")
	}
}
