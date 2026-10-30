package ui

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/secuone/nanny/internal/term"
	"github.com/secuone/nanny/internal/ws"
)

// The mouse beyond clicks: hovering a tab shows what is in it, right-click
// opens a menu of what can be done there, and tabs can be dragged around.

// hoverAt is the tab under the pointer: kind term (a space's terminal tab),
// space (row 0) or view (row 1); x is its left edge.
type hoverAt struct {
	kind string
	idx  int
	x    int
}

type dragFrom struct {
	kind string
	idx  int
}

// spaceLabel is a space's name with its icon, if it has one.
func spaceLabel(sp *ws.Space) string {
	if sp.Icon != "" {
		return sp.Icon + " " + sp.Name
	}
	return sp.Name
}

// hoverTarget finds the tab under (x, y), if any.
func (m Model) hoverTarget(x, y int) hoverAt {
	if !m.noModal() || m.menu != nil || m.view != nil || m.rev != nil || m.welcome != nil {
		return hoverAt{}
	}
	find := func(hits []hit, kind string) hoverAt {
		for _, h := range hits {
			if x >= h.x0 && x < h.x1 && (h.idx >= 0 || h.idx == tabTerm || kind == "term") {
				return hoverAt{kind, h.idx, h.x0}
			}
		}
		return hoverAt{}
	}
	switch {
	case y == 0:
		return find(m.vs.spaceHits, "space")
	case y == viewY:
		return find(m.vs.tabHits, "view")
	case y == termY && m.inSpace() && !m.dash:
		return find(m.vs.termHits, "term")
	case y == secY && m.hasSecs() && m.view == nil:
		return find(m.vs.secHits, "sec")
	}
	return hoverAt{}
}

// hoverMove follows the pointer. Most moves change nothing on screen, so the
// last frame is reused instead of drawn again.
func (m Model) hoverMove(e mouseEv) (tea.Model, tea.Cmd) {
	h := m.hoverTarget(e.X, e.Y)
	if h == m.vs.hover {
		m.vs.reuse = true
		return m, nil
	}
	m.vs.hover = h
	return m, nil
}

// dropTab ends a drag: a tab let go over another one takes its place.
func (m Model) dropTab(e mouseEv) (tea.Model, tea.Cmd) {
	d := m.vs.drag
	m.vs.drag = dragFrom{}
	if d.kind == "" {
		return m, nil
	}
	h := m.hoverTarget(e.X, e.Y)
	if h.kind != d.kind || h.idx < 0 || h.idx == d.idx {
		return m, nil
	}
	switch d.kind {
	case "term":
		if sp := m.space(); sp != nil && d.idx < len(sp.Tabs) && h.idx < len(sp.Tabs) {
			m.moveTab(sp, d.idx, h.idx)
		}
	case "space":
		if d.idx < len(m.spaces) && h.idx < len(m.spaces) {
			m.moveSpace(d.idx, h.idx)
		}
	}
	return m, nil
}

// moveTab puts tab from at position to, keeping it active.
func (m *Model) moveTab(sp *ws.Space, from, to int) {
	if from < 0 || to < 0 || from >= len(sp.Tabs) || to >= len(sp.Tabs) || from == to {
		return
	}
	t := sp.Tabs[from]
	sp.Tabs = append(sp.Tabs[:from], sp.Tabs[from+1:]...)
	sp.Tabs = append(sp.Tabs[:to], append([]*ws.Tab{t}, sp.Tabs[to:]...)...)
	sp.Active = to
	m.layoutPanes()
	m.saveSpaces()
}

// moveSpace puts space from at position to; the current space stays current.
func (m *Model) moveSpace(from, to int) {
	if from < 0 || to < 0 || from >= len(m.spaces) || to >= len(m.spaces) || from == to {
		return
	}
	var cur *ws.Space
	if m.inSpace() {
		cur = m.space()
	}
	sp := m.spaces[from]
	m.spaces = append(m.spaces[:from], m.spaces[from+1:]...)
	m.spaces = append(m.spaces[:to], append([]*ws.Space{sp}, m.spaces[to:]...)...)
	for i, s := range m.spaces {
		if s == cur {
			m.cur = i
		}
	}
	m.saveSpaces()
}

