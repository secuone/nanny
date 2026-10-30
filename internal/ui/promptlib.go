package ui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"
	"github.com/secuone/nanny/internal/claude"
	"github.com/secuone/nanny/internal/config"
	"github.com/secuone/nanny/internal/prompts"
	"github.com/secuone/nanny/internal/queue"
	"github.com/secuone/nanny/internal/term"
)

// promptLib is the full-screen prompt library: saved prompts (pinned first,
// then by use) and the prompts you type most, mined from Claude's history.
type promptLib struct {
	saved  []config.Snippet
	used   []prompts.Used
	cmds   []prompts.Command
	loaded bool
	err    string
	rows   []libRow
	cur    int
	filter textinput.Model
	ed     *promptEditor
}

// libRow is one saved prompt or one most-used prompt.
type libRow struct {
	s *config.Snippet
	u *prompts.Used
}

func (r libRow) text() string {
	if r.s != nil {
		return r.s.Text
	}
	return r.u.Text
}

func (r libRow) id() string {
	if r.s != nil {
		return "s/" + r.s.Name
	}
	return "u/" + r.u.Text
}

type promptsMsg struct {
	h   prompts.History
	err error
}

// promptEditor is the multi-line form behind n and e.
type promptEditor struct {
	orig  string // name being edited; "" for a new prompt
	field int    // 0 name · 1 tags · 2 text
	name  textinput.Model
	tags  textinput.Model
	text  textarea.Model
}

type promptEditedMsg struct {
	path string
	err  error
}

func historyCmd() tea.Cmd {
	return func() tea.Msg {
		h, err := prompts.Mine(claude.Dir("history.jsonl"))
		return promptsMsg{h, err}
	}
}

func (m *Model) openPrompts() tea.Cmd {
	in := newInput()
	in.Prompt = "/ "
	in.Placeholder = T("filter prompts, tags, projects…")
	m.lib = &promptLib{filter: in, saved: config.LoadSnippets()}
	m.lib.rebuild()
	m.show(tabPrompts)
	return historyCmd()
}

// promptsUpdate applies the library's async results; ok is false for other messages.
func (m *Model) promptsUpdate(msg tea.Msg) (tea.Cmd, bool) {
	l := m.lib
	switch msg := msg.(type) {
	case promptsMsg:
		if l != nil {
			l.loaded = true
			l.used, l.cmds = msg.h.Prompts, msg.h.Commands
			if msg.err != nil && !os.IsNotExist(msg.err) {
				l.err = msg.err.Error()
			}
			l.rebuild()
		}
		return nil, true
	case promptEditedMsg:
		b, err := os.ReadFile(msg.path)
		_ = os.Remove(msg.path)
		if l != nil && l.ed != nil && err == nil && msg.err == nil {
			l.ed.text.SetValue(strings.TrimRight(string(b), "\n"))
		}
		return nil, true
	}
	return nil, false
}

func (l *promptLib) reload() {
	l.saved = config.LoadSnippets()
	l.rebuild()
}

// rebuild filters both sections: literal token matches in rank order, then
// fuzzy matches on the short label.
func (l *promptLib) rebuild() {
	keep := ""
	if r := l.sel(); r != nil {
		keep = r.id()
	}
	q := strings.TrimSpace(l.filter.Value())
	toks := strings.Fields(strings.ToLower(q))
	add := func(rows []libRow, hay func(libRow) string, label func(libRow) string) {
		var fz []libRow
		for _, r := range rows {
			h := strings.ToLower(hay(r))
			ok := true
			for _, t := range toks {
				if !strings.Contains(h, t) {
					ok = false
					break
				}
			}
			switch {
			case ok:
				l.rows = append(l.rows, r)
			case len(fuzzy.Find(q, []string{label(r)})) > 0:
				fz = append(fz, r)
			}
		}
		l.rows = append(l.rows, fz...)
	}
	l.rows = l.rows[:0]
	var saved, used []libRow
	for i := range l.saved {
		saved = append(saved, libRow{s: &l.saved[i]})
	}
	for i := range l.used {
		used = append(used, libRow{u: &l.used[i]})
	}
	add(saved, func(r libRow) string { return r.s.Name + " " + r.s.Text + " " + strings.Join(r.s.Tags, " ") },
		func(r libRow) string { return r.s.Name + " " + strings.Join(r.s.Tags, " ") })
	add(used, func(r libRow) string { return r.u.Text + " " + project(r.u.Project) },
		func(r libRow) string { return trunc(r.u.Text, 60) })
	l.cur = max(0, min(l.cur, len(l.rows)-1))
	for i, r := range l.rows {
		if r.id() == keep {
			l.cur = i
		}
	}
}

