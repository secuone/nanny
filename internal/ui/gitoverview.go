package ui

import (
	"strconv"

	tea "charm.land/bubbletea/v2"
	"github.com/secuone/nanny/internal/gitwt"
	"github.com/secuone/nanny/internal/ws"
)

// Overview, the first section of the Git view: the branch, its state, its
// open PR and the worktrees of each space, all from git and what nanny has
// already collected; GitHub is not needed.

// ovRow is a line of the Overview: a space (head), the folder it is working
// in, or one of that folder's worktrees (wt).
type ovRow struct {
	space    int
	dir      string
	branch   string
	c        checkout
	pr       gitwt.PR
	hasPR    bool
	head, wt bool
	repo     bool
}

// ovRows are the rows of the spaces in scope: this space alone, else every one.
func (g *gitView) ovRows(m *Model) []ovRow {
	var rows []ovRow
	for i, sp := range m.spaces {
		if sc := m.spaceScope(); sc != nil && sc != sp {
			continue
		}
		where := spaceWhere(sp)
		c, ok := m.stateOf(where)
		br := c.branch
		if !ok {
			br = branchOf(where)
		}
		main := ovRow{space: i, dir: where, branch: br, c: c, repo: ok || br != ""}
		main.pr, main.hasPR = m.prFor(c.root, c.branch)
		head := main
		head.head = true
		rows = append(rows, head, main)
		for _, w := range m.spaceWorktrees(sp) {
			r := ovRow{space: i, dir: w.Path, branch: w.Branch, wt: true, repo: true,
				c: checkout{root: c.root, branch: w.Branch, dirty: w.Dirty, ahead: w.Ahead, behind: w.Behind}}
			r.pr, r.hasPR = m.prFor(c.root, w.Branch)
			rows = append(rows, r)
		}
	}
	return rows
}

func (g *gitView) ovSel(m *Model) (ovRow, bool) {
	rows := g.ovRows(m)
	if g.ov.idx < 0 || g.ov.idx >= len(rows) {
		return ovRow{}, false
	}
	return rows[g.ov.idx], true
}

func (g *gitView) ovLine(m *Model, r ovRow, w int) string {
	s := m.st
	sp := m.spaces[r.space]
	switch {
	case r.head:
		return s.Bold.Render("▍"+spaceLabel(sp)) + s.Muted.Render("  "+tilde(r.dir))
	case !r.repo:
		return "  " + s.Muted.Render(Tf("No git repository in %s.", tilde(r.dir)))
	}
	line := "  " + s.Blue.Render("⎇ "+trunc(r.branch, max(10, w/3))) + "  " + m.branchBits(r.c)
	if r.wt {
		line = "  " + line
	}
	if r.hasPR {
		line += "  " + m.prBadge(r.pr) + " " + s.Text.Render(r.pr.Title)
	}
	return line
}

func (g *gitView) viewOverview(m *Model, w, h int) string {
	s := m.st
	inner := w - 4
	title := T("Overview") + m.scopeNote()
	rows := g.ovRows(m)
	if len(rows) == 0 {
		return s.panel(title, s.emptyState(T("No spaces yet."), [][2]string{{"N", T("new space")}}, inner), w, h, !m.focusR)
	}
	blocks := make([][]string, len(rows))
	for i, r := range rows {
		line := g.ovLine(m, r, inner)
		if i == g.ov.idx {
			st := s.RowSel
			if m.focusR {
				st = s.RowSelBlur
			}
			line = s.Sel(st, fit(line, inner))
		}
		blocks[i] = []string{fit(line, inner)}
	}
	g.ov.clamp(len(rows), bodyRows(h))
	return s.panel(title, m.listBlocks(blocks, g.ov.idx, "git"+strconv.Itoa(g.cur), bodyRows(h), secY+2+gapOf(h)), w, h, !m.focusR)
}

func (g *gitView) ovState(r ovRow) string {
	c := r.c
	if c.dirty == 0 && c.ahead == 0 && c.behind == 0 {
		return T("nothing to commit or push")
	}
	return Tf("%d changed · %d to push · %d to pull", c.dirty, c.ahead, c.behind)
}

