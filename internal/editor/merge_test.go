package editor

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"deck/internal/ui"
)

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return m
}

func writeJSON(t *testing.T, path string, v map[string]any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMergeAddsMissingOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	seed := `{"colorscheme": "dracula", "softwrap": true, "tabsize": 2, "mine": "keep <me> & co"}`
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := MergeJSON(path, DefaultSettings(Features{LSP: true, Omni: true}))
	if err != nil || !changed {
		t.Fatalf("MergeJSON = %v, %v; want a change", changed, err)
	}
	got := readJSON(t, path)
	if got["softwrap"] != true || got["tabsize"] != float64(2) || got["mine"] != "keep <me> & co" {
		t.Errorf("existing values must survive: %v", got)
	}
	if got["mouse"] != true || got["linter"] != false || got["reload"] != "auto" {
		t.Errorf("missing defaults should be added: %v", got)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), `"keep <me> & co"`) {
		t.Errorf("values should not be HTML-escaped: %s", raw)
	}

	changed, err = MergeJSON(path, DefaultSettings(Features{LSP: true, Omni: true}))
	if err != nil || changed {
		t.Errorf("a second merge should change nothing, got %v, %v", changed, err)
	}
}

func TestMergeCreatesMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bindings.json")
	changed, err := MergeJSON(path, map[string]any{"Alt-q": "QuitAll"})
	if err != nil || !changed {
		t.Fatalf("MergeJSON = %v, %v", changed, err)
	}
	if got := readJSON(t, path); got["Alt-q"] != "QuitAll" {
		t.Errorf("bindings = %v", got)
	}
}

func TestMergeLeavesUnparseableFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	body := "// my settings\n{\"softwrap\": true,}\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := MergeJSON(path, DefaultSettings(Features{}))
	if changed || !errors.Is(err, ErrUnparseable) {
		t.Errorf("MergeJSON = %v, %v; want unchanged and ErrUnparseable", changed, err)
	}
	if got, _ := os.ReadFile(path); string(got) != body {
		t.Errorf("an unparseable file must be left alone, got %q", got)
	}
}

func TestBindingsOmittedWhenToolMissing(t *testing.T) {
	full := DefaultBindings(Features{LSP: true, Omni: true})
	for _, k := range []string{"Alt-q", "F12", "Alt-d", "Alt-k", "Ctrl-p", "Alt-F", "Alt-t", "Alt-MouseLeft", "CtrlSpace"} {
		if _, ok := full[k]; !ok {
			t.Errorf("full bindings are missing %s", k)
		}
	}
	noOmni := DefaultBindings(Features{LSP: true})
	for _, k := range []string{"Ctrl-p", "Alt-F", "Alt-l", "Alt-j", "Alt-{", "Alt-}"} {
		if _, ok := noOmni[k]; ok {
			t.Errorf("without MicroOmni %s should be left out", k)
		}
	}
	if noOmni["F12"] != "command:lsp goto-definition" {
		t.Errorf("LSP bindings should stay without MicroOmni: %v", noOmni)
	}
	noLSP := DefaultBindings(Features{Omni: true})
	for _, k := range []string{"F12", "Alt-d", "Alt-k", "F2", "CtrlSpace", "Alt-MouseLeft"} {
		if _, ok := noLSP[k]; ok {
			t.Errorf("without mlsp %s should be left out", k)
		}
	}
	if noLSP["Alt-q"] != "QuitAll" || noLSP["Alt-t"] != "command:tree" {
		t.Errorf("deck and tree bindings are always present: %v", noLSP)
	}
}

func TestSettingsFollowFeatures(t *testing.T) {
	full := DefaultSettings(Features{LSP: true, Omni: true})
	if full["MicroOmni.AutoSaveEnabled"] != false {
		t.Error("MicroOmni autosave should default to off")
	}
	if l, _ := full["statusformatl"].(string); !strings.Contains(l, "$(status.branch)") || !strings.Contains(l, "lsp:$(mlsp.status)") {
		t.Errorf("statusformatl = %q", l)
	}
	if r, want := full["statusformatr"], ui.Alt("q")+" deck · "+ui.Alt("t")+" tree · Ctrl-p files · "+ui.Alt("k")+" hover · F12 def"; r != want {
		t.Errorf("statusformatr = %q", r)
	}
	bare := DefaultSettings(Features{})
	if _, ok := bare["MicroOmni.AutoSaveEnabled"]; ok {
		t.Error("MicroOmni options belong only with MicroOmni")
	}
	if l, _ := bare["statusformatl"].(string); strings.Contains(l, "mlsp") {
		t.Errorf("statusformatl without mlsp = %q", l)
	}
	if r, want := bare["statusformatr"], ui.Alt("q")+" deck · "+ui.Alt("t")+" tree"; r != want {
		t.Errorf("statusformatr without tools = %q", r)
	}
}

func TestSyncReplacesStaleStatusHintsOnly(t *testing.T) {
	if ui.Alt("q") == "Alt-q" {
		t.Skip("labels unchanged on this platform")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	f := Features{LSP: true, Omni: true}
	writeJSON(t, path, map[string]any{"statusformatr": "Alt-q deck · Alt-t tree · Ctrl-p files · Alt-k hover · F12 def"})
	if _, err := mergeJSON(path, DefaultSettings(f), staleSettings()); err != nil {
		t.Fatal(err)
	}
	if got := readJSON(t, path)["statusformatr"]; got != statusHints(f, ui.Alt) {
		t.Errorf("stale deck hints should be replaced, got %q", got)
	}

	writeJSON(t, path, map[string]any{"statusformatr": "my own status"})
	if _, err := mergeJSON(path, DefaultSettings(f), staleSettings()); err != nil {
		t.Fatal(err)
	}
	if got := readJSON(t, path)["statusformatr"]; got != "my own status" {
		t.Errorf("a user's own status line must be kept, got %q", got)
	}
}
