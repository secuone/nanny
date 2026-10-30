package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/secuone/nanny/internal/claude"
	"github.com/secuone/nanny/internal/gitwt"
	"github.com/secuone/nanny/internal/ws"
)

type wtRow struct {
	repo     *gitwt.Repo
	wt       *gitwt.Worktree // nil for the repo header
	sessions []*claude.Session
	live     int
	busy     int
}

type wtTab struct {
	cur  cursor
	rows []wtRow
}

func (t *wtTab) sel() *wtRow {
	if t.cur.idx < 0 || t.cur.idx >= len(t.rows) {
		return nil
	}
	return &t.rows[t.cur.idx]
}

func (t *wtTab) rebuild(m *Model) {
	keep := ""
	if r := t.sel(); r != nil {
		keep = r.key()
	}
	// every worktree path, so sessions attach to the deepest matching checkout
	var paths []string
	for _, r := range m.repos {
		for _, w := range r.Worktrees {
			paths = append(paths, w.Path)
		}
	}
	owners := map[string]string{} // many sessions share a folder
	owner := func(cwd string) string {
		if o, ok := owners[cwd]; ok {
			return o
		}
		best := ""
		for _, p := range paths {
			if len(p) > len(best) && strings.HasPrefix(cwd, p) && (len(cwd) == len(p) || cwd[len(p)] == '/') {
				best = p
			}
		}
		owners[cwd] = best
		return best
	}
	byPath := map[string][]*claude.Session{}
	for _, s := range m.sessions {
		if o := owner(s.Cwd); o != "" {
			byPath[o] = append(byPath[o], s)
		}
	}
	t.rows = t.rows[:0]
	for i := range m.repos {
		r := &m.repos[i]
		if m.scope != "" && r.Name != m.scope || !repoInSpace(r, m.spaceScope()) {
			continue
		}
		t.rows = append(t.rows, wtRow{repo: r})
		for j := range r.Worktrees {
			w := &r.Worktrees[j]
			row := wtRow{repo: r, wt: w, sessions: byPath[w.Path]}
			for _, s := range row.sessions {
				if s.Live != nil {
					row.live++
					if s.Live.Status == "busy" {
						row.busy++
					}
				}
			}
			t.rows = append(t.rows, row)
		}
	}
	for i, r := range t.rows {
		if r.key() == keep {
			t.cur.idx = i
		}
	}
	t.cur.clamp(len(t.rows), 1<<30)
}

func (r wtRow) key() string {
	if r.wt != nil {
		return r.wt.Path
	}
	return "repo:" + r.repo.Root
}

var branchRe = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