// rename asks for a new name with the current one already typed.
func (m *Model) rename(title, cur string, set func(v string)) {
	m.prompt(title, "", cur, func(m *Model, v string) tea.Cmd {
		if v != "" {
			set(v)
			m.saveSpaces()
		}
		return nil
	})
	m.modal.input.SetValue(cur)
	m.modal.input.CursorEnd()
}

// ---------------------------------------------------------------- tooltips

// viewDesc says what a view is for, in one line.
func viewDesc(t tab) string {
	switch t {
	case tabLive:
		return T("agents running now: state, subagents, context · answer their prompts")
	case tabSessions:
		return T("every past conversation: search, read, resume or fork")
	case tabGit:
		return T("repos and git worktrees: changes, PRs, a new agent in one")
	case tabTasks:
		return T("a backlog handed to idle agents, one task at a time")
	case tabPrompts:
		return T("saved and most used prompts, inserted with ⏎")
	case tabSkills:
		return T("skills, commands, agents and the harness: hooks, MCP servers, plugins, CLAUDE.md, settings")
	case tabRadar:
		return T("everything else: Hermes, jobs, cron, open ports, containers, Tilt, this machine")
	case tabSettings:
		return T("theme, language, sources, sounds, prefix key")
	case tabJira:
		return T("your Jira issues: change their status, comment, start work on one in a worktree")
	case tabInsights:
		return T("what your Claude usage costs and does: tokens, tools, permissions, edits")
	}
	return T("this space's terminal tabs and panes")
}

// viewTooltip is the box under the hovered tab, and where it goes.
func (m Model) viewTooltip() (string, int, int) {
	h := m.vs.hover
	s := m.st
	var ls []string
	hint := ""
	switch h.kind {
	case "term":
		sp := m.space()
		if sp == nil || h.idx < 0 || h.idx >= len(sp.Tabs) { // the + buttons say it on their face
			return "", 0, 0
		}
		t := sp.Tabs[h.idx]
		ls = append(ls, s.Bold.Render(fmt.Sprintf("%d  %s", h.idx+1, t.Label))+"  "+m.stateLabel(m.tabState(t)))
		for i, p := range t.Panes {
			ls = append(ls, m.paneLines(t, p, i, len(t.Panes) > 1)...)
		}
		hint = T("click open · double-click rename · right-click menu · drag to move")
	case "space":
		if h.idx < 0 || h.idx >= len(m.spaces) {
			if h.idx == -1 {
				ls = append(ls, s.Bold.Render("⊞ "+T("all spaces")), s.Muted.Render(T("every agent, session and worktree, all spaces together")))
			}
			break
		}
		sp := m.spaces[h.idx]
		ls = append(ls, s.Bold.Render(spaceLabel(sp))+"  "+m.stateLabel(m.spaceState(sp)))
		for _, p := range sp.Paths {
			l := s.Peach.Render(tilde(p))
			if br := branchOf(p); br != "" {
				l += " " + s.Blue.Render("⎇ "+br)
			}
			ls = append(ls, l)
		}
		if len(sp.Tabs) == 0 {
			ls = append(ls, s.Muted.Render(T("no tabs yet")))
		}
		for i, t := range sp.Tabs {
			ls = append(ls, fmt.Sprintf("%s %s %s", s.Muted.Render(fmt.Sprint(i+1)), m.paneGlyph(m.tabState(t)), s.Text.Render(t.Label))+
				s.Muted.Render("  "+Tn(len(t.Panes), "%d pane", "%d panes")))
		}
		hint = T("click open · right-click rename, icon, move · drag to move")
	case "view":
		t := tab(h.idx)
		name, key := T("terminal"), "0"
		if h.idx != tabTerm {
			name, key = T(tabNames[t]), tabKey(t)
		}
		ls = append(ls, s.Bold.Render(name)+s.Muted.Render("  · "+T("key")+" ")+s.Key.Render(key), s.Text.Render(viewDesc(t)))
		if h.idx == tabTerm && !m.inSpace() && len(m.spaces) > 0 {
			ls = append(ls, s.Peach.Render(Tf("click: %s", spaceLabel(m.spaces[0]))))
		}
		if sp := m.spaceScope(); sp != nil && h.idx != tabTerm {
			ls = append(ls, s.Peach.Render("⌖ "+Tf("only %s", spaceLabel(sp))))
		}
	}
	if len(ls) == 0 {
		return "", 0, 0
	}
	w := 0
	for _, l := range ls {
		w = max(w, lipgloss.Width(l))
	}
	if hint != "" {
		w = max(w, lipgloss.Width(hint))
		ls = append(ls, s.Dim.Render(strings.Repeat("─", w)), s.Muted.Render(trunc(hint, w)))
	}
	w = min(w, m.w-6)
	for i, l := range ls {
		ls[i] = fit(l, w)
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(s.P.Overlay)).Padding(0, 1).Render(strings.Join(ls, "\n"))
	x := max(0, min(h.x, m.w-lipgloss.Width(box)))
	y := map[string]int{"space": 1, "view": viewY + 1, "term": termY + 1}[h.kind]
	return box, x, y
}

