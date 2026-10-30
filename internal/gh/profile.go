package gh

import (
	"context"
	"encoding/json"
	"sort"
	"time"
)

// RepoCount is a repository and how many things you did in it.
type RepoCount struct {
	Repo string
	N    int
}

// Profile is what you did in a stretch of time.
type Profile struct {
	Login     string
	From, To  time.Time
	Opened    int           // PRs you opened
	Merged    int           // PRs of yours that merged
	Reviews   int           // PRs you reviewed (not yours)
	Commits   int           // commits, when GitHub tells
	AvgMerge  time.Duration // mean time from opening to merge, of the merged ones
	Commited  []RepoCount   // commits by repo
	Authored  []RepoCount   // PRs by repo
	Reviewed  []RepoCount   // reviews by repo
	Contribed bool          // the contributions API answered (it may not, behind SSO)
}

const searchQuery = `query($m:String!,$c:String!,$r:String!){` +
	`merged:search(query:$m,type:ISSUE,first:100){issueCount nodes{... on PullRequest{createdAt mergedAt}}} ` +
	`created:search(query:$c,type:ISSUE,first:1){issueCount} ` +
	`reviewed:search(query:$r,type:ISSUE,first:1){issueCount}}`

const contribQuery = `query($from:DateTime!,$to:DateTime!){viewer{contributionsCollection(from:$from,to:$to){` +
	`totalCommitContributions totalPullRequestContributions totalPullRequestReviewContributions ` +
	`commitContributionsByRepository(maxRepositories:10){repository{nameWithOwner} contributions{totalCount}} ` +
	`pullRequestContributionsByRepository(maxRepositories:10){repository{nameWithOwner} contributions{totalCount}} ` +
	`pullRequestReviewContributionsByRepository(maxRepositories:10){repository{nameWithOwner} contributions{totalCount}}}}}`

type byRepo []struct {
	Repository struct {
		Name string `json:"nameWithOwner"`
	} `json:"repository"`
	Contributions struct {
		N int `json:"totalCount"`
	} `json:"contributions"`
}

func (b byRepo) counts() []RepoCount {
	var out []RepoCount
	for _, r := range b {
		if r.Contributions.N > 0 {
			out = append(out, RepoCount{r.Repository.Name, r.Contributions.N})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].N > out[j].N })
	return out
}

// Week is the Monday (00:00 local) of the week of t.
func Week(t time.Time) time.Time {
	d := (int(t.Weekday()) + 6) % 7
	y, m, day := t.AddDate(0, 0, -d).Date()
	return time.Date(y, m, day, 0, 0, 0, 0, t.Location())
}

// Month is the first of the month of t, 00:00 local.
func Month(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
}

// Mine reads your profile from from to now. The searches carry it; the
// contributions API adds the repos and the commits when it answers (it may
// not, behind an organization's SSO), and then the search counts stand.
func Mine(ctx context.Context, login string, from, to time.Time) (Profile, error) {
	p := Profile{Login: login, From: from, To: to}
	day := from.UTC().Format("2006-01-02")
	out, err := graphql(ctx, searchQuery, map[string]string{
		"m": "is:pr author:" + login + " merged:>=" + day,
		"c": "is:pr author:" + login + " created:>=" + day,
		"r": "is:pr reviewed-by:" + login + " -author:" + login + " updated:>=" + day,
	})
	if err != nil {
		return p, err
	}
	var s struct {
		Data struct {
			Merged struct {
				Count int `json:"issueCount"`
				Nodes []struct {
					Created time.Time `json:"createdAt"`
					Merged  time.Time `json:"mergedAt"`
				} `json:"nodes"`
			} `json:"merged"`
			Created struct {
				Count int `json:"issueCount"`
			} `json:"created"`
			Reviewed struct {
				Count int `json:"issueCount"`
			} `json:"reviewed"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &s); err != nil {
		return p, err
	}
	p.Merged, p.Opened, p.Reviews = s.Data.Merged.Count, s.Data.Created.Count, s.Data.Reviewed.Count
	var total time.Duration
	var n int
	for _, r := range s.Data.Merged.Nodes {
		if !r.Merged.IsZero() && r.Merged.After(r.Created) {
			total += r.Merged.Sub(r.Created)
			n++
		}
	}
	if n > 0 {
		p.AvgMerge = total / time.Duration(n)
	}
	p.contributions(ctx, from, to)
	return p, nil
}

func (p *Profile) contributions(ctx context.Context, from, to time.Time) {
	out, err := graphql(ctx, contribQuery, map[string]string{
		"from": from.UTC().Format(time.RFC3339), "to": to.UTC().Format(time.RFC3339)})
	if err != nil {
		return
	}
	var c struct {
		Data struct {
			Viewer struct {
				C struct {
					Commits int    `json:"totalCommitContributions"`
					PRs     int    `json:"totalPullRequestContributions"`
					Reviews int    `json:"totalPullRequestReviewContributions"`
					ByCom   byRepo `json:"commitContributionsByRepository"`
					ByPR    byRepo `json:"pullRequestContributionsByRepository"`
					ByRev   byRepo `json:"pullRequestReviewContributionsByRepository"`
				} `json:"contributionsCollection"`
			} `json:"viewer"`
		} `json:"data"`
	}
	if json.Unmarshal(out, &c) != nil {
		return
	}
	v := c.Data.Viewer.C
	if v.Commits+v.PRs+v.Reviews == 0 { // an organization's SSO answers with zeros
		return
	}
	p.Contribed = true
	p.Commits = v.Commits
	p.Reviews = max(p.Reviews, v.Reviews)
	p.Commited, p.Authored, p.Reviewed = v.ByCom.counts(), v.ByPR.counts(), v.ByRev.counts()
}
