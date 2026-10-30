package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func quietPane(t *testing.T) Model {
	m := manyPanes(t, 1)
	m.openTabIn(m.spaces[0], "quiet", []string{"/bin/sleep", "30"})
	m.layoutPanes()
	return m
}

func TestCursorFollowsThePane(t *testing.T) {
	m := quietPane(t)
	m.space().Tab().Pane().Note("\x1b[2J\x1b[Habc\x1b[2;4H\x1b[4 q")
	c := m.View().Cursor
	if c == nil {
		t.Fatal("no cursor")
	}
	r := m.vs.rects[0]
	if c.X != r.x+1+3 || c.Y != r.y+1+1 {
		t.Errorf("cursor at %d,%d, pane at %d,%d", c.X, c.Y, r.x, r.y)
	}
	if c.Shape != tea.CursorUnderline {
		t.Errorf("shape %v", c.Shape)
	}
}

func TestCursorHidesWithThePane(t *testing.T) {
	m := quietPane(t)
	m.space().Tab().Pane().Note("\x1b[?25l")
	if m.View().Cursor != nil {
		t.Error("a hidden cursor was shown")
	}
}

func TestNoCursorUnderThePrefix(t *testing.T) {
	m := quietPane(t)
	if m.View().Cursor == nil {
		t.Fatal("no cursor")
	}
	mm, _ := m.Update(keyPress("ctrl+b"))
	if mm.View().Cursor != nil {
		t.Error("the cursor stayed while the prefix waits")
	}
}

func TestNoCursorUnderAPopup(t *testing.T) {
	m := quietPane(t)
	m.openPalette("")
	if m.View().Cursor != nil {
		t.Error("the cursor shows through a popup")
	}
}

func TestPaneCacheIgnoresCursorMoves(t *testing.T) {
	m := quietPane(t)
	w, h := m.vs.area.w, m.vs.area.h
	p := m.space().Tab().Pane()
	p.Note("abc")
	first := m.viewPanes(w, h)
	if m.viewPanes(w, h) != first {
		t.Error("an idle pane was drawn differently")
	}
}

func TestCursorIsInTheFocusedPane(t *testing.T) {
	m := manyPanes(t, 3)
	tab := m.space().Tab()
	for i := range tab.Panes {
		tab.Focus = i
		m.vs.reuse, m.vs.noop = false, false
		c := m.View().Cursor
		if c == nil {
			continue
		}
		if r := m.vs.rects[i]; c.X <= r.x || c.X >= r.x+r.w-1 || c.Y <= r.y || c.Y >= r.y+r.h-1 {
			t.Errorf("pane %d: cursor %d,%d is outside %+v", i, c.X, c.Y, r)
		}
	}
}
