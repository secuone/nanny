package ui

import (
	"fmt"
	"os"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/secuone/nanny/internal/claude"
)

var home, _ = os.UserHomeDir()

func lipglossW(s string) int { return lipgloss.Width(s) }

func tilde(p string) string {
	if strings.HasPrefix(p, home) {
		return "~" + p[len(home):]
	}
	return p
}

type sessTab struct {
	cur      cursor
	filter   textinput.Model
	rows     []*claude.Session
	hideBots bool
	onlyWT   bool
	onlyLive bool
	total    int
	deepQ    string
	deep     map[string][]claude.Hit // full-text search results; nil when inactive
	deepBusy bool
}

type deepMsg struct {
	q    string
	hits map[string][]claude.Hit
}

func (t *sessTab) sel() *claude.Session {
	if t.cur.idx < 0 || t.cur.idx >= len(t.rows) {
		return nil
	}
	return t.rows[t.cur.idx]
}

func (t *sessTab) rebuild(m *Model) {
	var keep string
	if s := t.sel(); s != nil {
		keep = s.ID
	}
	var base []*claude.Session
	for _, s := range m.sessions {
		if t.hideBots && s.Bot() && s.Live == nil {
			continue
		}
		if t.onlyWT && s.Worktree == "" {
			continue
		}
		if t.onlyLive && s.Live == nil {
			continue
		}
		if hidden(m.cfg.HidePaths, s.Cwd) || !m.inScope(s.Cwd) {
			continue
		}
		if t.deep != nil {
			if _, ok := t.deep[s.ID]; !ok {
				continue
			}
		}
		base = append(base, s)
	}
	if m.cfg.MaxSessions > 0 && len(base) > m.cfg.MaxSessions {
		base = base[:m.cfg.MaxSessions]
	}
	t.total = len(base)
	q := strings.TrimSpace(t.filter.Value())
	if q == "" {
		t.rows = base
	} else {
		t.rows = claude.Match(base, q)
	}
	for i, s := range t.rows {
		if s.ID == keep {
			t.cur.idx = i
		}
	}
	t.cur.clamp(len(t.rows), 1<<30)
}

// highlight marks case-insensitive occurrences of q in plain text l.
func highlight(s Styles, l, q string) string {
	lq := strings.ToLower(q)
	ll := strings.ToLower(l)
	if q == "" || len(ll) != len(l) {
		return s.Text.Render(l)
	}
	var b strings.Builder
	for {
		i := strings.Index(ll, lq)
		if i < 0 {
			b.WriteString(s.Text.Render(l))
			return b.String()
		}
		b.WriteString(s.Text.Render(l[:i]) + s.BadgeColor(s.P.Yellow).Padding(0).Render(l[i:i+len(q)]))
		l, ll = l[i+len(q):], ll[i+len(q):]
	}
}

func hidden(pats []string, cwd string) bool {
	for _, p := range pats {
		if p != "" && strings.Contains(cwd, p) {
			return true
		}
	}
	return false
}

func (t *sessTab) filterKey(m *Model, k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		t.filter.Blur()
		t.filter.SetValue("")
		t.rebuild(m)
		return *m, m.refreshDetail()
	case "ctrl+k":
		t.filter.Blur()
		return *m, m.openPalette("")
	case "enter", "down", "up", "ctrl+j", "ctrl+n", "ctrl+p":
		switch k.String() {
		case "enter":
			t.filter.Blur()
		case "down", "ctrl+j", "ctrl+n":
			t.cur.move(1, len(t.rows), m.bodyHeight())
		case "up", "ctrl+p":
			t.cur.move(-1, len(t.rows), m.bodyHeight())
		}
		return *m, m.refreshDetail()
	case "ctrl+c":
		t.filter.Blur()
		return m.confirmQuit(false)
	}
	var cmd tea.Cmd
	t.filter, cmd = t.filter.Update(k)
	t.cur.idx = 0
	t.rebuild(m)
	return *m, tea.Batch(cmd, m.refreshDetail())
}

