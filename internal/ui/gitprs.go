package ui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/secuone/nanny/internal/config"
	"github.com/secuone/nanny/internal/gh"
	"github.com/secuone/nanny/internal/gitwt"
)

// My PRs and Reviews: two lists of the same kind, fetched, drawn and driven alike.

func (g *gitView) fetchPRs() tea.Cmd {
	g.prs.busy, g.prs.at = true, time.Now()
	login := g.login
	return func() tea.Msg {
		ctx, cancel := ghCtx()
		defer cancel()
		login, err := viewerOr(ctx, login)
		if err != nil {
			return ghPRsMsg{err: err}
		}
		prs, err := gh.MyPRs(ctx, login)
		return ghPRsMsg{prs, login, err}
	}
}

func (g *gitView) fetchReviews(m *Model) tea.Cmd {
	g.rev.busy, g.rev.at = true, time.Now()
	login, direct := g.login, m.cfg.Git.ReviewMode() == config.ReviewsDirect
	return func() tea.Msg {
		ctx, cancel := ghCtx()
		defer cancel()
		login, err := viewerOr(ctx, login)
		if err != nil {
			return ghReviewsMsg{err: err}
		}
		prs, err := gh.ReviewRequests(ctx, login, direct)
		return ghReviewsMsg{prs, login, err}
	}
}

// viewerOr is the login already known, else gh's.
func viewerOr(ctx context.Context, login string) (string, error) {
	if login != "" {
		return login, nil
	}
	return gh.Viewer(ctx)
}

// refresh is r: gh gets another chance, and the section is asked again.
func (g *gitView) refresh(m *Model) tea.Cmd {
	g.noSess, g.noGH = false, false
	g.prs.err, g.rev.err = nil, nil
	g.br.stale = true
	return g.due(m, true)
}

func (g *gitView) keyPR(m *Model, k tea.KeyPressMsg, l *prList, reviews bool) tea.Cmd {
	s := k.String()
	if moveKey(&l.cur, len(l.items), m.bodyHeight(), s) {
		return nil
	}
	if s == "r" {
		return g.refresh(m)
	}
	if cmd, ok := g.keyNoSess(m, s); ok {
		return cmd
	}
	p, ok := l.sel()
	if !ok {
		return nil
	}
	switch s {
	case "enter":
		return m.openURL(p.URL)
	case "v":
		return m.watch(fmt.Sprintf("#%d %s", p.Number, shortRepo(p.Repo)), gh.DiffCmd(p.Repo, p.Number), "")
	case "w":
		return g.checkout(m, p)
	case "y":
		return m.copy(p.URL, T("URL"))
	case "C":
		if dir, ok := g.localWT(m, p); ok {
			return m.openReview(dir)
		}
		m.setStatus(T("check it out first: w"), true)
	case "o":
		if dir, ok := g.localWT(m, p); ok {
			return m.shell(dir)
		}
		m.setStatus(T("check it out first: w"), true)
	case "J":
		return openJira(m, p.Head+" "+p.Title)
	case "X":
		if !reviews {
			return g.mergeAsk(m, p)
		}
	case "c":
		if reviews {
			return g.reviewAgent(m, p)
		}
	}
	l.cur.clamp(len(l.items), m.bodyHeight())
	return nil
}

// openJira opens the first issue key found in text, when Jira is set up.
func openJira(m *Model, text string) tea.Cmd {
	key := jiraKeyRe.FindString(text)
	if key == "" || jiraBrowseURL == nil {
		m.setStatus(T("no Jira issue to open here"), true)
		return nil
	}
	url := jiraBrowseURL(m, key)
	if url == "" {
		m.setStatus(T("no Jira issue to open here"), true)
		return nil
	}
	return m.openURL(url)
}

func shortRepo(repo string) string {
	if _, name, ok := strings.Cut(repo, "/"); ok {
		return name
	}
	return repo
}

// localWT is where the branch of a PR is checked out on this machine.
func (g *gitView) localWT(m *Model, p gh.PR) (string, bool) {
	root := g.local[p.Repo]
	if root == "" {
		return "", false
	}
	for _, r := range m.repos {
		if r.Root != root {
			continue
		}
		for _, w := range r.Worktrees {
			if w.Missing {
				continue
			}
			if p.Fork && strings.HasSuffix(w.Path, fmt.Sprintf("/pr-%d", p.Number)) || !p.Fork && w.Branch == p.Head {
				return w.Path, true
			}
		}
	}
	return "", false
}

