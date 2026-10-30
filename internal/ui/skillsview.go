package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/secuone/nanny/internal/skills"
)

type skillsMsg []skills.Item

type skillsTab struct {
	cur     cursor
	all     []skills.Item
	rows    []int // indexes into all
	filter  textinput.Model
	kind    skills.Kind // "" = all
	hideOff bool
	loaded  bool
	preview map[string]string
}

func (m Model) skillsCmd() tea.Cmd {
	var projects []string
	seen := map[string]bool{}
	for _, sp := range m.spaces {
		for _, p := range sp.Paths {
			if !seen[p] {
				seen[p] = true
				projects = append(projects, p)
			}
		}
	}
	for _, r := range m.repos {
		if !seen[r.Root] {
			seen[r.Root] = true
			projects = append(projects, r.Root)
		}
	}
	h := home
	return func() tea.Msg { return skillsMsg(skills.Scan(h, projects)) }
}

func (t *skillsTab) rebuild(m *Model) {
	keep := ""
	if it := t.sel(); it != nil {
		keep = it.Path
	}
	q := strings.ToLower(strings.TrimSpace(t.filter.Value()))
	t.rows = t.rows[:0]
	for i, it := range t.all {
		if t.kind == harnessKind && !it.Kind.Harness() || t.kind != "" && t.kind != harnessKind && it.Kind != t.kind {
			continue
		}
		if t.hideOff && it.Disabled {
			continue
		}
		if m.spaceScope() != nil && it.Scope == "project" && !m.spaceScope().Covers(itemProject(it)) {
			continue
		}
		if q != "" {
			hay := strings.ToLower(it.Name + " " + it.Description + " " + it.Owner + " " + string(it.Kind) + " " + it.Scope)
			ok := true
			for _, tok := range strings.Fields(q) {
				if !strings.Contains(hay, tok) {
					ok = false
					break
				}
			}
			if !ok {
				continue
			}
		}
		t.rows = append(t.rows, i)
	}
	for i, ri := range t.rows {
		if t.all[ri].Path == keep {
			t.cur.idx = i
		}
	}
	t.cur.clamp(len(t.rows), 1<<30)
}

func (t *skillsTab) sel() *skills.Item {
	if t.cur.idx < 0 || t.cur.idx >= len(t.rows) {
		return nil
	}
	return &t.all[t.rows[t.cur.idx]]
}

var skillNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}$`)

func (t *skillsTab) filterKey(m *Model, k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		t.filter.Blur()
		t.filter.SetValue("")
		t.rebuild(m)
		return *m, nil
	case "enter":
		t.filter.Blur()
		return *m, nil
	case "down", "ctrl+n":
		t.cur.move(1, len(t.rows), m.bodyHeight()/2)
		return *m, nil
	case "up", "ctrl+p":
		t.cur.move(-1, len(t.rows), m.bodyHeight()/2)
		return *m, nil
	case "ctrl+c":
		t.filter.Blur()
		return m.confirmQuit(false)
	}
	var cmd tea.Cmd
	t.filter, cmd = t.filter.Update(k)
	t.cur.idx = 0
	t.rebuild(m)
	return *m, cmd
}

func (t *skillsTab) key(m *Model, k tea.KeyPressMsg) tea.Cmd {
	n := len(t.rows)
	h := m.bodyHeight() / 2
	it := t.sel()
	switch k.String() {
	case "/":
		return t.filter.Focus()
	case "j", "down":
		t.cur.move(1, n, h)
	case "k", "up":
		t.cur.move(-1, n, h)
	case "g", "home":
		t.cur.idx = 0
	case "G", "end":
		t.cur.idx = n - 1
	case "pgdown", "ctrl+d":
		t.cur.move(h, n, h)
	case "pgup", "ctrl+u":
		t.cur.move(-h, n, h)
	case "J": // next / previous group (skills · commands · agents)
		for i := t.cur.idx + 1; it != nil && i < n; i++ {
			if t.all[t.rows[i]].Kind != it.Kind {
				t.cur.idx = i
				break
			}
		}
	case "K":
		for i := t.cur.idx - 1; it != nil && i >= 0; i-- {
			if t.all[t.rows[i]].Kind != it.Kind {
				for i > 0 && t.all[t.rows[i-1]].Kind == t.all[t.rows[i]].Kind {
					i--
				}
				t.cur.idx = i
				break
			}
		}
	case "t": // cycle the type filter
		t.kind = map[skills.Kind]skills.Kind{"": skills.Skill, skills.Skill: skills.Command, skills.Command: skills.Agent,
			skills.Agent: harnessKind, harnessKind: ""}[t.kind]
		t.cur.idx = 0
		t.rebuild(m)
		if t.kind == "" {
			m.setStatus(T("showing everything"), false)
		} else {
			m.setStatus(Tf("only %s", kindName(t.kind, true)), false)
		}
	case "H":
		t.hideOff = !t.hideOff
		t.rebuild(m)
		m.setStatus(map[bool]string{true: T("hiding disabled items"), false: T("showing disabled items")}[t.hideOff], false)
	case "enter", "e":
		if it != nil {
			return m.run(filepath.Dir(it.Path), T("edit")+" "+it.Name, "${EDITOR:-nano} "+shellQuote(it.Path))
		}
	case "o":
		if it != nil {
			dir := it.Root
			if st, err := os.Stat(dir); err == nil && !st.IsDir() {
				dir = filepath.Dir(dir)
			}
			return m.shell(dir)
		}
	case " ", "space":
		if it == nil {
			return nil
		}
		title, done := T("Disable %s “%s”?"), T("disabled %s")
		if it.Disabled {
			title, done = T("Enable %s “%s”?"), T("enabled %s")
		}
		if it.Kind == skills.Plugin {
			m.setStatus(Tf("turn plugins on and off with `claude plugin enable/disable %s`", it.Name), true)
			return nil
		}
		if it.Kind.Harness() {
			m.setStatus(T("⏎ opens the file it is set in"), false)
			return nil
		}
		if !it.Editable() {
			m.setStatus(Tf("plugin items follow the plugin: use `claude plugin disable %s`", it.Owner), true)
			return nil
		}
		cur := *it
		m.ask(fmt.Sprintf(title, kindName(cur.Kind, false), cur.Name),
			Tf("Moves it between %ss/ and %ss-disabled/ so Claude stops (or starts) loading it. New sessions pick up the change.", cur.Kind, cur.Kind),
			func(m *Model, _ string) tea.Cmd {
				nu, err := skills.Toggle(cur)
				if err != nil {
					m.setStatus(err.Error(), true)
					return nil
				}
				for i := range m.skills.all {
					if m.skills.all[i].Path == cur.Path {
						m.skills.all[i] = nu
					}
				}
				m.skills.rebuild(m)
				m.setStatus(fmt.Sprintf(done, nu.Name), false)
				return nil
			})
	case "n":
		m.newItemMenu()
	case "c":
		if it == nil {
			return nil
		}
		cur := *it
		def := home
		if sp := m.spaceScope(); sp != nil {
			def = sp.Dir()
		} else if len(m.spaces) > 0 {
			def = m.spaces[0].Dir()
		}
		m.prompt(Tf("Copy “%s” to a project", cur.Name), T("Copies it into <project>/.claude so everyone working on that repo gets it."), tilde(def),
			func(m *Model, v string) tea.Cmd {
				if v == "" {
					v = tilde(def)
				}
				dst, err := skills.CopyTo(cur, filepath.Join(expandPath(v), ".claude"))
				if err != nil {
					m.setStatus(err.Error(), true)
					return nil
				}
				m.setStatus(T("copied")+" → "+tilde(dst), false)
				return m.skillsCmd()
			})
	case "y":
		if it != nil && it.Kind.Harness() {
			return m.copy(it.Path, T("path"))
		}
		if it != nil {
			return m.copy(it.Invocation(), T("invocation"))
		}
	case "p":
		if it != nil {
			return m.copy(it.Path, T("path"))
		}
	case "esc":
		if t.filter.Value() != "" {
			t.filter.SetValue("")
			t.rebuild(m)
		}
	}
	t.cur.clamp(len(t.rows), h)
	return nil
}

func kindBadge(s Styles, k skills.Kind) string {
	switch k {
	case skills.Skill:
		return s.Pink.Render("◆")
	case skills.Command:
		return s.Blue.Render("/")
	case skills.Hook:
		return s.Yellow.Render("⚓")
	case skills.MCP:
		return s.Green.Render("⇄")
	case skills.Plugin:
		return s.Peach.Render("⧉")
	case skills.Memory:
		return s.Accent.Render("¶")
	case skills.Settings:
		return s.Muted.Render("⚙")
	}
	return s.Teal.Render("@")
}

// kindTitle is a kind as a heading: "Skills", "MCP servers"…
func kindTitle(k skills.Kind) string {
	return map[skills.Kind]string{skills.Skill: T("Skills"), skills.Command: T("Commands"), skills.Agent: T("Agents"),
		skills.Hook: T("Hooks"), skills.MCP: T("MCP servers"), skills.Plugin: T("Plugins"), skills.Memory: T("Memory (CLAUDE.md)"),
		skills.Settings: T("Settings files")}[k]
}

// kindName is a kind as shown: "skill", or "skills" when many.
func kindName(k skills.Kind, many bool) string {
	switch {
	case k == skills.Skill && many:
		return T("skills")
	case k == skills.Skill:
		return T("skill")
	case k == skills.Command && many:
		return T("commands")
	case k == skills.Command:
		return T("command")
	case k == skills.Agent && many:
		return T("agents")
	case k == skills.Agent:
		return T("agent")
	case k == harnessKind:
		return T("harness")
	case many:
		return strings.ToLower(kindTitle(k))
	}
	return map[skills.Kind]string{skills.Hook: T("hook"), skills.MCP: T("MCP server"), skills.Plugin: T("plugin"),
		skills.Memory: "CLAUDE.md", skills.Settings: T("settings file")}[k]
}

// harnessKind is the type filter that shows hooks, MCP servers, plugins,
// memory and settings together.
const harnessKind skills.Kind = "harness"

func (t *skillsTab) view(m *Model, w, h int) string {
	s := m.st
	inner := w - 4
	t.filter.SetWidth(inner - 24)
	fl := t.filter.View()
	if !t.filter.Focused() && t.filter.Value() == "" {
		fl = s.Muted.Render("  " + T("/ to filter"))
	}
	flags := []string{}
	if t.kind != "" {
		flags = append(flags, kindName(t.kind, true))
	}
	if t.hideOff {
		flags = append(flags, T("enabled"))
	}
	if len(flags) > 0 {
		f := s.Muted.Render("[" + strings.Join(flags, " · ") + "]")
		fl = fit(fl, inner-lipglossW(f)) + f
	}
	head := []string{fl, s.Dim.Render(strings.Repeat("─", inner))}
	if !t.loaded {
		return s.panel(T("Skills"), strings.Join(head, "\n")+"\n"+s.Muted.Render("  "+spinGlyph+" "+T("reading skills, commands & agents…")), w, h, !m.focusR)
	}
	blocks := make([][]string, len(t.rows))
	var last skills.Kind = "-"
	for i, ri := range t.rows {
		it := t.all[ri]
		var lines []string
		if it.Kind != last && (t.kind == "" || t.kind == harnessKind) {
			if i > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, s.Accent.Render("▍"+kindTitle(it.Kind)))
			last = it.Kind
		}
		name := s.Bold.Render(it.Name)
		if it.Disabled {
			name = s.Muted.Render(it.Name + " (" + T("off") + ")")
		}
		scope := s.Muted.Render(it.Scope)
		switch it.Scope {
		case "project":
			scope = s.Peach.Render(it.Owner)
		case "plugin":
			scope = s.Teal.Render(strings.SplitN(it.Owner, "@", 2)[0])
		}
		uses := ""
		if it.Uses > 0 {
			uses = s.Yellow.Render(fmt.Sprintf("%d×", it.Uses)) + " "
		}
		right := uses + scope
		l1 := " " + kindBadge(s, it.Kind) + " " + name
		l1 = fit(l1, inner-lipglossW(right)) + right
		l2 := "   " + s.Muted.Render(trunc(it.Description, inner-4))
		row := []string{l1, l2}
		if i == t.cur.idx {
			st := s.RowSel
			if m.focusR {
				st = s.RowSelBlur
			}
			for j := range row {
				row[j] = s.Sel(st, fit(row[j], inner))
			}
		}
		blocks[i] = append(lines, row...)
	}
	list := m.listBlocks(blocks, t.cur.idx, "skills", h-2-len(head), bodyY+1+len(head))
	if len(t.rows) == 0 {
		switch {
		case len(t.all) == 0:
			list = s.emptyState(T("No skills, commands or agents yet. They live in ~/.claude and in each project's .claude folder."),
				[][2]string{{"n", T("new skill from a template")}}, inner)
		case strings.TrimSpace(t.filter.Value()) != "":
			list = s.emptyState(Tf("Nothing matches “%s”.", strings.TrimSpace(t.filter.Value())), [][2]string{{"esc", T("clear the filter")}}, inner)
		default:
			list = s.emptyState(T("Nothing with these filters."), [][2]string{{"t", T("change the type")}, {"H", T("show disabled items")}}, inner)
		}
	}
	counts := map[skills.Kind]int{}
	for _, it := range t.all {
		counts[it.Kind]++
	}
	// only what there is: "◆3 /2 · harness ⚓4 ⇄8"
	part := func(ks []skills.Kind, marks []string) string {
		var ps []string
		for i, k := range ks {
			if counts[k] > 0 {
				ps = append(ps, fmt.Sprintf("%s%d", marks[i], counts[k]))
			}
		}
		return strings.Join(ps, " ")
	}
	title := T("Skills")
	if p := part([]skills.Kind{skills.Skill, skills.Command, skills.Agent}, []string{"◆", "/", "@"}); p != "" {
		title += " · " + p
	}
	if p := part([]skills.Kind{skills.Hook, skills.MCP, skills.Plugin}, []string{"⚓", "⇄", "⧉"}); p != "" {
		title += " · " + T("harness") + " " + p
	}
	return s.panel(title, strings.Join(head, "\n")+"\n"+list, w, h, !m.focusR)
}

func (t *skillsTab) detail(m *Model, w, h int) string {
	s := m.st
	it := t.sel()
	inner := w - 4
	if it == nil {
		return s.panel(T("Skill"), "", w, h, m.focusR)
	}
	var ls []string
	if it.Kind.Harness() {
		return t.harnessDetail(m, *it, w, h)
	}
	ls = append(ls, kindBadge(s, it.Kind)+" "+s.Bold.Render(it.Name)+"  "+s.Pink.Render(it.Invocation()))
	status := s.Green.Render(T("enabled"))
	if it.Disabled {
		status = s.Red.Render(T("disabled"))
	}
	ls = append(ls, status+s.Muted.Render("  · "+kindName(it.Kind, false)+" · "+it.Scope+" "+it.Owner), "")
	for _, l := range wrap(it.Description, inner, 6) {
		ls = append(ls, s.Text.Render(l))
	}
	ls = append(ls, "", s.kv(T("file"), tilde(it.Path), inner))
	ls = append(ls, s.kv(T("modified"), it.Modified.Format("2006-01-02 15:04")+"  ("+Tf("%s ago", ago(it.Modified))+")", inner))
	if it.Uses > 0 {
		ls = append(ls, s.kv(T("used"), Tn(it.Uses, "%d time · last %s ago", "%d times · last %s ago", ago(it.LastUsed)), inner))
	}
	if it.Kind == skills.Skill {
		if ents, err := os.ReadDir(it.Root); err == nil && len(ents) > 1 {
			var names []string
			for _, e := range ents {
				if e.Name() != "SKILL.md" {
					n := e.Name()
					if e.IsDir() {
						n += "/"
					}
					names = append(names, n)
				}
			}
			ls = append(ls, s.kv(T("bundles"), strings.Join(names, " "), inner))
		}
	}
	acts := [][2]string{{"⏎", T("edit")}, {"o", T("shell")}, {"y", T("copy") + " " + it.Invocation()}}
	if it.Editable() {
		acts = append(acts, [2]string{"space", map[bool]string{true: T("enable"), false: T("disable")}[it.Disabled]}, [2]string{"c", T("copy to project")})
	}
	ls = append(ls, "", s.hintsFit(acts, inner), "", s.Accent.Render(filepath.Base(it.Path)))
	if b, err := os.ReadFile(it.Path); err == nil {
		body := string(b)
		if len(body) > 16000 {
			body = body[:16000]
		}
		for _, l := range wrap(body, inner, 400) {
			st := s.Text
			switch {
			case strings.HasPrefix(l, "#"):
				st = s.Accent
			case strings.HasPrefix(l, "---") || strings.HasPrefix(l, "name:") || strings.HasPrefix(l, "description:"):
				st = s.Muted
			case strings.HasPrefix(strings.TrimSpace(l), "- ") || strings.HasPrefix(strings.TrimSpace(l), "* "):
				st = s.Text
			case strings.HasPrefix(l, "```"):
				st = s.Yellow
			}
			ls = append(ls, st.Render(l))
		}
	}
	return s.panel(map[skills.Kind]string{skills.Skill: T("Skill"), skills.Command: T("Command"), skills.Agent: T("Agent")}[it.Kind], m.detailScroll(ls, h), w, h, m.focusR)
}

