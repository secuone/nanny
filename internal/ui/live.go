package ui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/secuone/nanny/internal/claude"
	"github.com/secuone/nanny/internal/queue"
)

type liveRow struct {
	sess   *claude.Session
	sub    *claude.FlatSub
	hidden int      // finished subagents not shown under this agent
	fold   int      // a row folding its agent's finished subagents: how many
	unseen bool     // finished while you weren't looking
	local  *paneRef // the nanny pane running this agent
	total  int
	run    int
}

// agent is a row for an agent itself: not a subagent nor its finished fold.
func (r liveRow) agent() bool { return r.sub == nil && r.fold == 0 }

func (r liveRow) state() string {
	if r.agent() && r.unseen {
		if st := r.rawState(); st == "idle" {
			return "done"
		}
	}
	return r.rawState()
}

func (r liveRow) rawState() string {
	if r.sub != nil {
		return r.sub.Status()
	}
	if r.local != nil {
		switch st := agentState(r.local.pane); st {
		case "working", "blocked":
			return st
		}
		if r.sess != nil && r.sess.Live != nil && r.sess.Live.Status == "busy" {
			return "working"
		}
		return "idle"
	}
	if r.sess != nil && r.sess.Live != nil && r.sess.Live.Status == "busy" {
		return "working"
	}
	return "idle"
}

type liveTab struct {
	cur     cursor
	rows    []liveRow
	allRows []liveRow // unscoped, for the sidebar
	showAll bool
	screen  bool
	marks   map[string]bool // agents picked with space for a broadcast prompt
	only    string          // a state picked from the header counts: working, done, idle, blocked or subs
	open    map[string]bool // agents whose finished subagents are unfolded
}

func (t *liveTab) sel() *liveRow {
	if t.cur.idx < 0 || t.cur.idx >= len(t.rows) {
		return nil
	}
	return &t.rows[t.cur.idx]
}

func (t *liveTab) rebuild(m *Model) {
	var keep string
	if r := t.sel(); r != nil {
		keep = r.key()
	}
	var live []*claude.Session
	for _, s := range m.sessions {
		if s.Live != nil {
			live = append(live, s)
		}
	}
	rank := map[string]int{"blocked": 0, "working": 1, "done": 2, "idle": 3}
	var rows []liveRow
	for _, s := range live {
		r := liveRow{sess: s, unseen: m.alerts.unseen["s:"+s.ID]}
		if lr, ok := m.localPane(s.ID); ok {
			lr := lr
			r.local = &lr
		}
		rows = append(rows, r)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		ri, rj := rank[rows[i].state()], rank[rows[j].state()]
		if ri != rj {
			return ri < rj
		}
		return rows[i].started().After(rows[j].started())
	})
	t.rows = t.rows[:0]
	for _, r := range rows {
		var subs []claude.FlatSub
		old := 0 // finished a while ago: folded under one row unless opened
		if r.sess != nil {
			open := t.showAll || t.open[r.sess.ID]
			for _, f := range claude.Flatten(m.subs[r.sess.Path]) {
				r.total++
				if f.Running {
					r.run++
				}
				recent := f.Running || time.Since(f.Modified) < 2*time.Minute
				if !recent {
					old++
				}
				if recent || open {
					subs = append(subs, f)
				} else {
					r.hidden++
				}
			}
		}
		t.rows = append(t.rows, r)
		for i := range subs {
			t.rows = append(t.rows, liveRow{sess: r.sess, sub: &subs[i]})
		}
		if old > 0 && !t.showAll {
			t.rows = append(t.rows, liveRow{sess: r.sess, fold: old})
		}
	}
	t.allRows = t.allRows[:0]
	for _, r := range t.rows {
		if r.fold == 0 {
			t.allRows = append(t.allRows, r)
		}
	}
	if m.basic() { // subagents only as a count on their agent's row
		agents := t.rows[:0:0]
		for _, r := range t.rows {
			if r.agent() {
				agents = append(agents, r)
			}
		}
		t.rows = agents
	}
	t.rows = m.scopedRows(t.rows)
	if t.only != "" {
		t.rows = t.filter(t.rows)
	}
	for i, r := range t.rows {
		if r.key() == keep {
			t.cur.idx = i
		}
	}
	t.cur.clamp(len(t.rows), 1<<30)
}

