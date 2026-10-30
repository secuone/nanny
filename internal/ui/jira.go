package ui

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/secuone/nanny/internal/claude"
	"github.com/secuone/nanny/internal/config"
	"github.com/secuone/nanny/internal/gitwt"
	"github.com/secuone/nanny/internal/jira"
	"github.com/secuone/nanny/internal/secret"
)

// The Jira view (E): your issues by status, by sprint and what you closed in
// the last week, each one linked to its PR and its agent. Nothing is asked
// unless the view is on and Jira is set up; the token is read inside the
// commands that call Jira, never in Update.

const (
	jiraSecStatus = iota
	jiraSecSprint
	jiraSecDone

	jiraOffEvery  = 10 * time.Minute // fetching for the sidebar only
	jiraForceGap  = 5 * time.Second  // opening the view again in a row doesn't ask again
	jiraDevSettle = 500 * time.Millisecond
	jiraTimeout   = 25 * time.Second
	jiraDoneDays  = 7
)

// newJiraClient makes the client; tests put a fake in its place.
var newJiraClient = func(url, email, token string) jira.API { return jira.New(url, email, token) }

var errNoToken = errors.New("no Jira token")

// jiraAPI reads the token and makes the client. Call it only from a command.
func jiraAPI(cfg config.Jira) (jira.API, error) {
	tok, err := secret.Get(secret.JiraService, cfg.Account())
	if err != nil || tok == "" {
		return nil, errNoToken
	}
	return newJiraClient(cfg.BaseURL(), strings.TrimSpace(cfg.Email), tok), nil
}

func jiraCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), jiraTimeout)
}

type jiraIssuesMsg struct {
	issues      []jira.Issue
	sprintField string
	err         error
}
type jiraTransMsg struct {
	key string
	ts  []jira.Transition
	err error
}
type jiraDevMsg struct {
	key string
	prs []jira.DevPR
	err error
}
type jiraDoneMsg struct {
	text    string
	err     error
	refresh bool
}
type jiraUsersMsg struct {
	key   string
	users []jira.User
	err   error
}
type jiraDevWantMsg struct{ key string }

func (jiraIssuesMsg) bgMsg()  {}
func (jiraTransMsg) bgMsg()   {}
func (jiraDevMsg) bgMsg()     {}
func (jiraDoneMsg) bgMsg()    {}
func (jiraUsersMsg) bgMsg()   {}
func (jiraDevWantMsg) bgMsg() {}

type jiraRow struct {
	head  string
	issue *jira.Issue
}

type jiraView struct {
	featBase
	section  int
	filter   int // 0 all, 1 in progress, 2 to do
	cur      cursor
	search   textinput.Model
	typed    bool // the search box has the keys
	issues   []jira.Issue
	rows     []jiraRow
	at       time.Time
	loading  bool
	acting   int
	err      string
	unauth   bool
	noToken  bool
	retryAt  time.Time
	dev      map[string][]jira.DevPR
	devAsked map[string]bool
	devOff   bool
	wantKey  string
}

func newJiraView() *jiraView {
	in := newInput()
	in.Prompt = "/ "
	in.Placeholder = "…"
	return &jiraView{search: in, dev: map[string][]jira.DevPR{}, devAsked: map[string]bool{}}
}

func init() {
	jiraBrowseURL = func(m *Model, key string) string {
		if u := m.cfg.Jira.BaseURL(); u != "" {
			return u + "/browse/" + key
		}
		return ""
	}
	jiraClosedSince = func(m *Model, since time.Time) (int, bool) {
		j := m.jira
		if j == nil || j.at.IsZero() {
			return 0, false
		}
		n := 0
		for _, is := range j.issues {
			if is.StatusCat == "done" && closedAt(is).After(since) {
				n++
			}
		}
		return n, true
	}
}

func closedAt(is jira.Issue) time.Time {
	if !is.Resolved.IsZero() {
		return is.Resolved
	}
	return is.Updated
}

// ---------------------------------------------------------------- fetching

func (j *jiraView) setUp(m *Model) bool { return m.cfg.Jira.BaseURL() != "" }

func (j *jiraView) connected(m *Model) bool { return j.setUp(m) && !j.noToken }

func (j *jiraView) busy() bool { return j.loading || j.acting > 0 }

