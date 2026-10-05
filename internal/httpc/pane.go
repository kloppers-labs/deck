package httpc

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"

	"deck/internal/ui"
)

// requestTimeout bounds a single request so a hung endpoint can't wedge the UI.
const requestTimeout = 30 * time.Second

// maxRespBytes caps how much of a response body we read into the viewport.
const maxRespBytes = 1 << 20

// RespMsg carries a finished request's result. Exported so the root model can
// route it to this pane by type even when another tab is focused, keeping an
// in-flight request from being dropped if the user switches tabs.
type RespMsg struct {
	status      string
	code        int
	contentType string
	latency     time.Duration
	body        string
	err         error
}

// focus enumerates the editable fields, cycled with ctrl+n / ctrl+p.
const (
	focusMethod = iota
	focusURL
	focusHeaders
	focusBody
	numFields
)

// Pane is deck's HTTP tab: method + URL + headers + body editors above a
// status line and the highlighted response body.
type Pane struct {
	method    string
	methodSel *huh.Select[string]
	focus     int
	url       textinput.Model
	headers   textarea.Model
	body      textarea.Model
	resp      viewport.Model
	spinner   spinner.Model
	status    string
	theme     string
	sending   bool
	width     int
	height    int
}

// NewPane builds the HTTP tab with the URL field focused, ready to type. theme
// is the chroma style name for response-body highlighting.
func NewPane(theme string) Pane {
	u := textinput.New()
	u.Placeholder = "api.example.com/endpoint"
	u.Prompt = ""
	u.Focus()

	h := textarea.New()
	h.Placeholder = "Content-Type: application/json"
	h.ShowLineNumbers = false

	b := textarea.New()
	b.Placeholder = `{"key": "value"}`
	b.ShowLineNumbers = false

	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	sp.Style = lipgloss.NewStyle().Foreground(ui.ColorAccent)

	// Inline single-option-per-line picker keeps the compact method [URL] header;
	// filtering is off since there are only a handful of verbs. The current verb
	// is read back via Hovered(), so no value pointer is bound.
	ms := huh.NewSelect[string]().
		Options(huh.NewOptions(methods...)...).
		Inline(true).
		Filtering(false)

	return Pane{
		method:    methods[0],
		methodSel: ms,
		focus:     focusURL,
		url:       u,
		headers:   h,
		body:      b,
		resp:      viewport.New(0, 0),
		spinner:   sp,
		status:    ui.Muted.Render("ctrl+n/ctrl+p move · ctrl+s send"),
		theme:     theme,
	}
}

// SetSize lays out the request editors above the response viewport.
func (p *Pane) SetSize(w, h int) {
	p.width, p.height = w, h
	p.url.Width = w - 12
	p.headers.SetWidth(w - 4)
	p.headers.SetHeight(3)
	p.body.SetWidth(w - 4)
	p.body.SetHeight(5)

	// Rows above the response: URL line, two labelled editors, status, plus
	// padding/borders. Whatever's left goes to the response viewport.
	used := 1 + (1 + 3) + (1 + 5) + 1 + 4
	rh := h - used
	if rh < 3 {
		rh = 3
	}
	p.resp.Width = w - 4
	p.resp.Height = rh
}

// Capturing reports that the HTTP tab owns raw keystrokes (it has text inputs),
// so global single-key nav is suppressed while it's focused.
func (p Pane) Capturing() bool { return true }

// HelpBindings returns the HTTP-pane key hints shown in the help footer.
func (p Pane) HelpBindings() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("ctrl+n", "ctrl+p"), key.WithHelp("ctrl+n/p", "field")),
		key.NewBinding(key.WithKeys("left", "right"), key.WithHelp("←/→", "method")),
		key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "send")),
	}
}

