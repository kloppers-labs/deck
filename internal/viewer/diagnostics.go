package viewer

import (
	"slices"
	"strings"
)

type Severity int

const (
	SeverityError Severity = iota + 1
	SeverityWarning
	SeverityInfo
	SeverityHint
)

type Diagnostic struct {
	Range           Range
	Severity        Severity
	Message, Source string
}

type diagStore struct{ byPath map[string][]Diagnostic }

var diagSGR = map[Severity]string{
	SeverityError:   "\x1b[38;5;203m",
	SeverityWarning: "\x1b[38;5;221m",
	SeverityInfo:    "\x1b[38;5;117m",
	SeverityHint:    gutterSGR,
}

func (s Severity) glyph() string {
	switch s {
	case SeverityWarning:
		return "▲"
	case SeverityInfo:
		return "●"
	case SeverityHint:
		return "·"
	}
	return "●"
}

func (s Severity) rank() int {
	if s < SeverityError || s > SeverityHint {
		return int(SeverityError)
	}
	return int(s)
}

// SetDiagnostics replaces path's diagnostics; every copy of the viewer sees them.
func (m *Model) SetDiagnostics(path string, ds []Diagnostic) {
	if len(ds) == 0 {
		delete(m.diags.byPath, path)
		return
	}
	ds = slices.Clone(ds)
	slices.SortStableFunc(ds, func(a, b Diagnostic) int {
		switch {
		case a.Range.Start.before(b.Range.Start):
			return -1
		case b.Range.Start.before(a.Range.Start):
			return 1
		}
		return a.Severity.rank() - b.Severity.rank()
	})
	m.diags.byPath[path] = ds
}

func (m Model) diagnostics() []Diagnostic {
	if m.showingMD() {
		return nil
	}
	return m.diags.byPath[m.path]
}

func lineDiagnostic(ds []Diagnostic, line int) (Diagnostic, bool) {
	i, _ := slices.BinarySearchFunc(ds, line, func(d Diagnostic, l int) int { return d.Range.Start.Line - l })
	var best Diagnostic
	found := false
	for ; i < len(ds) && ds[i].Range.Start.Line == line; i++ {
		if !found || ds[i].Severity.rank() < best.Severity.rank() {
			best, found = ds[i], true
		}
	}
	return best, found
}

// NextDiagnostic is the diagnostic start after (or before) p, wrapping around.
func NextDiagnostic(ds []Diagnostic, p Pos, forward bool) (Pos, bool) {
	if len(ds) == 0 {
		return p, false
	}
	if forward {
		for _, d := range ds {
			if p.before(d.Range.Start) {
				return d.Range.Start, true
			}
		}
		return ds[0].Range.Start, true
	}
	for i := len(ds) - 1; i >= 0; i-- {
		if ds[i].Range.Start.before(p) {
			return ds[i].Range.Start, true
		}
	}
	return ds[len(ds)-1].Range.Start, true
}

func (d Diagnostic) summary() string {
	text := firstLine(d.Message)
	if d.Source != "" {
		text = d.Source + ": " + text
	}
	return d.Severity.glyph() + " " + strings.TrimSpace(text)
}
