package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/secuone/nanny/internal/gh"
	"github.com/secuone/nanny/internal/gitwt"
)

type scopeRepos struct{ api, web, plain, sub, wt string }

// scopeFixture makes two GitHub repos (api has an upstream, a subfolder and a
// worktree), and a folder that is no repo.
func scopeFixture(t *testing.T) scopeRepos {
	t.Helper()
	base := t.TempDir()
	mk := func(name string, remotes ...string) string {
		dir := filepath.Join(base, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		gitIn(t, dir, "init", "-q", "-b", "main")
		gitIn(t, dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init")
		for i := 0; i+1 < len(remotes); i += 2 {
			gitIn(t, dir, "remote", "add", remotes[i], remotes[i+1])
		}
		return dir
	}
	r := scopeRepos{
		api:   mk("acme-api", "origin", "https://github.com/acme/acme-api", "upstream", "git@github.com:up/acme-api.git"),
		web:   mk("web", "origin", "ssh://git@github.com/acme/web.git"),
		plain: filepath.Join(base, "notes"),
	}
	_ = os.MkdirAll(r.plain, 0o755)
	r.sub = filepath.Join(r.api, "src")
	_ = os.MkdirAll(r.sub, 0o755)
	r.wt = filepath.Join(base, "acme-api-wt")
	gitIn(t, r.api, "worktree", "add", "-q", "-b", "feat", r.wt)
	return r
}

func scopePRs() []gh.PR {
	return []gh.PR{
		{Number: 1, Title: "api change", Repo: "acme/acme-api", Head: "a"},
		{Number: 2, Title: "fork api change", Repo: "up/acme-api", Head: "b"},
		{Number: 3, Title: "web change", Repo: "acme/web", Head: "c"},
		{Number: 4, Title: "other repo", Repo: "x/else", Head: "d"},
	}
}

// scopeOn puts the Git view on section sec of the space on dir (or All spaces
// for ""), with the PRs and reviews gh would have answered.
func scopeOn(t *testing.T, dir string, sec int) tea.Model {
	t.Helper()
	useGH(t)
	m := boot(t, 150, 42).(Model)
	if dir != "" {
		sp, _ := m.spaceFor(dir, true)
		m.cur = m.indexOf(sp)
		m.layoutPanes()
	} else {
		m.cur = -1
	}
	m.show(tabGit)
	g := m.gitv
	g.noSess = false
	g.update(&m, ghPRsMsg{prs: scopePRs(), login: "me"})
	g.update(&m, ghReviewsMsg{prs: scopePRs(), login: "me"})
	g.setSec(&m, sec)
	return m
}

func TestPRsScopeFollowsSpace(t *testing.T) {
	r := scopeFixture(t)
	for sec, name := range map[int]string{gsPRs: "My PRs", gsReviews: "Reviews"} {
		for _, dir := range []string{r.api, r.sub, r.wt} {
			out := screen(scopeOn(t, dir, sec))
			for _, want := range []string{"api change", "fork api change", "· acme-api"} {
				if !strings.Contains(out, want) {
					t.Errorf("%s in %s lacks %q:\n%s", name, dir, want, out)
				}
			}
			for _, no := range []string{"web change", "other repo"} {
				if strings.Contains(out, no) {
					t.Errorf("%s in %s shows %q of another repo:\n%s", name, dir, no, out)
				}
			}
		}
		out := screen(scopeOn(t, r.web, sec))
		if !strings.Contains(out, "web change") || strings.Contains(out, "api change") {
			t.Errorf("%s in web:\n%s", name, out)
		}
		all := screen(scopeOn(t, "", sec))
		for _, want := range []string{"api change", "web change", "other repo", "all spaces"} {
			if !strings.Contains(all, want) {
				t.Errorf("%s in All spaces lacks %q:\n%s", name, want, all)
			}
		}
	}
}

func TestPRsScopeCountAndNoRefetch(t *testing.T) {
	r := scopeFixture(t)
	m := scopeOn(t, r.api, gsPRs).(Model)
	if g := m.gitv; len(g.prs.items) != 2 || len(g.prs.all) != 4 {
		t.Fatalf("scoped %d of %d", len(g.prs.items), len(g.prs.all))
	}
	if out := screen(m); !strings.Contains(out, "My PRs · 2") {
		t.Errorf("the count should be the scoped one:\n%s", out)
	}
	m.cur = -1
	if out := screen(m); !strings.Contains(out, "My PRs · 4") {
		t.Errorf("All spaces counts every PR:\n%s", out)
	}
}

func TestPRsScopeEmptyStates(t *testing.T) {
	r := scopeFixture(t)
	other := mkRepoWithRemote(t, "https://github.com/acme/empty.git")
	out := screen(scopeOn(t, other, gsPRs))
	if !strings.Contains(out, "No open PRs in empty") || !strings.Contains(out, "see all spaces") {
		t.Errorf("empty repo:\n%s", out)
	}
	out = screen(scopeOn(t, other, gsReviews))
	if !strings.Contains(out, "Nothing is waiting for your review in empty") {
		t.Errorf("empty reviews:\n%s", out)
	}
	out = screen(scopeOn(t, r.plain, gsPRs))
	if !strings.Contains(out, "notes is not a git repository") {
		t.Errorf("no repo:\n%s", out)
	}
	m := scopeOn(t, other, gsPRs)
	m = key(m, "a")
	if mm := m.(Model); mm.cur != -1 {
		t.Errorf("a should go to All spaces, cur = %d", mm.cur)
	}
	if out := screen(m); !strings.Contains(out, "api change") {
		t.Errorf("All spaces shows everything:\n%s", out)
	}
}

func mkRepoWithRemote(t *testing.T, url string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "empty")
	_ = os.MkdirAll(dir, 0o755)
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "remote", "add", "origin", url)
	return dir
}

