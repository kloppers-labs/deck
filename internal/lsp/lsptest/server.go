// Package lsptest is a scripted in-process language server over net.Pipe.
package lsptest

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Message struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
}

func (m Message) IsRequest() bool { return m.Method != "" && len(m.ID) > 0 }

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return fmt.Sprintf("jsonrpc error %d: %s", e.Code, e.Message) }

type Handler func(params json.RawMessage) (any, error)

type Server struct {
	server net.Conn
	client net.Conn
	reader *bufio.Reader

	wmu sync.Mutex

	mu       sync.Mutex
	handlers map[string]Handler
	received []Message
	changed  chan struct{}
	pending  map[string]chan Message
	nextID   int
	killed   bool

	IgnoreExit bool
	exited     chan struct{}
	exitOnce   sync.Once
}

func New() *Server {
	server, client := net.Pipe()
	s := &Server{
		server:   server,
		client:   client,
		reader:   bufio.NewReader(server),
		handlers: map[string]Handler{},
		changed:  make(chan struct{}),
		pending:  map[string]chan Message{},
		exited:   make(chan struct{}),
	}
	s.Handle("initialize", func(json.RawMessage) (any, error) {
		return map[string]any{"capabilities": map[string]any{}}, nil
	})
	s.Handle("shutdown", func(json.RawMessage) (any, error) { return nil, nil })
	go s.loop()
	return s
}

func (s *Server) Handle(method string, h Handler) {
	s.mu.Lock()
	s.handlers[method] = h
	s.mu.Unlock()
}

func (s *Server) Process() *Process { return &Process{s: s} }

func (s *Server) Crash() { s.exit() }

func (s *Server) Killed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.killed
}

func (s *Server) Received() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Message(nil), s.received...)
}

func (s *Server) Methods() []string {
	var out []string
	for _, m := range s.Received() {
		out = append(out, m.Method)
	}
	return out
}

func (s *Server) WaitFor(method string, n int, timeout time.Duration) ([]Message, error) {
	deadline := time.After(timeout)
	for {
		s.mu.Lock()
		var got []Message
		for _, m := range s.received {
			if m.Method == method {
				got = append(got, m)
			}
		}
		changed := s.changed
		s.mu.Unlock()
		if len(got) >= n {
			return got, nil
		}
		select {
		case <-changed:
		case <-deadline:
			return got, fmt.Errorf("lsptest: got %d %q messages, want %d", len(got), method, n)
		}
	}
}

func (s *Server) Notify(method string, params any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return s.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": json.RawMessage(raw)})
}

func (s *Server) Call(ctx context.Context, method string, params any) (Message, error) {
	s.mu.Lock()
	s.nextID++
	id := strconv.Itoa(s.nextID)
	ch := make(chan Message, 1)
	s.pending[id] = ch
	s.mu.Unlock()
	raw, err := json.Marshal(params)
	if err != nil {
		return Message{}, err
	}
	if err := s.write(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "method": method, "params": json.RawMessage(raw)}); err != nil {
		return Message{}, err
	}
	select {
	case m := <-ch:
		return m, nil
	case <-ctx.Done():
		return Message{}, ctx.Err()
	case <-s.exited:
		return Message{}, io.EOF
	}
}

func (s *Server) WriteRaw(b []byte) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	_, err := s.server.Write(b)
	return err
}

func (s *Server) write(v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.WriteRaw(append([]byte("Content-Length: "+strconv.Itoa(len(body))+"\r\n\r\n"), body...))
}

func (s *Server) exit() {
	s.exitOnce.Do(func() {
		s.server.Close()
		s.client.Close()
		close(s.exited)
	})
}

func (s *Server) loop() {
	defer s.exit()
	for {
		body, err := readFrame(s.reader)
		if err != nil {
			return
		}
		var m Message
		if err := json.Unmarshal(body, &m); err != nil {
			continue
		}
		if m.Method == "" {
			s.mu.Lock()
			ch := s.pending[string(m.ID)]
			delete(s.pending, string(m.ID))
			s.mu.Unlock()
			if ch != nil {
				ch <- m
			}
			continue
		}
		s.record(m)
		if m.Method == "exit" && !s.IgnoreExit {
			return
		}
		if m.IsRequest() {
			go s.respond(m)
		}
	}
}

func (s *Server) record(m Message) {
	s.mu.Lock()
	s.received = append(s.received, m)
	close(s.changed)
	s.changed = make(chan struct{})
	s.mu.Unlock()
}

func (s *Server) respond(m Message) {
	s.mu.Lock()
	h := s.handlers[m.Method]
	s.mu.Unlock()
	resp := map[string]any{"jsonrpc": "2.0", "id": m.ID}
	if h == nil {
		resp["result"] = nil
		_ = s.write(resp)
		return
	}
	result, err := h(m.Params)
	var rpcErr *Error
	switch {
	case errors.As(err, &rpcErr):
		resp["error"] = rpcErr
	case err != nil:
		resp["error"] = &Error{Code: -32603, Message: err.Error()}
	default:
		resp["result"] = result
	}
	_ = s.write(resp)
}

func readFrame(r *bufio.Reader) ([]byte, error) {
	tp := textproto.NewReader(r)
	length := -1
	for {
		line, err := tp.ReadLine()
		if err != nil {
			return nil, err
		}
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			length, err = strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return nil, err
			}
		}
	}
	if length < 0 {
		return nil, errors.New("lsptest: missing Content-Length")
	}
	body := make([]byte, length)
	_, err := io.ReadFull(r, body)
	return body, err
}

// Process is the client end of the pipe, shaped like lsp.Process.
type Process struct{ s *Server }

func (p *Process) Read(b []byte) (int, error)  { return p.s.client.Read(b) }
func (p *Process) Write(b []byte) (int, error) { return p.s.client.Write(b) }
func (p *Process) Close() error                { return p.s.client.Close() }
func (p *Process) Exited() <-chan struct{}     { return p.s.exited }

func (p *Process) Kill() error {
	p.s.mu.Lock()
	p.s.killed = true
	p.s.mu.Unlock()
	p.s.exit()
	return nil
}
