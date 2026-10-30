package ui

import (
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/secuone/nanny/internal/jira"
)

// Reading an issue without opening it: its description and comments come
// when the cursor rests on it, are kept per version of the issue (its
// updated stamp), and a search can reach beyond what is assigned to you.

// readVer names the version of an issue: it changes when Jira says it was updated.
func readVer(is jira.Issue) string {
	return is.Key + "@" + strconv.FormatInt(is.Updated.UnixNano(), 10)
}

// settleSel is what follows a move of the cursor: the PR and the reading are
// asked for once it stops.
func (j *jiraView) settleSel(m *Model) tea.Cmd {
	return tea.Batch(j.settleDev(m), j.settleRead(m))
}

// settleRead reads the selected issue once the cursor has rested on it for
// jiraDetSettle, and only when that version isn't kept already.
func (j *jiraView) settleRead(m *Model) tea.Cmd {
	is := j.sel()
	if is == nil || !j.connected(m) || j.readAsk[readVer(*is)] || j.readWant == is.Key {
		return nil
	}
	j.readWant = is.Key
	key := is.Key
	return tea.Tick(jiraDetSettle, func(time.Time) tea.Msg { return jiraDetWantMsg{key} })
}

func (j *jiraView) readFetch(m *Model, key string) tea.Cmd {
	if j.readWant == key {
		j.readWant = ""
	}
	is := j.sel()
	if is == nil || is.Key != key || j.readAsk[readVer(*is)] {
		return nil
	}
	return j.readNow(m, *is)
}

// readNow asks Jira for the issue at once.
func (j *jiraView) readNow(m *Model, is jira.Issue) tea.Cmd {
	ver, key := readVer(is), is.Key
	j.readAsk[ver] = true
	j.acting++
	cfg := m.cfg.Jira
	return func() tea.Msg {
		c, err := jiraAPI(cfg)
		if err != nil {
			return jiraDetailMsg{key: key, ver: ver, err: err}
		}
		ctx, cancel := jiraCtx()
		defer cancel()
		d, err := c.Detail(ctx, key)
		return jiraDetailMsg{key: key, ver: ver, d: d, err: err}
	}
}

func (j *jiraView) readIn(msg jiraDetailMsg) {
	j.acting--
	if len(j.read) >= jiraDetCache {
		j.read = map[string]jiraRead{}
	}
	if msg.err != nil {
		j.failed(msg.err)
		j.read[msg.key] = jiraRead{ver: msg.ver, err: jiraErrText(msg.err)}
		delete(j.readAsk, msg.ver) // it is asked again when the cursor comes back
		return
	}
	j.read[msg.key] = jiraRead{ver: msg.ver, d: msg.d}
}

// comments is how many comments the issue has, when it has been read.
func (j *jiraView) comments(key string) int {
	if r, ok := j.read[key]; ok && r.err == "" {
		return r.d.CommentTotal
	}
	return 0
}

// searchPrompt asks for what to look for in all of Jira.
func (j *jiraView) searchPrompt(m *Model) tea.Cmd {
	m.prompt(T("Search Jira"), T("Words look in the text of every issue you can see · jql: and a query runs it as typed."),
		T("words, or jql: project = PAY"), func(m *Model, v string) tea.Cmd { return j.remoteSearch(m, v) })
	if m.modal != nil && m.modal.input != nil {
		m.modal.input.SetValue(strings.TrimSpace(j.search.Value()))
	}
	return nil
}

// remoteSearch runs a search over all of Jira (text, or JQL after "jql:").
func (j *jiraView) remoteSearch(m *Model, v string) tea.Cmd {
	jql, ok := jira.SearchJQL(v)
	if !ok {
		return nil
	}
	j.acting++
	cfg := m.cfg.Jira
	field := cfg.SprintField
	q := strings.TrimSpace(v)
	return func() tea.Msg {
		c, err := jiraAPI(cfg)
		if err != nil {
			return jiraSearchMsg{q: q, err: err}
		}
		ctx, cancel := jiraCtx()
		defer cancel()
		is, err := c.Search(ctx, jql, field)
		return jiraSearchMsg{q: q, issues: is, err: err}
	}
}

