package ui

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/secuone/nanny/internal/claude"
	"github.com/secuone/nanny/internal/config"
	"github.com/secuone/nanny/internal/demo"
	"github.com/secuone/nanny/internal/i18n"
	"github.com/secuone/nanny/internal/ws"
)

// drain runs a command tree synchronously, feeding results back into the model.
func drain(m tea.Model, c tea.Cmd, depth int) tea.Model {
	if c == nil || depth > 3 {
		return m
	}
	msg := c()
	switch msg := msg.(type) {
	case tea.BatchMsg:
		for _, cc := range msg {
			m = drain(m, cc, depth)
		}
		return m
	case tickMsg, frameMsg, nil:
		return m
	}
	if _, ok := msg.(tea.Msg); !ok {
		return m
	}
	m, next := m.Update(msg)
	return drain(m, next, depth+1)
}

func boot(t testing.TB, w, h int) tea.Model {
	t.Helper()
	return bootIn(t, "en", w, h)
}

// bootIn is boot with the UI in lang.
func bootIn(t testing.TB, lang string, w, h int) tea.Model {
	t.Helper()
	t.Cleanup(func() { i18n.Set("en") })
	dir := t.TempDir()
	if err := demo.Build(dir); err != nil {
		t.Fatal(err)
	}
	claude.SetHome(dir)
	home = dir
	t.Setenv("XDG_STATE_HOME", dir+"/state")
	t.Setenv("XDG_CONFIG_HOME", dir+"/config") // never touch the real config
	cfg := config.Default()
	cfg.SetOff(append(quietSources(), config.LayoutExtras)) // nothing of this machine in the tests; no repeats
	cfg.Alerts.Sound, cfg.Alerts.Desktop = false, false
	cfg.Language = lang
	var m tea.Model = New(cfg)
	m, _ = m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	mm := m.(Model)
	m = drain(m, tea.Batch(mm.scanCmd(), mm.sourcesCmd()), 0)
	mm = m.(Model)
	m = drain(m, mm.refreshDetail(), 0)
	mm = m.(Model)
	return drain(m, mm.reposCmd(), 0)
}