// prWorktree makes the worktree of a PR's branch next to its repo. A branch
// of a fork can't be fetched from origin: gh puts it in a detached worktree.
func prWorktree(root string, p gh.PR) (string, error) {
	if p.Fork {
		path, err := gitwt.AddDetached(root, fmt.Sprintf("pr-%d", p.Number))
		if err != nil {
			return "", err
		}
		ctx, cancel := ghCtx()
		defer cancel()
		if err := gh.Checkout(ctx, path, p.Repo, p.Number); err != nil {
			return path, err
		}
		return path, nil
	}
	if err := gitwt.FetchBranch(root, p.Head); err != nil {
		return "", err
	}
	path, err := gitwt.Add(root, p.Head, "origin/"+p.Head)
	if err != nil && strings.Contains(err.Error(), "already exists") { // the branch is here, no worktree has it
		return gitwt.AddExisting(root, p.Head)
	}
	return path, err
}

// checkout puts a PR's branch in a worktree, or shows the one that has it.
func (g *gitView) checkout(m *Model, p gh.PR) tea.Cmd {
	if dir, ok := g.localWT(m, p); ok {
		m.setStatus(Tf("already checked out in %s", tilde(dir)), false)
		return nil
	}
	root := g.local[p.Repo]
	if root == "" {
		m.setStatus(Tf("no clone of %s on this machine", p.Repo), true)
		return nil
	}
	m.setStatus(Tf("checking out #%d…", p.Number), false)
	return func() tea.Msg {
		path, err := prWorktree(root, p)
		if err != nil {
			return statusMsg{err.Error(), true}
		}
		return wtChangedMsg{T("created ") + tilde(path)}
	}
}

// reviewAgent starts a Claude on the PR, in a worktree of its branch.
func (g *gitView) reviewAgent(m *Model, p gh.PR) tea.Cmd {
	if dir, ok := g.localWT(m, p); ok {
		return g.launchReview(m, p, dir)
	}
	root := g.local[p.Repo]
	if root == "" { // no clone: it reads the diff through gh from wherever it starts
		m.setStatus(Tf("no clone of %s: the agent reads the diff with gh", p.Repo), false)
		return g.launchReview(m, p, home)
	}
	g.doing = true
	m.setStatus(Tf("checking out #%d…", p.Number), false)
	return func() tea.Msg {
		path, err := prWorktree(root, p)
		return ghReviewMsg{p, path, err}
	}
}

// launchReview opens the Claude that reviews a PR; the prompt is [git] review_prompt.
func (g *gitView) launchReview(m *Model, p gh.PR, dir string) tea.Cmd {
	argv := m.claudeArgv(reviewPrompt(m.cfg.Git.Prompt(), p))
	label := fmt.Sprintf("review #%d", p.Number)
	if m.cfg.Launch == config.LaunchInline {
		q := make([]string, len(argv))
		for i, a := range argv {
			q[i] = shellQuote(a)
		}
		return m.run(dir, label, strings.Join(q, " "))
	}
	return m.openTab(dir, label, argv)
}

// mergeAsk asks before merging; s, m and b pick the method. A PR that isn't
// ready is not forced: it is set to merge by itself when GitHub lets it.
func (g *gitView) mergeAsk(m *Model, p gh.PR) tea.Cmd {
	switch {
	case p.Draft:
		m.setStatus(T("it is a draft"), true)
		return nil
	case p.Conflicts():
		m.setStatus(T("it has conflicts: fix them first"), true)
		return nil
	}
	method, auto := m.cfg.Git.Method(), !p.Ready()
	body := func() string {
		b := p.Repo + "  " + p.Title + "\n\n" + Tf("Method: %s  (s squash · m merge · b rebase)", method)
		if auto {
			b += "\n\n" + T("It isn't ready: GitHub will merge it by itself once its checks and reviews pass.")
		}
		return b
	}
	m.ask(Tf("Merge PR #%d?", p.Number), body(), func(m *Model, _ string) tea.Cmd {
		g.doing = true
		repo, n, meth := p.Repo, p.Number, method
		return func() tea.Msg {
			ctx, cancel := ghCtx()
			defer cancel()
			if err := gh.Merge(ctx, repo, n, meth, auto); err != nil {
				return ghDoneMsg{err: err}
			}
			if auto {
				return ghDoneMsg{text: Tf("#%d will merge by itself", n), refresh: true}
			}
			return ghDoneMsg{text: Tf("merged #%d", n), refresh: true}
		}
	})
	pick := func(meth string) func(*Model) tea.Cmd {
		return func(m *Model) tea.Cmd {
			method = meth
			m.modal.body = body()
			return nil
		}
	}
	m.modal.keys = map[string]func(*Model) tea.Cmd{"s": pick(gh.Squash), "m": pick(gh.MergeCommit), "b": pick(gh.Rebase)}
	return nil
}

