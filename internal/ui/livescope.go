package ui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/secuone/nanny/internal/ws"
)

// cwd is where the agent works: its session's folder, else where its process is.
func (r liveRow) cwd() string {
	if r.sess == nil {
		return ""
	}
	if r.sess.Cwd == "" && r.sess.Live != nil {
		return r.sess.Live.Cwd
	}
	return r.sess.Cwd
}

// spaceHas tells whether an agent belongs to sp: it runs in one of the
// space's panes, or its folder is inside the space's folders or one of the
// worktrees of its repo (also the ones that live elsewhere on disk).
func (m Model) spaceHas(sp *ws.Space) func(liveRow) bool {
	wts := m.spaceWorktrees(sp)
	return func(r liveRow) bool {
		if r.local != nil && r.local.space == sp {
			return true
		}
		cwd := r.cwd()
		if sp.Covers(cwd) {
			return true
		}
		for _, w := range wts {
			if inDir(cwd, w.Path) {
				return true
			}
		}
		return false
	}
}

// sideScope is the space the sidebar's agents are limited to: the one you are
// in, else the one picked from home; nil lists every agent.
func (m Model) sideScope() *ws.Space {
	if m.inSpace() {
		return m.space()
	}
	return m.scopeSpace
}

// inSideScope keeps the rows of the sidebar's scope (all when it has none).
func (m Model) inSideScope(rows []liveRow) []liveRow {
	sp := m.sideScope()
	if sp == nil {
		return rows
	}
	has := m.spaceHas(sp)
	var out []liveRow
	for _, r := range rows {
		if has(r) {
			out = append(out, r)
		}
	}
	return out
}

// waitingElsewhere counts the agents blocked on you outside the sidebar's scope.
func (m Model) waitingElsewhere() int {
	sp := m.sideScope()
	if sp == nil {
		return 0
	}
	has, n := m.spaceHas(sp), 0
	for _, r := range m.live.allRows {
		if r.agent() && r.state() == "blocked" && !has(r) {
			n++
		}
	}
	return n
}

// showAllSpaces goes home, where every agent is listed.
func (m *Model) showAllSpaces() {
	m.cur, m.scope, m.scopeSpace = -1, "", nil
	m.layoutPanes()
}

// allSpacesLink is a one-line way back to every space, clickable.
func (m Model) allSpacesLink(text string) string {
	return clickable(allSpacesKey, m.st.Accent.Render("⊞ "+text))
}

const allSpacesKey = "@allspaces"

// scopedRows keeps the Live rows of the space on screen (or the project picked
// with [ ]); home lists every agent.
func (m Model) scopedRows(rows []liveRow) []liveRow {
	var keep func(liveRow) bool
	switch sp := m.spaceScope(); {
	case sp != nil:
		keep = m.spaceHas(sp)
	case m.scope != "":
		keep = func(r liveRow) bool { return project(r.cwd()) == m.scope }
	default:
		return rows
	}
	out := rows[:0:0]
	for _, r := range rows {
		if keep(r) {
			out = append(out, r)
		}
	}
	return out
}

// jumpToBlocked widens Live to every space and lands on the first agent
// waiting on you.
func (m *Model) jumpToBlocked() tea.Cmd {
	m.showAllSpaces()
	m.live.only = ""
	m.rebuild()
	for i, r := range m.live.rows {
		if r.agent() && r.state() == "blocked" {
			m.live.cur.idx = i
			break
		}
	}
	return m.refreshDetail()
}
