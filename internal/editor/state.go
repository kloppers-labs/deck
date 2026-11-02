package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// State is where micro's deck plugin last saw the cursor, plus the files it saved.
type State struct {
	Path           string
	Line, Col, Top int
	Saved          []string
}

var stateSeq atomic.Int64

// StatePath names a fresh state file for one editor session; the plugin creates it.
func StatePath() string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("deck-editor-%d-%d.state", os.Getpid(), stateSeq.Add(1)))
}

// ReadState parses the bridge plugin's state file. Missing, stale or malformed files give ok == false.
func ReadState(file string, launchedAt time.Time) (State, bool) {
	if file == "" {
		return State{}, false
	}
	info, err := os.Stat(file)
	if err != nil || info.ModTime().Before(launchedAt.Truncate(time.Second)) {
		return State{}, false
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return State{}, false
	}
	return parseState(string(data))
}

func parseState(s string) (State, bool) {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	if len(lines) < 2 || !filepath.IsAbs(lines[0]) {
		return State{}, false
	}
	nums := strings.Fields(lines[1])
	if len(nums) != 3 {
		return State{}, false
	}
	var pos [3]int
	for i, f := range nums {
		n, err := strconv.Atoi(f)
		if err != nil || n < 1 {
			return State{}, false
		}
		pos[i] = n
	}
	st := State{Path: lines[0], Line: pos[0], Col: pos[1], Top: pos[2]}
	for _, l := range lines[2:] {
		if l != "" && !slices.Contains(st.Saved, l) {
			st.Saved = append(st.Saved, l)
		}
	}
	return st, true
}

// ReturnTarget picks where deck shows after the editor closes. "follow" (the
// default) goes where the editor ended if that is under root; "origin" goes back.
func ReturnTarget(origin Target, st State, ok bool, mode, root string) Target {
	if !ok || mode == "origin" {
		return origin
	}
	if root != "" && !within(st.Path, root) {
		return origin
	}
	return Target{Path: st.Path, Line: st.Line, Col: st.Col, Top: st.Top}
}