// scopeRepos makes the repos of the fixture the ones nanny knows.
func withFixtureRepos(m tea.Model, r scopeRepos) Model {
	mm := m.(Model)
	mm.repos = gitwt.Discover([]string{r.api, r.web})
	mm.wt.rebuild(&mm)
	return mm
}

func TestBranchesScope(t *testing.T) {
	r := scopeFixture(t)
	gitIn(t, r.api, "branch", "api-only")
	gitIn(t, r.web, "branch", "web-only")
	for _, c := range []struct{ dir, want, no string }{{r.api, "api-only", "web-only"}, {r.web, "web-only", "api-only"}, {"", "api-only", ""}} {
		mm := withFixtureRepos(scopeOn(t, c.dir, gsBranches), r)
		mm.gitv.br.busy = false
		m := drain(mm, mm.gitv.pollBranches(&mm, true), 0)
		out := screen(m)
		if !strings.Contains(out, c.want) || c.no != "" && strings.Contains(out, c.no) {
			t.Errorf("branches in %q:\n%s", c.dir, out)
		}
		if c.dir == "" && !strings.Contains(out, "web-only") {
			t.Errorf("All spaces shows every repo's branches:\n%s", out)
		}
	}
}

func TestWorktreesScope(t *testing.T) {
	r := scopeFixture(t)
	for _, c := range []struct{ dir, want, no string }{{r.api, "▍acme-api", "▍web"}, {r.wt, "▍acme-api", "▍web"}, {r.web, "▍web", "▍acme-api"}} {
		out := screen(withFixtureRepos(scopeOn(t, c.dir, gsWT), r))
		if !strings.Contains(out, c.want) || strings.Contains(out, c.no) {
			t.Errorf("worktrees in %s:\n%s", c.dir, out)
		}
	}
	out := screen(withFixtureRepos(scopeOn(t, "", gsWT), r))
	if !strings.Contains(out, "▍acme-api") || !strings.Contains(out, "▍web") {
		t.Errorf("All spaces shows every repo:\n%s", out)
	}
}

func TestProfileStaysGlobalWithNote(t *testing.T) {
	r := scopeFixture(t)
	m := scopeOn(t, r.api, gsProfile).(Model)
	g := m.gitv
	g.update(&m, ghProfileMsg{p: gh.Profile{Login: "me", Opened: 3}})
	out := screen(m)
	if !strings.Contains(out, "All your repos, not only acme-api") {
		t.Errorf("the note:\n%s", out)
	}
}
