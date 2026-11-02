package git

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"deck/internal/ui"
)

// GenCommitMsgRequest asks the root model to generate a commit message from the
// staged diff of Repo; Ctx is cancelled when the user aborts. The app replies with CommitMsgReady.
type GenCommitMsgRequest struct {
	Ctx  context.Context
	Repo *Repo
}

// CommitMsgReady carries a generated commit message, or the reason generation failed.
type CommitMsgReady struct {
	Text string
	Err  error
}

func (CommitMsgReady) RouteTo() ui.Dest { return ui.DestGit }

// RefreshMsg triggers an async status reload.
type RefreshMsg struct{}

func (RefreshMsg) RouteTo() ui.Dest { return ui.DestGit }

// Refresh is a command that reloads git status.
func Refresh() tea.Msg { return RefreshMsg{} }

// StatusLoadedMsg is the result of a status load. Seq drops stale loads; the
// Files tab receives it too so its badges follow every refresh.
type StatusLoadedMsg struct {
	Seq        int
	Repo       *Repo
	Snapshot   Snapshot
	IndexStamp int64
	Stamps     map[string]int64
	Err        error
}

func (StatusLoadedMsg) RouteTo() ui.Dest  { return ui.DestGit }
func (StatusLoadedMsg) AlsoTo() []ui.Dest { return []ui.Dest{ui.DestFiles} }

// DiffLoadedMsg is the result of a diff load for the entry under the cursor.
type DiffLoadedMsg struct {
	Seq      int
	Key      diffKey
	Text     string
	Rendered string
	Err      error
}

func (DiffLoadedMsg) RouteTo() ui.Dest { return ui.DestGit }

// EditMsg asks the root model to open Path in the editor at Line.
type EditMsg struct {
	Path string
	Line int
}

type diffDueMsg struct{ seq int }

type opDoneMsg struct {
	label string
	quiet bool
	err   error
}

type commitDoneMsg struct {
	subject string
	err     error
}

type headMessageMsg struct {
	text string
	err  error
}

type branchesLoadedMsg struct {
	names []string
	err   error
}

type logLoadedMsg struct {
	entries []LogEntry
	err     error
}

type showLoadedMsg struct {
	hash string
	text string
	err  error
}

func (diffDueMsg) RouteTo() ui.Dest        { return ui.DestGit }
func (opDoneMsg) RouteTo() ui.Dest         { return ui.DestGit }
func (commitDoneMsg) RouteTo() ui.Dest     { return ui.DestGit }
func (headMessageMsg) RouteTo() ui.Dest    { return ui.DestGit }
func (branchesLoadedMsg) RouteTo() ui.Dest { return ui.DestGit }
func (logLoadedMsg) RouteTo() ui.Dest      { return ui.DestGit }
func (showLoadedMsg) RouteTo() ui.Dest     { return ui.DestGit }