// newItemMenu asks what to create (skill, command, subagent), then where
// (your user folder, one of your spaces' repos, another folder), then its name.
func (m *Model) newItemMenu() {
	x, y := m.sideW()+4, bodyY+2
	var kinds []menuItem
	for _, k := range []struct {
		key  string
		kind skills.Kind
	}{{"s", skills.Skill}, {"c", skills.Command}, {"a", skills.Agent}} {
		k := k
		label := map[skills.Kind]string{skills.Skill: T("Skill"), skills.Command: T("Command"), skills.Agent: T("Agent")}[k.kind]
		kinds = append(kinds, menuItem{label, k.key, func(m *Model) tea.Cmd {
			m.newItemWhere(k.kind, x, y)
			return nil
		}})
	}
	m.openMenu(T("New…"), x, y, kinds)
}

func (m *Model) newItemWhere(k skills.Kind, x, y int) {
	pick := func(base string) func(m *Model) tea.Cmd {
		return func(m *Model) tea.Cmd { m.newItemName(k, base); return nil }
	}
	items := []menuItem{{T("Global: every project") + "  ~/.claude", "g", pick(filepath.Join(home, ".claude"))}, sep}
	seen := map[string]bool{}
	n := 0
	for _, sp := range m.spaces {
		for _, d := range sp.Paths {
			if seen[d] {
				continue
			}
			seen[d] = true
			key := ""
			if n < 9 {
				key = fmt.Sprint(n + 1)
			}
			n++
			items = append(items, menuItem{spaceLabel(sp) + "  " + tilde(d), key, pick(filepath.Join(d, ".claude"))})
		}
	}
	items = append(items, sep, menuItem{T("Another folder…"), "o", func(m *Model) tea.Cmd {
		m.prompt(T("Which folder?"), T("The repo or folder it belongs to; it goes into its .claude folder."), tilde(home),
			func(m *Model, v string) tea.Cmd {
				if v == "" {
					return nil
				}
				m.newItemName(k, filepath.Join(expandPath(v), ".claude"))
				return nil
			})
		return nil
	}})
	m.openMenu(Tf("Where does the %s go?", kindName(k, false)), x, y, items)
}

