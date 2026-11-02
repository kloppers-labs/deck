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
	hints := []string{"Alt-q deck", "Alt-t tree"}
	if f.Omni {
		hints = append(hints, "Ctrl-p files")
	}
	if f.LSP {
		hints = append(hints, "Alt-k hover", "F12 def")
	}
	out["statusformatr"] = strings.Join(hints, " · ")
	return out
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
		if _, ok := current[k]; !ok {
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
