package control

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	maxRequest  = 1 << 20
	connTimeout = 10 * time.Second
)

var navTimeout = 2 * time.Second

// Listen opens sock with mode 0600, replacing a stale socket file.
func Listen(sock string) (net.Listener, error) {
	if err := ensureDir(dirOf(sock)); err != nil {
		return nil, err
	}
	if c, err := net.DialTimeout("unix", sock, probeTimeout); err == nil {
		c.Close()
		return nil, fmt.Errorf("%s is already in use", sock)
	}
	os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(sock, 0o600); err != nil {
		ln.Close()
		return nil, err
	}
	return ln, nil
}

func Serve(ctx context.Context, sock string, query func(Request) Reply, send func(tea.Msg)) error {
	ln, err := Listen(sock)
	if err != nil {
		return err
	}
	ServeListener(ctx, ln, query, send)
	return nil
}

// ServeListener answers ping and where through query and hands every other
// command to the UI through send, until ctx is done; it removes the socket.
func ServeListener(ctx context.Context, ln net.Listener, query func(Request) Reply, send func(tea.Msg)) {
	go func() {
		<-ctx.Done()
		ln.Close()
	}()
	defer os.Remove(ln.Addr().String())
	for {
		c, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
				return
			}
			continue
		}
		go handle(c, query, send)
	}
}

func handle(c net.Conn, query func(Request) Reply, send func(tea.Msg)) {
	defer c.Close()
	c.SetDeadline(time.Now().Add(connTimeout))
	r := bufio.NewReaderSize(c, 64<<10)
	line, err := readLine(r)
	var reply Reply
	switch {
	case err != nil:
		reply = Reply{Error: "bad request: " + err.Error()}
	default:
		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			reply = Reply{Error: "bad request: " + err.Error()}
		} else {
			reply = dispatch(req, query, send)
		}
	}
	data, _ := json.Marshal(reply)
	c.Write(append(data, '\n'))
}

func readLine(r *bufio.Reader) ([]byte, error) {
	var buf []byte
	for {
		chunk, isPrefix, err := r.ReadLine()
		if err != nil {
			return nil, err
		}
		buf = append(buf, chunk...)
		if len(buf) > maxRequest {
			return nil, errors.New("request too large")
		}
		if !isPrefix {
			return buf, nil
		}
	}
}

func dispatch(req Request, query func(Request) Reply, send func(tea.Msg)) Reply {
	if req.V != Version {
		return Reply{Error: fmt.Sprintf("unsupported protocol version %d", req.V)}
	}
	switch req.Cmd {
	case "ping", "where":
		return query(req)
	case "open", "review", "diff":
		return navigate(req, send)
	}
	return Reply{Error: fmt.Sprintf("unknown command %q", req.Cmd)}
}

func navigate(req Request, send func(tea.Msg)) Reply {
	reply := make(chan Reply, 1)
	go send(RequestMsg{Req: req, Reply: reply, Deadline: time.Now().Add(navTimeout)})
	t := time.NewTimer(navTimeout)
	defer t.Stop()
	select {
	case r := <-reply:
		return r
	case <-t.C:
		return Reply{Error: "deck is busy: editor open"}
	}
}
