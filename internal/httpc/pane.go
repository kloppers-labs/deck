package httpc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kloppers-labs/deck/internal/ui"
)

const (
	requestTimeout = 30 * time.Second
	maxRespBytes   = 1 << 20
	wheelLines     = 3
	methodWidth    = 9
)

// RespMsg carries a finished request's result; seq ties it to the send that
// started it so a cancelled request's late reply is ignored.
type RespMsg struct {
	seq         int
	status      string
	code        int
	contentType string
	headers     http.Header
	latency     time.Duration
	body        string
	truncated   bool
	err         error
}

func (RespMsg) RouteTo() ui.Dest { return ui.DestHTTP }

const (
	focusMethod = iota
	focusURL
	focusHeaders
	focusBody
	numFields
)

const (
	tabBody = iota
	tabHeaders
)

const (
	headersRows = 3
	bodyRows    = 5
	headersTop  = 2
	bodyTop     = headersTop + headersRows + 1
	statusRow   = bodyTop + bodyRows
	requestRows = statusRow + 1
	respChrome  = 3
)

// Pane is the HTTP tab. Normal mode reads and scrolls the response and leaves
// global keys alone; insert mode types into the focused field.
type Pane struct {
	method  string
	focus   int
	insert  bool
	url     textinput.Model
	headers textarea.Model
	body    textarea.Model
	resp    viewport.Model
	respTab int
	last    *RespMsg
	spinner spinner.Model
	status  string
	auto    []string
	theme   string
	sending bool
	seq     int
	cancel  context.CancelFunc
	sidebar sidebar
	envs    envSet
	env     string
	vars    map[string]string
	dotenv  map[string]string
	width   int
	height  int
}

// NewPane builds the HTTP tab in normal mode with the URL field selected.
// theme is the chroma style for response highlighting.
func NewPane(theme string) Pane {
	u := textinput.New()
	u.Placeholder = "localhost:5000/endpoint"
	u.Prompt = ""

	h := textarea.New()
	h.Placeholder = "Accept: application/json"
	h.ShowLineNumbers = false

	b := textarea.New()
	b.Placeholder = `{"key": "value"}`
	b.ShowLineNumbers = false

	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	sp.Style = lipgloss.NewStyle().Foreground(ui.ColorAccent)

	return Pane{
		method:  methods[0],
		focus:   focusURL,
		url:     u,
		headers: h,
		body:    b,
		resp:    viewport.New(0, 0),
		spinner: sp,
		theme:   theme,
	}
}

func (p *Pane) SetSize(w, h int) {
	p.width, p.height = w, h
	p.url.Width = max(w-methodWidth-2, 1)
	p.headers.SetWidth(max(w-4, 1))
	p.headers.SetHeight(headersRows)
	p.body.SetWidth(max(w-4, 1))
	p.body.SetHeight(bodyRows)
	p.resp.Width = max(w-4, 0)
	p.resp.Height = max(h-requestRows-respChrome, 1)
}

// Capturing is true in insert mode and while the request sidebar is open.
func (p Pane) Capturing() bool { return p.insert || p.sidebar.open }

func (p Pane) HelpBindings() []key.Binding {
	if p.insert {
		return []key.Binding{
			key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "send")),
			key.NewBinding(key.WithKeys("ctrl+n", "ctrl+p"), key.WithHelp("ctrl+n/p", "field")),
			key.NewBinding(key.WithKeys("left", "right"), key.WithHelp("←/→", "method")),
		}
	}
	return []key.Binding{
		key.NewBinding(key.WithKeys("i", "enter"), key.WithHelp("i", "edit")),
		key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "send")),
		key.NewBinding(key.WithKeys("j", "k"), key.WithHelp("j/k g/G", "scroll")),
		key.NewBinding(key.WithKeys("[", "]"), key.WithHelp("[ ]", "body/headers")),
		key.NewBinding(key.WithKeys("ctrl+n", "ctrl+p"), key.WithHelp("ctrl+n/p", "field")),
		key.NewBinding(key.WithKeys("ctrl+o"), key.WithHelp("ctrl+o", ".http")),
		key.NewBinding(key.WithKeys("ctrl+e"), key.WithHelp("ctrl+e", "env")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	}
}

