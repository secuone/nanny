package ui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/secuone/nanny/internal/config"
)

// Screen profiles: basic (the bare minimum), full (everything) and your own,
// each a set of Customize switches. M walks them; the quiet word at the end
// of the top bar is the one in use: a click lists them, the wheel walks them.

const profileChipKey = "@profile"

// profileLabel is a profile's name as shown: the built-in ones translated.
func profileLabel(name string) string {
	switch name {
	case config.ProfileBasic:
		return T("Basic")
	case config.ProfileFull:
		return T("Full")
	}
	return name
}

// profileChip is the profile in use, dimmed, clickable.
func (m Model) profileChip() string {
	return clickable(profileChipKey, m.st.Muted.Render(trunc(profileLabel(m.cfg.ActiveProfile()), 14)))
}

// cycleProfile puts on the next (d=1) or previous (d=-1) profile.
func (m *Model) cycleProfile(d int) tea.Cmd {
	m.cfg.CycleProfile(d)
	return m.profileChanged()
}

// useProfile puts a profile on.
func (m *Model) useProfile(name string) tea.Cmd {
	m.cfg.Profile = name
	return m.profileChanged()
}

// profileChanged lays nanny out again for the profile now in use and saves it.
func (m *Model) profileChanged() tea.Cmd {
	m.focusR, m.detailOff = false, 0
	if !m.viewOn(m.tab) {
		m.tab = m.homeTab()
	}
	m.live.rebuild(m)
	cmd := m.saveConfig()
	m.setStatus(Tf("profile: %s · M next · = customizes", profileLabel(m.cfg.ActiveProfile())), false)
	return tea.Batch(cmd, m.refreshDetail())
}

// profileMenu lists the profiles (the one in use marked), and edits or makes one.
func (m *Model) profileMenu(x, y int) {
	var items []menuItem
	for i, n := range m.cfg.ProfileNames() {
		n, k := n, ""
		if i < 9 {
			k = fmt.Sprint(i + 1)
		}
		items = append(items, menuItem{radio(m.cfg.ActiveProfile() == n, profileLabel(n)), k, func(m *Model) tea.Cmd { return m.useProfile(n) }})
	}
	items = append(items, sep,
		menuItem{T("Edit this profile…"), "=", func(m *Model) tea.Cmd { m.openCustomize(); return nil }},
		menuItem{T("New profile…"), "N", func(m *Model) tea.Cmd {
			m.openCustomize()
			m.custNewName()
			return nil
		}})
	m.openMenu(T("Screen profile"), x, y, items)
}

// custNewName asks the name of a new profile, a copy of the one in use.
func (m *Model) custNewName() { m.custAsk(false, "") }

// custAsk opens the name line of the customize screen: a new profile, or a
// new name for the one in use.
func (m *Model) custAsk(rename bool, value string) {
	in := newInput()
	in.Prompt = "› "
	in.CharLimit = 24
	in.SetValue(value)
	in.CursorEnd()
	in.Focus()
	m.cust.name, m.cust.rename = &in, rename
}

// custName handles the name line: ⏎ makes or renames, esc drops it.
func (m Model) custName(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	c := m.cust
	switch k.String() {
	case "esc":
		c.name = nil
		return m, nil
	case "enter":
		name := c.name.Value()
		var err error
		if c.rename {
			_, err = m.cfg.RenameProfile(m.cfg.ActiveProfile(), name)
		} else {
			_, err = m.cfg.NewProfile(name, m.cfg.ActiveProfile())
		}
		if err != nil {
			m.setStatus(err.Error(), true)
			return m, nil
		}
		c.name = nil
		return m, m.profileChanged()
	}
	in, cmd := c.name.Update(k)
	c.name = &in
	return m, cmd
}

// custDelete deletes the profile in use after a second X.
func (m Model) custDelete() (tea.Model, tea.Cmd) {
	cur := m.cfg.ActiveProfile()
	if config.BuiltinProfile(cur) {
		m.setStatus(Tf("%s is built in: it can't be deleted", profileLabel(cur)), true)
		return m, nil
	}
	if !m.cust.del {
		m.cust.del = true
		m.setStatus(Tf("X again deletes %s", cur), false)
		return m, nil
	}
	m.cust.del = false
	_ = m.cfg.DeleteProfile(cur)
	return m, m.profileChanged()
}

// onProfileChip says whether (x, y) is on the profile word of the top bar.
func (m Model) onProfileChip(x, y int) bool {
	for _, h := range m.vs.keyHits {
		if h.key == profileChipKey && y == h.y && x >= h.x0 && x < h.x1 {
			return true
		}
	}
	return false
}
