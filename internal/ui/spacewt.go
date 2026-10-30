package ui

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/secuone/nanny/internal/gitwt"
	"github.com/secuone/nanny/internal/term"
	"github.com/secuone/nanny/internal/ws"
)

// The sidebar shows each space's branch and, for the space you are in, its
// git worktrees; right-click a space to make a new one.

type spaceWTMsg map[string][]gitwt.Worktree

// paneDir is the folder a pane's program is in now.
func paneDir(p *term.Pane) string {
	if p == nil || p.Pid() <= 0 {
		return ""
	}
	d, _ := os.Readlink(filepath.Join("/proc", strconv.Itoa(p.Pid()), "cwd"))
	return d
}

// repoTop is the checkout dir holds (walking up to its .git), and for a
// linked worktree also its main repo; dir itself when it isn't in one.
func repoTop(dir string) []string {
	for d := dir; d != "/" && d != "."; d = filepath.Dir(d) {
		st, err := os.Stat(filepath.Join(d, ".git"))
		if err != nil {
			continue
		}
		out := []string{d}
		if !st.IsDir() { // gitdir: /repo/.git/worktrees/name
			if b, err := os.ReadFile(filepath.Join(d, ".git")); err == nil {
				gd := strings.TrimSpace(strings.TrimPrefix(string(b), "gitdir:"))
				if i := strings.Index(gd, "/.git/worktrees/"); i > 0 {
					out = append(out, gd[:i])
				}
			}
		}
		return out
	}
	return []string{dir}
}

// spaceFocus is where a space's panes are working: the repos they are in.
func spaceFocus(sp *ws.Space) []string {
	h, _ := os.UserHomeDir()
	seen := map[string]bool{}
	var out []string
	for _, t := range sp.Tabs {
		for _, p := range t.Panes {
			d := paneDir(p)
			if d == "" || d == h || d == "/" {
				continue
			}
			for _, r := range repoTop(d) {
				if !seen[r] {
					seen[r] = true
					out = append(out, r)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// refreshFocus updates where each space is working; true when it changed.
func (m *Model) refreshFocus() bool {
	changed := false
	for _, sp := range m.spaces {
		if f := spaceFocus(sp); strings.Join(f, "\n") != strings.Join(sp.Focus, "\n") {
			sp.Focus, changed = f, true
		}
	}
	return changed
}

// spaceWhere is the folder a space is working in now: where the shell of its
// active tab has cd'd to, else the folder it was opened on.
func spaceWhere(sp *ws.Space) string {
	if t := sp.Tab(); t != nil {
		if p := t.Pane(); p != nil && p.Pid() > 0 {
			if d, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(p.Pid()), "cwd")); err == nil {
				return d
			}
		}
	}
	return sp.Dir()
}

// spaceWTCmd lists the worktrees of the folder every space is in, off the UI
// loop (git is slow on big repos).
func (m *Model) spaceWTCmd() tea.Cmd {
	m.spaceWTAt = time.Now()
	var dirs []string
	for _, sp := range m.spaces {
		dirs = append(dirs, spaceWhere(sp))
	}
	return func() tea.Msg {
		out := spaceWTMsg{}
		for _, d := range dirs {
			if root := gitwt.RepoRoot(d); root != "" {
				out[d] = gitwt.List(root)
			} else {
				out[d] = nil // not a repo: known, nothing to list
			}
		}
		return out
	}
}

// spaceMoved reports whether a space's shell has cd'd somewhere not listed yet.
func (m Model) spaceMoved() bool {
	for _, sp := range m.spaces {
		if _, ok := m.spaceWT[spaceWhere(sp)]; !ok {
			return true
		}
	}
	return false
}

// worktrees of a space, besides the folder it was opened on.
func (m Model) spaceWorktrees(sp *ws.Space) []gitwt.Worktree {
	var out []gitwt.Worktree
	dir := spaceWhere(sp)
	for _, w := range m.spaceWT[dir] {
		if !inDir(dir, w.Path) && !w.Missing {
			out = append(out, w)
		}
	}
	return out
}

// tabAt opens argv in a new tab of sp, in cwd, and shows it.
func (m *Model) tabAt(sp *ws.Space, cwd, label string, argv []string) tea.Cmd {
	p, err := m.spawn(cwd, argv)
	if err != nil {
		m.setStatus(T("could not start: ")+err.Error(), true)
		return nil
	}
	t := ws.NewTab(label)
	t.Panes = []*term.Pane{p}
	sp.Tabs = append(sp.Tabs, t)
	m.focusRef(paneRef{sp, t, p})
	m.saveSpaces()
	return nil
}

// newWorktree asks for a branch and makes a worktree of the space's repo
// for it, then opens Claude there, in the same space.
func (m *Model) newWorktree(sp *ws.Space) {
	root := gitwt.RepoRoot(spaceWhere(sp))
	if root == "" {
		m.setStatus(Tf("%s isn't a git repo", tilde(spaceWhere(sp))), true)
		return
	}
	m.prompt(Tf("New worktree in %s", spaceLabel(sp)),
		Tf("Branch name for the new worktree. It is created from the current HEAD under %s", tilde(root)+".worktrees/"),
		"feature/my-thing", func(m *Model, v string) tea.Cmd {
			if v == "" || !branchRe.MatchString(v) {
				m.setStatus(T("invalid branch name"), true)
				return nil
			}
			path, err := gitwt.Add(root, v, "")
			if err != nil {
				m.setStatus(err.Error(), true)
				return nil
			}
			if !sp.Contains(path) { // its agents belong to this space
				sp.Paths = append(sp.Paths, path)
			}
			cmd := m.tabAt(sp, path, v, m.claudeArgv(""))
			return tea.Batch(cmd, m.spaceWTCmd(), func() tea.Msg { return wtChangedMsg{T("created ") + tilde(path)} })
		})
}

// worktreeMenu is what a click on a worktree under a space offers.
func (m *Model) worktreeMenu(sp *ws.Space, w gitwt.Worktree, x, y int) {
	name := w.Branch
	if name == "" {
		name = tilde(w.Path)
	}
	items := []menuItem{
		{T("Claude here"), "c", func(m *Model) tea.Cmd { return m.tabAt(sp, w.Path, trunc(name, 20), m.claudeArgv("")) }},
		{T("Shell here"), "s", func(m *Model) tea.Cmd { return m.tabAt(sp, w.Path, trunc(name, 20), nil) }},
		{T("Review changes"), "C", func(m *Model) tea.Cmd { return m.openReview(w.Path) }},
		sep,
	}
	if c, ok := m.stateOf(spaceWhere(sp)); ok { // PR, pull, fetch
		items = append(append(items, m.gitItemsFor(w.Path, c.root, w.Branch, w.Behind)...), sep)
	}
	m.openMenu("⎇ "+name, x, y, append(items,
		menuItem{T("Copy folder"), "y", func(m *Model) tea.Cmd { return m.copy(w.Path, T("folder")) }}))
}

// inDir reports whether dir is root or a folder inside it.
func inDir(dir, root string) bool {
	return dir == root || strings.HasPrefix(dir, strings.TrimSuffix(root, "/")+"/")
}