func (l *promptLib) sel() *libRow {
	if l.cur < 0 || l.cur >= len(l.rows) {
		return nil
	}
	return &l.rows[l.cur]
}

func (l *promptLib) nSaved() int {
	n := 0
	for _, r := range l.rows {
		if r.s != nil {
			n++
		}
	}
	return n
}

func (m Model) promptsKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	l := m.lib
	if l.ed != nil {
		return m.editorKey(k)
	}
	if l.filter.Focused() {
		switch k.String() {
		case "esc":
			l.filter.SetValue("")
			l.filter.Blur()
			l.rebuild()
			return m, nil
		case "enter", "down", "up", "tab":
			l.filter.Blur()
			if k.String() == "enter" {
				return m, nil
			}
		default:
			var cmd tea.Cmd
			l.filter, cmd = l.filter.Update(k)
			l.rebuild()
			return m, cmd
		}
	}
	r := l.sel()
	switch k.String() {
	case "esc":
		if l.filter.Value() != "" {
			l.filter.SetValue("")
			l.rebuild()
			return m, nil
		}
		m.back()
	case "j", "down":
		l.cur = min(len(l.rows)-1, l.cur+1)
	case "k", "up":
		l.cur = max(0, l.cur-1)
	case "g", "home":
		l.cur = 0
	case "G", "end":
		l.cur = max(0, len(l.rows)-1)
	case "J", "K": // jump between Saved and Most used
		if n := l.nSaved(); r != nil && r.s != nil && n < len(l.rows) {
			l.cur = n
		} else {
			l.cur = 0
		}
	case "pgdown", "ctrl+d":
		l.cur = min(len(l.rows)-1, l.cur+m.bodyHeight()/2)
	case "pgup", "ctrl+u":
		l.cur = max(0, l.cur-m.bodyHeight()/2)
	case "/":
		l.filter.Focus()
		return m, textinput.Blink
	case "enter":
		if r != nil {
			name := ""
			if r.s != nil {
				name = r.s.Name
			}
			return m, m.usePrompt(name, r.text())
		}
	case "n":
		return m, m.editPrompt(nil, "")
	case "e":
		if r != nil {
			return m, m.editPrompt(r.s, r.text())
		}
	case "s":
		if r != nil && r.s != nil {
			m.setStatus(Tf("“%s” is already saved", r.s.Name), false)
		} else if r != nil {
			return m, m.savePrompt(r.u.Text)
		}
	case "d", "x":
		if r != nil && r.s != nil {
			name := r.s.Name
			m.ask(Tf("Delete “%s”?", name), trunc(r.s.Text, 200), func(m *Model, _ string) tea.Cmd {
				m.editSaved(func(ss []config.Snippet) ([]config.Snippet, error) {
					out := ss[:0]
					for _, s := range ss {
						if s.Name != name {
							out = append(out, s)
						}
					}
					return out, nil
				}, Tf("deleted “%s”", name))
				_ = config.RenameSnippetStats(name, "")
				return nil
			})
		} else if r != nil {
			m.setStatus(T("history comes from ~/.claude/history.jsonl: s saves it instead"), true)
		}
	case "p":
		if r != nil && r.s != nil {
			name, pin := r.s.Name, !r.s.Pinned
			m.editSaved(func(ss []config.Snippet) ([]config.Snippet, error) {
				for i := range ss {
					if ss[i].Name == name {
						ss[i].Pinned = pin
					}
				}
				return ss, nil
			}, map[bool]string{true: Tf("pinned “%s”", name), false: Tf("unpinned “%s”", name)}[pin])
		} else if r != nil {
			m.setStatus(T("save it first (s), then pin it"), true)
		}
	case "y":
		if r != nil {
			return m, m.copy(r.text(), T("prompt"))
		}
	case "q":
		if r != nil {
			dir := ""
			if r.u != nil {
				dir = r.u.Project
			}
			return m, m.openQueueForm(dir, r.text(), queue.New, nil)
		}
	case "?":
		m.help = true
	case "ctrl+k", ":":
		return m, m.openPalette("")
	}
	return m, nil
}

