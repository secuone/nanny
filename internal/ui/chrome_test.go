package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// paintAgain draws what a pane's output does: no update in between.
func paintAgain(m Model) string {
	m.vs.reuse, m.vs.noop, m.vs.pending = false, false, true
	return ansi.Strip(m.render())
}

func TestChromeStatusExpires(t *testing.T) {
	m := manyPanesAt(t, 2, 150, 42)
	m.status, m.statusAt = "saved-it", time.Now()
	if !strings.Contains(paintAgain(m), "saved-it") {
		t.Fatal("the status is not shown")
	}
	m.statusAt = time.Now().Add(-9 * time.Second)
	if strings.Contains(paintAgain(m), "saved-it") {
		t.Error("the status stays after 8s")
	}
}

func TestChromeFollowsAPaneExit(t *testing.T) {
	m := manyPanesAt(t, 1, 150, 42)
	_ = paintAgain(m)
	before := m.vs.chrome.side
	p := m.space().Tab().Panes[0]
	p.Close()
	for i := 0; i < 100 && !p.Exited(); i++ {
		time.Sleep(50 * time.Millisecond)
	}
	if !p.Exited() {
		t.Fatal("the pane did not exit")
	}
	_ = paintAgain(m)
	if m.vs.chrome.side == before {
		t.Error("the sidebar keeps the glyph of a pane that exited")
	}
}

func TestChromeFollowsAPaneTitle(t *testing.T) {
	m := manyPanesAt(t, 2, 150, 42)
	_ = paintAgain(m)
	before := m.vs.chrome.key
	m.space().Tab().Panes[0].Note("\x1b]2;foo\a")
	_ = paintAgain(m)
	if m.vs.chrome.key == before || !strings.Contains(m.vs.chrome.key, "foo") {
		t.Error("a new pane title does not redraw the chrome")
	}
}

func TestChromeFollowsTheTheme(t *testing.T) {
	m := boot(t, 150, 42)
	_ = viewOf(m)
	before := m.(Model).vs.chrome.header
	m = key(key(m, "T"), "j")
	_ = viewOf(m)
	if m.(Model).vs.chrome.header == before {
		t.Error("the header keeps the colors of the old theme")
	}
}

func TestChromeIsKeptUntilSomethingMoves(t *testing.T) {
	m := manyPanesAt(t, 2, 150, 42)
	_ = paintAgain(m)
	m.vs.chrome.header, m.vs.chrome.footer = "kept-header", "kept-footer"
	if out := paintAgain(m); !strings.Contains(out, "kept-header") || !strings.Contains(out, "kept-footer") {
		t.Error("the chrome is drawn again with nothing changed")
	}
	m.vs.gen++
	if out := paintAgain(m); strings.Contains(out, "kept-header") || strings.Contains(out, "kept-footer") {
		t.Error("the chrome survives an update")
	}
}

func TestChromeFollowsTheClock(t *testing.T) {
	old := now
	t.Cleanup(func() { now = old })
	at := time.Date(2026, 10, 30, 14, 18, 20, 0, time.Local)
	now = func() time.Time { return at }
	m := manyPanesAt(t, 2, 150, 42)
	if !strings.Contains(paintAgain(m), "14:18") {
		t.Fatal("the clock is not shown")
	}
	at = at.Add(30 * time.Second)
	if !strings.Contains(paintAgain(m), "14:18") {
		t.Error("the clock moved inside the minute")
	}
	at = at.Add(time.Minute)
	if out := paintAgain(m); !strings.Contains(out, "14:19") {
		t.Error("the clock stays after the minute rolled over")
	}
}
