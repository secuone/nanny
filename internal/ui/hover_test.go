package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/secuone/nanny/internal/gitwt"
	"github.com/secuone/nanny/internal/sources"
)

func mouseAt(m tea.Model, x, y int, b tea.MouseButton, a tea.MouseAction) tea.Model {
	m, c := m.Update(tea.MouseMsg{X: x, Y: y, Button: b, Action: a})
	return drain(m, c, 0)
}

func termHit(t *testing.T, m Model, idx int) hit {
	t.Helper()
	for _, h := range m.vs.termHits {
		if h.idx == idx {
			return h
		}
	}
	t.Fatalf("no terminal tab %d on screen", idx)
	return hit{}
}

// Hovering a terminal tab shows what runs in it; moving off hides it.
func TestHoverTermTab(t *testing.T) {
	m := withAgents(t, 150, 42, demoIdle)
	m.cur = len(m.spaces) - 1
	m.layoutPanes()
	viewOf(m)
	h := termHit(t, m, 0)
	mm := mouseAt(m, h.x0+1, termY, tea.MouseButtonNone, tea.MouseActionMotion)
	out := ansi.Strip(viewOf(mm))
	if !strings.Contains(out, "right-click menu") || !strings.Contains(out, tilde(m.space().Tabs[0].Pane().Cwd)) {
		t.Fatalf("hovering a tab should show its panes:\n%s", out)
	}
	fits(t, mm, 150, 42, "tooltip")
	mm = mouseAt(mm, 100, 20, tea.MouseButtonNone, tea.MouseActionMotion)
	if strings.Contains(ansi.Strip(viewOf(mm)), "right-click menu") {
		t.Error("moving away should hide the tooltip")
	}
	// the views bar explains each view
	for _, th := range mm.(Model).vs.tabHits {
		if th.idx == int(tabRadar) {
			mm = mouseAt(mm, th.x0+1, viewY, tea.MouseButtonNone, tea.MouseActionMotion)
		}
	}
	if !strings.Contains(ansi.Strip(viewOf(mm)), "open ports") {
		t.Error("hovering 7 Radar should say what it shows")
	}
}

// Right-click on a terminal tab opens its menu; the menu renames it.
func TestTermTabMenu(t *testing.T) {
	m := withAgents(t, 150, 42, demoIdle)
	m.cur = len(m.spaces) - 1
	m.layoutPanes()
	viewOf(m)
	h := termHit(t, m, 0)
	var mm tea.Model = mouseAt(m, h.x0+1, termY, tea.MouseButtonRight, tea.MouseActionPress)
	if mm.(Model).menu == nil {
		t.Fatal("right-click on a tab should open its menu")
	}
	out := ansi.Strip(viewOf(mm))
	for _, want := range []string{"Rename…", "Split right", "Close tab"} {
		if !strings.Contains(out, want) {
			t.Errorf("tab menu lacks %q:\n%s", want, out)
		}
	}
	fits(t, mm, 150, 42, "tab menu")
	mm = key(mm, ",")
	if mm.(Model).modal == nil || mm.(Model).modal.input.Value() != m.space().Tabs[0].Label {
		t.Fatal(", in the menu should ask for the name, prefilled")
	}
	mm.(Model).modal.input.SetValue("api")
	mm = key(mm, "enter")
	if got := mm.(Model).space().Tabs[0].Label; got != "api" {
		t.Errorf("tab label = %q, want api", got)
	}
	// a click outside closes a menu
	mm = mouseAt(mm, h.x0+1, termY, tea.MouseButtonRight, tea.MouseActionPress)
	viewOf(mm)
	if mm = mouseAt(mm, 140, 30, tea.MouseButtonLeft, tea.MouseActionPress); mm.(Model).menu != nil {
		t.Error("clicking elsewhere should close the menu")
	}
}

