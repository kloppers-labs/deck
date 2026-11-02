package editor

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"deck/internal/ui"
)

// ErrUnparseable marks a micro JSON file deck can't read (micro allows comments); it is left untouched.
var ErrUnparseable = errors.New("not plain JSON, left unchanged")

// Features are the optional parts of the bundle that are installed.
type Features struct {
	LSP  bool
	Omni bool
}

const (
	baseStatusL = "$(filename) $(modified)$(overwrite)($(line),$(col)) $(status.paste)| ft:$(opt:filetype) | $(status.branch)"
	lspStatusL  = " | lsp:$(mlsp.status)"
)

// DefaultSettings are the micro settings deck adds when missing.
func DefaultSettings(f Features) map[string]any {
	out := embeddedJSON("runtime/settings.json")
	if !f.Omni {
		for k := range out {
			if strings.HasPrefix(k, "MicroOmni.") {
				delete(out, k)
			}
		}
	}
	out["statusformatl"] = baseStatusL
	if f.LSP {
		out["statusformatl"] = baseStatusL + lspStatusL
	}
	out["statusformatr"] = statusHints(f, ui.Alt)
	return out
}

func statusHints(f Features, alt func(string) string) string {
	hints := []string{alt("q") + " deck", alt("t") + " tree"}
	if f.Omni {
		hints = append(hints, "Ctrl-p files")
	}
	if f.LSP {
		hints = append(hints, alt("k")+" hover", "F12 def")
	}
	return strings.Join(hints, " · ")
}

// staleSettings maps settings to values earlier deck versions wrote, so a sync may replace them.
func staleSettings() map[string]map[string]bool {
	legacyAlt := func(k string) string { return "Alt-" + k }
	hints := map[string]bool{}
	for _, f := range []Features{{}, {LSP: true}, {Omni: true}, {LSP: true, Omni: true}} {
		if old := statusHints(f, legacyAlt); old != statusHints(f, ui.Alt) {
			hints[old] = true
		}
	}
	return map[string]map[string]bool{"statusformatr": hints}
}

// DefaultBindings are deck's micro key bindings, minus those whose plugin isn't installed.
func DefaultBindings(f Features) map[string]any {
	out := embeddedJSON("runtime/bindings.json")
	for k, v := range out {
		action, _ := v.(string)
		omni := strings.HasPrefix(action, "command:Omni")
		lsp := strings.HasPrefix(action, "command:lsp ") || action == "lua:deck.gotoAtMouse"
		if (omni && !f.Omni) || (lsp && !f.LSP) {
			delete(out, k)
		}
	}
	return out
}

func embeddedJSON(name string) map[string]any {
	data, err := fs.ReadFile(runtimeFS, name)
	if err != nil {
		panic(err)
	}
	out := map[string]any{}
	if err := json.Unmarshal(data, &out); err != nil {
		panic(fmt.Sprintf("%s: %v", name, err))
	}
	return out
}

// MergeJSON adds the keys of defaults that path lacks, never changing an existing value.
func MergeJSON(path string, defaults map[string]any) (bool, error) {
	return mergeJSON(path, defaults, nil)
}

// mergeJSON is MergeJSON that also replaces a value listed in stale for its key.
func mergeJSON(path string, defaults map[string]any, stale map[string]map[string]bool) (bool, error) {
	current := map[string]any{}
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return false, err
	default:
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		if err := dec.Decode(&current); err != nil || dec.More() {
			return false, fmt.Errorf("%s: %w", filepath.Base(path), ErrUnparseable)
		}
		if current == nil {
			current = map[string]any{}
		}
	}
	changed := false
	for k, v := range defaults {
		cur, ok := current[k]
		old, isString := cur.(string)
		if !ok || (isString && stale[k][old] && cur != v) {
			current[k] = v
			changed = true
		}
	}
	if !changed {
		return false, nil
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "    ")
	if err := enc.Encode(current); err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, buf.Bytes(), 0o644)
}
