package viewer

import (
	"fmt"
	"strings"
	"testing"
)

func benchDoc(n int) *Document {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "    public async Task<IActionResult> Get%d(int id) { var x = await _svc.ListAsync(id); return Ok(x); }\n", i)
	}
	d := newDocument("/bench/Big.cs", []byte(b.String()), 4)
	copy(d.Styled, Highlight(d.Path, d.Raw, "dracula", 4))
	d.highlighted = true
	return d
}

func BenchmarkViewerView(b *testing.B) {
	m := New(Options{LineNumbers: true})
	m.SetSize(160, 202)
	m, _ = m.Open("/bench/Big.cs", nil)
	m, _ = m.Update(DocLoadedMsg{Path: "/bench/Big.cs", Gen: m.gen, Doc: benchDoc(50000)})
	m.SetFocused(true)
	m, _ = m.Update(keyMsg("ctrl+d"))
	b.ResetTimer()
	for b.Loop() {
		_ = m.View()
	}
}

func BenchmarkHighlight1MB(b *testing.B) {
	d := benchDoc(10000)
	for b.Loop() {
		Highlight(d.Path, d.Raw, "dracula", 4)
	}
}