// ---------------------------------------------------------------- drawing

// prState is the one word that says where a PR stands, with its colour.
func (g *gitView) prState(m *Model, p gh.PR) string {
	s := m.st
	switch {
	case p.Draft:
		return s.Muted.Render(T("draft"))
	case p.Conflicts():
		return s.Red.Render(T("conflicts"))
	case p.ChecksFailing():
		return s.Red.Render(T("checks failing"))
	case p.Decision == "CHANGES_REQUESTED":
		return s.Yellow.Render(T("changes requested"))
	case p.Ready():
		return s.Green.Render(T("ready"))
	case p.Unknown():
		return s.Muted.Render("?")
	case p.Checks == "PENDING" || p.Checks == "EXPECTED":
		return s.Yellow.Render(T("checks running"))
	case p.Decision == "APPROVED":
		return s.Green.Render(T("approved"))
	}
	return s.Muted.Render(T("waiting"))
}

func (g *gitView) prLine(m *Model, p gh.PR, w int, reviews bool) string {
	s := m.st
	icon := s.Green.Render("● ")
	if p.Draft {
		icon = s.Muted.Render("○ ")
	}
	left := icon + s.Muted.Render("#"+strconv.Itoa(p.Number)) + " " + s.Teal.Render(shortRepo(p.Repo)) + " " + s.Text.Render(p.Title)
	right := g.prState(m, p) + " " + s.Muted.Render(ago(p.Updated))
	if reviews {
		age := s.Muted.Render(ago(p.Created))
		if time.Since(p.Created) > 24*time.Hour {
			age = s.Red.Render(ago(p.Created))
		}
		right = s.Muted.Render(p.Author) + " " + age
	}
	return fit(left, w-lipglossW(right)-1) + " " + right
}

// noSessState is what is shown instead of GitHub's lists while gh can't be used.
func (g *gitView) noSessState(m *Model, w int) string {
	s := m.st
	if g.noGH {
		return s.emptyState(T("gh isn't installed."), [][2]string{{"o", T("open cli.github.com")}, {"r", T("try again")}}, w)
	}
	return s.emptyState(T("GitHub isn't connected. nanny reads your PRs and reviews with the gh CLI."),
		[][2]string{{"i", T("sign in now (gh auth login in a tab)")}, {"s", T("setup wizard")}, {"r", T("try again")}}, w)
}

// keyNoSess takes the keys of that state; false = not one of them.
func (g *gitView) keyNoSess(m *Model, k string) (tea.Cmd, bool) {
	switch {
	case !g.noSess:
		return nil, false
	case g.noGH && k == "o":
		return m.openURL("https://cli.github.com"), true
	case !g.noGH && k == "i":
		return m.run(home, "gh auth login", "gh auth login"), true
	case !g.noGH && k == "s":
		m.openSetup(setupGitHub)
		return nil, true
	}
	return nil, false
}

func (g *gitView) hint(m *Model, l *prList, w int) (string, bool) {
	s := m.st
	switch {
	case g.noSess:
		return g.noSessState(m, w), true
	case l.err != nil && len(l.items) == 0:
		return s.emptyState(trunc(l.err.Error(), w), [][2]string{{"r", T("try again")}}, w), true
	case !l.loaded:
		return s.emptyState(spinGlyph+" "+T("asking GitHub…"), nil, w), true
	}
	return "", false
}

