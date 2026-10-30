package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/secuone/nanny/internal/config"
)

// Customize (= or the ≡ menu) turns any part of nanny on and off: whole
// views, each Monitor source, bits of the sidebar and of the top bar. What is
// off is gone: not on the bar, not on its key, and a Monitor source turned off
// isn't even collected. It acts on the screen profile in use (profiles.go);
// a built-in one is copied into one of yours on the first change.

// custNode is one switch; a node with kids switches them all, and with an id
// of its own (the Monitor) it is a switch too.
type custNode struct {
	id, name, help string
	kids           []custNode
}

var custTree = []custNode{
	{"", M("Layout"), M("How much each row says."), []custNode{
		{config.LayoutDetail, M("Detail"), M("The detail pane, subagents on rows of their own, the full top bar. Off: one line per agent."), nil},
		{config.LayoutExtras, M("Extras"), M("The model, the context gauge always, agents in the sidebar on Live too, zero counts, every footer key."), nil},
	}},
	{"", M("Views"), M("Each view on the bar and its number. Off, it is gone from both and from the palette."), []custNode{
		{"view.live", M("Live"), "", nil},
		{"view.sessions", M("Sessions"), "", nil},
		{"view.git", M("Git"), M("Off, GitHub isn't asked and no branches are looked at."), []custNode{
			{"git.prs", M("My PRs"), "", nil},
			{"git.reviews", M("Reviews"), "", nil},
			{"git.worktrees", M("Worktrees"), "", nil},
			{"git.branches", M("Branches"), "", nil},
			{"git.profile", M("Profile"), "", nil},
		}},
		{"view.tasks", M("Tasks"), M("The view only: queued tasks still run."), nil},
		{"view.prompts", M("Prompts"), "", nil},
		{"view.skills", M("Skills"), "", nil},
		{"view.monitor", M("Monitor"), M("Off, nothing of it is collected."), []custNode{
			{"monitor.machine", M("Machine"), M("Load, memory, disk and what Claude takes; today's sessions."), nil},
			{"monitor.ports", M("Ports"), M("The TCP ports your programs listen on."), nil},
			{"monitor.containers", M("Containers"), M("docker ps."), nil},
			{"monitor.tilt", M("Tilt"), M("tilt get uiresources."), nil},
			{"monitor.jobs", M("Jobs"), M("Claude background jobs and daemon, pr-loop, opencode."), nil},
			{"monitor.cron", M("Cron"), M("crontab entries and systemd user timers."), nil},
			{"monitor.hermes", M("Hermes"), M("Hermes Agent: gateway, platforms, cron and sessions (~/.hermes)."), nil},
			{"monitor.watchers", M("Watchers"), M("Your own [[watch]] entries in the config."), nil},
		}},
		{"view.jira", M("Jira"), M("Off, Jira isn't asked."), nil},
		{"view.insights", M("Insights"), M("Off, your transcripts aren't read for it."), nil},
	}},
	{"", M("Sidebar"), M("What shows under each space and below them. B hides the whole sidebar."), []custNode{
		{"sidebar.branch", M("Branch and its state"), "", nil},
		{"sidebar.pr", M("Pull request"), "", nil},
		{"sidebar.worktrees", M("Worktrees"), "", nil},
		{"sidebar.agents", M("Agents"), M("A few or all is set in Settings (Sidebar agents)."), nil},
		{"sidebar.hermes", M("Hermes dot"), "", nil},
		{"sidebar.jira", M("Jira key of the branch"), "", nil},
	}},
	{"", M("Top bar"), "", []custNode{
		{"header.counts", M("Agent counts"), "", nil},
		{"header.queue", M("Queued tasks"), "", nil},
		{"header.reviews", M("Reviews waiting"), "", nil},
		{"header.cost", M("Today's cost"), "", nil},
		{"header.sound", M("Sound"), "", nil},
		{"header.clock", M("Clock"), "", nil},
	}},
}

// custPreset is a quick setup from the customize screen.
type custPreset struct {
	key, name string
	on        []string // turned on, after turning off what reset says
	reset     []string // ids (or a node's whole subtree) turned off first
}

var custPresets = []custPreset{
	{"a", M("everything"), []string{"Layout", "Views", "Sidebar", "Top bar"}, nil},
	{"e", M("essentials"), []string{"view.live", "view.sessions", "sidebar.branch", "header.counts", "header.sound"}, []string{"Views", "Sidebar", "Top bar"}},
	{"t", M("Monitor: only Tilt"), []string{"view.monitor", "monitor.tilt"}, []string{"view.monitor"}},
	{"d", M("Monitor: ports, containers, Tilt"), []string{"view.monitor", "monitor.ports", "monitor.containers", "monitor.tilt"}, []string{"view.monitor"}},
	{"n", M("no Monitor"), nil, []string{"view.monitor"}},
}