// filter keeps the agents in the state picked from the header, with their
// subagents; "subs" keeps the agents with subagents running and those.
func (t *liveTab) filter(rows []liveRow) []liveRow {
	var out []liveRow
	keep := false
	for _, r := range rows {
		if r.agent() {
			if t.only == "subs" {
				keep = r.run > 0
			} else {
				keep = r.state() == t.only
			}
		}
		if keep && (r.agent() || t.only != "subs" || r.sub != nil && r.sub.Running) {
			out = append(out, r)
		}
	}
	return out
}

// markSeen clears the ◆ done badge of the selected agent.
func (t *liveTab) markSeen(m *Model) {
	if r := t.sel(); r != nil && r.agent() && m.alerts.unseen[r.key()] {
		delete(m.alerts.unseen, r.key())
		r.unseen = false
	}
}

func (r liveRow) key() string {
	switch {
	case r.sub != nil:
		return "sub:" + r.sub.ID
	case r.fold > 0:
		return "fold:" + r.sess.ID
	case r.sess != nil:
		return "s:" + r.sess.ID
	}
	return ""
}

func (r liveRow) titleCwd() (string, string) {
	if r.sess != nil {
		if r.sess.Title == "" {
			return T("new session"), r.sess.Cwd
		}
		return r.sess.Title, r.sess.Cwd
	}
	return "", ""
}

func (r liveRow) started() time.Time {
	if r.sess != nil && r.sess.Live != nil {
		return r.sess.Live.Started()
	}
	return time.Time{}
}

func (t *liveTab) key(m *Model, k tea.KeyPressMsg) tea.Cmd {
	n := len(t.rows)
	h := m.bodyHeight()
	defer t.markSeen(m)
	switch k.String() {
	case "j", "down":
		t.cur.move(1, n, h)
	case "k", "up":
		t.cur.move(-1, n, h)
	case "J", "ctrl+n": // next agent, skipping subagents
		for i := t.cur.idx + 1; i < n; i++ {
			if t.rows[i].agent() {
				t.cur.idx = i
				break
			}
		}
	case "K", "ctrl+p":
		for i := t.cur.idx - 1; i >= 0; i-- {
			if t.rows[i].agent() {
				t.cur.idx = i
				break
			}
		}
	case "g", "home":
		t.cur.idx = 0
	case "G", "end":
		t.cur.idx = n - 1
	case "a":
		t.showAll = !t.showAll
		t.rebuild(m)
	case "O": // the agent's PR in the browser
		if r := t.sel(); r != nil {
			_, cwd := r.titleCwd()
			if p, ok := m.prOf(cwd); ok {
				return m.openURL(p.URL)
			}
			m.setStatus(T("no open PR for this branch"), true)
		}
	case " ", "space":
		if r := t.sel(); r != nil && r.agent() {
			if t.marks == nil {
				t.marks = map[string]bool{}
			}
			if t.marks[r.key()] {
				delete(t.marks, r.key())
			} else {
				t.marks[r.key()] = true
			}
			t.cur.move(1, n, h)
		}
	case "p":
		if len(t.marks) > 0 {
			return m.broadcast()
		}
		if r := t.sel(); r != nil {
			return m.sendPrompt(r)
		}
	case "n":
		dir := ""
		if r := t.sel(); r != nil && r.sess != nil {
			dir = r.sess.Cwd
		}
		return m.openNewTask(dir)
	case "C":
		if r := t.sel(); r != nil && r.sess != nil {
			return m.openReview(r.sess.Cwd)
		}
	case "+":
		if r := t.sel(); r != nil && r.sess != nil {
			return m.openQueueForm(r.sess.Cwd, "", queue.Here, nil)
		}
	case ".":
		if r := t.sel(); r != nil && r.agent() {
			return m.repeatPrompt(r)
		}
	case "v", "V":
		if r := t.sel(); r != nil && r.sess != nil {
			if r.sub != nil {
				return m.openViewer(r.sub.Path, true, r.sub.Description, r.sess, r.sub.Subagent)
			}
			return m.openViewer(r.sess.Path, false, r.sess.Title, r.sess, nil)
		}
	case "s":
		if r := t.sel(); r != nil && r.local == nil {
			m.setStatus(T("screen mirror needs an agent running in a nanny pane"), true)
			return nil
		}
		t.screen = !t.screen
		return m.refreshDetail()
	case "c":
		if r := t.sel(); r != nil && r.sess != nil {
			return m.copy(r.sess.Cwd, T("folder"))
		}
	case "enter":
		r := t.sel()
		if r == nil {
			return nil
		}
		if r.fold > 0 {
			t.toggleFold(m, r.sess.ID)
			return m.refreshDetail()
		}
		if r.sub != nil {
			return m.openViewer(r.sub.Path, true, r.sub.Description, r.sess, r.sub.Subagent)
		}
		if r.local != nil && r.state() == "blocked" && m.openAnswer(*r.local, r.sess.Title) {
			return nil
		}
		if r.local != nil {
			m.focusRef(*r.local)
			return nil
		}
		return m.resume(r.sess, false)
	case "r":
		if r := t.sel(); r != nil {
			return m.resume(r.sess, false)
		}
	case "f":
		if r := t.sel(); r != nil && r.sess != nil {
			return m.resume(r.sess, true)
		}
	case "o":
		if r := t.sel(); r != nil && r.sess != nil {
			return m.shell(r.sess.ResumeCwd())
		}
	case "y":
		if r := t.sel(); r != nil && r.sess != nil {
			return m.copy("cd "+shellQuote(r.sess.ResumeCwd())+" && "+m.resumeCmd(r.sess, false), T("resume command"))
		}
	case "i":
		if r := t.sel(); r != nil && r.sess != nil {
			return m.copy(r.sess.ID, T("session id"))
		}
	}
	t.cur.clamp(n, h)
	return nil
}

