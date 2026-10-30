package ui

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/secuone/nanny/internal/config"
	"github.com/secuone/nanny/internal/gh"
	"github.com/secuone/nanny/internal/gitwt"
)

// The Git & PRs view (3): the branch of each space, your pull requests, the reviews waiting for you, the
// worktrees, the local branches and your week. GitHub is asked through gh,
// only from due(), only for the section on screen (and the reviews badge),
// and not at all once gh is missing or has no session, until r.

// The sections of the view, in the order of its bar.
const (
	gsHere = iota
	gsPRs
	gsReviews
	gsWT
	gsBranches
	gsProfile
	gsN
)

var gitSecs = [gsN]struct{ name, id string }{
	{M("Overview"), "git.here"}, {M("My PRs"), "git.prs"}, {M("Reviews"), "git.reviews"}, {M("Worktrees"), "git.worktrees"},
	{M("Branches"), "git.branches"}, {M("Profile"), "git.profile"},
}

const (
	ghEvery = 90 * time.Second // the section on screen
	ghRetry = 10 * time.Second // a PR whose mergeability GitHub is still working out
	ghLimit = 40 * time.Second // one call of gh
)

type gitView struct {
	featBase
	cur    int
	ov     cursor // the row of the Overview
	auto   bool   // the section follows gh: My PRs, or Worktrees when gh can't be used
	login  string
	noSess bool // gh is missing or has no session: nothing is asked until r
	noGH   bool // …because it is not installed
	prs    prList
	rev    prList
	br     brList
	prof   [2]profData // this week, this month
	month  bool
	local  map[string]string // owner/repo → the clone on this machine
	locSig string
	doing  bool // a merge or a checkout is on its way
	screen bool // the view was on screen at the last tick
}

func newGitView() *gitView { return &gitView{cur: gsPRs, auto: true} }

// prList is a list of pull requests: yours, or the ones waiting for you.
type prList struct {
	items  []gh.PR
	cur    cursor
	at     time.Time
	busy   bool
	loaded bool
	err    error
}

func (l *prList) sel() (gh.PR, bool) {
	if l.cur.idx < 0 || l.cur.idx >= len(l.items) {
		return gh.PR{}, false
	}
	return l.items[l.cur.idx], true
}

func (l *prList) due(force bool, every time.Duration) bool {
	return !l.busy && (force || time.Since(l.at) >= every)
}

func (l *prList) unknown() bool {
	for _, p := range l.items {
		if p.Unknown() {
			return true
		}
	}
	return false
}

// messages: every one of them comes back from the background
type (
	ghPRsMsg struct {
		prs   []gh.PR
		login string
		err   error
	}
	ghReviewsMsg ghPRsMsg
	ghProfileMsg struct {
		p     gh.Profile
		month bool
		login string
		err   error
	}
	branchesMsg struct {
		rows  []brRow
		sig   string
		login string
		err   error
	}
	ghDoneMsg struct {
		text    string
		err     error
		refresh bool
	}
	ghLocalMsg  struct{ local map[string]string }
	ghReviewMsg struct {
		pr  gh.PR
		dir string
		err error
	}
	branchDoneMsg struct {
		text string
		err  error
	}
)

func (ghPRsMsg) bgMsg()      {}
func (ghReviewsMsg) bgMsg()  {}
func (ghProfileMsg) bgMsg()  {}
func (branchesMsg) bgMsg()   {}
func (ghDoneMsg) bgMsg()     {}
func (ghLocalMsg) bgMsg()    {}
func (ghReviewMsg) bgMsg()   {}
func (branchDoneMsg) bgMsg() {}

func ghCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), ghLimit)
}

func (g *gitView) on(m *Model, i int) bool { return m.cfg.On(gitSecs[i].id) }

// shown says whether the view is on screen.
func (g *gitView) shown(m *Model) bool { return m.onTab(tabGit) && m.viewOn(tabGit) }

// fix keeps the section in use one that is on, and follows gh while nobody picked one.
func (g *gitView) fix(m *Model) {
	order := []int{gsPRs, gsReviews, gsHere, gsWT, gsBranches, gsProfile}
	switch {
	case m.inSpace():
		order = []int{gsHere, gsPRs, gsReviews, gsWT, gsBranches, gsProfile}
	case g.noSess:
		order = []int{gsHere, gsWT, gsBranches, gsPRs, gsReviews, gsProfile}
	}
	if !g.auto && g.on(m, g.cur) {
		return
	}
	for _, i := range order {
		if g.on(m, i) {
			g.cur = i
			return
		}
	}
	g.cur = gsWT
}

