package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/secuone/nanny/internal/gitwt"
	"github.com/secuone/nanny/internal/queue"
	"github.com/secuone/nanny/internal/term"
	"github.com/secuone/nanny/internal/ws"
)

// taskQueue is the backlog nanny hands to agents (shared pointer, so it
// survives Model copies) plus the full-screen view over it.
type taskQueue struct {
	q        queue.Queue
	panes    map[string]*term.Pane // task id → pane it runs in, this process only
	starting map[string]bool       // worktree still being created
	boot     time.Time
	dirty    bool
	only     *ws.Space // the view is scoped to this space's folders
	sel      string    // selected task id
}

// bootGrace lets reattached panes register their session before a running
// task without one counts as lost.
const bootGrace = 15 * time.Second

var modeNames = map[string]string{queue.Here: M("here"), queue.New: M("new tab"), queue.Worktree: M("worktree")}

type queueWtMsg struct {
	id, from, path, label string
	argv                  []string
	err                   string
}

func loadQueue() *taskQueue {
	q, _ := queue.Load()
	return &taskQueue{q: q, panes: map[string]*term.Pane{}, starting: map[string]bool{}, boot: time.Now()}
}

func (m *Model) saveQueue() {
	m.tq.dirty = false
	if m.cfg.NoState {
		return
	}
	if err := queue.Save(m.tq.q); err != nil {
		m.setStatus(err.Error(), true)
	}
}

// ---------------------------------------------------------------- dispatcher

// dispatch runs on every refresh: running tasks end when their agent went
// from working to idle, then (auto on) pending ones start up to queue.max_parallel.
func (m *Model) dispatch() tea.Cmd {
	tq := m.tq
	now := time.Now()
	ended := tq.q.Step(m.taskState, now)
	for _, t := range ended {
		m.taskEnded(t)
	}
	var cmds []tea.Cmd
	if tq.q.Auto {
		room := tq.q.Room(m.cfg.Queue.MaxParallel)
		for _, t := range tq.q.Pending() {
			if room <= 0 {
				break
			}
			if ok, cmd := m.startTask(t); ok {
				room--
				cmds = append(cmds, cmd)
			}
		}
	}
	if len(ended) > 0 && tq.q.Idle() {
		m.fire(alertEvent{at: now, kind: alertQueueEmpty, title: T("every queued task ran"), project: T("Task queue")})
	}
	if len(ended) > 0 || tq.dirty {
		m.saveQueue()
	}
	return tea.Batch(cmds...)
}

// taskState is what the agent of a running task is doing; see queue.Task.Advance.
func (m *Model) taskState(t *queue.Task) string {
	tq := m.tq
	if tq.starting[t.ID] {
		return "starting"
	}
	p := tq.panes[t.ID]
	if p == nil && t.Session != "" {
		if r, ok := m.localPane(t.Session); ok {
			p = r.pane
			tq.panes[t.ID] = p
		}
	}
	if p == nil {
		if time.Since(tq.boot) < bootGrace {
			return "starting"
		}
		return ""
	}
	if p.Exited() {
		return "exited"
	}
	if id := m.paneSession(p); id != "" && id != t.Session {
		t.Session = id
		tq.dirty = true
	}
	st := agentState(p)
	if s := m.byID[t.Session]; st == "idle" && s != nil && s.Live != nil && s.Live.Status == "busy" {
		st = "working"
	}
	return st
}

// paneSession is the Claude session running in p, if known.
func (m *Model) paneSession(p *term.Pane) string {
	for id, r := range m.vs.bySession {
		if r.pane == p {
			return id
		}
	}
	return ""
}

