package ui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const doubleClick = 400 * time.Millisecond

var (
	keyUp    = keyPress("up")
	keyDown  = keyPress("down")
	keyEnter = keyPress("enter")
)

type mouseAction int

const (
	mousePress mouseAction = iota
	mouseRelease
	mouseMotion
)

// mouseEv is a pointer event, 0-based like the screen.
type mouseEv struct {
	X, Y   int
	Button tea.MouseButton
	Action mouseAction
}

func toMouseEv(e tea.MouseMsg) mouseEv {
	a := mousePress
	switch e.Action {
	case tea.MouseActionRelease:
		a = mouseRelease
	case tea.MouseActionMotion:
		a = mouseMotion
	}
	return mouseEv{X: e.X, Y: e.Y, Button: e.Button, Action: a}
}

// double reports whether this click repeats the previous one on the same target.
func (m Model) double(target string) bool {
	d := m.vs.clickKey == target && time.Since(m.vs.clickAt) < doubleClick
	m.vs.clickKey, m.vs.clickAt = target, time.Now()
	if d {
		m.vs.clickKey = ""
	}
	return d
}

// overlayMouse handles the mouse while a popup or the viewer is open; ok is
// false when nothing is on top of the dashboards.
func (m Model) overlayMouse(e mouseEv) (tea.Model, tea.Cmd, bool) {
	wheel := 0
	switch e.Button {
	case tea.MouseButtonWheelUp:
		wheel = -1
	case tea.MouseButtonWheelDown:
		wheel = 1
	}
	step := func(fn func(tea.KeyMsg) (tea.Model, tea.Cmd)) (tea.Model, tea.Cmd, bool) {
		k := keyDown
		if wheel < 0 {
			k = keyUp
		}
		var mm tea.Model = m
		var cmd tea.Cmd
		for i := 0; i < 3; i++ {
			mm, cmd = fn(k)
			m = mm.(Model)
		}
		return m, cmd, true
	}
	switch {
	case m.modal != nil || m.themes != nil || m.answer != nil || m.task != nil:
		return m, nil, true
	case m.help:
		if wheel != 0 {
			return step(m.helpKey)
		}
		if e.Button == tea.MouseButtonLeft {
			m.help, m.helpOff = false, 0
		}
		return m, nil, true
	case m.pal != nil, m.showInbox, m.disc != nil, m.mgr != nil, m.cust != nil:
		keyFn := m.key
		if wheel != 0 {
			k := keyDown
			if wheel < 0 {
				k = keyUp
			}
			mm, cmd := keyFn(k)
			return mm, cmd, true
		}
		i := e.Y - m.vs.modalY
		if e.Button != tea.MouseButtonLeft || i < 0 || i >= m.vs.modalN {
			return m, nil, true
		}
		idx := m.vs.modalStart + i
		switch {
		case m.pal != nil:
			m.pal.cur = idx
		case m.showInbox:
			m.inbox.cur = idx
		case m.disc != nil:
			m.disc.cur = idx
		case m.mgr != nil:
			m.mgr.cur = idx
		case m.cust != nil: // a click switches it
			m.cust.cur = idx
			mm, cmd := m.key(keyPress("space"))
			return mm, cmd, true
		}
		if m.double(fmt.Sprintf("modal/%d", idx)) {
			mm, cmd := m.key(keyEnter)
			return mm, cmd, true
		}
		return m, nil, true
	case m.libShown() && m.lib.ed != nil:
		return m, nil, true
	case m.libShown() && e.Y >= bodyY && e.X >= m.sideW():
		if m.lib.ed != nil {
			return m, nil, true
		}
		if wheel != 0 {
			return step(m.promptsKey)
		}
		if y := e.Y - m.vs.listY0; e.Button == tea.MouseButtonLeft && y >= 0 && y < len(m.vs.listRows) {
			idx := m.vs.listRows[y]
			m.lib.cur = idx
			if m.double(fmt.Sprintf("prompt/%d", idx)) {
				mm, cmd := m.promptsKey(keyEnter)
				return mm, cmd, true
			}
		}
		return m, nil, true
	case m.onTab(tabTasks) && e.Y >= bodyY && e.X >= m.sideW():
		if wheel != 0 {
			return step(m.queueKey)
		}
		if y := e.Y - m.vs.listY0; e.Button == tea.MouseButtonLeft && y >= 0 && y < len(m.vs.listRows) {
			rows := m.tq.rows()
			if idx := m.vs.listRows[y]; idx < len(rows) {
				m.tq.sel = rows[idx].ID
				if m.double(fmt.Sprintf("queue/%d", idx)) {
					mm, cmd := m.queueKey(keyEnter)
					return mm, cmd, true
				}
			}
		}
		return m, nil, true
	case m.rev != nil:
		if wheel != 0 {
			m.rev.off = max(0, min(m.rev.off+3*wheel, len(m.rev.diff)-1))
		}
		return m, nil, true
	case m.view != nil:
		if wheel != 0 {
			return step(m.viewerKey)
		}
		return m, nil, true
	}
	return m, nil, false
}

// listClick selects the dashboard row under the pointer; a double click opens it.
func (m Model) listClick(e mouseEv) (tea.Model, tea.Cmd) {
	if m.hasSecs() && e.Y == secY {
		f := m.feat(m.tab)
		var cmds []tea.Cmd
		for _, h := range m.vs.secHits {
			if e.X < h.x0 || e.X >= h.x1 {
				continue
			}
			if f != nil {
				cmds = append(cmds, f.setSec(&m, h.idx))
			} else {
				m.radar.setSec(&m, h.idx-m.radar.sec)
			}
		}
		return m, tea.Batch(append(cmds, m.refreshDetail())...)
	}
	var cmd tea.Cmd
	idx := -1
	if y := e.Y - m.vs.listY0; y >= 0 && y < len(m.vs.listRows) {
		idx = m.vs.listRows[y]
	}
	switch m.tab {
	case tabLive:
		if idx < 0 || idx >= len(m.live.rows) {
			return m, nil
		}
		m.live.cur.idx = idx
		m.live.markSeen(&m)
		if r := m.live.rows[idx]; r.fold > 0 { // one click unfolds
			m.live.toggleFold(&m, r.sess.ID)
			return m, m.refreshDetail()
		}
	case tabSessions:
		if idx < 0 || idx >= len(m.sess.rows) {
			return m, nil
		}
		m.sess.filter.Blur()
		m.sess.cur.idx = idx
	case tabSkills:
		if idx < 0 || idx >= len(m.skills.rows) {
			return m, nil
		}
		m.skills.filter.Blur()
		m.skills.cur.idx = idx
	case tabRadar:
		if idx < 0 || idx >= len(m.radar.items) || m.radar.head[idx] {
			return m, nil
		}
		m.radar.cur.idx = idx
	case tabSettings:
		if idx < 0 || m.settings.editing != nil {
			return m, nil
		}
		m.settings.cur.idx = idx
	default:
		f := m.feat(m.tab)
		if f == nil || idx < 0 {
			return m, nil
		}
		cmd = f.click(&m, idx)
	}
	m.detailOff = 0
	if m.double(fmt.Sprintf("list/%d/%d", m.tab, idx)) {
		return m.key(keyEnter)
	}
	return m, tea.Batch(cmd, m.refreshDetail())
}
