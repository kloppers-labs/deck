package ai

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"deck/internal/git"
	"deck/internal/ui"
)

// backends are cycled through with the switch key, matching the CLIs deck
// shells out to.
var backends = []string{"claude", "codex", "mods"}

// Pane is deck's AI tab: a prompt box on top, streamed output below. It reuses
// the same CLI adapter as the micro aiassist plugin so behaviour matches.
type Pane struct {
	backend  string
	model    string
	input    textinput.Model
	output   viewport.Model
	spinner  spinner.Model
	stream   *Stream
	busy     bool
	content  string // accumulated output; a plain string so the value-receiver
	lastFile string // Update can copy Pane safely (a strings.Builder cannot)
	width    int
	height   int
}

// NewPane builds the AI tab with the configured backend/model defaults.
func NewPane(backend, model string) Pane {
	ti := textinput.New()
	ti.Placeholder = "Ask the AI… (Enter to send)"
	ti.Focus()
	ti.Prompt = "» "
	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	sp.Style = lipgloss.NewStyle().Foreground(ui.ColorAccent)
	return Pane{
		backend: backend,
		model:   model,
		input:   ti,
		output:  viewport.New(0, 0),
		spinner: sp,
	}
}

// SetSize lays out the prompt and output regions.
func (p *Pane) SetSize(w, h int) {
	p.width, p.height = w, h
	p.input.Width = w - 6
	p.output.Width = w - 4
	p.output.Height = h - 4
}

// AskAbout seeds the pane with a file's contents and starts an explanation,
// used when the file navigator sends a file to AI.
func (p Pane) AskAbout(path string) (Pane, tea.Cmd) {
	data, err := os.ReadFile(path)
	if err != nil {
		p.content = ui.Err.Render("cannot read " + path + ": " + err.Error())
		p.output.SetContent(p.content)
		return p, nil
	}
	p.lastFile = path
	prompt := fmt.Sprintf("Explain this file (%s):\n\n%s", path, string(data))
	return p.start(prompt, "Explaining "+path)
}

// GenerateTests asks the AI to write unit tests for a file. Used by the file
// navigator's "t" action and the AI tab's ctrl+t shortcut.
func (p Pane) GenerateTests(path string) (Pane, tea.Cmd) {
	data, err := os.ReadFile(path)
	if err != nil {
		return p.message(ui.Err.Render("cannot read " + path + ": " + err.Error()))
	}
	p.lastFile = path
	prompt := fmt.Sprintf(
		"Write thorough unit tests for this file (%s). Match the language's idiomatic "+
			"test framework and the project's existing test style. Output only the test code.\n\n%s",
		path, string(data))
	return p.start(prompt, "Generating tests for "+path)
}

// Review runs an AI code review over the currently staged git diff.
func (p Pane) Review() (Pane, tea.Cmd) {
	diff, err := git.StagedDiff()
	if err != nil {
		return p.message(ui.Err.Render("git diff failed: " + err.Error()))
	}
	if strings.TrimSpace(diff) == "" {
		return p.message(ui.Muted.Render("nothing staged — stage changes in the Git tab first"))
	}
	prompt := "Review this staged git diff like a senior engineer. Call out bugs, risky " +
		"changes, and concrete improvements; be concise and specific with file:line refs.\n\n" + diff
	return p.start(prompt, "Reviewing staged diff")
}

// message shows a one-off note in the output pane without starting a request.
func (p Pane) message(text string) (Pane, tea.Cmd) {
	p.content = text
	p.output.SetContent(p.content)
	return p, nil
}

func (p Pane) start(prompt, header string) (Pane, tea.Cmd) {
	if p.busy {
		return p, nil
	}
	p.busy = true
	p.content = ui.Muted.Render(header) + "\n\n"
	p.output.SetContent(p.content)
	stream, cmd := Start(p.backend, p.model, prompt)
	p.stream = stream
	return p, tea.Batch(cmd, p.spinner.Tick)
}

// Update drives the prompt box, backend switching, and streaming output.
func (p Pane) Update(msg tea.Msg) (Pane, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+b":
			p.cycleBackend()
			return p, nil
		case "ctrl+r":
			return p.Review()
		case "ctrl+t":
			if p.lastFile == "" {
				return p.message(ui.Muted.Render("no file in context — open one from the Files tab (press a) first"))
			}
			return p.GenerateTests(p.lastFile)
		case "enter":
			if v := strings.TrimSpace(p.input.Value()); v != "" && !p.busy {
				header := fmt.Sprintf("[%s] %s", p.backend, v)
				p.input.Reset()
				return p.start(v, header)
			}
			return p, nil
		}
	case ChunkMsg:
		p.content += msg.Text
		p.output.SetContent(p.content)
		p.output.GotoBottom()
		if p.stream != nil {
			return p, p.stream.Next()
		}
		return p, nil
	case DoneMsg:
		p.busy = false
		if msg.Err != nil {
			p.content += "\n" + ui.Err.Render("error: "+msg.Err.Error())
			if msg.Stderr != "" {
				p.content += "\n" + ui.Muted.Render(msg.Stderr)
			}
			p.output.SetContent(p.content)
		}
		p.stream = nil
		return p, nil
	case spinner.TickMsg:
		if p.busy {
			var cmd tea.Cmd
			p.spinner, cmd = p.spinner.Update(msg)
			return p, cmd
		}
		return p, nil
	}

	var cmd tea.Cmd
	if p.busy {
		p.output, cmd = p.output.Update(msg)
	} else {
		p.input, cmd = p.input.Update(msg)
	}
	return p, cmd
}

func (p *Pane) cycleBackend() {
	for i, b := range backends {
		if b == p.backend {
			p.backend = backends[(i+1)%len(backends)]
			return
		}
	}
	p.backend = backends[0]
}

// View renders the prompt box and streamed output.
func (p Pane) View() string {
	status := ui.Muted.Render("backend: ") + ui.Active.Render(p.backend)
	if p.busy {
		status += "  " + p.spinner.View() + ui.Muted.Render(" thinking…")
	}
	header := lipgloss.JoinHorizontal(lipgloss.Top, p.input.View())
	return header + "\n" + status + "\n" + ui.Pane.Render(p.output.View())
}

// Backend reports the currently selected backend (for the help/status bar).
func (p Pane) Backend() string { return p.backend }

// ModelName reports the configured model (for the status bar).
func (p Pane) ModelName() string { return p.model }

// Capturing reports whether the prompt box should receive raw keystrokes. The
// AI prompt is always focused, so it always captures.
func (p Pane) Capturing() bool { return true }

// HelpBindings returns the AI-pane key hints shown in the help footer.
func (p Pane) HelpBindings() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "send")),
		key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "review diff")),
		key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("ctrl+t", "tests")),
		key.NewBinding(key.WithKeys("ctrl+b"), key.WithHelp("ctrl+b", "backend")),
	}
}
