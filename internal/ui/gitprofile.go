package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/secuone/nanny/internal/gh"
)

// Profile: what you did this week or this month, from GitHub (and Jira, when it is set up).

type profData struct {
	p      gh.Profile
	at     time.Time
	busy   bool
	loaded bool
	err    error
}

func (g *gitView) profNow() *profData { return &g.prof[b2i(g.month)] }

func (g *gitView) fetchProfile() tea.Cmd {
	month := g.month
	p := g.profNow()
	p.busy, p.at = true, time.Now()
	login := g.login
	return func() tea.Msg {
		ctx, cancel := ghCtx()
		defer cancel()
		login, err := viewerOr(ctx, login)
		if err != nil {
			return ghProfileMsg{month: month, err: err}
		}
		now := time.Now()
		from := gh.Week(now)
		if month {
			from = gh.Month(now)
		}
		pr, err := gh.Mine(ctx, login, from, now)
		return ghProfileMsg{pr, month, login, err}
	}
}

func (g *gitView) keyProfile(m *Model, k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "t":
		g.month = !g.month
		return g.due(m, true)
	case "r":
		return g.refresh(m)
	case "y":
		if p := g.profNow(); p.loaded {
			return m.copy(g.summary(m, p.p), T("summary"))
		}
	}
	return nil
}

func (g *gitView) periodName() string {
	if g.month {
		return T("this month")
	}
	return T("this week")
}

// closed is the issues closed since the start of the period, "—" without Jira.
func (g *gitView) closed(m *Model, from time.Time) string {
	if jiraClosedSince != nil {
		if n, ok := jiraClosedSince(m, from); ok {
			return fmt.Sprint(n)
		}
	}
	return "—"
}

func (g *gitView) summary(m *Model, p gh.Profile) string {
	ls := []string{
		fmt.Sprintf("%s · %s", p.Login, g.periodName()),
		fmt.Sprintf("%s: %d", T("PRs opened"), p.Opened),
		fmt.Sprintf("%s: %d", T("PRs merged"), p.Merged),
		fmt.Sprintf("%s: %d", T("reviews done"), p.Reviews),
	}
	if p.Contribed {
		ls = append(ls, fmt.Sprintf("%s: %d", T("commits"), p.Commits))
	}
	ls = append(ls, fmt.Sprintf("%s: %s", T("avg time to merge"), dur(p.AvgMerge)),
		fmt.Sprintf("%s: %s", T("tickets closed"), g.closed(m, p.From)))
	return strings.Join(ls, "\n")
}

// dur is a duration in the biggest unit that says something.
func dur(d time.Duration) string {
	switch {
	case d <= 0:
		return "—"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%.1fh", d.Hours())
	}
	return fmt.Sprintf("%.1fd", d.Hours()/24)
}

func (g *gitView) viewProfile(m *Model, w, h int) string {
	s := m.st
	inner := w - 4
	title := T("Profile") + " · " + g.periodName()
	p := g.profNow()
	switch {
	case g.noSess:
		return s.panel(title, s.emptyState(T("gh: sign in (gh auth login)"), [][2]string{{"r", T("try again")}}, inner), w, h, !m.focusR)
	case !p.loaded && p.err != nil:
		return s.panel(title, s.emptyState(trunc(p.err.Error(), inner), [][2]string{{"r", T("try again")}}, inner), w, h, !m.focusR)
	case !p.loaded:
		return s.panel(title, s.emptyState(spinGlyph+" "+T("asking GitHub…"), nil, inner), w, h, !m.focusR)
	}
	pr := p.p
	rows := [][2]string{
		{T("PRs opened"), fmt.Sprint(pr.Opened)},
		{T("PRs merged"), fmt.Sprint(pr.Merged)},
		{T("reviews done"), fmt.Sprint(pr.Reviews)},
	}
	if pr.Contribed {
		rows = append(rows, [2]string{T("commits"), fmt.Sprint(pr.Commits)})
	}
	rows = append(rows, [2]string{T("avg time to merge"), dur(pr.AvgMerge)}, [2]string{T("tickets closed"), g.closed(m, pr.From)})
	ls := []string{s.Bold.Render(pr.Login) + s.Muted.Render("  "+Tf("since %s", pr.From.Format("Mon 2 Jan"))), ""}
	for _, r := range rows {
		ls = append(ls, s.Label.Render(fit(r[0], 20))+s.Text.Render(r[1]))
	}
	ls = append(ls, "", s.hintsFit([][2]string{{"t", T("week / month")}, {"y", T("copy")}, {"r", T("refresh")}}, inner))
	return s.panel(title, strings.Join(ls, "\n"), w, h, !m.focusR)
}

func (g *gitView) detailProfile(m *Model, w, h int) string {
	s := m.st
	inner := w - 4
	p := g.profNow()
	if !p.loaded {
		return s.panel(T("By repo"), "", w, h, m.focusR)
	}
	var ls []string
	group := func(title string, rs []gh.RepoCount) {
		if len(rs) == 0 {
			return
		}
		top := rs[0].N
		ls = append(ls, s.Accent.Render(title))
		for _, r := range rs {
			bar := strings.Repeat("█", max(1, r.N*12/top))
			ls = append(ls, fit(s.Teal.Render(bar)+" "+s.Text.Render(fmt.Sprint(r.N))+" "+s.Muted.Render(r.Repo), inner))
		}
		ls = append(ls, "")
	}
	group(T("Commits"), p.p.Commited)
	group(T("PRs opened"), p.p.Authored)
	group(T("Reviews"), p.p.Reviewed)
	if len(ls) == 0 {
		ls = append(ls, s.Muted.Render(T("GitHub gave no per-repo numbers (an organization's SSO may hide them).")))
	}
	return s.panel(T("By repo"), m.detailScroll(ls, h), w, h, m.focusR)
}