// Dragging a terminal tab onto another moves it there.
func TestDragTab(t *testing.T) {
	m := withAgents(t, 150, 42, demoIdle)
	m.cur = len(m.spaces) - 1
	sp := m.space()
	if len(sp.Tabs) < 2 {
		m.openTabIn(sp, "shell", nil)
	}
	m.layoutPanes()
	viewOf(m)
	first := sp.Tabs[0]
	a, b := termHit(t, m, 0), termHit(t, m, 1)
	var mm tea.Model = mouseAt(m, a.x0+1, termY, tea.MouseButtonLeft, tea.MouseActionPress)
	mm = mouseAt(mm, b.x0+1, termY, tea.MouseButtonLeft, tea.MouseActionMotion)
	mm = mouseAt(mm, b.x0+1, termY, tea.MouseButtonLeft, tea.MouseActionRelease)
	if sp.Tabs[1] != first || sp.Active != 1 {
		t.Error("the dragged tab should land where it was dropped, still active")
	}
}

// A space gets an icon from its right-click menu, and keeps it on disk.
func TestSpaceIcon(t *testing.T) {
	m := withAgents(t, 150, 42, demoIdle)
	viewOf(m)
	i := len(m.spaces) - 1
	var x int
	for _, h := range m.vs.spaceHits {
		if h.idx == i {
			x = h.x0 + 1
		}
	}
	var mm tea.Model = mouseAt(m, x, 0, tea.MouseButtonRight, tea.MouseActionPress)
	viewOf(mm)
	mm = key(mm, "e") // Icon…
	if mm.(Model).menu == nil || mm.(Model).menu.cols != 8 {
		t.Fatal("Icon… should open the icon grid")
	}
	out := viewOf(mm)
	fits(t, mm, 150, 42, "icon picker")
	if !strings.Contains(out, "🚀") {
		t.Error("the grid should show icons")
	}
	mm = key(mm, "right") // 🔥
	mm = key(mm, "enter")
	sp := mm.(Model).spaces[i]
	if sp.Icon != "🔥" {
		t.Fatalf("icon = %q", sp.Icon)
	}
	if !strings.Contains(ansi.Strip(viewOf(mm)), "🔥 "+sp.Name) {
		t.Error("the icon should show before the space name")
	}
}

// The first start shows the guide; closing it remembers.
func TestWelcomeGuide(t *testing.T) {
	t.Setenv("NANNY_NO_WELCOME", "")
	m := bootEmpty(t, 80, 24)
	if m.(Model).welcome == nil {
		t.Fatal("the first start should show the welcome guide")
	}
	for p := 0; p < welcomePages; p++ {
		fits(t, m, 80, 24, "welcome page")
		if p == 1 && !strings.Contains(ansi.Strip(viewOf(m)), "Monitor") {
			t.Error("page 2 should list the views")
		}
		m = key(m, "right")
	}
	if m.(Model).welcome != nil {
		t.Fatal("→ on the last page should close the guide")
	}
	if firstRun() {
		t.Error("closing the guide should remember it")
	}
	if m.(Model).setup == nil {
		t.Fatal("closing the guide should open the setup wizard")
	}
	m = key(m, "esc")
	m = key(m, "ctrl+k")
	m = key(m, "welcome guide")
	if m = key(m, "enter"); m.(Model).welcome == nil {
		t.Error("ctrl+k → Welcome guide should open it again")
	}
}

// The Monitor splits what it sees into sections; one turned off isn't there.
func TestMonitorSections(t *testing.T) {
	b := key(boot(t, 150, 42), "7").(Model)
	b.cfg.SetOff([]string{"monitor.hermes"})
	m, _ := b.Update(sourcesMsg{
		{Group: "Open ports", Name: ":8000  php", Status: sources.Running, URL: "http://localhost:8000"},
		{Group: "Containers", Name: "db", Status: sources.Running},
		{Group: "Deploys", Name: "my watcher", Status: sources.Idle},
	})
	out := ansi.Strip(viewOf(m))
	for _, want := range []string{"All 3", "Ports 1", "Containers 1", "Watchers 1"} {
		if !strings.Contains(out, want) {
			t.Errorf("sections bar lacks %q:\n%s", want, out)
		}
	}
	m = key(m, "right")
	m = key(m, "right") // Ports
	if out := ansi.Strip(viewOf(m)); !strings.Contains(out, ":8000  php") || strings.Contains(out, "my watcher") {
		t.Errorf("Ports should list only ports:\n%s", out)
	}
	if bar := strings.Split(ansi.Strip(viewOf(m)), "\n")[secY]; strings.Contains(bar, "Hermes") {
		t.Errorf("Hermes is off: not on the sections bar: %q", bar)
	}
	for i := 0; i < 5; i++ { // Containers, Tilt, Jobs, Cron, then past Hermes to Watchers
		m = key(m, "right")
	}
	if got := m.(Model).radar.sec; got != secWatchers {
		t.Errorf("→ should skip Hermes, at %d", got)
	}
	fits(t, m, 150, 42, "monitor")
	for _, size := range [][2]int{{100, 30}, {80, 24}} {
		mm, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		fits(t, mm, size[0], size[1], "narrow monitor")
	}
}

