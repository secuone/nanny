package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/secuone/nanny/internal/ws"
)

// sideLine is the screen row of the first sidebar line that has s, or -1.
func sideLine(mm tea.Model, s string) int {
	for y, l := range strings.Split(ansi.Strip(viewOf(mm)), "\n") {
		if y >= bodyY && strings.Index(l, s) >= 0 && strings.Index(l, s) < sidebarW && !strings.Contains(l, "No agents in") {
			return y
		}
	}
	return -1
}

func manySpaces(t *testing.T, m Model, n int) Model {
	t.Helper()
	for i := 0; i < n; i++ {
		m.spaces = append(m.spaces, ws.NewSpace(fmt.Sprintf("space-%02d", i), t.TempDir()))
	}
	return m
}

func localAgents(m Model) int {
	n := 0
	for _, r := range m.live.allRows {
		if r.local != nil && r.sub == nil {
			n++
		}
	}
	return n
}

func focusedPane(m Model) any {
	if m.inSpace() && !m.dash {
		return m.space().Tab().Pane()
	}
	return nil
}

// Every line of an agent's block opens its pane, not just the title.
func TestSidebarAgentLinesAllClick(t *testing.T) {
	m := withAgents(t, 150, 42, demoIdle, demoBlocked)
	m = settle(t, m, func(m Model) bool { return localAgents(m) == 2 && blockedRow(m) >= 0 })
	for _, size := range [][2]int{{150, 42}, {110, 30}} {
		m.w, m.h = size[0], size[1]
		m.layoutPanes()
		m.cur = -1
		m.tab = tabSessions
		fits(t, m, size[0], size[1], "sidebar agents")
		var titles []sideRow
		for _, h := range m.vs.sideRows {
			if h.local != nil && h.sub == nil && (len(titles) == 0 || titles[len(titles)-1].local != h.local) {
				titles = append(titles, h)
			}
		}
		if len(titles) != 2 {
			t.Fatalf("%dx%d: %d agents in the sidebar", size[0], size[1], len(titles))
		}
		for _, h := range titles {
			for line := 0; line < 2; line++ {
				if !hasAgentLine(m, h, h.y+line) {
					continue // cut off by the bottom of the agents half
				}
				mm := click(m, 5, h.y+line).(Model)
				if mm.cur < 0 || mm.spaces[mm.cur] != h.local.space || mm.dash || focusedPane(mm) != any(h.local.pane) {
					t.Errorf("%dx%d: clicking line %d of %s: cur=%d dash=%v", size[0], size[1], line, h.local.space.Name, mm.cur, mm.dash)
				}
			}
		}
	}
}

// hasAgentLine says whether row y of the sidebar belongs to h's agent block.
func hasAgentLine(m Model, h sideRow, y int) bool {
	for _, r := range m.vs.sideRows {
		if r.y == y && r.local == h.local {
			return true
		}
	}
	return false
}

func externalRow(m Model) (sideRow, bool) {
	for _, h := range m.vs.sideRows {
		if h.local == nil && h.sess != nil && h.sub == nil && !h.isSpace {
			return h, true
		}
	}
	return sideRow{}, false
}

// An agent outside nanny opens its live conversation over where you are.
func TestSidebarExternalAgentOpensViewer(t *testing.T) {
	m := withAgents(t, 150, 42, demoIdle)
	m = settle(t, m, func(m Model) bool { return localAgents(m) == 1 })
	m.spaces = append(m.spaces, ws.NewSpace("web-checkout", filepath.Join(home, "dev", "web-checkout")))
	m.cur = len(m.spaces) - 1
	m.layoutPanes()
	viewOf(m)
	h, ok := externalRow(m)
	if !ok {
		t.Fatal("no agent outside nanny in the sidebar")
	}
	mm := click(m, 5, h.y).(Model)
	if mm.view == nil || mm.cur != m.cur || mm.view.sub {
		t.Fatalf("view=%v cur=%d want the viewer over space %d", mm.view != nil, mm.cur, m.cur)
	}
	if !strings.Contains(mm.status, "runs outside nanny") {
		t.Errorf("status %q", mm.status)
	}
	mm = key(mm, "esc").(Model)
	if mm.view != nil || mm.cur != m.cur || mm.dash {
		t.Errorf("after esc: view=%v cur=%d dash=%v", mm.view != nil, mm.cur, mm.dash)
	}
}

func TestSidebarFollowsCurrentSpace(t *testing.T) {
	m := manySpaces(t, boot(t, 110, 30).(Model), 25)
	m.cur = 20
	m.dash = true
	m.tab = tabSessions
	fits(t, m, 110, 30, "many spaces")
	if sideLine(m, "space-20") < 0 {
		t.Fatal("the current space is not in the sidebar")
	}
	var mm tea.Model = m
	for i := 0; i < 10; i++ {
		mm = mouseAt(mm, 5, bodyY+3, tea.MouseWheelUp, mousePress)
	}
	if sideLine(mm, "space-20") >= 0 {
		t.Fatal("the wheel was undone by following")
	}
	mm = key(mm, "alt+left")
	fits(t, mm, 110, 30, "after alt+left")
	if sideLine(mm, "space-19") < 0 {
		t.Error("space-19 is not visible after alt+left")
	}
}

