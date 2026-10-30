package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/secuone/nanny/internal/claude"
	"github.com/secuone/nanny/internal/gitwt"
	"github.com/secuone/nanny/internal/term"
	"github.com/secuone/nanny/internal/ws"
)

// paneOutMsg tells the UI some pane produced output (coalesced).
type paneOutMsg struct{}

// paneRef locates a pane inside the workspace tree.
type paneRef struct {
	space *ws.Space
	tab   *ws.Tab
	pane  *term.Pane
}

type rect struct{ x, y, w, h int }

func (m Model) inSpace() bool { return m.cur >= 0 && m.cur < len(m.spaces) }

func (m Model) space() *ws.Space {
	if !m.inSpace() {
		return nil
	}
	return m.spaces[m.cur]
}

// Redraw delays after pane output: the focused pane feels live, the rest can wait.
const (
	focusedDelay    = 16 * time.Millisecond
	backgroundDelay = 50 * time.Millisecond
)

// paneDelay picks how long to coalesce output, given whether the focused pane wrote.
func paneDelay(focusedWrote bool) time.Duration {
	if focusedWrote {
		return focusedDelay
	}
	return backgroundDelay
}

// focusedPane is the pane that takes the keys, if a space is open.
func (m Model) focusedPane() *term.Pane {
	if !m.inSpace() {
		return nil
	}
	return m.space().Tab().Pane()
}

// waitPane blocks until any pane writes, then coalesces bursts into one redraw:
// 16 ms when the focused pane wrote since the last frame, 50 ms for the others.
func (m Model) waitPane() tea.Cmd {
	ch := m.paneCh
	fp := m.focusedPane()
	var seq uint64
	if fp != nil {
		seq = fp.Seq()
	}
	return func() tea.Msg {
		<-ch
		time.Sleep(paneDelay(fp != nil && fp.Seq() != seq)) // seq moves before the signal
		for {
			select {
			case <-ch:
			default:
				return paneOutMsg{}
			}
		}
	}
}

func (m *Model) spawn(cwd string, argv []string) (*term.Pane, error) {
	w, h, notify := m.paneSize()
	return term.Start(cwd, argv, w, h, nil, notify)
}

// attach reconnects to a pane still running under its holder.
func (m *Model) attach(sock, cwd string, argv []string) (*term.Pane, error) {
	w, h, notify := m.paneSize()
	if _, ok := m.vs.paneArea(); !ok {
		w, h = 0, 0 // not laid out yet: keep the program's size, no resize
	}
	return term.Attach(sock, cwd, argv, w, h, notify)
}