// editSaved rewrites snippets.toml through fn and reloads the library.
func (m *Model) editSaved(fn func([]config.Snippet) ([]config.Snippet, error), ok string) bool {
	if err := config.EditSnippets(fn); err != nil {
		m.setStatus(err.Error(), true)
		return false
	}
	if m.lib != nil {
		m.lib.reload()
	}
	m.setStatus(ok, false)
	return true
}

// savePrompt asks for a name and adds a most-used prompt to Saved.
func (m *Model) savePrompt(text string) tea.Cmd {
	suggest := slugRe.ReplaceAllString(strings.ToLower(strings.Join(strings.Fields(text)[:min(3, len(strings.Fields(text)))], "-")), "")
	m.prompt(T("Save prompt"), trunc(text, 200), suggest, func(m *Model, v string) tea.Cmd {
		if v == "" {
			v = suggest
		}
		if m.editSaved(func(ss []config.Snippet) ([]config.Snippet, error) {
			for _, s := range ss {
				if s.Name == v {
					return nil, fmt.Errorf("%s", Tf("a prompt named “%s” exists", v))
				}
			}
			return append(ss, config.Snippet{Name: v, Text: text}), nil
		}, Tf("saved “%s”", v)) && m.lib != nil {
			m.lib.selectSaved(v)
		}
		return nil
	})
	return nil
}

func (l *promptLib) selectSaved(name string) {
	for i, r := range l.rows {
		if r.s != nil && r.s.Name == name {
			l.cur = i
		}
	}
}

// promptTarget is where an inserted prompt goes: the focused pane of a space
// (typed, you send it) or the agent selected in Live (submitted).
func (m *Model) promptTarget() (p *term.Pane, submit bool, where string) {
	if sp := m.space(); sp != nil {
		if t := sp.Tab(); t != nil && t.Pane() != nil && !t.Pane().Exited() {
			return t.Pane(), false, t.Label
		}
	}
	if r := m.live.sel(); r != nil && r.agent() && r.local != nil && !m.inSpace() {
		return r.local.pane, true, r.local.tab.Label
	}
	return nil, false, ""
}

// usePrompt asks for each {placeholder}, then inserts the prompt; name is the
// saved prompt's name ("" for one from history) and counts a use.
func (m *Model) usePrompt(name, text string) tea.Cmd {
	p, submit, where := m.promptTarget()
	if p == nil {
		m.setStatus(T("prompts go to the focused pane, or the agent selected in Live"), true)
		return nil
	}
	what := T("prompt")
	if name != "" {
		what = Tf("snippet “%s”", name)
	}
	phs := prompts.Placeholders(text)
	vals := map[string]string{}
	var step func(m *Model, i int) tea.Cmd
	step = func(m *Model, i int) tea.Cmd {
		if i < len(phs) {
			ph := phs[i]
			m.prompt(fmt.Sprintf("{%s}  %d/%d", ph, i+1, len(phs)), trunc(text, 160), ph, func(m *Model, v string) tea.Cmd {
				vals[ph] = v
				return step(m, i+1)
			})
			return nil
		}
		out := prompts.Fill(text, vals)
		m.back() // to the pane or the agent it went to
		if submit {
			typeInto(p, out)
			m.setStatus(Tf("%s sent to %s", what, where), false)
		} else {
			p.Write([]byte("\x1b[200~" + out + "\x1b[201~"))
			m.setStatus(Tf("%s typed · ⏎ in the pane sends it", what), false)
		}
		if name != "" {
			_ = config.UseSnippet(name)
		}
		return nil
	}
	return step(m, 0)
}