func (m Model) stateGlyph(state string) string {
	s := m.st
	switch state {
	case "working": // only agents spin; a running subagent is a steady dot
		return s.Yellow.Render(spinGlyph)
	case "running":
		return s.Yellow.Render("●")
	case "blocked":
		return s.Red.Render(blinkGlyph)
	case "done":
		return s.Blue.Render("◆")
	case "finished":
		return s.Green.Render("✓")
	}
	return s.Green.Render("○")
}

func (m Model) stateLabel(state string) string {
	s := m.st
	switch state {
	case "working":
		return s.Yellow.Render(T("working"))
	case "running":
		return s.Yellow.Render(T("running"))
	case "blocked":
		return s.Red.Render(T("needs you"))
	case "done":
		return s.Blue.Render(T("done"))
	case "finished":
		return s.Green.Render(T("finished"))
	}
	return s.Green.Render(T("idle"))
}

func project(cwd string) string {
	if cwd == "" {
		return "?"
	}
	if repo, _, ok := claude.HomeWorktree(cwd); ok {
		return repo
	}
	if i := strings.Index(cwd, "/.claude/worktrees/"); i >= 0 {
		return filepath.Base(cwd[:i])
	}
	if i := strings.Index(cwd, ".worktrees/"); i >= 0 {
		return strings.TrimSuffix(filepath.Base(cwd[:i+len(".worktrees")]), ".worktrees")
	}
	if cwd == home {
		return "~"
	}
	return filepath.Base(cwd)
}

