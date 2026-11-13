package ui

// ShowFileMsg asks the app to open Path in the Files tab at Line (1-based).
type ShowFileMsg struct {
	Path string
	Line int
}

func (ShowFileMsg) RouteTo() Dest { return DestApp }