func (p Pane) Update(msg tea.Msg) (Pane, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return p.key(msg)
	case tea.MouseMsg:
		return p.mouse(msg)
	case RespMsg:
		return p.response(msg), nil
	case LoadRequestMsg:
		return p, tea.Batch(loadCmd(msg.Path, msg.Line), ui.FocusTab(ui.DestHTTP))
	case requestLoadedMsg:
		return p.loaded(msg)
	case httpFilesMsg:
		p.sidebar.items = msg.items
		p.sidebar.loaded = true
		p.sidebar.move(0)
		if len(p.envs.names) == 0 && len(msg.envs.names) > 0 {
			p.envs = msg.envs
			p.env = msg.envs.names[0]
		}
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

func (p Pane) key(msg tea.KeyMsg) (Pane, tea.Cmd) {
	if p.sidebar.open {
		return p.sidebarKey(msg)
	}
	k := msg.String()
	switch k {
	case "ctrl+s":
		p.setInsert(false)
		return p.send()
	case "ctrl+o":
		p.sidebar.open = true
		if !p.sidebar.loaded {
			return p, discoverCmd("")
		}
		return p, nil
	case "ctrl+e":
		p.env = p.envs.next(p.env)
		return p, nil
	case "ctrl+n":
		return p, p.setFocus((p.focus + 1) % numFields)
	case "ctrl+p":
		return p, p.setFocus((p.focus + numFields - 1) % numFields)
	case "esc":
		p.cancelRequest()
		p.setInsert(false)
		return p, nil
	}
	if p.focus == focusMethod {
		switch {
		case k == "left" || (!p.insert && k == "h"):
			p.cycleMethod(-1)
			return p, nil
		case k == "right" || (!p.insert && k == "l"):
			p.cycleMethod(1)
			return p, nil
		}
	}
	if p.insert {
		if k == "enter" && (p.focus == focusURL || p.focus == focusMethod) {
			p.setInsert(false)
			return p.send()
		}
		return p.routeToField(msg)
	}
	return p.normalKey(k)
}

func (p Pane) normalKey(k string) (Pane, tea.Cmd) {
	switch k {
	case "i", "a", "enter":
		return p, p.setInsert(true)
	case "j", "down":
		p.resp.ScrollDown(1)
	case "k", "up":
		p.resp.ScrollUp(1)
	case "ctrl+d", "d":
		p.resp.HalfPageDown()
	case "ctrl+u", "u":
		p.resp.HalfPageUp()
	case "pgdown", " ", "f":
		p.resp.PageDown()
	case "pgup", "b":
		p.resp.PageUp()
	case "g", "home":
		p.resp.GotoTop()
	case "G", "end":
		p.resp.GotoBottom()
	case "[":
		p.showTab(tabBody)
	case "]":
		p.showTab(tabHeaders)
	}
	return p, nil
}

func (p Pane) sidebarKey(msg tea.KeyMsg) (Pane, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+o", "q":
		p.sidebar.open = false
	case "j", "down", "ctrl+n":
		p.sidebar.move(1)
	case "k", "up", "ctrl+p":
		p.sidebar.move(-1)
	case "g", "home":
		p.sidebar.cursor = 0
	case "G", "end":
		p.sidebar.move(len(p.sidebar.items))
	case "ctrl+e":
		p.env = p.envs.next(p.env)
	case "enter":
		it, ok := p.sidebar.selected()
		if !ok {
			return p, nil
		}
		p.sidebar.open = false
		return p, loadCmd(it.path, it.req.Start)
	}
	return p, nil
}

func (p Pane) mouse(msg tea.MouseMsg) (Pane, tea.Cmd) {
	if p.sidebar.open {
		return p, nil
	}
	if tea.MouseEvent(msg).IsWheel() {
		if msg.Button == tea.MouseButtonWheelDown {
			p.resp.ScrollDown(wheelLines)
		} else {
			p.resp.ScrollUp(wheelLines)
		}
		return p, nil
	}
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return p, nil
	}
	field := -1
	switch {
	case msg.Y == 0 && msg.X < methodWidth:
		field = focusMethod
	case msg.Y == 0:
		field = focusURL
	case msg.Y >= headersTop-1 && msg.Y < bodyTop-1:
		field = focusHeaders
	case msg.Y >= bodyTop-1 && msg.Y < statusRow:
		field = focusBody
	}
	if field < 0 {
		p.setInsert(false)
		return p, nil
	}
	p.focus = field
	return p, p.setInsert(true)
}

