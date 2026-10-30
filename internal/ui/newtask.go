package ui

import (
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/secuone/nanny/internal/gitwt"
	"github.com/secuone/nanny/internal/queue"
	"github.com/secuone/nanny/internal/term"
)

// newTask is the "new task in one go" form: folder, name, first prompt and
// an optional fresh worktree, then claude starts in a new tab.
type newTask struct {
	dirs   []taskDir
	shown  []int
	cur    int // index into shown
	field  int // 0 where · 1 name · 2 prompt · 3 worktree
	where  textinput.Model
	name   textinput.Model
	prompt textinput.Model
	wt     bool
	fill   promptFill
	// queued: the form adds a task to the queue instead of starting it
	queued bool
	mode   string // queue.Here, New or Worktree
	edit   string // id of the queued task being edited
	exact  bool   // the name is the branch as it is (an issue's), not a slug of it
}

type taskDir struct{ path, label string }

const (
	fieldWhere = iota
	fieldName
	fieldPrompt
	fieldWT
)

type taskReadyMsg struct {
	from, path, label string
	argv              []string
}

func (m *Model) openNewTask(prefer string) tea.Cmd {
	t := &newTask{dirs: m.taskDirs(prefer), field: fieldPrompt}
	mk := func(prompt, ph string) textinput.Model {
		in := newInput()
		in.Prompt, in.Placeholder = prompt, ph
		return in
	}
	t.where = mk("› ", T("type to filter folders"))
	t.name = mk("› ", T("optional · tab label and branch name"))
	t.prompt = mk("› ", T("what should Claude do first?"))
	t.prompt.CharLimit = 4000
	t.filter()
	t.focus()
	m.task = t
	return textinput.Blink
}

// openNewTaskWith opens the form already filled: the name (a branch when exact
// comes with a worktree) and the first prompt, ready to start with ⏎.
func (m *Model) openNewTaskWith(prefer, name, prompt string, wt bool) tea.Cmd {
	cmd := m.openNewTask(prefer)
	t := m.task
	t.name.SetValue(name)
	t.prompt.SetValue(prompt)
	t.wt, t.exact = wt, wt
	return cmd
}

// taskDirs lists where a task can start: prefer, spaces, checkouts, recent folders.
func (m *Model) taskDirs(prefer string) []taskDir {
	var out []taskDir
	seen := map[string]bool{}
	add := func(p, label string) {
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, taskDir{p, label})
	}
	if sp := m.space(); sp != nil && prefer == "" {
		prefer = sp.Dir()
	}
	if prefer != "" {
		label := project(prefer)
		if worktreesDir(prefer) != prefer+".worktrees" {
			label += " ⎇ " + filepath.Base(prefer)
		}
		add(prefer, label)
	}
	for _, sp := range m.spaces {
		for _, p := range sp.Paths {
			add(p, sp.Name)
		}
	}
	for _, r := range m.repos {
		for _, w := range r.Worktrees {
			if w.Missing {
				continue
			}
			label := r.Name
			if !w.Main {
				label += " ⎇ " + filepath.Base(w.Path)
			}
			add(w.Path, label)
		}
	}
	for _, s := range m.sessions {
		if len(out) >= 60 {
			break
		}
		if !s.Bot() && s.Cwd != "" && s.Cwd != home {
			add(s.Cwd, project(s.Cwd))
		}
	}
	return out
}

func (t *newTask) filter() {
	toks := strings.Fields(strings.ToLower(t.where.Value()))
	t.shown = t.shown[:0]
	for i, d := range t.dirs {
		h := strings.ToLower(d.label + " " + tilde(d.path))
		ok := true
		for _, tk := range toks {
			if !strings.Contains(h, tk) {
				ok = false
				break
			}
		}
		if ok {
			t.shown = append(t.shown, i)
		}
	}
	t.cur = max(0, min(t.cur, len(t.shown)-1))
}

func (t *newTask) dir() *taskDir {
	if t.cur < 0 || t.cur >= len(t.shown) {
		return nil
	}
	return &t.dirs[t.shown[t.cur]]
}

func (t *newTask) focus() {
	for i, in := range []*textinput.Model{&t.where, &t.name, &t.prompt} {
		if i == t.field {
			in.Focus()
		} else {
			in.Blur()
		}
	}
}

var slugRe = regexp.MustCompile(`[^a-z0-9._/-]+`)

// branch derives the worktree branch from the name, else the prompt's first words.
func (t *newTask) branch() string {
	if n := strings.TrimSpace(t.name.Value()); t.exact && n != "" {
		return n
	}
	return branchName(t.name.Value(), t.prompt.Value())
}

func branchName(name, prompt string) string {
	src := strings.TrimSpace(name)
	if src == "" {
		w := strings.Fields(prompt)
		src = strings.Join(w[:min(5, len(w))], " ")
	}
	b := strings.Trim(slugRe.ReplaceAllString(strings.ToLower(src), "-"), "-./")
	if len(b) > 40 {
		b = strings.TrimRight(b[:40], "-./")
	}
	if b == "" {
		b = "task-" + time.Now().Format("0102-1504")
	}
	return b
}