func key(m tea.Model, k string) tea.Model {
	var msg tea.KeyMsg
	switch k {
	case "ctrl+k":
		msg = tea.KeyMsg{Type: tea.KeyCtrlK}
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "right":
		msg = tea.KeyMsg{Type: tea.KeyRight}
	case "left":
		msg = tea.KeyMsg{Type: tea.KeyLeft}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
	m, c := m.Update(msg)
	return drain(m, c, 0)
}

// Every view must fill the terminal exactly, in every language: no overflowing or short lines.
func TestViewsFitTerminal(t *testing.T) {
	popups := map[string]bool{"n": true, "A": true, "?": true, "ctrl+k": true, "T": true, "u": true}
	for _, lang := range i18n.Langs {
		for _, size := range [][2]int{{150, 42}, {100, 30}, {80, 24}} {
			m := bootIn(t, lang, size[0], size[1])
			for _, tb := range []string{"1", "n", "A", "u", "2", "3", "4", "5", "6", "7", "8", "E", "I", "?", "ctrl+k", "T", "9"} {
				m = key(m, tb)
				out := m.View()
				lines := strings.Split(out, "\n")
				if len(lines) != size[1] {
					t.Errorf("%s %dx%d tab %s: %d lines", lang, size[0], size[1], tb, len(lines))
				}
				for i, l := range lines {
					if w := lipgloss.Width(l); w > size[0] {
						t.Errorf("%s %dx%d tab %s line %d is %d wide: %q", lang, size[0], size[1], tb, i, w, ansi.Strip(l))
					}
				}
				if popups[tb] {
					m = key(m, "esc")
				}
				if d := os.Getenv("NANNY_DUMP"); d != "" {
					_ = os.WriteFile(fmt.Sprintf("%s/view-%s-%dx%d-%s.txt", d, lang, size[0], size[1], tb), []byte(ansi.Strip(out)), 0o644)
				}
			}
		}
	}
}

// Labels drawn in fixed-width columns fit them in every language.
func TestTranslatedLabelsFit(t *testing.T) {
	defer i18n.Set("en")
	cols := map[int][]string{
		9:  {"agent id", "parent", "model", "mode", "started", "last act.", "worktree", "last tool", "folder", "branch", "session", "updated", "stats", "kind", "root", "worktrees", "path", "state", "file", "modified", "used", "bundles", "where", "name", "prompt", "answer", "task", "alerts", "spaces", "find", "context", "tokens", "cache", "cost", "added", "took", "run in"},
		8:  {"action", "live", "new", "shell", "review", "snippet"},
		11: {"version", "work", "heartbeat", "schedule", "next", "error", "last fail", "source", "detail", "needs", "intent", "workers", "enabled", "interval", "level", "last exit", "command", "output", "sessions", "subagents", "repos", "cost", "all time"},
	}
	for _, it := range settingsList {
		cols[23] = append(cols[23], it.name)
	}
	for _, lang := range i18n.Langs {
		i18n.Set(lang)
		for w, keys := range cols {
			for _, k := range keys {
				if got := T(k); lipgloss.Width(got) > w {
					t.Errorf("%s: %q → %q is wider than %d", lang, k, got, w)
				}
			}
		}
	}
}

// Changing the language in Settings applies at once.
func TestLanguageSwitchesLive(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := boot(t, 100, 30)
	m = key(m, "8")
	mm := m.(Model)
	for i, it := range settingsList {
		if it.name == "Language" {
			mm.settings.cur.idx = i
		}
	}
	m = key(mm, "l") // en → es
	if got := m.(Model).cfg.Language; got != "es" {
		t.Fatalf("language = %q, want es", got)
	}
	m = key(m, "1")
	if out := ansi.Strip(m.View()); !strings.Contains(out, "1 En vivo") || !strings.Contains(out, "Sesiones") { // Settings moved to the ≡ menu
		t.Errorf("tabs not in Spanish:\n%s", out)
	}
}

func TestLiveShowsSubagents(t *testing.T) {
	m := boot(t, 150, 42).(Model)
	if n := countAgents(m.live.rows); n != 2 {
		t.Fatalf("want 2 live agents, got %d", n)
	}
	subs := 0
	for _, r := range m.live.rows {
		if r.sub != nil {
			subs++
		}
	}
	if subs != 2 {
		t.Errorf("want 2 running subagents listed, got %d", subs)
	}
	out := ansi.Strip(m.View())
	for _, want := range []string{"Partial refunds for split payments", "Map payment adapters", "SPACES", "acme-api"} {
		if !strings.Contains(out, want) {
			t.Errorf("live view lacks %q", want)
		}
	}
}

func TestScopeFiltersEverything(t *testing.T) {
	m := boot(t, 150, 42)
	m = key(m, "2")
	all := len(m.(Model).sess.rows)
	mm := m.(Model)
	mm.scope = "web-checkout"
	mm.rebuild()
	if got := len(mm.sess.rows); got >= all || got != 2 {
		t.Errorf("scoped sessions = %d (all %d), want 2", got, all)
	}
	if n := countAgents(mm.live.rows); n != 1 {
		t.Errorf("scoped live agents = %d, want 1", n)
	}
}

func TestFilterAndPalette(t *testing.T) {
	m := boot(t, 150, 42)
	m = key(m, "2")
	m = key(m, "/")
	for _, r := range "zsh" {
		m = key(m, string(r))
	}
	if rows := m.(Model).sess.rows; len(rows) != 1 || rows[0].Title != "Tidy up zsh startup time" {
		t.Errorf("filter: got %d rows", len(rows))
	}
	m = key(m, "esc")
	m = key(m, "ctrl+k")
	if m.(Model).pal == nil {
		t.Fatal("palette did not open")
	}
	for _, r := range "new infra" {
		m = key(m, string(r))
	}
	p := m.(Model).pal
	if len(p.shown) == 0 || !strings.Contains(p.entries[p.shown[0]].label, "New claude in infra") {
		t.Errorf("palette top result wrong")
	}
}

func TestAlertsOnTransitions(t *testing.T) {
	m := boot(t, 150, 42).(Model)
	m.cfg.Alerts.Sound, m.cfg.Alerts.Desktop = false, false
	var busy *claude.Session
	for _, s := range m.sessions {
		if s.Live != nil && s.Live.Status == "busy" {
			busy = s
		}
	}
	if busy == nil {
		t.Fatal("demo has no busy session")
	}
	m.tab = tabSessions // not looking at it → it should be flagged as unseen
	busy.Live.Status = "idle"
	m.rebuild()
	if len(m.alerts.events) != 1 || m.alerts.events[0].kind != alertDone {
		t.Fatalf("want one done alert, got %+v", m.alerts.events)
	}
	if !m.alerts.unseen["s:"+busy.ID] {
		t.Error("finished agent should be unseen")
	}
	found := false
	for _, r := range m.live.allRows {
		if r.sess == busy && r.sub == nil {
			found = r.state() == "done"
		}
	}
	if !found {
		t.Error("unseen finished agent should show as done")
	}
	if !strings.Contains(ansi.Strip(m.View()), "finished") {
		t.Error("toast not rendered")
	}
	// looking at it clears the badge
	m.tab = tabLive
	for i, r := range m.live.rows {
		if r.sess == busy && r.agent() {
			m.live.cur.idx = i
		}
	}
	m.live.markSeen(&m)
	if m.alerts.unseen["s:"+busy.ID] {
		t.Error("selecting the agent should mark it seen")
	}
}

func TestSpacesWithRealPanes(t *testing.T) {
	m := boot(t, 140, 40).(Model)
	dir := t.TempDir()
	m.spaces = append(m.spaces, ws.NewSpace("proj", dir))
	m.cur = 0
	m.openTabIn(m.spaces[0], "shell", []string{"/bin/sh"})
	m.split(true, []string{"/bin/sh"})
	tab := m.space().Tab()
	if len(tab.Panes) != 2 || !tab.Vertical {
		t.Fatalf("split failed: %d panes", len(tab.Panes))
	}
	var mm tea.Model = m
	for _, r := range "echo nanny-$((6*7))" {
		mm = key(mm, string(r))
	}
	mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(ansi.Strip(mm.View()), "nanny-42") {
		time.Sleep(30 * time.Millisecond)
	}
	out := mm.View()
	if !strings.Contains(ansi.Strip(out), "nanny-42") {
		t.Fatalf("typed command output not visible:\n%s", ansi.Strip(out))
	}
	for i, l := range strings.Split(out, "\n") {
		if w := lipgloss.Width(l); w > 140 {
			t.Errorf("space view line %d is %d wide", i, w)
		}
	}
	// prefix + 0 goes home, prefix + ] comes back
	mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	mm = key(mm, "0")
	if mm.(Model).inSpace() {
		t.Error("prefix 0 should go home")
	}
	mm = key(mm, "alt+1")
	for _, p := range mm.(Model).space().Tab().Panes {
		p.Close()
	}
}

// Ten side-by-side panes must not balloon the rows with escapes of cut-away text.
func TestViewPanesManyPanesStaysLean(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	m := manyPanes(t, 10)
	tab := m.space().Tab()
	if len(tab.Panes) != 10 {
		t.Fatalf("got %d panes", len(tab.Panes))
	}
	w, h := m.vs.area.w, m.vs.area.h
	rows := strings.Split(m.viewPanes(w, h), "\n")
	if len(rows) != h {
		t.Fatalf("got %d rows, want %d", len(rows), h)
	}
	for y, row := range rows {
		if len(row) > 8*w {
			t.Errorf("row %d is %d bytes", y, len(row))
		}
		if strings.Contains(row, "\x1b[38;5;59m\x1b[0m") {
			t.Errorf("row %d has an empty SGR span", y)
		}
		plain := []rune(ansi.Strip(row))
		if len(plain) != w {
			t.Fatalf("row %d is %d cols, want %d", y, len(plain), w)
		}
		if y == 0 || y == h-1 {
			continue
		}
		for i, r := range m.vs.rects {
			if plain[r.x-m.vs.area.x] != '│' || plain[r.x-m.vs.area.x+r.w-1] != '│' {
				t.Errorf("row %d: pane %d border misplaced", y, i)
			}
		}
	}
}

func manyPanes(t testing.TB, n int) Model {
	m := boot(t, 200, 40).(Model)
	m.spaces = append(m.spaces, ws.NewSpace("proj", t.TempDir()))
	m.cur = 0
	m.openTabIn(m.spaces[0], "shell", []string{"/bin/sh"})
	for i := 1; i < n; i++ {
		m.split(true, []string{"/bin/sh"})
	}
	t.Cleanup(func() {
		for _, p := range m.space().Tab().Panes {
			p.Close()
		}
	})
	return m
}

func BenchmarkViewPanes10(b *testing.B) {
	m := manyPanes(b, 10)
	w, h := m.vs.area.w, m.vs.area.h
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.viewPanes(w, h)
	}
}