// Enter on a container offers what can be done with it.
func TestContainerMenu(t *testing.T) {
	b := key(boot(t, 150, 42), "7").(Model)
	b.cfg.SetOff(nil) // every section on
	m, _ := b.Update(sourcesMsg{
		{Group: "Containers", Name: "db", Status: sources.Running, Kind: "container", Ref: "db", URL: "http://localhost:5432"},
		{Group: "Tilt", Name: "api", Status: sources.Failed, Kind: "tilt", Ref: "api"},
	})
	m = key(key(key(key(m, "right"), "right"), "right"), "j") // Containers
	viewOf(m)
	if m = key(m, "enter"); m.(Model).menu == nil {
		t.Fatal("enter on a container should open its actions")
	}
	out := ansi.Strip(viewOf(m))
	for _, want := range []string{"Logs (read-only)", "Shell inside", "Restart", "Stop"} {
		if !strings.Contains(out, want) {
			t.Errorf("container menu lacks %q:\n%s", want, out)
		}
	}
	fits(t, m, 150, 42, "container menu")
	m = key(key(key(m, "esc"), "right"), "j") // Tilt
	viewOf(m)
	if m = key(m, "enter"); !strings.Contains(ansi.Strip(viewOf(m)), "Rebuild (trigger)") {
		t.Error("a Tilt resource should offer a rebuild")
	}
}

// A log opens read-only: typing does nothing, s opens a shell beside it, q closes it.
func TestReadOnlyLog(t *testing.T) {
	m := boot(t, 120, 36).(Model)
	m.watch("log", "sleep 5", "sh")
	t.Cleanup(func() {
		for _, r := range m.allPanes() {
			r.pane.Close()
		}
	})
	tb := m.space().Tab()
	if !tb.Pane().ReadOnly || !strings.Contains(ansi.Strip(viewOf(m)), "read-only") {
		t.Fatal("a log pane should be read-only and say so")
	}
	var mm tea.Model = key(m, "s")
	if len(tb.Panes) != 2 || tb.Pane().ReadOnly {
		t.Fatal("s should open a shell next to the log")
	}
	tb.Focus = 0
	mm = key(mm, "q")
	if len(tb.Panes) != 1 || tb.Pane().ReadOnly {
		t.Error("q should close the log, leaving the shell")
	}
}

// The counts on top open Live with only those agents; again shows them all.
func TestHeaderCountFilters(t *testing.T) {
	var m tea.Model = withAgents(t, 150, 42, demoIdle)
	viewOf(m)
	var h *keyHit
	for _, k := range m.(Model).vs.keyHits {
		if k.key == "@only:idle" {
			h = &k
		}
	}
	if h == nil {
		t.Fatal("the idle count should be clickable")
	}
	m = click(m, h.x0, h.y)
	if m.(Model).live.only != "idle" || !strings.Contains(ansi.Strip(viewOf(m)), "only idle") {
		t.Fatal("clicking idle should list only idle agents")
	}
	for _, r := range m.(Model).live.rows {
		if r.sub == nil && r.state() != "idle" {
			t.Errorf("a %s agent is listed under idle", r.state())
		}
	}
	if m = key(m, "esc"); m.(Model).live.only != "" {
		t.Error("esc should show every agent again")
	}
}

