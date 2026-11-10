package editor

import (
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kloppers-labs/deck/internal/lsp"
	"github.com/kloppers-labs/deck/internal/ui"
)

// BundleConfig drives one sync of deck's micro runtime. Nil lookups use the real
// system; Quiet records issues without asking for them to be shown.
type BundleConfig struct {
	Editor       string
	Dir          string
	LSP          bool
	Quiet        bool
	Specs        []lsp.ServerSpec
	LookPath     func(string) (string, error)
	LookupServer func(string) (string, error)
	Version      func(string) string
}

// SyncedMsg reports the startup sync; Notify is set when Issues are new and should be shown.
type SyncedMsg struct {
	Features Features
	Issues   []string
	Notify   bool
	Err      error
}

func (SyncedMsg) RouteTo() ui.Dest { return ui.DestApp }

// ResyncMsg asks for the micro bundle to be synced again, quietly, e.g. after a
// language server was installed.
type ResyncMsg struct{}

func (ResyncMsg) RouteTo() ui.Dest { return ui.DestApp }

// SyncCmd runs RunSync off the UI goroutine.
func SyncCmd(c BundleConfig) tea.Cmd { return func() tea.Msg { return RunSync(c) } }

// RunSync probes micro, renders z_deck.lua, syncs the bundle into c.Dir and runs Check.
func RunSync(c BundleConfig) SyncedMsg {
	if c.LookPath == nil {
		c.LookPath = exec.LookPath
	}
	if c.LookupServer == nil {
		c.LookupServer = lsp.NewRegistry(c.Specs, lsp.DefaultEnv()).LookupServer
	}
	if c.Version == nil {
		c.Version = MicroVersion
	}
	name, _ := editorFields(c.Editor)
	env := CheckEnv{LookPath: c.LookPath}
	microPath, err := c.LookPath(name)
	if err != nil {
		issues := Check(env)
		return SyncedMsg{Issues: issues, Notify: NotifyOnce(c.Dir, "", issues)}
	}
	env.MicroPath = microPath
	env.MicroVersion = c.Version(microPath)
	zDeck, missing := RenderZDeck(c.Specs, c.LookupServer)
	if c.LSP {
		env.Missing = missing
	}
	res, err := Sync(c.Dir, env.MicroVersion, SyncOptions{LSP: c.LSP, LookPath: c.LookPath, ZDeck: zDeck})
	if err != nil {
		return SyncedMsg{Err: err}
	}
	issues := append(Check(env), res.Warnings...)
	notify := NotifyOnce(c.Dir, res.Digest, issues)
	return SyncedMsg{Features: res.Features, Issues: issues, Notify: notify && len(issues) > 0 && !c.Quiet}
}
