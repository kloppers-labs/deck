package editor

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

//go:embed runtime
var runtimeFS embed.FS

const (
	manifestName  = ".deck-manifest.json"
	zDeckPath     = "plug/mlsp/z_deck.lua"
	minOmniMicro  = "2.0.14"
	templateExt   = ".tmpl"
	runtimePrefix = "runtime/"
)

var omniTools = []string{"fzf", "rg", "bat"}

// SyncOptions selects the optional parts of the bundle and supplies the rendered z_deck.lua.
type SyncOptions struct {
	LSP      bool
	LookPath func(string) (string, error)
	ZDeck    []byte
}

// SyncResult reports what Sync installed and changed.
type SyncResult struct {
	Features Features
	Written  []string
	Removed  []string
	Digest   string
	Warnings []string
}

type manifest struct {
	Files map[string]string `json:"files"`
}

// Sync installs deck's micro runtime into dir. Files are rewritten only when their
// hash changed, files deck never wrote are left alone, and settings/bindings gain missing keys only.
func Sync(dir, microVersion string, o SyncOptions) (SyncResult, error) {
	if o.LookPath == nil {
		o.LookPath = exec.LookPath
	}
	f := Features{LSP: o.LSP, Omni: omniUsable(microVersion, o.LookPath)}
	want, err := bundleFiles(f, o.ZDeck)
	if err != nil {
		return SyncResult{}, err
	}
	res := SyncResult{Features: f}
	old := readManifest(dir)
	next := manifest{Files: map[string]string{}}
	for _, rel := range slices.Sorted(maps.Keys(want)) {
		sum := hash(want[rel])
		next.Files[rel] = sum
		target := filepath.Join(dir, filepath.FromSlash(rel))
		if old.Files[rel] == sum && fileExists(target) {
			continue
		}
		if err := writeFile(target, want[rel]); err != nil {
			return res, err
		}
		res.Written = append(res.Written, rel)
	}
	for _, rel := range slices.Sorted(maps.Keys(old.Files)) {
		if _, ok := want[rel]; ok {
			continue
		}
		target := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.Remove(target); err == nil || errors.Is(err, fs.ErrNotExist) {
			res.Removed = append(res.Removed, rel)
			removeEmptyParents(filepath.Dir(target), dir)
		}
	}
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return res, err
	}
	res.Digest = hash(data)
	if err := writeIfChanged(filepath.Join(dir, manifestName), data); err != nil {
		return res, err
	}
	for name, defaults := range map[string]map[string]any{
		"settings.json": DefaultSettings(f),
		"bindings.json": DefaultBindings(f),
	} {
		if _, err := MergeJSON(filepath.Join(dir, name), defaults); err != nil {
			res.Warnings = append(res.Warnings, err.Error())
		}
	}
	sort.Strings(res.Warnings)
	return res, nil
}

func omniUsable(microVersion string, lookPath func(string) (string, error)) bool {
	if !versionAtLeast(microVersion, minOmniMicro) {
		return false
	}
	return len(missingTools(lookPath)) == 0
}

func missingTools(lookPath func(string) (string, error)) []string {
	var out []string
	for _, t := range omniTools {
		if _, err := lookPath(t); err != nil {
			out = append(out, t)
		}
	}
	return out
}

func bundleFiles(f Features, zDeck []byte) (map[string][]byte, error) {
	out := map[string][]byte{}
	err := fs.WalkDir(runtimeFS, "runtime", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel := strings.TrimPrefix(p, runtimePrefix)
		if !strings.Contains(rel, "/") || strings.HasSuffix(rel, templateExt) {
			return nil
		}
		if (strings.HasPrefix(rel, "plug/MicroOmni/") && !f.Omni) || (strings.HasPrefix(rel, "plug/mlsp/") && !f.LSP) {
			return nil
		}
		data, err := fs.ReadFile(runtimeFS, p)
		out[rel] = data
		return err
	})
	if f.LSP {
		out[zDeckPath] = zDeck
	}
	return out, err
}

func readManifest(dir string) manifest {
	var m manifest
	if data, err := os.ReadFile(filepath.Join(dir, manifestName)); err == nil {
		_ = json.Unmarshal(data, &m)
	}
	if m.Files == nil {
		m.Files = map[string]string{}
	}
	return m
}

func hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.Mode().IsRegular()
}

func writeFile(p string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

func writeIfChanged(p string, data []byte) error {
	if cur, err := os.ReadFile(p); err == nil && bytes.Equal(cur, data) {
		return nil
	}
	return writeFile(p, data)
}

func removeEmptyParents(dir, stop string) {
	for dir != stop && strings.HasPrefix(dir, stop) {
		if os.Remove(dir) != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}
