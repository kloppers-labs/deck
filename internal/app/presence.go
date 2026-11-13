package app

import (
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kloppers-labs/deck/internal/files"
	"github.com/kloppers-labs/deck/internal/presence"
	"github.com/kloppers-labs/deck/internal/viewer"
)

const recentFiles = 10

type selMemo struct {
	path string
	r    viewer.Range
	text string
}

// WithAgent publishes the user's position to store after every update and
// registers focus changes in controlDir; a nil store publishes nothing.
func (m Model) WithAgent(store *presence.Store, controlDir string) Model {
	m.pres = store
	m.controlDir = controlDir
	m.selMemo = &selMemo{}
	return m
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.update(msg)
	nm := next.(Model)
	if nm.pres != nil {
		if ev, ok := msg.(files.LSPEventMsg); ok {
			if path, ds, ok := ev.Diagnostics(); ok {
				nm.pres.SetDiagnostics(path, presenceDiagnostics(ds))
			}
		}
		nm.pres.Publish(nm.snapshot())
	}
	if _, ok := msg.(tea.FocusMsg); ok && nm.controlDir != "" {
		cmd = tea.Batch(cmd, touchCmd(nm.controlDir))
	}
	return nm, cmd
}

func (m Model) snapshot() presence.Snapshot {
	s := presence.Snapshot{Root: m.files.Root(), Tab: strings.ToLower(tabNames[m.active])}
	if m.active == tabGit {
		return m.gitSnapshot(s)
	}
	if m.active != tabFiles {
		return s
	}
	s.Recent = m.files.RecentFiles(recentFiles)
	file := m.files.ShownFile()
	if file == "" {
		s.Dir = m.files.SelectedDir()
		return s
	}
	s.File = file
	first, last := m.files.VisibleLines()
	s.Visible = [2]int{first + 1, last + 1}
	if title, start, end, ok := m.files.Section(); ok {
		s.Section = title
		s.Line, s.Col = start+1, 1
		s.Sel = &presence.Range{StartLine: start + 1, StartCol: 1, EndLine: end + 1,
			EndCol: utf8.RuneCountInString(m.files.SourceLine(end)) + 1}
		return s
	}
	loc := m.files.ViewerLoc()
	s.Line, s.Col = loc.Line+1, loc.Col+1
	if r, ok := m.files.Selection(); ok {
		s.Sel = &presence.Range{StartLine: r.Start.Line + 1, StartCol: r.Start.Col + 1,
			EndLine: r.End.Line + 1, EndCol: r.End.Col + 1}
		s.SelText = m.selectionText(file, r)
	}
	return s
}

func (m Model) selectionText(path string, r viewer.Range) string {
	memo := m.selMemo
	if memo == nil {
		memo = &selMemo{}
	}
	if memo.path != path || memo.r != r {
		text := m.files.SelectionText()
		if len(text) > presence.MaxSelText {
			text = text[:presence.MaxSelText]
		}
		*memo = selMemo{path: path, r: r, text: text}
	}
	return memo.text
}

var severityNames = map[viewer.Severity]string{
	viewer.SeverityError:   "error",
	viewer.SeverityWarning: "warning",
	viewer.SeverityInfo:    "information",
	viewer.SeverityHint:    "hint",
}

func presenceDiagnostics(ds []viewer.Diagnostic) []presence.Diagnostic {
	out := make([]presence.Diagnostic, 0, len(ds))
	for _, d := range ds {
		sev := severityNames[d.Severity]
		if sev == "" {
			sev = "error"
		}
		out = append(out, presence.Diagnostic{
			Line: d.Range.Start.Line + 1, Col: d.Range.Start.Col + 1,
			EndLine: d.Range.End.Line + 1, EndCol: d.Range.End.Col + 1,
			Severity: sev, Message: d.Message, Source: d.Source,
		})
	}
	return out
}

func (m Model) gitSnapshot(s presence.Snapshot) presence.Snapshot {
	spot, ok := m.git.Spot()
	if !ok {
		return s
	}
	s.Git = &presence.GitSpot{Mode: spot.Mode, Base: spot.Base, Side: spot.Side}
	if spot.Path == "" {
		return s
	}
	s.File, s.Line = spot.Path, spot.Line
	if s.Line > 0 {
		s.Col = 1
	}
	if spot.EndLine > spot.Line {
		s.Sel = &presence.Range{StartLine: spot.Line, StartCol: 1, EndLine: spot.EndLine, EndCol: spot.EndCol()}
		s.SelText = spot.Text
		if len(s.SelText) > presence.MaxSelText {
			s.SelText = s.SelText[:presence.MaxSelText]
		}
	}
	return s
}