// The + buttons and the Monitor's sections light up under the pointer.
func TestHoverButtonsAndSections(t *testing.T) {
	m := withAgents(t, 150, 42, demoIdle)
	m.cur = len(m.spaces) - 1
	m.layoutPanes()
	viewOf(m)
	h := termHit(t, m, -2)
	mm := mouseAt(m, h.x0+1, termY, tea.MouseButtonNone, tea.MouseActionMotion)
	if got := mm.(Model).vs.hover; got.kind != "term" || got.idx != -2 {
		t.Errorf("hovering + claude: %+v", got)
	}
	fits(t, mm, 150, 42, "hover on + claude")
	mb := boot(t, 150, 42).(Model)
	mb.cfg.SetOff(nil) // every section on
	mon := key(mb, "7")
	viewOf(mon)
	sh := mon.(Model).vs.secHits[2]
	mon = mouseAt(mon, sh.x0+1, secY, tea.MouseButtonNone, tea.MouseActionMotion)
	if got := mon.(Model).vs.hover; got.kind != "sec" || got.idx != 2 {
		t.Errorf("hovering a section: %+v", got)
	}
	if lines := strings.Split(ansi.Strip(viewOf(mon)), "\n"); strings.Contains(lines[2], "Ports") || !strings.Contains(lines[secY], "Ports") {
		t.Errorf("the sections bar should sit one row under the views bar:\n%s\n%s", lines[2], lines[secY])
	}
}

// n in Skills asks what and where, then the name.
func TestNewSkillAsksWhere(t *testing.T) {
	m := withAgents(t, 150, 42, demoIdle)
	var mm tea.Model = key(m, "6")
	mm = key(mm, "n")
	if mm.(Model).menu == nil || !strings.Contains(ansi.Strip(viewOf(mm)), "Command") {
		t.Fatal("n should ask what to create")
	}
	mm = key(mm, "c")
	out := ansi.Strip(viewOf(mm))
	if !strings.Contains(out, "Global") || !strings.Contains(out, tilde(m.spaces[0].Dir())) {
		t.Fatalf("then where, global or a space's repo:\n%s", out)
	}
	fits(t, mm, 150, 42, "where menu")
	if mm = key(mm, "1"); mm.(Model).modal == nil {
		t.Error("then its name")
	}
}

// A space shows its branch and worktrees; its menu makes a new worktree,
// which opens Claude in the same space.
func TestSpaceWorktrees(t *testing.T) {
	m := withAgents(t, 150, 42, demoIdle)
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"-c", "user.email=a@b", "-c", "user.name=a", "commit", "-q", "--allow-empty", "-m", "x"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Skip("git: ", string(out))
		}
	}
	sp, _ := m.spaceFor(dir, true)
	m.cur = m.indexOf(sp)
	bin := t.TempDir()
	os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\nsleep 5\n"), 0o755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Cleanup(func() {
		for _, r := range m.allPanes() {
			r.pane.Close()
		}
	})
	m.newWorktree(sp)
	m.modal.input.SetValue("feat-x")
	var mm tea.Model = key(m, "enter")
	if n := len(sp.Tabs); n == 0 || sp.Tab().Label != "feat-x" {
		t.Fatal("a new worktree should open Claude in a tab of the space")
	}
	mv := mm.(Model)
	mm = drain(mm, mv.spaceWTCmd(), 0)
	out := ansi.Strip(viewOf(mm))
	// the space follows its active tab, now in the worktree: the main checkout is the other one
	if !strings.Contains(out, "⎇ feat-x") || !strings.Contains(out, "▸ 1 worktree") || strings.Contains(out, "├ main") {
		t.Fatalf("the sidebar should show the branch and the worktrees folded:\n%s", out)
	}
}

