package gh

import (
	"context"
	"encoding/json"
	"sort"
	"time"
)

// PR is a pull request as the search shows it.
type PR struct {
	Number     int
	Title      string
	URL        string
	Repo       string // owner/name
	Author     string
	Head, Base string
	Draft      bool
	Fork       bool   // its branch lives in another repo
	HeadSHA    string // the last commit of its branch
	Created    time.Time
	Updated    time.Time
	Merged     time.Time // zero while open
	Additions  int
	Deletions  int
	Files      int
	Mergeable  string // MERGEABLE | CONFLICTING | UNKNOWN
	MergeState string // CLEAN | BLOCKED | DIRTY | BEHIND | UNSTABLE | DRAFT | HAS_HOOKS | UNKNOWN
	Decision   string // APPROVED | CHANGES_REQUESTED | REVIEW_REQUIRED | ""
	Checks     string // SUCCESS | FAILURE | ERROR | PENDING | EXPECTED | "" (none)
	Reviews    []Review
	Requested  []string // who was asked: logins, and team names
}

// Review is the latest review of one person.
type Review struct {
	Author string
	State  string // APPROVED | CHANGES_REQUESTED | COMMENTED | DISMISSED | PENDING
}

// Ready is a PR that can be merged now: nothing blocks it, it is approved or
// needs no approval, and its checks pass (or it has none).
func (p PR) Ready() bool {
	return p.MergeState == "CLEAN" && (p.Decision == "APPROVED" || p.Decision == "") &&
		(p.Checks == "SUCCESS" || p.Checks == "") && !p.Draft
}

// Conflicts is a PR that cannot merge until its branch is brought up to date.
func (p PR) Conflicts() bool { return p.Mergeable == "CONFLICTING" || p.MergeState == "DIRTY" }

// Unknown is a PR whose mergeability GitHub is still working out.
func (p PR) Unknown() bool { return p.Mergeable == "UNKNOWN" || p.MergeState == "UNKNOWN" }

// ChecksFailing says a check of the PR failed.
func (p PR) ChecksFailing() bool { return p.Checks == "FAILURE" || p.Checks == "ERROR" }

const prQuery = `query($q:String!){search(query:$q,type:ISSUE,first:50){nodes{... on PullRequest{` +
	`number title url isDraft isCrossRepository headRefOid createdAt updatedAt mergedAt headRefName baseRefName additions deletions changedFiles ` +
	`repository{nameWithOwner} author{login} mergeable mergeStateStatus reviewDecision ` +
	`latestReviews(first:10){nodes{state author{login}}} ` +
	`reviewRequests(first:10){nodes{requestedReviewer{__typename ... on User{login} ... on Team{name}}}} ` +
	`commits(last:1){nodes{commit{statusCheckRollup{state}}}}}}}}`