func (t *newTask) label() string {
	if n := strings.TrimSpace(t.name.Value()); n != "" {
		return trunc(n, 28)
	}
	if p := strings.TrimSpace(t.prompt.Value()); p != "" {
		return trunc(p, 24)
	}
	return "claude"
}

// claudeArgv starts claude with the configured args and an optional first prompt.
func (m *Model) claudeArgv(prompt string) []string {
	argv := append([]string{"claude"}, strings.Fields(m.cfg.ClaudeArgs)...)
	if prompt = strings.TrimSpace(prompt); prompt != "" {
		argv = append(argv, prompt)
	}
	return argv
}

func (m Model) taskKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	t := m.task
	ks := k.String()
	switch ks {
	case "esc", "ctrl+c":
		m.task = nil
		return m, nil
	case "enter":
		m.task = nil
		if t.queued {
			return m, m.queueFromForm(t)
		}
		return m, m.launchTask(t)
	case "tab":
		t.field = (t.field + 1) % 4
		t.focus()
		return m, nil
	case "shift+tab":
		t.field = (t.field + 3) % 4
		t.focus()
		return m, nil
	case "up", "down", "ctrl+p", "ctrl+n":
		d := 1
		if ks == "up" || ks == "ctrl+p" {
			d = -1
		}
		if t.field == fieldWhere {
			t.cur = max(0, min(len(t.shown)-1, t.cur+d))
		} else {
			t.field = (t.field + 4 + d) % 4
			t.focus()
		}
		return m, nil
	}
	if t.field == fieldWT && t.queued {
		switch ks {
		case " ", "space", "right", "x", "y", "n":
			t.mode = cycleStr(queue.Modes, t.mode, 1)
		case "left":
			t.mode = cycleStr(queue.Modes, t.mode, -1)
		}
		t.wt = t.mode == queue.Worktree
		return m, nil
	}
	if t.field == fieldWT {
		switch ks {
		case " ", "space", "left", "right", "x", "y", "n":
			t.wt = !t.wt
		}
		return m, nil
	}
	var cmd tea.Cmd
	switch t.field {
	case fieldWhere:
		t.where, cmd = t.where.Update(k)
		t.filter()
	case fieldName:
		t.name, cmd = t.name.Update(k)
	case fieldPrompt:
		if d := t.dir(); d != nil {
			t.fill.dir = d.path
		}
		if m.fillKey(&t.fill, ks, &t.prompt) {
			return m, nil
		}
		t.prompt, cmd = t.prompt.Update(k)
	}
	return m, cmd
}

func (m *Model) launchTask(t *newTask) tea.Cmd {
	d := t.dir()
	if d == nil {
		m.setStatus(T("pick a folder first"), true)
		return nil
	}
	argv := m.claudeArgv(t.prompt.Value())
	label := t.label()
	if !t.wt {
		return m.openTab(d.path, label, argv)
	}
	branch, from := t.branch(), d.path
	if !branchRe.MatchString(branch) {
		m.setStatus(T("invalid branch name")+": "+branch, true)
		return nil
	}
	m.setStatus(Tf("creating worktree %s…", branch), false)
	return func() tea.Msg {
		root := gitwt.RepoRoot(from)
		if root == "" {
			return statusMsg{T("not a git repository: ") + tilde(from), true}
		}
		path, err := gitwt.Add(root, branch, gitwt.Head(from))
		if err != nil {
			return statusMsg{err.Error(), true}
		}
		return taskReadyMsg{from, path, label, argv}
	}
}

// taskReady opens the tab once the worktree exists; the worktree joins the
// space of the folder it was branched from.
func (m *Model) taskReady(msg taskReadyMsg) tea.Cmd {
	if sp, _ := m.spaceFor(msg.from, true); !sp.Contains(msg.path) {
		sp.Paths = append(sp.Paths, msg.path)
	}
	m.loading["repos"] = true
	return tea.Batch(m.openTab(msg.path, msg.label, msg.argv), m.reposCmd())
}