type customize struct {
	cur    int
	shut   map[string]bool  // folded nodes, by name
	name   *textinput.Model // the name of a new profile, or a new name
	rename bool
	del    bool   // X once: a second one deletes
	note   string // where the last change went, when it made a copy
}

type custRow struct {
	n     *custNode
	depth int
}

func (m *Model) openCustomize() { m.cust = &customize{shut: map[string]bool{"Git": true}} } // its parts fold until you open them

// rows is the tree as it shows, folded nodes without their kids.
func (c *customize) rows() []custRow {
	var out []custRow
	var walk func(ns []custNode, d int)
	walk = func(ns []custNode, d int) {
		for i := range ns {
			n := &ns[i]
			out = append(out, custRow{n, d})
			if len(n.kids) > 0 && !c.shut[n.name] {
				walk(n.kids, d+1)
			}
		}
	}
	walk(custTree, 0)
	return out
}

func (m *Model) custOn(id string) bool { return m.cfg.On(id) }

// custSet switches a part in the profile in use; a built-in one is copied
// into one of yours first, and the screen says so.
func (m *Model) custSet(id string, on bool) {
	if m.cfg.On(id) == on {
		return
	}
	was := m.cfg.ActiveProfile()
	m.cfg.SetOn(id, on)
	if now := m.cfg.ActiveProfile(); now != was {
		m.cust.note = Tf("%s is built in: your changes go to %s (R renames it)", profileLabel(was), now)
	}
}

// Switch states: off, some of it, all of it.
const (
	custOff = iota
	custSome
	custAll
)

func (m *Model) custState(n *custNode) int {
	if n.id != "" && !m.custOn(n.id) {
		return custOff
	}
	if len(n.kids) == 0 {
		return custAll
	}
	seen := map[int]bool{}
	for i := range n.kids {
		seen[m.custState(&n.kids[i])] = true
	}
	switch {
	case !seen[custOff] && !seen[custSome]:
		return custAll
	case n.id == "" && !seen[custAll] && !seen[custSome]:
		return custOff
	}
	return custSome
}

// custSwitch turns a node on with everything under it, or off.
func (m *Model) custSwitch(n *custNode, on bool) {
	if n.id != "" {
		m.custSet(n.id, on)
		if !on {
			return // its kids keep their say for when it is back
		}
	}
	for i := range n.kids {
		m.custSwitch(&n.kids[i], on)
	}
}

// custFind is the node with this id or name.
func custFind(ns []custNode, key string) *custNode {
	for i := range ns {
		if ns[i].id == key || ns[i].id == "" && ns[i].name == key {
			return &ns[i]
		}
		if n := custFind(ns[i].kids, key); n != nil {
			return n
		}
	}
	return nil
}

func (m *Model) custPreset(p custPreset) {
	for _, k := range p.reset {
		if n := custFind(custTree, k); n != nil {
			for i := range n.kids {
				m.custSwitch(&n.kids[i], false)
			}
			if n.id != "" {
				m.custSet(n.id, false)
			}
		}
	}
	for _, k := range p.on {
		if n := custFind(custTree, k); n != nil {
			if len(n.kids) > 0 && n.id == "" {
				m.custSwitch(n, true)
			} else {
				m.custSet(n.id, true)
			}
		}
	}
}