func (g *gitView) viewPRs(m *Model, l *prList, title string, reviews bool, w, h int) string {
	s := m.st
	inner := w - 4
	if body, ok := g.hint(m, l, inner); ok {
		return s.panel(title, body, w, h, !m.focusR)
	}
	if len(l.items) == 0 {
		msg := T("No open PRs of yours.")
		if reviews {
			msg = T("Nothing is waiting for your review.")
		}
		return s.panel(title, s.emptyState(msg, nil, inner), w, h, !m.focusR)
	}
	blocks := make([][]string, len(l.items))
	for i, p := range l.items {
		line := g.prLine(m, p, inner, reviews)
		if i == l.cur.idx {
			st := s.RowSel
			if m.focusR {
				st = s.RowSelBlur
			}
			line = s.Sel(st, fit(line, inner))
		}
		blocks[i] = []string{line}
	}
	l.cur.clamp(len(l.items), h-2)
	t := title + " · " + strconv.Itoa(len(l.items))
	if l.err != nil {
		t += " · ⚠"
	}
	return s.panel(t, m.listBlocks(blocks, l.cur.idx, "git"+strconv.Itoa(g.cur), h-2, secY+2), w, h, !m.focusR)
}

func (g *gitView) detailPR(m *Model, l *prList, reviews bool, w, h int) string {
	s := m.st
	inner := w - 4
	p, ok := l.sel()
	if !ok {
		return s.panel(T("PR"), "", w, h, m.focusR)
	}
	var ls []string
	ls = append(ls, s.Bold.Render(trunc(p.Title, inner)), "")
	if l.err != nil {
		ls = append(ls, s.Red.Render(trunc(l.err.Error(), inner)), "")
	}
	ls = append(ls, s.kv("PR", fmt.Sprintf("%s #%d", p.Repo, p.Number), inner),
		s.kv(T("author"), p.Author, inner),
		s.kv(T("branch"), s.Text.Render(p.Head+" → "+p.Base), inner),
		s.kv(T("opened"), ago(p.Created)+"  ("+T("updated")+" "+ago(p.Updated)+")", inner),
		s.kv(T("size"), Tf("%s %s · %d files", s.Green.Render(fmt.Sprintf("+%d", p.Additions)), s.Red.Render(fmt.Sprintf("−%d", p.Deletions)), p.Files), inner+30),
		s.kv(T("state"), g.prState(m, p), inner+20))
	ls = append(ls, s.kv(T("checks"), checksLabel(p), inner))
	for _, r := range p.Reviews {
		ls = append(ls, s.kv(T("review"), r.Author+" · "+reviewLabel(r.State), inner))
	}
	if len(p.Requested) > 0 {
		ls = append(ls, s.kv(T("asked"), trunc(strings.Join(p.Requested, ", "), inner-12), inner))
	}
	if key := jiraKeyRe.FindString(p.Head + " " + p.Title); key != "" {
		ls = append(ls, s.kv("Jira", key, inner))
	}
	if dir, ok := g.localWT(m, p); ok {
		ls = append(ls, s.kv(T("here"), tilde(dir), inner))
	} else if g.local[p.Repo] != "" {
		ls = append(ls, s.kv(T("here"), s.Muted.Render(T("not checked out · w does it")), inner))
	}
	keys := [][2]string{{"⏎", T("open")}, {"v", T("diff")}, {"w", T("worktree")}, {"y", T("copy")}}
	if reviews {
		keys = append([][2]string{{"c", T("review with claude")}}, keys...)
	} else {
		keys = append(keys, [2]string{"X", T("merge")})
	}
	ls = append(ls, "", s.hintsFit(keys, inner))
	return s.panel(T("PR"), m.detailScroll(ls, h), w, h, m.focusR)
}

func checksLabel(p gh.PR) string {
	switch p.Checks {
	case "SUCCESS":
		return T("passing")
	case "FAILURE", "ERROR":
		return T("failing")
	case "PENDING", "EXPECTED":
		return T("running")
	}
	return T("none")
}

func reviewLabel(state string) string {
	switch state {
	case "APPROVED":
		return T("approved")
	case "CHANGES_REQUESTED":
		return T("changes requested")
	case "DISMISSED":
		return T("dismissed")
	}
	return T("commented")
}