func (t *liveTab) view(m *Model, w, h int) string {
	s := m.st
	inner := w - 4
	if len(t.rows) == 0 {
		msg := s.emptyState(T("No agents running right now. Agents you start here or in any terminal show up with their subagents, live."),
			[][2]string{{"n", T("start a new task")}}, inner)
		switch {
		case m.spaceScope() != nil:
			msg = s.emptyState(Tf("No agents in %s", spaceLabel(m.spaceScope())), [][2]string{{"n", T("start a new task")}}, inner) +
				"\n  " + m.allSpacesLink(T("see agents from all spaces"))
		case m.scope != "":
			msg = s.emptyState(Tf("No agents running in %s.", m.scope), [][2]string{{"\\", T("show every project")}}, inner)
		}
		if t.only != "" {
			msg = s.emptyState(Tf("No agents %s right now.", T(t.only)), [][2]string{{"esc", T("show every agent")}}, inner)
		}
		if m.loading["sessions"] {
			msg = s.emptyState(spinGlyph+" "+T("scanning sessions…"), nil, inner)
		}
		return s.panel(T("Live agents"), msg, w, h, !m.focusR)
	}
	blocks := make([][]string, len(t.rows))
	for i, r := range t.rows {
		selected := i == t.cur.idx
		var lines []string
		switch {
		case r.fold > 0:
			lines = []string{m.foldLine(r, inner-1)}
		case r.sub != nil:
			lines = []string{m.subLine(r, inner-1)}
		case m.basic():
			lines = []string{m.agentLine(r, inner-1)}
		default:
			lines = m.agentLines(r, inner-1)
		}
		if selected {
			st := s.RowSel
			if m.focusR {
				st = s.RowSelBlur
			}
			for j := range lines {
				lines[j] = s.Sel(st, fit(lines[j], inner-1))
			}
		}
		if r.agent() {
			bar := " "
			switch r.state() {
			case "blocked":
				bar = s.Red.Render("▌")
			case "working":
				bar = s.Yellow.Render("▏")
			case "done":
				bar = s.Blue.Render("▌")
			}
			for j := range lines {
				if j == 0 && t.marks[r.key()] {
					lines[j] = s.Accent.Render("✓") + lines[j]
					continue
				}
				lines[j] = bar + lines[j]
			}
			if i > 0 && !m.basic() {
				lines = append([]string{""}, lines...)
			}
		} else {
			lines[0] = " " + lines[0]
		}
		blocks[i] = lines
	}
	title := T("Live agents") + fmt.Sprintf(" · %d", countAgents(t.rows)) + m.scopeNote()
	if t.only != "" {
		title += " · " + T("only") + " " + T(t.only)
	}
	if len(t.marks) > 0 {
		title += " · " + Tf("%d picked", len(t.marks))
	}
	return s.panel(title, m.listBlocks(blocks, t.cur.idx, "live", bodyRows(h), bodyY+1+gapOf(h)), w, h, !m.focusR)
}

func countAgents(rows []liveRow) int {
	n := 0
	for _, r := range rows {
		if r.agent() {
			n++
		}
	}
	return n
}

