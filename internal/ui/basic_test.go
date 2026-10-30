package ui

import (
	"github.com/secuone/nanny/internal/sources"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/secuone/nanny/internal/config"
)

// M walks the profiles: from yours to Basic, the bare minimum — Live and
// Sessions, one line per agent, no subagent rows, no detail pane, the
// Monitor not even collected — and on to Full.
func TestBasicProfile(t *testing.T) {
	m := key(boot(t, 150, 42), "M")
	mm := m.(Model)
	if mm.cfg.ActiveProfile() != config.ProfileBasic || !mm.basic() {
		t.Fatalf("M should put Basic on, got %q", mm.cfg.ActiveProfile())
	}
	for _, r := range mm.live.rows {
		if r.sub != nil {
			t.Fatal("basic lists no subagent rows")
		}
	}
	fits(t, m, 150, 42, "basic")
	out := ansi.Strip(viewOf(m))
	lines := strings.Split(out, "\n")
	for _, w := range []string{"1 Live", "2 Sessions", "Partial refunds", "↳2", "Basic"} {
		if !strings.Contains(out, w) {
			t.Errorf("basic lacks %q:\n%s", w, out)
		}
	}
	for _, w := range []string{"Settings", "Git", "Jira", "Insights", "Monitor", "subs", "Prompts"} {
		if strings.Contains(lines[0]+lines[viewY], w) {
			t.Errorf("basic header/views bar should not show %q", w)
		}
	}
	if m.(Model).cfg.On("monitor.tilt") && m.(Model).cfg.On("view.monitor") {
		t.Error("basic collects nothing of the Monitor")
	}
	if m = key(m, "8"); !strings.Contains(ansi.Strip(viewOf(m)), "8 Settings") {
		t.Error("Settings is always reachable by number")
	}
	if m = key(m, "M"); m.(Model).cfg.ActiveProfile() != config.ProfileFull || m.(Model).basic() {
		t.Error("M again should put Full on")
	}
	small := key(boot(t, 80, 24), "M")
	fits(t, small, 80, 24, "basic small")
	if out := ansi.Strip(viewOf(small)); !strings.Contains(out, "Partial refunds") {
		t.Errorf("basic at 80x24 still lists the agents:\n%s", out)
	}
}

// Without Extras nothing shows twice: no sidebar agents while Live is on,
// all of them elsewhere, short footers; with Extras it all shows.
func TestLayoutExtras(t *testing.T) {
	m := withAgents(t, 150, 42, demoIdle)
	if m.full() || m.basic() {
		t.Fatal("the tests' profile has Detail on and Extras off")
	}
	live := ansi.Strip(viewOf(key(m, "1")))
	if strings.Contains(live, "AGENTS") {
		t.Error("no extras: the sidebar leaves agents to Live")
	}
	sess := ansi.Strip(viewOf(key(m, "2")))
	if !strings.Contains(sess, "AGENTS") || !strings.Contains(sess, "Partial refunds") || !strings.Contains(sess, "Migrate checkout") {
		t.Errorf("no extras: the sidebar lists the agents off Live:\n%s", sess)
	}
	lines := strings.Split(live, "\n")
	if l := lines[viewY]; !strings.Contains(l, "│ 5 Prompts") || strings.Index(l, "7 Monitor") < strings.Index(l, "5 Prompts") {
		t.Errorf("the views of the space left, the global ones apart on the right: %q", l)
	}
	m.cfg.SetOn(config.LayoutExtras, true)
	if full := ansi.Strip(viewOf(key(m, "1"))); !strings.Contains(full, "AGENTS") || !strings.Contains(full, "opus-5-5") {
		t.Error("extras: everything, sidebar agents and model included")
	}
}

// Settings → Screen: the Customize row says how much is off and opens it.
func TestScreenSettings(t *testing.T) {
	var m tea.Model = key(withAgents(t, 150, 42, demoIdle), "8")
	if !strings.Contains(ansi.Strip(viewOf(m)), "things off") {
		t.Error("the Customize row should say how much is off")
	}
	for i, it := range settingsList {
		if it.name == customizeRow {
			mm := m.(Model)
			mm.settings.cur.idx = i
			m = key(mm, "enter")
		}
	}
	if m.(Model).cust == nil {
		t.Fatal("⏎ on Customize should open it")
	}
}

