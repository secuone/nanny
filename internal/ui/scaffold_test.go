package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/secuone/nanny/internal/config"
)

type fakeMsg struct{}

func (fakeMsg) bgMsg() {}

// fakeFeat stands in for a feature and counts what the model asks of it.
type fakeFeat struct {
	featBase
	cur, dues, forced int
	off               int // a section that is turned off, -1 none
	isBusy, typ       bool
	keys              []string
	clicks            []int
	got               int
	badgeText         string
}

func (f *fakeFeat) secs(*Model) []secDef {
	var out []secDef
	for i, n := range []string{"Alpha", "Beta", "Gamma"} {
		out = append(out, secDef{n, i, i != f.off})
	}
	return out
}
func (f *fakeFeat) sec() int                       { return f.cur }
func (f *fakeFeat) setSec(_ *Model, i int) tea.Cmd { f.cur = i; return nil }
func (f *fakeFeat) view(m *Model, w, h int) string {
	return m.st.panel("Fake", "list", w, h, !m.focusR)
}
func (f *fakeFeat) detail(m *Model, w, h int) string { return m.st.panel("", "detail", w, h, m.focusR) }
func (f *fakeFeat) key(_ *Model, k tea.KeyMsg) tea.Cmd {
	f.keys = append(f.keys, k.String())
	return nil
}
func (f *fakeFeat) click(_ *Model, row int) tea.Cmd { f.clicks = append(f.clicks, row); return nil }
func (f *fakeFeat) badge(*Model) string {
	if f.badgeText == "" {
		return ""
	}
	return clickable(secKey(tabJira, 2), f.badgeText)
}
func (f *fakeFeat) due(_ *Model, force bool) tea.Cmd {
	f.dues++
	if force {
		f.forced++
	}
	return nil
}
func (f *fakeFeat) update(_ *Model, msg tea.Msg) (tea.Cmd, bool) {
	if _, ok := msg.(fakeMsg); ok {
		f.got++
		return nil, true
	}
	return nil, false
}
func (f *fakeFeat) typing() bool { return f.typ }
func (f *fakeFeat) busy() bool   { return f.isBusy }

func fakeJira(t *testing.T) *fakeFeat {
	t.Helper()
	f := &fakeFeat{off: -1}
	featFake[tabJira] = f
	t.Cleanup(func() { delete(featFake, tabJira) })
	return f
}

// E and I open Jira and Insights; 3 stays the Git view (the Worktrees one for now).
func TestFeatureViewKeys(t *testing.T) {
	m := boot(t, 150, 42)
	for k, want := range map[string]tab{"E": tabJira, "I": tabInsights, "3": tabGit, "1": tabLive} {
		if m = key(m, k); !m.(Model).onTab(want) {
			t.Errorf("%s should open %s", k, tabNames[want])
		}
	}
	bar := strings.Split(ansi.Strip(key(m, "E").View()), "\n")[viewY]
	for _, w := range []string{"3 Git", "E Jira", "I Insights"} {
		if !strings.Contains(bar, w) {
			t.Errorf("the views bar lacks %q: %q", w, bar)
		}
	}
	if strings.Contains(bar, "Worktrees") || strings.Contains(bar, "9 Jira") {
		t.Errorf("Worktrees is Git now, and Jira has a letter: %q", bar)
	}
}

// Tab walks every view that is on, the new ones included.
func TestTabReachesFeatureViews(t *testing.T) {
	var m tea.Model = key(boot(t, 150, 42), "8")
	seen := map[tab]bool{}
	for range 4 {
		m = key(m, "tab")
		seen[m.(Model).tab] = true
	}
	if !seen[tabJira] || !seen[tabInsights] {
		t.Errorf("tab should reach Jira and Insights: %v", seen)
	}
}

// Customize lists the ids of the features, and the basic profile has them off.
func TestCustomizeFeatureIds(t *testing.T) {
	m := key(boot(t, 150, 60), "=").(Model)
	m.cust.shut = map[string]bool{}
	have := map[string]bool{}
	for _, r := range m.cust.rows() {
		have[r.n.id] = true
	}
	for _, id := range []string{"view.git", "git.prs", "git.reviews", "git.worktrees", "git.branches", "git.profile",
		"view.jira", "view.insights", "sidebar.jira", "header.reviews"} {
		if !have[id] {
			t.Errorf("Customize lacks %s", id)
		}
	}
	if have["view.worktrees"] {
		t.Error("view.worktrees became view.git")
	}
}

func TestBasicLeavesFeatureViewsOut(t *testing.T) {
	var m tea.Model = key(boot(t, 150, 42), "M") // full → basic is next? walk until basic
	for m.(Model).cfg.ActiveProfile() != config.ProfileBasic {
		m = key(m, "M")
	}
	bar := strings.Split(ansi.Strip(m.View()), "\n")[viewY]
	for _, w := range []string{"Git", "Jira", "Insights"} {
		if strings.Contains(bar, w) {
			t.Errorf("basic shows %s on the bar: %q", w, bar)
		}
	}
	if m = key(m, "E"); m.(Model).onTab(tabJira) || !strings.Contains(m.(Model).status, "turned off") {
		t.Error("E should say Jira is off in basic")
	}
}

