package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Mouse selection, tmux style: nanny owns the mouse, so the terminal can't
// select. Drag to select (inside a pane it stays in that pane), let go to copy.

type selection struct {
	on             bool
	ax, ay, bx, by int  // where the drag started and where it is now
	box            rect // what it can't leave: a pane, or the whole screen
}

type pressAt struct {
	ok   bool
	x, y int
}

// selectMouse handles the drag of a selection; ok is false when the event is
// something else.
func (m Model) selectMouse(e mouseEv) (tea.Model, tea.Cmd, bool) {
	switch {
	case e.Action == mousePress && e.Button == tea.MouseLeft:
		m.vs.sel = selection{}
		m.vs.press = pressAt{true, e.X, e.Y}
		return m, nil, false // the press still clicks
	case e.Action == mouseMotion && e.Button == tea.MouseLeft:
		p := m.vs.press
		if !p.ok || m.vs.drag.kind != "" || m.menu != nil || !m.vs.sel.on && e.X == p.x && e.Y == p.y {
			return m, nil, false
		}
		if !m.vs.sel.on {
			m.vs.sel = selection{on: true, ax: p.x, ay: p.y, box: m.selBox(p.x, p.y)}
		}
		b := m.vs.sel.box
		m.vs.sel.bx = max(b.x, min(e.X, b.x+b.w-1))
		m.vs.sel.by = max(b.y, min(e.Y, b.y+b.h-1))
		return m, nil, true
	case e.Action == mouseRelease:
		m.vs.press = pressAt{}
		if !m.vs.sel.on {
			return m, nil, false
		}
		if text := m.selText(m.vs.frame); text != "" {
			return m, m.copy(text, T("selection")), true
		}
		return m, nil, true
	}
	return m, nil, false
}

// selBox is the pane under (x, y) when its terminals are on screen, else the screen.
func (m Model) selBox(x, y int) rect {
	if m.inSpace() && !m.dash {
		for _, r := range m.vs.rects {
			if x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h {
				return r
			}
		}
	}
	return rect{0, 0, m.w, m.h}
}

// selCols are the columns selected on row y, [c0, c1).
func (s selection) selCols(y int) (int, int, bool) {
	ay, ax, by, bx := s.ay, s.ax, s.by, s.bx
	if by < ay || by == ay && bx < ax {
		ay, ax, by, bx = by, bx, ay, ax
	}
	if !s.on || y < ay || y > by {
		return 0, 0, false
	}
	c0, c1 := s.box.x, s.box.x+s.box.w
	if y == ay {
		c0 = ax
	}
	if y == by {
		c1 = bx + 1
	}
	return c0, c1, c1 > c0
}

// paintSel shows the selection in reverse video.
func (s selection) paint(frame string) string {
	if !s.on {
		return frame
	}
	ls := strings.Split(frame, "\n")
	for y, l := range ls {
		if c0, c1, ok := s.selCols(y); ok {
			mid := ansi.Strip(ansi.Cut(l, c0, c1))
			mid += strings.Repeat(" ", max(0, c1-c0-ansi.StringWidth(mid)))
			ls[y] = ansi.Truncate(l, c0, "") + "\x1b[0;7m" + mid + "\x1b[0m" + ansi.TruncateLeft(l, c1, "")
		}
	}
	return strings.Join(ls, "\n")
}

// selText is the selected text, without styles or trailing blanks.
func (m Model) selText(frame string) string {
	var out []string
	for y, l := range strings.Split(frame, "\n") {
		if c0, c1, ok := m.vs.sel.selCols(y); ok {
			out = append(out, strings.TrimRight(ansi.Strip(ansi.Cut(l, c0, c1)), " "))
		}
	}
	return strings.Trim(strings.Join(out, "\n"), "\n")
}
