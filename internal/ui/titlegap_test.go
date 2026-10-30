package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// screenLines is the screen as plain text, one string per row.
func screenLines(m Model) []string { return strings.Split(ansi.Strip(viewOf(m)), "\n") }

// blank is true when the screen columns [x0, x1) of l hold nothing but borders and spaces.
func blank(l string, x0, x1 int) bool {
	return strings.Trim(ansi.Cut(l, x0, x1), " │") == ""
}

// A sidebar heading keeps one blank row over its first line, and clicking
// that first line still selects it.
func TestSidebarTitleGapKeepsClicks(t *testing.T) {
	m := withAgents(t, 150, 42, demoIdle, demoBlocked)
	m = settle(t, m, func(m Model) bool { return localAgents(m) == 2 })
	m.cur = -1
	m.tab = tabSessions
	ls := screenLines(m)
	head := sideLine(m, "AGENTS")
	if head < 0 {
		t.Fatal("no AGENTS heading")
	}
	if !blank(ls[head+1], 0, sidebarW) {
		t.Errorf("row under AGENTS: %q, want a blank", ls[head+1])
	}
	var first sideRow
	for _, h := range m.vs.sideRows {
		if h.local != nil && h.y > head {
			first = h
			break
		}
	}
	if first.y != head+2 {
		t.Fatalf("first agent at row %d, want %d", first.y, head+2)
	}
	mm := click(m, 5, head+2).(Model)
	if mm.cur < 0 || mm.spaces[mm.cur] != first.local.space {
		t.Errorf("click on the first agent: cur=%d", mm.cur)
	}
	sp := sideLine(m, "SPACES")
	if !blank(ls[sp+1], 0, sidebarW) || !strings.Contains(ls[sp+2], "all spaces") {
		t.Errorf("SPACES gap wrong: %q / %q", ls[sp+1], ls[sp+2])
	}
}

// A list under a panel title is one blank row below it; clicking its first row selects it.
func TestListTitleGapKeepsClicks(t *testing.T) {
	m := key(boot(t, 150, 42), "1")
	m = key(m, "j")
	ls := screenLines(m.(Model))
	top := -1
	for y, l := range ls {
		if strings.Contains(l, "Live agents") {
			top = y
			break
		}
	}
	if top < 0 {
		t.Fatal("no Live agents panel")
	}
	x := m.(Model).sideW() + 4
	if !strings.Contains(ls[top+2], "Partial refunds") || !blank(ls[top+1], m.(Model).sideW()+1, m.(Model).sideW()+60) {
		t.Fatalf("want a blank row then the first agent:\n%s\n%s", ls[top+1], ls[top+2])
	}
	if m.(Model).live.cur.idx == 0 {
		t.Fatal("j should have moved off the first row")
	}
	if got := click(m, x, top+2).(Model).live.cur.idx; got != 0 {
		t.Errorf("clicking the first row selected %d", got)
	}
}

// With no height to spare the gap goes, and the rows still line up.
func TestTitleGapDropsWhenShort(t *testing.T) {
	if gapOf(19) != 0 || gapOf(25) != 1 || bodyRows(19) != 17 || bodyRows(25) != 22 {
		t.Errorf("gap thresholds off: %d %d %d %d", gapOf(19), gapOf(25), bodyRows(19), bodyRows(25))
	}
}