func (j *jiraView) due(m *Model, force bool) tea.Cmd {
	if !m.viewOn(tabJira) || !j.setUp(m) || j.loading || time.Now().Before(j.retryAt) {
		return nil
	}
	every := m.cfg.Jira.Refresh()
	if !m.onTab(tabJira) {
		if !force && !(m.cfg.On("sidebar.jira") && m.sidebarOn()) {
			return nil
		}
		every = jiraOffEvery
	}
	since := time.Since(j.at)
	if force {
		if since < jiraForceGap {
			return nil
		}
	} else if since < every || j.noToken || j.unauth {
		return nil
	}
	j.loading = true
	cfg := m.cfg.Jira
	return func() tea.Msg {
		c, err := jiraAPI(cfg)
		if err != nil {
			return jiraIssuesMsg{err: err}
		}
		ctx, cancel := jiraCtx()
		defer cancel()
		field := cfg.SprintField
		if field == "" {
			field, _ = c.SprintField(ctx) // a site without sprints just has none
		}
		jql := strings.TrimSpace(cfg.JQL)
		if jql == "" {
			jql = jira.DefaultJQL
		}
		issues, err := c.Search(ctx, jql, field)
		return jiraIssuesMsg{issues: issues, sprintField: field, err: err}
	}
}

// jiraErrText says what went wrong in a line for the view.
func jiraErrText(err error) string {
	var rl *jira.RateLimitError
	switch {
	case errors.Is(err, jira.ErrUnauthorized):
		return T("token rejected → Settings → Connections")
	case errors.As(err, &rl):
		return Tf("Jira asks to wait %s", rl.After.Round(time.Second))
	}
	return err.Error()
}

// failed notes an error of a request: a 401 stops the fetching until you fix it,
// a 429 waits what Jira asked for.
func (j *jiraView) failed(err error) {
	var rl *jira.RateLimitError
	switch {
	case errors.Is(err, jira.ErrUnauthorized):
		j.unauth = true
	case errors.Is(err, errNoToken):
		j.noToken = true
	case errors.As(err, &rl):
		j.retryAt = time.Now().Add(rl.After)
	}
}

func (j *jiraView) update(m *Model, msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case jiraIssuesMsg:
		return j.issuesIn(m, msg), true
	case jiraTransMsg:
		j.acting--
		if msg.err != nil {
			j.failed(msg.err)
			m.setStatus(jiraErrText(msg.err), true)
			return nil, true
		}
		m.openMenu(Tf("Transition · %s", msg.key), m.sideW()+2, bodyY+3, j.transItems(msg))
		return nil, true
	case jiraUsersMsg:
		j.acting--
		if msg.err != nil {
			j.failed(msg.err)
			m.setStatus(jiraErrText(msg.err), true)
		} else if len(msg.users) == 0 {
			m.setStatus(T("nobody found"), false)
		} else {
			m.openMenu(Tf("Assign %s", msg.key), m.sideW()+2, bodyY+3, j.userItems(m, msg))
		}
		return nil, true
	case jiraDevMsg:
		j.acting--
		if msg.err != nil {
			j.devOff = true // an undocumented API: no error to show
		} else {
			j.dev[msg.key] = msg.prs
		}
		return nil, true
	case jiraDevWantMsg:
		return j.devFetch(m, msg.key), true
	case jiraDoneMsg:
		j.acting--
		if msg.err != nil {
			j.failed(msg.err)
			m.setStatus(jiraErrText(msg.err), true)
			return nil, true
		}
		m.setStatus(msg.text, false)
		if msg.refresh {
			j.at = time.Time{}
			return j.due(m, true), true
		}
		return nil, true
	}
	return nil, false
}

func (j *jiraView) issuesIn(m *Model, msg jiraIssuesMsg) tea.Cmd {
	j.loading = false
	j.at = time.Now()
	if msg.err != nil {
		j.failed(msg.err)
		j.err = ""
		if !errors.Is(msg.err, errNoToken) {
			j.err = jiraErrText(msg.err)
		}
		return nil
	}
	j.err, j.unauth, j.noToken = "", false, false
	if msg.sprintField != "" && msg.sprintField != m.cfg.Jira.SprintField {
		m.cfg.Jira.SprintField = msg.sprintField
		_ = config.Save(m.cfg)
	}
	if reflect.DeepEqual(j.issues, msg.issues) {
		m.vs.noop = true
		return nil
	}
	j.issues = msg.issues
	j.rebuild(m)
	return nil
}

