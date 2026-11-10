package files

import "github.com/kloppers-labs/deck/internal/ui"

const (
	minSidebar       = 16
	maxSidebarPct    = 40
	autoCollapseCols = 90
	infoWidth        = 24
	minViewerWidth   = 30
	sidebarStep      = 2
)

// Layout is the Files tab split into its outer boxes, borders included, in
// pane-local cells. Hidden regions have zero width.
type Layout struct {
	Tree, Viewer, Info ui.Rect
	ShowTree, ShowInfo bool
}

func clampSidebar(sidebar, w int) int {
	return max(min(sidebar, w*maxSidebarPct/100), minSidebar)
}

// ComputeLayout tiles w×h into the tree (sidebar columns, capped at 40%), the
// viewer, and the info panel when the viewer keeps room to read.
func ComputeLayout(w, h, sidebar int, showTree, showInfo bool) Layout {
	l := Layout{ShowTree: showTree && w > minSidebar+minViewerWidth}
	tw := 0
	if l.ShowTree {
		tw = clampSidebar(sidebar, w)
	}
	iw := 0
	if showInfo && w-tw-infoWidth >= minViewerWidth {
		iw = infoWidth
		l.ShowInfo = true
	}
	l.Tree = ui.Rect{X: 0, Y: 0, W: tw, H: h}
	l.Viewer = ui.Rect{X: tw, Y: 0, W: max(w-tw-iw, 0), H: h}
	l.Info = ui.Rect{X: tw + l.Viewer.W, Y: 0, W: iw, H: h}
	return l
}

func inset(r ui.Rect, dx, dy int) ui.Rect {
	return ui.Rect{X: r.X + dx, Y: r.Y + dy, W: max(r.W-2*dx, 0), H: max(r.H-2*dy, 0)}
}

// TreeInner is the tree box inside its border.
func (l Layout) TreeInner() ui.Rect { return inset(l.Tree, 1, 1) }

// ViewerInner is the viewer box inside its border and padding.
func (l Layout) ViewerInner() ui.Rect { return inset(l.Viewer, 2, 1) }

func (l Layout) InfoInner() ui.Rect { return inset(l.Info, 2, 1) }

// OnDivider reports whether x is the seam between the tree and the viewer.
func (l Layout) OnDivider(x, y int) bool {
	return l.ShowTree && y >= 0 && y < l.Tree.H && (x == l.Tree.W-1 || x == l.Tree.W)
}