// ---------------------------------------------------------------- editor

// editPrompt opens the editor on a saved prompt, or a new one prefilled with text.
func (m *Model) editPrompt(s *config.Snippet, text string) tea.Cmd {
	p := m.st.P
	c := func(h string) lipgloss.Color { return lipgloss.Color(h) }
	e := &promptEditor{}
	e.name = newInput()
	e.name.Prompt, e.name.Placeholder = "› ", T("short name, e.g. tests")
	e.tags = newInput()
	e.tags.Prompt, e.tags.Placeholder = "› ", T("optional, comma separated")
	e.text = textarea.New()
	e.text.ShowLineNumbers = false
	e.text.Prompt = "│ "
	e.text.Placeholder = T("the prompt · {name} asks for a value when inserted")
	fs, bs := textarea.DefaultStyles()
	for _, st := range []*textarea.Style{&fs, &bs} {
		st.Text = lipgloss.NewStyle().Foreground(c(p.Text))
		st.Placeholder = lipgloss.NewStyle().Foreground(c(p.Muted))
		st.Prompt = lipgloss.NewStyle().Foreground(c(p.Overlay))
		st.EndOfBuffer = lipgloss.NewStyle().Foreground(c(p.Overlay))
		st.CursorLine = lipgloss.NewStyle().Foreground(c(p.Text))
	}
	fs.CursorLine = lipgloss.NewStyle().Foreground(c(p.Text)).Background(c(p.Surface))
	fs.Prompt = lipgloss.NewStyle().Foreground(c(p.Accent2))
	e.text.FocusedStyle, e.text.BlurredStyle = fs, bs
	e.text.SetValue(text)
	e.field = 0
	if s != nil {
		e.orig = s.Name
		e.name.SetValue(s.Name)
		e.tags.SetValue(strings.Join(s.Tags, ", "))
		e.field = 2
	}
	e.focus()
	m.lib.ed = e
	return textinput.Blink
}

func (e *promptEditor) focus() {
	e.name.Blur()
	e.tags.Blur()
	e.text.Blur()
	switch e.field {
	case 0:
		e.name.Focus()
	case 1:
		e.tags.Focus()
	default:
		e.text.Focus()
	}
}

func (m Model) editorKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	e := m.lib.ed
	switch k.String() {
	case "esc":
		m.lib.ed = nil
		return m, nil
	case "ctrl+s":
		return m, m.saveEditor(e)
	case "tab":
		e.field = (e.field + 1) % 3
		e.focus()
		return m, nil
	case "shift+tab":
		e.field = (e.field + 2) % 3
		e.focus()
		return m, nil
	case "ctrl+e":
		return m, editInEditor(e.text.Value())
	case "enter":
		if e.field < 2 {
			e.field++
			e.focus()
			return m, nil
		}
	}
	var cmd tea.Cmd
	switch e.field {
	case 0:
		e.name, cmd = e.name.Update(k)
	case 1:
		e.tags, cmd = e.tags.Update(k)
	default:
		e.text, cmd = e.text.Update(k)
	}
	return m, cmd
}

// editInEditor opens the text in $EDITOR; promptEditedMsg brings it back.
func editInEditor(text string) tea.Cmd {
	f, err := os.CreateTemp("", "nanny-prompt-*.md")
	if err != nil {
		return func() tea.Msg { return statusMsg{err.Error(), true} }
	}
	_, _ = f.WriteString(text + "\n")
	f.Close()
	ed := os.Getenv("VISUAL")
	if ed == "" {
		ed = os.Getenv("EDITOR")
	}
	if ed == "" {
		ed = "nano"
	}
	argv := append(strings.Fields(ed), f.Name())
	return tea.ExecProcess(exec.Command(argv[0], argv[1:]...), func(err error) tea.Msg { return promptEditedMsg{f.Name(), err} })
}