func (m *Model) paneSize() (int, int, func()) {
	ch := m.paneCh
	w, h := 80, 24
	if r, ok := m.vs.paneArea(); ok {
		w, h = r.w-2, r.h-2
	}
	return w, h, func() {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// spaceFor returns the space owning dir (deepest path wins), creating one per repo if needed.
func (m *Model) spaceFor(dir string, create bool) (*ws.Space, int) {
	best, bi, bl := (*ws.Space)(nil), -1, -1
	for i, s := range m.spaces {
		for _, p := range s.Paths {
			if (dir == p || strings.HasPrefix(dir, strings.TrimSuffix(p, "/")+"/")) && len(p) > bl {
				best, bi, bl = s, i, len(p)
			}
		}
	}
	if best != nil || !create {
		return best, bi
	}
	root := gitwt.RepoRoot(dir)
	if root == "" || root == home {
		root = dir
	}
	s := ws.NewSpace(project(root), root)
	m.spaces = append(m.spaces, s)
	m.saveSpaces()
	return s, len(m.spaces) - 1
}

// openTab starts argv (empty = shell) in a new tab of the space owning cwd and focuses it.
func (m *Model) openTab(cwd, label string, argv []string) tea.Cmd {
	r, err := m.spawnTab(cwd, label, argv)
	if err != nil {
		m.setStatus(T("could not start: ")+err.Error(), true)
		return nil
	}
	m.focusRef(r)
	m.setStatus(Tf("opened %s in %s", label, r.space.Name), false)
	return nil
}

// spawnTab starts argv in a new tab of the space owning cwd, without switching to it.
func (m *Model) spawnTab(cwd, label string, argv []string) (paneRef, error) {
	s, _ := m.spaceFor(cwd, true)
	p, err := m.spawn(cwd, argv)
	if err != nil {
		return paneRef{}, err
	}
	t := ws.NewTab(label)
	t.Panes = []*term.Pane{p}
	s.Tabs = append(s.Tabs, t)
	m.saveSpaces()
	return paneRef{s, t, p}, nil
}

// split adds a pane next to the focused one.
func (m *Model) split(vertical bool, argv []string) {
	s := m.space()
	if s == nil || s.Tab() == nil {
		return
	}
	t := s.Tab()
	cwd := s.Dir()
	if p := t.Pane(); p != nil {
		cwd = p.Cwd
	}
	p, err := m.spawn(cwd, argv)
	if err != nil {
		m.setStatus(err.Error(), true)
		return
	}
	if len(t.Panes) == 1 {
		t.Vertical = vertical
	}
	t.Panes = append(t.Panes, p)
	t.Focus = len(t.Panes) - 1
	t.Zoom = false
	m.layoutPanes()
	m.saveSpaces()
}

func (m *Model) closePane(p *term.Pane) {
	for _, s := range m.spaces {
		for ti := 0; ti < len(s.Tabs); ti++ {
			t := s.Tabs[ti]
			for _, q := range t.Panes {
				if q == p {
					t.RemovePane(p)
					p.Close()
					if len(t.Panes) == 0 {
						s.Tabs = append(s.Tabs[:ti], s.Tabs[ti+1:]...)
						if s.Active >= len(s.Tabs) {
							s.Active = max(0, len(s.Tabs)-1)
						}
					}
					m.layoutPanes()
					m.saveSpaces()
					return
				}
			}
		}
	}
}

func (m *Model) allPanes() []paneRef {
	var out []paneRef
	for _, s := range m.spaces {
		for _, t := range s.Tabs {
			for _, p := range t.Panes {
				out = append(out, paneRef{s, t, p})
			}
		}
	}
	return out
}

// procChildren is swapped by tests to count /proc scans.
var procChildren = ws.ProcChildren

// trackAgents links pane processes to Claude sessions (by pid tree) and reaps exited panes.
func (m *Model) trackAgents() {
	refs := m.allPanes()
	if len(refs) == 0 {
		m.vs.byPid = nil
		return
	}
	for _, r := range refs {
		if r.pane.Exited() && len(r.pane.Argv) > 0 && filepath.Base(r.pane.Argv[0]) == "claude" {
			m.shellAfterClaude(r)
		}
	}
	live := claude.LiveProcs()
	m.vs.bySession = map[string]paneRef{}
	if len(live) == 0 {
		return // nothing can match: skip the /proc scan
	}
	procs := map[int]claude.Proc{}
	for _, p := range live {
		procs[p.Pid] = p
	}
	kids := procChildren()
	for _, r := range refs {
		for _, pid := range ws.Tree(r.pane.Pid(), kids) {
			if pr, ok := procs[pid]; ok {
				r.pane.Agent = "claude"
				if r.tab.SessionIDs[r.pane.ID] != pr.SessionID {
					r.tab.SessionIDs[r.pane.ID] = pr.SessionID
					m.vs.dirty = true
				}
				m.vs.bySession[pr.SessionID] = r
				break
			}
		}
	}
}

// shellAfterClaude swaps a Claude pane that exited (declined trust, /exit,
// ctrl+c) for a shell in the same folder, like a terminal would leave you.
func (m *Model) shellAfterClaude(r paneRef) {
	np, err := m.spawn(r.pane.Cwd, nil)
	if err != nil {
		return
	}
	label := r.tab.Label
	for i, p := range r.tab.Panes {
		if p == r.pane {
			r.tab.Panes[i] = np
		}
	}
	if id := r.tab.SessionIDs[r.pane.ID]; id != "" {
		r.tab.SessionIDs[np.ID] = id
	}
	delete(r.tab.SessionIDs, r.pane.ID)
	if r.tab.Label == "claude" {
		r.tab.Label = "shell"
	}
	np.Note("\x1b[2m" + Tf("claude exited (code %d) · this is a shell in %s · %s c starts Claude again", r.pane.ExitCode(), tilde(r.pane.Cwd), m.prefixLabel()) + "\x1b[0m\r\n")
	r.pane.Close()
	m.layoutPanes()
	m.saveSpaces()
	m.setStatus(Tf("claude exited in %s · you are in a shell now", label), false)
}

// localPane returns the nanny pane hosting a session, if any.
func (m Model) localPane(id string) (paneRef, bool) {
	if m.vs.bySession == nil {
		return paneRef{}, false
	}
	r, ok := m.vs.bySession[id]
	return r, ok
}

// focusRef switches to the space/tab/pane of r.
func (m *Model) focusRef(r paneRef) {
	for i, s := range m.spaces {
		if s != r.space {
			continue
		}
		m.cur = i
		for ti, t := range s.Tabs {
			if t == r.tab {
				s.Active = ti
				for pi, p := range t.Panes {
					if p == r.pane {
						t.Focus = pi
					}
				}
			}
		}
	}
	m.dash = false
	m.layoutPanes()
}

func (m *Model) saveSpaces() {
	if m.cfg.NoState {
		return
	}
	_ = ws.Save(m.spaces, m.cur)
	m.vs.dirty = false
}

// ---------------------------------------------------------------- layout

// paneRects computes pane rectangles for the active tab inside area.
func paneRects(t *ws.Tab, area rect) []rect {
	n := len(t.Panes)
	if n == 0 {
		return nil
	}
	if t.Zoom {
		out := make([]rect, n)
		out[t.Focus] = area
		return out
	}
	out := make([]rect, n)
	for i := range t.Panes {
		if t.Vertical {
			w := area.w / n
			x := area.x + i*w
			if i == n-1 {
				w = area.w - (x - area.x)
			}
			out[i] = rect{x, area.y, w, area.h}
		} else {
			h := area.h / n
			y := area.y + i*h
			if i == n-1 {
				h = area.h - (y - area.y)
			}
			out[i] = rect{area.x, y, area.w, h}
		}
	}
	return out
}

func (m *Model) layoutPanes() {
	area := rect{m.sideW(), termY + 1, m.mainW(), m.bodyHeight() - 1} // under the terminal tabs
	m.vs.area = area
	m.vs.rects = nil
	s := m.space()
	if s == nil || s.Tab() == nil {
		return
	}
	t := s.Tab()
	m.vs.rects = paneRects(t, area)
	for i, p := range t.Panes {
		r := m.vs.rects[i]
		if r.w > 0 {
			p.Resize(r.w-2, r.h-2)
		}
	}
}

func (v *viewState) paneArea() (rect, bool) { return v.area, v.area.w > 0 }

// ---------------------------------------------------------------- views

// paneStateTTL is how long a pane's state is reused: it costs a /proc read or a
// screen scan, and every frame asks for it again, per pane and per rollup.
const paneStateTTL = 250 * time.Millisecond

func (m Model) paneState(p *term.Pane) string {
	if p.Exited() {
		return "exited"
	}
	return p.Memo(paneStateTTL, func() string { return m.livePaneState(p) })
}

func (m Model) livePaneState(p *term.Pane) string {
	if p.Agent != "claude" {
		if fg := p.ForegroundPid(); fg != 0 && fg != p.Pid() {
			return "running"
		}
		return "shell"
	}
	return agentState(p)
}

// tabState rolls up a tab: blocked > working > done(bell) > idle.
func (m Model) tabState(t *ws.Tab) string {
	best := ""
	rank := map[string]int{"blocked": 5, "working": 4, "running": 3, "idle": 2, "shell": 1, "exited": 0, "": -1}
	for _, p := range t.Panes {
		st := m.paneState(p)
		if rank[st] > rank[best] {
			best = st
		}
	}
	return best
}

func (m Model) spaceState(s *ws.Space) string {
	best := ""
	rank := map[string]int{"blocked": 5, "working": 4, "running": 3, "idle": 2, "shell": 1, "exited": 0, "": -1}
	for _, t := range s.Tabs {
		if st := m.tabState(t); rank[st] > rank[best] {
			best = st
		}
	}
	return best
}

func (m Model) paneGlyph(st string) string {
	switch st {
	case "working", "running":
		return m.stateGlyph("working")
	case "blocked":
		return m.stateGlyph("blocked")
	case "idle":
		return m.stateGlyph("idle")
	case "shell":
		return m.st.Muted.Render("›")
	case "exited":
		return m.st.Dim.Render("×")
	}
	return m.st.Dim.Render("·")
}

// viewSpaceTabs is the top bar: logo, home, one tab per space, +. When the
// spaces don't fit in budget columns, a window around the current one is
// drawn, with ‹N and N› chips for the hidden ones.
func (m Model) viewSpaceTabs(budget int) string {
	s := m.st
	logo := s.Logo.Render("◆ nanny")
	home := " ⊞ " + T("all") + " "
	if !m.inSpace() {
		home = s.TabActive.Render(home)
	} else {
		home = s.Tab.Render(home)
	}
	x := lipgloss.Width(logo)
	m.vs.spaceHits = append(m.vs.spaceHits[:0], hit{0, x, -3}) // the logo: the ≡ menu
	m.vs.spaceHits = append(m.vs.spaceHits, hit{x, x + lipgloss.Width(home), -1})
	x += lipgloss.Width(home)
	parts := []string{logo, home}
	chipAt := func(i, x int) string {
		sp := m.spaces[i]
		label := fmt.Sprintf("%s %s", m.paneGlyph(m.spaceState(sp)), spaceLabel(sp))
		if len(sp.Tabs) > 1 {
			label += s.Muted.Render(fmt.Sprintf(" %d", len(sp.Tabs)))
		}
		switch {
		case i == m.cur:
			return chip(s.TabActive, " "+label+" ")
		case m.vs.hover == hoverAt{"space", i, x}:
			return chip(s.TabHover, label)
		}
		return s.Tab.Render(label)
	}
	lo, hi := m.spaceWindow(chipAt, budget-x-3)
	m.vs.spaceLo, m.vs.spaceHi = lo, hi
	more := func(label string, idx int) {
		r := s.Muted.Render(label)
		m.vs.spaceHits = append(m.vs.spaceHits, hit{x, x + lipgloss.Width(r), idx})
		x += lipgloss.Width(r)
		parts = append(parts, r)
	}
	if lo > 0 {
		more(fmt.Sprintf(" ‹%d ", lo), -4)
	}
	for i := lo; i <= hi; i++ {
		r := chipAt(i, x)
		m.vs.spaceHits = append(m.vs.spaceHits, hit{x, x + lipgloss.Width(r), i})
		x += lipgloss.Width(r)
		parts = append(parts, r)
	}
	if hi < len(m.spaces)-1 {
		more(fmt.Sprintf(" %d› ", len(m.spaces)-1-hi), -5)
	}
	plus := s.Muted.Render(" + ")
	m.vs.spaceHits = append(m.vs.spaceHits, hit{x, x + 3, -2})
	return strings.Join(append(parts, plus), "")
}

// spaceWindow is the range of space chips to draw in room columns: all when
// they fit, else the ones around the current space (the first at home).
func (m Model) spaceWindow(chipAt func(i, x int) string, room int) (int, int) {
	n := len(m.spaces)
	w := make([]int, n)
	total := 0
	for i := range w {
		w[i] = lipgloss.Width(chipAt(i, -1))
		total += w[i]
	}
	if total <= room || n == 0 {
		return 0, n - 1
	}
	lo := max(m.cur, 0)
	hi := lo
	room -= w[lo] + 2*5 // the chip itself and both overflow chips
	for grew := true; grew; {
		grew = false
		if hi+1 < n && w[hi+1] <= room {
			hi++
			room -= w[hi]
			grew = true
		}
		if lo > 0 && w[lo-1] <= room {
			lo--
			room -= w[lo]
			grew = true
		}
	}
	return lo, hi
}

// hiddenSpacesMenu lists the spaces the top bar left out, before or after
// the ones drawn.
func (m *Model) hiddenSpacesMenu(before bool, x int) {
	from, to := m.vs.spaceHi+1, len(m.spaces)
	if before {
		from, to = 0, m.vs.spaceLo
	}
	var items []menuItem
	for i := from; i < to; i++ {
		i := i
		items = append(items, menuItem{label: spaceLabel(m.spaces[i]), run: func(m *Model) tea.Cmd {
			m.cur = i
			m.layoutPanes()
			return m.refreshDetail()
		}})
	}
	m.openMenu(Tf("%d more spaces", to-from), x, 1, items)
}

type hit struct{ x0, x1, idx int }

// viewTabBar is row 1: the views, the same at home and in every space (where
// they are scoped to it and "0 terminal" comes first). Labels shrink to fit.
func (m Model) viewTabBar() string {
	s := m.st
	type item struct {
		label  string
		active bool
		idx    int
	}
	var items []item
	// "0 terminal" is always first, so the bar doesn't shift when you enter a
	// space; from all spaces it is dimmed and opens the first space.
	if m.inSpace() || len(m.spaces) > 0 {
		items = append(items, item{"0 ▣ " + T("terminal"), m.inSpace() && !m.dash, tabTerm})
	}
	for _, t := range m.barTabs() {
		items = append(items, item{tabKey(t) + " " + T(tabNames[t]), m.dashShown() && t == m.tab, int(t)})
	}
	badge := ""
	switch { // in a space its name is already on the top bar
	case m.inSpace():
	case m.scope != "" || m.scopeSpace != nil:
		sc := m.scope
		if m.scopeSpace != nil {
			sc = m.scopeSpace.Name
		}
		badge = s.BadgeColor(s.P.Peach).Render("⌖ "+sc) + s.Muted.Render(" \\ "+T("clears"))
	}
	// the views of this space (or of all of them) on the left; the ones that
	// are the same everywhere — prompts, skills, the machine, settings, the
	// nursery — apart on the right
	var mine, global []item
	for _, it := range items {
		if globalView(it.idx) {
			global = append(global, it)
		} else {
			mine = append(mine, it)
		}
	}
	// tight drops the extra padding and "terminal" before any label is cut
	render := func(items []item, x int, room int, tight bool) (string, []hit) {
		var parts []string
		var hits []hit
		for _, it := range items {
			label := it.label
			if tight && it.idx == tabTerm && !it.active {
				label = "0 ▣"
			}
			if num, name, ok := strings.Cut(label, " "); ok && !it.active && lipgloss.Width(name) > room {
				label = num
				if room > 1 {
					label += " " + trunc(name, room)
				}
			}
			st := s.Tab
			switch {
			case it.active:
				st = s.TabActive
			case m.vs.hover == hoverAt{"view", it.idx, x}:
				st = s.TabHover
			case it.idx == tabTerm && !m.inSpace():
				st = s.Dim.Padding(0, 1)
			case globalView(it.idx):
				st = s.Tab.Foreground(lipgloss.Color(s.P.Muted))
			}
			if tight && !it.active {
				st = st.Padding(0, 0).PaddingLeft(1)
			}
			r := st.Render(label)
			hits = append(hits, hit{x, x + lipgloss.Width(r), it.idx})
			x += lipgloss.Width(r)
			parts = append(parts, r)
		}
		return strings.Join(parts, ""), hits
	}
	sep := s.Dim.Render("│")
	var left, right string
	var hits []hit
	for _, try := range []struct {
		room  int
		tight bool
	}{{99, false}, {99, true}, {10, true}, {9, true}, {8, true}, {7, true}, {6, true}, {0, true}} { // tighter, then inactive labels shrink, then go
		l, lh := render(mine, 1, try.room, try.tight)
		left, hits = " "+l, lh
		right = ""
		if len(global) > 0 {
			r, _ := render(global, 0, try.room, try.tight)
			x := m.w - lipgloss.Width(r) - 1
			r, rh := render(global, x, try.room, try.tight)
			right = r
			hits = append(hits, rh...)
		}
		if lipgloss.Width(left)+lipgloss.Width(right)+3 <= m.w {
			break
		}
	}
	m.vs.tabHits = append(m.vs.tabHits[:0], hits...)
	gap := m.w - 1 - lipgloss.Width(left) - lipgloss.Width(right)
	if badge != "" && lipgloss.Width(badge)+4 <= gap-2 {
		left += "  " + badge
		gap -= lipgloss.Width(badge) + 2
	}
	if right == "" {
		return left
	}
	return left + strings.Repeat(" ", max(1, gap-1)) + sep + right
}

// globalView is a view that shows the same in every space.
func globalView(idx int) bool {
	switch tab(idx) {
	case tabPrompts, tabSkills, tabRadar, tabSettings, tabJira, tabInsights:
		return idx >= 0
	}
	return false
}

// Screen rows: the top bar is row 0, a blank row, the views bar, another blank
// row, and the body from bodyY. A space's terminal tabs (and the Monitor's
// sections) are the body's first row.
const (
	viewY = 2
	bodyY = 4
	termY = bodyY
)

// tabTerm is the hit index of "0 terminal" in the view bar.
const tabTerm = -3

// viewTermTabs is the strip over a space's terminals: its tabs and +, then
// where it is (folder and branch), lined up with the panes.
func (m Model) viewTermTabs(w int) string {
	s := m.st
	sp := m.space()
	x := m.sideW()
	m.vs.termHits = m.vs.termHits[:0]
	var parts []string
	add := func(label string, active bool, idx int) {
		r := s.Tab.Render(label)
		hover := m.vs.hover == hoverAt{"term", idx, x}
		switch {
		case active: // one color on the lit tab: a green glyph on orange can't be read
			r = s.TabSel.Render(ansi.Strip(label))
		case idx < 0 && hover: // the + buttons
			r = s.BtnHover.Render(label)
		case idx < 0:
			r = s.Btn.Render(label)
		case hover:
			r = chip(s.TabHover, label)
		}
		m.vs.termHits = append(m.vs.termHits, hit{x, x + lipgloss.Width(r), idx})
		x += lipgloss.Width(r)
		parts = append(parts, r)
	}
	pad := func(n int) {
		x += n
		parts = append(parts, strings.Repeat(" ", n))
	}
	for i, t := range sp.Tabs {
		label := fmt.Sprintf("%d %s %s", i+1, m.paneGlyph(m.tabState(t)), t.Label)
		if len(t.Panes) > 1 {
			label += fmt.Sprintf(" ⊞%d", len(t.Panes))
		}
		add(label, i == sp.Active, i)
	}
	pad(3) // the buttons stand apart from the tabs
	add("+ claude", false, -2)
	pad(1)
	add("+ "+T("shell"), false, -4)
	left := strings.Join(parts, "")
	right := s.Peach.Render(tilde(sp.Dir()))
	if br := branchOf(sp.Dir()); br != "" {
		right += " " + clickable("@githere:"+strconv.Itoa(m.cur), s.Blue.Render("⎇ "+br))
	}
	gap := w - lipgloss.Width(left) - lipgloss.Width(right) - 1
	if gap < 1 {
		return fit(left, w)
	}
	return left + strings.Repeat(" ", gap) + right + " "
}

var branchCache = map[string]branchEntry{}

type branchEntry struct {
	at time.Time
	br string
}

func branchOf(dir string) string {
	if e, ok := branchCache[dir]; ok && time.Since(e.at) < 10*time.Second {
		return e.br
	}
	in := dir
	for d := dir; d != "/" && d != "."; d = filepath.Dir(d) { // a folder inside the repo
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			dir = d
			break
		}
	}
	b, err := os.ReadFile(filepath.Join(dir, ".git", "HEAD"))
	br := ""
	if err == nil {
		br = strings.TrimPrefix(strings.TrimSpace(string(b)), "ref: refs/heads/")
		if len(br) == 40 {
			br = br[:8]
		}
	} else { // linked worktree: .git is a file
		if out, err := os.ReadFile(filepath.Join(dir, ".git")); err == nil && strings.HasPrefix(string(out), "gitdir:") {
			gd := strings.TrimSpace(strings.TrimPrefix(string(out), "gitdir:"))
			if hb, err := os.ReadFile(filepath.Join(gd, "HEAD")); err == nil {
				br = strings.TrimPrefix(strings.TrimSpace(string(hb)), "ref: refs/heads/")
			}
		}
	}
	branchCache[in] = branchEntry{time.Now(), br}
	return br
}

// paneLines is a pane's body and bottom border as last drawn, good while key holds.
type paneLines struct {
	key  string
	rest []string
}

// paneRest draws what sits under a pane's title line: the body between its side
// borders, then the bottom border. It redraws only when the pane's output, size,
// scroll, border colour or look changed; the cache lives in keep.
func (m Model) paneRest(p *term.Pane, r rect, bc lipgloss.Style, col string, keep map[*term.Pane]paneLines) []string {
	key := fmt.Sprintf("%d|%d|%d|%d|%t|%s|%s", p.Seq(), r.w, r.h, p.Scrolled(), p.Exited(), col, m.look.fenceOn())
	if c, ok := m.vs.paneLines[p]; ok && c.key == key {
		keep[p] = c
		return c.rest
	}
	var rest []string
	body := p.View()
	side := bc.Render("│")
	on, off := m.look.fenceOn(), m.look.fenceOff()
	for j := 0; j < r.h-2; j++ {
		l := ""
		if j < len(body) {
			l = body[j]
		}
		rest = append(rest, side+on+fit(l+"\x1b[0m", r.w-2)+off+side)
	}
	rest = append(rest, bc.Render("╰"+strings.Repeat("─", r.w-2)+"╯"))
	keep[p] = paneLines{key, rest}
	return rest
}

// viewPanes draws the active tab's panes with titled borders.
func (m Model) viewPanes(w, h int) string {
	s := m.st
	sp := m.space()
	t := sp.Tab()
	if t == nil || len(t.Panes) == 0 {
		return m.viewEmptySpace(sp, w, h)
	}
	type seg struct {
		x, w int
		s    string
	}
	rows := make([][]seg, h)
	keep := make(map[*term.Pane]paneLines, len(t.Panes)) // the cache only holds this tab's panes
	defer func() { m.vs.paneLines = keep }()
	rects := paneRects(t, rect{0, 0, w, h})
	for i, p := range t.Panes {
		r := rects[i]
		if r.w == 0 {
			continue
		}
		focused := i == t.Focus
		st := m.paneState(p)
		bc, col := s.BorderOverlay, s.P.Overlay
		if focused {
			bc, col = s.BorderAccent, s.P.Accent
		}
		if st == "blocked" {
			bc, col = s.BorderRed, s.P.Red
		}
		title := " " + m.paneGlyph(st) + " " + paneTitle(p) + " "
		if sc := p.Scrolled(); sc > 0 {
			title += s.Yellow.Render(fmt.Sprintf("↑%d ", sc))
		}
		switch {
		case p.Exited():
			title += s.Muted.Render(Tf("exited %d · x close · ⏎ restart ", p.ExitCode()))
		case p.ReadOnly && len(p.Shell) > 0:
			title += s.Muted.Render(T("read-only · s shell · k j scroll · q close") + " ")
		case p.ReadOnly:
			title += s.Muted.Render(T("read-only · k j scroll · q close") + " ")
		}
		tw := lipgloss.Width(title)
		top := bc.Render("╭─") + title + bc.Render(strings.Repeat("─", max(0, r.w-3-tw))+"╮")
		if tw > r.w-3 {
			top = bc.Render("╭" + strings.Repeat("─", r.w-2) + "╮")
		}
		lines := append([]string{top}, m.paneRest(p, r, bc, col, keep)...)
		if focused && !m.prefix {
			m.vs.paneCursor = m.paneCursor(p, r)
		}
		for j, l := range lines {
			y := r.y + j
			if y >= h {
				break
			}
			rows[y] = append(rows[y], seg{r.x, r.w, l})
		}
	}
	// stitch segments left to right; cutting a string would drag its escapes along
	out := make([]string, h)
	for y, segs := range rows {
		sort.Slice(segs, func(a, b int) bool { return segs[a].x < segs[b].x })
		var b strings.Builder
		x := 0
		for _, sg := range segs {
			if sg.x > x {
				b.WriteString(strings.Repeat(" ", sg.x-x))
			}
			b.WriteString(sg.s)
			x = sg.x + sg.w
		}
		if x < w {
			b.WriteString(strings.Repeat(" ", w-x))
		}
		out[y] = b.String()
	}
	return strings.Join(out, "\n")
}

// paneCursor is the pane's own cursor on the screen: inside its borders, below the terminal tabs.
func (m Model) paneCursor(p *term.Pane, r rect) *tea.Cursor {
	x, y, ok, style, blink := p.Cursor()
	if !ok {
		return nil
	}
	c := tea.NewCursor(m.vs.area.x+r.x+1+x, m.vs.area.y+r.y+1+y)
	c.Blink = blink
	switch style {
	case vt.CursorUnderline:
		c.Shape = tea.CursorUnderline
	case vt.CursorBar:
		c.Shape = tea.CursorBar
	}
	return c
}

func paneTitle(p *term.Pane) string {
	if t := strings.TrimSpace(p.Title()); t != "" {
		return trunc(t, 40)
	}
	if p.Agent == "claude" {
		return "claude"
	}
	return filepath.Base(p.Argv[0])
}

func (m Model) viewEmptySpace(sp *ws.Space, w, h int) string {
	s := m.st
	k := func(key, d string) string { return "   " + clickable(key, s.Key.Render(fit(key, 12))+s.Text.Render(d)) }
	px := m.prefixLabel()
	lines := []string{
		"", "",
		"   " + s.Bold.Render(spaceLabel(sp)) + s.Muted.Render("  ·  "+tilde(sp.Dir())),
		"",
		k("c", T("new Claude tab here")),
		k("s", T("new shell tab")),
		k("r", T("resume the latest session of this space")),
		k("f", T("find a session of this space")),
		k("w", T("manage spaces · D discover")),
		"",
		s.Muted.Render("   " + Tf("inside a pane, commands use the prefix %s (%s ? for help)", px, px)),
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	return s.framed(spaceLabel(sp), strings.Join(lines[:h-2], "\n"), w, h, true, 0)
}

// ---------------------------------------------------------------- input

func (m Model) prefixLabel() string {
	p := m.cfg.Prefix
	if p == "" {
		p = "ctrl+b"
	}
	return p
}

// csiKeys are the keys sent as CSI sequences: a final byte, or a number and "~".
var csiKeys = map[rune]string{
	tea.KeyUp: "A", tea.KeyDown: "B", tea.KeyRight: "C", tea.KeyLeft: "D", tea.KeyHome: "H", tea.KeyEnd: "F",
	tea.KeyPgUp: "5~", tea.KeyPgDown: "6~", tea.KeyDelete: "3~", tea.KeyInsert: "2~",
	tea.KeyF1: "P", tea.KeyF2: "Q", tea.KeyF3: "R", tea.KeyF4: "S",
	tea.KeyF5: "15~", tea.KeyF6: "17~", tea.KeyF7: "18~", tea.KeyF8: "19~",
	tea.KeyF9: "20~", tea.KeyF10: "21~", tea.KeyF11: "23~", tea.KeyF12: "24~",
}

// csiKey is a CSI key with its modifier parameter (1 + shift + 2 alt + 4 ctrl); F1-F4 are SS3 when plain.
func csiKey(code rune, param int) []byte {
	spec := csiKeys[code]
	last, num := spec[len(spec)-1:], spec[:len(spec)-1]
	switch {
	case param == 1 && code >= tea.KeyF1 && code <= tea.KeyF4:
		return []byte("\x1bO" + spec)
	case param == 1:
		return []byte("\x1b[" + spec)
	case last == "~":
		return []byte(fmt.Sprintf("\x1b[%s;%d~", num, param))
	}
	return []byte(fmt.Sprintf("\x1b[1;%d%s", param, last))
}

// keyBytes encodes a key press the way a terminal would send it.
func keyBytes(k tea.KeyPressMsg) []byte {
	mod := k.Mod &^ (tea.ModCapsLock | tea.ModNumLock | tea.ModScrollLock)
	ctrl, alt, shift := mod.Contains(tea.ModCtrl), mod.Contains(tea.ModAlt), mod.Contains(tea.ModShift)
	if k.Text != "" && !ctrl && !alt {
		return []byte(k.Text)
	}
	if _, ok := csiKeys[k.Code]; ok {
		param := 1
		if shift {
			param += 1
		}
		if alt {
			param += 2
		}
		if ctrl {
			param += 4
		}
		return csiKey(k.Code, param)
	}
	var b []byte
	switch c := k.Code; {
	case c == tea.KeyEnter || c == tea.KeyKpEnter:
		b = []byte("\r")
	case c == tea.KeyTab && shift:
		return []byte("\x1b[Z")
	case c == tea.KeyTab:
		b = []byte("\t")
	case c == tea.KeyBackspace && ctrl:
		b = []byte{0x08}
	case c == tea.KeyBackspace:
		b = []byte{0x7f}
	case c == tea.KeyEsc:
		return []byte{0x1b}
	case c == tea.KeySpace && ctrl:
		b = []byte{0}
	case c == tea.KeySpace:
		b = []byte(" ")
	case ctrl && c >= 'a' && c <= 'z':
		b = []byte{byte(c) & 0x1f}
	case ctrl && c == '?':
		b = []byte{0x7f}
	case ctrl && c >= '@' && c <= '_':
		b = []byte{byte(c) & 0x1f}
	case k.Text != "":
		b = []byte(k.Text)
	case shift && c >= 'a' && c <= 'z':
		b = []byte(string(c - 'a' + 'A'))
	case c > 0 && c != tea.KeyExtended:
		b = []byte(string(c))
	}
	if alt && len(b) > 0 {
		b = append([]byte{0x1b}, b...)
	}
	return b
}

// paneKey routes keys while a space is shown: prefix commands, else to the pane.
func (m Model) paneKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	ks := k.String()
	sp := m.space()
	t := sp.Tab()
	if m.prefix {
		m.prefix = false
		return m.prefixCmd(ks)
	}
	if ks == m.prefixLabel() {
		m.prefix = true
		return m, nil
	}
	// global space switching without prefix
	switch ks {
	case "alt+0", "alt+`":
		m.cur = -1
		return m, m.refreshDetail()
	case "alt+1", "alt+2", "alt+3", "alt+4", "alt+5", "alt+6", "alt+7", "alt+8", "alt+9":
		i := int(ks[4] - '1')
		if i < len(m.spaces) {
			m.cur = i
			m.layoutPanes()
		}
		return m, nil
	case "alt+right", "alt+]":
		m.cycleSpace(1)
		return m, nil
	case "alt+left", "alt+[":
		m.cycleSpace(-1)
		return m, nil
	}
	if t == nil || len(t.Panes) == 0 {
		// empty space: single-key commands
		return m.prefixCmd(ks)
	}
	p := t.Pane()
	if p.Exited() {
		switch ks {
		case "enter":
			np, err := m.spawn(p.Cwd, p.Argv)
			if err == nil {
				t.Panes[t.Focus] = np
				if id := t.SessionIDs[p.ID]; id != "" && p.Agent == "claude" {
					np.Close()
					np, _ = m.spawn(p.Cwd, append([]string{"claude", "--resume", id}, strings.Fields(m.cfg.ClaudeArgs)...))
					t.Panes[t.Focus] = np
				}
				m.layoutPanes()
			}
		case "x", "q":
			m.closePane(p)
		}
		return m, nil
	}
	if p.ReadOnly {
		return m.readOnlyKey(p, ks)
	}
	if b := keyBytes(k); len(b) > 0 {
		p.Write(b)
	}
	return m, nil
}

// readOnlyKey drives a log pane: scroll it, open a shell next to it, close it.
// Nothing typed reaches the program.
func (m Model) readOnlyKey(p *term.Pane, ks string) (tea.Model, tea.Cmd) {
	_, h := p.Size()
	switch ks {
	case "s":
		if len(p.Shell) > 0 {
			m.split(false, p.Shell)
		}
	case "q", "x", "ctrl+c":
		m.closePane(p)
	case "k", "up":
		p.Scroll(1)
	case "j", "down":
		p.Scroll(-1)
	case "pgup", "ctrl+u", "u":
		p.Scroll(h / 2)
	case "pgdown", "ctrl+d", "d", " ", "space":
		p.Scroll(-h / 2)
	case "g", "home":
		p.Scroll(1 << 30)
	case "G", "end", "esc":
		p.Scroll(-(1 << 30))
	default:
		m.setStatus(T("this pane only shows a log: nothing you type reaches it"), false)
	}
	return m, nil
}

func (m *Model) cycleSpace(d int) {
	n := len(m.spaces) + 1 // + home
	i := (m.cur + 1 + d + n) % n
	m.cur = i - 1
	m.layoutPanes()
}

func (m Model) prefixCmd(ks string) (tea.Model, tea.Cmd) {
	sp := m.space()
	var t *ws.Tab
	if sp != nil {
		t = sp.Tab()
	}
	claudeArgv := append([]string{"claude"}, strings.Fields(m.cfg.ClaudeArgs)...)
	switch ks {
	case m.prefixLabel(): // prefix twice sends it literally
		if t != nil && t.Pane() != nil {
			t.Pane().Write(keyBytes(keyPress(m.prefixLabel())))
		}
	case "c":
		return m, m.openTabIn(sp, "claude", claudeArgv)
	case "C":
		return m, m.openNewTask("")
	case "G":
		cmd := m.openGitHere(m.cur)
		return m, cmd
	case "P":
		return m.switchTab(tabPrompts)
	case "U":
		return m.switchTab(tabTasks)
	case "g":
		if t != nil && t.Pane() != nil {
			return m, m.openReview(t.Pane().Cwd)
		} else if sp != nil {
			return m, m.openReview(sp.Dir())
		}
	case "s", "t":
		return m, m.openTabIn(sp, "shell", nil)
	case "a":
		return m, m.openClassroom()
	case "v", "%", "|":
		m.split(true, nil)
	case "-", "\"":
		m.split(false, nil)
	case "V":
		m.split(true, claudeArgv)
	case "n", "tab":
		if sp != nil && len(sp.Tabs) > 0 {
			sp.Active = (sp.Active + 1) % len(sp.Tabs)
			m.layoutPanes()
		}
	case "p", "shift+tab":
		if sp != nil && len(sp.Tabs) > 0 {
			sp.Active = (sp.Active - 1 + len(sp.Tabs)) % len(sp.Tabs)
			m.layoutPanes()
		}
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		if i := int(ks[0] - '1'); sp != nil && i < len(sp.Tabs) {
			sp.Active = i
			m.layoutPanes()
		}
	case "h", "left", "k", "up":
		if t != nil && len(t.Panes) > 1 {
			t.Focus = (t.Focus - 1 + len(t.Panes)) % len(t.Panes)
		}
	case "l", "right", "j", "down", "o":
		if t != nil && len(t.Panes) > 1 {
			t.Focus = (t.Focus + 1) % len(t.Panes)
		}
	case "z":
		if t != nil {
			t.Zoom = !t.Zoom
			m.layoutPanes()
		}
	case "x":
		if t != nil && t.Pane() != nil {
			p := t.Pane()
			if p.Exited() || p.Agent == "" && m.paneState(p) == "shell" {
				m.closePane(p)
			} else {
				body := Tf("%s is still running. Closing kills it.", paneTitle(p))
				if p.Agent == "claude" {
					body = Tf("%s is still running. Closing kills it (the Claude session stays on disk, resume it any time).", paneTitle(p))
				}
				m.ask(T("Close this pane?"), body,
					func(m *Model, _ string) tea.Cmd { m.closePane(p); return nil })
			}
		}
	case "X":
		if t != nil {
			panes := append([]*term.Pane{}, t.Panes...)
			m.ask(Tf("Close tab “%s”?", t.Label), Tn(len(panes), "Kills %d pane.", "Kills %d panes."), func(m *Model, _ string) tea.Cmd {
				for _, p := range panes {
					m.closePane(p)
				}
				return nil
			})
		}
	case ",":
		if t != nil {
			m.rename(T("Rename tab"), t.Label, func(v string) { t.Label = v })
		}
	case "$", "R":
		if sp != nil {
			m.rename(T("Rename space"), sp.Name, func(v string) { sp.Name = v })
		}
	case "r":
		if sp != nil {
			for _, s := range m.sessions {
				if sp.Contains(s.Cwd) && !s.Bot() && s.Live == nil {
					return m, m.resume(s, false)
				}
			}
			m.setStatus(T("no previous session in this space"), true)
		}
	case "f":
		return m.switchTab(tabSessions)
	case "i": // the views of this space, and back
		if m.inSpace() && m.dash {
			m.back()
			return m, nil
		}
		return m.switchTab(m.tab)
	case "]":
		m.cycleSpace(1)
	case "[":
		m.cycleSpace(-1)
	case "0", "H":
		m.cur = -1
		return m, m.refreshDetail()
	case "w":
		m.openManager()
	case "N":
		return m, m.newSpacePrompt()
	case "D":
		return m, m.discoverCmd()
	case "K", ":", "ctrl+k":
		return m, m.openPalette("")
	case "A":
		m.showInbox = true
	case "S":
		return m.toggleSound()
	case "B":
		m.sidebar = !m.sidebar
		m.layoutPanes()
	case "M":
		return m, m.cycleProfile(1)
	case "u", "pgup", "[ ":
		if t != nil && t.Pane() != nil {
			_, h := t.Pane().Size()
			t.Pane().Scroll(h / 2)
		}
	case "d", "pgdown":
		if t != nil && t.Pane() != nil {
			_, h := t.Pane().Size()
			t.Pane().Scroll(-h / 2)
		}
	case "?":
		m.help = true
	case "m":
		m.appMenu(1, bodyY)
	case "q":
		return m.confirmQuit(false)
	case "Q":
		return m.confirmQuit(true)
	}
	return m, nil
}

func (m *Model) openTabIn(sp *ws.Space, label string, argv []string) tea.Cmd {
	dir := home
	if sp != nil {
		dir = sp.Dir()
		if t := sp.Tab(); t != nil && t.Pane() != nil {
			dir = t.Pane().Cwd
		}
	}
	return m.openTab(dir, label, argv)
}

// confirmQuit quits leaving panes running in the background (kill = stop them
// all instead). Without saved state nothing could reattach, so it always kills.
func (m Model) confirmQuit(kill bool) (tea.Model, tea.Cmd) {
	kill = kill || m.cfg.NoState
	n := 0
	for _, r := range m.allPanes() {
		if !r.pane.Exited() {
			n++
		}
	}
	if n == 0 {
		return m, m.quit(kill)
	}
	if kill {
		m.ask(T("Quit nanny and stop every pane?"), Tn(n, "%d pane is running and will be stopped. Your layout is saved: next start reopens your spaces and resumes every Claude session.",
			"%d panes are running and will be stopped. Your layout is saved: next start reopens your spaces and resumes every Claude session."),
			func(m *Model, _ string) tea.Cmd { return m.quit(true) })
		m.modal.quit = true
		return m, nil
	}
	m.ask(T("Quit nanny?"), Tn(n, "%d pane keeps running in the background; reopen nanny to reattach. Press k to quit and stop it instead.",
		"%d panes keep running in the background; reopen nanny to reattach. Press k to quit and stop them instead."),
		func(m *Model, _ string) tea.Cmd { return m.quit(false) })
	m.modal.quit = true
	m.modal.altKey, m.modal.altLabel = "k", T("quit & stop panes")
	m.modal.alt = func(m *Model) tea.Cmd { return m.quit(true) }
	return m, nil
}

func (m *Model) quit(kill bool) tea.Cmd {
	m.saveSpaces()
	for _, r := range m.allPanes() {
		if kill {
			r.pane.Close()
		} else {
			r.pane.Detach()
		}
	}
	return tea.Quit
}
