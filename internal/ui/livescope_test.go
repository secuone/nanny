package ui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/secuone/nanny/internal/claude"
	"github.com/secuone/nanny/internal/gitwt"
	"github.com/secuone/nanny/internal/ws"
)

// twoSpaces is the demo with a space on acme-api and another on web-checkout,
// each with one live agent.
func twoSpaces(t *testing.T) Model {
	t.Helper()
	m := boot(t, 150, 42).(Model)
	m.spaces = append(m.spaces,
		ws.NewSpace("acme-api", filepath.Join(home, "dev", "acme-api")),
		ws.NewSpace("web-checkout", filepath.Join(home, "dev", "web-checkout")))
	m.cur = -1
	return m
}

func liveCwds(m Model) []string {
	var out []string
	for _, r := range m.live.rows {
		if r.agent() {
			out = append(out, filepath.Base(r.cwd()))
		}
	}
	return out
}

func wantLive(t *testing.T, m Model, what string, want ...string) {
	t.Helper()
	got := liveCwds(m)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("%s: live agents %v, want %v", what, got, want)
	}
	if n := countAgents(m.live.rows); n != len(want) {
		t.Errorf("%s: counted %d agents, want %d", what, n, len(want))
	}
}

func upd(m tea.Model) Model {
	m, _ = m.Update(tea.WindowSizeMsg{Width: 150, Height: 42})
	return m.(Model)
}

func TestLiveShowsOnlyTheSelectedSpace(t *testing.T) {
	m := twoSpaces(t)
	wantLive(t, m, "home", "PAY-412", "web-checkout")

	// the 1 key from another view of the space
	a := m
	a.cur, a.dash, a.tab = 0, true, tabSessions
	a = key(upd(a), "1").(Model)
	wantLive(t, a, "1 key", "PAY-412")
	if out := ansi.Strip(viewOf(a)); !strings.Contains(out, "Live agents · 1 · only acme-api") {
		t.Errorf("title does not carry the scope:\n%s", out)
	}

	// toLive from a sidebar or hover action
	b := m
	b.cur = 1
	b.toLive()
	wantLive(t, upd(b), "toLive", "web-checkout")

	// the space's own terminals, then the views again
	c := m
	c.cur = 0
	c = upd(c)
	c.dash = false
	c = upd(c)
	c.toLive()
	wantLive(t, upd(c), "back to the views", "PAY-412")

	// a global view in the space and back
	d := m
	d.cur, d.dash, d.tab = 1, true, tabSettings
	d = upd(d)
	d = key(d, "1").(Model)
	wantLive(t, d, "after Settings", "web-checkout")

	// home and into a space again
	e := m
	e.cur, e.tab = 0, tabLive
	e = upd(e)
	wantLive(t, e, "entering", "PAY-412")
	e.cur = -1
	wantLive(t, upd(e), "home again", "PAY-412", "web-checkout")
	e.cur = 1
	wantLive(t, upd(e), "other space", "web-checkout")
}

func TestLiveSpaceFollowsClicksOnTheSidebar(t *testing.T) {
	m := twoSpaces(t)
	m.tab = tabLive
	m = upd(m)
	_ = viewOf(m)
	for i := 0; i < 2; i++ {
		var row *sideRow
		for _, h := range m.vs.sideRows {
			if h.isSpace && h.space == i {
				h := h
				row = &h
			}
		}
		if row == nil {
			t.Fatalf("no sidebar row for space %d", i)
		}
		mm := click(m, 5, row.y).(Model)
		mm.toLive()
		wantLive(t, upd(mm), "click on "+mm.space().Name, map[int]string{0: "PAY-412", 1: "web-checkout"}[i])
	}
}

func TestLiveEmptyStateNamesTheSpace(t *testing.T) {
	m := twoSpaces(t)
	m.spaces = append(m.spaces, ws.NewSpace("quiet", filepath.Join(home, "dev", "quiet")))
	m.cur, m.dash, m.tab = 2, true, tabLive
	m = upd(m)
	wantLive(t, m, "quiet space")
	out := ansi.Strip(viewOf(m))
	for _, want := range []string{"No agents in quiet", "see agents from all spaces"} {
		if !strings.Contains(out, want) {
			t.Errorf("empty state lacks %q:\n%s", want, out)
		}
	}
	for y, l := range strings.Split(viewOf(m), "\n") {
		if x := strings.Index(ansi.Strip(l), "see agents from all spaces"); x >= 0 {
			m = click(m, x+2, y).(Model)
			break
		}
	}
	if m.inSpace() {
		t.Error("the hint did not go to all spaces")
	}
	wantLive(t, upd(m), "after the hint", "PAY-412", "web-checkout")
}

