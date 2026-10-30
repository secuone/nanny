package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/secuone/nanny/internal/gh"
	"github.com/secuone/nanny/internal/i18n"
)

// ghFake answers gh from the fixtures of internal/gh and remembers every call.
type ghFake struct {
	mu     sync.Mutex
	calls  []string
	fail   error  // every call fails with it
	merged string // what the search for merged PRs answers
}

func (f *ghFake) run(_ context.Context, args ...string) ([]byte, error) {
	all := strings.Join(args, " ")
	f.mu.Lock()
	f.calls = append(f.calls, all)
	fail := f.fail
	f.mu.Unlock()
	if fail != nil {
		return nil, fail
	}
	read := func(name string) ([]byte, error) { return os.ReadFile(filepath.Join("..", "gh", "testdata", name)) }
	switch {
	case strings.Contains(all, "api user"):
		return []byte("me\n"), nil
	case strings.HasPrefix(all, "pr merge"):
		return nil, nil
	case strings.Contains(all, "is:merged"):
		if f.merged != "" {
			return []byte(f.merged), nil
		}
		return []byte(`{"data":{"search":{"nodes":[]}}}`), nil
	case strings.Contains(all, "review-requested:"):
		return read("reviews.json")
	case strings.Contains(all, "reviewed:search"):
		return read("search.json")
	case strings.Contains(all, "contributionsCollection"):
		return read("contrib.json")
	case strings.Contains(all, "author:me"):
		return read("mine.json")
	}
	return nil, errors.New("unexpected gh call: " + all)
}

func (f *ghFake) saw(sub string) bool { return f.count(sub) > 0 }

func (f *ghFake) count(sub string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if strings.Contains(c, sub) {
			n++
		}
	}
	return n
}

func useGH(t *testing.T) *ghFake {
	t.Helper()
	f := &ghFake{}
	old := gh.Exec
	gh.Exec = f.run
	t.Cleanup(func() { gh.Exec = old })
	return f
}

func gitOf(m tea.Model) *gitView { return m.(Model).gitv }

func screen(m tea.Model) string { return ansi.Strip(viewOf(m)) }

// bar is the sections bar of the view on screen.
func bar(m tea.Model) string { return strings.Split(screen(m), "\n")[secY] }

func turnOff(m tea.Model, ids ...string) tea.Model {
	mm := m.(Model)
	mm.cfg.SetOff(append(slices.Clone(mm.cfg.OffList(mm.cfg.ActiveProfile())), ids...))
	return mm
}

func ghTick(m tea.Model) tea.Model {
	mm := m.(Model)
	return drain(mm, mm.featDue(false), 0)
}

// Git opens on My PRs when gh works, and the list says where each PR stands.
func TestGitMyPRs(t *testing.T) {
	f := useGH(t)
	m := key(boot(t, 150, 42), "3")
	g := gitOf(m)
	if g.cur != gsPRs || len(g.prs.items) != 4 {
		t.Fatalf("section %d, %d PRs", g.cur, len(g.prs.items))
	}
	out := screen(m)
	for _, w := range []string{"My PRs", "Reviews", "Worktrees", "Branches", "Profile", "conflicts", "ready", "checks failing", "draft", "ready one"} {
		if !strings.Contains(out, w) {
			t.Errorf("the view lacks %q:\n%s", w, out)
		}
	}
	if !f.saw("is:open author:me archived:false") {
		t.Errorf("my PRs were not asked: %v", f.calls)
	}
	for k, want := range map[string]string{"j": "o/red", "G": "o/new"} {
		m = key(m, k)
		if !strings.Contains(screen(m), "PR        "+want+" #") {
			t.Errorf("the detail should show %s after %s", want, k)
		}
	}
}