// startTask hands a task to an agent by its mode; false leaves it pending
// (a "here" task whose folder's agents are all busy or waiting on you).
func (m *Model) startTask(t *queue.Task) (bool, tea.Cmd) {
	tq := m.tq
	now := time.Now()
	if _, err := os.Stat(t.Dir); err != nil {
		t.Fail(M("folder doesn't exist"), now)
		tq.dirty = true
		m.taskEnded(t)
		return false, nil
	}
	label := t.Name
	if label == "" {
		label = trunc(t.Prompt, 24)
	}
	argv := m.claudeArgv(t.Prompt)
	switch t.Mode {
	case queue.Here:
		r, busy := m.idleAgent(t.Dir)
		if r != nil {
			typeInto(r.pane, t.Prompt)
			tq.panes[t.ID] = r.pane
			t.Start(r.space.Name+"/"+r.tab.Label, now)
			t.Session = m.paneSession(r.pane)
			tq.dirty = true
			return true, nil
		}
		if busy {
			return false, nil
		}
	case queue.Worktree:
		branch := branchName(t.Name, t.Prompt)
		if !branchRe.MatchString(branch) {
			t.Fail(T("invalid branch name")+": "+branch, now)
			tq.dirty = true
			m.taskEnded(t)
			return false, nil
		}
		tq.starting[t.ID] = true
		t.Start("⎇ "+branch, now)
		tq.dirty = true
		id, from := t.ID, t.Dir
		return true, func() tea.Msg {
			root := gitwt.RepoRoot(from)
			if root == "" {
				return queueWtMsg{id: id, err: T("not a git repository: ") + tilde(from)}
			}
			path, err := gitwt.Add(root, branch, gitwt.Head(from))
			if err != nil {
				return queueWtMsg{id: id, err: err.Error()}
			}
			return queueWtMsg{id, from, path, label, argv, ""}
		}
	}
	r, err := m.spawnTab(t.Dir, label, argv)
	if err != nil {
		t.Fail(err.Error(), now)
		tq.dirty = true
		m.taskEnded(t)
		return false, nil
	}
	tq.panes[t.ID] = r.pane
	t.Start(r.space.Name+"/"+r.tab.Label, now)
	tq.dirty = true
	return true, nil
}

// queueUpdate opens the tab of a worktree task once git made the worktree.
func (m *Model) queueUpdate(msg tea.Msg) (tea.Cmd, bool) {
	w, ok := msg.(queueWtMsg)
	if !ok {
		return nil, false
	}
	tq := m.tq
	delete(tq.starting, w.id)
	t := tq.q.Find(w.id)
	if t == nil || t.Status != queue.Running {
		return nil, true
	}
	if w.err != "" {
		t.Fail(w.err, time.Now())
		m.taskEnded(t)
		m.saveQueue()
		return nil, true
	}
	if sp, _ := m.spaceFor(w.from, true); !sp.Contains(w.path) {
		sp.Paths = append(sp.Paths, w.path)
	}
	r, err := m.spawnTab(w.path, w.label, w.argv)
	if err != nil {
		t.Fail(err.Error(), time.Now())
		m.taskEnded(t)
	} else {
		tq.panes[t.ID] = r.pane
		t.Where = r.space.Name + "/" + r.tab.Label
	}
	m.saveQueue()
	m.loading["repos"] = true
	return m.reposCmd(), true
}

// idleAgent finds an idle claude pane started in dir that no running task
// holds; busy reports the folder has agents, all busy or waiting on you.
func (m *Model) idleAgent(dir string) (*paneRef, bool) {
	held := map[*term.Pane]bool{}
	for _, t := range m.tq.q.Tasks {
		if p := m.tq.panes[t.ID]; p != nil && t.Status == queue.Running {
			held[p] = true
		}
	}
	busy := false
	for _, r := range m.allPanes() {
		p := r.pane
		if p.Agent != "claude" || p.Exited() || filepath.Clean(p.Cwd) != filepath.Clean(dir) {
			continue
		}
		s := m.byID[m.paneSession(p)]
		if held[p] || agentState(p) != "idle" || s != nil && s.Live != nil && s.Live.Status == "busy" {
			busy = true
			continue
		}
		return &r, false
	}
	return nil, busy
}

func (m *Model) taskEnded(t *queue.Task) {
	ev := alertEvent{at: time.Now(), kind: alertQueue, title: "✓ " + taskTitle(t), project: project(t.Dir)}
	if t.Status == queue.Failed {
		ev.title, ev.detail = "✕ "+taskTitle(t), T(t.Err)
	}
	if t.Session != "" {
		ev.key = "s:" + t.Session
	}
	m.fire(ev)
}

func taskTitle(t *queue.Task) string {
	if t.Name != "" {
		return t.Name
	}
	return oneLine(t.Prompt)
}

// queueBadge is the header's "☰ 3 queued" while tasks wait or run.
func (m Model) queueBadge() string {
	n := m.tq.q.Count(queue.Pending) + m.tq.q.Count(queue.Running)
	if n == 0 {
		return ""
	}
	return m.st.Accent.Render(fmt.Sprintf("☰ %d", n)) + m.st.Muted.Render(" "+T("queued"))
}

