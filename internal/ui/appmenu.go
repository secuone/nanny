package ui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// The app menu (≡ under the spaces, the logo, or m) holds what you set once
// and rarely touch, so the views bar keeps only the views: settings, keys,
// how much shows, which views and features are on, config, detach.

// appMenuKey is the clickable marker of the ≡ menu button.
const appMenuKey = "@menu"

// model runs a Model method that returns a new model as a menu action.
func model(f func(m Model) (tea.Model, tea.Cmd)) func(m *Model) tea.Cmd {
	return func(m *Model) tea.Cmd {
		mm, c := f(*m)
		*m = mm.(Model)
		return c
	}
}

// radio is a menu label of one choice among several.
func radio(on bool, label string) string {
	if on {
		return "● " + label
	}
	return "○ " + label
}

func (m *Model) appMenu(x, y int) {
	sub := func(open func(m *Model, x, y int)) func(m *Model) tea.Cmd {
		return func(m *Model) tea.Cmd { open(m, x, y); return nil }
	}
	items := []menuItem{
		{T("Settings"), tabKey(tabSettings), model(func(m Model) (tea.Model, tea.Cmd) { return m.switchTab(tabSettings) })},
		{T("Keys and help"), "?", func(m *Model) tea.Cmd { m.help = true; return nil }},
		{T("Theme…"), "T", func(m *Model) tea.Cmd { m.openThemes(); return nil }},
		{T("Appearance…"), "a", func(m *Model) tea.Cmd { m.openLook(true); return nil }},
		{T("Classroom in 3D (browser)"), "9", func(m *Model) tea.Cmd { return m.openClassroom() }},
		sep,
		{Tf("Screen profile: %s", profileLabel(m.cfg.ActiveProfile())) + " ▸", "M", sub((*Model).profileSubmenu)},
		{T("Customize: what shows and what runs…"), "=", func(m *Model) tea.Cmd { m.openCustomize(); return nil }},
		{T("Alerts inbox"), "A", func(m *Model) tea.Cmd { m.showInbox = true; m.inbox.cur = 0; return nil }},
		sep,
		{T("Manage spaces"), "W", func(m *Model) tea.Cmd { m.openManager(); return nil }},
		{T("Discover places"), "D", func(m *Model) tea.Cmd { return m.discoverCmd() }},
		{T("Rescan everything"), "R", func(m *Model) tea.Cmd { return m.rescan() }},
		{T("Edit config file"), "e", func(m *Model) tea.Cmd { return m.editConfig() }},
		{T("Reload config"), "r", func(m *Model) tea.Cmd { return func() tea.Msg { return configEditedMsg{} } }},
		{T("Welcome guide: what each view is, first steps"), "g", func(m *Model) tea.Cmd { m.welcome = &welcome{}; return nil }},
		{T("Setup wizard…"), "", func(m *Model) tea.Cmd { m.openSetup(setupIntro); return nil }},
		sep,
		{T("Detach: quit, panes keep running"), "d", model(func(m Model) (tea.Model, tea.Cmd) { return m.confirmQuit(false) })},
		{T("Quit and stop every pane"), "Q", model(func(m Model) (tea.Model, tea.Cmd) { return m.confirmQuit(true) })},
	}
	m.openMenu("≡ nanny", x, y, items)
}

// stay runs a change and opens the same menu again on the same item, so
// several things can be switched in a row.
func stay(open func(m *Model, x, y int), x, y, cur int, change func(m *Model) tea.Cmd) func(m *Model) tea.Cmd {
	return func(m *Model) tea.Cmd {
		cmd := change(m)
		open(m, x, y)
		m.menu.cur = cur
		return cmd
	}
}

// back is the first item of a submenu: the app menu again.
func back(x, y int) menuItem {
	return menuItem{"◂ " + T("back"), "b", func(m *Model) tea.Cmd { m.appMenu(x, y); return nil }}
}

// profileSubmenu is the profile menu inside the app menu, with a way back.
func (m *Model) profileSubmenu(x, y int) {
	m.profileMenu(x, y)
	m.menu.items = append([]menuItem{back(x, y)}, m.menu.items...)
}