func (g *gitView) secs(m *Model) []secDef {
	g.fix(m)
	out := make([]secDef, gsN)
	for i := range out {
		n := 0
		switch i {
		case gsPRs:
			n = len(g.prs.items)
		case gsReviews:
			n = len(g.rev.items)
		case gsBranches:
			n = len(g.br.rows)
		}
		out[i] = secDef{gitSecs[i].name, n, g.on(m, i)}
	}
	return out
}

func (g *gitView) sec() int { return g.cur }

func (g *gitView) setSec(m *Model, i int) tea.Cmd {
	if i < 0 || i >= gsN || !g.on(m, i) {
		return nil
	}
	g.cur, g.auto = i, false
	return g.due(m, true)
}

func (g *gitView) view(m *Model, w, h int) string {
	g.fix(m)
	switch g.cur {
	case gsHere:
		return g.viewOverview(m, w, h)
	case gsPRs:
		return g.viewPRs(m, &g.prs, T("My PRs"), false, w, h)
	case gsReviews:
		return g.viewPRs(m, &g.rev, T("Reviews"), true, w, h)
	case gsBranches:
		return g.viewBranches(m, w, h)
	case gsProfile:
		return g.viewProfile(m, w, h)
	}
	return m.wt.view(m, w, h)
}

func (g *gitView) detail(m *Model, w, h int) string {
	g.fix(m)
	switch g.cur {
	case gsHere:
		return g.detailOverview(m, w, h)
	case gsPRs:
		return g.detailPR(m, &g.prs, false, w, h)
	case gsReviews:
		return g.detailPR(m, &g.rev, true, w, h)
	case gsBranches:
		return g.detailBranch(m, w, h)
	case gsProfile:
		return g.detailProfile(m, w, h)
	}
	return m.wt.detail(m, w, h)
}

func (g *gitView) key(m *Model, k tea.KeyPressMsg) tea.Cmd {
	g.fix(m)
	switch g.cur {
	case gsHere:
		return g.keyOverview(m, k)
	case gsPRs:
		return g.keyPR(m, k, &g.prs, false)
	case gsReviews:
		return g.keyPR(m, k, &g.rev, true)
	case gsBranches:
		return g.keyBranch(m, k)
	case gsProfile:
		return g.keyProfile(m, k)
	}
	return m.wt.key(m, k)
}

func (g *gitView) click(m *Model, row int) tea.Cmd {
	var c *cursor
	n := 0
	switch g.cur {
	case gsHere:
		c, n = &g.ov, len(g.ovRows(m))
	case gsPRs:
		c, n = &g.prs.cur, len(g.prs.items)
	case gsReviews:
		c, n = &g.rev.cur, len(g.rev.items)
	case gsBranches:
		c, n = &g.br.cur, len(g.br.rows)
	case gsWT:
		c, n = &m.wt.cur, len(m.wt.rows)
	}
	if c != nil && row >= 0 && row < n {
		c.idx = row
	}
	return nil
}

// moveKey moves a cursor with the keys every list takes; false = not one of them.
func moveKey(c *cursor, n, h int, k string) bool {
	switch k {
	case "j", "down":
		c.move(1, n, h)
	case "k", "up":
		c.move(-1, n, h)
	case "g", "home":
		c.idx = 0
		c.clamp(n, h)
	case "G", "end":
		c.idx = n - 1
		c.clamp(n, h)
	default:
		return false
	}
	return true
}

func (g *gitView) footer(m *Model) [][2]string {
	g.fix(m)
	switch g.cur {
	case gsHere:
		return [][2]string{{"⏎", T("open")}, {"O", T("open PR")}, {"C", T("changes")}, {"n", T("new claude")}, {"w", T("worktree")}, {"r", T("refresh")}}
	case gsPRs:
		return [][2]string{{"⏎", T("open")}, {"v", T("diff")}, {"w", T("worktree")}, {"C", T("changes")}, {"X", T("merge")}, {"y", T("copy")}, {"r", T("refresh")}}
	case gsReviews:
		return [][2]string{{"⏎", T("open")}, {"c", T("review with claude")}, {"v", T("diff")}, {"w", T("worktree")}, {"y", T("copy")}, {"r", T("refresh")}}
	case gsBranches:
		return [][2]string{{"d", T("delete")}, {"x", T("delete merged")}, {"p", T("push")}, {"w", T("worktree")}, {"o", T("shell")}, {"y", T("copy")}}
	case gsProfile:
		return [][2]string{{"t", T("week / month")}, {"y", T("copy")}, {"r", T("refresh")}}
	}
	return [][2]string{{"⏎", T("resume")}, {"C", T("changes")}, {"n", T("new claude")}, {"o", T("shell")}, {"a", T("add")}, {"d", T("remove")}}
}

