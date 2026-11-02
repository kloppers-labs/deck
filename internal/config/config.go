// Package config loads deck's TOML settings: the global file under
// $XDG_CONFIG_HOME/deck, then a project-local .deck.toml merged over it.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// ProjectFile is the project-local config, looked up at the repository root.
const ProjectFile = ".deck.toml"

// Config holds user-tunable settings for deck. AIBackend and AIModel select the
// CLI used by the Git tab's ctrl+g commit-message helper.
type Config struct {
	AIBackend string `toml:"ai_backend"`
	AIModel   string `toml:"ai_model"`
	// Editor may carry arguments, e.g. "micro -softwrap false" or "code -w";
	// the file path is appended last.
	Editor string `toml:"editor"`
	// Theme is the chroma/glamour style used for code, diffs and HTTP bodies.
	Theme        string `toml:"theme"`
	Mouse        string `toml:"mouse"`
	EditorReturn string `toml:"editor_return"`
	MicroBundle  bool   `toml:"micro_bundle"`
	MicroLSP     string `toml:"micro_lsp"`
	Files        Files  `toml:"files"`
	Viewer       Viewer `toml:"viewer"`
	LSP          LSP    `toml:"lsp"`
	Git          Git    `toml:"git"`
	Tasks        []Task `toml:"tasks"`
}

type Files struct {
	SidebarWidth int      `toml:"sidebar_width"`
	ShowHidden   bool     `toml:"show_hidden"`
	Hide         []string `toml:"hide"`
	Icons        string   `toml:"icons"`
	InfoPanel    bool     `toml:"info_panel"`
}

type Viewer struct {
	TabWidth          int      `toml:"tab_width"`
	Wrap              bool     `toml:"wrap"`
	LineNumbers       bool     `toml:"line_numbers"`
	ScrollLines       int      `toml:"scroll_lines"`
	HoverDelayMS      int      `toml:"hover_delay_ms"`
	GotoClick         []string `toml:"goto_click"`
	MaxHighlightBytes int      `toml:"max_highlight_bytes"`
}

// LSP configures language servers. Built-in server definitions live in the lsp
// package; Servers only overrides or adds to them.
type LSP struct {
	Enabled          bool                 `toml:"enabled"`
	Prestart         bool                 `toml:"prestart"`
	RequestTimeoutMS int                  `toml:"request_timeout_ms"`
	Servers          map[string]LSPServer `toml:"servers"`
}

type LSPServer struct {
	Command     []string `toml:"command"`
	RootMarkers []string `toml:"root_markers"`
	Solution    string   `toml:"solution"`
	Enabled     *bool    `toml:"enabled"`
}

// IsEnabled treats an omitted enabled key as true.
func (s LSPServer) IsEnabled() bool { return s.Enabled == nil || *s.Enabled }

type Git struct {
	ConfirmCommit bool `toml:"confirm_commit"`
}

// Task is a named dev command for the Run tab. Cmd runs via "sh -c" in Dir
// (relative to the project root) with Env added to the environment.
type Task struct {
	Name string            `toml:"name"`
	Cmd  string            `toml:"cmd"`
	Dir  string            `toml:"dir"`
	Env  map[string]string `toml:"env"`
	Kind string            `toml:"kind"`
}

// Warning is a non-fatal problem found while loading a config file.
type Warning struct {
	Path    string
	Message string
}

func (w Warning) String() string {
	if w.Path == "" {
		return w.Message
	}
	return filepath.Base(w.Path) + ": " + w.Message
}

// Default is the single source of deck's default settings.
func Default() Config {
	return Config{
		AIBackend:    "claude",
		AIModel:      "sonnet",
		Editor:       "micro",
		Theme:        "dracula",
		Mouse:        "hover",
		EditorReturn: "follow",
		MicroBundle:  true,
		MicroLSP:     "on",
		Files: Files{
			SidebarWidth: 32,
			Hide:         []string{"bin", "obj", ".git", "node_modules", ".vs", ".idea", "TestResults"},
			Icons:        "unicode",
		},
		Viewer: Viewer{
			TabWidth:          4,
			LineNumbers:       true,
			ScrollLines:       3,
			HoverDelayMS:      350,
			GotoClick:         []string{"ctrl", "double", "right"},
			MaxHighlightBytes: 2_000_000,
		},
		LSP: LSP{
			Enabled:          true,
			Prestart:         true,
			RequestTimeoutMS: 8000,
		},
		Git: Git{ConfirmCommit: true},
	}
}

// Path returns $XDG_CONFIG_HOME/deck/config.toml, or ~/.config/deck/config.toml.
func Path() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "deck", "config.toml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "deck-config.toml"
	}
	return filepath.Join(home, ".config", "deck", "config.toml")
}

// Load reads the global config and the project-local .deck.toml. Problems never
// abort loading; they come back as warnings and the defaults stand in.
func Load() (Config, []Warning) {
	cwd, err := os.Getwd()
	if err != nil {
		return load(Path())
	}
	return load(Path(), projectPath(cwd))
}

