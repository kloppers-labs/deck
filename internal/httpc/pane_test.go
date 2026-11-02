package httpc

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"deck/internal/ui"
)

func TestRespMsgRoutesToHTTP(t *testing.T) {
	if got := (RespMsg{}).RouteTo(); got != ui.DestHTTP {
		t.Errorf("RespMsg routes to %v, want DestHTTP", got)
	}
}

func TestViewIsExactlyTheOuterBox(t *testing.T) {
	for _, size := range [][2]int{{60, 11}, {80, 19}, {120, 35}, {200, 45}} {
		p := NewPane("dracula")
		p.SetSize(size[0], size[1])
		p, _ = p.Update(RespMsg{status: "200 OK", code: 200, contentType: "text/plain",
			body: strings.Repeat(strings.Repeat("x", 300)+"\n", 200)})
		v := p.View()
		if got := lipgloss.Height(v); got != size[1] {
			t.Errorf("%v: height = %d, want %d", size, got, size[1])
		}
		if got := lipgloss.Width(v); got > size[0] {
			t.Errorf("%v: width = %d, want <= %d", size, got, size[0])
		}
	}
}

func TestResponseHeightFollowsPane(t *testing.T) {
	p := NewPane("dracula")
	p.SetSize(100, 40)
	tall := p.resp.Height
	p.SetSize(100, 16)
	if p.resp.Height >= tall || p.resp.Height < 1 {
		t.Errorf("response height %d (was %d) should shrink with the pane and stay >= 1", p.resp.Height, tall)
	}
	p.SetSize(100, 5)
	if p.resp.Height != 1 {
		t.Errorf("response height at a tiny pane = %d, want the minimum of 1", p.resp.Height)
	}
	if v := ansi.Strip(p.View()); lipgloss.Height(v) != 5 {
		t.Errorf("tiny pane view height = %d, want 5", lipgloss.Height(v))
	}
}