func (t *sessTab) key(m *Model, k tea.KeyPressMsg) tea.Cmd {
	n := len(t.rows)
	h := m.bodyHeight() / 2
	switch k.String() {
	case "/":
		return t.filter.Focus()
	case "j", "down":
		t.cur.move(1, n, h)
	case "k", "up":
		t.cur.move(-1, n, h)
	case "pgdown", "ctrl+d":
		t.cur.move(h, n, h)
	case "pgup", "ctrl+u":
		t.cur.move(-h, n, h)
	case "g", "home":
		t.cur.idx = 0
	case "G", "end":
		t.cur.idx = n - 1
	case "b":
		t.hideBots = !t.hideBots
		t.rebuild(m)
		m.setStatus(map[bool]string{true: T("hiding bot/headless sessions"), false: T("showing bot/headless sessions")}[t.hideBots], false)
	case "w":
		t.onlyWT = !t.onlyWT
		t.rebuild(m)
		m.setStatus(map[bool]string{true: T("only sessions in worktrees"), false: T("sessions in every folder")}[t.onlyWT], false)
	case "L":
		t.onlyLive = !t.onlyLive
		t.rebuild(m)
		m.setStatus(map[bool]string{true: T("only live sessions"), false: T("live and closed sessions")}[t.onlyLive], false)
	case "enter", "r":
		return m.resume(t.sel(), false)
	case "v", "V":
		if s := t.sel(); s != nil {
			return m.openViewer(s.Path, false, s.Title, s, nil)
		}
	case "f":
		return m.resume(t.sel(), true)
	case "o":
		if s := t.sel(); s != nil {
			return m.shell(s.ResumeCwd())
		}
	case "y":
		if s := t.sel(); s != nil {
			return m.copy("cd "+shellQuote(s.ResumeCwd())+" && "+m.resumeCmd(s, false), T("resume command"))
		}
	case "c":
		if s := t.sel(); s != nil {
			return m.copy(s.Cwd, T("folder"))
		}
	case "i":
		if s := t.sel(); s != nil {
			return m.copy(s.ID, T("session id"))
		}
	case "F", "ctrl+f":
		m.prompt(T("Search inside every conversation"), T("Case-insensitive full-text search over all transcripts (your prompts and Claude's answers)."),
			T("e.g. liquibase rollback"), func(m *Model, v string) tea.Cmd {
				if v == "" {
					return nil
				}
				m.sess.deepBusy = true
				m.sess.deepQ = v
				all := m.sessions
				return func() tea.Msg { return deepMsg{v, claude.Search(all, v, 3)} }
			})
	case "esc":
		if t.filter.Value() != "" {
			t.filter.SetValue("")
			t.rebuild(m)
		} else if t.deep != nil {
			t.deep, t.deepQ = nil, ""
			t.rebuild(m)
		}
	}
	t.cur.clamp(len(t.rows), h)
	return nil
}

func (t *sessTab) view(m *Model, w, h int) string {
	s := m.st
	inner := w - 4
	// filter line
	t.filter.SetWidth(inner - 24)
	fl := t.filter.View()
	if !t.filter.Focused() && t.filter.Value() == "" {
		fl = s.Muted.Render("  " + T("/ to filter"))
	}
	var flags []string
	if t.hideBots {
		flags = append(flags, T("no bots"))
	}
	if t.onlyWT {
		flags = append(flags, T("worktrees"))
	}
	if t.onlyLive {
		flags = append(flags, T("live"))
	}
	if t.deepBusy {
		flags = append(flags, spinGlyph+" "+Tf("searching “%s”", t.deepQ))
	} else if t.deep != nil {
		flags = append(flags, Tf("text “%s”: %d · esc", t.deepQ, len(t.deep)))
	}
	if len(flags) > 0 {
		f := s.Muted.Render("[" + strings.Join(flags, " · ") + "]")
		fl = fit(fl, inner-lipglossW(f)) + f
	}
	head := []string{fl, s.Dim.Render(strings.Repeat("─", inner))}

	blocks := make([][]string, len(t.rows))
	for i, ses := range t.rows {
		lines := m.sessionLines(ses, inner)
		if i == t.cur.idx {
			st := s.RowSel
			if m.focusR {
				st = s.RowSelBlur
			}
			for j := range lines {
				lines[j] = s.Sel(st, fit(lines[j], inner))
			}
		}
		blocks[i] = lines
	}
	list := m.listBlocks(blocks, t.cur.idx, "sess", bodyRows(h)-len(head), bodyY+1+gapOf(h)+len(head))
	if len(t.rows) == 0 {
		list = t.empty(m, inner)
	}
	title := T("Sessions") + fmt.Sprintf(" · %d", len(t.rows))
	if len(t.rows) != t.total {
		title += fmt.Sprintf("/%d", t.total)
	}
	if note := m.scopeNote(); note != "" {
		title += note
	}
	return s.panel(title, strings.Join(head, "\n")+"\n"+list, w, h, !m.focusR)
}

