package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/sahilm/fuzzy"
	"github.com/secuone/nanny/internal/config"
)

// palette is a ctrl+k command launcher over actions, live agents, sessions and worktrees.
type palette struct {
	input   textinput.Model
	entries []paletteEntry
	shown   []int
	cur     int
}

type paletteEntry struct {
	kind  string // action | live | session | new | shell
	label string
	hint  string
	run   func(m *Model) tea.Cmd
}

type paletteSrc []paletteEntry

func (p paletteSrc) String(i int) string { return T(p[i].kind) + " " + p[i].label + " " + p[i].hint }
func (p paletteSrc) Len() int            { return len(p) }

func (m *Model) openPalette(prefill string) tea.Cmd {
	in := newInput()
	in.Prompt = "❯ "
	in.Placeholder = T("jump to anything — sessions, agents, worktrees, actions…")
	in.SetValue(prefill)
	in.CursorEnd()
	in.Focus()
	p := &palette{input: in, entries: m.paletteEntries()}
	p.filter()
	m.pal = p
	return textinput.Blink
}

func (p *palette) filter() {
	q := strings.TrimSpace(p.input.Value())
	p.shown = p.shown[:0]
	if q == "" {
		for i := range p.entries {
			p.shown = append(p.shown, i)
		}
	} else {
		// literal token matches keep their natural order (live, actions, checkouts,
		// recent sessions); fuzzy-only matches follow by score
		toks := strings.Fields(strings.ToLower(q))
		seen := map[int]bool{}
		src := paletteSrc(p.entries)
		for i := range p.entries {
			h := strings.ToLower(src.String(i))
			ok := true
			for _, t := range toks {
				if !strings.Contains(h, t) {
					ok = false
					break
				}
			}
			if ok {
				p.shown = append(p.shown, i)
				seen[i] = true
			}
		}
		for _, r := range fuzzy.FindFrom(q, src) {
			if !seen[r.Index] {
				p.shown = append(p.shown, r.Index)
			}
		}
	}
	if p.cur >= len(p.shown) {
		p.cur = max(0, len(p.shown)-1)
	}
}

func (m *Model) paletteEntries() []paletteEntry {
	var es []paletteEntry
	act := func(label, hint string, fn func(m *Model) tea.Cmd) {
		es = append(es, paletteEntry{M("action"), label, hint, fn})
	}
	// live agents first: the most common jump target
	for _, s := range m.sessions {
		if s.Live == nil {
			continue
		}
		s := s
		es = append(es, paletteEntry{M("live"), s.Title, project(s.Cwd) + " " + s.Worktree, func(m *Model) tea.Cmd { return m.resume(s, false) }})
	}
	act(T("Welcome guide: what each view is, first steps"), "", func(m *Model) tea.Cmd { m.welcome = &welcome{}; return nil })
	act(T("Setup wizard…"), "", func(m *Model) tea.Cmd { m.openSetup(setupIntro); return nil })
	act(T("Next agent that needs you"), "!", func(m *Model) tea.Cmd { return m.nextAttention() })
	act(T("New task… (folder, prompt, worktree)"), "n", func(m *Model) tea.Cmd { return m.openNewTask("") })
	act(T("Search inside all conversations…"), "F", func(m *Model) tea.Cmd {
		m.show(tabSessions)
		return m.sess.key(m, keyPress("F"))
	})
	act(T("Customize: what shows and what runs…"), "=", func(m *Model) tea.Cmd { m.openCustomize(); return nil })
	for i, n := range tabNames {
		i := i
		if !m.viewOn(tab(i)) {
			continue
		}
		act(Tf("Go to %s", T(n)), tabKey(tab(i)), func(m *Model) tea.Cmd {
			mm, c := m.switchTab(tab(i))
			*m = mm.(Model)
			return c
		})
	}
	for _, f := range m.feats() {
		es = append(es, f.palette(m)...)
	}
	act(T("Classroom in 3D (browser)"), "9", func(m *Model) tea.Cmd { return m.openClassroom() })
	act(T("Rescan everything"), "R", func(m *Model) tea.Cmd {
		mm, c := m.key(keyPress("R"))
		*m = mm.(Model)
		return c
	})
	act(T("Next screen profile"), "M", func(m *Model) tea.Cmd { return m.cycleProfile(1) })
	for _, n := range m.cfg.ProfileNames() {
		n := n
		act(Tf("Screen profile: %s", profileLabel(n)), "", func(m *Model) tea.Cmd { return m.useProfile(n) })
	}
	act(T("Theme…"), "T", func(m *Model) tea.Cmd { m.openThemes(); return nil })
	act(T("Appearance…"), "", func(m *Model) tea.Cmd { m.openLook(true); return nil })
	act(T("Next theme"), "t", func(m *Model) tea.Cmd { settingsList[0].cycle(&m.cfg, 1); return m.saveConfig() })
	act(T("Edit config file"), "e", func(m *Model) tea.Cmd { return m.editConfig() })
	act(T("Edit snippets…"), "snippets.toml", func(m *Model) tea.Cmd { return m.editSnippets() })
	// saved prompts, pinned then most used first
	for _, sn := range config.LoadSnippets() {
		sn := sn
		label := Tf("Snippet: %s", sn.Name)
		if sn.Uses > 0 {
			label += fmt.Sprintf(" ×%d", sn.Uses)
		}
		es = append(es, paletteEntry{M("snippet"), label, sn.Text, func(m *Model) tea.Cmd { return m.usePrompt(sn.Name, sn.Text) }})
	}
	// new agent / shell in every known checkout
	seen := map[string]bool{}
	for _, r := range m.repos {
		for _, w := range r.Worktrees {
			if w.Missing || seen[w.Path] {
				continue
			}
			seen[w.Path] = true
			p, name := w.Path, r.Name
			if !w.Main {
				name += " ⎇ " + filepath.Base(w.Path)
			}
			es = append(es, paletteEntry{M("new"), Tf("New claude in %s", name), w.Branch, func(m *Model) tea.Cmd {
				return m.run(p, filepath.Base(p), strings.TrimSpace("claude "+m.cfg.ClaudeArgs))
			}})
			es = append(es, paletteEntry{M("shell"), Tf("Shell in %s", name), tilde(p), func(m *Model) tea.Cmd { return m.shell(p) }})
			if w.Dirty > 0 || w.Ahead > 0 {
				es = append(es, paletteEntry{M("review"), Tf("Review changes in %s", name), fmt.Sprintf("✎%d ↑%d", w.Dirty, w.Ahead), func(m *Model) tea.Cmd { return m.openReview(p) }})
			}
		}
	}
	// recent sessions
	n := 0
	for _, s := range m.sessions {
		if s.Live != nil || (s.Bot() && m.cfg.HideBots) || s.Title == "" {
			continue
		}
		if n++; n > 400 {
			break
		}
		s := s
		hint := project(s.Cwd)
		if s.Worktree != "" {
			hint += " ⎇ " + s.Worktree
		} else if s.Branch != "" {
			hint += " ⎇ " + s.Branch
		}
		hint += " · " + ago(s.Modified)
		es = append(es, paletteEntry{M("session"), s.Title, hint, func(m *Model) tea.Cmd { return m.resume(s, false) }})
	}
	return es
}

