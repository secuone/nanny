package ui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/secuone/nanny/internal/claude"
	"github.com/secuone/nanny/internal/gitwt"
	"github.com/secuone/nanny/internal/ws"
)

// space is a project (repo) with its rolled-up agent state, at a glance.
type space struct {
	name     string
	state    string // blocked | working | idle | "" (nothing live)
	live     int
	sessions int
	last     time.Time
}

const sidebarW = 30

func (m Model) sidebarOn() bool { return m.sidebar && m.w >= 110 }

// buildSpaces groups recent sessions and live agents by project.
func (m *Model) buildSpaces() {
	by := map[string]*space{}
	get := func(n string) *space {
		sp, ok := by[n]
		if !ok {
			sp = &space{name: n}
			by[n] = sp
		}
		return sp
	}
	rank := map[string]int{"blocked": 3, "working": 2, "idle": 1, "": 0}
	cutoff := time.Now().Add(-21 * 24 * time.Hour)
	for _, s := range m.sessions {
		if s.Live == nil && (s.Modified.Before(cutoff) || s.Bot()) {
			continue
		}
		sp := get(project(s.Cwd))
		sp.sessions++
		if s.Modified.After(sp.last) {
			sp.last = s.Modified
		}
	}
	for _, r := range m.live.allRows {
		if r.sub != nil {
			continue
		}
		_, cwd := r.titleCwd()
		sp := get(project(cwd))
		sp.live++
		st := r.state()
		if st == "done" {
			st = "idle"
		}
		if r.run > 0 && st == "idle" {
			st = "working"
		}
		if rank[st] > rank[sp.state] {
			sp.state = st
		}
	}
	m.projects = m.projects[:0]
	for _, sp := range by {
		if sp.name == "?" {
			continue
		}
		m.projects = append(m.projects, *sp)
	}
	sort.Slice(m.projects, func(i, j int) bool {
		a, b := m.projects[i], m.projects[j]
		if rank[a.state] != rank[b.state] {
			return rank[a.state] > rank[b.state]
		}
		return a.last.After(b.last)
	})
	if len(m.projects) > 40 {
		m.projects = m.projects[:40]
	}
}

// inScope reports whether a folder belongs to the selected space (or no scope is set).
func (m *Model) inScope(cwd string) bool {
	if sp := m.spaceScope(); sp != nil {
		return sp.Covers(cwd)
	}
	return m.scope == "" || project(cwd) == m.scope
}

func (m *Model) cycleScope(d int) {
	names := []string{""}
	for _, sp := range m.projects {
		names = append(names, sp.name)
	}
	m.scopeSpace = nil
	m.scope = cycleStr(names, m.scope, d)
	if m.scope == "" {
		m.setStatus(T("scope: all projects"), false)
	} else {
		m.setStatus(Tf("scope: %s  ([ ] to change · \\ to clear)", m.scope), false)
	}
	m.rebuild()
}

// sidebarHit maps a click row inside the sidebar to a row.
func (m *Model) sidebarHit(y int) (sideRow, bool) {
	for _, h := range m.vs.sideRows {
		if h.y == y {
			return h, true
		}
	}
	return sideRow{}, false
}

type sideRow struct {
	y       int
	scope   string
	space   int
	isSpace bool
	sess    *claude.Session
	sub     *claude.Subagent
	local   *paneRef
	wt      *gitwt.Worktree // a worktree listed under its space
	toggle  *ws.Space       // shows or hides the worktrees of this space
	url     string          // a PR: a click opens it
	scroll  int             // a "more" line: a click pages its half by this many rows
}

func (m Model) spaceGlyph(st string) string {
	switch st {
	case "":
		return m.st.Dim.Render("·")
	case "idle":
		return m.stateGlyph("idle")
	}
	return m.stateGlyph(st)
}

// sbLine is a sidebar line and, when it can be clicked, what it is.
type sbLine struct {
	text string
	hit  *sideRow
}