// The cached lines of a pane give way as soon as it prints, scrolls or loses focus.
func TestViewPanesCacheFollowsThePane(t *testing.T) {
	m := manyPanes(t, 2)
	w, h := m.vs.area.w, m.vs.area.h
	tab := m.space().Tab()
	tab.Panes[0].Note("fresh-output\r\n")
	if out := ansi.Strip(m.viewPanes(w, h)); !strings.Contains(out, "fresh-output") {
		t.Error("new output is not drawn")
	}
	first := m.viewPanes(w, h)
	tab.Focus = 0
	if m.viewPanes(w, h) == first {
		t.Error("focus change is not drawn")
	}
	for i := 0; i < 60; i++ {
		tab.Panes[0].Note("line\r\n")
	}
	before := m.viewPanes(w, h)
	tab.Panes[0].Scroll(10)
	if m.viewPanes(w, h) == before {
		t.Error("scrolling is not drawn")
	}
}

func TestViewerOpensSubagent(t *testing.T) {
	m := boot(t, 150, 42)
	mm := m.(Model)
	for i, r := range mm.live.rows {
		if r.sub != nil {
			mm.live.cur.idx = i
			break
		}
	}
	var c tea.Cmd
	m, c = tea.Model(mm).Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = drain(m, c, 0)
	if m.(Model).view == nil {
		t.Fatal("enter on a subagent should open the viewer")
	}
	if out := ansi.Strip(m.View()); !strings.Contains(out, "Subagent") || !strings.Contains(out, "On it") {
		t.Errorf("viewer missing content:\n%s", out)
	}
}

