package app

import (
	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) mouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.duplicateMotion(msg) {
		return m, nil
	}
	if m.help.open {
		switch msg.Button {
		case tea.MouseButtonWheelDown:
			m.scrollHelp(1)
		case tea.MouseButtonWheelUp:
			m.scrollHelp(-1)
		}
		return m, nil
	}
	if msg.Y < tabBarHeight {
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if t := tabHitTest(msg.X, m.tabWidths()); t >= 0 {
				return m, m.jump(tab(t))
			}
		}
		return m, nil
	}
	if msg.Y >= tabBarHeight+m.bodyHeight() {
		return m, nil
	}
	msg.Y -= tabBarHeight
	return m.routeTo(tabDests[m.active], msg)
}

func (m *Model) duplicateMotion(msg tea.MouseMsg) bool {
	if msg.Action != tea.MouseActionMotion {
		return false
	}
	c := cell{msg.X, msg.Y}
	if m.lastMotion != nil && *m.lastMotion == c {
		return true
	}
	m.lastMotion = &c
	return false
}

func tabHitTest(x int, widths []int) int {
	if x < 0 {
		return -1
	}
	for i, w := range widths {
		if x < w {
			return i
		}
		x -= w
	}
	return -1
}