func (g *gitView) detailOverview(m *Model, w, h int) string {
	s := m.st
	inner := w - 4
	r, ok := g.ovSel(m)
	if !ok {
		return s.panel(T("Overview"), "", w, h, m.focusR)
	}
	name := r.branch
	if r.head {
		name = spaceLabel(m.spaces[r.space])
	}
	ls := []string{s.Bold.Render(trunc(name, inner)), "", s.kv(T("folder"), tilde(r.dir), inner)}
	if !r.repo {
		ls = append(ls, "", s.Muted.Render(trunc(Tf("No git repository in %s.", tilde(r.dir)), inner)))
		return s.panel(T("Overview"), m.detailScroll(ls, h), w, h, m.focusR)
	}
	ls = append(ls, s.kv(T("branch"), s.Blue.Render(r.branch), inner), s.kv(T("state"), g.ovState(r), inner+20))
	pr := s.Muted.Render(T("no open PR"))
	if r.hasPR {
		pr = m.prLine(r.pr) + s.Dim.Render(" · ") + s.Text.Render(r.pr.Title)
	}
	ls = append(ls, s.kv("PR", pr, inner+40), "")
	for _, k := range [][2]string{
		{"⏎", T("go to it: the space, or a shell in that worktree")},
		{"O  C", T("open its PR · review its changes")},
		{"P  F", T("pull (fast-forward) · fetch")},
		{"n  w", T("new claude in it · new worktree")},
	} {
		ls = append(ls, "  "+s.Key.Render(fit(k[0], 6))+s.Text.Render(trunc(k[1], inner-8)))
	}
	return s.panel(T("Overview"), m.detailScroll(ls, h), w, h, m.focusR)
}

func (g *gitView) keyOverview(m *Model, k tea.KeyPressMsg) tea.Cmd {
	rows := g.ovRows(m)
	ks := k.String()
	if moveKey(&g.ov, len(rows), m.bodyHeight(), ks) {
		return nil
	}
	if ks == "r" {
		return tea.Batch(m.gitStateCmd(), m.spaceWTCmd(), m.prsCmd())
	}
	r, ok := g.ovSel(m)
	if !ok {
		return nil
	}
	sp := m.spaces[r.space]
	switch {
	case ks == "enter" && r.wt:
		return m.tabAt(sp, r.dir, trunc(r.branch, 20), nil)
	case ks == "enter":
		m.cur = r.space
		m.back()
	case ks == "w":
		m.newWorktree(sp)
	case !r.repo:
		return nil
	case ks == "O":
		if !r.hasPR {
			m.setStatus(T("no open PR"), true)
			return nil
		}
		return m.openURL(r.pr.URL)
	case ks == "C":
		return m.openReview(r.dir)
	case ks == "P":
		if r.c.behind > 0 {
			return m.gitRun(r.dir, "pull")
		}
	case ks == "F":
		return m.gitRun(r.dir, "fetch")
	case ks == "n":
		return m.tabAt(sp, r.dir, trunc(spaceLabel(sp), 20), m.claudeArgv(""))
	case ks == "y" && r.branch != "":
		return m.copy(r.branch, T("branch"))
	}
	return nil
}

// menuOverview is the right-click menu of the selected row.
func (g *gitView) menuOverview(m *Model) []menuItem {
	r, ok := g.ovSel(m)
	if !ok || !r.repo {
		return nil
	}
	items := []menuItem{
		{T("New claude in it"), "n", press("n")},
		{T("Review changes"), "C", press("C")},
		sep,
	}
	return append(items, m.gitItemsFor(r.dir, r.c.root, r.branch, r.c.behind)...)
}

// openGitHere shows the Overview on space i: from the ⎇ branch label and
// prefix G. With the view or the section off it opens that space's git menu.
func (m *Model) openGitHere(i int) tea.Cmd {
	if i < 0 || i >= len(m.spaces) {
		return nil
	}
	sp := m.spaces[i]
	if !m.viewOn(tabGit) || !m.cfg.On("git.here") {
		return m.gitHereMenu(sp)
	}
	m.cur = i
	m.show(tabGit)
	g := m.gitv
	g.cur, g.auto = gsHere, false
	return tea.Batch(g.due(m, true), m.refreshDetail())
}

func (m *Model) gitHereMenu(sp *ws.Space) tea.Cmd {
	m.setStatus(T("Git is off in this profile · = turns it on"), false)
	items := m.spaceGitItems(sp)
	if len(items) == 0 {
		return nil
	}
	m.openMenu("⎇ "+branchOf(spaceWhere(sp)), m.w/2, m.h/2, items)
	return nil
}