// ---------------------------------------------------------------- rows

func (j *jiraView) hidden(is jira.Issue) bool {
	if q := strings.TrimSpace(j.search.Value()); q != "" && !is.Matches(q) {
		return true
	}
	return false
}

func (j *jiraView) shown(is jira.Issue) bool {
	switch j.filter {
	case 1:
		return is.StatusCat == "indeterminate"
	case 2:
		return is.StatusCat == "new"
	}
	return true
}

func doneRecently(is jira.Issue) bool {
	return is.StatusCat == "done" && time.Since(closedAt(is)) < jiraDoneDays*24*time.Hour
}

// list is the issues of a section, filtered, newest first.
func (j *jiraView) list(sec int) []jira.Issue {
	var out []jira.Issue
	for _, is := range j.issues {
		if j.hidden(is) {
			continue
		}
		switch {
		case sec == jiraSecDone && doneRecently(is):
		case sec != jiraSecDone && is.StatusCat != "done" && j.shown(is):
		default:
			continue
		}
		out = append(out, is)
	}
	sort.SliceStable(out, func(a, b int) bool {
		if sec == jiraSecDone {
			return closedAt(out[a]).After(closedAt(out[b]))
		}
		return out[a].Updated.After(out[b].Updated)
	})
	return out
}

var catRank = map[string]int{"indeterminate": 0, "new": 1, "done": 3}
var sprintRank = map[string]int{"active": 0, "future": 1, "closed": 2}

func (j *jiraView) rebuild(m *Model) {
	keep := ""
	if is := j.sel(); is != nil {
		keep = is.Key
	}
	j.rows = j.rows[:0]
	list := j.list(j.section)
	if j.section == jiraSecDone {
		for i := range list {
			j.rows = append(j.rows, jiraRow{issue: &list[i]})
		}
	} else {
		j.group(list)
	}
	j.cur.idx = 0
	for i, r := range j.rows {
		if r.issue != nil && r.issue.Key == keep {
			j.cur.idx = i
		}
	}
	j.settle(1)
	j.cur.clamp(len(j.rows), 1<<30)
}

// group puts the issues under a title each: their status or their sprint.
func (j *jiraView) group(list []jira.Issue) {
	type grp struct {
		name  string
		rank  int
		items []jira.Issue
	}
	var gs []*grp
	by := map[string]*grp{}
	for _, is := range list {
		name, rank := is.Status, catRank[is.StatusCat]
		if j.section == jiraSecSprint {
			name, rank = is.Sprint, 3
			if name != "" {
				rank = sprintRank[is.SprintState]
			}
		}
		g := by[name]
		if g == nil {
			g = &grp{name: name, rank: rank}
			by[name] = g
			gs = append(gs, g)
		}
		g.items = append(g.items, is)
	}
	sort.SliceStable(gs, func(a, b int) bool {
		if gs[a].rank != gs[b].rank {
			return gs[a].rank < gs[b].rank
		}
		return gs[a].name < gs[b].name
	})
	for _, g := range gs {
		name := g.name
		if name == "" {
			name = T("No sprint")
		}
		j.rows = append(j.rows, jiraRow{head: fmt.Sprintf("%s · %d", name, len(g.items))})
		for i := range g.items {
			j.rows = append(j.rows, jiraRow{issue: &g.items[i]})
		}
	}
}

// settle moves the cursor off a title, in direction d (else the other way).
func (j *jiraView) settle(d int) {
	n := len(j.rows)
	for _, dir := range []int{d, -d} {
		for i := j.cur.idx; i >= 0 && i < n; i += dir {
			if j.rows[i].issue != nil {
				j.cur.idx = i
				return
			}
		}
	}
}

func (j *jiraView) sel() *jira.Issue {
	if j.cur.idx < 0 || j.cur.idx >= len(j.rows) {
		return nil
	}
	return j.rows[j.cur.idx].issue
}

func (j *jiraView) selKey(m *Model) string {
	if is := j.sel(); is != nil {
		return is.Key
	}
	return ""
}