func (m Model) agentLines(r liveRow, w int) []string {
	s := m.st
	state := r.state()
	title, cwd, age := "", "", ""
	if r.sess != nil {
		title = r.sess.Title
		cwd = r.sess.Cwd
		if cwd == "" {
			cwd = r.sess.Live.Cwd
		}
		age = ago(r.sess.Live.Started())
	}
	if title == "" {
		title = T("(untitled)")
	}
	right := m.stateLabel(state)
	glyph := m.stateGlyph(state)
	if state == "idle" && r.run > 0 { // parent waits while background subagents work
		right = s.Teal.Render(Tn(r.run, "%d subagent", "%d subagents"))
		glyph = s.Teal.Render(spinGlyph)
	}
	l1 := glyph + " " + s.Bold.Render(trunc(title, w-16))
	l1 = fit(l1, w-lipglossW(right)) + right

	meta := []string{s.Muted.Render(project(cwd))}
	if r.sess != nil {
		if r.sess.Worktree != "" {
			meta = append(meta, s.Muted.Render("⎇ "+r.sess.Worktree))
		} else if r.sess.Branch != "" {
			meta = append(meta, s.Muted.Render("⎇ "+r.sess.Branch))
		}
		// its checkout's state (only when there is something to do) and its PR
		git := ""
		if c, ok := m.stateOf(cwd); ok && c.dirty+c.ahead+c.behind > 0 {
			git = m.branchBits(c)
		}
		if p, ok := m.prOf(cwd); ok {
			git = strings.TrimSpace(git + " " + m.prBadge(p))
		}
		if git != "" {
			meta[len(meta)-1] += " " + git
		}
		if mdl := shortModel(r.sess.Model); mdl != "" && m.full() {
			meta = append(meta, s.Muted.Render(mdl))
		}
	}
	if r.local != nil {
		meta = append(meta, s.Accent.Render("◆ "+r.local.space.Name+"/"+r.local.tab.Label))
	}
	tail := ""
	if r.total > 0 {
		tail = s.Muted.Render(fmt.Sprintf("↳ %d/%d", r.run, r.total))
		if r.hidden > 0 {
			tail += s.Muted.Render(fmt.Sprintf(" (+%d)", r.hidden))
		}
		tail += "  "
	}
	tail += s.Muted.Render(age)
	// the gauge (and cost) always when full; else only once the context fills up
	var u *claude.Usage
	if r.sess != nil {
		u = r.sess.Usage
	}
	if u != nil && (m.full() || u.Pct() >= 60) { // keep room for the project before the gauge
		room := w - lipglossW(tail) - 3 - 16
		if !m.full() {
			room = min(room, lipglossW(m.meter(u, 1)))
		}
		if g := m.fitMeter(u, room); g != "" {
			tail = " " + g + "  " + tail
		}
	}
	l2 := "  " + strings.Join(meta, s.Dim.Render(" · "))
	l2 = fit(l2, w-lipglossW(tail)) + tail
	if p := r.prompt(); p != nil {
		hint := s.Muted.Render("⏎ " + T("answer"))
		l3 := "  " + s.Yellow.Render("⚠ "+trunc(promptSummary(p), w-14))
		return []string{l1, l2, fit(l3, w-lipglossW(hint)) + hint}
	}
	return []string{l1, l2}
}

func (m Model) subLine(r liveRow, w int) string {
	s := m.st
	sub := r.sub
	branch := "├─"
	if sub.Last {
		branch = "└─"
	}
	indent := "  " + strings.Repeat("│  ", sub.Level)
	state := "finished"
	if sub.Running {
		state = "running"
	}
	head := s.Dim.Render(indent+branch) + " " + m.stateGlyph(state) + " " + s.Muted.Render(sub.Type) + " "
	desc := sub.Description
	tail := s.Muted.Render(ago(sub.Modified))
	if sub.Running && sub.LastTool != "" {
		tool := trunc(strings.Fields(sub.LastTool)[0], 18)
		tail = s.Muted.Render(tool) + " " + tail
	}
	room := w - lipglossW(head) - lipglossW(tail) - 1
	return head + fit(s.Text.Render(trunc(desc, room)), room) + " " + tail
}

// foldLine is the row folding an agent's subagents that finished a while ago.
func (m Model) foldLine(r liveRow, w int) string {
	s := m.st
	arrow, hint := "▸", T("⏎ show")
	if m.live.open[r.sess.ID] {
		arrow, hint = "▾", T("⏎ hide")
	}
	head := s.Dim.Render("  └─") + " " + s.Green.Render("✓") + " " + s.Muted.Render(arrow+" "+Tn(r.fold, "%d finished subagent", "%d finished subagents"))
	tail := s.Muted.Render(hint)
	var last *claude.FlatSub
	for _, f := range claude.Flatten(m.subs[r.sess.Path]) {
		if !f.Running && (last == nil || f.Modified.After(last.Modified)) {
			last = &f
		}
	}
	if last != nil && !m.live.open[r.sess.ID] {
		room := w - lipglossW(head) - lipglossW(tail) - 6
		if room > 12 {
			head += s.Dim.Render(" · ") + s.Muted.Render(trunc(T("last")+": "+last.Description, room))
		}
	}
	return fit(head, w-lipglossW(tail)) + tail
}