func (m *Model) saveEditor(e *promptEditor) tea.Cmd {
	name := strings.TrimSpace(e.name.Value())
	text := strings.TrimSpace(e.text.Value())
	if name == "" || text == "" {
		m.setStatus(T("a prompt needs a name and some text"), true)
		return nil
	}
	var tags []string
	for _, t := range strings.FieldsFunc(e.tags.Value(), func(r rune) bool { return r == ',' || r == ' ' }) {
		tags = append(tags, strings.TrimPrefix(t, "#"))
	}
	ok := m.editSaved(func(ss []config.Snippet) ([]config.Snippet, error) {
		for _, s := range ss {
			if s.Name == name && name != e.orig {
				return nil, fmt.Errorf("%s", Tf("a prompt named “%s” exists", name))
			}
		}
		for i := range ss {
			if e.orig != "" && ss[i].Name == e.orig {
				ss[i].Name, ss[i].Text, ss[i].Tags = name, text, tags
				return ss, nil
			}
		}
		return append(ss, config.Snippet{Name: name, Text: text, Tags: tags}), nil
	}, Tf("saved “%s”", name))
	if ok {
		if e.orig != "" {
			_ = config.RenameSnippetStats(e.orig, name)
			m.lib.reload()
		}
		m.lib.ed = nil
		m.lib.selectSaved(name)
	}
	return nil
}

func (m Model) viewPromptEditor() string {
	s := m.st
	e := m.lib.ed
	w := min(84, m.w-4)
	inner := w - 6
	e.name.Width, e.tags.Width = inner-4, inner-4
	e.text.SetWidth(inner)
	e.text.SetHeight(max(3, min(12, m.h-17)))
	title := T("New prompt")
	if e.orig != "" {
		title = Tf("Edit “%s”", trunc(e.orig, 40))
	}
	label := func(i int, t string) string {
		if i == e.field {
			return s.Accent.Render(t)
		}
		return s.Label.Render(t)
	}
	var b strings.Builder
	b.WriteString(s.Accent.Render(title) + "\n\n")
	b.WriteString(label(0, T("Name")) + "\n" + e.name.View() + "\n")
	b.WriteString(label(1, T("Tags")) + "\n" + e.tags.View() + "\n")
	b.WriteString(label(2, T("Text")) + s.Muted.Render("  "+T("{name} asks for a value on insert")) + "\n" + e.text.View() + "\n\n")
	b.WriteString(s.hintsFit([][2]string{{"ctrl+s", T("save")}, {"tab", T("field")}, {"ctrl+e", "$EDITOR"}, {"esc", T("cancel")}}, inner))
	return s.Modal.Width(w).Render(b.String())
}

// ---------------------------------------------------------------- view

