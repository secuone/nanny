package ui

import (
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/secuone/nanny/internal/ws"
)

// welcome is the short guide shown the first time nanny opens: what it is,
// what each view is for and the keys worth knowing. ctrl+k brings it back.
type welcome struct{ page int }

const welcomePages = 4

func welcomedPath() string { return filepath.Join(filepath.Dir(ws.StatePath()), "welcomed") }

// firstRun is true until the guide has been closed once.
func firstRun() bool {
	if os.Getenv("NANNY_NO_WELCOME") != "" {
		return false
	}
	_, err := os.Stat(welcomedPath())
	return os.IsNotExist(err)
}

func (m *Model) closeWelcome() {
	m.welcome = nil
	p := welcomedPath()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = os.WriteFile(p, nil, 0o644)
	m.maybeSetup()
}

func (m Model) welcomeKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	w := m.welcome
	switch k.String() {
	case "esc", "q", "ctrl+c":
		m.closeWelcome()
	case "right", "l", "n", "enter", " ", "tab":
		if w.page++; w.page >= welcomePages {
			m.closeWelcome()
		}
	case "left", "h", "p", "shift+tab":
		w.page = max(0, w.page-1)
	case "?":
		m.closeWelcome()
		m.help = true
	}
	return m, nil
}

func (m Model) welcomeMouse(e tea.MouseMsg) (tea.Model, tea.Cmd) {
	if e.Button == tea.MouseButtonLeft {
		if mm, cmd, ok := m.keyClick(e.X, e.Y); ok {
			return mm, cmd
		}
	}
	return m, nil
}

func (m Model) viewWelcome() string {
	s := m.st
	w := min(84, m.w-4)
	inner := w - 6
	px := m.prefixLabel()
	var title string
	var rows [][2]string
	var intro, outro []string
	switch m.welcome.page {
	case 0:
		title = T("Welcome to nanny 👋")
		intro = []string{T("nanny keeps an eye on every Claude Code agent on this machine and gives you terminals to run them in.")}
		rows = [][2]string{
			{"⊞ " + T("all spaces"), T("everything at once: every agent, session and worktree")},
			{"◆ " + T("spaces"), T("a project with its own terminal tabs (Claude and shells), like tmux")},
			{T("views"), T("the bar on top (Live, Sessions…): at home it shows everything, in a space only that space")},
			{T("alerts"), T("a sound and a badge when an agent finishes or needs you")},
			{"q", T("quitting leaves the panes running; open nanny again to get them back")},
		}
		outro = []string{T("One minute and you know your way around.")}
	case 1:
		title = T("The views · keys 1-9")
		for i, n := range tabNames {
			rows = append(rows, [2]string{tabKey(tab(i)) + " " + T(n), viewDesc(tab(i))})
		}
		rows = append(rows, [2]string{"0 " + T("terminal"), T("in a space: back to its terminal tabs")})
		outro = []string{T("Hover a tab to see what it holds. tab / shift+tab move along the bar.")}
	case 2:
		title = T("Spaces and terminals")
		intro = []string{Tf("In a space every command starts with the prefix: press %s, let go, then the key.", px)}
		rows = [][2]string{
			{"N", T("new space: pick a folder")},
			{px + " c  ·  s", T("new Claude tab · new shell tab")},
			{px + " v  ·  -", T("split right · split down")},
			{px + " h l  ·  z", T("move between panes · zoom one")},
			{px + " x  ·  X", T("close the pane · close the tab")},
			{px + " ,", T("rename the tab (or double-click it)")},
			{px + " i  ·  0", T("this space's views · back to the terminal")},
			{"alt+1-9", T("switch space · alt+0 all spaces")},
		}
	case 3:
		title = T("Mouse and handy keys")
		rows = [][2]string{
			{T("click"), T("tabs, + buttons and every key hint on screen")},
			{T("hover"), T("a tab shows what is inside")},
			{T("right-click"), T("a tab, pane, space, worktree or row: rename, icon, new worktree, split, close…")},
			{T("drag"), T("a tab or a space to put it somewhere else")},
			{"ctrl+k", T("command palette: jump to anything")},
			{"!", T("next agent that needs you")},
			{"M", T("next screen profile: Basic · Full · yours")},
			{"?", T("every shortcut")},
		}
		outro = []string{T("ctrl+k → Welcome guide shows this again.")}
	}
	var b strings.Builder
	b.WriteString(s.Accent.Render(title) + "\n\n")
	para := func(ps []string) {
		for _, p := range ps {
			for _, l := range wrap(p, inner, 4) {
				b.WriteString(s.Text.Render(l) + "\n")
			}
			b.WriteString("\n")
		}
	}
	para(intro)
	kw := 0
	for _, r := range rows {
		kw = max(kw, lipgloss.Width(r[0]))
	}
	kw = min(kw+2, inner/3)
	for _, r := range rows {
		ds := wrap(r[1], inner-kw, 3)
		for i, d := range ds {
			k := ""
			if i == 0 {
				k = r[0]
			}
			b.WriteString(s.Key.Render(fit(k, kw)) + s.Text.Render(d) + "\n")
		}
	}
	if len(outro) > 0 {
		b.WriteString("\n")
		for _, l := range wrap(outro[0], inner, 3) {
			b.WriteString(s.Muted.Render(l) + "\n")
		}
	}
	dots := ""
	for i := range welcomePages {
		if i == m.welcome.page {
			dots += s.Accent.Render("●") + " "
		} else {
			dots += s.Dim.Render("○") + " "
		}
	}
	next := T("next") + " →"
	if m.welcome.page == welcomePages-1 {
		next = T("start") + " ✓"
	}
	nav := ""
	if m.welcome.page > 0 {
		nav += clickable("p", s.Key.Render("← "+T("back"))) + s.Dim.Render(" · ")
	}
	nav += clickable("n", s.Key.Render(next)) + s.Dim.Render(" · ") + clickable("esc", s.Key.Render("esc")+" "+s.KeyDesc.Render(T("close")))
	b.WriteString("\n" + fit(dots, inner-lipgloss.Width(nav)) + nav)
	return s.Modal.Width(w).Render(b.String())
}