// sideWindow adds rows from *off within room lines, with "more" lines (that
// page) when they don't fit, and returns how far they can scroll. follow, when
// not -1, is a row to bring into view.
func (m Model) sideWindow(ls *[]string, rows []sbLine, room int, off *int, follow int) int {
	s := m.st
	fits := len(rows) <= room
	page := room
	if fits {
		room, *off = len(rows), 0
	} else {
		room = max(1, room-2) // the "more above / below" lines
	}
	maxOff := max(0, len(rows)-room)
	if follow >= 0 {
		*off = max(min(*off, follow), follow-room+1)
	}
	*off = max(0, min(*off, maxOff))
	if !fits {
		up := ""
		var hit *sideRow
		if *off > 0 {
			up = Tf("↑ %d more", *off)
			hit = &sideRow{y: len(*ls) + bodyY, space: -1, scroll: -page}
			m.vs.sideRows = append(m.vs.sideRows, *hit)
		}
		*ls = append(*ls, s.Dim.Render("   "+up))
	}
	for i := *off; i < len(rows) && i < *off+room; i++ {
		if r := rows[i].hit; r != nil {
			r.y = len(*ls) + bodyY
			m.vs.sideRows = append(m.vs.sideRows, *r)
		}
		*ls = append(*ls, rows[i].text)
	}
	if !fits {
		down := ""
		if rest := len(rows) - *off - room; rest > 0 {
			down = Tf("↓ %d more · wheel", rest)
			m.vs.sideRows = append(m.vs.sideRows, sideRow{y: len(*ls) + bodyY, space: -1, scroll: page})
		}
		*ls = append(*ls, s.Dim.Render("   "+down))
	}
	return maxOff
}