// paneLines describes one pane of a tab: program, folder, branch and, for
// Claude, its session and context.
func (m Model) paneLines(t *ws.Tab, p *term.Pane, i int, many bool) []string {
	s := m.st
	st := m.paneState(p)
	what := paneTitle(p)
	if fg := p.ForegroundPid(); st == "running" && fg != 0 {
		if b, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", fg)); err == nil {
			what = strings.TrimSpace(string(b))
		}
	}
	l1 := m.paneGlyph(st) + " " + s.Text.Render(trunc(what, 30))
	if many {
		l1 = s.Muted.Render(fmt.Sprintf("%d ", i+1)) + l1
	}
	l1 += "  " + s.Peach.Render(tilde(p.Cwd))
	if br := branchOf(p.Cwd); br != "" {
		l1 += " " + s.Blue.Render("⎇ "+br)
	}
	ls := []string{l1}
	var l2 []string
	switch {
	case p.Exited():
		l2 = append(l2, Tf("exited (code %d)", p.ExitCode()))
	case st == "shell":
		l2 = append(l2, T("shell, waiting for a command"))
	}
	if ses := m.byID[t.SessionIDs[p.ID]]; ses != nil && p.Agent == "claude" {
		if ses.Title != "" {
			l2 = append(l2, "“"+trunc(ses.Title, 40)+"”")
		}
		if u := m.usageOf(ses); u != nil {
			l2 = append(l2, s.ctxStyle(u.Pct()).Render(Tf("context %d%%", u.Pct()))+s.Muted.Render(" · "+money(u.Cost())))
		}
	}
	if d := p.IdleFor(); !p.Exited() && d.Seconds() >= 60 {
		l2 = append(l2, Tf("quiet %s", ago(time.Now().Add(-d))))
	}
	if len(l2) > 0 {
		ls = append(ls, "   "+s.Muted.Render(strings.Join(l2, " · ")))
	}
	return ls
}

// ---------------------------------------------------------------- menus

// ctxMenu is a small popup of actions where the mouse was right-clicked.
// With cols > 1 the items are a grid (the icon picker).
type ctxMenu struct {
	title string
	items []menuItem
	cur   int
	x, y  int
	cols  int
}

type menuItem struct {
	label, key string // label "" is a separator
	run        func(m *Model) tea.Cmd
}

// menuHit is where one item landed on screen.
type menuHit struct {
	y, x0, x1, idx int
}

func (mn *ctxMenu) move(d int) {
	for n := 0; n < len(mn.items); n++ {
		mn.cur = (mn.cur + d + len(mn.items)) % len(mn.items)
		if mn.items[mn.cur].label != "" {
			return
		}
	}
}

func (m *Model) openMenu(title string, x, y int, items []menuItem) {
	m.menu = &ctxMenu{title: title, items: items, x: x, y: y, cols: 1}
	if items[0].label == "" {
		m.menu.move(1)
	}
	m.vs.hover = hoverAt{}
}

