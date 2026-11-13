package git

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/kloppers-labs/deck/internal/ui"
)

// View renders the list column and the bordered right column, filling exactly
// the box given to SetSize.
func (p Pane) View() string {
	lw := p.listWidth()
	left := ui.Fit(strings.Join(p.leftLines(lw), "\n"), lw, p.height)
	innerW, innerH := p.rightInner()
	var content string
	switch {
	case p.branching:
		content = p.branchView()
	case p.committing:
		content = p.commitView() + "\n" + p.diff.View()
	default:
		content = p.diffView()
	}
	style := ui.Pane
	if p.focus == focusDiff && !p.Capturing() {
		style = ui.PaneActive
	}
	right := style.Render(ui.Fit(content, innerW, innerH))
	return ui.Fit(lipgloss.JoinHorizontal(lipgloss.Top, left, right), p.width, p.height)
}

func (p Pane) leftLines(lw int) []string {
	w := max(lw-1, 0)
	if p.mode == modeReview {
		return p.reviewLines(w)
	}
	header := ui.Muted.Render("⎇ …")
	if p.snap.Branch != "" {
		header = headerLine(p.snap)
	}
	lines := []string{ui.FitWidth(header, w, "…"), ui.FitWidth(p.infoLine(), w, "…")}
	switch {
	case p.notRepo:
		return append(lines, ui.Muted.Render("not a git repository"))
	case p.mode == modeLog && !p.logLoaded:
		return append(lines, ui.Muted.Render("loading log…"))
	case p.mode == modeLog:
		return append(lines, p.logList.View())
	case len(p.items) == 0 && p.repo == nil:
		return append(lines, ui.Muted.Render("loading…"))
	case len(p.items) == 0:
		return append(lines, ui.Muted.Render("working tree clean"))
	}
	end := min(p.offset+p.visibleRows(), len(p.rows))
	for _, r := range p.rows[p.offset:end] {
		lines = append(lines, p.rowView(r, w))
	}
	return lines
}

var sectionColors = [numSections]lipgloss.TerminalColor{ui.ColorString, ui.ColorWarn, ui.ColorInfo, ui.ColorError}

func (p Pane) rowView(r row, w int) string {
	color := lipgloss.NewStyle().Foreground(sectionColors[r.sec])
	if r.header {
		return ui.FitWidth(color.Bold(true).Render(fmt.Sprintf("%s (%d)", r.sec, r.count)), w, "…")
	}
	it := p.items[r.item]
	code := it.Code()
	path := TruncatePath(it.label(), w-3-len(code))
	switch {
	case r.item == p.cursor && p.focus == focusList:
		return ui.Active.Render("› " + code + " " + path)
	case r.item == p.cursor:
		return ui.Muted.Render("› ") + color.Render(code) + " " + path
	}
	return "  " + color.Render(code) + " " + path
}

func (p Pane) infoLine() string {
	var text string
	switch {
	case p.pending != nil:
		return lipgloss.NewStyle().Foreground(ui.ColorWarn).Bold(true).Render(p.pending.text)
	case p.busy != "":
		text = p.busy
	case p.status != "" && p.statusErr:
		text = ui.Err.Render(firstLine(p.status))
	case p.status != "":
		text = ui.Muted.Render(firstLine(p.status))
	case p.loading:
		text = ui.Muted.Render("loading…")
	}
	if p.spinning() {
		return p.spin.View() + text
	}
	return text
}