func (m Model) viewTask() string {
	s := m.st
	t := m.task
	w := min(86, m.w-4)
	inner := w - 6
	lab := func(f int, name string) string {
		if t.field == f {
			return s.Accent.Render(fit(name, 10))
		}
		return s.Label.Render(fit(name, 10))
	}
	for _, in := range []*textinput.Model{&t.where, &t.name, &t.prompt} {
		in.SetWidth(inner - 14)
	}
	var ls []string
	head := s.Accent.Render(T("New task")) + s.Muted.Render("  "+T("claude starts in a new tab with your prompt"))
	if t.queued {
		head = s.Accent.Render(T("Queue a task")) + s.Muted.Render("  "+T("it starts when an agent is free"))
		if t.edit != "" {
			head = s.Accent.Render(T("Edit queued task"))
		}
	}
	ls = append(ls, head, "")
	ls = append(ls, lab(fieldWhere, T("where"))+t.where.View())
	rows := 3
	if m.h >= 30 {
		rows = 5
	}
	start := max(0, min(t.cur-rows/2, len(t.shown)-rows))
	for i := start; i < min(len(t.shown), start+rows); i++ {
		d := t.dirs[t.shown[i]]
		lw := min(30, (inner-10)/2)
		mark := "  "
		if i == t.cur {
			mark = s.Accent.Render("❯ ")
		}
		row := fit(mark+s.Text.Render(trunc(d.label, lw-3)), lw) + s.Muted.Render(tailTrunc(tilde(d.path), inner-10-lw))
		if i == t.cur {
			row = s.Sel(s.RowSel, row)
		}
		ls = append(ls, "          "+row)
	}
	if len(t.shown) == 0 {
		ls = append(ls, "          "+s.Muted.Render(T("no folder matches")))
	}
	for len(ls) < 3+rows {
		ls = append(ls, "")
	}
	ls = append(ls, lab(fieldName, T("name"))+t.name.View(), lab(fieldPrompt, T("prompt"))+t.prompt.View())
	box := "[ ]"
	if t.wt {
		box = s.Green.Render("[x]")
	}
	if t.queued {
		ls = append(ls, lab(fieldWT, T("run in"))+m.modePicker(t.mode)+s.Muted.Render("  space"))
	} else {
		ls = append(ls, lab(fieldWT, T("worktree"))+box+" "+s.Text.Render(T("new git worktree and branch"))+s.Muted.Render("  space"))
	}
	if t.wt {
		where := ""
		if d := t.dir(); d != nil {
			where = Tf("in %s", tilde(worktreesDir(d.path))+"/")
		}
		ls = append(ls, "          "+s.Blue.Render(trunc("⎇ "+t.branch(), inner/2))+" "+s.Muted.Render(trunc(where, inner/2-2)))
	} else {
		ls = append(ls, "")
	}
	ok := T("start")
	if t.queued {
		ok = T("queue")
	}
	ls = append(ls, "", s.hintsFit([][2]string{{"⏎", ok}, {"tab", T("next field")}, {"↑↓", T("pick folder")}, {"^r", T("most used")}, {"esc", T("cancel")}}, inner))
	for i := range ls {
		ls[i] = fit(ls[i], inner)
	}
	return s.Modal.Width(w + 2).Render(strings.Join(ls, "\n"))
}

// worktreesDir is where gitwt.Add puts new worktrees for dir, without running git.
func worktreesDir(dir string) string {
	if i := strings.Index(dir, "/.claude/worktrees/"); i >= 0 {
		dir = dir[:i]
	} else if i := strings.Index(dir, ".worktrees/"); i >= 0 {
		dir = dir[:i]
	}
	return dir + ".worktrees"
}

// typeInto pastes text into a pane and submits it.
func typeInto(p *term.Pane, text string) {
	p.Write([]byte("\x1b[200~" + text + "\x1b[201~"))
	p.Write([]byte("\r"))
}

// broadcast sends one prompt to every agent picked with space in Live.
func (m *Model) broadcast() tea.Cmd {
	var refs []paneRef
	outside := 0
	for _, r := range m.live.allRows {
		if r.sub != nil || !m.live.marks[r.key()] {
			continue
		}
		if r.local == nil {
			outside++
			continue
		}
		refs = append(refs, *r.local)
	}
	if len(refs) == 0 {
		m.setStatus(T("the picked agents run outside nanny — open them here (⏎) to type into them"), true)
		return nil
	}
	body := Tn(len(refs), "Typed into %d agent pane and submitted.", "Typed into %d agent panes and submitted.")
	if outside > 0 {
		body += " " + Tn(outside, "%d picked agent runs outside nanny and is skipped.", "%d picked agents run outside nanny and are skipped.")
	}
	m.prompt(Tn(len(refs), "Send to %d agent", "Send to %d agents"), body, T("e.g. rebase on main and rerun the tests"),
		func(m *Model, v string) tea.Cmd {
			if v == "" {
				return nil
			}
			for _, r := range refs {
				typeInto(r.pane, v)
			}
			m.live.marks = nil
			m.setStatus(Tn(len(refs), "sent to %d agent", "sent to %d agents"), false)
			return nil
		})
	m.modal.fill = &promptFill{}
	return nil
}

// tailTrunc keeps the end of a path, which tells folders apart.
func tailTrunc(p string, w int) string {
	if w <= 1 || lipglossW(p) <= w {
		return trunc(p, w)
	}
	r := []rune(p)
	for len(r) > 0 && lipglossW(string(r))+1 > w {
		r = r[1:]
	}
	return "…" + string(r)
}