func TestThemePicker(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := boot(t, 100, 30)
	orig := m.(Model).cfg.Theme
	m = key(m, "T")
	if m.(Model).themes == nil {
		t.Fatal("T should open the theme picker")
	}
	m = key(m, "j")
	next := m.(Model).cfg.Theme
	if next == orig || m.(Model).st.P != config.Themes[next] {
		t.Fatalf("moving should preview the next theme, got %q", next)
	}
	m = key(m, "esc")
	if mm := m.(Model); mm.themes != nil || mm.cfg.Theme != orig || mm.st.P != config.Themes[orig] {
		t.Fatal("esc should revert the preview")
	}
	// every theme renders within bounds, light ones included
	m = key(m, "T")
	for range config.Themes {
		for i, l := range strings.Split(m.View(), "\n") {
			if w := lipgloss.Width(l); w > 100 {
				t.Errorf("%s line %d is %d wide", m.(Model).cfg.Theme, i, w)
			}
		}
		m = key(m, "j")
	}
	m = key(m, "enter")
	saved := m.(Model).cfg.Theme
	if m.(Model).themes != nil || config.Load().Theme != saved {
		t.Errorf("enter should save %q", saved)
	}
	// palette entry and prefix+T
	m = key(m, "ctrl+k")
	for _, r := range "theme…" {
		m = key(m, string(r))
	}
	m = key(m, "enter")
	if m.(Model).themes == nil {
		t.Fatal("palette Theme… should open the picker")
	}
	m = key(m, "esc")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	m = key(m, "T")
	if m.(Model).themes == nil {
		t.Error("prefix T should open the picker")
	}
}