func TestAltDownWalksAgents(t *testing.T) {
	m := withAgents(t, 150, 42, demoIdle, demoBlocked)
	m = settle(t, m, func(m Model) bool { return localAgents(m) == 2 && blockedRow(m) >= 0 })
	agents := m.walkAgents()
	start := map[string]int{"terminal": -1, "view": -1}
	for i, r := range agents {
		if r.local != nil && start["terminal"] < 0 {
			start["terminal"] = i
			m.focusRef(*r.local)
		}
	}
	for _, from := range []string{"terminal", "view"} {
		m.vs.sideCurKey = ""
		cur := m
		if from == "view" {
			cur.cur, cur.dash, cur.tab = -1, false, tabSessions
			start[from] = len(agents) - 1
		}
		var mm tea.Model = cur
		for i := 1; i <= len(agents)+1; i++ {
			mm = key(mm, "alt+down")
			got, want := mm.(Model), agents[(start[from]+i)%len(agents)]
			switch {
			case want.local != nil && (got.dash || got.view != nil || focusedPane(got) != any(want.local.pane)):
				t.Errorf("%s: alt+down #%d: dash=%v view=%v, want the pane of %s", from, i, got.dash, got.view != nil, want.local.space.Name)
			case want.local == nil && (got.view == nil || got.view.path != want.sess.Path):
				t.Errorf("%s: alt+down #%d did not open the viewer of %s", from, i, want.sess.Title)
			}
		}
		back := key(mm, "alt+up").(Model)
		if want := agents[(start[from]+len(agents))%len(agents)]; back.vs.sideCurKey != want.key() {
			t.Errorf("%s: alt+up stepped to %q, want %q", from, back.vs.sideCurKey, want.key())
		}
	}
}

func TestAltDownWithoutAgents(t *testing.T) {
	m := boot(t, 150, 42).(Model)
	m.live.allRows = nil
	mm := key(m, "alt+down").(Model)
	if !strings.Contains(mm.status, "no agents to go to") {
		t.Errorf("status %q", mm.status)
	}
}

func TestSidebarMoreLinesClickable(t *testing.T) {
	m := manySpaces(t, boot(t, 110, 30).(Model), 25)
	m.cur = -1
	m.tab = tabSessions
	fits(t, m, 110, 30, "many spaces")
	if m.vs.sideSpOff != 0 {
		t.Fatalf("offset %d", m.vs.sideSpOff)
	}
	y := sideLine(m, "more · wheel")
	if y < 0 {
		t.Fatal("no ↓ more line")
	}
	mm := click(m, 5, y).(Model)
	if mm.vs.sideSpOff == 0 {
		t.Fatal("clicking ↓ more did not scroll")
	}
	fits(t, mm, 110, 30, "after paging")
	up := sideLine(mm, "↑")
	if up < 0 {
		t.Fatal("no ↑ more line")
	}
	paged := mm.vs.sideSpOff
	if back := click(mm, 5, up).(Model); back.vs.sideSpOff >= paged {
		t.Errorf("clicking ↑ more did not scroll back: %d -> %d", paged, back.vs.sideSpOff)
	}
}

func TestHeaderOverflowKeepsCurrent(t *testing.T) {
	for _, size := range [][2]int{{150, 42}, {110, 30}} {
		m := manySpaces(t, boot(t, size[0], size[1]).(Model), 25)
		m.cur = 20
		m.dash = true
		m.tab = tabSessions
		out := fits(t, m, size[0], size[1], "header")
		head := strings.Split(out, "\n")[0]
		if !strings.Contains(head, "space-20") || !strings.Contains(head, "›") {
			t.Fatalf("%dx%d header: %q", size[0], size[1], head)
		}
		for _, h := range m.vs.spaceHits {
			if h.x1 > size[0] {
				t.Errorf("%dx%d: a hit past the screen: %+v", size[0], size[1], h)
			}
		}
		x := -1
		for _, h := range m.vs.spaceHits {
			if h.idx == -5 {
				x = h.x0
			}
		}
		if x < 0 {
			t.Fatal("no N› chip")
		}
		mm := click(m, x, 0).(Model)
		if mm.menu == nil || !strings.Contains(viewOf(mm), "space-24") {
			t.Errorf("%dx%d: the N› chip did not list space-24", size[0], size[1])
		}
	}
}

// The agents half starts at the middle of the sidebar whatever the spaces need.
func TestSidebarAgentsStartHalfway(t *testing.T) {
	m := withAgents(t, 150, 42, demoIdle, demoBlocked)
	m = settle(t, m, func(m Model) bool { return localAgents(m) == 2 })
	for _, size := range [][2]int{{150, 42}, {110, 30}} {
		m.w, m.h = size[0], size[1]
		m.layoutPanes()
		m.cur, m.tab = -1, tabSessions
		_ = viewOf(m)
		if mid := size[1] / 2; m.vs.sideSplit < mid-3 || m.vs.sideSplit > mid+3 {
			t.Errorf("%dx%d: agents start at row %d, want about %d", size[0], size[1], m.vs.sideSplit, mid)
		}
	}
}
