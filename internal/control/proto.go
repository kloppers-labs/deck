package control

import (
	"time"

	"github.com/kloppers-labs/deck/internal/presence"
	"github.com/kloppers-labs/deck/internal/ui"
)

// Version is the control protocol version every request carries.
const Version = 1

// Request asks a deck to do something; lines and columns are 1-based, 0 = unset.
type Request struct {
	V           int    `json:"v"`
	Cmd         string `json:"cmd"`
	Path        string `json:"path,omitempty"`
	Line        int    `json:"line,omitempty"`
	Col         int    `json:"col,omitempty"`
	EndLine     int    `json:"endLine,omitempty"`
	Base        string `json:"base,omitempty"`
	Uncommitted bool   `json:"uncommitted,omitempty"`
	From        string `json:"from,omitempty"`
}

type Reply struct {
	OK      bool               `json:"ok"`
	Error   string             `json:"error,omitempty"`
	Message string             `json:"message,omitempty"`
	Where   *presence.Snapshot `json:"where,omitempty"`
}

// RequestMsg carries a navigation request into the UI; the app answers on
// Reply and drops it once Deadline has passed.
type RequestMsg struct {
	Req      Request
	Reply    chan<- Reply
	Deadline time.Time
}

func (RequestMsg) RouteTo() ui.Dest { return ui.DestApp }

// Answer sends r without blocking; Reply is buffered by the server.
func (m RequestMsg) Answer(r Reply) {
	if m.Reply == nil {
		return
	}
	select {
	case m.Reply <- r:
	default:
	}
}

// HeldMessage is the reply when deck holds a jump because the user is typing.
const HeldMessage = "held: user is typing"