// pfx runs a prefix command as a menu action.
func pfx(k string) func(m *Model) tea.Cmd {
	return func(m *Model) tea.Cmd {
		mm, c := m.prefixCmd(k)
		*m = mm.(Model)
		return c
	}
}

// press runs a plain key as a menu action (the same keys the list takes).
func press(k string) func(m *Model) tea.Cmd {
	return func(m *Model) tea.Cmd {
		km, ok := hintKey(k)
		if !ok {
			return nil
		}
		mm, c := m.key(km)
		*m = mm.(Model)
		return c
	}
}

var sep = menuItem{}

// termTabMenu is the right-click menu of terminal tab i.
func (m *Model) termTabMenu(i, x int) {
	sp := m.space()
	if sp == nil || i >= len(sp.Tabs) {
		return
	}
	t := sp.Tabs[i]
	on := func(k string) func(m *Model) tea.Cmd {
		return func(m *Model) tea.Cmd {
			sp.Active, m.dash = i, false
			m.layoutPanes()
			return pfx(k)(m)
		}
	}
	items := []menuItem{
		{T("Rename…"), ",", func(m *Model) tea.Cmd {
			m.rename(T("Rename tab"), t.Label, func(v string) { t.Label = v })
			return nil
		}},
		sep,
		{T("Split right"), "v", on("v")},
		{T("Split down"), "-", on("-")},
		{T("Split right with Claude"), "V", on("V")},
	}
	if len(t.Panes) > 1 {
		items = append(items, menuItem{T("Zoom pane"), "z", on("z")})
	}
	items = append(items, sep)
	if i > 0 {
		items = append(items, menuItem{T("Move left"), "<", func(m *Model) tea.Cmd { m.moveTab(sp, i, i-1); return nil }})
	}
	if i < len(sp.Tabs)-1 {
		items = append(items, menuItem{T("Move right"), ">", func(m *Model) tea.Cmd { m.moveTab(sp, i, i+1); return nil }})
	}
	dir := sp.Dir()
	if p := t.Pane(); p != nil {
		dir = p.Cwd
	}
	items = append(items,
		menuItem{T("Review changes"), "g", on("g")},
		menuItem{T("Copy folder"), "y", func(m *Model) tea.Cmd { return m.copy(dir, T("folder")) }},
		sep,
		menuItem{T("Close tab"), "X", on("X")},
	)
	m.openMenu(fmt.Sprintf("%d  %s", i+1, t.Label), x, termY+1, items)
}

// paneMenu is the right-click menu of pane i of the active tab.
func (m *Model) paneMenu(i, x, y int) {
	sp := m.space()
	t := sp.Tab()
	if t == nil || i >= len(t.Panes) {
		return
	}
	t.Focus = i
	p := t.Panes[i]
	items := []menuItem{
		{T("Split right"), "v", pfx("v")},
		{T("Split down"), "-", pfx("-")},
		{T("Split right with Claude"), "V", pfx("V")},
	}
	if len(t.Panes) > 1 {
		zoom := T("Zoom pane")
		if t.Zoom {
			zoom = T("Unzoom")
		}
		items = append(items, menuItem{zoom, "z", pfx("z")})
	}
	items = append(items,
		sep,
		menuItem{T("New Claude tab"), "c", pfx("c")},
		menuItem{T("New shell tab"), "s", pfx("s")},
		menuItem{T("Review changes"), "g", pfx("g")},
		menuItem{T("Copy folder"), "y", func(m *Model) tea.Cmd { return m.copy(p.Cwd, T("folder")) }},
		sep,
		menuItem{T("Close pane"), "x", pfx("x")},
	)
	m.openMenu(paneTitle(p), x, y, items)
}

