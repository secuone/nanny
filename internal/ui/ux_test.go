package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/secuone/nanny/internal/claude"
	"github.com/secuone/nanny/internal/config"
)

// bootEmpty starts nanny on a home with no sessions, repos, skills or spaces.
func bootEmpty(t *testing.T, w, h int) tea.Model {
	t.Helper()
	dir := t.TempDir()
	claude.SetHome(dir)
	home = dir
	t.Setenv("XDG_STATE_HOME", dir+"/state")
	t.Setenv("XDG_CONFIG_HOME", dir+"/config")
	cfg := config.Default()
	cfg.SetOff(append(quietSources(), config.LayoutExtras)) // nothing of this machine in the tests; no repeats
	cfg.Alerts.Sound, cfg.Alerts.Desktop = false, false
	cfg.Language = "en"
	var m tea.Model = New(cfg)
	m, _ = m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	mm := m.(Model)
	return drain(m, tea.Batch(mm.scanCmd(), mm.sourcesCmd(), mm.reposCmd(), mm.skillsCmd()), 0)
}

func click(m tea.Model, x, y int) tea.Model {
	m, c := m.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	return drain(m, c, 0)
}

// The help screen fits every size, scrolls, and lists the per-view keys.
func TestHelpFitsAndScrolls(t *testing.T) {
	for _, size := range [][2]int{{150, 42}, {100, 30}, {80, 24}} {
		m := key(boot(t, size[0], size[1]), "?")
		out := m.View()
		if n := len(strings.Split(out, "\n")); n != size[1] {
			t.Errorf("%dx%d help: %d lines", size[0], size[1], n)
		}
		for i, l := range strings.Split(out, "\n") {
			if w := lipgloss.Width(l); w > size[0] {
				t.Errorf("%dx%d help line %d is %d wide", size[0], size[1], i, w)
			}
		}
		for range 200 {
			m = key(m, "j")
		}
		if !m.(Model).help {
			t.Fatal("j should scroll help, not close it")
		}
		if !strings.Contains(ansi.Strip(m.View()), "Popups") {
			t.Errorf("%dx%d: scrolling to the end should show the last section", size[0], size[1])
		}
		m = key(m, "k")
		if off := m.(Model).helpOff; off == 0 || off > 200 {
			t.Errorf("help offset not clamped: %d", off)
		}
		if m = key(m, "esc"); m.(Model).help {
			t.Error("esc should close help")
		}
	}
}

// Every footer keeps "? help" visible, even at 80 columns.
func TestFooterAlwaysShowsHelp(t *testing.T) {
	m := boot(t, 80, 24)
	for _, tb := range []string{"1", "2", "3", "4", "5", "6", "7", "8"} {
		m = key(m, tb)
		lines := strings.Split(ansi.Strip(m.View()), "\n")
		if foot := lines[len(lines)-1]; !strings.Contains(foot, "? help") {
			t.Errorf("tab %s footer lacks ? help: %q", tb, foot)
		}
	}
}

// A first run says what to do next instead of showing blank panels.
func TestEmptyStatesGuide(t *testing.T) {
	m := bootEmpty(t, 120, 36)
	for tb, want := range map[string]string{
		"1": "Welcome to nanny",
		"2": "No Claude Code sessions yet",
		"3": "No repositories yet",
		"6": "No skills, commands or agents yet",
		"7": "customize what shows",
	} {
		m = key(m, tb)
		if out := ansi.Strip(m.View()); !strings.Contains(out, want) {
			t.Errorf("tab %s lacks %q:\n%s", tb, want, out)
		}
	}
}

func TestClickSelectsAndDoubleClickOpens(t *testing.T) {
	m := key(boot(t, 150, 42), "2")
	_ = m.View()
	vs := m.(Model).vs
	y := -1
	for i, r := range vs.listRows {
		if r == 2 {
			y = vs.listY0 + i
			break
		}
	}
	if y < 0 {
		t.Fatal("third session not on screen")
	}
	x := m.(Model).sideW() + 4
	if m = click(m, x, y); m.(Model).sess.cur.idx != 2 {
		t.Errorf("click selected %d, want 2", m.(Model).sess.cur.idx)
	}
	// Live: double-clicking a subagent opens the viewer
	m = key(m, "1")
	_ = m.View()
	vs = m.(Model).vs
	for i, r := range vs.listRows {
		if m.(Model).live.rows[r].sub != nil {
			y = vs.listY0 + i
			break
		}
	}
	m = click(click(m, x, y), x, y)
	if m.(Model).view == nil {
		t.Fatal("double-click on a subagent should open the viewer")
	}
	// the viewer takes the whole body: no dashboard border peeks around it
	if out := ansi.Strip(m.View()); strings.Contains(out, "Live agents") {
		t.Error("viewer should replace the dashboards, not float over them")
	}
}

func TestClickInPalettePicksRow(t *testing.T) {
	m := key(boot(t, 150, 42), "ctrl+k")
	_ = m.View()
	y := m.(Model).vs.modalY + 3
	m = click(m, 75, y)
	if p := m.(Model).pal; p == nil || p.cur != 3 {
		t.Errorf("click in palette should select row 3")
	}
}

// v reads, s mirrors the screen, the same way in every view.
func TestKeyVocabulary(t *testing.T) {
	m := boot(t, 150, 42)
	if m = key(m, "v"); m.(Model).view == nil {
		t.Error("v on Live should open the conversation viewer")
	}
	m = key(m, "esc")
	m = key(m, "3")
	m = key(m, "j")
	m = key(m, "j")
	if m = key(m, "v"); m.(Model).view == nil {
		t.Error("v on a worktree with sessions should open the viewer")
	}
	m = key(m, "esc")
	m = key(m, "6")
	if m = key(m, "t"); m.(Model).skills.kind == "" {
		t.Error("t should cycle the skills type filter")
	}
}
