package ide

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/coder/websocket"

	"github.com/kloppers-labs/deck/internal/presence"
	"github.com/kloppers-labs/deck/internal/ui"
)

const (
	authHeader    = "X-Claude-Code-Ide-Authorization"
	minPort       = 10000
	maxPort       = 65535
	listenTries   = 50
	writeTimeout  = 5 * time.Second
	clientBacklog = 64
)

// Options configures the bridge. ConfigDir is Claude's config directory; Send
// delivers messages into the Bubble Tea program.
type Options struct {
	Root, ConfigDir string
	PID             int
	Store           *presence.Store
	Send            func(tea.Msg)
	Debounce        time.Duration
	Version         string
}

// ClientsMsg reports how many Claude sessions are connected.
type ClientsMsg struct{ N int }

func (ClientsMsg) RouteTo() ui.Dest { return ui.DestApp }

type Bridge struct {
	opts   Options
	token  string
	port   int
	lock   string
	srv    *http.Server
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu       sync.Mutex
	clients  map[*client]bool
	lastSent string
	diffs    map[string]chan struct{}
	closed   bool
	lines    lineCache
	poke     chan struct{}
}

type client struct {
	conn *websocket.Conn
	out  chan []byte
	mu   sync.Mutex
	done bool
}

// send drops data when a slow client's backlog is full rather than block deck.
func (c *client) send(data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.done {
		return
	}
	select {
	case c.out <- data:
	default:
	}
}

func (c *client) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.done {
		c.done = true
		close(c.out)
	}
}

// Start listens on a random loopback port and writes Claude's lockfile.
func Start(ctx context.Context, o Options) (*Bridge, error) {
	if o.Debounce <= 0 {
		o.Debounce = 100 * time.Millisecond
	}
	if o.Send == nil {
		o.Send = func(tea.Msg) {}
	}
	if fi, err := os.Stat(o.ConfigDir); err != nil || !fi.IsDir() {
		return nil, ErrNoClaude
	}
	dir := filepath.Join(o.ConfigDir, "ide")
	sweep(dir)
	token, err := newToken()
	if err != nil {
		return nil, err
	}
	ln, port, err := listen()
	if err != nil {
		return nil, err
	}
	b := &Bridge{opts: o, token: token, port: port, clients: map[*client]bool{},
		diffs: map[string]chan struct{}{}, poke: make(chan struct{}, 1)}
	b.ctx, b.cancel = context.WithCancel(ctx)
	b.lock, err = writeLock(dir, port, lockFile{
		PID: o.PID, WorkspaceFolders: []string{o.Root}, IDEName: ideName, Transport: "ws", AuthToken: token,
	})
	if err != nil {
		ln.Close()
		b.cancel()
		return nil, err
	}
	b.srv = &http.Server{Handler: http.HandlerFunc(b.serveHTTP), ReadHeaderTimeout: 5 * time.Second}
	b.wg.Add(2)
	go func() {
		defer b.wg.Done()
		b.srv.Serve(ln)
	}()
	go func() {
		defer b.wg.Done()
		b.notifyLoop()
	}()
	return b, nil
}

func listen() (net.Listener, int, error) {
	var last error
	for range listenTries {
		port := minPort + rand.IntN(maxPort-minPort+1)
		ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
		if err == nil {
			return ln, port, nil
		}
		last = err
	}
	return nil, 0, last
}

func (b *Bridge) Port() int { return b.port }

func (b *Bridge) Clients() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.clients)
}

// Close disconnects every client, stops the server and removes the lockfile.
func (b *Bridge) Close() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	b.mu.Unlock()
	os.Remove(b.lock)
	b.cancel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	b.srv.Shutdown(ctx)
	b.mu.Lock()
	for c := range b.clients {
		c.conn.CloseNow()
	}
	b.mu.Unlock()
	b.wg.Wait()
}

func (b *Bridge) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get(authHeader) != b.token {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Header.Get("Origin") != "" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"mcp"}})
	if err != nil {
		return
	}
	conn.SetReadLimit(4 << 20)
	c := &client{conn: conn, out: make(chan []byte, clientBacklog)}
	b.addClient(c)
	defer b.removeClient(c)
	go b.writeLoop(c)
	b.readLoop(c)
}

func (b *Bridge) addClient(c *client) {
	b.mu.Lock()
	b.clients[c] = true
	n := len(b.clients)
	b.lastSent = ""
	b.mu.Unlock()
	b.opts.Send(ClientsMsg{N: n})
	b.pokeNotify()
}

func (b *Bridge) removeClient(c *client) {
	b.mu.Lock()
	delete(b.clients, c)
	n := len(b.clients)
	closed := b.closed
	b.mu.Unlock()
	c.close()
	c.conn.CloseNow()
	if !closed {
		b.opts.Send(ClientsMsg{N: n})
	}
}

func (b *Bridge) writeLoop(c *client) {
	for data := range c.out {
		ctx, cancel := context.WithTimeout(b.ctx, writeTimeout)
		err := c.conn.Write(ctx, websocket.MessageText, data)
		cancel()
		if err != nil {
			c.conn.CloseNow()
			return
		}
	}
}

func (b *Bridge) readLoop(c *client) {
	for {
		_, data, err := c.conn.Read(b.ctx)
		if err != nil {
			return
		}
		go b.handle(c, data)
	}
}

func (b *Bridge) broadcast(data []byte) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	for c := range b.clients {
		c.send(data)
	}
	return len(b.clients)
}

var errClosed = errors.New("bridge closed")

// ErrNoClaude means Claude Code's config directory doesn't exist, so there is no Claude to connect.
var ErrNoClaude = errors.New("no Claude Code config directory")

func (b *Bridge) pokeNotify() {
	select {
	case b.poke <- struct{}{}:
	default:
	}
}

func (b *Bridge) pokeCh() <-chan struct{} { return b.poke }

func (b *Bridge) closeDiff(tab string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ch, ok := b.diffs[tab]; ok {
		close(ch)
		delete(b.diffs, tab)
	}
}

// awaitDiff holds an openDiff call until Claude closes its tab, leaving the
// decision to Claude's own prompt, then reports it as rejected.
func (b *Bridge) awaitDiff(raw []byte) string {
	var args struct {
		TabName string `json:"tab_name"`
	}
	json.Unmarshal(raw, &args)
	ch := make(chan struct{})
	b.mu.Lock()
	if old, ok := b.diffs[args.TabName]; ok {
		close(old)
	}
	b.diffs[args.TabName] = ch
	b.mu.Unlock()
	select {
	case <-ch:
	case <-b.ctx.Done():
	}
	return "DIFF_REJECTED"
}
