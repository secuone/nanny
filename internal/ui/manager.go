package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/secuone/nanny/internal/discover"
	"github.com/secuone/nanny/internal/ws"
)

// manager is the spaces modal (prefix w / W at home).
type manager struct {
	cur int
}

// discovery is the "found places" picker.
type discovery struct {
	places []discover.Place
	picked map[int]bool
	cur    int
	busy   bool
}

type discoverMsg []discover.Place

func (m *Model) openManager() {
	m.mgr = &manager{cur: max(0, m.cur)}
}

func expandPath(p string) string {
	p = strings.TrimSpace(p)
	if strings.HasPrefix(p, "~/") || p == "~" {
		p = home + p[1:]
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	return p
}

// newSpacePrompt creates a space: the folder first, then its name (the folder's by default).
func (m *Model) newSpacePrompt() tea.Cmd {
	return m.openFolderPicker(T("New space · folder"), T("Pick the folder this space works in: every tab and split starts there."), "",
		func(m *Model, dir string) tea.Cmd {
			if s, i := m.spaceFor(dir, false); s != nil && s.Dir() == dir {
				m.cur = i
				m.setStatus(T("space already exists: ")+s.Name, false)
				return nil
			}
			name := filepath.Base(dir)
			m.prompt(T("New space · name"), "", name, func(m *Model, n string) tea.Cmd {
				if n == "" {
					n = name
				}
				m.spaces = append(m.spaces, ws.NewSpace(n, dir))
				m.cur = len(m.spaces) - 1
				m.saveSpaces()
				m.layoutPanes()
				m.setStatus(Tf("space “%s” created — c for Claude, s for a shell", n), false)
				return nil
			})
			m.modal.input.SetValue(name)
			m.modal.input.CursorEnd()
			return nil
		})
}

// setFolderPrompt changes the folder of a space. Panes already open stay where they
// are; the tabs and splits started from now on begin in the new folder.
func (m *Model) setFolderPrompt(sp *ws.Space) tea.Cmd {
	start := ""
	if sp.HasFolder() {
		start = tilde(sp.Dir()) + "/"
	}
	return m.openFolderPicker(Tf("Folder of %s", sp.Name), T("New tabs and splits start here. Panes that are already open stay where they are."), start,
		func(m *Model, dir string) tea.Cmd {
			sp.SetFolder(dir)
			m.saveSpaces()
			m.layoutPanes()
			m.setStatus(Tf("“%s” starts in %s now", sp.Name, tilde(dir)), false)
			return nil
		})
}

// folderTag is the folder of a space for a row or a title; one without a folder says so, and a click sets it.
func (m Model) folderTag(sp *ws.Space, sep string) string {
	s := m.st
	if sp.HasFolder() {
		return s.Muted.Render(sep + tilde(sp.Dir()))
	}
	return s.Muted.Render(sep) + clickable("@setfolder:"+strconv.Itoa(m.indexOf(sp)), s.Yellow.Render(T("no folder · set")))
}

func (m *Model) discoverCmd() tea.Cmd {
	m.disc = &discovery{busy: true, picked: map[int]bool{}}
	sessions := m.sessions
	roots := m.cfg.DiscoverRoots
	if len(roots) == 0 {
		roots = discover.DefaultRoots()
	} else {
		for i, r := range roots {
			roots[i] = expandPath(r)
		}
	}
	extra := append([]string{}, m.cfg.ExtraRepos...)
	for _, sp := range m.spaces {
		extra = append(extra, sp.Paths...)
	}
	return func() tea.Msg { return discoverMsg(discover.Find(sessions, roots, extra)) }
}

func (m *Model) existing(path string) bool {
	for _, s := range m.spaces {
		for _, p := range s.Paths {
			if p == path {
				return true
			}
		}
	}
	return false
}

func (m Model) managerKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	g := m.mgr
	n := len(m.spaces)
	var sp *ws.Space
	if g.cur < n {
		sp = m.spaces[g.cur]
	}
	switch k.String() {
	case "esc", "q", "w", "W":
		m.mgr = nil
	case "j", "down":
		g.cur = min(n-1, g.cur+1)
	case "k", "up":
		g.cur = max(0, g.cur-1)
	case "g", "home":
		g.cur = 0
	case "G", "end":
		g.cur = max(0, n-1)
	case "enter":
		m.mgr = nil
		if sp != nil {
			m.cur = g.cur
			m.layoutPanes()
		}
	case "n", "N":
		m.mgr = nil
		return m, m.newSpacePrompt()
	case "D":
		m.mgr = nil
		return m, m.discoverCmd()
	case "f":
		if sp != nil {
			m.mgr = nil
			return m, m.setFolderPrompt(sp)
		}
	case "r":
		if sp != nil {
			m.rename(T("Rename space"), sp.Name, func(v string) { sp.Name = v })
		}
	case "a":
		if sp != nil {
			m.prompt(Tf("Add a folder to %s", sp.Name), T("Sessions and agents in this folder will belong to the space."), T("~/path/to/folder"),
				func(m *Model, v string) tea.Cmd {
					dir := expandPath(v)
					if st, err := os.Stat(dir); err != nil || !st.IsDir() {
						m.setStatus(T("not a folder: ")+v, true)
						return nil
					}
					sp.Paths = append(sp.Paths, dir)
					m.saveSpaces()
					return nil
				})
		}
	case "x":
		if sp != nil && len(sp.Paths) > 1 {
			sp.Paths = sp.Paths[:len(sp.Paths)-1]
			m.saveSpaces()
			m.setStatus(Tf("removed last folder from %s", sp.Name), false)
		}
	case "d":
		if sp != nil {
			running := 0
			for _, t := range sp.Tabs {
				running += len(t.Panes)
			}
			m.ask(Tf("Delete space “%s”?", sp.Name), Tn(running, "Closes its %d pane. Sessions stay on disk; folders aren't touched.", "Closes its %d panes. Sessions stay on disk; folders aren't touched."),
				func(m *Model, _ string) tea.Cmd {
					for _, t := range sp.Tabs {
						for _, p := range t.Panes {
							p.Close()
						}
					}
					for i, s := range m.spaces {
						if s == sp {
							m.spaces = append(m.spaces[:i], m.spaces[i+1:]...)
							break
						}
					}
					if m.cur >= len(m.spaces) {
						m.cur = len(m.spaces) - 1
					}
					m.saveSpaces()
					m.layoutPanes()
					return nil
				})
		}
	case "K", "shift+up":
		if g.cur > 0 {
			m.spaces[g.cur], m.spaces[g.cur-1] = m.spaces[g.cur-1], m.spaces[g.cur]
			g.cur--
			m.saveSpaces()
		}
	case "J", "shift+down":
		if g.cur < n-1 {
			m.spaces[g.cur], m.spaces[g.cur+1] = m.spaces[g.cur+1], m.spaces[g.cur]
			g.cur++
			m.saveSpaces()
		}
	}
	return m, nil
}

