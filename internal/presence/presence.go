// Package presence holds what the user is looking at in deck, shared between
// the UI, the control socket and the IDE bridge.
package presence

import (
	"maps"
	"reflect"
	"slices"
	"sync"
)

// MaxSelText caps the selected text carried in a snapshot.
const MaxSelText = 16 << 10

// Range is 1-based with rune columns; EndLine is inclusive, EndCol exclusive.
type Range struct {
	StartLine int `json:"startLine"`
	StartCol  int `json:"startCol"`
	EndLine   int `json:"endLine"`
	EndCol    int `json:"endCol"`
}

// GitSpot describes the Git tab: Mode is changes, review or log; Side is new or old.
type GitSpot struct {
	Mode string `json:"mode"`
	Base string `json:"base,omitempty"`
	Side string `json:"side,omitempty"`
}

type Diagnostic struct {
	Line     int    `json:"line"`
	Col      int    `json:"col"`
	EndLine  int    `json:"endLine"`
	EndCol   int    `json:"endCol"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Source   string `json:"source,omitempty"`
}

// Snapshot is the user's position: 1-based lines and rune columns.
type Snapshot struct {
	Root    string   `json:"root"`
	Tab     string   `json:"tab"`
	File    string   `json:"file,omitempty"`
	Dir     string   `json:"dir,omitempty"`
	Line    int      `json:"line,omitempty"`
	Col     int      `json:"col,omitempty"`
	Sel     *Range   `json:"selection,omitempty"`
	SelText string   `json:"selectedText,omitempty"`
	Section string   `json:"section,omitempty"`
	Visible [2]int   `json:"visible"`
	Git     *GitSpot `json:"git,omitempty"`
	Recent  []string `json:"recent,omitempty"`
	Seq     uint64   `json:"seq"`
}

func (s Snapshot) clone() Snapshot {
	if s.Sel != nil {
		sel := *s.Sel
		s.Sel = &sel
	}
	if s.Git != nil {
		g := *s.Git
		s.Git = &g
	}
	s.Recent = slices.Clone(s.Recent)
	return s
}

type Store struct {
	mu    sync.Mutex
	snap  Snapshot
	subs  map[int]chan uint64
	next  int
	diags map[string][]Diagnostic
}

func New() *Store {
	return &Store{subs: map[int]chan uint64{}, diags: map[string][]Diagnostic{}}
}

// Publish stores snap when it differs from the current snapshot, ignoring Seq,
// and notifies subscribers; it reports whether anything changed.
func (s *Store) Publish(snap Snapshot) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap.Seq = s.snap.Seq
	if reflect.DeepEqual(snap, s.snap) {
		return false
	}
	snap = snap.clone()
	snap.Seq++
	s.snap = snap
	for _, ch := range s.subs {
		select {
		case <-ch:
		default:
		}
		ch <- snap.Seq
	}
	return true
}

func (s *Store) Get() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snap.clone()
}

// Subscribe returns a channel that holds the latest Seq after each change; the
// func unsubscribes and closes it.
func (s *Store) Subscribe() (<-chan uint64, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.next
	s.next++
	ch := make(chan uint64, 1)
	s.subs[id] = ch
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			delete(s.subs, id)
			close(ch)
		})
	}
}

func (s *Store) SetDiagnostics(path string, d []Diagnostic) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(d) == 0 {
		delete(s.diags, path)
		return
	}
	s.diags[path] = slices.Clone(d)
}

// Diagnostics returns the diagnostics for path, or for every path when path is "".
func (s *Store) Diagnostics(path string) map[string][]Diagnostic {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string][]Diagnostic{}
	if path == "" {
		for p, d := range maps.All(s.diags) {
			out[p] = slices.Clone(d)
		}
		return out
	}
	if d, ok := s.diags[path]; ok {
		out[path] = slices.Clone(d)
	}
	return out
}
