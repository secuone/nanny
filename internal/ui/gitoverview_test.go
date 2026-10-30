package ui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/secuone/nanny/internal/gh"
)

// overviewIn is a space on web-checkout with a branch, its state and an open PR.
func overviewIn(t *testing.T, w, h int) Model {
	t.Helper()
	m := withAgents(t, w, h, demoIdle)
	dir := filepath.Join(home, "dev", "web-checkout")
	sp, _ := m.spaceFor(dir, true)
	m.cur = m.indexOf(sp)
	m.layoutPanes()
	m.gitState = gitStateMsg{dir: {root: dir, branch: "main", dirty: 3, ahead: 2, behind: 1}}
	m.prs = prsMsg{dir: {"main": {Number: 96, Title: "Checkout redesign", URL: "https://x/pull/96", Branch: "main", Checks: "pass", Review: "APPROVED"}}}
	m.rebuild()
	return m
}

// colOf is the screen column of sub in line y.
func colOf(t *testing.T, m tea.Model, y int, sub string) int {
	t.Helper()
	line := strings.Split(ansi.Strip(viewOf(m)), "\n")[y]
	i := strings.Index(line, sub)
	if i < 0 {
		t.Fatalf("%q not on row %d: %q", sub, y, line)
	}
	return ansi.StringWidth(line[:i])
}

func wantOverview(t *testing.T, m tea.Model) {
	t.Helper()
	mm := m.(Model)
	if mm.tab != tabGit || !mm.dash || gitOf(m).cur != gsHere {
		t.Fatalf("the Overview should be on screen: tab %v dash %v section %d", mm.tab, mm.dash, gitOf(m).cur)
	}
	out := screen(m)
	for _, want := range []string{"⎇ main", "●3 ↑2 ↓1", "#96", "Checkout redesign"} {
		if !strings.Contains(out, want) {
			t.Errorf("the Overview lacks %q:\n%s", want, out)
		}
	}
}

func TestPrefixGOpensOverview(t *testing.T) {
	useGH(t)
	m := overviewIn(t, 150, 42)
	var mm tea.Model = m
	mm = key(mm, "ctrl+b")
	mm = key(mm, "G")
	wantOverview(t, mm)
	fits(t, mm, 150, 42, "overview")
	fits(t, key(mm, "esc"), 150, 42, "back")
}

func TestClickBranchOpensOverview(t *testing.T) {
	useGH(t)
	m := overviewIn(t, 150, 42)
	viewOf(m)
	mm := click(m, colOf(t, m, termY, "⎇ main")+1, termY)
	wantOverview(t, mm)

	lines := strings.Split(ansi.Strip(viewOf(m)), "\n")
	for y, l := range lines[bodyY:] {
		if i := strings.Index(l, "web-checkout"); i >= 0 && i < 30 {
			branchY := bodyY + y + 1
			mm = click(m, colOf(t, m, branchY, "⎇ main")+1, branchY)
			wantOverview(t, mm)
			return
		}
	}
	t.Fatalf("no web-checkout in the sidebar:\n%s", strings.Join(lines, "\n"))
}

func TestBranchClickBasicProfile(t *testing.T) {
	m := turnOff(overviewIn(t, 150, 42), "view.git")
	viewOf(m)
	tabBefore := m.(Model).tab
	mm := click(m, colOf(t, m, termY, "⎇ main")+1, termY)
	if mm.(Model).menu == nil || mm.(Model).tab != tabBefore || mm.(Model).onTab(tabGit) {
		t.Errorf("with Git off the branch opens a menu: menu %v, tab %v", mm.(Model).menu, mm.(Model).tab)
	}
}

