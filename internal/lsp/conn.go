package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
)

const (
	CodeMethodNotFound   = -32601
	CodeInternalError    = -32603
	CodeRequestCancelled = -32800
	CodeContentModified  = -32801
)

var (
	ErrServerExited   = errors.New("lsp: server exited")
	ErrMethodNotFound = errors.New("lsp: method not found")
)

type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("lsp: %s (%d)", e.Message, e.Code) }

// IsQuiet reports errors that mean "the answer is no longer wanted" and should not be shown.
func IsQuiet(err error) bool {
	var rpc *RPCError
	if errors.As(err, &rpc) {
		return rpc.Code == CodeRequestCancelled || rpc.Code == CodeContentModified
	}
	return errors.Is(err, context.Canceled)
}

type RequestHandler func(ctx context.Context, method string, params json.RawMessage) (any, error)

type NotifyHandler func(method string, params json.RawMessage)

type ConnOptions struct {
	OnRequest RequestHandler
	OnNotify  NotifyHandler
	Logf      func(format string, args ...any)
}

type wireMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type response struct {
	result json.RawMessage
	err    error
}

// Conn is a JSON-RPC 2.0 connection with LSP Content-Length framing.
type Conn struct {
	rwc  io.ReadWriteCloser
	opts ConnOptions

	wmu sync.Mutex

	mu        sync.Mutex
	nextID    int64
	pending   map[int64]chan response
	cancelled map[int64]struct{}
	err       error

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

func NewConn(rwc io.ReadWriteCloser, opts ConnOptions) *Conn {
	ctx, cancel := context.WithCancel(context.Background())
	c := &Conn{
		rwc:       rwc,
		opts:      opts,
		pending:   map[int64]chan response{},
		cancelled: map[int64]struct{}{},
		ctx:       ctx,
		cancel:    cancel,
		done:      make(chan struct{}),
	}
	go c.readLoop()
	return c
}

func (c *Conn) Done() <-chan struct{} { return c.done }

func (c *Conn) Close() error {
	err := c.rwc.Close()
	<-c.done
	return err
}

func (c *Conn) Call(ctx context.Context, method string, params, result any) error {
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		return c.err
	}
	c.nextID++
	id := c.nextID
	ch := make(chan response, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	rawID := json.RawMessage(strconv.FormatInt(id, 10))
	if err := c.send(wireMessage{ID: rawID, Method: method, Params: mustRaw(params)}); err != nil {
		c.forget(id)
		return err
	}
	select {
	case resp := <-ch:
		if resp.err != nil {
			return resp.err
		}
		if result == nil || isNull(resp.result) {
			return nil
		}
		return json.Unmarshal(resp.result, result)
	case <-ctx.Done():
		c.mu.Lock()
		_, live := c.pending[id]
		delete(c.pending, id)
		if live {
			c.cancelled[id] = struct{}{}
		}
		c.mu.Unlock()
		if live {
			go c.Notify("$/cancelRequest", map[string]json.RawMessage{"id": rawID})
		}
		return ctx.Err()
	}
}

func (c *Conn) Notify(method string, params any) error {
	return c.send(wireMessage{Method: method, Params: mustRaw(params)})
}

func (c *Conn) forget(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func (c *Conn) send(m wireMessage) error {
	m.JSONRPC = "2.0"
	body, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return c.write(body)
}

func (c *Conn) write(body []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	select {
	case <-c.done:
		return ErrServerExited
	default:
	}
	if _, err := c.rwc.Write(append([]byte("Content-Length: "+strconv.Itoa(len(body))+"\r\n\r\n"), body...)); err != nil {
		return fmt.Errorf("%w: %v", ErrServerExited, err)
	}
	return nil
}

func (c *Conn) readLoop() {
	r := bufio.NewReaderSize(c.rwc, 64*1024)
	var err error
	for {
		var body []byte
		body, err = ReadMessage(r)
		if err != nil {
			break
		}
		var m wireMessage
		if jerr := json.Unmarshal(body, &m); jerr != nil {
			c.logf("lsp: malformed message: %v", jerr)
			continue
		}
		c.dispatch(m)
	}
	c.cancel()
	c.mu.Lock()
	c.err = ErrServerExited
	if err != nil && !errors.Is(err, io.EOF) {
		c.err = fmt.Errorf("%w: %v", ErrServerExited, err)
	}
	pending := c.pending
	c.pending = map[int64]chan response{}
	c.mu.Unlock()
	for _, ch := range pending {
		ch <- response{err: ErrServerExited}
	}
	close(c.done)
}

func (c *Conn) dispatch(m wireMessage) {
	switch {
	case m.Method != "" && len(m.ID) > 0:
		go c.answer(m)
	case m.Method != "":
		if c.opts.OnNotify != nil {
			c.opts.OnNotify(m.Method, m.Params)
		}
	default:
		c.resolve(m)
	}
}

func (c *Conn) resolve(m wireMessage) {
	id, err := strconv.ParseInt(strings.Trim(string(m.ID), `"`), 10, 64)
	if err != nil {
		c.logf("lsp: response with unexpected id %s", m.ID)
		return
	}
	c.mu.Lock()
	ch, ok := c.pending[id]
	delete(c.pending, id)
	_, wasCancelled := c.cancelled[id]
	delete(c.cancelled, id)
	c.mu.Unlock()
	if !ok {
		if !wasCancelled {
			c.logf("lsp: response for unknown id %d", id)
		}
		return
	}
	if m.Error != nil {
		ch <- response{err: m.Error}
		return
	}
	ch <- response{result: m.Result}
}

func (c *Conn) answer(m wireMessage) {
	var (
		result any
		err    = ErrMethodNotFound
	)
	if c.opts.OnRequest != nil {
		result, err = c.opts.OnRequest(c.ctx, m.Method, m.Params)
	}
	resp := struct {
		JSONRPC string           `json:"jsonrpc"`
		ID      json.RawMessage  `json:"id"`
		Result  *json.RawMessage `json:"result,omitempty"`
		Error   *RPCError        `json:"error,omitempty"`
	}{JSONRPC: "2.0", ID: m.ID}
	var rpc *RPCError
	switch {
	case errors.Is(err, ErrMethodNotFound):
		resp.Error = &RPCError{Code: CodeMethodNotFound, Message: "method not found: " + m.Method}
	case errors.As(err, &rpc):
		resp.Error = rpc
	case err != nil:
		resp.Error = &RPCError{Code: CodeInternalError, Message: err.Error()}
	default:
		raw := mustRaw(result)
		if raw == nil {
			raw = json.RawMessage("null")
		}
		resp.Result = &raw
	}
	body, jerr := json.Marshal(resp)
	if jerr != nil {
		c.logf("lsp: encode response: %v", jerr)
		return
	}
	_ = c.write(body)
}

func (c *Conn) logf(format string, args ...any) {
	if c.opts.Logf != nil {
		c.opts.Logf(format, args...)
	}
}

// ReadMessage reads one Content-Length framed message body.
func ReadMessage(r *bufio.Reader) ([]byte, error) {
	tp := textproto.NewReader(r)
	length := -1
	for {
		line, err := tp.ReadLine()
		if err != nil {
			return nil, err
		}
		if line == "" {
			if length < 0 {
				continue
			}
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok || !strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			continue
		}
		length, err = strconv.Atoi(strings.TrimSpace(value))
		if err != nil || length < 0 {
			return nil, fmt.Errorf("lsp: bad Content-Length %q", value)
		}
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return body, nil
}

func mustRaw(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	if raw, ok := v.(json.RawMessage); ok {
		return raw
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}

func isNull(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return s == "" || s == "null"
}