// spaceMenu is the right-click menu of space i (top row or sidebar).
func (m *Model) spaceMenu(i, x, y int) {
	if i < 0 || i >= len(m.spaces) {
		return
	}
	sp := m.spaces[i]
	open := func(m *Model) tea.Cmd {
		m.cur = m.indexOf(sp)
		m.dash = false
		m.layoutPanes()
		return m.refreshDetail()
	}
	items := []menuItem{
		{T("Open"), "⏎", open},
		{T("Its views (Live, Sessions…)"), "i", func(m *Model) tea.Cmd {
			c := open(m)
			mm, c2 := m.switchTab(m.tab)
			*m = mm.(Model)
			return tea.Batch(c, c2)
		}},
		sep,
		{T("Rename…"), "r", func(m *Model) tea.Cmd {
			m.rename(T("Rename space"), sp.Name, func(v string) { sp.Name = v })
			return nil
		}},
		{T("Icon…"), "e", func(m *Model) tea.Cmd { m.iconPicker(sp, x, y); return nil }},
	}
	if i > 0 {
		items = append(items, menuItem{T("Move left"), "<", func(m *Model) tea.Cmd { m.moveSpace(i, i-1); return nil }})
	}
	if i < len(m.spaces)-1 {
		items = append(items, menuItem{T("Move right"), ">", func(m *Model) tea.Cmd { m.moveSpace(i, i+1); return nil }})
	}
	mgr := func(k string) func(m *Model) tea.Cmd {
		return func(m *Model) tea.Cmd {
			m.openManager()
			m.mgr.cur = m.indexOf(sp)
			km, _ := hintKey(k)
			mm, c := m.managerKey(km)
			*m = mm.(Model)
			m.mgr = nil
			return c
		}
	}
	if g := m.spaceGitItems(sp); len(g) > 0 { // open its PR, pull, fetch
		items = append(append(items, sep), g...)
	}
	items = append(items, sep,
		menuItem{T("New worktree…"), "t", func(m *Model) tea.Cmd { m.newWorktree(sp); return nil }},
		menuItem{T("Add a folder…"), "a", mgr("a")},
		menuItem{T("Manage spaces…"), "w", func(m *Model) tea.Cmd { m.openManager(); m.mgr.cur = m.indexOf(sp); return nil }},
		sep,
		menuItem{T("Delete space"), "d", mgr("d")},
	)
	m.openMenu(spaceLabel(sp), x, y, items)
}

func (m Model) indexOf(sp *ws.Space) int {
	for i, s := range m.spaces {
		if s == sp {
			return i
		}
	}
	return -1
}

// spaceIcons are the picker's choices; any emoji can be typed instead.
var spaceIcons = []string{
	"🚀", "🔥", "⭐", "💎", "🧪", "🐛", "🔧", "📦",
	"🌐", "💾", "🔒", "💳", "🛒", "📊", "📝", "🎨",
	"🤖", "🧠", "🐘", "🐍", "🦀", "🐹", "🍕", "🌱",
	"🌊", "⚡", "🎯", "🏠", "🎫", "🧭", "📱", "💻",
	"🔔", "🎉", "👾", "🦄", "🐙", "🍀", "🌈", "🧩",
}

// iconPicker is a grid of icons for a space, plus "type your own" and "none".
func (m *Model) iconPicker(sp *ws.Space, x, y int) {
	set := func(ic string) func(m *Model) tea.Cmd {
		return func(m *Model) tea.Cmd {
			sp.Icon = ic
			m.saveSpaces()
			return nil
		}
	}
	var items []menuItem
	cur := 0
	for i, ic := range spaceIcons {
		if ic == sp.Icon {
			cur = i
		}
		items = append(items, menuItem{ic, "", set(ic)})
	}
	items = append(items,
		menuItem{"✎ " + T("type one"), "", func(m *Model) tea.Cmd {
			m.prompt(Tf("Icon for %s", sp.Name), T("Any emoji or a short symbol. Empty removes it."), "🙂", func(m *Model, v string) tea.Cmd {
				sp.Icon = strings.TrimSpace(v)
				if lipgloss.Width(sp.Icon) > 4 {
					sp.Icon = trunc(sp.Icon, 4)
				}
				m.saveSpaces()
				return nil
			})
			m.modal.input.SetValue(sp.Icon)
			return nil
		}},
		menuItem{"✕ " + T("no icon"), "", set("")},
	)
	m.openMenu(Tf("Icon for %s", sp.Name), x, y, items)
	m.menu.cols, m.menu.cur = 8, cur
}

