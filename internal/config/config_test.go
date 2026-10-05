package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultTheme(t *testing.T) {
	if got := Default().Theme; got != "dracula" {
		t.Errorf("Default().Theme = %q, want %q", got, "dracula")
	}
}

func TestLoadFromMissingFileUsesDefaults(t *testing.T) {
	c := loadFrom(filepath.Join(t.TempDir(), "does-not-exist.toml"))
	if c.Theme != "dracula" {
		t.Errorf("Theme = %q, want %q", c.Theme, "dracula")
	}
	if c.AIBackend != "claude" || c.AIModel != "sonnet" || c.Editor != "micro" {
		t.Errorf("missing-file load did not apply defaults: %+v", c)
	}
}

func TestLoadFromOverridesTheme(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("theme = \"monokai\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := loadFrom(path).Theme; got != "monokai" {
		t.Errorf("Theme = %q, want %q", got, "monokai")
	}
}

func TestLoadFromUnknownThemeDoesNotPanic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("theme = \"not-a-real-theme\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// loadFrom does not validate against chroma's style set; it just round-trips
	// the value. Highlight call sites fall back to a safe default at render time.
	if got := loadFrom(path).Theme; got != "not-a-real-theme" {
		t.Errorf("Theme = %q, want it preserved verbatim", got)
	}
}
