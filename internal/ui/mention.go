package ui

// MentionMsg asks the app to point the agent at lines of a file: 0-based,
// LineEnd inclusive.
type MentionMsg struct {
	Path               string
	LineStart, LineEnd int
}

func (MentionMsg) RouteTo() Dest { return DestApp }