// listMenu is the right-click menu of the selected row of a list view: the
// same keys the list takes, with their names.
func (m *Model) listMenu(x, y int) {
	var items []menuItem
	add := func(k, label string) { items = append(items, menuItem{label, k, press(k)}) }
	switch m.tab {
	case tabLive:
		add("⏎", T("Go to the agent"))
		add("v", T("Read the conversation"))
		add("p", T("Type a prompt"))
		add("+", T("Queue a task in its folder"))
		items = append(items, sep)
		add("C", T("Review changes"))
		add("o", T("Shell in its folder"))
		add("c", T("Copy folder"))
		add("i", T("Copy session id"))
	case tabSessions:
		add("⏎", T("Resume"))
		add("f", T("Fork (new session from here)"))
		add("v", T("Read the conversation"))
		items = append(items, sep)
		add("o", T("Shell in its folder"))
		add("y", T("Copy resume command"))
		add("i", T("Copy session id"))
	case tabGit:
		items = m.gitv.menu(m)
	case tabRadar:
		if m.itemMenu(x, y) {
			return
		}
		add("⏎", T("Open"))
		add("L", T("Follow log"))
		add("o", T("Shell in its folder"))
		add("y", T("Copy"))
	default:
		return
	}
	m.openMenu(T("Actions"), x, y, items)
}

// rightClick opens the menu of whatever is under the pointer.
func (m Model) rightClick(e mouseEv) (tea.Model, tea.Cmd) {
	if !m.noModal() || m.view != nil || m.rev != nil {
		return m, nil
	}
	h := m.hoverTarget(e.X, e.Y)
	switch {
	case h.kind == "term" && h.idx >= 0:
		m.termTabMenu(h.idx, h.x)
		return m, nil
	case h.kind == "space" && h.idx >= 0:
		m.spaceMenu(h.idx, h.x, 1)
		return m, nil
	case e.X < m.sideW() && e.Y >= bodyY:
		if r, ok := m.sidebarHit(e.Y); ok && r.wt != nil {
			m.worktreeMenu(m.spaces[r.space], *r.wt, e.X, e.Y+1)
		} else if ok && r.isSpace && r.space >= 0 {
			m.spaceMenu(r.space, e.X, e.Y+1)
		} else if ok && r.sess != nil && r.sub == nil && m.toLive() {
			m.cur = -1
			for i, lr := range m.live.rows {
				if lr.sess == r.sess && lr.agent() {
					m.live.cur.idx = i
				}
			}
			m.listMenu(e.X, e.Y+1)
		}
		return m, nil
	case m.inSpace() && !m.dash:
		for i, r := range m.vs.rects {
			if e.X >= r.x && e.X < r.x+r.w && e.Y >= r.y && e.Y < r.y+r.h {
				m.paneMenu(i, e.X, e.Y)
				return m, nil
			}
		}
		return m, nil
	case m.dashShown() && e.Y > bodyY && e.X < m.sideW()+m.leftWidth():
		mm, cmd := m.listClick(e)
		m = mm.(Model)
		m.vs.clickKey = "" // a right-click is never half of a double click
		m.listMenu(e.X, e.Y+1)
		return m, cmd
	}
	return m, nil
}