// empty explains why the list is empty and how to get rows back.
func (t *sessTab) empty(m *Model, w int) string {
	s := m.st
	q := strings.TrimSpace(t.filter.Value())
	switch {
	case m.loading["sessions"]:
		return s.emptyState(spinGlyph+" "+T("scanning ~/.claude/projects…"), nil, w)
	case t.deepBusy:
		return s.emptyState(spinGlyph+" "+T("searching every conversation…"), nil, w)
	case q != "":
		return s.emptyState(Tf("Nothing matches “%s”.", q), [][2]string{{"esc", T("clear the filter")}, {"F", T("search inside conversations instead")}}, w)
	case t.deep != nil:
		return s.emptyState(Tf("No conversation mentions “%s”.", t.deepQ), [][2]string{{"esc", T("clear the search")}}, w)
	case t.onlyWT || t.onlyLive:
		return s.emptyState(T("No sessions with these filters."), [][2]string{{"w", T("show sessions outside worktrees")}, {"L", T("show closed sessions")}}, w)
	case m.scope != "" || m.spaceScope() != nil:
		return s.emptyState(T("No sessions in this project yet."), [][2]string{{"\\", T("show every project")}}, w)
	}
	return s.emptyState(T("No Claude Code sessions yet. Run claude anywhere and its sessions appear here, ready to resume."),
		[][2]string{{"N", T("new space from a folder")}, {"D", T("discover the places you work in")}}, w)
}

func (m Model) sessionLines(ses *claude.Session, w int) []string {
	s := m.st
	dot := s.Dim.Render("·")
	if ses.Live != nil {
		if ses.Live.Status == "busy" {
			dot = m.stateGlyph("working")
		} else {
			dot = m.stateGlyph("idle")
		}
	} else if ses.Bot() {
		dot = s.Dim.Render("⚙")
	}
	title := ses.Title
	if title == "" {
		title = s.Muted.Render(T("(empty session)"))
	}
	age := s.Muted.Render(ago(ses.Modified))
	l1 := dot + " " + s.Bold.Render(trunc(title, w-10))
	l1 = fit(l1, w-lipglossW(age)) + age

	meta := []string{s.Muted.Render(project(ses.Cwd))}
	if ses.Worktree != "" {
		meta = append(meta, s.Muted.Render("⎇ "+ses.Worktree))
	} else if ses.Branch != "" {
		meta = append(meta, s.Muted.Render("⎇ "+ses.Branch))
	}
	if ses.Kind == "bg" {
		meta = append(meta, s.Muted.Render("bg"))
	}
	right := ""
	if ses.Subagents > 0 {
		right += s.Muted.Render(fmt.Sprintf("↳%d ", ses.Subagents))
	}
	if len(ses.PRs) > 0 {
		right += s.Green.Render(fmt.Sprintf("PR%d ", len(ses.PRs)))
	}
	right += s.Muted.Render(humanBytes(ses.Size))
	l2 := "  " + strings.Join(meta, s.Dim.Render(" · "))
	l2 = fit(l2, w-lipglossW(right)) + right
	return []string{l1, l2}
}