func (t *wtTab) key(m *Model, k tea.KeyMsg) tea.Cmd {
	n := len(t.rows)
	h := m.bodyHeight()
	r := t.sel()
	switch k.String() {
	case "j", "down":
		t.cur.move(1, n, h)
	case "k", "up":
		t.cur.move(-1, n, h)
	case "J":
		for i := t.cur.idx + 1; i < n; i++ {
			if t.rows[i].wt == nil {
				t.cur.idx = i
				break
			}
		}
	case "K":
		for i := t.cur.idx - 1; i >= 0; i-- {
			if t.rows[i].wt == nil {
				t.cur.idx = i
				break
			}
		}
	case "g", "home":
		t.cur.idx = 0
	case "G", "end":
		t.cur.idx = n - 1
	case "enter", "r":
		if r == nil || r.wt == nil {
			return nil
		}
		if len(r.sessions) == 0 {
			return m.run(r.wt.Path, filepath.Base(r.wt.Path), strings.TrimSpace("claude "+m.cfg.ClaudeArgs))
		}
		return m.resume(r.sessions[0], false)
	case "v":
		if r != nil && len(r.sessions) > 0 {
			ses := r.sessions[0]
			return m.openViewer(ses.Path, false, ses.Title, ses, nil)
		}
		m.setStatus(T("no session here yet · n starts claude"), false)
	case "n":
		if r != nil {
			p := r.repo.Root
			if r.wt != nil {
				p = r.wt.Path
			}
			return m.run(p, filepath.Base(p), strings.TrimSpace("claude "+m.cfg.ClaudeArgs))
		}
	case "C":
		if r != nil {
			p := r.repo.Root
			if r.wt != nil {
				p = r.wt.Path
			}
			return m.openReview(p)
		}
	case "o":
		if r != nil {
			p := r.repo.Root
			if r.wt != nil {
				p = r.wt.Path
			}
			return m.shell(p)
		}
	case "y", "c":
		if r != nil {
			p := r.repo.Root
			if r.wt != nil {
				p = r.wt.Path
			}
			return m.copy(p, T("path"))
		}
	case "a":
		if r == nil {
			return nil
		}
		repo := r.repo
		m.prompt(Tf("New worktree in %s", repo.Name),
			Tf("Branch name for the new worktree. It is created from the current HEAD under %s", tilde(repo.Root)+".worktrees/"),
			"feature/my-thing", func(m *Model, v string) tea.Cmd {
				if v == "" || !branchRe.MatchString(v) {
					m.setStatus(T("invalid branch name"), true)
					return nil
				}
				return func() tea.Msg {
					path, err := gitwt.Add(repo.Root, v, "")
					if err != nil {
						return statusMsg{err.Error(), true}
					}
					return wtChangedMsg{T("created ") + tilde(path)}
				}
			})
	case "d":
		if r == nil || r.wt == nil {
			return nil
		}
		if r.wt.Main {
			m.setStatus(T("the main checkout can't be removed"), true)
			return nil
		}
		// a dirty worktree needs --force; the warning says what is lost
		wt, repo := r.wt, r.repo
		force := wt.Dirty > 0
		warn := ""
		if force {
			warn = "\n\n⚠ " + Tn(wt.Dirty, "%d uncommitted change will be LOST.", "%d uncommitted changes will be LOST.")
		}
		if r.live > 0 {
			warn += "\n⚠ " + Tn(r.live, "%d live agent is running in it.", "%d live agents are running in it.")
		}
		m.ask(T("Remove worktree?"), tilde(wt.Path)+"  ("+wt.Branch+")"+warn+"\n\n"+T("The branch itself is kept."),
			func(m *Model, _ string) tea.Cmd {
				return func() tea.Msg {
					if err := gitwt.Remove(repo.Root, wt.Path, force); err != nil {
						return statusMsg{err.Error(), true}
					}
					return wtChangedMsg{T("removed ") + tilde(wt.Path)}
				}
			})
	case "p":
		if r == nil {
			return nil
		}
		root := r.repo.Root
		return func() tea.Msg {
			if err := gitwt.Prune(root); err != nil {
				return statusMsg{err.Error(), true}
			}
			return wtChangedMsg{Tf("pruned stale worktrees in %s", filepath.Base(root))}
		}
	}
	t.cur.clamp(len(t.rows), h)
	return nil
}

type wtChangedMsg struct{ text string }

