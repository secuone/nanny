package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/secuone/nanny/internal/config"
)

type helpSec struct {
	title string
	rows  [][2]string
}

// helpSections is the single source of truth for the ? screen.
func (m Model) helpSections() []helpSec {
	px := m.prefixLabel()
	pk := func(name, key string) string { return fit(T(name), 9) + " " + key } // popup name + its key
	home := []helpSec{
		{T("Home"), [][2]string{
			{"1-9  tab", T("switch view: Live · Sessions · Git · Tasks · Prompts · Skills · Monitor · Settings · 9 3D classroom")},
			{"E  I", T("Jira · Insights")},
			{"m  ≡", T("menu: settings, keys, what shows on screen, customize, config, detach")},
			{"  │", T("views left of │ show the space you are in (or all spaces); the ones right of it are the same everywhere")},
			{"j k  ↑ ↓", T("move · g G top / bottom · pgup pgdn page")},
			{"J K", T("next / previous agent, repo or group")},
			{"l → / h ←", T("focus the detail pane to scroll it / back")},
			{"click", T("select a row · double-click opens it")},
			{T("click a key"), T("tabs, + buttons and every key hint on screen press their key")},
			{T("right-click"), T("a tab, pane, space, worktree or row: rename, icon, new worktree, split, close…")},
			{T("hover · drag"), T("a tab shows what is inside · drag tabs and spaces to reorder")},
			{"ctrl+k  :", T("command palette: jump to anything")},
			{"!", T("next agent that needs you")},
			{"P", T("prompt library: saved and most used prompts")},
			{"u", T("task queue: a backlog handed to agents")},
			{"[ ]  \\", T("scope every view to a project · clear")},
			{"N W D", T("new space · manage spaces · discover places")},
			{"A S B", T("alerts inbox · mute sounds · sidebar")},
			{"M", T("next screen profile: Basic · Full · yours")},
			{"=", T("customize: turn views, Monitor sources, sidebar and top bar bits on and off")},
			{"T", T("theme picker, previews live")},
			{"R  q", T("rescan everything · quit (panes keep running; reopen to reattach)")},
			{"alt+↑ ↓", T("previous / next agent")},
		}},
		{T("Same key in every list"), [][2]string{
			{"⏎", T("main action: go to · resume · edit · open")},
			{"v", T("read the conversation, full screen")},
			{"r  f", T("resume · fork (--fork-session)")},
			{"o", T("shell in its folder")},
			{"y", T("copy: resume command · path · /invocation")},
			{"/  esc", T("filter · clear and go back")},
		}},
		{T("Live"), [][2]string{
			{"⏎", T("go to the agent's pane, or resume it · subagent: read it")},
			{"⏎ ⚠", T("waiting on a permission prompt: answer it here")},
			{"p", T("type a prompt into the agent's pane")},
			{"space  p", T("pick agents · send one prompt to all of them")},
			{"+  .", T("queue a task in its folder · send its last prompt again")},
			{"O", T("open the PR of its branch · ⎇ main ●3 ↑2 ↓1: changed, to push, to pull · #96✓✔: PR, checks, approved")},
			{"n", T("new task: folder, first prompt, optional worktree")},
			{"C", T("review the changes in its folder")},
			{"s  a", T("mirror its screen · show finished subagents")},
			{"c  i", T("copy folder · session id")},
		}},
		{T("Sessions"), [][2]string{
			{"F", T("full-text search inside every conversation")},
			{"b w L", T("bots · only worktrees · only live")},
			{"c  i", T("copy folder · session id")},
		}},
	}
	if qk := m.cfg.QuitKeyName(); qk != "" {
		home[0].rows = append(home[0].rows, [2]string{keyShort(qk), T("quit (asks first; panes keep running)")})
	}
	for _, f := range m.feats() { // Git, Jira and Insights bring their own
		home = append(home, f.help()...)
	}
	home = append(home, []helpSec{
		{T("Skills"), [][2]string{
			{"⏎  e", T("edit in $EDITOR")},
			{"space", T("enable / disable")},
			{"n  c", T("new skill, command or agent (global or a repo) · copy into a project's .claude")},
			{"t H p", T("type filter, the harness too (hooks, MCP, plugins, CLAUDE.md) · hide disabled · copy path")},
		}},
		{T("Monitor & Settings"), [][2]string{
			{"← →  ⏎  L", T("Monitor: sections · open (browser, resume, run, log; containers and Tilt: their actions) · follow log")},
			{"space  e", T("Monitor: turn on a section that is off · edit [[watch]] entries")},
			{"←→  ⏎", T("Settings: change · edit text")},
			{"e  t", T("Settings: config in $EDITOR · next theme")},
		}},
	}...)
	spaces := []helpSec{
		{Tf("Spaces & panes · %s then", px), [][2]string{
			{"c  s", T("new Claude tab · new shell tab")},
			{"C  g", T("new task (prompt, worktree) · review this pane's changes")},
			{"P  U", T("prompt library · task queue")},
			{"i  0", T("this space's views (Live, Sessions, Tasks…) · 0 back to the terminal")},
			{"a", T("your agents as a classroom in 3D, in the browser")},
			{"v - V", T("split right · split down · split right with Claude")},
			{"n p 1-9", T("next · previous · go to tab")},
			{"h l  z", T("move between panes · zoom")},
			{"x  X", T("close pane · close tab")},
			{",  R", T("rename tab · rename space")},
			{"r  f", T("resume this space's latest session · find its sessions")},
			{"] [  0", T("next · previous space · home")},
			{"G", T("this space's Git: branch, PR, worktrees")},
			{"w N D", T("manage spaces · new space · discover")},
			{"u d", T("scroll back · forward (or the wheel)")},
			{"K :  T  ?", T("command palette · themes · this help")},
			{"m", T("menu: settings, keys, what shows on screen, customize, config, detach")},
			{"q  Q", T("quit, panes keep running · quit and stop them all")},
			{px + " " + px, Tf("send %s to the program", px)},
			{"alt+1-9", T("switch space without the prefix (alt+0 home)")},
			{"alt+↑ ↓", T("previous / next agent")},
		}},
		{T("Conversation viewer"), [][2]string{
			{"j k", T("scroll · pgup pgdn page · g top · G follow")},
			{"t", T("show / hide tool calls")},
			{"⏎", T("go to the agent (its pane, or resume it)")},
			{"esc", T("back")},
		}},
		{T("Changes view"), [][2]string{
			{"j k  l h", T("pick a file · into the diff (j k scroll) · back")},
			{"space b  n N", T("page the diff · next / previous hunk")},
			{"t", T("uncommitted vs HEAD · whole branch vs its base")},
			{"c  w  y", T("ask the agent to commit · open the PR (gh) · copy branch")},
			{"o  r  esc", T("shell there · refresh · back")},
		}},
		{T("Prompt library"), [][2]string{
			{"⏎", T("insert: typed into the pane, or sent to the Live agent")},
			{"{name}", T("placeholders ask for a value first")},
			{"/  J K", T("filter · jump between Saved and Most used")},
			{"s  n  e", T("save a most used one · new · edit (ctrl+s saves)")},
			{"d  p  y", T("delete · pin to the top · copy")},
			{"q", T("add it to the task queue")},
		}},
		{T("Task queue"), [][2]string{
			{"n  e  d", T("add · edit · remove a task")},
			{"J K", T("move a pending task up / down")},
			{"⏎", T("run it now (any cap) · running: go to its agent")},
			{"a", T("auto-dispatch: start pending tasks by themselves")},
			{"g  r  c", T("go to its agent · retry · clear finished")},
		}},
		{T("Popups"), [][2]string{
			{pk(M("answer"), "⏎"), T("y yes · a always · n no · 1-9 option · g go to pane")},
			{T("snippets"), T("^k then “snippet”: a saved prompt into the pane")},
			{pk(M("task"), "n"), T("tab field · ↑↓ folder · space worktree · ⏎ start")},
			{T("prompt box"), T("alt+1-9 a saved prompt · ctrl+r the folder's most used")},
			{pk(M("alerts"), "A"), T("⏎ jump · c clear · S mute")},
			{pk(M("spaces"), "W"), T("⏎ open · n new · r rename · a x folders · d delete · J K move")},
			{pk(M("find"), "D"), T("space pick · a with agents · A all · ⏎ add")},
		}},
	}
	if m.inSpace() || m.view != nil {
		return append(spaces, home...)
	}
	return append(home, spaces...)
}

