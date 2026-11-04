package ui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/secuone/nanny/internal/gh"
	"github.com/secuone/nanny/internal/gitwt"
)

// Branches: the local branches that have no worktree, with the ones that are
// done on top of their lives: merged into the default branch, or merged by a
// PR of yours (a squash leaves no trace in git, so the PR says it).

// brRow is a local branch of a repo.
type brRow struct {
	gitwt.Branch
	root, repo string
	prMerged   int // the PR that merged this very commit, 0 = none
}

// done says the branch can go: git has it merged, or a PR merged its last commit.
func (r brRow) done() bool { return r.Merged || r.prMerged > 0 }

// force is whether git has to be told: a squash leaves the branch looking unmerged.
func (r brRow) force() bool { return !r.Merged && r.prMerged > 0 }

type brList struct {
	rows   []brRow
	cur    cursor
	busy   bool
	loaded bool
	stale  bool
	sig    string
	err    error
}

func (l *brList) set(rows []brRow) {
	keep := ""
	if r, ok := l.sel(); ok {
		keep = r.root + "\x00" + r.Name
	}
	l.rows = rows
	for i, r := range rows {
		if r.root+"\x00"+r.Name == keep {
			l.cur.idx = i
		}
	}
	l.cur.clamp(len(rows), 1<<30)
}

func (l *brList) sel() (brRow, bool) {
	if l.cur.idx < 0 || l.cur.idx >= len(l.rows) {
		return brRow{}, false
	}
	return l.rows[l.cur.idx], true
}

// branchRepos are the repos the view shows: the ones of the Worktrees list (the scope applies).
func branchRepos(m *Model) []*gitwt.Repo {
	var out []*gitwt.Repo
	for _, r := range m.wt.rows {
		if r.wt == nil {
			out = append(out, r.repo)
		}
	}
	return out
}

// sigOf changes when the repos or their worktrees do, and the branches are read again.
func sigOf(repos []*gitwt.Repo) string {
	var b strings.Builder
	for _, r := range repos {
		b.WriteString(r.Root + "\n")
		for _, w := range r.Worktrees {
			b.WriteString(w.Path + " " + w.Branch + "\n")
		}
	}
	return b.String()
}

func (g *gitView) pollBranches(m *Model, force bool) tea.Cmd {
	if !g.on(m, gsBranches) || g.br.busy || len(m.wt.rows) == 0 {
		return nil
	}
	repos := branchRepos(m)
	sig := sigOf(repos)
	if !(force || !g.br.loaded || g.br.stale || sig != g.br.sig) {
		return nil
	}
	g.br.busy = true
	roots := make([]string, len(repos))
	for i, r := range repos {
		roots[i] = r.Root
	}
	login, withGH := g.login, !g.noSess
	return func() tea.Msg { return readBranches(roots, sig, login, withGH) }
}

// readBranches lists the branches of every repo, and asks GitHub which of them a PR merged.
func readBranches(roots []string, sig, login string, withGH bool) tea.Msg {
	msg := branchesMsg{sig: sig, login: login}
	merged := map[string]gh.PR{} // repo + branch → the PR
	if withGH {
		ctx, cancel := ghCtx()
		defer cancel()
		l, err := viewerOr(ctx, login)
		if err == nil {
			var prs []gh.PR
			if prs, err = gh.MergedPRs(ctx, l); err == nil {
				for _, p := range prs {
					merged[p.Repo+"\x00"+p.Head] = p
				}
			}
			msg.login = l
		}
		msg.err = err
	}
	for _, root := range roots {
		def, slug := gitwt.DefaultBranch(root), gitwt.Slug(root)
		for _, b := range gitwt.Branches(root) {
			if b.Name == def || b.Worktree != "" {
				continue
			}
			row := brRow{Branch: b, root: root, repo: filepath.Base(root)}
			if p, ok := merged[slug+"\x00"+b.Name]; ok && slug != "" && p.HeadSHA == b.Tip {
				row.prMerged = p.Number
			}
			msg.rows = append(msg.rows, row)
		}
	}
	sort.SliceStable(msg.rows, func(i, j int) bool {
		a, b := msg.rows[i], msg.rows[j]
		if a.repo != b.repo {
			return a.repo < b.repo
		}
		return a.When.After(b.When)
	})
	return msg
}

