package lsp

import (
	"sync"
	"time"
)

type State int

const (
	StateIdle State = iota
	StateStarting
	StateIndexing
	StateReady
	StateExited
	StateFailed
	StateStopped
)

func (s State) String() string {
	switch s {
	case StateStarting:
		return "starting"
	case StateIndexing:
		return "indexing"
	case StateReady:
		return "ready"
	case StateExited:
		return "exited"
	case StateFailed:
		return "failed"
	case StateStopped:
		return "stopped"
	}
	return "idle"
}

// StatusEvent reports a server's state; Percent is -1 when the server gave none.
type StatusEvent struct {
	Server  string
	Root    string
	State   State
	Title   string
	Message string
	Percent int
	Err     error
	Hint    string
	Install []string
	Since   time.Time
}

type DiagnosticsEvent struct {
	Server      string
	URI         string
	Path        string
	Version     *int
	Diagnostics []Diagnostic
}

type LogEvent struct {
	Server  string
	Type    MessageType
	Message string
}

type ShowMessageEvent struct {
	Server  string
	Type    MessageType
	Message string
}

const eventBuffer = 256

// eventQueue never blocks producers: when full it drops the oldest LogEvent, else the oldest event.
type eventQueue struct {
	mu     sync.Mutex
	items  []any
	cap    int
	signal chan struct{}
}

func newEventQueue(capacity int) *eventQueue {
	return &eventQueue{cap: capacity, signal: make(chan struct{}, 1)}
}

func (q *eventQueue) push(ev any) {
	q.mu.Lock()
	if len(q.items) >= q.cap {
		if _, isLog := ev.(LogEvent); isLog {
			q.mu.Unlock()
			return
		}
		drop := 0
		for i, it := range q.items {
			if _, isLog := it.(LogEvent); isLog {
				drop = i
				break
			}
		}
		q.items = append(q.items[:drop], q.items[drop+1:]...)
	}
	q.items = append(q.items, ev)
	q.mu.Unlock()
	select {
	case q.signal <- struct{}{}:
	default:
	}
}

func (q *eventQueue) pop() (any, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) == 0 {
		return nil, false
	}
	ev := q.items[0]
	q.items[0] = nil
	q.items = q.items[1:]
	return ev, true
}

func (q *eventQueue) len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}
