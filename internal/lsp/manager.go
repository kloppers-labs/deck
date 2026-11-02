package lsp

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"sync"
)

type Option func(*Manager)

func WithEnv(env Env) Option { return func(m *Manager) { m.env = env } }

func WithStarter(s Starter) Option { return func(m *Manager) { m.starter = s } }

func withClock(c clock) Option { return func(m *Manager) { m.clock = c } }

func withTimings(t timings) Option { return func(m *Manager) { m.timing = t } }

func withSpecs(specs []ServerSpec) Option { return func(m *Manager) { m.specs = specs } }

// WithInstaller replaces how install commands run; it must return the combined output.
func WithInstaller(run InstallRunner) Option { return func(m *Manager) { m.installer = run } }

type ServerLog struct {
	Server string
	Root   string
	Stderr string
}

// Manager starts one Client per (server, root) on demand and fans their events into one channel.
type Manager struct {
	cfg     Config
	env     Env
	specs   []ServerSpec
	starter Starter
	clock   clock
	timing  timings
	reg     *Registry

	installer InstallRunner

	queue  *eventQueue
	events chan any
	done   chan struct{}

	mu       sync.Mutex
	clients  map[string]*Client
	failed   map[string]StatusEvent
	installs map[int]context.CancelFunc
	nextID   int
	closed   bool
}

func NewManager(cfg Config, opts ...Option) *Manager {
	m := &Manager{
		cfg:       cfg,
		env:       DefaultEnv(),
		starter:   StartProcess,
		clock:     realClock{},
		timing:    defaultTimings(),
		queue:     newEventQueue(eventBuffer),
		events:    make(chan any),
		done:      make(chan struct{}),
		clients:   map[string]*Client{},
		failed:    map[string]StatusEvent{},
		installs:  map[int]context.CancelFunc{},
		installer: RunInstall,
	}
	for _, o := range opts {
		o(m)
	}
	if m.specs == nil {
		m.specs = MergeSpecs(DefaultSpecs(), cfg.Servers)
	}
	m.reg = NewRegistry(m.specs, m.env)
	go m.pump()
	return m
}

func (m *Manager) Registry() *Registry { return m.reg }

// Events delivers StatusEvent, DiagnosticsEvent, LogEvent and ShowMessageEvent; it closes after Shutdown.
func (m *Manager) Events() <-chan any { return m.events }

func (m *Manager) pump() {
	defer close(m.events)
	for {
		ev, ok := m.queue.pop()
		if !ok {
			select {
			case <-m.queue.signal:
				continue
			case <-m.done:
				return
			}
		}
		select {
		case m.events <- ev:
		case <-m.done:
			return
		}
	}
}

func (m *Manager) emit(ev any) { m.queue.push(ev) }

// ClientFor returns the running client for path's server and project root, starting it if needed.
func (m *Manager) ClientFor(ctx context.Context, path string) (*Client, error) {
	if !m.cfg.Enabled {
		return nil, ErrDisabled
	}
	spec, ok := m.reg.SpecFor(path)
	if !ok {
		_, err := m.reg.Resolve(path)
		return nil, err
	}
	if err := m.failure(spec.Name); err != nil {
		return nil, err
	}
	if IsVirtual(path) {
		return m.runningClient(spec.Name, path)
	}
	res, err := m.reg.Resolve(path)
	if err != nil {
		return nil, m.fail(spec, err)
	}
	return m.clientFor(res)
}

// Prestart starts servers whose prestart markers are found from cwd, e.g. csharp-ls for a .sln.
func (m *Manager) Prestart(ctx context.Context, cwd string) error {
	if !m.cfg.Enabled || !m.cfg.Prestart {
		return nil
	}
	var errs []error
	for _, spec := range m.reg.Specs() {
		if !spec.Enabled || len(spec.Command) == 0 || len(spec.PrestartMarkers) == 0 {
			continue
		}
		if _, _, ok := findRootFrom(cwd, spec.PrestartMarkers, m.reg.boundaryFrom(cwd)); !ok {
			continue
		}
		if m.failure(spec.Name) != nil {
			continue
		}
		res, err := m.reg.resolveIn(spec, cwd)
		if err != nil {
			errs = append(errs, m.fail(spec, err))
			continue
		}
		if _, err := m.clientFor(res); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (m *Manager) failure(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrShutdown
	}
	if ev, ok := m.failed[name]; ok {
		return ev.Err
	}
	return nil
}

func (m *Manager) fail(spec ServerSpec, err error) error {
	var missing *MissingBinaryError
	if !errors.As(err, &missing) {
		return err
	}
	ev := StatusEvent{Server: spec.Name, State: StateFailed, Err: err, Hint: missing.Hint, Install: missing.Install, Percent: -1, Since: m.clock.Now()}
	m.mu.Lock()
	_, seen := m.failed[spec.Name]
	m.failed[spec.Name] = ev
	m.mu.Unlock()
	if !seen {
		m.emit(ev)
	}
	return err
}

func (m *Manager) clientFor(res Resolved) (*Client, error) {
	key := res.Spec.Name + "\x00" + res.Root
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, ErrShutdown
	}
	c, ok := m.clients[key]
	if !ok {
		c = newClient(res, m.starter, m.clock, m.timing, m.cfg.RequestTimeout, m.emit)
		m.clients[key] = c
	}
	m.mu.Unlock()
	if _, err := c.ensureRunning(); err != nil {
		return nil, err
	}
	return c, nil
}

func (m *Manager) runningClient(name, uri string) (*Client, error) {
	for _, c := range m.sortedClients() {
		if c.Name() != name {
			continue
		}
		if s := c.State(); s == StateIndexing || s == StateReady {
			return c, nil
		}
	}
	return nil, &NoServerError{Ext: filepath.Ext(uri)}
}

func (m *Manager) sortedClients() []*Client {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Client, 0, len(m.clients))
	for _, c := range m.clients {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name() != out[j].Name() {
			return out[i].Name() < out[j].Name()
		}
		return out[i].Root() < out[j].Root()
	})
	return out
}

// Status lists every server deck has tried to start, including ones that failed to resolve.
func (m *Manager) Status() []StatusEvent {
	var out []StatusEvent
	for _, c := range m.sortedClients() {
		out = append(out, c.Status())
	}
	m.mu.Lock()
	for _, ev := range m.failed {
		out = append(out, ev)
	}
	m.mu.Unlock()
	sort.SliceStable(out, func(i, j int) bool { return out[i].Server < out[j].Server })
	return out
}

func (m *Manager) Logs() []ServerLog {
	var out []ServerLog
	for _, c := range m.sortedClients() {
		out = append(out, ServerLog{Server: c.Name(), Root: c.Root(), Stderr: c.Stderr()})
	}
	return out
}

// Shutdown sends shutdown/exit to every server, then kills each process group; ctx bounds the wait.
func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	for _, cancel := range m.installs {
		cancel()
	}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, c := range m.sortedClients() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.shutdown(ctx)
		}()
	}
	wg.Wait()
	close(m.done)
	return ctx.Err()
}
