package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/secuone/nanny/internal/claude"
)

// viewer is a full-screen, live-following transcript of a session or subagent.
type viewer struct {
	path    string
	sub     bool
	title   string
	parent  *claude.Session
	agent   *claude.Subagent
	entries []claude.Entry
	off     int  // lines from the bottom
	follow  bool // stick to the newest output
	tools   bool // show tool calls
}

type viewerMsg struct {
	path    string
	entries []claude.Entry
}

func viewerCmd(path string, sub bool) tea.Cmd {
	return func() tea.Msg {
		return viewerMsg{path, claude.FullTranscript(path, sub)}
	}
}

func (m *Model) openViewer(path string, sub bool, title string, parent *claude.Session, agent *claude.Subagent) tea.Cmd {
	m.view = &viewer{path: path, sub: sub, title: title, parent: parent, agent: agent, follow: true, tools: true}
	return viewerCmd(path, sub)
}

func (m Model) viewerKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	v := m.view
	page := max(1, m.h-8)
	switch k.String() {
	case "esc", "q":
		m.view = nil
	case "alt+down", "alt+up": // on to the next agent
		m.view = nil
		from := m.curAgentKey()
		if v.parent != nil {
			from = "s:" + v.parent.ID
		}
		mm, cmd, _ := m.navFrom(k.String(), from)
		return mm, cmd
	case "k", "up":
		v.off++
		v.follow = false
	case "j", "down":
		v.off = max(0, v.off-1)
		v.follow = v.off == 0
	case "pgup", "ctrl+u", "b":
		v.off += page
		v.follow = false
	case "pgdown", "ctrl+d", " ", "space":
		v.off = max(0, v.off-page)
		v.follow = v.off == 0
	case "g", "home":
		v.off = 1 << 30
		v.follow = false
	case "G", "end", "f":
		v.off = 0
		v.follow = true
	case "t":
		v.tools = !v.tools
	case "enter", "p":
		if v.parent != nil {
			m.view = nil
			if r, ok := m.localPane(v.parent.ID); ok {
				m.focusRef(r)
				return m, nil
			}
			return m, m.resume(v.parent, false)
		}
	}
	return m, nil
}

func (m Model) viewViewer(w, h int) string {
	s := m.st
	v := m.view
	inner := w - 4
	var head []string
	state := ""
	if v.agent != nil {
		st := "finished"
		if v.agent.Running {
			st = "running"
		}
		state = m.stateGlyph(st) + " " + s.Text.Render(T(st))
		head = append(head, s.Muted.Render(v.agent.Type)+"  "+s.Bold.Render(trunc(v.agent.Description, inner-30))+"  "+state)
		if v.parent != nil {
			head = append(head, s.Muted.Render("↳ "+T("subagent of")+" ")+s.Text.Render(trunc(v.parent.Title, inner/2))+s.Muted.Render("  · "+project(v.parent.Cwd)))
		}
	} else {
		head = append(head, s.Bold.Render(trunc(v.title, inner)))
		if v.parent != nil {
			head = append(head, s.Muted.Render(tilde(v.parent.Cwd)+" · "+v.parent.ID))
		}
	}
	head = append(head, s.Dim.Render(strings.Repeat("─", inner)))

	var body []string
	for _, e := range v.entries {
		switch e.Role {
		case "user":
			body = append(body, "")
			for i, l := range wrap(e.Text, inner-2, 60) {
				p := "  "
				if i == 0 {
					p = s.Accent.Render("❯ ")
				}
				body = append(body, p+s.Bold.Render(l))
			}
		case "assistant":
			for i, l := range wrap(e.Text, inner-2, 200) {
				p := "  "
				if i == 0 {
					p = s.Teal.Render("● ")
				}
				body = append(body, p+s.Text.Render(l))
			}
		case "tool":
			if !v.tools {
				continue
			}
			name, arg, _ := strings.Cut(e.Text, " ")
			ts := ""
			if !e.At.IsZero() {
				ts = s.Dim.Render(e.At.Local().Format("15:04:05") + " ")
			}
			body = append(body, "  "+ts+s.Dim.Render("⚙ ")+s.Muted.Render(name+" "+trunc(arg, inner-16-len(name))))
		}
	}
	if len(body) == 0 {
		body = []string{s.Muted.Render("  " + spinGlyph + " " + T("loading…"))}
	}
	room := h - 2 - len(head) - 1
	maxOff := max(0, len(body)-room)
	off := min(v.off, maxOff)
	end := len(body) - off
	start := max(0, end-room)
	shown := body[start:end]
	for len(shown) < room {
		shown = append(shown, "")
	}
	pos := s.Green.Render("● " + T("following"))
	if !v.follow {
		pos = s.Yellow.Render("↑ " + Tn(off, "%d line up", "%d lines up"))
	}
	foot := pos + s.Muted.Render("  ·  "+map[bool]string{true: T("tool calls shown"), false: T("tool calls hidden")}[v.tools])
	content := strings.Join(head, "\n") + "\n" + strings.Join(shown, "\n") + "\n" + foot
	title := T("Conversation")
	if v.agent != nil {
		title = T("Subagent")
	}
	return s.panel(title, content, w, h, true)
}