func (m Model) helpKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	body, _ := m.helpBody()
	maxOff := max(0, len(body)-m.helpRoom())
	switch k.String() {
	case "j", "down":
		m.helpOff++
	case "k", "up":
		m.helpOff--
	case "pgdown", "ctrl+d", " ", "space":
		m.helpOff += max(1, m.helpRoom()-2)
	case "pgup", "ctrl+u":
		m.helpOff -= max(1, m.helpRoom()-2)
	case "g", "home":
		m.helpOff = 0
	case "G", "end":
		m.helpOff = maxOff
	default:
		m.help, m.helpOff = false, 0
	}
	m.helpOff = max(0, min(m.helpOff, maxOff))
	return m, nil
}

// helpRoom is how many body lines fit: screen minus border, padding, head and foot.
func (m Model) helpRoom() int { return max(1, m.h-8) }

// helpBody lays the sections out in one or two columns; w is the box width.
func (m Model) helpBody() ([]string, int) {
	s := m.st
	secs := m.helpSections()
	cols := 1
	w := min(m.w-4, 90)
	if m.w >= 150 {
		cols, w = 2, min(m.w-4, 176)
	}
	inner := w - 4
	colW := (inner - 3*(cols-1)) / cols
	render := func(sec helpSec) []string {
		ls := []string{s.Accent.Render(sec.title)}
		for _, r := range sec.rows {
			ls = append(ls, "  "+s.Key.Render(fit(r[0], 13))+" "+s.Text.Render(trunc(r[1], colW-16)))
		}
		return append(ls, "")
	}
	var all [][]string
	total := 0
	for _, sec := range secs {
		b := render(sec)
		all = append(all, b)
		total += len(b)
	}
	// greedy split into columns of roughly equal height
	columns := make([][]string, cols)
	c := 0
	for _, b := range all {
		if c < cols-1 && len(columns[c]) > 0 && len(columns[c])+len(b)/2 > total/cols {
			c++
		}
		columns[c] = append(columns[c], b...)
	}
	var body []string
	for i := 0; ; i++ {
		var parts []string
		done := true
		for _, col := range columns {
			l := ""
			if i < len(col) {
				l, done = col[i], false
			}
			parts = append(parts, fit(l, colW))
		}
		if done {
			break
		}
		body = append(body, strings.Join(parts, "   "))
	}
	for len(body) > 0 && strings.TrimSpace(body[len(body)-1]) == "" {
		body = body[:len(body)-1]
	}
	return body, w
}

func (m Model) viewHelp() string {
	s := m.st
	body, w := m.helpBody()
	room := m.helpRoom()
	off := min(m.helpOff, max(0, len(body)-room))
	more := off+room < len(body)
	body = body[off:min(len(body), off+room)]
	foot := s.hints([][2]string{{"j/k", T("scroll")}, {"esc", T("close")}})
	if more {
		foot = s.Yellow.Render("↓ "+T("more")) + "  " + foot
	}
	foot += s.Muted.Render("   " + T("config") + " " + tilde(config.Path()))
	head := s.Logo.Render("◆ nanny") + s.Muted.Render("  "+T("keys"))
	return s.Modal.Width(w + 2).Render(head + "\n\n" + strings.Join(body, "\n") + "\n\n" + fit(foot, w-4))
}
