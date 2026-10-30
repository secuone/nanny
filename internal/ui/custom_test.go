package ui

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/secuone/nanny/internal/config"
	"github.com/secuone/nanny/internal/sources"
)

// quietSources turns off every Monitor source that reads this machine, so the
// tests see only what they feed in (the user's watchers stay on).
func quietSources() []string {
	return []string{"monitor.machine", "monitor.ports", "monitor.containers", "monitor.tilt", "monitor.jobs", "monitor.cron", "monitor.hermes"}
}

// custGo puts the customize cursor on a node, by its id.
func custGo(t *testing.T, m tea.Model, id string) tea.Model {
	t.Helper()
	mm := m.(Model)
	for i, r := range mm.cust.rows() {
		if r.n.id == id {
			mm.cust.cur = i
			return mm
		}
	}
	t.Fatalf("no row %s", id)
	return m
}

// spyCollectors swaps the Monitor's sources for ones that only say they ran.
func spyCollectors(t *testing.T) *[]string {
	t.Helper()
	var ran []string
	old := sources.Collectors
	t.Cleanup(func() { sources.Collectors = old })
	sources.Collectors = nil
	for _, c := range old {
		name := c.Name
		c.Run = []func() []sources.Item{func() []sources.Item { ran = append(ran, name); return nil }}
		sources.Collectors = append(sources.Collectors, c)
	}
	return &ran
}

// = opens Customize; the "only Tilt" preset leaves the Monitor with Tilt
// alone: the other sections are gone and their sources don't even run.
func TestCustomizeMonitorOnlyTilt(t *testing.T) {
	var m tea.Model = key(boot(t, 150, 46), "7")
	if m = key(m, "="); m.(Model).cust == nil {
		t.Fatal("= should open Customize")
	}
	if out := ansi.Strip(m.View()); !strings.Contains(out, "Customize") || !strings.Contains(out, "Monitor") || !strings.Contains(out, "Top bar") {
		t.Fatalf("the tree of what can be turned off:\n%s", out)
	}
	m = key(m, "t")
	c := m.(Model).cfg
	for _, sc := range monSecs[1:] {
		if on := m.(Model).monOn(sc.src); on != (sc.src == "tilt") {
			t.Errorf("%s on = %v", sc.src, on)
		}
	}
	if !c.On("view.monitor") {
		t.Error("the Monitor stays on")
	}
	ran := spyCollectors(t)
	m.(Model).sourcesCmd()()
	if !slices.Equal(*ran, []string{"tilt"}) {
		t.Errorf("only Tilt should be collected, ran %v", *ran)
	}
	m = key(m, "esc")
	m, _ = m.Update(sourcesMsg{{Group: "Tilt", Name: "api", Status: sources.Running}, {Group: "Open ports", Name: ":8000", Status: sources.Running}})
	out := ansi.Strip(m.View())
	bar := strings.Split(out, "\n")[secY]
	if !strings.Contains(bar, "Tilt 1") || strings.Contains(bar, "Ports") || strings.Contains(bar, "Hermes") {
		t.Errorf("the sections bar should have All and Tilt only: %q", bar)
	}
	if strings.Contains(out, ":8000") {
		t.Error("an item of a section turned off still shows")
	}
	// no Monitor at all: off the bar, its key does nothing, nothing collected
	m = key(key(key(m, "="), "n"), "esc")
	if m.(Model).onTab(tabRadar) {
		t.Error("the Monitor turned off should leave the screen")
	}
	*ran = nil
	m.(Model).sourcesCmd()()
	if len(*ran) != 0 {
		t.Errorf("nothing should be collected with the Monitor off, ran %v", *ran)
	}
	if m = key(m, "7"); m.(Model).onTab(tabRadar) {
		t.Error("7 should not open a view that is off")
	}
}

