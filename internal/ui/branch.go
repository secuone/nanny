package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/secuone/nanny/internal/gitwt"
	"github.com/secuone/nanny/internal/ws"
)

// Where each space and agent is in git: the branch's local changes, how far
// it is ahead of or behind its upstream, and its open pull request (via gh).

// checkout is the git state of a folder.
type checkout struct {
	root, branch         string
	dirty, ahead, behind int
}

type gitStateMsg map[string]checkout       // folder → its state
type prsMsg map[string]map[string]gitwt.PR // repo root → branch → open PR

const (
	gitStateEvery = 20 * time.Second
	gitStateIdle  = time.Minute
	prsEvery      = 3 * time.Minute
)

// gitDirs are the folders worth knowing about: where each space is working
// and where each agent runs.
func (m Model) gitDirs() []string {
	seen := map[string]bool{}
	var out []string
	add := func(d string) {
		if d != "" && !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	for _, sp := range m.spaces {
		add(spaceWhere(sp))
	}
	for _, r := range m.live.allRows {
		if r.sub == nil {
			_, cwd := r.titleCwd()
			add(cwd)
		}
	}
	sort.Strings(out)
	return out
}

// gitStateCmd reads the git state of gitDirs, off the UI loop.
func (m *Model) gitStateCmd() tea.Cmd {
	m.gitAt = time.Now()
	dirs := m.gitDirs()
	return func() tea.Msg {
		out := gitStateMsg{}
		for _, d := range dirs {
			root := gitwt.RepoRoot(d)
			if root == "" {
				continue
			}
			c := checkout{root: root, branch: branchOf(d)}
			c.dirty, c.ahead, c.behind, _ = gitwt.Status(d)
			out[d] = c
		}
		return out
	}
}

// prsCmd asks gh for the open PRs of every repo nanny shows.
func (m *Model) prsCmd() tea.Cmd {
	m.prsAt = time.Now()
	if !gitwt.HasGH() || !m.showsPRs() {
		return nil
	}
	roots := map[string]bool{}
	for _, c := range m.gitState {
		roots[c.root] = true
	}
	for _, r := range m.repos {
		roots[r.Root] = true
	}
	return func() tea.Msg {
		out := prsMsg{}
		for root := range roots {
			if prs, err := gitwt.OpenPRs(root); err == nil {
				out[root] = prs
			}
		}
		return out
	}
}

// showsPRs says whether a PR shows anywhere: the sidebar, Live's rows and
// detail (not in the one-line layout), or the Worktrees view. If not, gh
// isn't asked.
func (m Model) showsPRs() bool {
	return m.cfg.On("sidebar.pr") || !m.basic() || m.viewOn(tabGit) && m.cfg.On("git.worktrees")
}

// stateOf is the git state of dir (or of the closest folder above it known).
func (m Model) stateOf(dir string) (checkout, bool) {
	best, bl := checkout{}, -1
	for d, c := range m.gitState {
		if inDir(dir, d) && len(d) > bl {
			best, bl = c, len(d)
		}
	}
	return best, bl >= 0
}

// prOf is the open PR of the branch dir is on.
func (m Model) prOf(dir string) (gitwt.PR, bool) {
	c, ok := m.stateOf(dir)
	if !ok {
		return gitwt.PR{}, false
	}
	return m.prFor(c.root, c.branch)
}

func (m Model) prFor(root, branch string) (gitwt.PR, bool) {
	p, ok := m.prs[root][branch]
	return p, ok && branch != ""
}

// branchBits is a checkout's state in a few cells: ●3 changed, ↑2 to push,
// ↓1 to pull (the warning one), or ✓ when there is nothing to do.
func (m Model) branchBits(c checkout) string {
	s := m.st
	var b []string
	if c.dirty > 0 {
		b = append(b, s.Yellow.Render(fmt.Sprintf("●%d", c.dirty)))
	}
	if c.ahead > 0 {
		b = append(b, s.Teal.Render(fmt.Sprintf("↑%d", c.ahead)))
	}
	if c.behind > 0 {
		b = append(b, s.Peach.Bold(true).Render(fmt.Sprintf("↓%d", c.behind)))
	}
	if len(b) == 0 {
		return s.Green.Render("✓")
	}
	return strings.Join(b, " ")
}

// prBadge is a PR in a few cells: #96 and its checks (✓ ✗ ◌), dimmed as a draft.
func (m Model) prBadge(p gitwt.PR) string {
	s := m.st
	n := fmt.Sprintf("#%d", p.Number)
	if p.Draft {
		return s.Muted.Render(n + " " + T("draft"))
	}
	out := s.Accent.Render(n)
	switch p.Checks {
	case "pass":
		out += s.Green.Render("✓")
	case "fail":
		out += s.Red.Bold(true).Render("✗")
	case "pending":
		out += s.Yellow.Render("◌")
	}
	if p.Review == "APPROVED" {
		out += s.Green.Render("✔")
	} else if p.Review == "CHANGES_REQUESTED" {
		out += s.Red.Render("✎")
	}
	return out
}

// prLine is a PR for a detail pane: number, checks and review in words.
func (m Model) prLine(p gitwt.PR) string {
	s := m.st
	parts := []string{m.prBadge(p)}
	switch p.Checks {
	case "pass":
		parts = append(parts, s.Green.Render(T("checks pass")))
	case "fail":
		parts = append(parts, s.Red.Render(T("checks fail")))
	case "pending":
		parts = append(parts, s.Yellow.Render(T("checks running")))
	}
	switch p.Review {
	case "APPROVED":
		parts = append(parts, s.Green.Render(T("approved")))
	case "CHANGES_REQUESTED":
		parts = append(parts, s.Red.Render(T("changes requested")))
	case "REVIEW_REQUIRED":
		parts = append(parts, s.Muted.Render(T("review pending")))
	}
	return strings.Join(parts, s.Dim.Render(" · "))
}

// gitItems are the git actions on a checkout, for its menus: open its PR,
// pull, fetch.
func (m *Model) gitItems(dir string) []menuItem {
	c, ok := m.stateOf(dir)
	if !ok {
		return nil
	}
	return m.gitItemsFor(dir, c.root, c.branch, c.behind)
}

func (m *Model) gitItemsFor(dir, root, branch string, behind int) []menuItem {
	var items []menuItem
	if p, ok := m.prFor(root, branch); ok {
		url := p.URL
		items = append(items, menuItem{Tf("Open PR #%d", p.Number), "O", func(m *Model) tea.Cmd { return m.openURL(url) }})
	}
	if behind > 0 {
		items = append(items, menuItem{Tf("Pull %d commits (fast-forward)", behind), "P", func(m *Model) tea.Cmd { return m.gitRun(dir, "pull") }})
	}
	return append(items, menuItem{T("Fetch"), "F", func(m *Model) tea.Cmd { return m.gitRun(dir, "fetch") }})
}

// gitRun pulls or fetches dir in the background and refreshes what is shown.
func (m *Model) gitRun(dir, what string) tea.Cmd {
	doing, done := T("fetching %s…"), T("fetched %s")
	if what == "pull" {
		doing, done = T("pulling %s…"), T("pulled %s")
	}
	m.setStatus(fmt.Sprintf(doing, tilde(dir)), false)
	return func() tea.Msg {
		err := gitwt.Fetch(dir)
		if what == "pull" {
			err = gitwt.Pull(dir)
		}
		if err != nil {
			return statusMsg{err.Error(), true}
		}
		return wtChangedMsg{fmt.Sprintf(done, tilde(dir))}
	}
}

// spaceGitItems are the git actions of where a space is working.
func (m *Model) spaceGitItems(sp *ws.Space) []menuItem { return m.gitItems(spaceWhere(sp)) }