// viewMenu draws the open menu and records where its items are.
func (m Model) viewMenu() (string, int, int) {
	s := m.st
	mn := m.menu
	m.vs.menuHits = m.vs.menuHits[:0]
	var ls []string
	inner := lipgloss.Width(mn.title)
	cell := 4
	if mn.cols > 1 {
		inner = max(inner, mn.cols*cell)
	}
	for _, it := range mn.items {
		if mn.cols == 1 || lipgloss.Width(it.label) > 2 {
			inner = max(inner, lipgloss.Width(it.label)+lipgloss.Width(it.key)+4)
		}
	}
	inner = min(inner, m.w-4)
	ls = append(ls, s.Muted.Render(trunc(mn.title, inner)), s.Dim.Render(strings.Repeat("─", inner)))
	type spot struct{ row, col, w, idx int }
	var spots []spot
	if mn.cols > 1 {
		row, col := "", 0
		flush := func() {
			if row != "" {
				ls = append(ls, row)
				row, col = "", 0
			}
		}
		for i, it := range mn.items {
			if lipgloss.Width(it.label) > 2 { // a wide item gets its own line
				flush()
				l := " " + it.label
				if i == mn.cur {
					l = s.Sel(s.RowSel, fit(l, inner))
				}
				spots = append(spots, spot{len(ls), 0, inner, i})
				ls = append(ls, fit(l, inner))
				continue
			}
			c := fit(" "+it.label+" ", cell)
			if i == mn.cur {
				c = s.Sel(s.RowSel, c)
			}
			spots = append(spots, spot{len(ls), col * cell, cell, i})
			row += c
			if col++; col == mn.cols {
				flush()
			}
		}
		flush()
	} else {
		for i, it := range mn.items {
			if it.label == "" {
				ls = append(ls, s.Dim.Render(strings.Repeat("─", inner)))
				continue
			}
			l := fit(" "+s.Text.Render(it.label), inner-lipgloss.Width(it.key)-1) + s.Key.Render(it.key) + " "
			if i == mn.cur {
				l = s.Sel(s.RowSel, l)
			}
			spots = append(spots, spot{len(ls), 0, inner, i})
			ls = append(ls, l)
		}
	}
	for i, l := range ls {
		ls[i] = fit(l, inner)
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(s.P.Accent2)).Padding(0, 1).Render(strings.Join(ls, "\n"))
	bw, bh := lipgloss.Width(box), lipgloss.Height(box)
	x := max(0, min(mn.x, m.w-bw))
	y := max(0, min(mn.y, m.h-bh))
	for _, sp := range spots {
		m.vs.menuHits = append(m.vs.menuHits, menuHit{y + 1 + sp.row, x + 2 + sp.col, x + 2 + sp.col + sp.w, sp.idx})
	}
	m.vs.menuBox = rect{x, y, bw, bh}
	return box, x, y
}

func (m Model) menuAt(x, y int) int {
	for _, h := range m.vs.menuHits {
		if y == h.y && x >= h.x0 && x < h.x1 {
			return h.idx
		}
	}
	return -1
}

func (m Model) runMenu(i int) (tea.Model, tea.Cmd) {
	it := m.menu.items[i]
	m.menu = nil
	if it.run == nil {
		return m, nil
	}
	cmd := it.run(&m)
	return m, cmd
}

func (m Model) menuMouse(e mouseEv) (tea.Model, tea.Cmd) {
	i := m.menuAt(e.X, e.Y)
	switch {
	case e.Action == mouseMotion:
		if i < 0 || i == m.menu.cur {
			m.vs.reuse = true
			return m, nil
		}
		m.menu.cur = i
	case e.Action == mouseRelease:
	case e.Button == tea.MouseWheelUp:
		m.menu.move(-1)
	case e.Button == tea.MouseWheelDown:
		m.menu.move(1)
	case i >= 0 && e.Button == tea.MouseLeft:
		return m.runMenu(i)
	case !m.vs.menuBox.has(e.X, e.Y): // a click elsewhere closes it and goes through
		m.menu = nil
		return m.mouse(e)
	}
	return m, nil
}

func (m Model) menuKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	mn := m.menu
	switch ks := k.String(); ks {
	case "esc", "q", "ctrl+c":
		m.menu = nil
	case "up", "k":
		mn.move(-mn.cols)
	case "down", "j", "tab":
		mn.move(mn.cols)
	case "left", "h":
		mn.move(-1)
	case "right", "l":
		mn.move(1)
	case "enter", " ", "space":
		return m.runMenu(mn.cur)
	default:
		for i, it := range mn.items {
			if it.key == ks && it.label != "" {
				return m.runMenu(i)
			}
		}
	}
	return m, nil
}

func (r rect) has(x, y int) bool { return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h }

// middleClose closes the terminal tab under a middle click, like a browser.
func (m Model) middleClose(e mouseEv) (tea.Model, tea.Cmd) {
	if h := m.hoverTarget(e.X, e.Y); h.kind == "term" && h.idx >= 0 {
		m.space().Active = h.idx
		m.layoutPanes()
		return m.prefixCmd("X")
	}
	return m, nil
}