func (g *gitView) keyBranch(m *Model, k tea.KeyPressMsg) tea.Cmd {
	s := k.String()
	if moveKey(&g.br.cur, len(g.br.rows), m.bodyHeight(), s) {
		return nil
	}
	if s == "r" {
		return g.refresh(m)
	}
	if s == "x" {
		return g.deleteDone(m)
	}
	b, ok := g.br.sel()
	if !ok {
		return nil
	}
	switch s {
	case "d":
		return g.deleteOne(m, b)
	case "p":
		if b.Upstream != "" && b.Ahead == 0 && !b.Gone {
			m.setStatus(T("nothing to push"), false)
			return nil
		}
		return g.run(m, Tf("pushed %s", b.Name), func() error { return gitwt.Push(b.root, b.Name) })
	case "w":
		root, name := b.root, b.Name
		return func() tea.Msg {
			path, err := gitwt.AddExisting(root, name)
			if err != nil {
				return statusMsg{err.Error(), true}
			}
			return wtChangedMsg{T("created ") + tilde(path)}
		}
	case "o":
		return m.shell(b.root)
	case "y":
		return m.copy(b.Name, T("branch name"))
	}
	return nil
}

// run does a git call off the UI loop and refreshes the branches after.
func (g *gitView) run(m *Model, text string, do func() error) tea.Cmd {
	g.doing = true
	m.setStatus(text+"…", false)
	return func() tea.Msg { return branchDoneMsg{text, do()} }
}

func (g *gitView) deleteOne(m *Model, b brRow) tea.Cmd {
	if !b.done() {
		m.setStatus(T("not merged: its commits only live here"), true)
		return nil
	}
	body := b.repo + " · " + b.Name
	if b.force() {
		body += "\n\n" + Tf("PR #%d merged it as a squash, so git calls it unmerged: it is deleted anyway.", b.prMerged)
	}
	m.ask(Tf("Delete branch %s?", b.Name), body, func(m *Model, _ string) tea.Cmd {
		return g.run(m, Tf("deleted %s", b.Name), func() error { return gitwt.DeleteBranch(b.root, b.Name, b.force()) })
	})
	return nil
}

// deleteDone deletes every branch that is done, after listing them.
func (g *gitView) deleteDone(m *Model) tea.Cmd {
	var rows []brRow
	var names []string
	for _, r := range g.br.rows {
		if r.done() {
			rows = append(rows, r)
			names = append(names, r.repo+" · "+r.Name)
		}
	}
	if len(rows) == 0 {
		m.setStatus(T("no merged branches"), false)
		return nil
	}
	shown := names[:min(len(names), 10)]
	body := strings.Join(shown, "\n")
	if len(names) > len(shown) {
		body += "\n" + Tf("… and %d more", len(names)-len(shown))
	}
	m.ask(Tn(len(rows), "Delete %d merged branch?", "Delete %d merged branches?"), body, func(m *Model, _ string) tea.Cmd {
		return g.run(m, Tn(len(rows), "deleted %d branch", "deleted %d branches"), func() error {
			var first error
			for _, r := range rows {
				if err := gitwt.DeleteBranch(r.root, r.Name, r.force()); err != nil && first == nil {
					first = err
				}
			}
			return first
		})
	})
	return nil
}

// ---------------------------------------------------------------- drawing

