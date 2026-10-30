package ui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/secuone/nanny/internal/claude"
)

// sideAgents are the sidebar's agents (top-level only): none while Live (the
// same list) is on screen below full; in minimal only the ones busy or
// waiting on you. quiet is the rest, one click from Live.
func (m Model) sideAgents() (agents, quiet []liveRow) {
	for _, r := range m.live.allRows {
		if r.sub != nil {
			continue
		}
		all := !m.basic() || m.cfg.Screen.SidebarAgents == "all"
		if st := r.state(); all || st == "blocked" || st == "working" || r.run > 0 || r.prompt() != nil {
			agents = append(agents, r)
		} else {
			quiet = append(quiet, r)
		}
	}
	return agents, quiet
}

// curAgentKey is the agent you are on: the one in the focused pane of the
// space on screen, else the last one walked to.
func (m Model) curAgentKey() string {
	if m.inSpace() && !m.dash {
		if t := m.space().Tab(); t != nil && len(t.Panes) > 0 {
			for _, r := range m.live.allRows {
				if r.sub == nil && r.local != nil && r.local.pane == t.Pane() {
					return r.key()
				}
			}
		}
	}
	return m.vs.sideCurKey
}

// openAgent is the one action of an agent anywhere: a subagent opens its
// viewer, an agent in a nanny pane its pane, one outside nanny its live
// conversation (esc comes back to where you were).
func (m *Model) openAgent(sess *claude.Session, sub *claude.Subagent, local *paneRef) tea.Cmd {
	switch {
	case sub != nil:
		return m.openViewer(sub.Path, true, sub.Description, sess, sub)
	case local != nil:
		m.focusRef(*local)
		m.vs.sideCurKey = "s:" + sess.ID
		return nil
	}
	m.vs.sideCurKey = "s:" + sess.ID
	m.setStatus(Tf("%s runs outside nanny: reading it live", trunc(sess.Title, 40)), false)
	return m.openViewer(sess.Path, false, sess.Title, sess, nil)
}

// navKey walks the sidebar's agents with alt+up/down, from anywhere.
func (m Model) navKey(ks string) (tea.Model, tea.Cmd, bool) {
	return m.navFrom(ks, m.curAgentKey())
}

// navFrom walks from the agent with this key.
func (m Model) navFrom(ks, from string) (tea.Model, tea.Cmd, bool) {
	d := map[string]int{"alt+down": 1, "alt+up": -1}[ks]
	if d == 0 {
		return m, nil, false
	}
	agents, _ := m.sideAgents()
	if len(agents) == 0 {
		m.setStatus(T("no agents to go to"), false)
		return m, nil, true
	}
	i := -1
	for j, r := range agents {
		if r.key() == from {
			i = j
		}
	}
	if i < 0 && d < 0 {
		i = 0
	}
	r := agents[(i+d+len(agents))%len(agents)]
	return m, m.openAgent(r.sess, nil, r.local), true
}