func (m Model) customizeKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	c := m.cust
	if c.name != nil {
		return m.custName(k)
	}
	rows := c.rows()
	c.cur = max(0, min(c.cur, len(rows)-1))
	r := rows[c.cur]
	ks := k.String()
	if ks != "X" {
		c.del = false
	}
	switch ks {
	case "tab", "]":
		return m, m.cycleProfile(1)
	case "shift+tab", "[":
		return m, m.cycleProfile(-1)
	case "N":
		m.custNewName()
		return m, textinput.Blink
	case "R":
		if cur := m.cfg.ActiveProfile(); config.BuiltinProfile(cur) {
			m.setStatus(Tf("%s is built in: N makes a copy to rename", profileLabel(cur)), true)
			return m, nil
		}
		m.custAsk(true, m.cfg.ActiveProfile())
		return m, textinput.Blink
	case "X":
		return m.custDelete()
	case "esc", "q", "=":
		m.cust = nil
	case "j", "down":
		c.cur = min(len(rows)-1, c.cur+1)
	case "k", "up":
		c.cur = max(0, c.cur-1)
	case "g", "home":
		c.cur = 0
	case "G", "end":
		c.cur = len(rows) - 1
	case "left", "h":
		if len(r.n.kids) > 0 && !c.shut[r.n.name] {
			c.shut[r.n.name] = true
		} else { // to its parent
			for i := c.cur - 1; i >= 0; i-- {
				if rows[i].depth < r.depth {
					c.cur = i
					break
				}
			}
		}
	case "right", "l":
		delete(c.shut, r.n.name)
	case " ", "space", "enter", "x":
		m.custSwitch(r.n, m.custState(r.n) != custAll)
		m.live.rebuild(&m)
		return m, m.saveConfig()
	default:
		for _, p := range custPresets {
			if ks == p.key {
				m.custPreset(p)
				m.live.rebuild(&m)
				m.setStatus(Tf("preset: %s", T(p.name)), false)
				return m, m.saveConfig()
			}
		}
	}
	return m, nil
}

func (m Model) viewCustomize() string {
	s := m.st
	c := m.cust
	w := min(84, m.w-6)
	inner := w - 6
	var b strings.Builder
	b.WriteString(s.Accent.Render(T("Customize")) + s.Muted.Render("  "+T("what shows and what runs · off is gone, and not collected")) + "\n")
	b.WriteString(m.custProfiles(inner) + "\n")
	b.WriteString(s.Yellow.Render(fit(c.note, inner)) + "\n")
	rows := c.rows()
	c.cur = max(0, min(c.cur, len(rows)-1))
	room := max(5, min(len(rows), m.h-14))
	start := max(0, min(c.cur-room/2, len(rows)-room))
	m.vs.modalY, m.vs.modalStart, m.vs.modalN = 4, start, min(len(rows), start+room)-start
	for i := start; i < min(len(rows), start+room); i++ {
		r := rows[i]
		box := map[int]string{custOff: s.Muted.Render("☐"), custSome: s.Yellow.Render("◩"), custAll: s.Green.Render("☑")}[m.custState(r.n)]
		fold := "  "
		if len(r.n.kids) > 0 {
			fold = "▾ "
			if c.shut[r.n.name] {
				fold = "▸ "
			}
		}
		name := T(r.n.name)
		if r.depth == 0 {
			name = s.Bold.Render(name)
		} else if m.custState(r.n) == custOff {
			name = s.Muted.Render(name)
		}
		line := strings.Repeat("   ", r.depth) + s.Dim.Render(fold) + box + " " + name
		if i == c.cur {
			line = s.Sel(s.RowSel, fit(line, inner))
		}
		b.WriteString(line + "\n")
	}
	help := ""
	if r := rows[c.cur]; r.n.help != "" {
		help = T(r.n.help)
	}
	hl := wrap(help, inner, 2)
	if len(hl) == 0 {
		hl = []string{""}
	}
	for _, l := range hl {
		b.WriteString(s.Muted.Render(l) + "\n")
	}
	// the presets, as many on a line as fit
	line, sep := s.Muted.Render(T("presets")+":")+" ", ""
	for _, p := range custPresets {
		ps := s.Accent.Render(p.key) + " " + s.Muted.Render(T(p.name))
		if lipglossW(line)+3+lipglossW(ps) > inner {
			b.WriteString(line + "\n")
			line, sep = "  ", ""
		}
		line += sep + ps
		sep = s.Dim.Render(" · ")
	}
	b.WriteString(line + "\n")
	b.WriteString(s.hintsFit([][2]string{{"space", T("on/off")}, {"←→", T("fold")}, {"tab", T("profile")}, {"N", T("new")}, {"R", T("rename")}, {"X", T("delete")}, {"esc", T("close")}}, inner))
	return s.Modal.Width(w).Render(b.String())
}

// custProfiles is the profile line: every profile, the one being edited
// marked, or the name being typed.
func (m Model) custProfiles(w int) string {
	s := m.st
	if c := m.cust; c.name != nil {
		what := T("new profile")
		if c.rename {
			what = T("rename")
		}
		return s.Muted.Render(what+" ") + c.name.View()
	}
	line := s.Muted.Render(T("profile") + ": ")
	for i, n := range m.cfg.ProfileNames() {
		name := profileLabel(n)
		if n == m.cfg.ActiveProfile() {
			name = s.Accent.Render("[" + name + "]")
		} else {
			name = s.Muted.Render(name)
		}
		if i > 0 {
			line += " "
		}
		line += name
	}
	return fit(line, w)
}