func (g *gitView) brTags(m *Model, r brRow) string {
	s := m.st
	var t []string
	switch {
	case r.Merged:
		t = append(t, s.Green.Render(T("merged")))
	case r.prMerged > 0:
		t = append(t, s.Green.Render(Tf("PR #%d merged", r.prMerged)))
	case r.Gone:
		t = append(t, s.Yellow.Render(T("upstream gone")))
	case r.Upstream == "":
		t = append(t, s.Yellow.Render(T("not pushed")))
	}
	if r.Ahead > 0 {
		t = append(t, s.Green.Render(fmt.Sprintf("↑%d", r.Ahead)))
	}
	if r.Behind > 0 {
		t = append(t, s.Red.Render(fmt.Sprintf("↓%d", r.Behind)))
	}
	t = append(t, s.Muted.Render(ago(r.When)))
	return strings.Join(t, " ")
}

func (g *gitView) viewBranches(m *Model, w, h int) string {
	s := m.st
	inner := w - 4
	title := T("Branches")
	switch {
	case g.br.loaded && len(g.br.rows) == 0:
		return s.panel(title, s.emptyState(T("No branches without a worktree."), nil, inner), w, h, !m.focusR)
	case !g.br.loaded:
		return s.panel(title, s.emptyState(spinGlyph+" "+T("looking at the branches…"), nil, inner), w, h, !m.focusR)
	}
	blocks := make([][]string, len(g.br.rows))
	for i, r := range g.br.rows {
		right := g.brTags(m, r)
		left := s.Teal.Render("⎇ ") + s.Bold.Render(r.Name) + " " + s.Muted.Render(r.repo)
		line := fit(left, inner-lipglossW(right)-1) + " " + right
		if i == g.br.cur.idx {
			st := s.RowSel
			if m.focusR {
				st = s.RowSelBlur
			}
			line = s.Sel(st, fit(line, inner))
		}
		blocks[i] = []string{line}
	}
	g.br.cur.clamp(len(g.br.rows), bodyRows(h))
	n := 0
	for _, r := range g.br.rows {
		if r.done() {
			n++
		}
	}
	t := title + " · " + strconv.Itoa(len(g.br.rows))
	if n > 0 {
		t += " · " + Tf("%d merged", n)
	}
	t += g.scopeTitle(m)
	return s.panel(t, m.listBlocks(blocks, g.br.cur.idx, "gitbr", bodyRows(h), secY+2+gapOf(h)), w, h, !m.focusR)
}

func (g *gitView) detailBranch(m *Model, w, h int) string {
	s := m.st
	inner := w - 4
	r, ok := g.br.sel()
	if !ok {
		return s.panel(T("Branch"), "", w, h, m.focusR)
	}
	ls := []string{s.Bold.Render(r.Name), "", s.kv("repo", r.repo+"  "+s.Muted.Render(tilde(r.root)), inner+30)}
	up := s.Muted.Render(T("none"))
	if r.Upstream != "" {
		up = r.Upstream
	}
	ls = append(ls, s.kv("upstream", up, inner))
	if r.Ahead+r.Behind > 0 {
		ls = append(ls, s.kv("", Tf("%s ahead · %s behind", s.Green.Render(fmt.Sprint(r.Ahead)), s.Red.Render(fmt.Sprint(r.Behind))), inner+30))
	}
	ls = append(ls, s.kv(T("last commit"), ago(r.When), inner), "")
	switch {
	case r.Merged:
		ls = append(ls, s.Green.Render(T("Merged into the default branch.")), s.Muted.Render(T("d deletes it.")))
	case r.prMerged > 0:
		ls = append(ls, s.Green.Render(Tf("PR #%d merged this very commit.", r.prMerged)), s.Muted.Render(T("d deletes it, forced: a squash leaves git unsure.")))
	default:
		ls = append(ls, s.Yellow.Render(T("Not merged: its commits only live here.")))
		if r.Upstream == "" || r.Ahead > 0 {
			ls = append(ls, s.Muted.Render(T("p pushes it.")))
		}
	}
	ls = append(ls, "", s.hintsFit([][2]string{{"d", T("delete")}, {"p", T("push")}, {"w", T("worktree")}, {"o", T("shell")}, {"y", T("copy")}}, inner))
	return s.panel(T("Branch"), m.detailScroll(ls, h), w, h, m.focusR)
}
