package git

import (
	"fmt"
	"io"
	"strings"

	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"

	"deck/internal/ui"
)

// GenCommitMsgRequest asks the root model to generate a commit message from the
// staged diff via the AI pane. The app replies with CommitMsgReady.
type GenCommitMsgRequest struct{}

// CommitMsgReady carries an AI-generated commit message back into the pane.
type CommitMsgReady struct{ Text string }

// refreshMsg triggers a status reload.
type refreshMsg struct{}

// Refresh is a command that reloads git status; the app fires it when the Git
// tab gains focus.
func Refresh() tea.Msg { return refreshMsg{} }

// branchItem adapts a branch name to list.Item for the picker.
type branchItem string

func (b branchItem) FilterValue() string { return string(b) }

// branchDelegate renders one branch row, marking the checked-out branch.
type branchDelegate struct{ current string }

func (branchDelegate) Height() int                         { return 1 }
func (branchDelegate) Spacing() int                        { return 0 }
func (branchDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (d branchDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	b, ok := item.(branchItem)
	if !ok {
		return
	}
	name := string(b)
	if name == d.current {
		name += lipgloss.NewStyle().Foreground(ui.ColorString).Render(" ✓")
	}
	if index == m.Index() {
		fmt.Fprint(w, ui.Active.Render("› "+name))
		return
	}
	fmt.Fprint(w, "  "+name)
}

// Pane is deck's Git tab: a change list on the left, a highlighted diff on the
// right, plus staging, committing, a branch picker, and a commit-log view.
type Pane struct {
	changes    []Change
	cursor     int
	diff       viewport.Model
	branch     string
	committing bool
	confirming bool
	commitBox  textinput.Model
	commitText string
	confirm    *huh.Confirm
	confirmVal *bool
	branching  bool
	branchList list.Model
	showLog    bool
	status     string
	theme      string
	err        error
	width      int
	height     int
}

// NewPane builds the Git tab. theme is the chroma style name for diff
// highlighting.
func NewPane(theme string) Pane {
	ti := textinput.New()
	ti.Placeholder = "commit message (Enter to confirm, Esc to cancel, ctrl+g for AI)"
	ti.Prompt = "✎ "
	// Default the confirm to Yes since the user reaches it by submitting a
	// message. confirmVal is a stable heap pointer shared across pane copies.
	confirmVal := new(bool)
	*confirmVal = true
	cf := huh.NewConfirm().
		Title("Commit this message?").
		Affirmative("Yes").
		Negative("No").
		Inline(true).
		Value(confirmVal)
	bl := list.New(nil, branchDelegate{}, 0, 0)
	bl.SetShowTitle(false)
	bl.SetShowStatusBar(false)
	bl.SetShowHelp(false)
	bl.KeyMap.Quit = key.Binding{} // deck's global "q" owns quit
	p := Pane{diff: viewport.New(0, 0), commitBox: ti, theme: theme,
		confirm: cf, confirmVal: confirmVal, branchList: bl}
	p.reload()
	return p
}

// SetSize lays out the change list and diff viewport.
func (p *Pane) SetSize(w, h int) {
	p.width, p.height = w, h
	listW := w / 3
	if listW < 24 {
		listW = 24
	}
	p.diff.Width = w - listW - 4
	p.diff.Height = h - 3
	p.commitBox.Width = w - 6
	p.branchList.SetSize(w-listW-4, h-4)
}

func (p *Pane) reload() {
	if !IsRepo() {
		p.err = nil
		p.changes = nil
		p.status = "not a git repository"
		return
	}
	changes, err := Status()
	p.err = err
	p.changes = changes
	if p.cursor >= len(changes) {
		p.cursor = max(0, len(changes)-1)
	}
	if b, err := CurrentBranch(); err == nil {
		p.branch = b
	}
	p.refreshDiff()
}

func (p *Pane) refreshDiff() {
	if p.showLog {
		p.refreshLog()
		return
	}
	c, ok := p.selected()
	if !ok {
		p.diff.SetContent("")
		return
	}
	var (
		out string
		err error
	)
	if c.IsStaged() {
		out, err = run("diff", "--staged", "--", c.Path)
	} else {
		out, err = Diff(c.Path)
	}
	if err != nil {
		p.diff.SetContent(ui.Err.Render(err.Error()))
		return
	}
	if strings.TrimSpace(out) == "" {
		out = "(no textual diff — new, binary, or empty)"
	}
	p.diff.SetContent(p.highlightDiff(out))
	p.diff.GotoTop()
}

// refreshLog fills the right viewport with the decorated commit log.
func (p *Pane) refreshLog() {
	out, err := Log(50)
	if err != nil {
		p.diff.SetContent(ui.Err.Render(strings.TrimSpace(out)))
		return
	}
	p.diff.SetContent(styleLog(out))
	p.diff.GotoTop()
}

// styleLog colours each `git log --oneline --decorate` line: the leading hash
// in accent, any "(branch, tag)" decoration in green.
func styleLog(out string) string {
	hash := lipgloss.NewStyle().Foreground(ui.ColorAccent)
	deco := lipgloss.NewStyle().Foreground(ui.ColorString)
	var b strings.Builder
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		h, rest, ok := strings.Cut(line, " ")
		if !ok {
			b.WriteString(line + "\n")
			continue
		}
		if strings.HasPrefix(rest, "(") {
			if end := strings.Index(rest, ")"); end != -1 {
				rest = deco.Render(rest[:end+1]) + rest[end+1:]
			}
		}
		b.WriteString(hash.Render(h) + " " + rest + "\n")
	}
	return b.String()
}

