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

// bytesSpace is a space whose pane prints every byte it receives, in hex.
func bytesSpace(t *testing.T, m Model) (Model, *term.Pane) {
	t.Helper()
	m.spaces = append(m.spaces, ws.NewSpace("proj", t.TempDir()))
	m.cur = len(m.spaces) - 1
	m.dash = false
	m.openTabIn(m.spaces[m.cur], "bytes", []string{"/bin/sh", "-c", "stty raw -echo; od -An -tx1 -w1"})
	m.layoutPanes()
	m.status = ""
	p := m.space().Tab().Pane()
	t.Cleanup(p.Close)
	screenHas(t, p, "") // started
	time.Sleep(200 * time.Millisecond)
	return m, p
}

func TestCtrlQFromSpaceTerminal(t *testing.T) {
	m, p := bytesSpace(t, boot(t, 150, 42).(Model))
	mm, _ := m.Update(keyPress("ctrl+q"))
	m = mm.(Model)
	if m.modal == nil || !m.modal.quit {
		t.Fatal("ctrl+q in a space should ask to quit")
	}
	time.Sleep(300 * time.Millisecond)
	if strings.Contains(p.Text(20), "11") {
		t.Errorf("the pane got the key:\n%s", p.Text(20))
	}
	mm, cmd := m.Update(keyPress("ctrl+q"))
	if mm.(Model).modal != nil || cmd == nil {
		t.Fatal("ctrl+q again should answer yes")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("the second ctrl+q should quit")
	}
}

func TestCtrlQOffPassesThrough(t *testing.T) {
	m := boot(t, 150, 42).(Model)
	m.cfg.QuitKey = "none"
	m, p := bytesSpace(t, m)
	mm, _ := m.Update(keyPress("ctrl+q"))
	if mm.(Model).modal != nil {
		t.Fatal("quit_key none should not open the question")
	}
	screenHas(t, p, "11")
	if out := fits(t, mm, 150, 42, "space"); strings.Contains(out, "quit") && strings.Contains(out, "^q") {
		t.Error("the footer should not show a quit key that is off")
	}
}

func TestCtrlQNotStolenWhileTyping(t *testing.T) {
	m := boot(t, 150, 42)
	m = key(m, "n")
	if m.(Model).task == nil {
		t.Fatal("n should open the new task form")
	}
	if m = key(m, "ctrl+q"); m.(Model).modal != nil {
		t.Error("ctrl+q must not quit while a form takes text")
	}
}

func TestFooterShowsQuit(t *testing.T) {
	for _, w := range []int{150, 100} {
		m := boot(t, w, 42)
		if out := fits(t, m, w, 42, "home"); !strings.Contains(out, "^q quit") {
			t.Errorf("%d cols: home footer without ^q quit", w)
		}
		sm, _ := bytesSpace(t, m.(Model))
		if out := fits(t, sm, w, 42, "space"); !strings.Contains(out, "^q quit") {
			t.Errorf("%d cols: space footer without ^q quit", w)
		}
	}
}

func TestPaletteHasQuitAndSpaces(t *testing.T) {
	m := withAgents(t, 150, 42, demoIdle)
	m.cur = -1
	mm := key(m, "ctrl+k").(Model)
	mm.pal.input.SetValue("quit")
	mm.pal.filter()
	if e := mm.pal.entries[mm.pal.shown[0]]; e.label != "Quit nanny: panes keep running" {
		t.Errorf("top result for quit is %q", e.label)
	}
	mm = key(mm, "esc").(Model)
	mm = key(mm, "ctrl+k").(Model)
	mm.pal.input.SetValue("go to space infra")
	mm.pal.filter()
	if len(mm.pal.shown) == 0 || !strings.HasPrefix(mm.pal.entries[mm.pal.shown[0]].label, "Go to space") {
		t.Fatal("the palette should list spaces")
	}
	mm = key(mm, "enter").(Model)
	if mm.cur != len(mm.spaces)-1 {
		t.Errorf("enter should go to the space, cur=%d", mm.cur)
	}
}

func TestWelcomeSaysCtrlQ(t *testing.T) {
	t.Setenv("NANNY_NO_WELCOME", "")
	m := bootEmpty(t, 80, 24)
	if out := ansi.Strip(viewOf(m)); !strings.Contains(out, "ctrl+q") {
		t.Errorf("welcome page 0 should say ctrl+q:\n%s", out)
	}
}
