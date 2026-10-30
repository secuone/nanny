package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/secuone/nanny/internal/gitwt"
	"github.com/secuone/nanny/internal/jira"
)

// The keys of the Jira view and what they ask Jira (or git) to do.

func (j *jiraView) key(m *Model, k tea.KeyMsg) tea.Cmd {
	if j.typed {
		return j.searchKey(m, k)
	}
	if !j.connected(m) {
		if k.String() == "enter" {
			m.openSetup(setupJira)
		}
		return nil
	}
	is := j.sel()
	switch k.String() {
	case "j", "down":
		return j.step(m, 1)
	case "k", "up":
		return j.step(m, -1)
	case "g", "home":
		j.cur.idx = 0
		j.settle(1)
		return j.settleDev(m)
	case "G", "end":
		j.cur.idx = len(j.rows) - 1
		j.settle(-1)
		return j.settleDev(m)
	case "/":
		j.typed = true
		j.search.Focus()
		return textinput.Blink
	case "f":
		j.filter = (j.filter + 1) % 3
		j.rebuild(m)
	case "r":
		j.at = j.at.Add(-jiraForceGap)
		return j.due(m, true)
	}
	if is == nil {
		return nil
	}
	switch k.String() {
	case "enter":
		return m.openURL(is.URL)
	case "y":
		return m.copy(is.Key, T("issue key"))
	case "Y":
		return m.copy(is.URL, T("link"))
	case "t":
		return j.transCmd(m, is)
	case "c":
		return j.commentPrompt(m, is)
	case "a":
		j.assignMenu(m, is)
	case "n":
		return j.work(m, is, false)
	case "b":
		return j.work(m, is, true)
	case "O":
		return j.openPR(m, is)
	case "s":
		if ag := j.agentOf(m, is.Key); ag != nil {
			return m.resume(ag, false)
		}
		m.setStatus(Tf("no agent on %s yet · n starts one", is.Key), false)
	case "v":
		if ag := j.agentOf(m, is.Key); ag != nil {
			return m.openViewer(ag.Path, false, ag.Title, ag, nil)
		}
		m.setStatus(Tf("no agent on %s yet · n starts one", is.Key), false)
	}
	return nil
}

func (j *jiraView) step(m *Model, d int) tea.Cmd {
	j.cur.idx = max(0, min(len(j.rows)-1, j.cur.idx+d))
	j.settle(d)
	return j.settleDev(m)
}

func (j *jiraView) searchKey(m *Model, k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		j.search.SetValue("")
		j.search.Blur()
		j.typed = false
		j.rebuild(m)
		return nil
	case "enter":
		j.search.Blur()
		j.typed = false
		return nil
	}
	before := j.search.Value()
	var cmd tea.Cmd
	j.search, cmd = j.search.Update(k)
	if j.search.Value() != before {
		j.rebuild(m)
	}
	return cmd
}

// act counts a request on its way, for the spinner.
func (j *jiraView) act() { j.acting++ }

func (j *jiraView) transCmd(m *Model, is *jira.Issue) tea.Cmd {
	j.act()
	cfg, key := m.cfg.Jira, is.Key
	return func() tea.Msg {
		c, err := jiraAPI(cfg)
		if err != nil {
			return jiraTransMsg{key: key, err: err}
		}
		ctx, cancel := jiraCtx()
		defer cancel()
		ts, err := c.Transitions(ctx, key)
		return jiraTransMsg{key: key, ts: ts, err: err}
	}
}

func (j *jiraView) transItems(msg jiraTransMsg) []menuItem {
	var items []menuItem
	for i, t := range msg.ts {
		if i >= 9 {
			break
		}
		label := t.Name
		if t.To != "" && t.To != t.Name {
			label += " → " + t.To
		}
		items = append(items, menuItem{label, strconv.Itoa(i + 1), j.doTransition(msg.key, t)})
	}
	return items
}

func (j *jiraView) doTransition(key string, t jira.Transition) func(m *Model) tea.Cmd {
	return func(m *Model) tea.Cmd {
		return j.write(m, Tf("%s moved to %s", key, firstOf(t.To, t.Name)), func(c jira.API) error {
			ctx, cancel := jiraCtx()
			defer cancel()
			return c.DoTransition(ctx, key, t.ID)
		})
	}
}

// write runs a change in Jira, then refreshes the list.
func (j *jiraView) write(m *Model, done string, fn func(c jira.API) error) tea.Cmd {
	j.act()
	cfg := m.cfg.Jira
	return func() tea.Msg {
		c, err := jiraAPI(cfg)
		if err == nil {
			err = fn(c)
		}
		return jiraDoneMsg{text: done, err: err, refresh: true}
	}
}

func (j *jiraView) commentPrompt(m *Model, is *jira.Issue) tea.Cmd {
	key := is.Key
	m.prompt(Tf("Comment on %s", key), T("It goes to Jira as a comment, in plain text."), T("your comment"),
		func(m *Model, v string) tea.Cmd {
			if strings.TrimSpace(v) == "" {
				return nil
			}
			return j.write(m, Tf("commented on %s", key), func(c jira.API) error {
				ctx, cancel := jiraCtx()
				defer cancel()
				return c.Comment(ctx, key, v)
			})
		})
	return textinput.Blink
}

