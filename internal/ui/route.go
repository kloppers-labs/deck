package ui

// Dest names the part of deck that owns a message.
type Dest int

const (
	DestApp Dest = iota
	DestFiles
	DestGit
	DestRun
	DestHTTP
)

// Routed is implemented by async and cross-tab messages. The root model delivers
// them to their destination whichever tab is active.
type Routed interface{ RouteTo() Dest }

// Broadcast is a Routed message that other destinations receive as well.
type Broadcast interface {
	Routed
	AlsoTo() []Dest
}
