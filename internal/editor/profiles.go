package editor

import (
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

type profile int

const (
	pathOnly profile = iota
	plusLineCol
	vimCursor
	fileLineCol
	codeGoto
	nanoPlus
	jetbrains
)

var profiles = map[string]profile{
	"micro": plusLineCol, "emacs": plusLineCol, "emacsclient": plusLineCol, "kak": plusLineCol,
	"vi": vimCursor, "vim": vimCursor, "nvim": vimCursor,
	"hx": fileLineCol, "helix": fileLineCol, "zed": fileLineCol, "subl": fileLineCol,
	"code": codeGoto, "code-insiders": codeGoto, "codium": codeGoto, "vscodium": codeGoto, "cursor": codeGoto, "windsurf": codeGoto,
	"nano":  nanoPlus,
	"rider": jetbrains, "idea": jetbrains, "goland": jetbrains, "webstorm": jetbrains, "pycharm": jetbrains, "clion": jetbrains,
}

var guiUnlessWait = map[string]bool{
	"zed": true, "subl": true,
	"code": true, "code-insiders": true, "codium": true, "vscodium": true, "cursor": true, "windsurf": true,
}

var displayNames = map[string]string{
	"code": "VS Code", "code-insiders": "VS Code Insiders", "codium": "VSCodium", "vscodium": "VSCodium",
	"cursor": "Cursor", "windsurf": "Windsurf", "zed": "Zed", "subl": "Sublime Text",
	"rider": "Rider", "idea": "IntelliJ IDEA", "goland": "GoLand", "webstorm": "WebStorm", "pycharm": "PyCharm", "clion": "CLion",
}

func editorFields(editorCfg string) (string, []string) {
	parts := strings.Fields(editorCfg)
	if len(parts) == 0 {
		return "micro", nil
	}
	return parts[0], parts[1:]
}

// IsMicro reports whether the configured editor is micro (the default).
func IsMicro(editorCfg string) bool {
	name, _ := editorFields(editorCfg)
	return filepath.Base(name) == "micro"
}

// DisplayName is the editor's human name, e.g. "VS Code" for code.
func DisplayName(editorCfg string) string {
	name, _ := editorFields(editorCfg)
	base := filepath.Base(name)
	if d, ok := displayNames[base]; ok {
		return d
	}
	return base
}

// Args returns the program, its arguments and the launch mode that open t in editorCfg.
func Args(editorCfg string, t Target) (string, []string, Mode) {
	return buildArgs(editorCfg, t, nil)
}

func buildArgs(editorCfg string, t Target, extra []string) (string, []string, Mode) {
	name, user := editorFields(editorCfg)
	base := filepath.Base(name)
	mode := modeFor(base, user)
	if slices.ContainsFunc(user, isPlaceholder) {
		return name, substitute(user, t), mode
	}
	args := slices.Concat(user, extra)
	if t.Line <= 0 {
		return name, append(args, t.Path), mode
	}
	return name, append(args, position(profiles[base], t)...), mode
}

func modeFor(base string, user []string) Mode {
	switch {
	case profiles[base] == jetbrains:
		return Detach
	case guiUnlessWait[base] && !slices.Contains(user, "-w") && !slices.Contains(user, "--wait"):
		return Detach
	}
	return Suspend
}

func isPlaceholder(arg string) bool { return strings.Contains(arg, "{file}") }

func substitute(args []string, t Target) []string {
	r := strings.NewReplacer("{file}", t.Path, "{line}", strconv.Itoa(max(t.Line, 1)), "{col}", strconv.Itoa(max(t.Col, 1)))
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = r.Replace(a)
	}
	return out
}

func position(p profile, t Target) []string {
	line, col := strconv.Itoa(t.Line), strconv.Itoa(t.Col)
	lineCol := line
	if t.Col > 0 {
		lineCol += ":" + col
	}
	switch p {
	case plusLineCol:
		return []string{"+" + lineCol, t.Path}
	case vimCursor:
		if t.Col <= 0 {
			return []string{"+" + line, t.Path}
		}
		return []string{"+call cursor(" + line + "," + col + ")", t.Path}
	case fileLineCol:
		return []string{t.Path + ":" + lineCol}
	case codeGoto:
		return []string{"-g", t.Path + ":" + lineCol}
	case nanoPlus:
		if t.Col <= 0 {
			return []string{"+" + line, t.Path}
		}
		return []string{"+" + line + "," + col, t.Path}
	case jetbrains:
		args := []string{"--line", line}
		if t.Col > 0 {
			args = append(args, "--column", col)
		}
		return append(args, t.Path)
	}
	return []string{t.Path}
}