func (m Model) viewSidebar(h int) string {
	s := m.st
	w := sidebarW - 2
	var ls []string
	m.vs.sideRows = m.vs.sideRows[:0]
	gap := gapOf(h)
	// a section: its name, a rule to the count
	head := func(t string, n int) {
		name := s.Accent.Bold(true).Render(strings.ToUpper(t))
		cnt := s.Muted.Render(fmt.Sprintf(" %d", n))
		rule := s.Dim.Render(" " + strings.Repeat("─", max(1, w-2-lipgloss.Width(name)-lipgloss.Width(cnt))))
		ls = append(ls, " "+name+rule+cnt)
		if gap > 0 {
			ls = append(ls, "")
		}
	}
	// each half gets what it needs; when both overflow they share what is
	// left (see sideRooms) and scroll on their own (wheel over it)
	inner := h - 2 // above the connection footer
	agents, quiet := m.sideAgents()
	noAgents := !m.cfg.On("sidebar.agents") || m.cfg.Screen.SidebarAgents != "all" && !m.full() && m.onTab(tabLive)
	if noAgents {
		agents, quiet = nil, nil
	}

	// spaces: home + the user's workspaces with their rolled-up state
	head(T("spaces"), len(m.spaces))
	var rows []sbLine
	curRow := 0
	srow := func(idx int, line string, sel bool) {
		if sel {
			line = s.Sel(s.RowSel, fit(line, w))
			curRow = len(rows)
		}
		rows = append(rows, sbLine{line, &sideRow{space: idx, isSpace: true}})
	}
	srow(-1, " "+s.Accent.Render("⊞")+" "+s.Bold.Render(T("all spaces")), !m.inSpace())
	for i, sp := range m.spaces {
		st := m.spaceState(sp)
		name := s.Text.Render(trunc(spaceLabel(sp), w-9))
		if len(sp.Tabs) == 0 {
			name = s.Muted.Render(trunc(spaceLabel(sp), w-9))
		}
		cnt := ""
		if len(sp.Tabs) > 0 {
			cnt = s.Muted.Render(fmt.Sprintf("%2d", len(sp.Tabs)))
		}
		key := s.Dim.Render(" ")
		if i < 9 {
			key = s.Dim.Render(fmt.Sprint(i + 1))
		}
		srow(i, key+" "+m.paneGlyph(st)+" "+fit(name, w-8)+cnt, m.cur == i)
		// its branch, then its worktrees folded into one line that opens them
		var wts []gitwt.Worktree
		if m.cfg.On("sidebar.worktrees") {
			wts = m.spaceWorktrees(sp)
		}
		shown := 0
		if m.wtOpen[sp] {
			shown = len(wts)
		}
		// where it is working: branch and its state, its open PR, worktrees
		where := spaceWhere(sp)
		if br := branchOf(where); br != "" && m.cfg.On("sidebar.branch") {
			bits := ""
			if c, ok := m.stateOf(where); ok {
				bits = " " + m.branchBits(c)
			}
			info := s.Blue.Render("⎇ " + trunc(br, w-6-lipgloss.Width(bits)))
			rows = append(rows, sbLine{"    " + clickable("@githere:"+fmt.Sprint(i), info) + bits, nil})
		}
		if p, ok := m.prOf(where); ok && m.cfg.On("sidebar.pr") {
			rows = append(rows, sbLine{"    " + m.prBadge(p) + " " + s.Muted.Render(trunc(p.Title, w-8-lipgloss.Width(m.prBadge(p)))),
				&sideRow{space: i, url: p.URL}})
		}
		if tag, url := m.jiraSide(branchOf(where), w-6); tag != "" {
			rows = append(rows, sbLine{"    " + tag, &sideRow{space: i, url: url}})
		}
		if len(wts) > 0 {
			arrow := "▸ "
			if shown > 0 {
				arrow = "▾ "
			}
			rows = append(rows, sbLine{"    " + s.Muted.Render(arrow+Tn(len(wts), "%d worktree", "%d worktrees")), &sideRow{space: i, toggle: sp}})
		}
		for j := 0; j < shown; j++ {
			wt := wts[j]
			name := wt.Branch
			if name == "" {
				name = filepath.Base(wt.Path)
			}
			// its state when there is something to do, and its PR
			dirty := ""
			if wt.Dirty+wt.Ahead+wt.Behind > 0 {
				dirty = " " + m.branchBits(checkout{dirty: wt.Dirty, ahead: wt.Ahead, behind: wt.Behind})
			}
			if c, ok := m.stateOf(where); ok {
				if p, ok := m.prFor(c.root, wt.Branch); ok {
					dirty += " " + m.prBadge(p)
				}
			}
			rows = append(rows, sbLine{s.Dim.Render("    ├ ") + s.Teal.Render(trunc(name, w-10-lipgloss.Width(dirty))) + dirty,
				&sideRow{space: i, isSpace: true, wt: &wt}})
		}
	}
	spRows := rows
	arows, first := m.agentRows(agents, quiet, true)
	avail := inner - 5 - 2*gap
	if noAgents {
		avail = inner - 5 - gap
	}
	spRoom, agRoom := sideRooms(len(spRows), len(arows), avail, noAgents)
	if len(arows) > agRoom { // no blank lines between agents when they overflow
		arows, first = m.agentRows(agents, quiet, false)
		spRoom, agRoom = sideRooms(len(spRows), len(arows), avail, noAgents)
	}
	m.vs.sideSplit = bodyY + spRoom + 4 + gap
	if noAgents {
		m.vs.sideSplit = bodyY + inner + 1
	}
	follow := -1
	if m.cur != m.vs.sideFollowSpace {
		follow, m.vs.sideFollowSpace = curRow, m.cur
	}
	m.vs.sideSpMax = m.sideWindow(&ls, spRows, spRoom, &m.vs.sideSpOff, follow)
	for len(ls) < spRoom+2+gap {
		ls = append(ls, "")
	}
	// pinned under the spaces, so a new space and the menu are one click away
	ls = append(ls, " "+m.sideActions(w-1), "")

	if !noAgents {
		head(T("agents"), len(agents)+len(quiet))
		follow = -1
		if k := m.curAgentKey(); k != m.vs.sideFollowAgent {
			m.vs.sideFollowAgent = k
			if i, ok := first[k]; ok {
				follow = i
			}
		}
		m.vs.sideMax = m.sideWindow(&ls, arows, agRoom, &m.vs.sideOff, follow)
	}

	// connection footer
	for len(ls) < h-2 {
		ls = append(ls, "")
	}
	ls = ls[:h-2]
	dot := func(on bool, name string) string {
		if on {
			return s.Green.Render("●") + " " + s.Muted.Render(name)
		}
		return s.Dim.Render("○ " + name)
	}
	hermes := false
	for _, it := range m.items {
		if it.Group == "Hermes" && it.Status != "off" {
			hermes = true
		}
	}
	foot := clickable("B", s.Dim.Render("B "+T("hide sidebar")))
	if m.cfg.On("sidebar.hermes") && m.monOn("hermes") {
		foot = dot(hermes, "hermes") + "  " + foot
	}
	ls = append(ls, s.Dim.Render(strings.Repeat("─", w)), " "+foot)
	for i := range ls {
		ls[i] = fit(ls[i], w)
	}
	sep := lipgloss.NewStyle().Foreground(lipgloss.Color(s.P.Overlay)).Render("│")
	out := make([]string, len(ls))
	for i, l := range ls {
		out[i] = " " + l + sep
	}
	return strings.Join(out, "\n")
}

