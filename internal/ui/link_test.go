package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// clickAt presses and lets go at x, y of the pane's view; it returns what the
// browser was asked to open.
func clickAt(t *testing.T, m Model, x, y int) string {
	t.Helper()
	var got string
	was := browse
	browse = func(url string) bool { got = url; return true }
	defer func() { browse = was }()
	r := m.vs.rects[0]
	mm := mouseAt(m, r.x+1+x, r.y+1+y, tea.MouseLeft, mousePress)
	mouseAt(mm, r.x+1+x, r.y+1+y, tea.MouseLeft, mouseRelease)
	return got
}

func TestClickOpensALinkInAPane(t *testing.T) {
	m := quietPane(t)
	p := m.space().Tab().Pane()
	p.Note("\x1b[2J\x1b[Hsee https://example.com/a_(b). ok\r\n\x1b]8;;https://osc.example/x\x1b\\click me\x1b]8;;\x1b\\ plain\r\n")
	m.View()
	if got := clickAt(t, m, 6, 0); got != "https://example.com/a_(b)" {
		t.Errorf("URL in the text: %q", got)
	}
	if got := clickAt(t, m, 2, 1); got != "https://osc.example/x" {
		t.Errorf("OSC 8 link: %q", got)
	}
	for _, at := range [][2]int{{1, 0}, {30, 0}, {10, 1}, {0, 5}} {
		if got := clickAt(t, m, at[0], at[1]); got != "" {
			t.Errorf("click at %v opened %q", at, got)
		}
	}
}

func TestClickOpensAWrappedLink(t *testing.T) {
	m := quietPane(t)
	p := m.space().Tab().Pane()
	w, _ := p.Size()
	url := "https://example.com/" + strings.Repeat("x", w)
	p.Note("\x1b[2J\x1b[H" + url + " end\r\n")
	m.View()
	for _, y := range []int{0, 1} {
		if got := clickAt(t, m, 3, y); got != url {
			t.Errorf("row %d: %q", y, got)
		}
	}
}

func TestClickOpensALinkInTheScrollback(t *testing.T) {
	m := quietPane(t)
	p := m.space().Tab().Pane()
	_, h := p.Size()
	p.Note("\x1b[2J\x1b[Hhttps://old.example/\r\n" + strings.Repeat("filler\r\n", h+5))
	p.Scroll(h + 5)
	m.View()
	if got := clickAt(t, m, 2, 0); got != "https://old.example/" {
		t.Errorf("scrolled: %q", got)
	}
}

func TestDragOverALinkDoesNotOpenIt(t *testing.T) {
	m := quietPane(t)
	m.space().Tab().Pane().Note("\x1b[2J\x1b[Hhttps://example.com/\r\n")
	m.View()
	var got string
	was := browse
	browse = func(url string) bool { got = url; return true }
	defer func() { browse = was }()
	r := m.vs.rects[0]
	mm := mouseAt(m, r.x+2, r.y+1, tea.MouseLeft, mousePress)
	mm = mouseAt(mm, r.x+8, r.y+1, tea.MouseLeft, mouseMotion)
	mouseAt(mm, r.x+8, r.y+1, tea.MouseLeft, mouseRelease)
	if got != "" {
		t.Errorf("a drag opened %q", got)
	}
}