// Live off: gone from the bar and its key, home is the next view; it is saved
// in the config and survives a reload.
func TestCustomizeLiveOff(t *testing.T) {
	var m tea.Model = withAgents(t, 150, 42, demoIdle)
	m = key(m, "=")
	m = custGo(t, m, "view.live")
	m = key(key(m, " "), "esc")
	bar := strings.Split(ansi.Strip(m.View()), "\n")[viewY]
	if strings.Contains(bar, "Live") || !strings.Contains(bar, "Sessions") {
		t.Errorf("Live should be off the bar: %q", bar)
	}
	if !m.(Model).onTab(tabSessions) {
		t.Error("home is the first view still on")
	}
	if m = key(m, "1"); m.(Model).onTab(tabLive) || !strings.Contains(m.(Model).status, "turned off") {
		t.Error("1 should say Live is off instead of opening it")
	}
	if got := config.Load(); got.On("view.live") {
		t.Fatalf("Live off should be saved: %v", got.OffList(got.ActiveProfile()))
	}
	m, _ = m.Update(configEditedMsg{})
	if m.(Model).cfg.On("view.live") {
		t.Error("a reload keeps it off")
	}
	// on again with a click on its row
	m = key(m, "=")
	mm := custGo(t, m, "view.live").(Model)
	m.View()
	m = click(mm, 10, mm.vs.modalY+mm.cust.cur-mm.vs.modalStart)
	if !m.(Model).cfg.On("view.live") {
		t.Error("a click on the row turns it back on")
	}
}

// The Monitor row shows partial when some of its sections are off, and a
// switch on it turns it all on; the top bar and the sidebar lose what is off.
func TestCustomizeTreeAndBars(t *testing.T) {
	var m tea.Model = withAgents(t, 150, 42, demoIdle)
	mm := m.(Model)
	mon := custFind(custTree, "view.monitor")
	if mm.custState(mon) != custSome {
		t.Fatal("the tests turn some Monitor sources off: partial")
	}
	m = custGo(t, key(m, "="), "view.monitor")
	m = key(m, " ")
	if mm := m.(Model); mm.custState(mon) != custAll || !mm.monOn("hermes") {
		t.Error("switching a partial node turns all of it on")
	}
	m = key(m, " ")
	if mm := m.(Model); mm.custState(mon) != custOff || mm.cfg.On("view.monitor") {
		t.Error("and again turns it off")
	}
	m = custGo(t, m, "header.clock")
	m = key(key(m, " "), "esc")
	top := strings.Split(ansi.Strip(m.View()), "\n")[0]
	if strings.Contains(top[max(0, len(top)-8):], ":") {
		t.Errorf("clock still shown: %q", top)
	}
	m = key(key(m, "="), "e") // essentials
	mm = m.(Model)
	if !mm.cfg.On("view.live") || mm.cfg.On("view.skills") || mm.cfg.On("sidebar.pr") || mm.cfg.On("sidebar.agents") {
		t.Errorf("essentials: %v", mm.cfg.OffList(mm.cfg.ActiveProfile()))
	}
	m = key(m, "a") // everything
	if mm = m.(Model); len(mm.cfg.OffList(mm.cfg.ActiveProfile())) != 0 {
		t.Errorf("everything on: %v", mm.cfg.OffList(mm.cfg.ActiveProfile()))
	}
	fits(t, m, 150, 42, "customize")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	fits(t, m, 80, 24, "customize narrow")
}

