package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/secuone/nanny/internal/config"
)

// The compact layout (Detail off, as in the basic profile) trims nanny down
// to what herdr-style tools show: spaces, panes and one line per agent.

func (m Model) basic() bool { return !m.cfg.On(config.LayoutDetail) }

// full shows everything, repeats included (Extras on).
func (m Model) full() bool { return !m.basic() && m.cfg.On(config.LayoutExtras) }

// barTabs are the views on the views bar (and in the tab cycle): all those
// turned on, plus the one on screen.
func (m Model) barTabs() []tab {
	var out []tab
	for i := range tabNames {
		t := tab(i)
		if !m.viewOn(t) {
			continue
		}
		hidden := t == tabSettings // Settings is in the ≡ menu
		if !hidden || t == m.tab && m.dashShown() {
			out = append(out, t)
		}
	}
	return out
}

// viewOn reports whether a view is turned on; off, it is gone from the bar,
// its number and the palette. Settings always is.
func (m Model) viewOn(t tab) bool {
	return t == tabSettings || m.cfg.On(viewID(t))
}

// viewID is a view's id in the config's off list: "view.monitor".
func viewID(t tab) string {
	id, _, _ := strings.Cut(tabNames[t], " ") // "Git & PRs" is view.git
	return "view." + strings.ToLower(id)
}

// homeTab is the first view turned on: Live unless it is off.
func (m Model) homeTab() tab {
	for i := range tabNames {
		if m.viewOn(tab(i)) {
			return tab(i)
		}
	}
	return tabSettings
}

// toLive shows Live for an agent; with Live turned off it says so instead.
func (m *Model) toLive() bool {
	if !m.viewOn(tabLive) {
		m.setStatus(Tf("%s is turned off · = customizes", T(tabNames[tabLive])), false)
		return false
	}
	m.show(tabLive)
	return true
}

// noDetail is true when the home view is a single full-width list.
func (m Model) noDetail() bool {
	return m.basic() && !m.inSpace() && m.tab <= tabGit
}

// basicHeader is the quiet top bar: spaces left, plain counts right.
func (m Model) basicHeader(left string, busy, idle, blocked int) string {
	s := m.st
	var parts []string
	if blocked > 0 {
		parts = append(parts, clickable("@only:blocked", s.Red.Bold(true).Render("● "+Tf("%d need you", blocked))))
	}
	if n := m.waitingElsewhere(); n > 0 && m.spaceScope() != nil {
		parts = append(parts, clickable(allSpacesKey, s.Red.Render(fmt.Sprintf("+%d ", n))+s.Muted.Render(T("waiting elsewhere"))))
	}
	if busy > 0 && m.cfg.On("header.counts") {
		parts = append(parts, clickable("@only:working", s.Yellow.Render(fmt.Sprintf("● %d", busy))+s.Muted.Render(" "+T("working"))))
	}
	if idle > 0 && m.cfg.On("header.counts") {
		parts = append(parts, clickable("@only:idle", s.Muted.Render("○ "+fmt.Sprint(idle)+" "+T("idle"))))
	}
	if c := m.todaySpend(); c > 0 && m.cfg.On("header.cost") {
		parts = append(parts, s.Muted.Render(money(c)))
	}
	if !m.cfg.Alerts.Sound && m.cfg.On("header.sound") {
		parts = append(parts, s.Muted.Render("✕♪"))
	}
	return m.headerLine(left, strings.Join(parts, "  "), "")
}

// headerLine puts left and right on the top bar, then the profile word when
// it fits; to make room for it the clock (drop) goes first.
func (m Model) headerLine(left, right, drop string) string {
	right = strings.TrimRight(right, " ")
	chip := m.profileChip()
	room := func(r string) bool { return m.w-lipgloss.Width(left)-lipgloss.Width(r)-lipgloss.Width(chip)-2 > 1 }
	if !room(right) && drop != "" && strings.HasSuffix(right, drop) {
		if r := strings.TrimRight(strings.TrimSuffix(right, drop), " "); room(r) {
			right = r
		}
	}
	if room(right) {
		if right != "" {
			right += "  "
		}
		right += chip
	}
	right += " "
	gap := m.w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return fit(left, m.w)
	}
	return left + strings.Repeat(" ", gap) + right
}

// agentLine is an agent in one row: state, title, where, subagents, state word.
func (m Model) agentLine(r liveRow, w int) string {
	s := m.st
	state := r.state()
	title, cwd := r.titleCwd()
	if title == "" {
		title = T("(untitled)")
	}
	right := m.stateLabel(state)
	if p := r.prompt(); p != nil {
		right = s.Yellow.Render("⚠ "+trunc(promptSummary(p), 24)) + "  " + right
	}
	where := project(cwd)
	if r.local != nil {
		where = r.local.space.Name + "/" + r.local.tab.Label
	}
	mid := s.Muted.Render(trunc(where, 22))
	if r.run > 0 {
		mid += s.Muted.Render(fmt.Sprintf("  ↳%d", r.run))
	}
	head := m.stateGlyph(state) + " "
	room := w - lipgloss.Width(head) - lipgloss.Width(mid) - lipgloss.Width(right) - 4
	if r.sess != nil { // keep room for the title before the gauge
		if g := m.fitMeter(r.sess.Usage, room-2-16); g != "" {
			right = g + "  " + right
			room -= lipgloss.Width(g) + 2
		}
	}
	return fit(head+fit(s.Bold.Render(trunc(title, room)), room)+"  "+mid, w-lipgloss.Width(right)) + right
}
