package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/secuone/nanny/internal/claude"
	"github.com/secuone/nanny/internal/config"
	"github.com/secuone/nanny/internal/prompts"
)

// promptFill backs the shortcuts of the boxes that type a prompt for an agent:
// alt+1-9 adds a top saved prompt, ctrl+r cycles the folder's most used
// prompts containing what was typed, like a shell's history search.
type promptFill struct {
	dir  string
	q    string // typed before the first ctrl+r
	hits []string
	n    int
}

// fillKey applies a shortcut to in; false when ks isn't one.
func (m *Model) fillKey(f *promptFill, ks string, in *textinput.Model) bool {
	if ks != "ctrl+r" {
		f.hits = nil
	}
	switch {
	case ks == "ctrl+r":
		if f.hits == nil {
			f.q, f.n = in.Value(), 0
			f.hits = projectPrompts(f.dir, f.q)
			if len(f.hits) == 0 {
				m.setStatus(Tf("no prompt used in %s matches", project(f.dir)), true)
				f.hits = nil
				return true
			}
		} else {
			f.n = (f.n + 1) % len(f.hits)
		}
		in.SetValue(f.hits[f.n])
		in.CursorEnd()
		m.setStatus(Tf("most used in %s · %d/%d · ctrl+r next", project(f.dir), f.n+1, len(f.hits)), false)
		return true
	case len(ks) == 5 && strings.HasPrefix(ks, "alt+") && ks[4] >= '1' && ks[4] <= '9':
		ss := config.LoadSnippets()
		i := int(ks[4] - '1')
		if i >= len(ss) {
			m.setStatus(Tn(len(ss), "only %d saved prompt", "only %d saved prompts"), true)
			return true
		}
		v := strings.TrimSpace(in.Value())
		if v != "" {
			v += " "
		}
		in.SetValue(v + oneLine(ss[i].Text))
		in.CursorEnd()
		_ = config.UseSnippet(ss[i].Name)
		m.setStatus(Tf("snippet “%s”", ss[i].Name), false)
		return true
	}
	return false
}

// projectPrompts are the most used prompts typed in dir's project (all of
// them when dir is ""), best first, containing every word of q.
func projectPrompts(dir, q string) []string {
	h, _ := prompts.Mine(claude.Dir("history.jsonl"))
	toks := strings.Fields(strings.ToLower(q))
	var out []string
	for _, u := range h.Prompts {
		if dir != "" && u.Projects[dir] == 0 && project(u.Project) != project(dir) {
			continue
		}
		ok := true
		for _, t := range toks {
			if !strings.Contains(strings.ToLower(u.Text), t) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, u.Text)
		}
	}
	return out
}

// repeatPrompt sends an idle agent the last prompt it was given, again.
func (m *Model) repeatPrompt(r *liveRow) tea.Cmd {
	if r.local == nil || r.sess == nil {
		m.setStatus(T("this agent runs outside nanny — open it here (⏎) to type into it"), true)
		return nil
	}
	last := ""
	for _, e := range m.transcripts[r.sess.Path] {
		if e.Role == "user" {
			last = e.Text
		}
	}
	switch st := r.state(); {
	case last == "":
		m.setStatus(T("no prompt to repeat yet"), true)
	case st == "working" || st == "blocked":
		m.setStatus(T("it's busy: repeat it once it's idle"), true)
	default:
		lp := r.local.pane
		m.ask(T("Send the last prompt again?"), trunc(last, 300), func(m *Model, _ string) tea.Cmd {
			typeInto(lp, last)
			m.setStatus(T("sent"), false)
			return nil
		})
	}
	return nil
}