// ---------------------------------------------------------------- actions

func (m *Model) openQueue() {
	m.show(tabTasks)
}

// openQueueForm is the new-task form in queue mode; edit is the task to change.
func (m *Model) openQueueForm(dir, prompt, mode string, edit *queue.Task) tea.Cmd {
	if edit != nil {
		dir, prompt, mode = edit.Dir, edit.Prompt, edit.Mode
	}
	cmd := m.openNewTask(dir)
	t := m.task
	t.queued, t.mode, t.wt = true, mode, mode == queue.Worktree
	t.prompt.SetValue(prompt)
	t.prompt.CursorEnd()
	if edit != nil {
		t.edit = edit.ID
		t.name.SetValue(edit.Name)
	}
	return cmd
}

func (m *Model) queueFromForm(f *newTask) tea.Cmd {
	d := f.dir()
	text := strings.TrimSpace(f.prompt.Value())
	switch {
	case d == nil:
		m.setStatus(T("pick a folder first"), true)
		return nil
	case text == "":
		m.setStatus(T("a queued task needs a prompt"), true)
		return nil
	}
	name := strings.TrimSpace(f.name.Value())
	tq := m.tq
	if t := tq.q.Find(f.edit); t != nil {
		t.Prompt, t.Dir, t.Mode, t.Name = text, d.path, f.mode, name
	} else {
		t := queue.NewTask(text, d.path, f.mode, name)
		tq.q.Tasks = append(tq.q.Tasks, t)
		tq.sel = t.ID
	}
	m.saveQueue()
	msg := Tf("queued: %s", trunc(text, 40))
	if !tq.q.Auto {
		msg += " · " + T("u then a starts the queue")
	}
	m.setStatus(msg, false)
	return nil
}

// runNow starts a task at once, whatever the cap and auto-dispatch say.
func (m *Model) runNow(t *queue.Task) tea.Cmd {
	if t.Status == queue.Failed || t.Status == queue.Done {
		t.Retry()
	}
	ok, cmd := m.startTask(t)
	switch {
	case ok:
		m.setStatus(Tf("started in %s", t.Where), false)
	case t.Status == queue.Pending:
		m.setStatus(Tf("every agent in %s is busy: it waits for one", project(t.Dir)), true)
	}
	m.saveQueue()
	return cmd
}

// goToTask focuses the pane a task runs in.
func (m *Model) goToTask(t *queue.Task) {
	p := m.tq.panes[t.ID]
	if r, ok := m.localPane(t.Session); p == nil && ok {
		p = r.pane
	}
	for _, r := range m.allPanes() {
		if p != nil && r.pane == p {
			m.focusRef(r)
			return
		}
	}
	m.setStatus(T("its agent isn't in a nanny pane anymore"), true)
}

// ---------------------------------------------------------------- view

// rows lists the tasks by section: pending in queue order, running, then
// done and failed, newest first.
func (tq *taskQueue) rows() []*queue.Task {
	var pend, run, end []*queue.Task
	for _, t := range tq.q.Tasks {
		if tq.only != nil && !tq.only.Covers(t.Dir) {
			continue
		}
		switch t.Status {
		case queue.Pending:
			pend = append(pend, t)
		case queue.Running:
			run = append(run, t)
		default:
			end = append(end, t)
		}
	}
	sort.SliceStable(end, func(i, j int) bool { return end[i].Ended.After(end[j].Ended) })
	return append(append(pend, run...), end...)
}

func (tq *taskQueue) cur(rows []*queue.Task) int {
	for i, t := range rows {
		if t.ID == tq.sel {
			return i
		}
	}
	return 0
}

func (tq *taskQueue) selTask() *queue.Task {
	rows := tq.rows()
	if len(rows) == 0 {
		return nil
	}
	return rows[tq.cur(rows)]
}