func (p *Pane) selected() (Change, bool) {
	if p.cursor < 0 || p.cursor >= len(p.changes) {
		return Change{}, false
	}
	return p.changes[p.cursor], true
}

// Update handles staging, committing, branch switching, and diff scrolling.
func (p Pane) Update(msg tea.Msg) (Pane, tea.Cmd) {
	switch msg := msg.(type) {
	case refreshMsg:
		p.reload()
		return p, nil
	case CommitMsgReady:
		p.commitBox.SetValue(strings.TrimSpace(msg.Text))
		return p, nil
	case tea.KeyMsg:
		if p.committing {
			return p.updateCommitting(msg)
		}
		if p.branching {
			return p.updateBranching(msg)
		}
		switch msg.String() {
		case "up", "k":
			if p.cursor > 0 {
				p.cursor--
				p.refreshDiff()
			}
		case "down", "j":
			if p.cursor < len(p.changes)-1 {
				p.cursor++
				p.refreshDiff()
			}
		case " ", "s":
			p.toggleStage()
		case "c":
			if hasStaged(p.changes) {
				p.committing = true
				p.commitBox.Focus()
				return p, textinput.Blink
			}
			p.status = "nothing staged to commit"
		case "b":
			return p.openBranches(), nil
		case "l":
			p.showLog = !p.showLog
			p.refreshDiff()
		case "esc":
			if p.showLog {
				p.showLog = false
				p.refreshDiff()
			}
		case "r":
			p.reload()
		}
	}
	var cmd tea.Cmd
	p.diff, cmd = p.diff.Update(msg)
	return p, cmd
}

// openBranches loads the branch list and shows the picker in the right column.
func (p Pane) openBranches() Pane {
	branches, err := Branches()
	if err != nil {
		p.status = "branches: " + err.Error()
		return p
	}
	items := make([]list.Item, len(branches))
	for i, b := range branches {
		items[i] = branchItem(b)
	}
	p.branchList.SetDelegate(branchDelegate{current: p.branch})
	p.branchList.ResetFilter()
	p.branchList.SetItems(items)
	p.branchList.Select(0)
	p.branching = true
	return p
}

// updateBranching drives the branch picker: enter checks out, esc closes (or
// cancels the filter first), everything else moves/filters the list.
func (p Pane) updateBranching(msg tea.KeyMsg) (Pane, tea.Cmd) {
	filtering := p.branchList.FilterState() == list.Filtering
	if !filtering {
		switch msg.String() {
		case "esc":
			p.branching = false
			return p, nil
		case "enter":
			b, ok := p.branchList.SelectedItem().(branchItem)
			p.branching = false
			if !ok {
				return p, nil
			}
			if out, err := Checkout(string(b)); err != nil {
				p.status = "checkout failed: " + strings.TrimSpace(out)
			} else {
				p.status = "switched to " + string(b)
			}
			p.reload()
			return p, nil
		}
	}
	var cmd tea.Cmd
	p.branchList, cmd = p.branchList.Update(msg)
	return p, cmd
}

func (p Pane) updateCommitting(msg tea.KeyMsg) (Pane, tea.Cmd) {
	if p.confirming {
		return p.updateConfirming(msg)
	}
	switch msg.String() {
	case "esc":
		return p.cancelCommit(), nil
	case "ctrl+g":
		return p, func() tea.Msg { return GenCommitMsgRequest{} }
	case "enter":
		text := strings.TrimSpace(p.commitBox.Value())
		if text == "" {
			p.status = "empty commit message"
			return p, nil
		}
		// Move to the confirm step rather than committing immediately.
		p.commitText = text
		p.confirming = true
		*p.confirmVal = true
		p.commitBox.Blur()
		p.confirm.Focus()
		return p, nil
	}
	var cmd tea.Cmd
	p.commitBox, cmd = p.commitBox.Update(msg)
	return p, cmd
}