func (j *jiraView) searchIn(m *Model, msg jiraSearchMsg) tea.Cmd {
	j.acting--
	if msg.err != nil {
		j.failed(msg.err)
		m.setStatus(jiraErrText(msg.err), true)
		return nil
	}
	j.remote, j.remoteQ = msg.issues, msg.q
	if j.remote == nil {
		j.remote = []jira.Issue{}
	}
	j.search.SetValue("")
	j.section = jiraSecSearch
	*m.off("jira") = 0
	j.rebuild(m)
	return j.settleSel(m)
}

// clearAll drops the status filter, the search box and the search results.
func (j *jiraView) clearAll(m *Model) {
	j.filter = ""
	j.search.SetValue("")
	j.remote, j.remoteQ = nil, ""
	if j.section == jiraSecSearch {
		j.section = jiraSecStatus
		*m.off("jira") = 0
	}
	j.rebuild(m)
}

// barNote are the chips at the right end of the sections bar: the status
// filter, the sort and the search results; each one is a click.
func (j *jiraView) barNote(m *Model) string {
	s := m.st
	var chips []string
	if j.remote != nil {
		chips = append(chips, clickable("F", s.Accent.Render("⌕ "+trunc(j.remoteQ, 24)+" ✕")))
	}
	if j.filter != "" {
		chips = append(chips, clickable("F", s.Accent.Render("⚑ "+trunc(j.filterName(), 20)+" ✕")))
	} else {
		chips = append(chips, clickable("f", s.Muted.Render("⚑ "+T("all"))))
	}
	chips = append(chips, clickable("o", s.Muted.Render("↕ "+j.sortName())))
	return strings.Join(chips, "  ")
}

// readLines is the reader of the detail pane: description, subtasks, links and comments.
func (j *jiraView) readLines(m *Model, is jira.Issue, inner int) []string {
	s := m.st
	r, ok := j.read[is.Key]
	switch {
	case !ok:
		return []string{"", s.Muted.Render(spinGlyph + " " + T("reading the issue…"))}
	case r.err != "":
		return []string{"", s.Red.Render(trunc("✗ "+r.err, inner))}
	}
	d := r.d
	ls := []string{"", s.Accent.Render(T("Description"))}
	if strings.TrimSpace(d.Description) == "" {
		ls = append(ls, s.Muted.Render(T("No description.")))
	} else {
		ls = append(ls, m.readBody(d.Description, inner, "")...)
	}
	ls = append(ls, j.linkLines(m, T("Subtasks"), d.Subtasks, inner)...)
	ls = append(ls, j.linkLines(m, T("Linked issues"), d.Links, inner)...)
	ls = append(ls, "", s.Accent.Render(Tn(d.CommentTotal, "%d comment", "%d comments")))
	if d.CommentTotal == 0 {
		return append(ls, s.Muted.Render(T("No comments.")))
	}
	if n := d.CommentTotal - len(d.Comments); n > 0 {
		ls = append(ls, s.Muted.Render(Tf("%d earlier not shown", n)))
	}
	for _, c := range d.Comments {
		ls = append(ls, "", s.Bold.Render(trunc(c.Author, inner-8))+s.Muted.Render(" · "+ago(c.Created)))
		ls = append(ls, m.readBody(c.Body, inner-2, "  ")...)
	}
	return ls
}

func (j *jiraView) linkLines(m *Model, title string, links []jira.Link, inner int) []string {
	if len(links) == 0 {
		return nil
	}
	s := m.st
	ls := []string{"", s.Accent.Render(title)}
	for _, l := range links {
		rel := ""
		if l.Rel != "" && l.Rel != "subtask" {
			rel = l.Rel + " "
		}
		ls = append(ls, trunc(s.Muted.Render(rel)+s.Blue.Render(l.Key)+" "+l.Summary+s.Muted.Render(" · "+l.Status), inner))
	}
	return ls
}

// readBody wraps text from Jira to the pane, each line under indent, and
// dims what is code or quoted.
func (m Model) readBody(text string, w int, indent string) []string {
	s := m.st
	var out []string
	for _, l := range wrap(text, w, 0) {
		switch {
		case strings.HasPrefix(l, "│ "):
			l = s.Dim.Render(l)
		case strings.HasPrefix(l, "> "), strings.HasPrefix(l, "┃ "):
			l = s.Muted.Render(l)
		case strings.HasPrefix(l, "#"):
			l = s.Bold.Render(l)
		}
		out = append(out, indent+l)
	}
	return out
}