func (m Model) viewManager() string {
	s := m.st
	g := m.mgr
	w := min(100, m.w-6)
	inner := w - 6
	var b strings.Builder
	b.WriteString(s.Accent.Render(T("Spaces")) + s.Muted.Render(fmt.Sprintf("  %d · ", len(m.spaces))+T("each space is a project with its own tabs")) + "\n\n")
	if len(m.spaces) == 0 {
		b.WriteString(s.Muted.Render(T("No spaces yet. A space is a project: its folders, its tabs and its agents.")) + "\n\n")
		b.WriteString(strings.Join(s.keyList([][2]string{{"n", T("new space from a folder")}, {"D", T("discover the places you already work in")}}, inner), "\n") + "\n")
	}
	m.vs.modalY, m.vs.modalStart, m.vs.modalN = 2, 0, len(m.spaces)
	for i, sp := range m.spaces {
		panes := 0
		for _, t := range sp.Tabs {
			panes += len(t.Panes)
		}
		sess := 0
		for _, ss := range m.sessions {
			if sp.Contains(ss.Cwd) {
				sess++
			}
		}
		right := s.Muted.Render(Tn(len(sp.Tabs), "%d tab", "%d tabs") + " · " + Tn(panes, "%d pane", "%d panes") + " · " + Tn(sess, "%d session", "%d sessions"))
		line := fmt.Sprintf("%s %s  %s", m.paneGlyph(m.spaceState(sp)), s.Bold.Render(spaceLabel(sp)), s.Peach.Render(trunc(tilde(sp.Dir()), inner/2)))
		if !sp.HasFolder() {
			line = fmt.Sprintf("%s %s  %s", m.paneGlyph(m.spaceState(sp)), s.Bold.Render(spaceLabel(sp)), s.Yellow.Render(T("no folder · set")))
		}
		if len(sp.Paths) > 1 {
			line += s.Muted.Render(fmt.Sprintf(" +%d", len(sp.Paths)-1))
		}
		line = fit(line, inner-lipglossW(right)) + right
		if i == g.cur {
			line = s.Sel(s.RowSel, fit(line, inner))
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\n" + s.hintsFit([][2]string{{"⏎", T("open")}, {"n", T("new")}, {"D", T("discover")}, {"f", T("folder")}, {"r", T("rename")}, {"a/x", T("add/drop folder")}, {"d", T("delete")}, {"J/K", T("move")}, {"esc", T("close")}}, inner))
	return s.Modal.Width(w + 2).Render(b.String())
}

func (m Model) discoveryKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	d := m.disc
	n := len(d.places)
	switch k.String() {
	case "esc", "q":
		m.disc = nil
	case "j", "down":
		d.cur = min(n-1, d.cur+1)
	case "k", "up":
		d.cur = max(0, d.cur-1)
	case "g", "home":
		d.cur = 0
	case "G", "end":
		d.cur = max(0, n-1)
	case " ", "space", "x":
		if d.cur < n && !m.existing(d.places[d.cur].Path) {
			d.picked[d.cur] = !d.picked[d.cur]
		}
		d.cur = min(n-1, d.cur+1)
	case "a": // pick everything with agent activity
		for i, p := range d.places {
			if p.Sessions > 0 && !m.existing(p.Path) {
				d.picked[i] = true
			}
		}
	case "A":
		for i, p := range d.places {
			if !m.existing(p.Path) {
				d.picked[i] = true
			}
		}
	case "u":
		d.picked = map[int]bool{}
	case "enter":
		added := 0
		for i, p := range d.places {
			if d.picked[i] && !m.existing(p.Path) {
				m.spaces = append(m.spaces, ws.NewSpace(p.Name, p.Path))
				added++
			}
		}
		m.disc = nil
		m.saveSpaces()
		m.setStatus(Tn(added, "added %d space", "added %d spaces"), false)
	}
	return m, nil
}

func (m Model) viewDiscovery() string {
	s := m.st
	d := m.disc
	w := min(110, m.w-6)
	inner := w - 6
	var b strings.Builder
	b.WriteString(s.Accent.Render(T("Discover")) + s.Muted.Render("  "+T("places where you work: agent sessions and git repos in your code folders")) + "\n\n")
	if d.busy && len(d.places) == 0 {
		b.WriteString(s.Muted.Render(spinGlyph+" "+T("scanning…")) + "\n")
	} else if len(d.places) == 0 {
		b.WriteString(s.Muted.Render(T("Found nothing. Add your code folders under discover_roots in the config file, or create a space by hand with N.")) + "\n")
	}
	rows := min(18, m.h-12)
	start := 0
	if d.cur >= rows {
		start = d.cur - rows + 1
	}
	m.vs.modalY, m.vs.modalStart, m.vs.modalN = 2, start, max(0, min(len(d.places), start+rows)-start)
	for i := start; i < min(len(d.places), start+rows); i++ {
		p := d.places[i]
		box := s.Muted.Render("[ ]")
		if m.existing(p.Path) {
			box = s.Green.Render("[✓]")
		} else if d.picked[i] {
			box = s.Accent.Render("[x]")
		}
		var tags []string
		if p.Live > 0 {
			tags = append(tags, s.Yellow.Render(Tf("%d live", p.Live)))
		}
		if p.Sessions > 0 {
			tags = append(tags, s.Teal.Render(Tn(p.Sessions, "%d session", "%d sessions")))
		}
		for _, src := range p.Sources {
			if src != "agents" {
				tags = append(tags, s.Muted.Render(src))
			}
		}
		age := ago(p.Last)
		right := strings.Join(tags, s.Dim.Render(" · ")) + "  " + s.Muted.Render(strings.Repeat(" ", max(0, 6-len(age)))+age)
		line := box + " " + s.Bold.Render(fit(trunc(p.Name, 26), 26)) + " " + s.Peach.Render(trunc(tilde(p.Path), inner-32-lipglossW(right)))
		line = fit(line, inner-lipglossW(right)) + right
		if i == d.cur {
			line = s.Sel(s.RowSel, fit(line, inner))
		}
		b.WriteString(line + "\n")
	}
	picked := 0
	for range d.picked {
		picked++
	}
	count := s.Muted.Render(Tf("%d found", len(d.places)) + " · " + Tf("%d picked", picked) + "   ")
	b.WriteString("\n" + count + s.hintsFit([][2]string{{"space", T("pick")}, {"a", T("with agents")}, {"A", T("all")}, {"u", T("none")}, {"⏎", T("add")}, {"esc", T("close")}}, inner-lipglossW(count)))
	return s.Modal.Width(w + 2).Render(b.String())
}