func (p *Pane) cycleMethod(d int) {
	i := slices.Index(methods, p.method)
	if i < 0 {
		i = 0
	}
	p.method = methods[(i+d+len(methods))%len(methods)]
}

func (p *Pane) setFocus(f int) tea.Cmd {
	p.focus = f
	return p.setInsert(p.insert)
}

func (p *Pane) setInsert(on bool) tea.Cmd {
	p.insert = on
	p.url.Blur()
	p.headers.Blur()
	p.body.Blur()
	if !on {
		return nil
	}
	switch p.focus {
	case focusURL:
		return p.url.Focus()
	case focusHeaders:
		return p.headers.Focus()
	case focusBody:
		return p.body.Focus()
	}
	return nil
}

func (p Pane) routeToField(msg tea.Msg) (Pane, tea.Cmd) {
	if !p.insert {
		return p, nil
	}
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

func (p *Pane) showTab(t int) {
	p.respTab = t
	p.renderResponse()
	p.resp.GotoTop()
}

func (p *Pane) renderResponse() {
	if p.last == nil || p.last.err != nil {
		p.resp.SetContent("")
		return
	}
	if p.respTab == tabHeaders {
		p.resp.SetContent(formatHeaders(p.last.headers))
		return
	}
	p.resp.SetContent(highlightBody(p.last.contentType, p.last.body, p.theme))
}

func (p Pane) resolver() resolver {
	return resolver{file: p.vars, env: p.envs.vars(p.env), dotenv: p.dotenv}
}

func (p Pane) send() (Pane, tea.Cmd) {
	if p.sending {
		return p, nil
	}
	r := p.resolver()
	req, auto, err := buildRequest(p.method, r.resolve(p.url.Value()), r.resolve(p.headers.Value()), r.resolve(p.body.Value()))
	if err != nil {
		p.status = ui.Err.Render("bad request: " + err.Error())
		return p, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	p.seq++
	p.cancel = cancel
	p.sending = true
	p.auto = auto
	p.status = ui.Muted.Render("sending " + p.method + " " + req.URL.String() + " …")
	return p, tea.Batch(doRequest(ctx, req, p.seq), p.spinner.Tick)
}

func (p *Pane) cancelRequest() {
	if !p.sending {
		return
	}
	if p.cancel != nil {
		p.cancel()
	}
	p.cancel = nil
	p.sending = false
	p.seq++
	p.status = ui.Muted.Render("cancelled")
}

func (p Pane) response(msg RespMsg) Pane {
	if msg.seq != p.seq {
		return p
	}
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.sending = false
	p.last = &msg
	if msg.err != nil {
		p.status = ui.Err.Render(fmt.Sprintf("error · %s · %s", msg.latency.Round(time.Millisecond), msg.err))
	} else {
		p.status = statusLine(msg)
	}
	p.renderResponse()
	p.resp.GotoTop()
	return p
}

func (p Pane) loaded(msg requestLoadedMsg) (Pane, tea.Cmd) {
	if msg.err != nil {
		p.status = ui.Err.Render("load: " + msg.err.Error())
		return p, nil
	}
	p.method = msg.req.Method
	p.url.SetValue(msg.req.URL)
	p.headers.SetValue(strings.Join(msg.req.Headers, "\n"))
	p.body.SetValue(msg.req.Body)
	p.vars, p.dotenv = msg.vars, msg.dotenv
	if len(msg.envs.names) > 0 || len(msg.envs.byEnv) > 0 {
		p.envs = msg.envs
		if !slices.Contains(p.envs.names, p.env) && len(p.envs.names) > 0 {
			p.env = p.envs.names[0]
		}
	}
	p.auto = nil
	name := msg.req.Name
	if name == "" {
		name = msg.req.Method + " " + msg.req.URL
	}
	p.status = ui.Muted.Render("loaded " + name + " · ctrl+s send")
	p.setInsert(false)
	return p, nil
}

// doRequest performs req and reports the result; latency includes reading
// the body, which is capped at maxRespBytes.
func doRequest(ctx context.Context, req *http.Request, seq int) tea.Cmd {
	return func() tea.Msg {
		start := time.Now()
		resp, err := http.DefaultClient.Do(req.WithContext(ctx))
		if err != nil {
			return RespMsg{seq: seq, latency: time.Since(start), err: friendlyErr(err)}
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxRespBytes+1))
		lat := time.Since(start)
		if err != nil {
			return RespMsg{seq: seq, latency: lat, err: friendlyErr(err)}
		}
		truncated := len(body) > maxRespBytes
		if truncated {
			body = body[:maxRespBytes]
		}
		return RespMsg{
			seq:         seq,
			status:      resp.Status,
			code:        resp.StatusCode,
			contentType: resp.Header.Get("Content-Type"),
			headers:     resp.Header,
			latency:     lat,
			body:        string(body),
			truncated:   truncated,
		}
	}
}

func friendlyErr(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("timed out after %s", requestTimeout)
	}
	return err
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
	s := style.Render(m.status) + ui.Muted.Render(meta)
	if m.truncated {
		s += "  " + lipgloss.NewStyle().Foreground(ui.ColorWarn).Render("truncated at 1 MiB")
	}
	return s
}