func (j *jiraView) secs(m *Model) []secDef {
	if !j.connected(m) || len(j.issues) == 0 {
		return nil
	}
	return []secDef{
		{M("By status"), len(j.list(jiraSecStatus)), true},
		{M("By sprint"), len(j.list(jiraSecSprint)), true},
		{M("Done 7d"), len(j.list(jiraSecDone)), true},
	}
}

func (j *jiraView) sec() int { return j.section }

func (j *jiraView) setSec(m *Model, i int) tea.Cmd {
	j.section = i
	*m.off("jira") = 0
	j.rebuild(m)
	return nil
}

func (j *jiraView) click(m *Model, row int) tea.Cmd {
	if row >= 0 && row < len(j.rows) && j.rows[row].issue != nil {
		j.cur.idx = row
		return j.settleDev(m)
	}
	return nil
}

func (j *jiraView) typing() bool { return j.typed }

// ---------------------------------------------------------------- links

// keysIn are the issue keys a text names (a branch, a title).
func keysIn(s string) []string { return jiraKeyRe.FindAllString(strings.ToUpper(s), -1) }

func mentions(s, key string) bool {
	for _, k := range keysIn(s) {
		if k == key {
			return true
		}
	}
	return false
}

// prOf is the open PR of the issue: a branch of the PRs nanny already knows
// that has its key, else what Jira's development panel says.
func (j *jiraView) prOf(m *Model, key string) (gitwt.PR, bool) {
	roots := make([]string, 0, len(m.prs))
	for r := range m.prs {
		roots = append(roots, r)
	}
	sort.Strings(roots)
	for _, r := range roots {
		branches := make([]string, 0, len(m.prs[r]))
		for b := range m.prs[r] {
			branches = append(branches, b)
		}
		sort.Strings(branches)
		for _, b := range branches {
			if mentions(b, key) {
				return m.prs[r][b], true
			}
		}
	}
	for _, p := range j.dev[key] {
		if p.URL != "" {
			return gitwt.PR{Title: p.Name, URL: p.URL, Branch: p.Branch}, true
		}
	}
	return gitwt.PR{}, false
}

// agentOf is the agent working on the issue: a session whose branch,
// worktree or title has the key; a live one first, else the newest.
func (j *jiraView) agentOf(m *Model, key string) *claude.Session {
	var best *claude.Session
	for _, s := range m.sessions {
		if !mentions(s.Branch+" "+s.Worktree+" "+s.Title, key) {
			continue
		}
		if best == nil || s.Live != nil && best.Live == nil || (s.Live != nil) == (best.Live != nil) && s.Modified.After(best.Modified) {
			best = s
		}
	}
	return best
}

// settleDev asks Jira's development panel for the PR of the selected issue
// once the selection stops moving, and only when nanny doesn't know it.
func (j *jiraView) settleDev(m *Model) tea.Cmd {
	is := j.sel()
	if is == nil || j.devOff || j.devAsked[is.Key] || is.ID == "" || j.wantKey == is.Key || !j.connected(m) {
		return nil
	}
	if _, ok := j.prOf(m, is.Key); ok {
		return nil
	}
	j.wantKey = is.Key
	key := is.Key
	return tea.Tick(jiraDevSettle, func(time.Time) tea.Msg { return jiraDevWantMsg{key} })
}

func (j *jiraView) devFetch(m *Model, key string) tea.Cmd {
	j.wantKey = ""
	is := j.sel()
	if is == nil || is.Key != key || j.devOff || j.devAsked[key] {
		return nil
	}
	j.devAsked[key] = true
	j.acting++
	cfg, id := m.cfg.Jira, is.ID
	return func() tea.Msg {
		c, err := jiraAPI(cfg)
		if err != nil {
			return jiraDevMsg{key: key, err: err}
		}
		ctx, cancel := jiraCtx()
		defer cancel()
		prs, err := c.DevPRs(ctx, id)
		return jiraDevMsg{key: key, prs: prs, err: err}
	}
}