func (g *gitView) help() []helpSec {
	return []helpSec{
		{T("Overview"), [][2]string{
			{"⏎", T("go to it: the space, or a shell in that worktree")},
			{"O  C", T("open its PR · review its changes")},
			{"P  F", T("pull (fast-forward) · fetch")},
			{"n  w", T("new claude in it · new worktree")},
		}},
		{T("My PRs"), [][2]string{
			{"⏎  y", T("open it in the browser · copy its URL")},
			{"v", T("read its diff in a tab")},
			{"w", T("check its branch out in a worktree")},
			{"C  o", T("review its changes · shell, once it is checked out")},
			{"X", T("merge it (s squash · m merge · b rebase), after asking")},
			{"J", T("open its Jira issue")},
			{"r", T("ask GitHub again")},
		}},
		{T("Reviews"), [][2]string{
			{"c", T("a Claude reviews it in a worktree of the PR; nothing is posted")},
			{"⏎  v  w  y", T("open · diff · worktree · copy URL")},
		}},
		{T("Worktrees"), [][2]string{
			{"⏎  r", T("resume the newest session there (or start claude)")},
			{"n", T("new claude in it")},
			{"C", T("review its changes: files, diff, PR, commit")},
			{"a d p", T("add · remove · prune stale worktrees")},
		}},
		{T("Branches"), [][2]string{
			{"d  x", T("delete a merged branch · all the merged ones, after asking")},
			{"p  w  o", T("push it · worktree from it · shell")},
		}},
		{T("Profile"), [][2]string{
			{"t  y", T("this week or this month · copy the summary")},
		}},
	}
}

func (g *gitView) palette(m *Model) []paletteEntry {
	var out []paletteEntry
	for i := range gitSecs {
		if i == gsWT || !g.on(m, i) {
			continue
		}
		spec := secKey(tabGit, i)
		out = append(out, paletteEntry{"action", T("Git") + " · " + T(gitSecs[i].name), "", func(m *Model) tea.Cmd {
			mm, cmd := m.showSec(strings.TrimPrefix(spec, "@sec:"))
			*m = mm.(Model)
			return cmd
		}})
	}
	return out
}

// menu is the right-click menu of the selected row.
func (g *gitView) menu(m *Model) []menuItem {
	add := func(items *[]menuItem, k, label string) { *items = append(*items, menuItem{label, k, press(k)}) }
	var items []menuItem
	switch g.cur {
	case gsHere:
		items = g.menuOverview(m)
	case gsPRs:
		add(&items, "⏎", T("Open in the browser"))
		add(&items, "v", T("Read the diff"))
		add(&items, "w", T("Check out in a worktree"))
		add(&items, "X", T("Merge…"))
		items = append(items, sep)
		add(&items, "y", T("Copy URL"))
	case gsReviews:
		add(&items, "⏎", T("Open in the browser"))
		add(&items, "c", T("Review with Claude"))
		add(&items, "v", T("Read the diff"))
		add(&items, "w", T("Check out in a worktree"))
		items = append(items, sep)
		add(&items, "y", T("Copy URL"))
	case gsBranches:
		add(&items, "d", T("Delete the branch"))
		add(&items, "p", T("Push it"))
		add(&items, "w", T("Worktree from it"))
		items = append(items, sep)
		add(&items, "y", T("Copy name"))
	case gsProfile:
		add(&items, "t", T("Week / month"))
		add(&items, "y", T("Copy the summary"))
	default:
		add(&items, "⏎", T("Resume the newest session"))
		add(&items, "n", T("New claude in it"))
		add(&items, "C", T("Review changes"))
		add(&items, "o", T("Shell in its folder"))
	}
	return items
}

// busy is true while something the view shows is on its way (frames then run
// for the spinner); a look for the reviews badge from another view is silent.
func (g *gitView) busy() bool {
	if !g.screen {
		return false
	}
	switch g.cur {
	case gsPRs:
		return g.prs.busy || g.doing
	case gsReviews:
		return g.rev.busy || g.doing
	case gsBranches:
		return g.br.busy || g.doing
	case gsProfile:
		return g.profNow().busy
	}
	return false
}