// updateConfirming drives the huh.Confirm step. y / enter-on-Yes commit; n /
// enter-on-No return to the edit box; esc cancels the whole flow. Other keys
// (left/right) toggle the confirm's highlighted button.
func (p Pane) updateConfirming(msg tea.KeyMsg) (Pane, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return p.cancelCommit(), nil
	case "y":
		return p.doCommit()
	case "n":
		return p.backToEdit(), nil
	case "enter":
		if v, ok := p.confirm.GetValue().(bool); ok && v {
			return p.doCommit()
		}
		return p.backToEdit(), nil
	}
	model, cmd := p.confirm.Update(msg)
	p.confirm = model.(*huh.Confirm)
	return p, cmd
}

// doCommit runs the commit for the pending message and resets the flow.
func (p Pane) doCommit() (Pane, tea.Cmd) {
	text := p.commitText
	out, err := Commit(text)
	p = p.cancelCommit()
	if err != nil {
		p.status = "commit failed: " + strings.TrimSpace(out)
	} else {
		p.status = "committed: " + text
	}
	p.reload()
	return p, nil
}

// backToEdit returns from the confirm step to the (still-populated) edit box.
func (p Pane) backToEdit() Pane {
	p.confirming = false
	p.confirm.Blur()
	p.commitBox.Focus()
	return p
}

// cancelCommit tears down the entire commit flow.
func (p Pane) cancelCommit() Pane {
	p.committing = false
	p.confirming = false
	p.commitText = ""
	p.commitBox.Reset()
	p.commitBox.Blur()
	p.confirm.Blur()
	return p
}

func (p *Pane) toggleStage() {
	c, ok := p.selected()
	if !ok {
		return
	}
	var err error
	if c.IsStaged() {
		err = Unstage(c.Path)
	} else {
		err = Stage(c.Path)
	}
	if err != nil {
		p.status = err.Error()
	}
	p.reload()
}

// View renders the change list, diff, and (when active) the commit box.
func (p Pane) View() string {
	listW := p.width / 3
	if listW < 24 {
		listW = 24
	}
	var b strings.Builder
	b.WriteString(ui.Active.Render("⎇ "+p.branch) + "\n\n")
	if p.err != nil {
		b.WriteString(ui.Err.Render(p.err.Error()) + "\n")
	}
	if len(p.changes) == 0 {
		b.WriteString(ui.Muted.Render("working tree clean"))
	}
	for i, c := range p.changes {
		mark := c.Staged + c.Unstaged
		line := mark + " " + c.Path
		switch {
		case i == p.cursor:
			b.WriteString(ui.Active.Render("› " + line))
		case c.IsStaged():
			b.WriteString("  " + lipgloss.NewStyle().Foreground(ui.ColorString).Render(line))
		default:
			b.WriteString("  " + line)
		}
		b.WriteString("\n")
	}
	if p.status != "" {
		b.WriteString("\n" + ui.Muted.Render(p.status))
	}
	left := lipgloss.NewStyle().Width(listW).Height(p.height).Render(b.String())
	right := p.diff.View()
	switch {
	case p.branching:
		right = ui.Active.Render("switch branch") +
			ui.Muted.Render("  (enter switch · / filter · esc cancel)") +
			"\n\n" + p.branchList.View()
	case p.confirming:
		right = p.commitBox.View() + "\n" + p.confirm.View() + "\n" + p.diff.View()
	case p.committing:
		right = p.commitBox.View() + "\n" + p.diff.View()
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, left, ui.Pane.Render(right))
}

// Capturing reports whether a modal flow owns the keyboard: the commit edit
// box, the commit confirm step, or the branch picker.
func (p Pane) Capturing() bool { return p.committing || p.confirming || p.branching }

// Branch reports the checked-out branch for the status bar.
func (p Pane) Branch() string { return p.branch }

// DirtyCount reports the number of changed paths for the status bar.
func (p Pane) DirtyCount() int { return len(p.changes) }

// HelpBindings returns the Git-pane key hints shown in the help footer.
func (p Pane) HelpBindings() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "move")),
		key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "stage")),
		key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "commit")),
		key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "branches")),
		key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "log")),
		key.NewBinding(key.WithKeys("ctrl+g"), key.WithHelp("ctrl+g", "AI msg")),
		key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
	}
}

func hasStaged(changes []Change) bool {
	for _, c := range changes {
		if c.IsStaged() {
			return true
		}
	}
	return false
}

func (p Pane) highlightDiff(diff string) string {
	lexer := lexers.Get("diff")
	if lexer == nil {
		return diff
	}
	it, err := lexer.Tokenise(nil, diff)
	if err != nil {
		return diff
	}
	style := styles.Get(p.theme)
	if style == nil {
		style = styles.Fallback
	}
	var buf strings.Builder
	if err := formatters.Get("terminal256").Format(&buf, style, it); err != nil {
		return diff
	}
	return buf.String()
}