// A view with sections gets the bar under the views bar: ←→ skip the ones that
// are off and a click picks one.
func TestFeatureSections(t *testing.T) {
	f := fakeJira(t)
	f.off = 1
	m := key(boot(t, 150, 42), "E")
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	if bar := lines[secY]; !strings.Contains(bar, "Alpha") || strings.Contains(bar, "Beta") || !strings.Contains(bar, "Gamma 2") {
		t.Errorf("sections bar: %q", bar)
	}
	if m = key(m, "right"); f.cur != 2 {
		t.Errorf("→ should skip the section that is off: %d", f.cur)
	}
	if m = key(m, "right"); f.cur != 0 {
		t.Errorf("→ wraps: %d", f.cur)
	}
	m = key(m, "left")
	if f.cur != 2 {
		t.Errorf("← wraps back: %d", f.cur)
	}
	m.View()
	var alpha hit
	for _, h := range m.(Model).vs.secHits {
		if h.idx == 0 {
			alpha = h
		}
	}
	if mm := mouseAt(m, alpha.x0+1, secY, tea.MouseButtonNone, tea.MouseActionMotion); mm.(Model).vs.hover.kind != "sec" {
		t.Error("hovering a section should mark it")
	}
	if click(m, alpha.x0+1, secY); f.cur != 0 {
		t.Errorf("a click on Alpha should pick it: %d", f.cur)
	}
	m = key(m, "E")
	lines = strings.Split(ansi.Strip(m.View()), "\n")
	if got := strings.Join(lines, "\n"); !strings.Contains(got, "list") || !strings.Contains(got, "detail") {
		t.Errorf("list and detail should show:\n%s", got)
	}
}

// A feature fetches only when asked: on the tick, on opening, on R.
func TestFeatureDue(t *testing.T) {
	f := fakeJira(t)
	var m tea.Model = boot(t, 150, 42)
	before := f.dues
	m, _ = m.Update(tickMsg(time.Now()))
	if f.dues != before+1 || f.forced != 0 {
		t.Errorf("a tick asks once without force: due %d forced %d", f.dues, f.forced)
	}
	m = key(m, "E")
	if f.forced != 1 {
		t.Errorf("opening the view forces: %d", f.forced)
	}
	key(m, "R")
	if f.forced != 2 {
		t.Errorf("R forces: %d", f.forced)
	}
}

// busy() makes frames run; its own messages reach update and are drawn at the flush.
func TestFeatureBusyAndMessages(t *testing.T) {
	f := fakeJira(t)
	idle := bootEmpty(t, 150, 42).(Model)
	if idle.animating() {
		t.Fatal("nothing moves in an empty home")
	}
	f.isBusy = true
	if !idle.animating() {
		t.Error("a feature waiting for an answer should start the frames")
	}
	if !background(fakeMsg{}) {
		t.Error("a bgMsg is a background message")
	}
	if _, cmd := idle.Update(fakeMsg{}); f.got != 1 {
		t.Errorf("update should get its message: %d %v", f.got, cmd)
	}
}

// A text box of the feature gets every key, the global ones too; a badge on the
// top bar opens the view on its section.
func TestFeatureTypingAndBadge(t *testing.T) {
	f := fakeJira(t)
	var m tea.Model = key(boot(t, 150, 42), "E")
	f.typ = true
	for _, k := range []string{"q", "1", "E", "R"} {
		m = key(m, k)
	}
	if !m.(Model).onTab(tabJira) || strings.Join(f.keys, "") != "q1ER" {
		t.Errorf("typing: tab %v keys %v", m.(Model).tab, f.keys)
	}
	f.typ = false
	m = key(m, "1")
	f.badgeText = "⚑ 2 waiting"
	m.View()
	var hit keyHit
	for _, h := range m.(Model).vs.keyHits {
		if h.key == secKey(tabJira, 2) {
			hit = h
		}
	}
	if hit.key == "" {
		t.Fatal("the badge should be a click target on the top bar")
	}
	if !strings.Contains(strings.Split(ansi.Strip(m.View()), "\n")[hit.y], "⚑ 2 waiting") {
		t.Error("the badge should show on the top bar")
	}
	m = click(m, hit.x0+1, hit.y)
	if !m.(Model).onTab(tabJira) || f.cur != 2 {
		t.Errorf("a click on the badge opens its section: tab %v sec %d", m.(Model).tab, f.cur)
	}
}

// A click on a row goes to the feature.
func TestFeatureClickRow(t *testing.T) {
	f := &fakeFeat{off: -1}
	featFake[tabInsights] = f
	t.Cleanup(func() { delete(featFake, tabInsights) })
	m := key(boot(t, 150, 42), "I")
	mm := m.(Model)
	mm.vs.listY0, mm.vs.listRows = 8, []int{0, 1, 2}
	click(mm, 50, 9)
	if len(f.clicks) != 1 || f.clicks[0] != 1 {
		t.Errorf("clicks: %v", f.clicks)
	}
}