func (g *gitView) selKey(m *Model) string {
	switch g.cur {
	case gsHere:
		return "h" + strconv.Itoa(g.ov.idx)
	case gsPRs:
		return "p" + strconv.Itoa(g.prs.cur.idx)
	case gsReviews:
		return "r" + strconv.Itoa(g.rev.cur.idx)
	case gsBranches:
		return "b" + strconv.Itoa(g.br.cur.idx)
	case gsProfile:
		return "f" + strconv.FormatBool(g.month)
	}
	return strconv.Itoa(m.wt.cur.idx)
}

// badge is the reviews waiting, on the top bar from every view.
func (g *gitView) badge(m *Model) string {
	if !g.reviewsOn(m) || g.noSess || len(g.rev.items) == 0 {
		return ""
	}
	s := m.st
	st := s.Yellow
	if time.Since(g.rev.items[0].Created) > 24*time.Hour {
		st = s.Red
	}
	return clickable(secKey(tabGit, gsReviews), st.Render("⚑ "+Tn(len(g.rev.items), "%d review waiting", "%d reviews waiting")))
}

// barNote is what gh says about itself, at the right end of the sections bar.
func (g *gitView) barNote(m *Model) string {
	switch {
	case g.noGH:
		return m.st.Red.Render(T("gh: not installed"))
	case g.noSess:
		return m.st.Red.Render(T("gh: not signed in"))
	case g.login != "":
		return m.st.Muted.Render("gh ✓ " + g.login)
	}
	return ""
}

// reviewsOn says whether the reviews are looked for even when the view is not shown.
func (g *gitView) reviewsOn(m *Model) bool {
	return m.cfg.On("header.reviews") && m.cfg.On("view.git") && m.cfg.On("git.reviews")
}

// due is what to ask GitHub (or git) for on this tick, on opening the view and on R.
func (g *gitView) due(m *Model, force bool) tea.Cmd {
	if !m.cfg.On("view.git") {
		return nil
	}
	g.fix(m)
	var cmds []tea.Cmd
	shown := g.shown(m)
	g.screen = shown
	if shown && force && m.repos == nil && !m.loading["repos"] && (g.on(m, gsWT) || g.on(m, gsBranches)) {
		m.loading["repos"] = true
		cmds = append(cmds, m.reposCmd())
	}
	if g.on(m, gsPRs) || g.on(m, gsReviews) || g.on(m, gsBranches) {
		cmds = append(cmds, g.localCmd(m))
	}
	if shown && g.cur == gsBranches {
		cmds = append(cmds, g.pollBranches(m, force))
	}
	if shown && force && g.cur == gsHere && time.Since(m.prsAt) > time.Minute {
		cmds = append(cmds, m.prsCmd())
	}
	if !g.noSess {
		switch {
		case shown && g.cur == gsPRs:
			every := ghEvery
			if g.prs.unknown() {
				every = ghRetry
			}
			if g.prs.due(force, every) {
				cmds = append(cmds, g.fetchPRs())
			}
		case shown && g.cur == gsProfile:
			if p := g.profNow(); !p.busy && (force || time.Since(p.at) >= ghEvery) {
				cmds = append(cmds, g.fetchProfile())
			}
		}
		if g.on(m, gsReviews) && (shown && g.cur == gsReviews || g.reviewsOn(m)) {
			every := time.Duration(m.cfg.Git.ReviewEvery()) * time.Minute
			if shown && g.cur == gsReviews {
				every = ghEvery
			}
			if g.rev.due(force && (shown && g.cur == gsReviews || !g.rev.loaded), every) {
				cmds = append(cmds, g.fetchReviews(m))
			}
		}
	}
	return tea.Batch(cmds...)
}

// took notes what a call to gh said about the session.
func (g *gitView) took(login string, err error) {
	if login != "" {
		g.login = login
	}
	if gh.NoSession(err) {
		g.noSess, g.noGH = true, errors.Is(err, gh.ErrNoGH)
	}
}