// projectPath finds .deck.toml at the repository root above cwd, or in cwd when
// there is no repository.
func projectPath(cwd string) string {
	for dir := cwd; ; {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return filepath.Join(dir, ProjectFile)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return filepath.Join(cwd, ProjectFile)
		}
		dir = parent
	}
}

// load decodes each existing file over the previous result; later files
// override scalars and append tasks.
func load(paths ...string) (Config, []Warning) {
	c := Default()
	var warnings []Warning
	for _, path := range paths {
		warnings = append(warnings, decodeInto(&c, path)...)
	}
	fillZero(reflect.ValueOf(&c).Elem(), reflect.ValueOf(Default()))
	warnings = append(warnings, validate(&c)...)
	return c, warnings
}

func decodeInto(c *Config, path string) []Warning {
	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	var probe Config
	if _, err := toml.DecodeFile(path, &probe); err != nil {
		return []Warning{{Path: path, Message: "ignored, " + firstLine(err.Error())}}
	}
	prior := c.Tasks
	c.Tasks = nil
	md, err := toml.DecodeFile(path, c)
	c.Tasks = append(prior, c.Tasks...)
	if err != nil {
		return []Warning{{Path: path, Message: firstLine(err.Error())}}
	}
	return unknownKeys(path, md.Undecoded())
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

func unknownKeys(path string, keys []toml.Key) []Warning {
	undecoded := make(map[string]bool, len(keys))
	for _, k := range keys {
		undecoded[k.String()] = true
	}
	var out []Warning
	for _, k := range keys {
		if hasUndecodedParent(k, undecoded) {
			continue
		}
		msg := fmt.Sprintf("unknown key %q", k.String())
		if s := suggest(k); s != "" {
			msg += fmt.Sprintf(" (did you mean %q?)", s)
		}
		out = append(out, Warning{Path: path, Message: msg})
	}
	return out
}

func hasUndecodedParent(k toml.Key, undecoded map[string]bool) bool {
	for i := 1; i < len(k); i++ {
		if undecoded[k[:i].String()] {
			return true
		}
	}
	return false
}

func suggest(k toml.Key) string {
	paths, leaves := knownKeys()
	if s := closest(k.String(), paths); s != "" {
		return s
	}
	return closest(k[len(k)-1], leaves)
}

func closest(word string, candidates []string) string {
	best, bestDist := "", max(1, len(word)/3)+1
	for _, c := range candidates {
		if d := levenshtein(word, c); d < bestDist {
			best, bestDist = c, d
		}
	}
	return best
}

func levenshtein(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

// knownKeys lists every dotted key the schema accepts, plus every field name at
// any depth for keys under maps such as lsp.servers.<name>.
func knownKeys() (paths, leaves []string) {
	var walk func(t reflect.Type, prefix string, record bool)
	walk = func(t reflect.Type, prefix string, record bool) {
		for i := range t.NumField() {
			name := t.Field(i).Tag.Get("toml")
			if name == "" {
				continue
			}
			full := name
			if prefix != "" {
				full = prefix + "." + name
			}
			if record {
				paths = append(paths, full)
			}
			if !slices.Contains(leaves, name) {
				leaves = append(leaves, name)
			}
			ft := t.Field(i).Type
			switch {
			case ft.Kind() == reflect.Struct:
				walk(ft, full, record)
			case ft.Kind() == reflect.Slice && ft.Elem().Kind() == reflect.Struct:
				walk(ft.Elem(), full, record)
			case ft.Kind() == reflect.Map && ft.Elem().Kind() == reflect.Struct:
				walk(ft.Elem(), full, false)
			}
		}
	}
	walk(reflect.TypeFor[Config](), "", true)
	return paths, leaves
}

// fillZero copies defaults into zero strings and numbers and nil slices and
// maps. Booleans are left alone: false is a real choice.
func fillZero(dst, def reflect.Value) {
	for i := range dst.NumField() {
		f, d := dst.Field(i), def.Field(i)
		switch f.Kind() {
		case reflect.Struct:
			fillZero(f, d)
		case reflect.String, reflect.Int, reflect.Int64, reflect.Float64, reflect.Slice, reflect.Map:
			if f.IsZero() {
				f.Set(d)
			}
		}
	}
}

func validate(c *Config) []Warning {
	d := Default()
	enums := []struct {
		key     string
		val     *string
		def     string
		choices []string
	}{
		{"mouse", &c.Mouse, d.Mouse, []string{"hover", "click", "off"}},
		{"editor_return", &c.EditorReturn, d.EditorReturn, []string{"follow", "origin"}},
		{"micro_lsp", &c.MicroLSP, d.MicroLSP, []string{"on", "off"}},
		{"files.icons", &c.Files.Icons, d.Files.Icons, []string{"unicode", "nerd", "none"}},
	}
	var out []Warning
	for _, e := range enums {
		if slices.Contains(e.choices, *e.val) {
			continue
		}
		out = append(out, Warning{Message: fmt.Sprintf("%s = %q is not one of %s; using %q",
			e.key, *e.val, strings.Join(e.choices, ", "), e.def)})
		*e.val = e.def
	}
	return out
}
