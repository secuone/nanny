package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// In a space the view bar stays, scoped to the space; prefix i opens it, 0
// and esc go back to the terminal.
func TestSpaceViews(t *testing.T) {
	m := withAgents(t, 150, 42, demoIdle)
	m.cur = len(m.spaces) - 1
	m.layoutPanes()
	sp := m.space()
	out := ansi.Strip(viewOf(m))
	if lines := strings.Split(out, "\n"); !strings.Contains(lines[viewY], "0 ▣ terminal") || !strings.Contains(lines[viewY], "1 Live") {
		t.Fatalf("the view bar should stay in a space:\n%s", lines[viewY])
	}
	mm, _ := m.Update(keyPress("ctrl+b"))
	m = key(mm, "i").(Model)
	if !m.dash || !m.onTab(tabLive) {
		t.Fatal("prefix i should show the space's views")
	}
	for _, r := range m.live.rows {
		if _, cwd := r.titleCwd(); !sp.Contains(cwd) {
			t.Errorf("Live in a space lists %s, outside it", cwd)
		}
	}
	if m = key(m, "4").(Model); !m.onTab(tabTasks) {
		t.Error("4 should show the tasks of the space")
	}
	fits(t, m, 150, 42, "tasks in a space")
	if m = key(m, "0").(Model); m.dash {
		t.Error("0 should go back to the terminal")
	}
	m.dash = true
	if m = key(m, "esc").(Model); m.dash {
		t.Error("esc should go back to the terminal")
	}
	// home shows everything again
	m.cur = -1
	mm, _ = m.Update(statusMsg{})
	if m = mm.(Model); len(m.live.rows) <= 1 {
		t.Errorf("home should list every agent, got %d rows", len(m.live.rows))
	}
}

// Key hints, the view bar and the terminal strip are clickable.
func TestClickHints(t *testing.T) {
	var m tea.Model = boot(t, 150, 42)
	viewOf(m)
	at := func(m tea.Model, k string, y int) keyHit {
		t.Helper()
		for _, h := range m.(Model).vs.keyHits {
			if h.key == k && (y < 0 || h.y == y) {
				return h
			}
		}
		t.Fatalf("no %q hint on screen", k)
		return keyHit{}
	}
	h := at(m, "?", 41)
	if m = click(m, h.x0, h.y); !m.(Model).help {
		t.Error("clicking ? help should open the help")
	}
	m = key(m, "esc")

	// in a space, footer hints are prefix commands, never typed into the pane
	mm := withAgents(t, 150, 42, demoIdle)
	mm.cur = len(mm.spaces) - 1
	mm.layoutPanes()
	m = mm
	viewOf(m)
	h = at(m, "z", 41)
	if m = click(m, h.x0, h.y); !m.(Model).space().Tab().Zoom {
		t.Error("clicking z zoom should zoom the pane")
	}
	n := len(m.(Model).space().Tabs)
	for _, th := range m.(Model).vs.termHits {
		if th.idx == -4 {
			m = click(m, th.x0, termY)
		}
	}
	if got := len(m.(Model).space().Tabs); got != n+1 {
		t.Errorf("+ shell should open a tab: %d → %d", n, got)
	}
	for _, th := range m.(Model).vs.tabHits {
		if th.idx == int(tabSessions) {
			m = click(m, th.x0, viewY)
		}
	}
	if mm := m.(Model); !mm.dash || !mm.onTab(tabSessions) {
		t.Error("clicking 2 Sessions in a space should show its sessions")
	}
}

// A Claude pane that exits (declined trust, /exit) becomes a shell in its folder.
func TestClaudeExitLeavesShell(t *testing.T) {
	m := boot(t, 120, 36).(Model)
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	dir := t.TempDir()
	r, err := m.spawnTab(dir, "claude", []string{"claude"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, r := range m.allPanes() {
			r.pane.Close()
		}
	})
	for end := time.Now().Add(5 * time.Second); !r.pane.Exited() && time.Now().Before(end); {
		time.Sleep(20 * time.Millisecond)
	}
	m.trackAgents()
	p := r.tab.Pane()
	if p == r.pane || p.Exited() || r.tab.Label != "shell" {
		t.Fatalf("the exited claude should be swapped for a shell (label %q)", r.tab.Label)
	}
	screenHas(t, p, "claude exited (code 1)")
}

// "+ N new" stays in the sidebar once there are spaces, and a click opens the form.
func TestSidebarNewSpaceStays(t *testing.T) {
	m := withAgents(t, 150, 42, demoIdle)
	for _, cur := range []int{-1, len(m.spaces) - 1} {
		m.cur = cur
		m.layoutPanes()
		viewOf(m)
		var hit *keyHit
		for _, h := range m.vs.keyHits {
			if h.key == "N" && h.x0 < m.sideW() {
				hit = &h
			}
		}
		if hit == nil {
			t.Fatalf("cur=%d: no N new in the sidebar", cur)
		}
		if mm := click(m, hit.x0, hit.y).(Model); mm.modal == nil {
			t.Errorf("cur=%d: clicking N new should ask for the folder", cur)
		}
	}
}