func (m Model) viewPrompts(w, h int) string {
	s := m.st
	l := m.lib
	inner := w - 4
	// filter line: the box when in use, else what's in the library
	var head string
	if l.filter.Focused() || l.filter.Value() != "" {
		l.filter.Width = inner - 4
		head = l.filter.View()
	} else {
		head = s.Muted.Render(Tf("%d saved · %d most used", len(l.saved), len(l.used)))
		if len(l.cmds) > 0 {
			var cs []string
			for _, c := range l.cmds[:min(4, len(l.cmds))] {
				cs = append(cs, fmt.Sprintf("%s ×%d", c.Name, c.Count))
			}
			head += s.Dim.Render("   "+T("commands")+": ") + s.Muted.Render(strings.Join(cs, "  "))
		}
	}
	heads := []string{fit(head, inner), s.Dim.Render(strings.Repeat("─", inner))}
	room := h - 2 - len(heads)
	lw := max(28, min(70, inner*45/100))
	pw := inner - lw - 3

	// list: a section header rides on the first row of each section
	var blocks [][]string
	section := func(t string, n int) []string {
		return []string{s.Muted.Render(fit(t, lw-4)) + s.Dim.Render(fmt.Sprintf("%4d", n))}
	}
	nSaved := l.nSaved()
	for i, r := range l.rows {
		var b []string
		switch {
		case i == 0 && r.s != nil:
			b = section(T("SAVED"), nSaved)
		case i == nSaved && r.u != nil:
			if i > 0 {
				b = append(b, "")
			}
			b = append(b, section(T("MOST USED"), len(l.rows)-nSaved)...)
		}
		var line string
		if r.s != nil {
			mark := "  "
			if r.s.Pinned {
				mark = s.Yellow.Render("★ ")
			}
			right := ""
			if r.s.Uses > 0 {
				right = s.Teal.Render(fmt.Sprintf("×%d", r.s.Uses))
			}
			name := s.Accent.Render(trunc(r.s.Name, lw/3))
			rest := lw - 2 - lipglossW(name) - lipglossW(right) - 2
			line = fit(mark+name+" "+s.Muted.Render(trunc(r.s.Text, rest)), lw-lipglossW(right)) + right
		} else {
			badge := s.Teal.Render(fmt.Sprintf("%3s", fmt.Sprintf("×%d", r.u.Count)))
			when := s.Muted.Render(" " + ago(r.u.Last))
			line = fit(badge+" "+s.Text.Render(trunc(r.u.Text, lw-lipglossW(badge)-lipglossW(when)-2)), lw-lipglossW(when)) + when
		}
		if i == l.cur {
			line = s.Sel(s.RowSel, fit(line, lw))
		}
		blocks = append(blocks, append(b, line))
	}
	var left []string
	switch {
	case len(l.rows) > 0:
		left = strings.Split(m.listBlocks(blocks, l.cur, "prompts", room, bodyY+1+len(heads)), "\n")
	case l.filter.Value() != "":
		left = []string{s.Muted.Render(T("nothing matches"))}
	default:
		left = strings.Split(s.emptyState(T("No saved prompts yet."), [][2]string{{"n", T("new prompt")}}, lw), "\n")
	}
	if !l.loaded {
		left = append(left, "", s.Muted.Render(spinGlyph+" "+T("reading history…")))
	} else if l.err != "" {
		left = append(left, "", s.Red.Render(trunc(l.err, lw)))
	} else if len(l.used) == 0 {
		left = append(left, "", s.Muted.Render(trunc(T("Nothing typed twice in Claude's history yet."), lw)))
	}

	// preview: meta, then the full text
	var right []string
	if r := l.sel(); r != nil {
		if r.s != nil {
			right = append(right, s.Bold.Render(trunc(r.s.Name, pw)))
			meta := []string{}
			if r.s.Pinned {
				meta = append(meta, s.Yellow.Render("★ "+T("pinned")))
			}
			if r.s.Uses > 0 {
				meta = append(meta, s.Teal.Render(Tf("used %d×", r.s.Uses))+s.Muted.Render(" · "+Tf("last %s", ago(r.s.Last))))
			} else {
				meta = append(meta, s.Muted.Render(T("not used yet")))
			}
			right = append(right, fit(strings.Join(meta, s.Dim.Render(" · ")), pw))
			if len(r.s.Tags) > 0 {
				right = append(right, s.Blue.Render(trunc("#"+strings.Join(r.s.Tags, " #"), pw)))
			}
		} else {
			right = append(right, s.Bold.Render(T("Most used"))+s.Muted.Render(" · "+T("from your history")))
			right = append(right, s.Teal.Render(Tf("typed %d×", r.u.Count))+s.Muted.Render(" · "+Tf("last %s", ago(r.u.Last))))
			if r.u.Project != "" {
				where := tilde(r.u.Project)
				if n := len(r.u.Projects); n > 1 {
					where += "  " + Tf("+%d more", n-1)
				}
				right = append(right, s.Muted.Render(tailTrunc(where, pw)))
			}
		}
		if ph := prompts.Placeholders(r.text()); len(ph) > 0 {
			right = append(right, s.Peach.Render(trunc(T("asks for")+" {"+strings.Join(ph, "} {")+"}", pw)))
		}
		right = append(right, s.Dim.Render(strings.Repeat("─", pw)))
		for _, t := range wrap(r.text(), pw, room-len(right)) {
			right = append(right, s.Text.Render(t))
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
	return s.panel(T("Prompts"), strings.Join(append(heads, body...), "\n"), w, h, true)
}
