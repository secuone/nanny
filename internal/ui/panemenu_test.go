package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/secuone/nanny/internal/term"
	"github.com/secuone/nanny/internal/ws"
)

const (
	mouseOn  = `stty -icanon -echo; printf '\033[?1000h\033[?1006h'; exec cat -v`
	mouseOff = `exec cat -v`
)

// paneSpace shows a new space whose only pane runs script.
func paneSpace(t *testing.T, script string) (Model, *term.Pane) {
	t.Helper()
	m := boot(t, 150, 42).(Model)
	cwd := t.TempDir()
	sp := ws.NewSpace("pm", cwd)
	p, err := m.spawn(cwd, []string{"sh", "-c", script})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	tb := ws.NewTab("sh")
	tb.Panes = []*term.Pane{p}
	sp.Tabs = append(sp.Tabs, tb)
	m.spaces = append(m.spaces, sp)
	m.cur, m.dash = len(m.spaces)-1, false
	m.layoutPanes()
	viewOf(m)
	return m, p
}

func shiftRightClick(m tea.Model, x, y int) tea.Model {
	m, c := m.Update(tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseRight, Mod: tea.ModShift}))
	return drain(m, c, 0)
}

func paneSays(p *term.Pane, want string) bool {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(p.Text(30), want) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func TestRenamePaneSetsClearsAndPersists(t *testing.T) {
	m, p := paneSpace(t, mouseOff)
	auto := paneTitle(p)
	var mm tea.Model = m
	mm = key(mm, "ctrl+b")
	mm = key(mm, "e")
	if mm.(Model).modal == nil {
		t.Fatal("prefix e should ask for the pane's name")
	}
	mm.(Model).modal.input.SetValue("api")
	mm = key(mm, "enter")
	if p.Name != "api" || paneTitle(p) != "api" {
		t.Fatalf("name = %q title = %q", p.Name, paneTitle(p))
	}
	if !strings.Contains(ansi.Strip(viewOf(mm)), " api ") {
		t.Error("the pane border should show the name")
	}
	st, err := ws.Load()
	if err != nil {
		t.Fatal(err)
	}
	sp := st.Spaces[len(st.Spaces)-1]
	if got := sp.Tabs[0].Panes[0].Name; got != "api" {
		t.Fatalf("saved name = %q", got)
	}
	back := ws.Restore(st, true, nil, m.spawn, nil)
	rp := back[len(back)-1].Tabs[0].Panes[0]
	defer rp.Close()
	if rp.Name != "api" {
		t.Errorf("restored name = %q", rp.Name)
	}
	mm = key(mm, "ctrl+b")
	mm = key(mm, "e")
	mm.(Model).modal.input.SetValue("")
	mm = key(mm, "enter")
	if p.Name != "" || paneTitle(p) != auto {
		t.Errorf("empty name should clear it: %q %q", p.Name, paneTitle(p))
	}
}

func TestPaneMenuOrderAndRename(t *testing.T) {
	m, p := paneSpace(t, mouseOff)
	r := m.vs.rects[0]
	mm := mouseAt(m, r.x+5, r.y+5, tea.MouseRight, mousePress)
	if mm.(Model).menu == nil {
		t.Fatal("right-click in a pane should open the menu")
	}
	out := ansi.Strip(viewOf(mm))
	last := -1
	for _, want := range []string{"Rename pane…", "Split right", "Send right-clicks to pane", "New Claude tab", "Close pane"} {
		i := strings.Index(out, want)
		if i < 0 || i < last {
			t.Fatalf("menu item %q missing or out of order:\n%s", want, out)
		}
		last = i
	}
	if strings.Contains(out, "Zoom pane") {
		t.Error("zoom makes no sense with one pane")
	}
	mm = key(mm, "e")
	mm.(Model).modal.input.SetValue("web")
	key(mm, "enter")
	if p.Name != "web" {
		t.Errorf("name = %q", p.Name)
	}
}

func TestRightClickForwardedWhenOnAndProgramWantsMouse(t *testing.T) {
	m, p := paneSpace(t, mouseOn)
	deadline := time.Now().Add(3 * time.Second)
	for !p.WantsMouse() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !p.WantsMouse() {
		t.Fatal("the program should have turned mouse reporting on")
	}
	r := m.vs.rects[0]
	x, y := r.x+5, r.y+4
	mm := mouseAt(m, x, y, tea.MouseRight, mousePress)
	if mm.(Model).menu == nil {
		t.Fatal("with the toggle off the menu opens")
	}
	mm = key(mm, "esc")
	p.RightClicks = true
	mm = mouseAt(mm, x, y, tea.MouseRight, mousePress)
	if mm.(Model).menu != nil {
		t.Fatal("toggle on + mouse program: no menu")
	}
	mm = mouseAt(mm, x, y, tea.MouseRight, mouseRelease)
	// pane-relative, 1-based on the wire: col 5, row 4 inside the border
	// (a freshly attached pane drops answers for a moment: send until it lands)
	for i := 0; i < 100 && !strings.Contains(p.Text(30), "^[[<2;5;4m"); i++ {
		mm = mouseAt(mm, x, y, tea.MouseRight, mousePress)
		mm = mouseAt(mm, x, y, tea.MouseRight, mouseRelease)
		time.Sleep(20 * time.Millisecond)
	}
	if !paneSays(p, "^[[<2;5;4M") || !paneSays(p, "^[[<2;5;4m") {
		t.Fatalf("press and release should reach the program:\n%s", p.Text(30))
	}
	if mm.(Model).menu != nil {
		t.Error("release must not open the menu")
	}
	// the border and shift bring the menu back
	for name, mmm := range map[string]tea.Model{
		"title row":  mouseAt(mm, r.x+10, r.y, tea.MouseRight, mousePress),
		"left edge":  mouseAt(mm, r.x, r.y+3, tea.MouseRight, mousePress),
		"bottom row": mouseAt(mm, r.x+10, r.y+r.h-1, tea.MouseRight, mousePress),
		"shift":      shiftRightClick(mm, x, y),
	} {
		if mmm.(Model).menu == nil {
			t.Errorf("%s should open the menu", name)
		}
	}
	out := ansi.Strip(viewOf(mouseAt(mm, r.x, r.y+3, tea.MouseRight, mousePress)))
	if !strings.Contains(out, "✓ Send right-clicks to pane") {
		t.Error("the toggle should show a check when on")
	}
}

func TestRightClickOpensMenuWhenProgramIgnoresMouse(t *testing.T) {
	m, p := paneSpace(t, mouseOff)
	p.RightClicks = true
	r := m.vs.rects[0]
	mm := mouseAt(m, r.x+5, r.y+4, tea.MouseRight, mousePress)
	if mm.(Model).menu == nil {
		t.Fatal("a program without mouse reporting still gets the menu")
	}
}

func TestSendRightClicksToggleSavesAndFlips(t *testing.T) {
	m, p := paneSpace(t, mouseOff)
	r := m.vs.rects[0]
	mm := mouseAt(m, r.x+5, r.y+4, tea.MouseRight, mousePress)
	for _, it := range mm.(Model).menu.items {
		if strings.Contains(it.label, "Send right-clicks") {
			mod := mm.(Model)
			it.run(&mod)
			mm = mod
		}
	}
	if !p.RightClicks {
		t.Fatal("choosing it should turn it on")
	}
	st, _ := ws.Load()
	if !st.Spaces[len(st.Spaces)-1].Tabs[0].Panes[0].RightClicks {
		t.Error("the toggle should be saved")
	}
	back := ws.Restore(st, true, nil, m.spawn, nil)
	rp := back[len(back)-1].Tabs[0].Panes[0]
	defer rp.Close()
	if !rp.RightClicks {
		t.Error("the toggle should be restored")
	}
}