func (g *gitView) update(m *Model, msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case ghPRsMsg:
		g.prs.busy = false
		g.took(msg.login, msg.err)
		g.prs.err = msg.err
		if msg.err == nil {
			g.prs.items, g.prs.loaded = msg.prs, true
		}
		g.prs.cur.clamp(len(g.prs.items), m.bodyHeight())
		g.fix(m)
	case ghReviewsMsg:
		g.rev.busy = false
		g.took(msg.login, msg.err)
		g.rev.err = msg.err
		if msg.err == nil {
			g.rev.items, g.rev.loaded = msg.prs, true
		}
		g.rev.cur.clamp(len(g.rev.items), m.bodyHeight())
		g.fix(m)
	case ghProfileMsg:
		p := &g.prof[b2i(msg.month)]
		p.busy, p.loaded, p.err = false, p.loaded || msg.err == nil, msg.err
		g.took(msg.login, msg.err)
		if msg.err == nil {
			p.p = msg.p
		}
		g.fix(m)
	case branchesMsg:
		g.br.busy, g.br.loaded, g.br.stale = false, true, false
		g.br.sig, g.br.err = msg.sig, msg.err
		g.took(msg.login, msg.err)
		g.br.set(msg.rows)
		g.fix(m)
	case ghLocalMsg:
		g.local = msg.local
	case ghDoneMsg:
		g.doing = false
		m.setStatus(doneText(msg.text, msg.err), msg.err != nil)
		if msg.refresh && !g.prs.busy && !g.noSess {
			return g.fetchPRs(), true
		}
	case ghReviewMsg:
		g.doing = false
		if msg.err != nil {
			m.setStatus(msg.err.Error(), true)
			return nil, true
		}
		launch := g.launchReview(m, msg.pr, msg.dir)
		return tea.Batch(launch, func() tea.Msg { return wtChangedMsg{T("created ") + tilde(msg.dir)} }), true
	case branchDoneMsg:
		g.doing = false
		g.br.stale = true
		m.setStatus(doneText(msg.text, msg.err), msg.err != nil)
		return g.pollBranches(m, true), true
	default:
		return nil, false
	}
	return nil, true
}

func doneText(text string, err error) string {
	if err != nil {
		return strings.TrimSpace(err.Error())
	}
	return text
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// localCmd finds which repo of this machine is which GitHub repo, again when the repos change.
func (g *gitView) localCmd(m *Model) tea.Cmd {
	var roots []string
	for _, r := range m.repos {
		roots = append(roots, r.Root)
	}
	sig := strings.Join(roots, "\n")
	if len(roots) == 0 || sig == g.locSig {
		return nil
	}
	g.locSig = sig
	return func() tea.Msg {
		out := map[string]string{}
		for _, r := range roots {
			if s := gitwt.Slug(r); s != "" {
				out[s] = r
			}
		}
		return ghLocalMsg{out}
	}
}

// reviewPrompt fills what the review agent is told.
func reviewPrompt(tpl string, p gh.PR) string {
	return strings.NewReplacer("{url}", p.URL, "{number}", strconv.Itoa(p.Number), "{repo}", p.Repo).Replace(tpl)
}

// the Settings rows of the Git view
func init() {
	settingsList = append(settingsList,
		setting{M("Git"), M("Merge method"), M("How X merges a PR: squash, merge or rebase. It always asks first, and never forces a PR that isn't ready."),
			func(c *config.Config) string { return c.Git.Method() },
			func(c *config.Config, d int) {
				c.Git.MergeMethod = cycleStr([]string{config.MergeSquash, config.MergeCommit, config.MergeRebase}, c.Git.Method(), d)
			}, nil, nil},
		setting{M("Git"), M("Reviews to show"), M("all: the PRs asked of you and of your teams. direct: only the ones asked of you."),
			func(c *config.Config) string { return T(c.Git.ReviewMode()) },
			func(c *config.Config, d int) {
				c.Git.Reviews = cycleStr([]string{config.ReviewsAll, config.ReviewsDirect}, c.Git.ReviewMode(), d)
			}, nil, nil},
		setting{M("Git"), M("Look for reviews every"), M("Minutes between looks for reviews while Git isn't on screen (the top bar shows how many wait)."),
			func(c *config.Config) string { return Tf("%d min", c.Git.ReviewEvery()) },
			func(c *config.Config, d int) { c.Git.ReviewMins = min(60, max(1, c.Git.ReviewEvery()+d)) }, nil, nil},
		setting{M("Git"), M("Review prompt"), M("What the Claude that reviews a PR is told (c in Reviews). {url} {number} {repo} are filled in. Empty = the default."),
			func(c *config.Config) string { return c.Git.Prompt() }, nil,
			func(c *config.Config, v string) error { c.Git.ReviewPrompt = strings.TrimSpace(v); return nil }, nil},
	)
}
