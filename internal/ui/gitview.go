package ui

import (
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
)

// gitView is the Git view (3). For now it is the Worktrees view under its new
// name: the same keys, list and detail.
type gitView struct{ featBase }

func (g *gitView) view(m *Model, w, h int) string   { return m.wt.view(m, w, h) }
func (g *gitView) detail(m *Model, w, h int) string { return m.wt.detail(m, w, h) }
func (g *gitView) key(m *Model, k tea.KeyMsg) tea.Cmd {
	return m.wt.key(m, k)
}

func (g *gitView) click(m *Model, row int) tea.Cmd {
	if row >= 0 && row < len(m.wt.rows) {
		m.wt.cur.idx = row
	}
	return nil
}

func (g *gitView) footer(m *Model) [][2]string {
	return [][2]string{{"⏎", T("resume")}, {"C", T("changes")}, {"n", T("new claude")}, {"o", T("shell")}, {"a", T("add")}, {"d", T("remove")}}
}

func (g *gitView) help() []helpSec {
	return []helpSec{{T("Worktrees"), [][2]string{
		{"⏎  r", T("resume the newest session there (or start claude)")},
		{"n", T("new claude in it")},
		{"C", T("review its changes: files, diff, PR, commit")},
		{"a d p", T("add · remove · prune stale worktrees")},
	}}}
}

func (g *gitView) due(m *Model, force bool) tea.Cmd {
	if force && m.repos == nil && !m.loading["repos"] {
		m.loading["repos"] = true
		return m.reposCmd()
	}
	return nil
}

func (g *gitView) selKey(m *Model) string { return strconv.Itoa(m.wt.cur.idx) }
