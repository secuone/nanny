package gh

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// fixtures answers gh the way GitHub would, by the query it is asked.
func fixtures(t *testing.T, calls *[][]string) Runner {
	t.Helper()
	read := func(name string) []byte {
		b, err := os.ReadFile("testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	return func(_ context.Context, args ...string) ([]byte, error) {
		*calls = append(*calls, args)
		all := strings.Join(args, " ")
		switch {
		case strings.Contains(all, "api user"):
			return []byte("me\n"), nil
		case strings.Contains(all, "is:merged"):
			return read("merged.json"), nil
		case strings.Contains(all, "review-requested:"):
			return read("reviews.json"), nil
		case strings.Contains(all, "author:me archived:false"):
			return read("mine.json"), nil
		case strings.Contains(all, "issueCount") && strings.Contains(all, "reviewed:search"):
			return read("search.json"), nil
		case strings.Contains(all, "contributionsCollection"):
			return read("contrib.json"), nil
		}
		return nil, errors.New("unexpected call: " + all)
	}
}

func use(t *testing.T) *[][]string {
	t.Helper()
	var calls [][]string
	old := Exec
	Exec = fixtures(t, &calls)
	t.Cleanup(func() { Exec = old })
	return &calls
}

func TestViewer(t *testing.T) {
	use(t)
	if l, err := Viewer(context.Background()); err != nil || l != "me" {
		t.Fatalf("viewer %q %v", l, err)
	}
}

func TestMyPRsReadyConflictsAndChecks(t *testing.T) {
	calls := use(t)
	prs, err := MyPRs(context.Background(), "me")
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 4 {
		t.Fatalf("the empty node is no PR: %d", len(prs))
	}
	if !strings.Contains(strings.Join((*calls)[0], " "), "is:pr is:open author:me archived:false sort:updated-desc") {
		t.Errorf("query: %v", (*calls)[0])
	}
	by := map[int]PR{}
	for _, p := range prs {
		by[p.Number] = p
	}
	if prs[0].Number != 2 {
		t.Errorf("the last touched comes first: %d", prs[0].Number)
	}
	if !by[1].Ready() || by[1].Conflicts() || by[1].Repo != "o/ready" || by[1].Reviews[0].Author != "ana" {
		t.Errorf("1 should be ready: %+v", by[1])
	}
	if by[2].Ready() || !by[2].Conflicts() {
		t.Errorf("2 has conflicts: %+v", by[2])
	}
	if by[3].Ready() || !by[3].ChecksFailing() || strings.Join(by[3].Requested, ",") != "bob,FLOW" {
		t.Errorf("3 has failing checks and two requests: %+v", by[3])
	}
	if !by[4].Unknown() || by[4].Ready() || !by[4].Draft || by[4].Checks != "" {
		t.Errorf("4 is still being worked out: %+v", by[4])
	}
}

func TestReviewRequestsOldestFirst(t *testing.T) {
	calls := use(t)
	prs, err := ReviewRequests(context.Background(), "me", false)
	if err != nil {
		t.Fatal(err)
	}
	var got []int
	for _, p := range prs {
		got = append(got, p.Number)
	}
	if len(got) != 3 || got[0] != 10 || got[1] != 20 || got[2] != 30 {
		t.Errorf("oldest first: %v", got)
	}
	if _, err := ReviewRequests(context.Background(), "me", true); err != nil {
		t.Fatal(err)
	}
	if q := strings.Join((*calls)[1], " "); !strings.Contains(q, " user-review-requested:me ") {
		t.Errorf("direct asks for you alone: %s", q)
	}
}

func TestProfileMetrics(t *testing.T) {
	use(t)
	from := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	p, err := Mine(context.Background(), "me", from, from.AddDate(0, 0, 4))
	if err != nil {
		t.Fatal(err)
	}
	if p.Merged != 3 || p.Opened != 5 || p.Reviews != 11 || p.Commits != 9 || !p.Contribed {
		t.Errorf("counts: %+v", p)
	}
	if p.AvgMerge != 4*time.Hour {
		t.Errorf("mean of 2h and 6h; a PR merged at once is left out: %v", p.AvgMerge)
	}
	if len(p.Commited) != 2 || p.Commited[0].Repo != "o/b" || len(p.Reviewed) != 1 {
		t.Errorf("repos: %+v %+v", p.Commited, p.Reviewed)
	}
}

func TestProfileWithoutContributions(t *testing.T) {
	use(t)
	old := Exec
	Exec = func(ctx context.Context, args ...string) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), "contributionsCollection") {
			return nil, errors.New("SSO required")
		}
		return old(ctx, args...)
	}
	p, err := Mine(context.Background(), "me", time.Now().AddDate(0, 0, -3), time.Now())
	if err != nil || p.Contribed || p.Reviews != 7 || p.Opened != 5 {
		t.Errorf("the search counts stand: %+v %v", p, err)
	}
}

func TestMergeNeverForces(t *testing.T) {
	got := strings.Join(MergeArgs("o/r", 7, "squash", false), " ")
	if got != "pr merge 7 --repo o/r --squash" {
		t.Errorf("merge: %s", got)
	}
	auto := strings.Join(MergeArgs("o/r", 7, "rebase", true), " ")
	if auto != "pr merge 7 --repo o/r --rebase --auto" || strings.Contains(auto, "admin") {
		t.Errorf("auto: %s", auto)
	}
	if !strings.Contains(strings.Join(MergeArgs("o/r", 7, "bogus", false), " "), "--squash") {
		t.Error("an unknown method is a squash")
	}
}

func TestNoSession(t *testing.T) {
	for err, want := range map[error]bool{ErrNoGH: true, ErrNoAuth: true, &Error{"To get started with GitHub CLI, please run: gh auth login"}: true,
		&Error{"You are not logged in"}: true, &Error{"HTTP 502"}: false, nil: false} {
		if NoSession(err) != want {
			t.Errorf("%v should be %v", err, want)
		}
	}
}

func TestDiffCmd(t *testing.T) {
	if c := DiffCmd("o/r", 3); c != "gh pr diff 3 -R o/r --color=always | less -R" {
		t.Error(c)
	}
}

func TestWeekStartsOnMonday(t *testing.T) {
	sun := time.Date(2026, 11, 1, 15, 0, 0, 0, time.UTC)
	if w := Week(sun); w.Weekday() != time.Monday || w.Day() != 26 {
		t.Errorf("week of a Sunday: %v", w)
	}
}

// A merged PR carries what says which local branch it was: name and last commit.
func TestMergedPRs(t *testing.T) {
	calls := use(t)
	prs, err := MergedPRs(context.Background(), "me")
	if err != nil || len(prs) != 2 {
		t.Fatalf("%v %v", prs, err)
	}
	if prs[0].Number != 8 || prs[0].Head != "feat/done" || prs[0].HeadSHA != "abc123" || prs[0].Repo != "o/r" || prs[0].Merged.IsZero() {
		t.Errorf("newest merge first: %+v", prs[0])
	}
	if q := strings.Join((*calls)[0], " "); !strings.Contains(q, "is:pr is:merged author:me") || strings.Contains(q, "statusCheckRollup") {
		t.Errorf("a light query: %s", q)
	}
}
