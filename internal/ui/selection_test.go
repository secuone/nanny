package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/secuone/nanny/internal/claude"
)

// Dragging selects (shown in reverse video) and letting go copies the text
// under it; a key clears it.
func TestMouseSelectionCopies(t *testing.T) {
	var m tea.Model = boot(t, 150, 42)
	viewOf(m)
	m = mouseAt(m, 40, 6, tea.MouseButtonLeft, mousePress)
	m = mouseAt(m, 60, 6, tea.MouseButtonLeft, mouseMotion)
	out := viewOf(m)
	if !strings.Contains(out, "\x1b[0;7m") {
		t.Fatal("the selection should show")
	}
	want := strings.TrimRight(ansi.Strip(ansi.Cut(strings.Split(out, "\n")[6], 40, 61)), " ")
	if got := m.(Model).selText(m.(Model).vs.frame); got != want || got == "" {
		t.Errorf("selected %q, want %q", got, want)
	}
	m, cmd := m.Update(mouseMsg(60, 6, tea.MouseButtonLeft, mouseRelease))
	if cmd == nil {
		t.Error("letting go should copy")
	}
	if m = key(m, "j"); m.(Model).vs.sel.on {
		t.Error("a key clears the selection")
	}
	// over several rows: the first from the start column, the last up to the end one
	s := selection{on: true, ax: 5, ay: 2, bx: 3, by: 4, box: rect{0, 0, 10, 10}}
	for y, want := range map[int][2]int{2: {5, 10}, 3: {0, 10}, 4: {0, 4}} {
		if c0, c1, ok := s.selCols(y); !ok || c0 != want[0] || c1 != want[1] {
			t.Errorf("row %d: %d-%d", y, c0, c1)
		}
	}
	if _, _, ok := s.selCols(5); ok {
		t.Error("row 5 is outside")
	}
}

// A frame where only the spinners move reuses the last one, with the spinner
// swapped in: nothing is drawn again.
func TestSpinnerFramesReuse(t *testing.T) {
	m := withAgents(t, 150, 42)
	var mm tea.Model = m
	first := viewOf(mm)
	if strings.Contains(first, spinGlyph) || strings.Contains(first, blinkGlyph) {
		t.Fatal("placeholders must never reach the screen")
	}
	mm, _ = mm.Update(frameMsg{})
	raw := mm.(Model).vs.raw
	next := viewOf(mm)
	if mm.(Model).vs.raw != raw {
		t.Error("an animation frame should not draw the view again")
	}
	if strings.Contains(raw, spinGlyph) && next == first {
		t.Error("the spinner should have moved")
	}
}

// A background refresh doesn't draw at once: the ones that come back together
// after a tick are drawn once, at the flush. A key still draws right away.
func TestBackgroundRefreshesCoalesce(t *testing.T) {
	coalesce = true
	defer func() { coalesce = false }()
	m := withAgents(t, 150, 42)
	var mm tea.Model = m
	before := viewOf(mm)
	ss := append([]*claude.Session(nil), m.sessions...)
	renamed := *ss[0]
	renamed.Title = "Renamed while you look"
	ss[0] = &renamed
	mm, cmd := mm.Update(sessionsMsg(ss))
	if cmd == nil || viewOf(mm) != before {
		t.Fatal("a background refresh should wait for the flush")
	}
	mm, _ = mm.Update(subsMsg{path: "/nowhere"})
	if viewOf(mm) != before {
		t.Fatal("the next one waits too")
	}
	mm, _ = mm.Update(flushMsg{})
	if !strings.Contains(ansi.Strip(viewOf(mm)), "Renamed while you look") {
		t.Fatal("the flush draws them")
	}
	mm, _ = mm.Update(sessionsMsg(m.sessions))
	mm, _ = mm.Update(keyPress("j"))
	if strings.Contains(ansi.Strip(viewOf(mm)), "Renamed while you look") {
		t.Error("a key draws at once, with what the refresh brought")
	}
}
