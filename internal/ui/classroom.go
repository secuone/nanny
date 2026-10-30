package ui

import (
	"github.com/secuone/nanny/internal/classroom"
	"github.com/secuone/nanny/internal/claude"

	tea "github.com/charmbracelet/bubbletea"
)

// The 3D classroom: your agents as teachers and their subagents as kids, in a
// browser window nanny serves on localhost and keeps up to date.

// classWords are the page's labels; the page shows them as given.
var classWords = []string{M("working"), M("needs you"), M("done"), M("idle"), M("running"), M("finished"),
	M("Lesson"), M("recess"), M("playing"), M("asleep"), M("No agents running right now"), M("start one in nanny and it walks in"),
	M("last tool"),
	M("waiting for you"), M("context"), M("started"), M("last act."), M("click a kid or a teacher · drag to look around · scroll to zoom"),
	M("all classes"), M("follow")}

// openClassroom starts the page's server once and opens it in the browser.
func (m *Model) openClassroom() tea.Cmd {
	if m.class == nil {
		s, err := classroom.Start()
		if err != nil {
			m.setStatus(T("could not start the 3D classroom: ")+err.Error(), true)
			return nil
		}
		m.class = s
	}
	m.pushClassroom()
	return tea.Batch(m.openURL(m.class.URL()), m.classSubs())
}

// classSubs keeps every live agent's subagents fresh while the page is open,
// whatever view is on screen.
func (m *Model) classSubs() tea.Cmd { return m.classSubsIf(nil) }

// classSubsIf is classSubs for the transcripts in (all with nil) only.
func (m *Model) classSubsIf(in func(path string) bool) tea.Cmd {
	if m.class == nil {
		return nil
	}
	var cmds []tea.Cmd
	for _, r := range m.live.allRows {
		if r.agent() && r.sess != nil && (in == nil || in(r.sess.Path)) {
			cmds = append(cmds, subsCmd(r.sess.Path))
		}
	}
	return tea.Batch(cmds...)
}

// pushClassroom hands the page the agents as they are now.
func (m *Model) pushClassroom() {
	if m.class == nil {
		return
	}
	snap := classroom.Snapshot{Words: map[string]string{}, Font: m.cfg.Look.WebFont}
	for _, w := range classWords {
		snap.Words[w] = T(w)
	}
	for _, r := range m.live.allRows {
		if !r.agent() || r.sess == nil {
			continue
		}
		title, cwd := r.titleCwd()
		c := classroom.Class{ID: r.sess.ID, Title: title, Project: project(cwd), Branch: r.sess.Branch,
			State: r.state(), Lesson: r.sess.LastPrompt, Cost: r.sess.Cost}
		if r.sess.Worktree != "" {
			c.Branch = r.sess.Worktree
		}
		if u := r.sess.Usage; u != nil {
			c.Context = u.Pct()
		}
		if p := r.prompt(); p != nil {
			c.Prompt = promptSummary(p)
		}
		if r.local != nil {
			c.Where = r.local.space.Name + "/" + r.local.tab.Label
		}
		for _, f := range claude.Flatten(m.subs[r.sess.Path]) {
			c.Kids = append(c.Kids, classroom.Kid{ID: f.ID, Type: f.Type, Desc: f.Description, Tool: f.LastTool,
				Text: f.LastText, Running: f.Running, Started: f.Started, Modified: f.Modified})
		}
		snap.Classes = append(snap.Classes, c)
	}
	m.class.Set(snap)
}