// A section whose id is off is not shown and never asked.
func TestGitSectionsOff(t *testing.T) {
	f := useGH(t)
	m := turnOff(boot(t, 150, 42), "git.reviews")
	m = key(ghTick(m), "3")
	m = ghTick(m)
	if b := bar(m); strings.Contains(b, "Reviews") || !strings.Contains(b, "My PRs") {
		t.Errorf("the bar: %q", b)
	}
	if f.saw("review-requested") {
		t.Errorf("reviews were asked: %v", f.calls)
	}
	if !f.saw("author:me") {
		t.Error("My PRs should still be asked")
	}
	f2 := useGH(t)
	m = turnOff(boot(t, 150, 42), "git.prs", "git.profile")
	m = key(m, "3")
	if g := gitOf(m); g.cur != gsReviews || strings.Contains(bar(m), "Profile") {
		t.Errorf("the first section that is on opens: %d %q", g.cur, bar(m))
	}
	if f2.saw("author:me archived") {
		t.Error("My PRs was off and asked")
	}
}

// With gh missing or logged out the hint shows and nothing more is asked until r.
func TestGitNoSessionAsksOnce(t *testing.T) {
	f := useGH(t)
	f.fail = &gh.Error{Msg: "To get started with GitHub CLI, please run: gh auth login"}
	m := key(boot(t, 150, 42), "3")
	if g := gitOf(m); !g.noSess || g.cur != gsWT {
		t.Fatalf("noSess %v, section %d: the view falls back to Worktrees", g.noSess, g.cur)
	}
	n := len(f.calls)
	for i := 0; i < 3; i++ {
		m = ghTick(key(key(m, "1"), "3"))
	}
	if len(f.calls) != n {
		t.Errorf("no more calls without r: %d → %d", n, len(f.calls))
	}
	m = key(key(m, "left"), "left") // My PRs
	if !strings.Contains(screen(m), "gh auth login") {
		t.Errorf("the hint should show:\n%s", screen(m))
	}
	f.fail = nil
	m = key(m, "r")
	if len(f.calls) == n || gitOf(m).noSess || len(gitOf(m).prs.items) == 0 {
		t.Errorf("r tries again: %d calls, %d PRs", len(f.calls), len(gitOf(m).prs.items))
	}
}

// The Worktrees section is what the Worktrees view was.
func TestGitWorktreesSection(t *testing.T) {
	m := key(boot(t, 150, 42), "3")
	if gitOf(m).cur != gsWT {
		t.Fatal("without gh the view opens on Worktrees")
	}
	mm := m.(Model)
	for i, r := range mm.wt.rows {
		if r.wt != nil && filepath.Base(r.wt.Path) == "PAY-412" {
			mm.wt.cur.idx = i
		}
	}
	if m = key(mm, "C"); m.(Model).rev == nil {
		t.Error("C should open the changes of the worktree")
	}
	m = key(m, "esc")
	if out := screen(m); !strings.Contains(out, "PAY-412") || !strings.Contains(out, "Sessions here") {
		t.Errorf("the worktrees and their detail:\n%s", out)
	}
	m = key(m, "left")
	if gitOf(m).cur != gsReviews {
		t.Errorf("← walks the sections: %d", gitOf(m).cur)
	}
}

// The reviews waiting show on the top bar, unless header.reviews is off.
func TestReviewsBadge(t *testing.T) {
	f := useGH(t)
	m := key(ghTick(boot(t, 150, 42)), "1")
	head := strings.Split(screen(m), "\n")[:4]
	if !strings.Contains(strings.Join(head, "\n"), "⚑ 3 reviews waiting") {
		t.Errorf("the badge:\n%s", strings.Join(head, "\n"))
	}
	viewOf(m)
	var hit keyHit
	for _, h := range m.(Model).vs.keyHits {
		if h.key == secKey(tabGit, gsReviews) {
			hit = h
		}
	}
	if hit.key == "" {
		t.Fatal("the badge is a click target")
	}
	m = click(m, hit.x0+1, hit.y)
	if g := gitOf(m); !m.(Model).onTab(tabGit) || g.cur != gsReviews || len(g.rev.items) != 3 {
		t.Errorf("a click opens Reviews: tab %v section %d", m.(Model).tab, g.cur)
	}
	if first := gitOf(m).rev.items[0]; first.Number != 10 {
		t.Errorf("the oldest waits first: #%d", first.Number)
	}

	f2 := useGH(t)
	m = key(ghTick(turnOff(boot(t, 150, 42), "header.reviews")), "1")
	if strings.Contains(screen(m), "reviews waiting") || f2.saw("review-requested") {
		t.Error("with header.reviews off there is no badge and no look for reviews")
	}
	_ = f
}