type rawPR struct {
	Number      int        `json:"number"`
	Title       string     `json:"title"`
	URL         string     `json:"url"`
	IsDraft     bool       `json:"isDraft"`
	Cross       bool       `json:"isCrossRepository"`
	HeadOid     string     `json:"headRefOid"`
	MergedAt    *time.Time `json:"mergedAt"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	HeadRefName string     `json:"headRefName"`
	BaseRefName string     `json:"baseRefName"`
	Additions   int        `json:"additions"`
	Deletions   int        `json:"deletions"`
	Changed     int        `json:"changedFiles"`
	Repository  struct {
		Name string `json:"nameWithOwner"`
	} `json:"repository"`
	Author *struct {
		Login string `json:"login"`
	} `json:"author"`
	Mergeable      string `json:"mergeable"`
	MergeState     string `json:"mergeStateStatus"`
	ReviewDecision string `json:"reviewDecision"`
	LatestReviews  struct {
		Nodes []struct {
			State  string `json:"state"`
			Author *struct {
				Login string `json:"login"`
			} `json:"author"`
		} `json:"nodes"`
	} `json:"latestReviews"`
	ReviewRequests struct {
		Nodes []struct {
			Who *struct {
				Login string `json:"login"`
				Name  string `json:"name"`
			} `json:"requestedReviewer"`
		} `json:"nodes"`
	} `json:"reviewRequests"`
	Commits struct {
		Nodes []struct {
			Commit struct {
				Rollup *struct {
					State string `json:"state"`
				} `json:"statusCheckRollup"`
			} `json:"commit"`
		} `json:"nodes"`
	} `json:"commits"`
}

func (r rawPR) pr() PR {
	p := PR{Number: r.Number, Title: r.Title, URL: r.URL, Repo: r.Repository.Name, Head: r.HeadRefName, Base: r.BaseRefName,
		Draft: r.IsDraft, Fork: r.Cross, HeadSHA: r.HeadOid, Created: r.CreatedAt, Updated: r.UpdatedAt, Additions: r.Additions, Deletions: r.Deletions,
		Files: r.Changed, Mergeable: r.Mergeable, MergeState: r.MergeState, Decision: r.ReviewDecision}
	if r.MergedAt != nil {
		p.Merged = *r.MergedAt
	}
	if r.Author != nil {
		p.Author = r.Author.Login
	}
	if len(r.Commits.Nodes) > 0 && r.Commits.Nodes[0].Commit.Rollup != nil {
		p.Checks = r.Commits.Nodes[0].Commit.Rollup.State
	}
	for _, n := range r.LatestReviews.Nodes {
		if n.Author != nil {
			p.Reviews = append(p.Reviews, Review{n.Author.Login, n.State})
		}
	}
	for _, n := range r.ReviewRequests.Nodes {
		switch {
		case n.Who == nil:
		case n.Who.Login != "":
			p.Requested = append(p.Requested, n.Who.Login)
		case n.Who.Name != "":
			p.Requested = append(p.Requested, n.Who.Name)
		}
	}
	return p
}

// mergedQuery asks for little: a hundred merged PRs with the full fields is too much for GitHub (502).
const mergedQuery = `query($q:String!){search(query:$q,type:ISSUE,first:100){nodes{... on PullRequest{` +
	`number url isCrossRepository headRefOid mergedAt headRefName baseRefName repository{nameWithOwner}}}}}`

// search runs a PR search; the empty nodes (issues, not PRs) are skipped.
func search(ctx context.Context, query, q string) ([]PR, error) {
	out, err := graphql(ctx, query, map[string]string{"q": q})
	if err != nil {
		return nil, err
	}
	var res struct {
		Data struct {
			Search struct {
				Nodes []rawPR `json:"nodes"`
			} `json:"search"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return nil, err
	}
	var prs []PR
	for _, n := range res.Data.Search.Nodes {
		if n.Number != 0 {
			prs = append(prs, n.pr())
		}
	}
	return prs, nil
}

// MyPRs are the open pull requests of login, the last touched first.
func MyPRs(ctx context.Context, login string) ([]PR, error) {
	prs, err := search(ctx, prQuery, "is:pr is:open author:"+login+" archived:false sort:updated-desc")
	sort.SliceStable(prs, func(i, j int) bool { return prs[i].Updated.After(prs[j].Updated) })
	return prs, err
}

// ReviewRequests are the open pull requests waiting for login's review, the
// oldest first. direct leaves out the ones asked of a team only.
func ReviewRequests(ctx context.Context, login string, direct bool) ([]PR, error) {
	qual := "review-requested:"
	if direct {
		qual = "user-review-requested:"
	}
	prs, err := search(ctx, prQuery, "is:pr is:open "+qual+login+" archived:false sort:created-asc")
	sort.SliceStable(prs, func(i, j int) bool { return prs[i].Created.Before(prs[j].Created) })
	return prs, err
}

// MergedPRs are the pull requests of login that merged lately, the newest
// first: they say which local branches are done even when the merge was a squash.
func MergedPRs(ctx context.Context, login string) ([]PR, error) {
	prs, err := search(ctx, mergedQuery, "is:pr is:merged author:"+login+" archived:false sort:updated-desc")
	sort.SliceStable(prs, func(i, j int) bool { return prs[i].Merged.After(prs[j].Merged) })
	return prs, err
}
