package viewer

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// NewVirtualDocument builds an already-styled document that was not read from
// a file, such as a directory card. styled[i] must render plain[i].
func NewVirtualDocument(path string, plain, styled []string) *Document {
	raw := make([]string, len(plain))
	copy(raw, plain)
	for i, s := range styled {
		if !strings.HasSuffix(s, sgrReset) {
			styled[i] = s + sgrReset
		}
	}
	return &Document{
		Path:        path,
		Raw:         raw,
		Plain:       plain,
		Styled:      styled,
		TabWidth:    1,
		highlighted: true,
		virtual:     true,
	}
}

// OpenVirtual shows the document build returns, building it off the update
// loop under the same load generation as Open.
func (m Model) OpenVirtual(path string, at *Spot, build func() (*Document, error)) (Model, tea.Cmd) {
	m, _ = m.Open(path, at)
	gen := m.gen
	return m, func() tea.Msg {
		d, err := build()
		return DocLoadedMsg{Path: path, Gen: gen, Doc: d, Err: err}
	}
}