// Customize edits the profile in use: a built-in one is copied into one of
// yours first. tab walks the profiles, N makes one, R renames it, X X deletes it.
func TestCustomizeProfiles(t *testing.T) {
	var m tea.Model = withAgents(t, 150, 42, demoIdle)
	mm := m.(Model)
	mm.cfg.Profile = config.ProfileBasic
	m = custGo(t, key(mm, "="), "view.monitor")
	m = key(m, " ")
	mm = m.(Model)
	if p := mm.cfg.ActiveProfile(); config.BuiltinProfile(p) || !mm.cfg.On("view.monitor") || mm.cfg.On("view.skills") {
		t.Fatalf("the change should land in a copy of Basic, in use: %q", p)
	}
	if len(mm.cfg.OffList(config.ProfileBasic)) == 0 || mm.cfg.OffList(config.ProfileBasic)[0] != config.LayoutDetail {
		t.Error("Basic itself never changes")
	}
	if out := ansi.Strip(mm.View()); !strings.Contains(out, "built in") {
		t.Errorf("the screen says where the change went:\n%s", out)
	}
	m = key(m, "N")
	for _, r := range "tilt" {
		m = key(m, string(r))
	}
	m = key(m, "enter")
	if mm = m.(Model); mm.cfg.ActiveProfile() != "tilt" || !mm.cfg.On("view.monitor") {
		t.Fatalf("N makes a copy of the one in use: %q", mm.cfg.ActiveProfile())
	}
	m = key(m, "R")
	m = key(key(key(key(m, "backspace"), "backspace"), "backspace"), "backspace")
	for _, r := range "dev" {
		m = key(m, string(r))
	}
	if m = key(m, "enter"); m.(Model).cfg.ActiveProfile() != "dev" {
		t.Fatalf("R renames: %q", m.(Model).cfg.ActiveProfile())
	}
	if out := ansi.Strip(m.View()); !strings.Contains(out, "[dev]") || !strings.Contains(out, "Basic") {
		t.Errorf("the profile line lists them, the one in use marked:\n%s", out)
	}
	if got := config.Load(); got.ActiveProfile() != "dev" {
		t.Errorf("saved: %q", got.ActiveProfile())
	}
	m = key(m, "X")
	if m.(Model).cfg.ActiveProfile() != "dev" {
		t.Fatal("one X only asks")
	}
	if m = key(m, "X"); m.(Model).cfg.ActiveProfile() != config.ProfileFull {
		t.Error("X X deletes it and goes back to Full")
	}
	m = key(m, "X")
	if m.(Model).cfg.ActiveProfile() != config.ProfileFull || !strings.Contains(m.(Model).status, "built in") {
		t.Error("built-in profiles can't be deleted")
	}
	if m = key(m, "tab"); m.(Model).cfg.ActiveProfile() == config.ProfileFull {
		t.Error("tab walks the profiles")
	}
	fits(t, m, 150, 42, "customize profiles")
}

// The quiet profile word at the end of the top bar: a click lists the
// profiles, picking one puts it on; the wheel over it and M walk them; it
// fits a small terminal.
func TestProfileChip(t *testing.T) {
	for _, size := range [][2]int{{150, 42}, {80, 24}} {
		var m tea.Model = boot(t, size[0], size[1])
		chip := func(m tea.Model) (int, int) {
			m.View()
			for _, h := range m.(Model).vs.keyHits {
				if h.key == profileChipKey {
					return h.x0, h.y
				}
			}
			t.Fatalf("%dx%d: no profile word on the top bar:\n%s", size[0], size[1], ansi.Strip(m.View()))
			return 0, 0
		}
		x, y := chip(m)
		if top := ansi.Strip(strings.Split(m.View(), "\n")[0]); !strings.HasSuffix(strings.TrimRight(top, " "), "mine") {
			t.Errorf("the word ends the top bar: %q", top)
		}
		m = click(m, x, y)
		mn := m.(Model).menu
		if mn == nil {
			t.Fatal("a click lists the profiles")
		}
		for i, it := range mn.items {
			if strings.Contains(it.label, "Basic") {
				mm := m.(Model)
				mm.menu.cur = i
				m = key(mm, "enter")
			}
		}
		if m.(Model).cfg.ActiveProfile() != config.ProfileBasic {
			t.Fatalf("picking Basic puts it on: %q", m.(Model).cfg.ActiveProfile())
		}
		fits(t, m, size[0], size[1], "basic")
		x, y = chip(m)
		m = mouseAt(m, x, y, tea.MouseButtonWheelDown, tea.MouseActionPress)
		if m.(Model).cfg.ActiveProfile() != config.ProfileFull {
			t.Errorf("the wheel walks them: %q", m.(Model).cfg.ActiveProfile())
		}
		if m = key(m, "M"); m.(Model).cfg.ActiveProfile() != "mine" {
			t.Errorf("M walks them: %q", m.(Model).cfg.ActiveProfile())
		}
		fits(t, m, size[0], size[1], "profile chip")
	}
}