// sessionHeader is the metadata block shared by Live and Sessions details.
func (m Model) sessionHeader(ses *claude.Session, w int) []string {
	s := m.st
	var ls []string
	for _, l := range wrap(ses.Title, w, 3) {
		ls = append(ls, s.Bold.Render(l))
	}
	status := s.Muted.Render(T("closed"))
	if ses.Live != nil {
		status = m.stateLabel(map[bool]string{true: "working", false: "idle"}[ses.Live.Status == "busy"]) +
			s.Muted.Render("  "+Tf("pid %d · up %s", ses.Live.Pid, ago(ses.Live.Started())))
	}
	ls = append(ls, status, "")
	ls = append(ls, s.kv(T("folder"), tilde(ses.Cwd), w))
	if _, err := os.Stat(ses.Cwd); err != nil && ses.Cwd != "" {
		ls = append(ls, s.kv("", s.Red.Render(T("folder no longer exists")), w))
	}
	if ses.Worktree != "" {
		ls = append(ls, s.kv(T("worktree"), s.Text.Render(ses.Worktree), w))
	}
	if ses.Branch != "" {
		br := s.Text.Render(ses.Branch)
		if c, ok := m.stateOf(ses.Cwd); ok {
			br += "  " + m.branchBits(c)
		}
		ls = append(ls, s.kv(T("branch"), br, w))
	}
	if p, ok := m.prOf(ses.Cwd); ok {
		ls = append(ls, s.kv("PR", m.prLine(p), w), s.kv("", s.Muted.Render(trunc(p.Title, w-12)), w))
	}
	ls = append(ls, s.kv(T("session"), ses.ID, w))
	ls = append(ls, s.kv(T("updated"), ses.Modified.Format("Mon 02 Jan 15:04")+"  ("+Tf("%s ago", ago(ses.Modified))+")", w))
	info := []string{}
	if ses.Model != "" {
		info = append(info, shortModel(ses.Model))
	}
	if ses.Messages > 0 {
		info = append(info, Tn(ses.Messages, "%d msg", "%d msgs"))
	}
	info = append(info, humanBytes(ses.Size))
	if ses.Cost > 0 {
		info = append(info, fmt.Sprintf("$%.2f", ses.Cost))
	}
	if ses.LinesAdd+ses.LinesDel > 0 {
		info = append(info, s.Green.Render(fmt.Sprintf("+%d", ses.LinesAdd))+" "+s.Red.Render(fmt.Sprintf("-%d", ses.LinesDel)))
	}
	ls = append(ls, s.kv(T("stats"), strings.Join(info, s.Dim.Render(" · ")), w+40))
	if ses.Kind != "" || ses.Entrypoint != "" {
		ls = append(ls, s.kv(T("kind"), strings.TrimSpace(ses.Kind+" "+ses.Entrypoint+" v"+ses.Version), w))
	}
	for _, pr := range ses.PRs {
		ls = append(ls, s.kv("PR", s.Green.Render(pr), w))
	}
	return ls
}

func (t *sessTab) detail(m *Model, w, h int) string {
	s := m.st
	ses := t.sel()
	inner := w - 4
	if ses == nil {
		return s.panel(T("Session"), "", w, h, m.focusR)
	}
	ls := m.sessionHeader(ses, inner)
	ls = append(ls, m.usageLines(m.usageOf(ses), inner)...)
	if hits := t.deep[ses.ID]; len(hits) > 0 {
		ls = append(ls, "", s.Accent.Render(Tf("Matches for “%s”", t.deepQ)))
		for _, h := range hits {
			p := s.Accent.Render("❯ ")
			if h.Role == "assistant" {
				p = s.Teal.Render("● ")
			}
			for i, l := range wrap(h.Snippet, inner-2, 3) {
				if i > 0 {
					p = "  "
				}
				ls = append(ls, p+highlight(s, l, t.deepQ))
			}
		}
	}
	if ses.FirstPrompt != "" && ses.FirstPrompt != ses.Title {
		ls = append(ls, "", s.Accent.Render(T("First prompt")))
		for _, l := range wrap(ses.FirstPrompt, inner, 5) {
			ls = append(ls, s.Text.Render(l))
		}
	}
	if subs := m.subs[ses.Path]; len(subs) > 0 {
		flat := claude.Flatten(subs)
		ls = append(ls, "", s.Accent.Render(T("Subagents")+fmt.Sprintf(" · %d", len(flat))))
		for i, f := range flat {
			if i >= 12 {
				ls = append(ls, s.Muted.Render("  "+Tf("… %d more", len(flat)-12)))
				break
			}
			st := "finished"
			if f.Running {
				st = "running"
			}
			ls = append(ls, strings.Repeat("  ", f.Level)+m.stateGlyph(st)+" "+s.Muted.Render(f.Type)+" "+s.Text.Render(trunc(f.Description, inner-len(f.Type)-6)))
		}
	}
	ls = append(ls, "", s.Accent.Render(T("Conversation tail")))
	headN := len(ls)
	ls = append(ls, m.entryLines(m.transcripts[ses.Path], inner)...)
	return s.panel(T("Session"), m.detailTail(ls, headN, h), w, h, m.focusR)
}