// X asks, s picks the method, y merges: with the exact call, and never --admin.
func TestMergeConfirm(t *testing.T) {
	f := useGH(t)
	m := key(boot(t, 150, 42), "3")
	m = key(key(m, "j"), "j") // #1, the ready one
	m = key(m, "X")
	if md := m.(Model).modal; md == nil || !strings.Contains(md.title, "Merge PR #1?") {
		t.Fatalf("X should ask: %+v", m.(Model).modal)
	}
	m = key(m, "s")
	m = key(m, "y")
	if !f.saw("pr merge 1 --repo o/ready --squash") || f.saw("--admin") || f.saw("--auto") {
		t.Errorf("calls: %v", f.calls)
	}
	if !strings.Contains(m.(Model).status, "merged #1") {
		t.Errorf("status %q", m.(Model).status)
	}
}

func TestMergeMethodAndAuto(t *testing.T) {
	f := useGH(t)
	m := key(boot(t, 150, 42), "3")
	m = key(m, "j") // #3: checks failing, not ready
	m = key(m, "X")
	if body := m.(Model).modal.body; !strings.Contains(body, "isn't ready") || !strings.Contains(body, "Method: squash") {
		t.Errorf("body: %s", body)
	}
	m = key(key(m, "b"), "y")
	if !f.saw("pr merge 3 --repo o/red --rebase --auto") || f.saw("--admin") {
		t.Errorf("a PR that isn't ready is not forced: %v", f.calls)
	}
	m = key(boot(t, 150, 42), "3") // #2: conflicts
	if m = key(m, "X"); m.(Model).modal != nil || !strings.Contains(m.(Model).status, "conflicts") {
		t.Errorf("a PR with conflicts is refused: %q", m.(Model).status)
	}
	if m = key(key(m, "j"), "j"); m.(Model).modal != nil {
		t.Fatal("no modal")
	}
	m = key(key(m, "X"), "n")
	if n := f.count("pr merge"); n != 1 {
		t.Errorf("n merges nothing: %d merges", n)
	}
}

// c opens a Claude on the PR: its URL is in what the agent is told.
func TestReviewAgent(t *testing.T) {
	useGH(t)
	m := withAgents(t, 150, 42)
	var tm tea.Model = m
	tm = key(tm, "3")
	tm = key(tm, "right") // Reviews
	if g := gitOf(tm); g.cur != gsReviews || len(g.rev.items) != 3 {
		t.Fatalf("section %d, %d reviews", g.cur, len(g.rev.items))
	}
	tm = key(tm, "c")
	fin := tm.(Model)
	t.Cleanup(func() {
		for _, r := range fin.allPanes() {
			r.pane.Close()
		}
	})
	found := false
	for _, r := range fin.allPanes() {
		if r.pane.Argv[0] == "claude" && strings.Contains(strings.Join(r.pane.Argv, " "), "https://github.com/o/b/pull/10") {
			found = true
		}
	}
	if !found {
		t.Errorf("a claude tab with the URL of the oldest PR should open; status %q", tm.(Model).status)
	}
}