// Settings scrolls to the selected one, and a click picks what is under it.
func TestSettingsScrolls(t *testing.T) {
	var m tea.Model = key(boot(t, 100, 30), "8")
	for range settingsList {
		m = key(m, "j")
	}
	lastName := settingsList[len(settingsList)-1].name
	out := ansi.Strip(viewOf(m))
	if !strings.Contains(out, lastName) {
		t.Fatalf("the last setting should scroll into view:\n%s", out)
	}
	mm := m.(Model)
	for y, idx := range mm.vs.listRows {
		if idx == 2 {
			m = click(m, 40, mm.vs.listY0+y)
			if got := m.(Model).settings.cur.idx; got != 2 {
				t.Errorf("click picked %d", got)
			}
		}
	}
}

// A space opened on your home shows everything (and says so) until its panes
// are in a repo; then its views narrow to that repo. Global views ignore it.
func TestHomeSpaceNarrowsToItsRepos(t *testing.T) {
	m := withAgents(t, 150, 42, demoIdle)
	sp, _ := m.spaceFor(home, true)
	m.cur = m.indexOf(sp)
	var mm tea.Model = key(m, "i")
	mm = key(mm, "1")
	out := ansi.Strip(viewOf(mm))
	if !strings.Contains(out, "all of ~") || !strings.Contains(out, "Partial refunds") {
		t.Fatalf("a home space shows everything and says so:\n%s", out)
	}
	sp.Focus = []string{filepath.Join(home, "dev", "web-checkout")}
	mv := mm.(Model)
	mv.rebuild()
	out = ansi.Strip(viewOf(mv))
	if strings.Contains(out, "Partial refunds for split") || !strings.Contains(out, "Migrate checkout") || !strings.Contains(out, "only web-checkout") {
		t.Errorf("narrowed to the repo its shell is in:\n%s", out)
	}
}

// The ≡ menu (m, the logo, or the sidebar button) holds settings, the
// screen profiles and Customize.
func TestAppMenu(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var m tea.Model = withAgents(t, 150, 42, demoIdle)
	if bar := strings.Split(ansi.Strip(viewOf(m)), "\n")[viewY]; strings.Contains(bar, "Settings") || strings.Contains(bar, "Nursery") {
		t.Errorf("Settings belongs in the menu now, and the Nursery is gone: %q", bar)
	}
	if out := ansi.Strip(viewOf(m)); !strings.Contains(out, "≡ menu") {
		t.Error("the sidebar has no ≡ menu button")
	}
	m = key(m, "m")
	if m.(Model).menu == nil {
		t.Fatal("m should open the menu")
	}
	m = key(key(m, "M"), "2") // screen profile → Full
	if !m.(Model).full() || m.(Model).cfg.ActiveProfile() != config.ProfileFull {
		t.Fatal("the profile submenu should pick Full")
	}
	if m = key(key(m, "m"), "="); m.(Model).cust == nil {
		t.Fatal("the menu should open Customize")
	}
	m = key(key(key(m, "esc"), "m"), "8")
	if !m.(Model).onTab(tabSettings) {
		t.Error("the menu should open Settings")
	}
}

// A count on top of the Monitor takes you somewhere: a state to its next
// item, ports and containers to their section.
func TestMonitorCountsJump(t *testing.T) {
	m := boot(t, 150, 42).(Model)
	m.cfg.SetOff(nil) // every section on
	m.radar.rebuild([]sources.Item{
		{Group: "Open ports", Name: ":8080", Status: sources.Running},
		{Group: "Claude background jobs", Name: "fix flaky test", Status: sources.Blocked, SessionID: "x"},
		{Group: "Containers", Name: "db", Status: sources.Running},
	})
	m.monJump(string(sources.Blocked))
	if it := m.radar.sel(); it == nil || it.Name != "fix flaky test" {
		t.Fatalf("need you should select the blocked job, got %+v", it)
	}
	m.monJump("Containers")
	if monSecs[m.radar.sec].name != "Containers" {
		t.Errorf("containers should open their section, got %s", monSecs[m.radar.sec].name)
	}
	m.monJump(string(sources.Blocked)) // not in this section: back to All
	if it := m.radar.sel(); m.radar.sec != secAll || it == nil || it.Status != sources.Blocked {
		t.Errorf("need you from another section should go back to All and pick it")
	}
}