func (m Model) queueKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	tq := m.tq
	rows := tq.rows()
	i := tq.cur(rows)
	t := tq.selTask()
	to := func(j int) {
		if len(rows) > 0 {
			tq.sel = rows[max(0, min(len(rows)-1, j))].ID
		}
	}
	switch k.String() {
	case "esc":
		m.back()
	case "q":
		if !m.inSpace() {
			return m.confirmQuit(false)
		}
	case "j", "down":
		to(i + 1)
	case "k", "up":
		to(i - 1)
	case "home":
		to(0)
	case "end":
		to(len(rows) - 1)
	case "pgdown", "ctrl+d":
		to(i + m.bodyHeight()/2)
	case "pgup", "ctrl+u":
		to(i - m.bodyHeight()/2)
	case "n":
		dir := ""
		if t != nil {
			dir = t.Dir
		}
		return m, m.openQueueForm(dir, "", queue.New, nil)
	case "e":
		if t != nil && (t.Status == queue.Pending || t.Status == queue.Failed) {
			return m, m.openQueueForm("", "", "", t)
		} else if t != nil {
			m.setStatus(T("only pending or failed tasks can be edited"), true)
		}
	case "d", "x":
		if t != nil {
			body := trunc(t.Prompt, 200)
			if t.Status == queue.Running {
				body = T("Its agent keeps running; the queue just stops watching it.")
			}
			id := t.ID
			m.ask(Tf("Remove “%s”?", trunc(taskTitle(t), 40)), body, func(m *Model, _ string) tea.Cmd {
				m.tq.q.Remove(id)
				delete(m.tq.panes, id)
				m.saveQueue()
				return nil
			})
		}
	case "J", "K":
		d := 1
		if k.String() == "K" {
			d = -1
		}
		if t != nil && tq.q.Move(t.ID, d) {
			m.saveQueue()
		}
	case "enter":
		if t == nil {
			return m, nil
		}
		if t.Status == queue.Running {
			m.goToTask(t)
			return m, nil
		}
		return m, m.runNow(t)
	case "g":
		if t != nil {
			m.goToTask(t)
		}
	case "r":
		if t != nil && (t.Status == queue.Failed || t.Status == queue.Done) {
			t.Retry()
			m.saveQueue()
			m.setStatus(Tf("back in the queue: %s", trunc(taskTitle(t), 40)), false)
		}
	case "a":
		tq.q.Auto = !tq.q.Auto
		m.saveQueue()
		if tq.q.Auto {
			m.setStatus(Tn(m.cfg.Queue.MaxParallel, "auto-dispatch on: up to %d agent at once", "auto-dispatch on: up to %d agents at once"), false)
			return m, m.dispatch()
		}
		m.setStatus(T("auto-dispatch off: ⏎ starts a task"), false)
	case "c":
		keep := tq.q.Tasks[:0]
		for _, t := range tq.q.Tasks {
			if t.Status == queue.Pending || t.Status == queue.Running {
				keep = append(keep, t)
			}
		}
		tq.q.Tasks = keep
		m.saveQueue()
	case "?":
		m.help = true
	case "ctrl+k", ":":
		return m, m.openPalette("")
	}
	return m, nil
}