// toggleFold shows or folds an agent's finished subagents.
func (t *liveTab) toggleFold(m *Model, id string) {
	if t.open == nil {
		t.open = map[string]bool{}
	}
	t.open[id] = !t.open[id]
	t.rebuild(m)
}

func shortModel(id string) string {
	id = strings.TrimPrefix(id, "claude-")
	if i := strings.LastIndex(id, "-2"); i > 0 && len(id)-i > 8 { // drop date suffixes
		id = id[:i]
	}
	return id
}

func (t *liveTab) detail(m *Model, w, h int) string {
	s := m.st
	r := t.sel()
	inner := w - 4
	if r == nil {
		return s.panel(T("Get started"), m.welcomeCard(inner), w, h, m.focusR)
	}
	var ls []string
	add := func(l ...string) { ls = append(ls, l...) }
	if r.sub != nil {
		sub := r.sub
		add(s.Muted.Render(sub.Type)+"  "+m.stateLabel(map[bool]string{true: "running", false: "finished"}[sub.Running]), "")
		for _, l := range wrap(sub.Description, inner, 4) {
			add(s.Bold.Render(l))
		}
		add("")
		add(s.kv(T("agent id"), sub.ID, inner))
		add(s.kv(T("parent"), trunc(r.sess.Title, inner-10), inner))
		if sub.Model != "" {
			add(s.kv(T("model"), sub.Model, inner))
		}
		mode := T("foreground")
		if sub.Background {
			mode = T("background")
		}
		add(s.kv(T("mode"), mode, inner))
		add(s.kv(T("started"), sub.Started.Format("15:04:05")+"  ("+Tf("%s ago", ago(sub.Started))+")", inner))
		add(s.kv(T("last act."), Tf("%s ago", ago(sub.Modified)), inner))
		if sub.Worktree != "" {
			add(s.kv(T("worktree"), sub.Worktree, inner))
		}
		if sub.LastTool != "" {
			add(s.kv(T("last tool"), sub.LastTool, inner))
		}
		add("", s.Accent.Render(T("Activity")))
		headN := len(ls)
		add(m.entryLines(m.transcripts[sub.Path], inner)...)
		return s.panel(T("Subagent"), m.detailTail(ls, headN, h), w, h, m.focusR)
	}
	if t.screen && r.local != nil {
		add(s.Muted.Render(T("screen")+" · "+r.local.space.Name+"/"+r.local.tab.Label+" · "+T("s to hide")), "")
		for _, l := range strings.Split(strings.TrimRight(r.local.pane.Text(h), "\n"), "\n") {
			add(s.Text.Render(trunc(l, inner)))
		}
		if len(ls) > bodyRows(h) && m.detailOff == 0 {
			ls = append(ls[:2], ls[len(ls)-(bodyRows(h)-2):]...)
		}
		return s.panel(T("Screen"), m.detailScroll(ls, h), w, h, m.focusR)
	}
	if r.sess != nil {
		if p := r.prompt(); p != nil {
			add(s.Red.Render("● " + T("Waiting for you")))
			add(m.promptLines(p, inner, 6)...)
			add("")
			for _, o := range p.Options {
				add("  " + s.Key.Render(o.Key) + "  " + s.Text.Render(trunc(o.Label, inner-5)))
			}
			add("", s.hintsFit([][2]string{{"⏎", T("answer here")}, {"s", T("screen")}, {"!", T("next")}}, inner), "")
		}
		ls = append(ls, m.sessionHeader(r.sess, inner)...)
		ls = append(ls, m.usageLines(r.sess.Usage, inner)...)
		if subs := m.subs[r.sess.Path]; len(subs) > 0 {
			add("", s.Accent.Render(T("Subagents")+" · "+Tf("%d running / %d", r.run, r.total)))
			var done []claude.FlatSub
			for _, f := range claude.Flatten(subs) {
				if !f.Running {
					done = append(done, f)
					continue
				}
				add(strings.Repeat("  ", f.Level) + m.stateGlyph("running") + " " + s.Muted.Render(f.Type) + " " + s.Text.Render(trunc(f.Description, inner-len(f.Type)-6)))
				if f.LastTool != "" {
					add(strings.Repeat("  ", f.Level) + "  " + s.Muted.Render(trunc("↳ "+f.LastTool, inner-4)))
				}
			}
			if len(done) > 0 { // what they did, newest first
				sort.SliceStable(done, func(i, j int) bool { return done[i].Modified.After(done[j].Modified) })
				add("", s.Accent.Render(Tn(len(done), "%d finished", "%d finished")))
				for _, f := range done {
					age := s.Muted.Render(ago(f.Modified))
					room := inner - len(f.Type) - lipglossW(age) - 5
					add(m.stateGlyph("finished") + " " + s.Muted.Render(f.Type) + " " + fit(s.Text.Render(trunc(f.Description, room)), room) + " " + age)
				}
			}
		}
		add("", s.Accent.Render(T("Recent")))
		headN := len(ls)
		add(m.entryLines(m.transcripts[r.sess.Path], inner)...)
		title := T("Agent")
		if r.local != nil {
			title += " · s " + T("screen")
		}
		return s.panel(title, m.detailTail(ls, headN, h), w, h, m.focusR)
	}
	return s.panel(T("Agent"), m.detailScroll(ls, h), w, h, m.focusR)
}