func (m *Model) newItemName(k skills.Kind, base string) {
	m.prompt(Tf("New %s", kindName(k, false)), Tf("Created in %s and opened in your editor.", tilde(base)), "my-"+string(k),
		func(m *Model, v string) tea.Cmd {
			if !skillNameRe.MatchString(v) {
				m.setStatus(T("use lowercase letters, digits and dashes"), true)
				return nil
			}
			p, err := skills.NewItem(base, k, v)
			if err != nil {
				m.setStatus(err.Error(), true)
				return nil
			}
			return tea.Batch(m.skillsCmd(), m.run(filepath.Dir(p), T("edit")+" "+v, "${EDITOR:-nano} "+shellQuote(p)))
		})
}

// harnessDetail shows what a hook, MCP server, plugin or settings file sets
// (names of env vars and headers only, never their values), and CLAUDE.md
// files in full.
func (t *skillsTab) harnessDetail(m *Model, it skills.Item, w, h int) string {
	s := m.st
	inner := w - 4
	ls := []string{kindBadge(s, it.Kind) + " " + s.Bold.Render(it.Name)}
	where := T("global")
	if it.Scope == "project" {
		where = it.Owner
	}
	status := s.Green.Render(T("enabled"))
	if it.Disabled {
		status = s.Red.Render(T("disabled"))
	}
	ls = append(ls, status+s.Muted.Render("  · "+kindName(it.Kind, false)+" · "+where), "")
	for _, d := range it.Detail {
		k, v, ok := strings.Cut(d, ": ")
		if !ok {
			ls = append(ls, s.Text.Render(d))
			continue
		}
		for i, l := range wrap(v, inner-14, 6) {
			if i > 0 {
				k = ""
			}
			ls = append(ls, s.Label.Render(fit(k, 14))+s.Text.Render(l))
		}
	}
	if len(it.Detail) == 0 {
		ls = append(ls, s.Text.Render(it.Description))
	}
	ls = append(ls, "", s.kv(T("file"), tilde(it.Path), inner))
	if !it.Modified.IsZero() {
		ls = append(ls, s.kv(T("modified"), Tf("%s ago", ago(it.Modified)), inner))
	}
	ls = append(ls, "", s.hintsFit([][2]string{{"⏎", T("edit the file")}, {"o", T("shell")}, {"y", T("copy path")}}, inner))
	if it.Kind == skills.Memory {
		if b, err := os.ReadFile(it.Path); err == nil {
			ls = append(ls, "")
			body := string(b)
			if len(body) > 16000 {
				body = body[:16000]
			}
			for _, l := range wrap(body, inner, 400) {
				st := s.Text
				if strings.HasPrefix(l, "#") {
					st = s.Accent
				}
				ls = append(ls, st.Render(l))
			}
		}
	}
	return s.panel(kindTitle(it.Kind), m.detailScroll(ls, h), w, h, m.focusR)
}

// itemProject is the project folder a project-scoped item belongs to.
func itemProject(it skills.Item) string {
	if i := strings.Index(it.Path, "/.claude/"); i >= 0 {
		return it.Path[:i]
	}
	return filepath.Dir(it.Path) // CLAUDE.md, .mcp.json at the repo root
}