func (m Model) paletteKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.pal
	switch k.String() {
	case "esc", "ctrl+c", "ctrl+k":
		m.pal = nil
		return m, nil
	case "up", "ctrl+p":
		p.cur = max(0, p.cur-1)
		return m, nil
	case "down", "ctrl+n", "tab":
		p.cur = min(len(p.shown)-1, p.cur+1)
		return m, nil
	case "enter":
		m.pal = nil
		if p.cur < len(p.shown) {
			e := p.entries[p.shown[p.cur]]
			cmd := e.run(&m)
			return m, cmd
		}
		return m, nil
	}
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(k)
	p.cur = 0
	p.filter()
	return m, cmd
}

func (m Model) viewPalette() string {
	s := m.st
	p := m.pal
	w := min(96, m.w-6)
	inner := w - 6
	p.input.Width = inner - 4
	var b strings.Builder
	b.WriteString(p.input.View() + "\n" + s.Dim.Render(strings.Repeat("─", inner)) + "\n")
	rows := min(14, m.h-10)
	start := 0
	if p.cur >= rows {
		start = p.cur - rows + 1
	}
	m.vs.modalY, m.vs.modalStart, m.vs.modalN = 2, start, max(0, min(len(p.shown), start+rows)-start)
	kindStyle := map[string]func(string) string{
		"live":    func(t string) string { return s.Yellow.Render(t) },
		"session": func(t string) string { return s.Blue.Render(t) },
		"new":     func(t string) string { return s.Green.Render(t) },
		"shell":   func(t string) string { return s.Teal.Render(t) },
		"action":  func(t string) string { return s.Pink.Render(t) },
		"review":  func(t string) string { return s.Peach.Render(t) },
		"snippet": func(t string) string { return s.Accent.Render(t) },
	}
	for i := start; i < min(len(p.shown), start+rows); i++ {
		e := p.entries[p.shown[i]]
		tag := kindStyle[e.kind](fit(T(e.kind), 8))
		hint := s.Muted.Render(trunc(e.hint, inner/3))
		label := trunc(e.label, inner-10-lipglossW(hint)-1)
		line := tag + " " + s.Text.Render(label)
		line = fit(line, inner-lipglossW(hint)) + hint
		if i == p.cur {
			line = s.Sel(s.RowSel, fit(line, inner))
		}
		b.WriteString(line + "\n")
	}
	if len(p.shown) == 0 {
		b.WriteString(s.Muted.Render(T("nothing matches")) + "\n")
	}
	count := s.Muted.Render(Tn(len(p.shown), "%d result", "%d results") + "   ")
	b.WriteString(count + s.hintsFit([][2]string{{"↑↓", T("move")}, {"⏎", T("run")}, {"esc", T("close")}}, inner-lipglossW(count)))
	return s.Modal.Width(w).Render(b.String())
}

// nextAttention jumps the Live view to the next agent that is blocked, then done, then working.
func (m *Model) nextAttention() tea.Cmd {
	if !m.toLive() {
		return nil
	}
	n := len(m.live.rows)
	for _, want := range []string{"blocked", "done", "working"} {
		for k := 1; k <= n; k++ {
			i := (m.live.cur.idx + k) % n
			r := m.live.rows[i]
			if r.agent() && r.state() == want {
				m.live.cur.idx = i
				return m.refreshDetail()
			}
		}
	}
	m.setStatus(T("nobody needs you right now ☕"), false)
	return nil
}

// sendPrompt types a prompt into a live agent's pane.
func (m *Model) sendPrompt(r *liveRow) tea.Cmd {
	if r.local == nil {
		m.setStatus(T("this agent runs outside nanny — open it here (⏎) to type into it"), true)
		return nil
	}
	lp := r.local.pane
	m.prompt(Tf("Send a prompt to %s", trunc(paneTitle(lp), 40)), T("Typed into the agent's pane and submitted."), T("e.g. run the tests and open the PR"),
		func(m *Model, v string) tea.Cmd {
			if v != "" {
				typeInto(lp, v)
				m.setStatus(T("sent"), false)
			}
			return nil
		})
	if r.sess != nil {
		m.modal.fill = &promptFill{dir: r.sess.Cwd}
	}
	return nil
}