// Update drives field focus, method cycling, sending, and the response.
func (p Pane) Update(msg tea.Msg) (Pane, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+n":
			p.cycleFocus(1)
			return p, nil
		case "ctrl+p":
			p.cycleFocus(-1)
			return p, nil
		case "ctrl+s":
			return p.send()
		}
		if p.focus == focusMethod {
			if msg.String() == "enter" {
				return p.send()
			}
			model, cmd := p.methodSel.Update(msg)
			p.methodSel = model.(*huh.Select[string])
			if v, ok := p.methodSel.Hovered(); ok {
				p.method = v
			}
			return p, cmd
		}
		if msg.String() == "enter" && p.focus == focusURL {
			return p.send()
		}
	case RespMsg:
		p.sending = false
		if msg.err != nil {
			p.status = ui.Err.Render(fmt.Sprintf("error · %s · %s",
				msg.latency.Round(time.Millisecond), msg.err))
			p.resp.SetContent("")
			return p, nil
		}
		p.status = statusLine(msg)
		p.resp.SetContent(highlightBody(msg.contentType, msg.body, p.theme))
		p.resp.GotoTop()
		return p, nil
	case spinner.TickMsg:
		if p.sending {
			var cmd tea.Cmd
			p.spinner, cmd = p.spinner.Update(msg)
			return p, cmd
		}
		return p, nil
	}
	return p.routeToField(msg)
}

func (p Pane) routeToField(msg tea.Msg) (Pane, tea.Cmd) {
	var cmd tea.Cmd
	switch p.focus {
	case focusURL:
		p.url, cmd = p.url.Update(msg)
	case focusHeaders:
		p.headers, cmd = p.headers.Update(msg)
	case focusBody:
		p.body, cmd = p.body.Update(msg)
	}
	return p, cmd
}

func (p *Pane) cycleFocus(dir int) {
	p.focus = (p.focus + dir + numFields) % numFields
	p.url.Blur()
	p.headers.Blur()
	p.body.Blur()
	p.methodSel.Blur()
	switch p.focus {
	case focusMethod:
		p.methodSel.Focus()
	case focusURL:
		p.url.Focus()
	case focusHeaders:
		p.headers.Focus()
	case focusBody:
		p.body.Focus()
	}
}

func (p Pane) send() (Pane, tea.Cmd) {
	if p.sending {
		return p, nil
	}
	req, err := buildRequest(p.method, p.url.Value(), p.headers.Value(), p.body.Value())
	if err != nil {
		p.status = ui.Err.Render("bad request: " + err.Error())
		return p, nil
	}
	p.sending = true
	p.status = ui.Muted.Render("sending " + p.method + " " + req.URL.String() + " …")
	return p, tea.Batch(doRequest(req), p.spinner.Tick)
}

// doRequest performs the request off the UI goroutine and reports the result as
// a RespMsg.
func doRequest(req *http.Request) tea.Cmd {
	return func() tea.Msg {
		start := time.Now()
		client := &http.Client{Timeout: requestTimeout}
		resp, err := client.Do(req)
		lat := time.Since(start)
		if err != nil {
			return RespMsg{latency: lat, err: err}
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxRespBytes))
		return RespMsg{
			status:      resp.Status,
			code:        resp.StatusCode,
			contentType: resp.Header.Get("Content-Type"),
			latency:     lat,
			body:        string(body),
		}
	}
}

func statusLine(m RespMsg) string {
	style := ui.Active
	switch {
	case m.code >= 400:
		style = lipgloss.NewStyle().Foreground(ui.ColorError)
	case m.code >= 200 && m.code < 300:
		style = lipgloss.NewStyle().Foreground(ui.ColorString)
	}
	meta := fmt.Sprintf("  · %s · %d bytes", m.latency.Round(time.Millisecond), len(m.body))
	return style.Render(m.status) + ui.Muted.Render(meta)
}

// View renders the request editors above the status line and response body.
func (p Pane) View() string {
	urlLine := lipgloss.JoinHorizontal(lipgloss.Top, p.methodView(), " ", p.url.View())
	status := p.status
	if p.sending {
		status = p.spinner.View() + " " + p.status
	}
	return urlLine + "\n" +
		p.label("Headers", focusHeaders) + "\n" + p.headers.View() + "\n" +
		p.label("Body", focusBody) + "\n" + p.body.View() + "\n" +
		status + "\n" +
		ui.Pane.Render(p.resp.View())
}

func (p Pane) methodView() string {
	if p.focus == focusMethod {
		return p.methodSel.View()
	}
	return lipgloss.NewStyle().Bold(true).Padding(0, 1).
		Foreground(ui.ColorAccent).Render(p.method)
}

func (p Pane) label(text string, field int) string {
	if p.focus == field {
		return ui.Active.Render("▸ " + text)
	}
	return ui.Muted.Render("  " + text)
}