func TestSpaceHasWorktreesAndPanes(t *testing.T) {
	m := twoSpaces(t)
	sp := m.spaces[0]
	wt := filepath.Join(home, "dev", "acme-api.worktrees", "PAY-9")
	m.spaceWT = spaceWTMsg{spaceWhere(sp): {{Path: wt, Branch: "PAY-9"}, {Path: filepath.Join(home, "dev", "elsewhere"), Branch: "x", Missing: true}}}
	has := m.spaceHas(sp)
	row := func(cwd string) liveRow { return liveRow{sess: &claude.Session{Cwd: cwd}} }
	for cwd, want := range map[string]bool{
		filepath.Join(home, "dev", "acme-api"):                              true,
		filepath.Join(home, "dev", "acme-api", "src"):                       true,
		filepath.Join(home, "dev", "acme-api", ".claude", "worktrees", "A"): true,
		filepath.Join(wt, "src"):                                            true,
		filepath.Join(home, "dev", "acme-api-other"):                        false,
		filepath.Join(home, "dev", "elsewhere"):                             false,
		filepath.Join(home, "dev", "web-checkout"):                          false,
		"": false,
	} {
		if has(row(cwd)) != want {
			t.Errorf("cwd %q: in space = %v, want %v", cwd, !want, want)
		}
	}
	if !has(liveRow{sess: &claude.Session{Cwd: "/tmp/x"}, local: &paneRef{space: sp}}) {
		t.Error("an agent in the space's own pane belongs to it wherever it went")
	}
	_ = gitwt.Worktree{}
}

// blockedAway is a model with an agent waiting on you in one space (OPS-88)
// and you in another, web-checkout.
func blockedAway(t *testing.T) Model {
	t.Helper()
	m := withAgents(t, 150, 42, demoBlocked)
	m = settle(t, m, func(m Model) bool { return blockedRow(m) >= 0 })
	m.spaces = append(m.spaces, ws.NewSpace("web-checkout", filepath.Join(home, "dev", "web-checkout")))
	m.cur = len(m.spaces) - 1
	m.dash, m.tab = true, tabSessions
	return upd(m)
}

func TestSidebarHintsAtAgentsWaitingElsewhere(t *testing.T) {
	m := blockedAway(t)
	if n := m.waitingElsewhere(); n != 1 {
		t.Fatalf("waiting elsewhere = %d, want 1", n)
	}
	out := ansi.Strip(viewOf(m))
	if !strings.Contains(out, "1 waiting elsewhere") {
		t.Errorf("sidebar lacks the hint:\n%s", out)
	}
	if !strings.Contains(out, "+1 waiting elsewhere") {
		t.Errorf("header lacks the chip:\n%s", out)
	}
	for _, r := range m.live.rows {
		if r.state() == "blocked" {
			t.Error("a blocked agent of another space is listed in Live")
		}
	}
	for y, l := range strings.Split(viewOf(m), "\n") {
		if x := strings.Index(ansi.Strip(l), "1 waiting elsewhere"); x >= 0 && x < sidebarW {
			mm := click(m, x+2, y).(Model)
			if mm.inSpace() {
				t.Error("clicking the hint stays in the space")
			}
			if blockedRow(upd(mm)) < 0 {
				t.Error("all spaces does not list the blocked agent")
			}
			return
		}
	}
	t.Error("hint line not found")
}

func TestBangJumpsToABlockedAgentOfAnotherSpace(t *testing.T) {
	m := blockedAway(t)
	m = key(m, "!").(Model)
	if m.inSpace() || m.scopeSpace != nil {
		t.Fatalf("! stays scoped: cur=%d", m.cur)
	}
	if r := m.live.sel(); r == nil || r.state() != "blocked" {
		t.Errorf("! landed on %v, want the blocked agent", r)
	}
}

func TestSidebarAgentsFollowTheSpace(t *testing.T) {
	m := twoSpaces(t)
	all, _ := m.sideAgents()
	if len(all) != 2 {
		t.Fatalf("home lists %d agents, want 2", len(all))
	}
	m.cur = 0
	in, quiet := m.sideAgents()
	if len(in)+len(quiet) != 1 || filepath.Base(in[0].cwd()) != "PAY-412" {
		t.Errorf("space lists %d+%d agents, want only PAY-412", len(in), len(quiet))
	}
	m.cur = 1
	in, quiet = m.sideAgents()
	if len(in)+len(quiet) != 1 || filepath.Base(in[0].cwd()) != "web-checkout" {
		t.Errorf("other space lists %d+%d agents, want only web-checkout", len(in), len(quiet))
	}
}
