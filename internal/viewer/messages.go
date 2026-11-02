package viewer

import "deck/internal/ui"

// Msg is implemented by every message the viewer produces, so a container can
// forward them without listing each type.
type Msg interface {
	ui.Routed
	viewerMsg()
}

// DocLoadedMsg carries a loaded (not yet highlighted) document.
type DocLoadedMsg struct {
	Path string
	Gen  int
	Doc  *Document
	Err  error
}

// DocHighlightedMsg carries styled lines for Doc starting at line From.
// Skipped means the request was dropped because a newer load superseded it.
type DocHighlightedMsg struct {
	Doc     *Document
	Gen     int
	From    int
	Lines   []string
	Skipped bool
}

// MarkdownRenderedMsg carries glamour output for Doc at Width columns.
type MarkdownRenderedMsg struct {
	Doc   *Document
	Gen   int
	Width int
	Lines []string
}

func (DocLoadedMsg) RouteTo() ui.Dest        { return ui.DestFiles }
func (DocHighlightedMsg) RouteTo() ui.Dest   { return ui.DestFiles }
func (MarkdownRenderedMsg) RouteTo() ui.Dest { return ui.DestFiles }

func (DocLoadedMsg) viewerMsg()        {}
func (DocHighlightedMsg) viewerMsg()   {}
func (MarkdownRenderedMsg) viewerMsg() {}
