package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/secuone/nanny/internal/claude"
	"github.com/secuone/nanny/internal/term"
	"github.com/secuone/nanny/internal/ws"
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
		if mm := click(m, hit.x0, hit.y).(Model); mm.fp == nil {
			t.Errorf("cur=%d: clicking N new should ask for the folder", cur)
		}
	}
}

// Two marks on a line, around wide characters, still start and end where they are drawn.
func TestHarvestKeysWidths(t *testing.T) {
	line := "ab" + clickable("k1", "\x1b[31m日本\x1b[0m") + "é" + clickable("k2", "😀x") + " tail"
	out, hits := harvestKeys("top\n" + line)
	if ansi.Strip(out) != "top\nab日本é😀x tail" {
		t.Fatalf("the marks stay in the frame: %q", out)
	}
	want := []keyHit{{y: 1, x0: 2, x1: 6, key: "k1"}, {y: 1, x0: 7, x1: 10, key: "k2"}}
	if !slices.Equal(hits, want) {
		t.Errorf("hits = %v, want %v", hits, want)
	}
}

// With no live Claude session nothing can match, so the /proc scan is skipped;
// a session that appears makes the next tick scan and link it.
func TestTrackAgentsScansOnlyWithLiveSessions(t *testing.T) {
	m := boot(t, 120, 36).(Model)
	r, err := m.spawnTab(t.TempDir(), "shell", []string{"/bin/sh"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.pane.Close() })
	scans := 0
	procChildren = func() map[int][]int { scans++; return ws.ProcChildren() }
	t.Cleanup(func() { procChildren = ws.ProcChildren })
	old, _ := filepath.Glob(claude.Dir("sessions", "*.json"))
	for _, f := range old {
		_ = os.Remove(f)
	}

	m.trackAgents()
	if scans != 0 || len(m.vs.bySession) != 0 {
		t.Fatalf("scanned %d times with no sessions", scans)
	}

	dir := claude.Dir("sessions")
	_ = os.MkdirAll(dir, 0o755)
	js := fmt.Sprintf(`{"pid":%d,"sessionId":"new-session","startedAt":1}`, r.pane.Pid())
	if err := os.WriteFile(filepath.Join(dir, "new.json"), []byte(js), 0o644); err != nil {
		t.Fatal(err)
	}
	m.trackAgents()
	if scans != 1 {
		t.Fatalf("scans = %d, want 1 once a session is live", scans)
	}
	if got, ok := m.vs.bySession["new-session"]; !ok || got.pane != r.pane {
		t.Fatalf("new session not linked: %v", m.vs.bySession)
	}
}

func TestPaneDelayFavoursTheFocusedPane(t *testing.T) {
	if got := paneDelay(true); got != 16*time.Millisecond {
		t.Errorf("focused pane delay = %v", got)
	}
	if got := paneDelay(false); got != 50*time.Millisecond {
		t.Errorf("background delay = %v", got)
	}
}

// waitPane redraws fast for the focused pane's output and slowly for the rest.
func TestWaitPaneCoalescesBackgroundOutput(t *testing.T) {
	m := boot(t, 120, 36).(Model)
	a, err := m.spawnTab(t.TempDir(), "a", []string{"/bin/sh"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.spawnTab(t.TempDir(), "b", []string{"/bin/sh"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.pane.Close(); b.pane.Close() })
	m.cur = len(m.spaces) - 1
	m.space().Active = len(m.space().Tabs) - 1
	focus, other := m.focusedPane(), a.pane
	if focus == nil || focus == other {
		t.Fatalf("focus %v other %v", focus, other)
	}
	for len(m.paneCh) > 0 {
		<-m.paneCh
	}
	time.Sleep(300 * time.Millisecond) // let the shells' prompts settle
	for len(m.paneCh) > 0 {
		<-m.paneCh
	}
	took := func(p *term.Pane) time.Duration {
		cmd := m.waitPane()
		start := time.Now()
		go func() { p.Note("x"); m.paneCh <- struct{}{} }()
		cmd()
		return time.Since(start)
	}
	if d := took(focus); d >= 45*time.Millisecond {
		t.Errorf("focused output took %v", d)
	}
	if d := took(other); d < 50*time.Millisecond {
		t.Errorf("background output took %v", d)
	}
}