func (j *jiraView) assignMenu(m *Model, is *jira.Issue) {
	key := is.Key
	me := func(m *Model) tea.Cmd {
		return j.write(m, Tf("%s assigned to you", key), func(c jira.API) error {
			ctx, cancel := jiraCtx()
			defer cancel()
			u, err := c.Myself(ctx)
			if err != nil {
				return err
			}
			return c.Assign(ctx, key, u.AccountID)
		})
	}
	none := func(m *Model) tea.Cmd {
		return j.write(m, Tf("%s unassigned", key), func(c jira.API) error {
			ctx, cancel := jiraCtx()
			defer cancel()
			return c.Assign(ctx, key, "")
		})
	}
	m.openMenu(Tf("Assign %s", key), m.sideW()+2, bodyY+3, []menuItem{
		{T("Assign to me"), "1", me},
		{T("Unassigned"), "2", none},
		{T("Search…"), "3", func(m *Model) tea.Cmd { return j.userPrompt(m, key) }},
	})
}

func (j *jiraView) userPrompt(m *Model, key string) tea.Cmd {
	m.prompt(Tf("Assign %s", key), T("Part of a name or an email."), T("name"), func(m *Model, v string) tea.Cmd {
		if strings.TrimSpace(v) == "" {
			return nil
		}
		j.act()
		cfg := m.cfg.Jira
		return func() tea.Msg {
			c, err := jiraAPI(cfg)
			if err != nil {
				return jiraUsersMsg{key: key, err: err}
			}
			ctx, cancel := jiraCtx()
			defer cancel()
			us, err := c.AssignableUsers(ctx, key, v)
			return jiraUsersMsg{key: key, users: us, err: err}
		}
	})
	return textinput.Blink
}

func (j *jiraView) userItems(m *Model, msg jiraUsersMsg) []menuItem {
	var items []menuItem
	for i, u := range msg.users {
		if i >= 9 {
			break
		}
		u := u
		items = append(items, menuItem{u.Name, strconv.Itoa(i + 1), func(m *Model) tea.Cmd {
			return j.write(m, Tf("%s assigned to %s", msg.key, u.Name), func(c jira.API) error {
				ctx, cancel := jiraCtx()
				defer cancel()
				return c.Assign(ctx, msg.key, u.AccountID)
			})
		}})
	}
	return items
}

func (j *jiraView) openPR(m *Model, is *jira.Issue) tea.Cmd {
	if p, ok := j.prOf(m, is.Key); ok && p.URL != "" {
		return m.openURL(p.URL)
	}
	m.setStatus(Tf("no PR for %s yet", is.Key), false)
	return nil
}

// ---------------------------------------------------------------- starting work

// branchFor is the branch of an issue by the template: {key}-{slug} unless set.
func (j *jiraView) branchFor(m *Model, is *jira.Issue) string {
	slug := branchName(is.Summary, "")
	b := strings.NewReplacer("{key}", is.Key, "{slug}", slug).Replace(m.cfg.Jira.Branch())
	return strings.Trim(b, "-./")
}

// workDir is where work on the issue starts: the repo of the newest session
// that already talks about its project, else the folder the form proposes.
func (j *jiraView) workDir(m *Model, is *jira.Issue) string {
	prefix := is.Project + "-"
	var cwd string
	var newest int64
	for _, s := range m.sessions {
		if s.Bot() || s.Cwd == "" || s.Modified.Unix() < newest {
			continue
		}
		if strings.Contains(strings.ToUpper(s.Branch+" "+s.Worktree+" "+s.Title), prefix) {
			cwd, newest = s.Cwd, s.Modified.Unix()
		}
	}
	for _, r := range m.repos {
		for _, w := range r.Worktrees {
			if cwd != "" && inDir(cwd, w.Path) {
				return r.Root
			}
		}
	}
	return cwd
}

// work starts on the issue: the new-task form with the branch, a worktree and
// the issue in the prompt; or, with only, just the branch as a worktree.
func (j *jiraView) work(m *Model, is *jira.Issue, only bool) tea.Cmd {
	branch, dir := j.branchFor(m, is), j.workDir(m, is)
	if !only {
		prompt := fmt.Sprintf("Work on %s: %s (%s)", is.Key, is.Summary, is.URL)
		return m.openNewTaskWith(dir, branch, prompt, true)
	}
	if dir == "" {
		if sp := m.space(); sp != nil {
			dir = sp.Dir()
		}
	}
	if dir == "" || !branchRe.MatchString(branch) {
		m.setStatus(Tf("no repository for %s yet · n picks a folder", is.Key), true)
		return nil
	}
	m.setStatus(Tf("creating worktree %s…", branch), false)
	return func() tea.Msg {
		root := gitwt.RepoRoot(dir)
		if root == "" {
			return statusMsg{T("not a git repository: ") + tilde(dir), true}
		}
		path, err := gitwt.Add(root, branch, gitwt.Head(dir))
		if err != nil {
			return statusMsg{err.Error(), true}
		}
		return wtChangedMsg{T("created ") + tilde(path)}
	}
}