// jiraSide is the line the sidebar shows under a branch: KEY · status. The key
// comes from the branch, so it costs nothing; the status from what Jira said last.
func (m Model) jiraSide(branch string, width int) (text, url string) {
	if !m.cfg.On("sidebar.jira") || !m.cfg.On("view.jira") || m.jira == nil || m.cfg.Jira.BaseURL() == "" {
		return "", ""
	}
	ks := keysIn(branch)
	if len(ks) == 0 {
		return "", ""
	}
	key := ks[0]
	out := m.st.Blue.Render(key)
	for _, is := range m.jira.issues {
		if is.Key == key {
			out += m.st.Muted.Render(" · " + trunc(is.Status, max(4, width-len(key)-3)))
			break
		}
	}
	return out, m.cfg.Jira.BaseURL() + "/browse/" + key
}

// ---------------------------------------------------------------- drawing

func (j *jiraView) catStyle(m *Model, cat string) func(...string) string {
	s := m.st
	switch cat {
	case "indeterminate":
		return s.Blue.Render
	case "done":
		return s.Green.Render
	}
	return s.Muted.Render
}

func (j *jiraView) filterName() string {
	return [3]string{T("all"), T("in progress"), T("to do")}[j.filter]
}

func (j *jiraView) view(m *Model, w, h int) string {
	s := m.st
	inner := w - 4
	title := T("Jira")
	if !j.connected(m) {
		msg := s.emptyState(T("Connect Jira to see your issues, move them and start work on them from here."),
			[][2]string{{"⏎", T("Connect Jira")}}, inner)
		if j.noToken && j.setUp(m) {
			msg = s.emptyState(T("No token for this Jira yet."), [][2]string{{"⏎", T("Connect Jira")}}, inner)
		}
		return s.panel(title, msg, w, h, !m.focusR)
	}
	var head []string
	if j.typed || strings.TrimSpace(j.search.Value()) != "" {
		head = append(head, " "+j.search.View())
	}
	if j.err != "" {
		head = append(head, " "+s.Red.Render(trunc("✗ "+j.err, inner-2)))
	}
	if len(j.rows) == 0 {
		msg := T("No issues here.")
		switch {
		case j.loading && j.at.IsZero():
			msg = spinGlyph + " " + T("asking Jira…")
		case len(j.issues) == 0 && !j.at.IsZero():
			msg = T("Nothing assigned to you.")
		}
		return s.panel(title, strings.Join(append(head, s.emptyState(msg, nil, inner)), "\n"), w, h, !m.focusR)
	}
	title += " · " + Tn(len(j.list(j.section)), "%d issue", "%d issues")
	if j.filter > 0 {
		title += " · " + j.filterName()
	}
	if j.loading {
		title += " " + spinGlyph
	}
	pw := 0
	cells := make([]string, len(j.rows))
	for i, r := range j.rows {
		if r.issue != nil {
			cells[i] = j.cells(m, *r.issue)
			pw = max(pw, lipglossW(cells[i]))
		}
	}
	blocks := make([][]string, len(j.rows))
	for i, r := range j.rows {
		if r.issue == nil {
			blocks[i] = []string{s.Accent.Render("▍" + trunc(r.head, inner-1))}
			if i > 0 {
				blocks[i] = append([]string{""}, blocks[i]...)
			}
			continue
		}
		line := j.line(m, *r.issue, cells[i], pw, inner)
		if i == j.cur.idx {
			st := s.RowSel
			if m.focusR {
				st = s.RowSelBlur
			}
			line = s.Sel(st, fit(line, inner))
		}
		blocks[i] = []string{line}
	}
	list := m.listBlocks(blocks, j.cur.idx, "jira", h-2-len(head), bodyY+1+len(head))
	return s.panel(title, strings.Join(append(head, list), "\n"), w, h, !m.focusR)
}

// cells are the right-hand marks of a row: agent, PR, status.
func (j *jiraView) cells(m *Model, is jira.Issue) string {
	s := m.st
	var parts []string
	if ag := j.agentOf(m, is.Key); ag != nil {
		if ag.Live != nil {
			parts = append(parts, s.Green.Render("●"))
		} else {
			parts = append(parts, s.Dim.Render("○"))
		}
	}
	if p, ok := j.prOf(m, is.Key); ok {
		if p.Number > 0 {
			parts = append(parts, m.prBadge(p))
		} else {
			parts = append(parts, s.Accent.Render("PR"))
		}
	}
	parts = append(parts, j.catStyle(m, is.StatusCat)(trunc(is.Status, 14)))
	return strings.Join(parts, " ")
}

