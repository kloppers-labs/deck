package files

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/kloppers-labs/deck/internal/editor"
	"github.com/kloppers-labs/deck/internal/ui"
	"github.com/kloppers-labs/deck/internal/viewer"
)

const (
	installTimeout   = 5 * time.Minute
	installToastTTL  = 15 * time.Second
	installTailLines = 2
)

// Installer is an LSP that can install a missing language server.
type Installer interface {
	Install(ctx context.Context, server string) (string, error)
}

var installerLookPath = exec.LookPath

type installOffer struct {
	server, label, language, pkg string
	argv                         []string
}

type installDoneMsg struct {
	offer installOffer
	out   string
	err   error
}

func (installDoneMsg) RouteTo() ui.Dest { return ui.DestFiles }

// offerFor offers st's server when it failed with an install command whose
// installer is on PATH and skip doesn't hold it.
func offerFor(st viewer.ServerStatus, skip map[string]bool, lookPath func(string) (string, error)) (installOffer, bool) {
	if st.State != viewer.ServerFailed || st.Server == "" || len(st.Install) == 0 || skip[st.Server] {
		return installOffer{}, false
	}
	if _, err := lookPath(st.Install[0]); err != nil {
		return installOffer{}, false
	}
	lang := st.Language
	if lang == "" {
		lang = st.Label
	}
	if lang == "" {
		lang = st.Server
	}
	return installOffer{server: st.Server, label: st.Label, language: lang, pkg: installPackage(st.Install), argv: slices.Clone(st.Install)}, true
}

// installPackage names what argv installs: the first argument after
// "install"/"add" that isn't a flag, without a version or import path.
func installPackage(argv []string) string {
	for i, a := range argv {
		if a != "install" && a != "add" {
			continue
		}
		for _, p := range argv[i+1:] {
			if strings.HasPrefix(p, "-") {
				continue
			}
			if at := strings.LastIndexByte(p, '@'); at > 0 {
				p = p[:at]
			}
			return path.Base(p)
		}
	}
	if len(argv) == 0 {
		return ""
	}
	return path.Base(argv[len(argv)-1])
}

func (o installOffer) question() string {
	return fmt.Sprintf("%s language server isn't installed. Install %s now? (%s)", o.language, o.pkg, strings.Join(o.argv, " "))
}

func (m *Model) maybeOffer() {
	if !m.offerInstall || m.offer != nil || m.nav == nil || m.pick.Active() || m.prompt.kind != promptNone {
		return
	}
	if _, ok := m.nav.(Installer); !ok {
		return
	}
	p := m.viewer.Path()
	if p == "" || p == m.cardPath || viewer.IsVirtual(p) || !m.viewer.Loaded() {
		return
	}
	if o, ok := offerFor(m.nav.Status(p), m.noOffer, installerLookPath); ok {
		m.offer = &o
	}
}

func (m Model) offerKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	o := *m.offer
	m.offer = nil
	switch msg.String() {
	case "y", "Y":
		m.noOffer[o.server] = true
		return m.startInstall(o)
	case "n", "N", "esc":
		m.noOffer[o.server] = true
	}
	return m, nil
}

func (m Model) startInstall(o installOffer) (Model, tea.Cmd) {
	inst, ok := m.nav.(Installer)
	if !ok {
		return m, nil
	}
	m.lspStatus = upsertStatus(m.lspStatus, viewer.ServerStatus{
		Server: o.server, Label: o.label, Language: o.language, State: viewer.ServerStarting,
		Title: "installing " + o.pkg, Percent: -1, Since: m.now(),
	})
	return m, tea.Batch(installCmd(inst, o), m.startLSPTick(), ui.Toast(ui.LevelInfo, "installing "+o.pkg+"…"))
}

func installCmd(inst Installer, o installOffer) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), installTimeout)
		defer cancel()
		out, err := inst.Install(ctx, o.server)
		if errors.Is(err, context.DeadlineExceeded) {
			err = fmt.Errorf("timed out after %s", installTimeout)
		}
		return installDoneMsg{offer: o, out: out, err: err}
	}
}

func (m Model) installDone(msg installDoneMsg) (Model, tea.Cmd) {
	o := msg.offer
	m.lspStatus = withoutStatus(m.lspStatus, o.server, "")
	if msg.err != nil {
		m.lspStatus = upsertStatus(m.lspStatus, viewer.ServerStatus{
			Server: o.server, Label: o.label, Language: o.language, State: viewer.ServerFailed, Err: msg.err.Error(), Percent: -1,
		})
		text := o.pkg + " install failed: " + failureDetail(msg.err, msg.out)
		return m, func() tea.Msg { return ui.ToastMsg{Level: ui.LevelError, Text: text, TTL: installToastTTL} }
	}
	text := "installed " + o.pkg + ", starting the " + o.language + " language server"
	cmds := []tea.Cmd{
		func() tea.Msg { return ui.ToastMsg{Level: ui.LevelInfo, Text: text, TTL: installToastTTL} },
		func() tea.Msg { return editor.ResyncMsg{} },
	}
	if p := m.viewer.Path(); p != "" && m.nav != nil && m.nav.Status(p).Server == o.server {
		var cmd tea.Cmd
		m.viewer, cmd = m.viewer.Reload()
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func withoutStatus(list []viewer.ServerStatus, server, project string) []viewer.ServerStatus {
	return slices.DeleteFunc(slices.Clone(list), func(s viewer.ServerStatus) bool {
		return s.Server == server && s.Project == project
	})
}

func failureDetail(err error, out string) string {
	var tail []string
	lines := strings.Split(strings.TrimSpace(ansi.Strip(out)), "\n")
	for i := len(lines) - 1; i >= 0 && len(tail) < installTailLines; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			tail = append([]string{l}, tail...)
		}
	}
	if len(tail) == 0 {
		return err.Error()
	}
	return err.Error() + ": " + strings.Join(tail, " · ")
}

var (
	offerBox      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(ui.ColorWarn).Padding(0, 1)
	offerQuestion = lipgloss.NewStyle().Foreground(ui.ColorWarn).Bold(true)
	offerBindings = []key.Binding{
		key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "install")),
		key.NewBinding(key.WithKeys("n", "esc"), key.WithHelp("n/esc", "don't ask again")),
		key.NewBinding(key.WithKeys("any"), key.WithHelp("any other key", "later")),
	}
)

func (m Model) offerView() string {
	q := m.offer.question()
	inner := max(min(ansi.StringWidth(q)+5, m.width-4), 1)
	text := offerQuestion.Render(q) + ui.Muted.Render("  y/n")
	return offerBox.Render(lipgloss.NewStyle().Width(inner).Render(text))
}

func (m Model) overlayOffer(body string) string {
	box := m.offerView()
	x := max((m.width-lipgloss.Width(box))/2, 0)
	y := max(m.height-lipgloss.Height(box), 0)
	return ui.Overlay(body, box, x, y)
}