// t in Skills reaches the harness: hooks, MCP servers, plugins, memory.
func TestSkillsHarness(t *testing.T) {
	var m tea.Model = key(boot(t, 150, 42), "6")
	m, _ = m.Update(skillsMsg{
		{Kind: "hook", Name: "Stop", Description: "notify", Scope: "user", Path: "/x/settings.json", Detail: []string{"event: Stop", "command: notify"}},
		{Kind: "mcp", Name: "github", Description: "stdio · npx gh", Scope: "user", Path: "/x/.claude.json"},
		{Kind: "skill", Name: "tdd", Scope: "user", Path: "/x/skills/tdd/SKILL.md"},
	})
	for i := 0; i < 4; i++ {
		m = key(m, "t")
	}
	out := ansi.Strip(viewOf(m))
	if !strings.Contains(out, "Hooks") || !strings.Contains(out, "MCP servers") || strings.Contains(out, "tdd") {
		t.Fatalf("the harness filter should list hooks and MCP servers only:\n%s", out)
	}
	if !strings.Contains(out, "command") || !strings.Contains(out, "notify") {
		t.Error("the detail should say what the hook runs")
	}
	fits(t, m, 150, 42, "harness")
}

// Worktrees are folded under the branch, "▸ N worktrees" opens them all and
// closes them again; the agents start in the bottom half of the sidebar.
func TestSidebarWorktreesExpand(t *testing.T) {
	m := withAgents(t, 150, 42, demoIdle)
	sp, _ := m.spaceFor(t.TempDir(), true)
	m.cur = m.indexOf(sp)
	m.spaceWT = spaceWTMsg{sp.Dir(): nil}
	for i := 0; i < 12; i++ {
		m.spaceWT[sp.Dir()] = append(m.spaceWT[sp.Dir()], gitwt.Worktree{Path: fmt.Sprintf("/wt/%02d", i), Branch: fmt.Sprintf("feat-%02d", i)})
	}
	find := func(mm tea.Model, s string) int {
		for y, l := range strings.Split(ansi.Strip(viewOf(mm)), "\n") {
			if strings.Contains(l[:min(len(l), 40)], s) {
				return y
			}
		}
		return -1
	}
	var mm tea.Model = m
	if find(mm, "feat-00") >= 0 || find(mm, "▸ 12 worktrees") < 0 {
		t.Fatalf("folded by default, even in the space you are in:\n%s", ansi.Strip(viewOf(mm)))
	}
	mm = click(mm, 8, find(mm, "▸ 12 worktrees"))
	if find(mm, "feat-00") < 0 || find(mm, "▾ 12 worktrees") < 0 {
		t.Fatalf("a click lists them all:\n%s", ansi.Strip(viewOf(mm)))
	}
	if y := find(mm, "AGENTS"); y < bodyY+(42-bodyY-1-2)/2 {
		t.Errorf("the agents should start in the bottom half, at row %d", y)
	}
	mm = click(mm, 8, find(mm, "▾ 12 worktrees"))
	if find(mm, "feat-00") >= 0 {
		t.Fatalf("a second click folds them:\n%s", ansi.Strip(viewOf(mm)))
	}
}

// The branch shows its state (●changed ↑push ↓pull) and its open PR, in the
// sidebar, on the agent's Live row and in its detail.
func TestBranchStateAndPR(t *testing.T) {
	m := withAgents(t, 150, 42, demoIdle)
	dir := filepath.Join(home, "dev", "web-checkout")
	sp, _ := m.spaceFor(dir, true)
	m.cur = m.indexOf(sp)
	m.dash = true
	m.tab = tabSessions
	m.gitState = gitStateMsg{dir: {root: dir, branch: "main", dirty: 3, ahead: 2, behind: 1}}
	m.prs = prsMsg{dir: {"main": {Number: 96, Title: "Checkout redesign", URL: "https://x/pull/96", Branch: "main", Checks: "fail", Review: "APPROVED"}}}
	m.rebuild()
	out := ansi.Strip(viewOf(m))
	for _, want := range []string{"●3 ↑2 ↓1", "#96✗✔", "Checkout redesign"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	var mm tea.Model = key(m, "1")
	live := ansi.Strip(viewOf(mm))
	if !strings.Contains(live, "#96") || !strings.Contains(live, "checks fail") {
		t.Errorf("the Live row and detail should show the PR:\n%s", live)
	}
}