func (p Pane) View() string {
	status := p.status
	if status == "" {
		status = ui.Muted.Render("i edit · ctrl+s send · ctrl+o .http requests · ctrl+e env")
	}
	if p.sending {
		status = p.spinner.View() + " " + status
	}
	box := ui.Pane
	if !p.insert {
		box = ui.PaneActive
	}
	respView := p.tabLine() + "\n" + ui.Fit(p.resp.View(), p.resp.Width, p.resp.Height)
	view := ui.FitWidth(p.methodView()+" "+p.url.View(), p.width, "") + "\n" +
		p.label("Headers", focusHeaders) + p.autoHeaders() + "\n" + p.headers.View() + "\n" +
		p.label("Body", focusBody) + "\n" + p.body.View() + "\n" +
		ui.FitWidth(status, p.width, "…") + "\n" +
		box.Render(ui.Fit(respView, p.resp.Width, p.resp.Height+1))
	view = ui.Fit(view, p.width, p.height)
	if p.sidebar.open {
		w := min(sidebarMaxWidth, p.width)
		view = ui.Fit(ui.Overlay(view, p.sidebar.view(w, p.height), 0, 0), p.width, p.height)
	}
	return view
}

func (p Pane) tabLine() string {
	tabs := []string{"Body", "Headers"}
	for i, t := range tabs {
		if i == p.respTab {
			tabs[i] = ui.Active.Render(t)
		} else {
			tabs[i] = ui.Muted.Render(t)
		}
	}
	left := strings.Join(tabs, ui.Muted.Render(" │ "))
	right := ""
	if p.env != "" {
		right = ui.Muted.Render("env " + p.env)
	}
	pad := max(p.resp.Width-lipgloss.Width(left)-lipgloss.Width(right), 1)
	return ui.FitWidth(left+strings.Repeat(" ", pad)+right, p.resp.Width, "")
}

func (p Pane) autoHeaders() string {
	if len(p.auto) == 0 {
		return ""
	}
	parts := make([]string, len(p.auto))
	for i, h := range p.auto {
		parts[i] = h + " (auto)"
	}
	return ui.Dim.Render("  + " + strings.Join(parts, ", "))
}

func (p Pane) methodView() string {
	text := p.method
	style := lipgloss.NewStyle().Bold(true).Foreground(ui.ColorAccent)
	if p.focus == focusMethod {
		text = "‹" + text + "›"
		style = style.Underline(true)
	}
	return ui.Fit(style.Render(" "+text), methodWidth, 1)
}

func (p Pane) label(text string, field int) string {
	switch {
	case p.focus == field && p.insert:
		return ui.Active.Render("✎ " + text)
	case p.focus == field:
		return ui.Active.Render("▸ " + text)
	}
	return ui.Muted.Render("  " + text)
}