func (t *wtTab) view(m *Model, w, h int) string {
	s := m.st
	inner := w - 4
	if len(t.rows) == 0 {
		msg := s.emptyState(T("No repositories yet. Repos show up here once an agent has worked in them."),
			[][2]string{{"8", T("add one under Extra repos in Settings")}, {"D", T("discover the places you work in")}}, inner)
		if m.scope != "" {
			msg = s.emptyState(Tf("No repositories in %s.", m.scope), [][2]string{{"\\", T("show every project")}}, inner)
		}
		if m.loading["repos"] {
			msg = s.emptyState(spinGlyph+" "+T("discovering repositories & worktrees…"), nil, inner)
		}
		return s.panel(T("Worktrees"), msg, w, h, !m.focusR)
	}
	// right-hand columns line up across every worktree row
	cells := make([][]string, len(t.rows))
	var widths []int
	for i, r := range t.rows {
		if r.wt == nil {
			continue
		}
		cells[i] = m.wtCells(r)
		for len(widths) < len(cells[i]) {
			widths = append(widths, 0)
		}
		for j, c := range cells[i] {
			widths[j] = max(widths[j], lipglossW(c))
		}
	}
	blocks := make([][]string, len(t.rows))
	for i, r := range t.rows {
		var line string
		if r.wt == nil {
			line = s.Accent.Render("▍"+r.repo.Name) + s.Muted.Render(fmt.Sprintf("  %s · %s", Tn(len(r.repo.Worktrees), "%d worktree", "%d worktrees"), tilde(r.repo.Root)))
			if i > 0 {
				blocks[i] = []string{""}
			}
		} else {
			line = m.wtLine(r, cells[i], widths, inner)
		}
		if i == t.cur.idx {
			st := s.RowSel
			if m.focusR {
				st = s.RowSelBlur
			}
			line = s.Sel(st, fit(line, inner))
		}
		blocks[i] = append(blocks[i], line)
	}
	return s.panel(T("Worktrees")+" · "+Tn(countRepos(t.rows), "%d repo", "%d repos")+m.scopeNote(), m.listBlocks(blocks, t.cur.idx, "wt", h-2, bodyY+1), w, h, !m.focusR)
}

// wtCells are the right-hand columns of a worktree row: live agents,
// sessions, changes, ahead/behind and last commit age.
func (m Model) wtCells(r wtRow) []string {
	s := m.st
	wt := r.wt
	live := ""
	if r.busy > 0 {
		live = m.stateGlyph("working") + s.Yellow.Render(fmt.Sprint(r.live))
	} else if r.live > 0 {
		live = m.stateGlyph("idle") + s.Green.Render(fmt.Sprint(r.live))
	}
	sess := ""
	if len(r.sessions) > 0 {
		sess = s.Muted.Render(Tf("%d sess", len(r.sessions)))
	}
	dirty := ""
	switch {
	case wt.Missing:
		dirty = s.Red.Render(T("missing"))
	case wt.Prunable:
		dirty = s.Red.Render(T("prunable"))
	case wt.Dirty > 0:
		dirty = s.Yellow.Render(fmt.Sprintf("✎%d", wt.Dirty))
	}
	sync := ""
	if wt.Ahead > 0 {
		sync += s.Green.Render(fmt.Sprintf("↑%d", wt.Ahead))
	}
	if wt.Behind > 0 {
		sync += s.Red.Render(fmt.Sprintf("↓%d", wt.Behind))
	}
	if p, ok := m.prFor(r.repo.Root, wt.Branch); ok {
		sync = strings.TrimSpace(sync + " " + m.prBadge(p))
	}
	return []string{live, sess, dirty, sync, s.Muted.Render(ago(wt.LastWhen))}
}

func (m Model) wtLine(r wtRow, cells []string, widths []int, w int) string {
	s := m.st
	wt := r.wt
	icon := s.Teal.Render("  ⎇ ")
	if wt.Main {
		icon = s.Accent.Render("  ★ ")
	}
	name := filepath.Base(wt.Path)
	if wt.Main {
		name = T("main checkout")
	}
	var right []string
	for j, c := range cells {
		if widths[j] == 0 {
			continue
		}
		right = append(right, strings.Repeat(" ", widths[j]-lipglossW(c))+c)
	}
	rs := strings.Join(right, " ")
	left := icon + s.Bold.Render(name)
	if wt.Branch != "" && wt.Branch != name && !(wt.Main && wt.Branch == "main") {
		left += " " + s.Muted.Render(wt.Branch)
	}
	return fit(left, w-lipglossW(rs)-1) + " " + rs
}