func (j *jiraView) line(m *Model, is jira.Issue, cell string, pw, w int) string {
	s := m.st
	right := strings.Repeat(" ", pw-lipglossW(cell)) + cell
	left := " " + s.Accent.Render(is.Key) + " " + s.Text.Render(is.Summary)
	return fit(left, w-lipglossW(right)-1) + " " + right
}

func (j *jiraView) detail(m *Model, w, h int) string {
	s := m.st
	is := j.sel()
	if is == nil || !j.connected(m) {
		return s.panel("", "", w, h, m.focusR)
	}
	inner := w - 4
	ls := []string{s.Accent.Render(is.Key) + " " + s.Muted.Render(is.Type)}
	for _, l := range wrap(is.Summary, inner, 4) {
		ls = append(ls, s.Bold.Render(l))
	}
	ls = append(ls, "", s.kv(T("status"), j.catStyle(m, is.StatusCat)(is.Status), inner))
	if is.Priority != "" {
		ls = append(ls, s.kv(T("priority"), is.Priority, inner))
	}
	if is.Sprint != "" {
		ls = append(ls, s.kv(T("sprint"), is.Sprint+" · "+is.SprintState, inner))
	}
	if len(is.Labels) > 0 {
		ls = append(ls, s.kv(T("labels"), strings.Join(is.Labels, ", "), inner))
	}
	if !is.Updated.IsZero() {
		ls = append(ls, s.kv(T("updated"), ago(is.Updated), inner))
	}
	if doneRecently(*is) {
		ls = append(ls, s.kv(T("closed"), ago(closedAt(*is)), inner))
	}
	ls = append(ls, s.kv("URL", s.Muted.Render(trunc(is.URL, inner-10)), inner))
	if p, ok := j.prOf(m, is.Key); ok {
		line := m.prLine(p)
		if p.Number == 0 {
			line = s.Accent.Render("PR") + s.Muted.Render(" "+trunc(p.URL, inner-16))
		}
		ls = append(ls, s.kv("PR", line, inner+30))
		if p.Title != "" {
			ls = append(ls, s.kv("", s.Muted.Render(trunc(p.Title, inner-10)), inner))
		}
	}
	if ag := j.agentOf(m, is.Key); ag != nil {
		ls = append(ls, "", s.Accent.Render(T("Agent")))
		ls = append(ls, m.sessionLines(ag, inner)[0])
	} else {
		ls = append(ls, "", s.Muted.Render(T("No agent yet · n starts one on it")))
	}
	ls = append(ls, "", s.hintsFit(j.footer(m), inner))
	return s.panel(T("Issue"), m.detailScroll(ls, h), w, h, m.focusR)
}

func (j *jiraView) footer(m *Model) [][2]string {
	if !j.connected(m) {
		return [][2]string{{"⏎", T("Connect Jira")}}
	}
	return [][2]string{{"⏎", T("open")}, {"n", T("start work")}, {"t", T("transition")}, {"c", T("comment")}, {"a", T("assign")},
		{"/", T("search")}, {"f", T("filter")}, {"y", T("copy")}, {"O", T("PR")}, {"s", T("agent")}}
}

func (j *jiraView) help() []helpSec {
	return []helpSec{{T("Jira"), [][2]string{
		{"E  ← →", T("your issues · by status, by sprint, done in the last 7 days")},
		{"/  f", T("search · filter: all, in progress, to do")},
		{"⏎  y Y", T("open in the browser · copy the key · copy the link")},
		{"t  c  a", T("move to another status · comment · assign")},
		{"n  b", T("start work: branch or worktree and a Claude task with the issue · only the branch")},
		{"O  s  v", T("open its PR · go to its agent · read its conversation")},
	}}}
}

func (j *jiraView) palette(m *Model) []paletteEntry {
	es := []paletteEntry{{M("action"), T("Refresh Jira"), "", func(m *Model) tea.Cmd { j.at = time.Time{}; return j.due(m, true) }}}
	if !j.connected(m) {
		es = append(es, paletteEntry{M("action"), T("Connect Jira"), "", func(m *Model) tea.Cmd { m.openSetup(setupJira); return nil }})
	}
	return es
}