func (m Model) viewQueue(w, h int) string {
	s := m.st
	tq := m.tq
	inner := w - 4
	rows := tq.rows()
	cur := tq.cur(rows)
	nPend, nRun := tq.q.Count(queue.Pending), tq.q.Count(queue.Running)
	auto := s.Red.Render("⏸ "+T("auto off")) + s.Muted.Render(" · "+T("a starts it, ⏎ runs one now"))
	if tq.q.Auto {
		auto = s.Green.Render("▶ "+T("auto on")) + s.Muted.Render(" · "+Tn(m.cfg.Queue.MaxParallel, "up to %d agent at once", "up to %d agents at once"))
	}
	heads := []string{fit(auto, inner), s.Dim.Render(strings.Repeat("─", inner))}
	room := h - 2 - len(heads)
	lw := max(28, min(70, inner*45/100))
	pw := inner - lw - 3

	var blocks [][]string
	section := func(t string, n int) []string {
		return []string{s.Muted.Render(fit(t, lw-4)) + s.Dim.Render(fmt.Sprintf("%4d", n))}
	}
	for i, t := range rows {
		var b []string
		if i == 0 || rows[i-1].Status != t.Status && !(isEnded(rows[i-1]) && isEnded(t)) {
			if i > 0 {
				b = append(b, "")
			}
			switch t.Status {
			case queue.Pending:
				b = append(b, section(T("PENDING"), nPend)...)
			case queue.Running:
				b = append(b, section(T("RUNNING"), nRun)...)
			default:
				b = append(b, section(T("DONE"), len(rows)-nPend-nRun)...)
			}
		}
		right := s.Muted.Render(" " + ago(t.Added))
		switch t.Status {
		case queue.Pending:
			right = s.Muted.Render(" " + T(modeNames[t.Mode]))
		case queue.Running:
			right = s.Muted.Render(" " + ago(t.Started))
		case queue.Done, queue.Failed:
			right = s.Muted.Render(" " + ago(t.Ended))
		}
		head := m.taskGlyph(t) + " " + s.Muted.Render(trunc(project(t.Dir), lw/4)) + " "
		line := fit(head+s.Text.Render(trunc(taskTitle(t), lw-lipglossW(head)-lipglossW(right))), lw-lipglossW(right)) + right
		if i == cur {
			line = s.Sel(s.RowSel, fit(line, lw))
		}
		blocks = append(blocks, append(b, line))
	}
	var left []string
	if len(rows) > 0 {
		left = strings.Split(m.listBlocks(blocks, cur, "queue", room, bodyY+1+len(heads)), "\n")
	} else {
		left = strings.Split(s.emptyState(T("Nothing queued. Add tasks and nanny hands them to Claude agents, a few at a time."),
			[][2]string{{"n", T("add a task")}, {"a", T("auto-dispatch on/off")}}, lw), "\n")
	}

	var right []string
	if t := tq.selTask(); t != nil {
		right = append(right, m.taskGlyph(t)+" "+m.taskStatus(t))
		right = append(right, s.kv(T("folder"), tailTrunc(tilde(t.Dir), pw-10), pw))
		right = append(right, s.kv(T("run in"), T(modeNames[t.Mode]), pw))
		if t.Name != "" {
			right = append(right, s.kv(T("name"), t.Name, pw))
		}
		if t.Where != "" {
			right = append(right, s.kv(T("agent"), t.Where, pw))
		}
		right = append(right, s.kv(T("added"), Tf("%s ago", ago(t.Added)), pw))
		if !t.Ended.IsZero() {
			right = append(right, s.kv(T("took"), t.Ended.Sub(t.Started).Round(time.Second).String(), pw))
		}
		if t.Err != "" {
			right = append(right, s.Red.Render(trunc("✕ "+T(t.Err), pw)))
		}
		right = append(right, s.Dim.Render(strings.Repeat("─", pw)))
		for _, l := range wrap(t.Prompt, pw, room-len(right)) {
			right = append(right, s.Text.Render(l))
		}
	}
	sep := s.Dim.Render("│")
	body := make([]string, room)
	for i := range body {
		a, b := "", ""
		if i < len(left) {
			a = left[i]
		}
		if i < len(right) {
			b = right[i]
		}
		body[i] = fit(a, lw) + " " + sep + " " + fit(b, pw)
	}
	title := T("Task queue")
	if nPend+nRun > 0 {
		title += " · " + Tf("%d pending · %d running", nPend, nRun)
	}
	return s.panel(title+m.scopeNote(), strings.Join(append(heads, body...), "\n"), w, h, true)
}

func isEnded(t *queue.Task) bool { return t.Status == queue.Done || t.Status == queue.Failed }

func (m Model) taskGlyph(t *queue.Task) string {
	s := m.st
	switch t.Status {
	case queue.Running:
		if p := m.tq.panes[t.ID]; p != nil && !p.Exited() && agentState(p) == "blocked" {
			return m.stateGlyph("blocked")
		}
		return m.stateGlyph("working")
	case queue.Done:
		return s.Green.Render("✓")
	case queue.Failed:
		return s.Red.Render("✕")
	}
	return s.Muted.Render("·")
}

func (m Model) taskStatus(t *queue.Task) string {
	s := m.st
	switch t.Status {
	case queue.Running:
		if p := m.tq.panes[t.ID]; p != nil && !p.Exited() && agentState(p) == "blocked" {
			return m.stateLabel("blocked")
		}
		if m.tq.starting[t.ID] {
			return s.Yellow.Render(T("creating the worktree…"))
		}
		return m.stateLabel("working")
	case queue.Done:
		return s.Green.Render(T("done"))
	case queue.Failed:
		return s.Red.Render(T("failed"))
	}
	return s.Muted.Render(T("pending"))
}

// modePicker shows the queue modes with the chosen one highlighted.
func (m Model) modePicker(mode string) string {
	s := m.st
	var parts []string
	for _, md := range queue.Modes {
		if md == mode {
			parts = append(parts, s.Green.Render("["+T(modeNames[md])+"]"))
		} else {
			parts = append(parts, s.Muted.Render(" "+T(modeNames[md])+" "))
		}
	}
	return strings.Join(parts, " ")
}