func TestOverviewHomeAllSpaces(t *testing.T) {
	useGH(t)
	m := overviewIn(t, 150, 42)
	n := len(m.spaces)
	m.cur = -1
	m.dash = false
	mm := inSection(key(m, "3"), gsHere)
	out := screen(mm)
	for _, sp := range mm.(Model).spaces {
		if !strings.Contains(out, "▍"+spaceLabel(sp)) {
			t.Errorf("home lists every space (%d), %q is missing:\n%s", n, sp.Name, out)
		}
	}
	in := overviewIn(t, 150, 42)
	in.dash = true
	in.show(tabGit)
	one := screen(inSection(in, gsHere))
	if !strings.Contains(one, "▍web-checkout") || strings.Contains(one, "▍infra") {
		t.Errorf("in a space only its own rows show:\n%s", one)
	}
}

func inSection(m tea.Model, sec int) tea.Model {
	mm := m.(Model)
	mm.gitv.setSec(&mm, sec)
	return mm
}

func TestGitNoSessionActions(t *testing.T) {
	m := key(boot(t, 150, 42), "3")
	g := gitOf(m)
	if !g.noSess || g.cur != gsHere {
		t.Fatalf("the stub gh has no session: noSess %v, section %d", g.noSess, g.cur)
	}
	m = key(m, "right") // My PRs
	out := screen(m)
	for _, want := range []string{"GitHub isn't connected", "sign in now", "gh: not signed in"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	setup := key(m, "s")
	if w := setup.(Model).setup; w == nil || w.step != setupGitHub {
		t.Error("s opens the setup wizard at GitHub")
	}
	login := key(m, "i").(Model)
	if sp := login.space(); sp == nil || sp.Tab().Label != "gh auth login" {
		t.Errorf("i runs gh auth login in a tab: %v", login.space())
	}
}

func TestGitNoGHActions(t *testing.T) {
	f := useGH(t)
	f.fail = gh.ErrNoGH
	m := key(boot(t, 150, 42), "3")
	m = key(m, "right")
	out := screen(m)
	for _, want := range []string{"gh isn't installed.", "open cli.github.com", "gh: not installed"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
}

func TestGitDefaultSectionInSpace(t *testing.T) {
	useGH(t)
	m := key(boot(t, 150, 42), "3")
	if g := gitOf(m); g.cur != gsPRs {
		t.Errorf("at home with gh the view opens on My PRs, not %d", g.cur)
	}
	in := overviewIn(t, 150, 42)
	in.dash = true
	var mm tea.Model = in
	mm = key(mm, "3")
	if g := gitOf(mm); g.cur != gsHere || !g.auto {
		t.Errorf("in a space the view opens on the Overview, not %d", g.cur)
	}
}

func TestOverviewEmpty(t *testing.T) {
	m := inSection(key(bootEmpty(t, 120, 36), "3"), gsHere)
	if out := screen(m); !strings.Contains(out, "No spaces yet.") || !strings.Contains(out, "new space") {
		t.Errorf("an empty Overview says what to do:\n%s", out)
	}
}

func TestOverviewNotARepo(t *testing.T) {
	useGH(t)
	m := overviewIn(t, 150, 42)
	sp, _ := m.spaceFor(t.TempDir(), true)
	m.cur = m.indexOf(sp)
	m.show(tabGit)
	mm := inSection(m, gsHere)
	if out := screen(mm); !strings.Contains(out, "No git repository in") {
		t.Errorf("a folder that is not a repo says so:\n%s", out)
	}
}

func TestOverviewEnterGoesToSpace(t *testing.T) {
	useGH(t)
	m := overviewIn(t, 150, 42)
	want := m.cur
	m.cur = -1
	mm := inSection(key(m, "3"), gsHere)
	cur := mm.(Model)
	for i, r := range cur.gitv.ovRows(&cur) {
		if r.space == want && !r.head {
			cur.gitv.ov.idx = i
		}
	}
	mm = key(cur, "enter")
	if got := mm.(Model); got.cur != want || got.dash {
		t.Errorf("⏎ goes to the space terminal: cur %d (want %d), dash %v", got.cur, want, got.dash)
	}
}
