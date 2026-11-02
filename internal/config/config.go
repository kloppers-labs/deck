package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config holds user-tunable settings for deck. AIBackend and AIModel select the
// CLI used by the Git tab's ctrl+g commit-message helper.
type Config struct {
	AIBackend string `toml:"ai_backend"`
	AIModel   string `toml:"ai_model"`
	// Editor may carry arguments, e.g. "micro -softwrap false" or "code -w";
	// the file path is appended last. Bare "micro" gets deck's bundled Dracula
	// colorscheme automatically.
	Editor string `toml:"editor"`
	// Theme is the chroma/glamour style name used for code preview, git diffs,
	// and HTTP response bodies. It controls syntax highlighting only; the
	// lipgloss chrome (borders, accents) stays on deck's Dracula palette,
	// adapting between dark and light terminal backgrounds.
	Theme string `toml:"theme"`
	Tasks []Task `toml:"tasks"`
}

// Task is a named dev command for the Run tab, e.g. {Name:"api", Cmd:"dotnet run"}.
// Cmd runs via "sh -c", so pipes and arguments work as typed.
type Task struct {
	Name string `toml:"name"`
	Cmd  string `toml:"cmd"`
}

func Default() Config {
	return Config{AIBackend: "claude", AIModel: "sonnet", Editor: "micro", Theme: "dracula"}
}

// Path returns ~/.config/deck/config.toml (kept alongside micro's config).
func Path() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "deck-config.toml"
	}
	return filepath.Join(home, ".config", "deck", "config.toml")
}

// Load reads the user's config file if present, falling back to defaults for
// any missing field. A missing file is not an error.
func Load() Config {
	return loadFrom(Path())
}

// loadFrom reads config from path (if it exists), then applies per-field
// defaults. An empty or missing path yields the defaults. Exposed for testing.
func loadFrom(path string) Config {
	c := Default()
	if path != "" {
		if _, err := os.Stat(path); err == nil {
			_, _ = toml.DecodeFile(path, &c)
		}
	}
	if c.AIBackend == "" {
		c.AIBackend = "claude"
	}
	if c.AIModel == "" {
		c.AIModel = "sonnet"
	}
	if c.Editor == "" {
		c.Editor = "micro"
	}
	if c.Theme == "" {
		c.Theme = "dracula"
	}
	return c
}