// sideRooms splits the lines left in two fixed halves: spaces on top, agents
// from the middle down, however many of each there are; each half scrolls.
func sideRooms(sp, ag, avail int, noAgents bool) (int, int) {
	if noAgents {
		return avail + 2, 0
	}
	avail = max(avail, 2)
	return avail / 2, avail - avail/2
}

// agentRows are the agents of the sidebar: three lines each, their subagents
// under them, and where each one's block starts. Every line of a block opens it.
func (m Model) agentRows(agents, quiet []liveRow, spacers bool) ([]sbLine, map[string]int) {
	s := m.st
	w := sidebarW - 2
	first := map[string]int{}
	var rows []sbLine
	cur := m.curAgentKey()
	if sp := m.sideScope(); sp != nil {
		if n := m.waitingElsewhere(); n > 0 {
			rows = append(rows, sbLine{" " + s.Red.Render("● "+trunc(Tf("%d waiting elsewhere", n), w-4)), &sideRow{space: -1, isSpace: true}})
		}
		if len(agents)+len(quiet) == 0 {
			rows = append(rows, sbLine{" " + s.Muted.Render(trunc(Tf("No agents in %s", spaceLabel(sp)), w-2)), nil})
		}
	}
	for i, r := range agents {
		if i > 0 && spacers {
			rows = append(rows, sbLine{text: ""})
		}
		first[r.key()] = len(rows)
		hit := func() *sideRow { return &sideRow{space: -1, sess: r.sess, local: r.local} }
		title, cwd := r.titleCwd()
		st := r.state()
		glyph := m.stateGlyph(st)
		if st == "idle" && r.run > 0 {
			glyph = s.Teal.Render(spinGlyph)
		}
		subs := ""
		if r.run > 0 {
			subs = s.Muted.Render(fmt.Sprintf("↳%d", r.run))
		}
		line := " " + glyph + " " + s.Text.Render(trunc(title, w-4-lipgloss.Width(subs)))
		line = fit(line, w-lipgloss.Width(subs)) + subs
		if st == "blocked" {
			line = s.Red.Render("▌") + line[1:]
		}
		if r.key() == cur {
			line = s.Sel(s.RowSel, fit(line, w))
		}
		rows = append(rows, sbLine{line, hit()})
		where := project(cwd)
		if r.local != nil {
			where = "◆ " + r.local.space.Name
		}
		if p := r.prompt(); p != nil {
			rows = append(rows, sbLine{"   " + s.Yellow.Render(trunc("⚠ "+promptSummary(p), w-3)), hit()})
		} else {
			rows = append(rows, sbLine{"   " + s.Muted.Render(trunc(where, w-3)), hit()})
		}
		// running subagents nested under their agent
		if r.sess != nil && r.run > 0 {
			shown := 0
			for _, f := range claude.Flatten(m.subs[r.sess.Path]) {
				if !f.Running {
					continue
				}
				if shown == 3 {
					rows = append(rows, sbLine{s.Dim.Render("   └ " + Tf("+%d more", r.run-shown)), hit()})
					break
				}
				shown++
				f := f
				rows = append(rows, sbLine{s.Dim.Render("   ├ ") + m.stateGlyph("running") + " " + s.Text.Render(trunc(f.Type, 8)) + " " + s.Muted.Render(trunc(f.Description, w-16)),
					&sideRow{space: -1, sess: r.sess, sub: f.Subagent}})
			}
		}
	}
	if len(quiet) > 0 { // the rest, one click from Live
		if len(rows) > 0 && spacers {
			rows = append(rows, sbLine{text: ""})
		}
		rows = append(rows, sbLine{"   " + clickable("1", s.Muted.Render(Tn(len(quiet), "○ %d more idle", "○ %d more idle")+" · 1 "+T("Live"))), nil})
	}
	return rows, first
}

// sideActions is the line under the spaces: new space, discover, the ≡ menu;
// labels go before the icons do when it is narrow.
func (m Model) sideActions(w int) string {
	s := m.st
	dot := s.Dim.Render(" · ")
	for _, full := range []bool{true, false} {
		nw, ds, mn := "+ "+T("new"), "⌕ "+T("discover"), "≡ "+T("menu")
		if !full {
			ds = "⌕"
		}
		l := clickable("N", s.Muted.Render(nw)) + dot + clickable("D", s.Muted.Render(ds)) + dot + clickable(appMenuKey, s.Accent.Render(mn))
		if lipgloss.Width(l) <= w || !full {
			return l
		}
	}
	return ""
}