func (t *wtTab) detail(m *Model, w, h int) string {
	s := m.st
	r := t.sel()
	inner := w - 4
	if r == nil {
		return s.panel(T("Worktree"), "", w, h, m.focusR)
	}
	var ls []string
	if r.wt == nil {
		ls = append(ls, s.Bold.Render(r.repo.Name), "", s.kv(T("root"), tilde(r.repo.Root), inner),
			s.kv(T("worktrees"), fmt.Sprint(len(r.repo.Worktrees)), inner), "",
			s.hintsFit([][2]string{{"a", T("add a worktree")}, {"C", T("changes")}, {"n", T("new claude")}, {"o", T("shell")}, {"p", T("prune")}}, inner))
		return s.panel(T("Repository"), strings.Join(ls, "\n"), w, h, m.focusR)
	}
	wt := r.wt
	ls = append(ls, s.Bold.Render(filepath.Base(wt.Path)), "")
	ls = append(ls, s.kv(T("path"), tilde(wt.Path), inner))
	ls = append(ls, s.kv(T("branch"), s.Text.Render(wt.Branch), inner))
	if p, ok := m.prFor(r.repo.Root, wt.Branch); ok {
		ls = append(ls, s.kv("PR", m.prLine(p), inner), s.kv("", s.Muted.Render(trunc(p.Title, inner-12)), inner))
	}
	if len(wt.Head) > 10 {
		ls = append(ls, s.kv("head", wt.Head[:10], inner))
	}
	state := s.Green.Render(T("clean"))
	if wt.Dirty > 0 {
		state = s.Yellow.Render(Tn(wt.Dirty, "%d changed file", "%d changed files"))
	}
	if wt.Missing {
		state = s.Red.Render(T("directory missing — prune with p"))
	}
	ls = append(ls, s.kv(T("state"), state, inner))
	if !wt.Missing && (wt.Dirty > 0 || wt.Ahead > 0) {
		ls = append(ls, s.kv("", s.Muted.Render(T("C review the changes")), inner))
	}
	if wt.Ahead+wt.Behind > 0 {
		ls = append(ls, s.kv("upstream", Tf("%s ahead · %s behind", s.Green.Render(fmt.Sprint(wt.Ahead)), s.Red.Render(fmt.Sprint(wt.Behind))), inner+30))
	}
	if wt.LastMsg != "" {
		ls = append(ls, s.kv("commit", wt.LastMsg+"  ("+ago(wt.LastWhen)+")", inner))
	}
	if _, err := os.Stat(filepath.Join(wt.Path, ".claude")); err == nil {
		ls = append(ls, s.kv("claude", T(".claude/ present"), inner))
	}
	ls = append(ls, "", s.Accent.Render(T("Sessions here")+fmt.Sprintf(" · %d", len(r.sessions))))
	if len(r.sessions) == 0 {
		ls = append(ls, s.Muted.Render(T("none yet · ⏎ or n starts claude here")))
	}
	for i, ses := range r.sessions {
		if i >= 15 {
			ls = append(ls, s.Muted.Render(Tf("… %d more (filter them in Sessions)", len(r.sessions)-15)))
			break
		}
		lines := m.sessionLines(ses, inner)
		ls = append(ls, lines[0])
	}
	return s.panel(T("Worktree"), m.detailScroll(ls, h), w, h, m.focusR)
}

// repoInSpace reports whether a repository belongs to sp (always true without one):
// the space holds it or one of its worktrees, or lives inside it.
func repoInSpace(r *gitwt.Repo, sp *ws.Space) bool {
	if sp == nil || sp.Covers(r.Root) {
		return true
	}
	for _, d := range sp.Dirs() { // the space is inside the repo (one of its worktrees)
		if strings.HasPrefix(d+"/", strings.TrimSuffix(r.Root, "/")+"/") {
			return true
		}
	}
	for _, w := range r.Worktrees {
		if sp.Covers(w.Path) {
			return true
		}
	}
	return false
}

// countRepos is how many repos the rows list.
func countRepos(rows []wtRow) int {
	n := 0
	for _, r := range rows {
		if r.wt == nil {
			n++
		}
	}
	return n
}