// entryLines renders transcript entries newest-last, chat style.
func (m Model) entryLines(es []claude.Entry, w int) []string {
	s := m.st
	if len(es) == 0 {
		return []string{s.Muted.Render("…")}
	}
	var out []string
	for _, e := range es {
		switch e.Role {
		case "user":
			out = append(out, "")
			for i, l := range wrap(e.Text, w-2, 6) {
				p := "  "
				if i == 0 {
					p = s.Accent.Render("❯ ")
				}
				out = append(out, p+s.Bold.Render(l))
			}
		case "assistant":
			for i, l := range wrap(e.Text, w-2, 8) {
				p := "  "
				if i == 0 {
					p = s.Teal.Render("● ")
				}
				out = append(out, p+s.Text.Render(l))
			}
		case "tool":
			name, arg, _ := strings.Cut(e.Text, " ")
			out = append(out, "  "+s.Dim.Render("⚙ ")+s.Muted.Render(name+" "+trunc(arg, w-6-len(name))))
		}
	}
	return out
}

// welcomeCard is the empty-Live card: what nanny shows and the first keys to press.
func (m Model) welcomeCard(w int) string {
	s := m.st
	ls := []string{s.Bold.Render(T("Welcome to nanny"))}
	for _, l := range wrap(T("Real terminals, every agent and its subagents live, and every past session one key away."), w, 6) {
		ls = append(ls, s.Muted.Render(l))
	}
	ls = append(ls, "", s.Accent.Render(T("Get started")))
	rows := [][2]string{{"D", T("discover where you work")}, {"N", T("new space from a folder")}}
	if len(m.spaces) > 0 {
		rows = [][2]string{{"alt+1", T("open your first space")}, {"W", T("manage spaces")}}
	}
	rows = append(rows, [2]string{"n", T("new task: folder, prompt, worktree")}, [2]string{"2", T("sessions · ⏎ resumes one")}, [2]string{"^k", T("jump to anything")}, [2]string{"?", T("all keys")})
	ls = append(ls, s.keyList(rows, w)...)
	var recent []string
	for _, ses := range m.sessions {
		if len(recent) == 5 {
			break
		}
		if ses.Bot() || ses.Title == "" || !m.inScope(ses.Cwd) {
			continue
		}
		age := s.Muted.Render(ago(ses.Modified))
		l := "  " + s.Text.Render(trunc(ses.Title, w-20)) + s.Dim.Render(" · ") + s.Muted.Render(project(ses.Cwd))
		recent = append(recent, fit(l, w-lipglossW(age))+age)
	}
	if len(recent) > 0 {
		ls = append(ls, "", s.Accent.Render(T("Pick up where you left off"))+s.Muted.Render("  · "+T("2 then ⏎")))
		ls = append(ls, recent...)
	}
	return strings.Join(ls, "\n")
}