func TestReviewPromptFills(t *testing.T) {
	p := gh.PR{Number: 7, URL: "https://github.com/o/r/pull/7", Repo: "o/r"}
	if got := reviewPrompt("see {url} #{number} in {repo}", p); got != "see https://github.com/o/r/pull/7 #7 in o/r" {
		t.Errorf("prompt: %q", got)
	}
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// A merged branch with no worktree shows up, d asks and deletes it, x deletes all the merged ones.
func TestGitBranches(t *testing.T) {
	useGH(t)
	m := boot(t, 150, 42)
	var root string
	for _, r := range m.(Model).repos {
		if r.Name == "acme-api" {
			root = r.Root
		}
	}
	if root == "" {
		t.Fatal("the demo has acme-api")
	}
	gitIn(t, root, "branch", "done-one")
	gitIn(t, root, "branch", "done-two")
	m = key(m, "3")
	m = key(m, "right")
	m = key(m, "right")
	m = key(m, "right") // Branches
	g := gitOf(m)
	if g.cur != gsBranches {
		t.Fatalf("section %d", g.cur)
	}
	out := screen(m)
	if !strings.Contains(out, "done-one") || !strings.Contains(out, "merged") {
		t.Errorf("the merged branches:\n%s", out)
	}
	for _, r := range g.br.rows {
		if r.Name == "main" || strings.Contains(r.Name, "PAY-412") {
			t.Errorf("the default branch and the ones with a worktree are left out: %s", r.Name)
		}
	}
	for i, r := range g.br.rows {
		if r.Name == "done-one" {
			g.br.cur.idx = i
		}
	}
	m = key(m, "d")
	if md := m.(Model).modal; md == nil || !strings.Contains(md.title, "done-one") {
		t.Fatalf("d asks first: %+v", m.(Model).modal)
	}
	m = key(m, "y")
	if branches := gitIn(t, root, "branch", "--list", "done-one"); strings.TrimSpace(branches) != "" {
		t.Errorf("done-one should be gone: %q", branches)
	}
	m = key(m, "x")
	if md := m.(Model).modal; md == nil || !strings.Contains(md.body, "done-two") {
		t.Fatalf("x lists what goes: %+v", m.(Model).modal)
	}
	key(m, "y")
	if branches := gitIn(t, root, "branch", "--list", "done-two"); strings.TrimSpace(branches) != "" {
		t.Errorf("done-two should be gone: %q", branches)
	}
}

// A branch with commits of its own is never deleted.
func TestGitBranchNotMergedIsKept(t *testing.T) {
	useGH(t)
	m := boot(t, 150, 42)
	var root string
	for _, r := range m.(Model).repos {
		if r.Name == "acme-api" {
			root = r.Root
		}
	}
	gitIn(t, root, "branch", "wip")
	wt := filepath.Join(t.TempDir(), "wip")
	gitIn(t, root, "worktree", "add", "-q", wt, "wip")
	if err := os.WriteFile(filepath.Join(wt, "x.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, wt, "add", ".")
	gitIn(t, wt, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "work")
	gitIn(t, root, "worktree", "remove", "--force", wt)
	m = key(m, "3")
	for range 3 {
		m = key(m, "right")
	}
	g := gitOf(m)
	for i, r := range g.br.rows {
		if r.Name == "wip" {
			g.br.cur.idx = i
			if r.done() {
				t.Error("wip has a commit of its own")
			}
		}
	}
	if m = key(m, "d"); m.(Model).modal != nil || !strings.Contains(m.(Model).status, "not merged") {
		t.Errorf("d refuses: %q", m.(Model).status)
	}
	if !strings.Contains(screen(m), "not pushed") {
		t.Error("it was never pushed, and the list says so")
	}
}

// The Profile has the week's numbers, t switches to the month, and the Jira hook fills the tickets.
func TestGitProfile(t *testing.T) {
	f := useGH(t)
	old := jiraClosedSince
	jiraClosedSince = nil
	t.Cleanup(func() { jiraClosedSince = old })
	m := key(boot(t, 150, 42), "3")
	for range 4 {
		m = key(m, "right")
	}
	if gitOf(m).cur != gsProfile {
		t.Fatalf("section %d", gitOf(m).cur)
	}
	out := screen(m)
	for _, w := range []string{"this week", "PRs merged", "3", "avg time to merge", "4.0h", "tickets closed", "—", "o/b"} {
		if !strings.Contains(out, w) {
			t.Errorf("profile lacks %q:\n%s", w, out)
		}
	}
	m = key(m, "t")
	if !gitOf(m).month || !strings.Contains(screen(m), "this month") {
		t.Error("t switches to the month")
	}
	if n := f.count("reviewed:search"); n != 2 {
		t.Errorf("the week and the month are two asks: %d", n)
	}
	jiraClosedSince = func(*Model, time.Time) (int, bool) { return 5, true }
	if !regexp.MustCompile(`tickets closed\s+5`).MatchString(screen(m)) {
		t.Error("the closed tickets come from Jira when it is set up")
	}
}

// A PR of the list opens the Jira issue of its branch through the hook.
func TestGitOpenJira(t *testing.T) {
	useGH(t)
	old := jiraBrowseURL
	t.Cleanup(func() { jiraBrowseURL = old })
	jiraBrowseURL = nil
	m := key(boot(t, 150, 42), "3")
	if m = key(m, "J"); !strings.Contains(m.(Model).status, "no Jira issue") {
		t.Errorf("without the hook: %q", m.(Model).status)
	}
}

// Every section fills the terminal exactly, in every language and at every size.
func TestGitSectionsFit(t *testing.T) {
	useGH(t)
	for _, lang := range i18n.Langs {
		for _, size := range [][2]int{{150, 42}, {100, 30}, {80, 24}} {
			m := bootIn(t, lang, size[0], size[1])
			m = key(m, "3")
			for i := range gitSecs {
				m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
				fits(t, m, size[0], size[1], fmt.Sprintf("%s git section %d", lang, gitOf(m).cur))
				m = drain(m, gitOf(m).due(ptr(m), true), 0)
				fits(t, m, size[0], size[1], fmt.Sprintf("%s git section %d loaded", lang, gitOf(m).cur))
				_ = i
			}
		}
	}
}

func ptr(m tea.Model) *Model { mm := m.(Model); return &mm }

// withOwnCommit makes a branch with a commit of its own, in a repo of the demo, and gives its tip.
func withOwnCommit(t *testing.T, root, name string) string {
	t.Helper()
	gitIn(t, root, "branch", name)
	wt := filepath.Join(t.TempDir(), "w")
	gitIn(t, root, "worktree", "add", "-q", wt, name)
	if err := os.WriteFile(filepath.Join(wt, name+".txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, wt, "add", ".")
	gitIn(t, wt, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "work")
	gitIn(t, root, "worktree", "remove", "--force", wt)
	return strings.TrimSpace(gitIn(t, root, "rev-parse", name))
}

// A squash leaves git unsure, but a merged PR with this very commit says the branch is done.
func TestGitBranchMergedBySquash(t *testing.T) {
	f := useGH(t)
	m := boot(t, 150, 42)
	var root string
	for _, r := range m.(Model).repos {
		if r.Name == "acme-api" {
			root = r.Root
		}
	}
	gitIn(t, root, "remote", "add", "origin", "git@github.com:o/acme.git")
	tip := withOwnCommit(t, root, "feat-sq")
	other := withOwnCommit(t, root, "feat-reused")
	node := func(n int, head, sha string) string {
		return `{"number":` + strconv.Itoa(n) + `,"headRefName":"` + head + `","headRefOid":"` + sha + `","mergedAt":"2026-10-29T10:00:00Z","repository":{"nameWithOwner":"o/acme"}}`
	}
	f.merged = `{"data":{"search":{"nodes":[` + node(8, "feat-sq", tip) + `,` + node(9, "feat-reused", "0000000000000000000000000000000000000000") + `]}}}`
	_ = other
	m = key(m, "3")
	for range 3 {
		m = key(m, "right")
	}
	g := gitOf(m)
	for i, r := range g.br.rows {
		switch r.Name {
		case "feat-sq":
			g.br.cur.idx = i
			if r.prMerged != 8 || !r.force() {
				t.Errorf("PR #8 merged that commit: %+v", r)
			}
		case "feat-reused":
			if r.done() {
				t.Error("a branch with a commit the PR doesn't have is not done")
			}
		}
	}
	if !strings.Contains(screen(m), "PR #8 merged") {
		t.Error("the list says which PR merged it")
	}
	m = key(m, "d")
	if md := m.(Model).modal; md == nil || !strings.Contains(md.body, "squash") {
		t.Fatalf("d says why it is forced: %+v", m.(Model).modal)
	}
	key(m, "y")
	if strings.TrimSpace(gitIn(t, root, "branch", "--list", "feat-sq")) != "" || strings.TrimSpace(gitIn(t, root, "branch", "--list", "feat-reused")) == "" {
		t.Error("only the squash-merged branch goes")
	}
}
