package config

import "strings"

// Git is the [git] table.
type Git struct {
	MergeMethod  string `toml:"merge_method,omitempty"`  // squash | merge | rebase
	Reviews      string `toml:"reviews,omitempty"`       // all (teams too) | direct (asked of you)
	ReviewMins   int    `toml:"review_mins,omitempty"`   // minutes between looks for reviews, from anywhere
	ReviewPrompt string `toml:"review_prompt,omitempty"` // what the review agent is told: {url} {number} {repo}
}

// Merge methods and review modes, as [git] takes them.
const (
	MergeSquash   = "squash"
	MergeCommit   = "merge"
	MergeRebase   = "rebase"
	ReviewsAll    = "all"
	ReviewsDirect = "direct"
)

// DefaultReviewPrompt is what the review agent is told when [git] review_prompt is empty.
const DefaultReviewPrompt = "Review the pull request {url} (read it with: gh pr diff {number} -R {repo}). " +
	"Check correctness, tests and risks, and tell me what you find. Do not post anything on GitHub."

// Method is the merge method, squash unless set to another one.
func (g Git) Method() string {
	switch m := strings.ToLower(g.MergeMethod); m {
	case MergeCommit, MergeRebase:
		return m
	}
	return MergeSquash
}

// ReviewMode is all unless set to direct.
func (g Git) ReviewMode() string {
	if strings.ToLower(g.Reviews) == ReviewsDirect {
		return ReviewsDirect
	}
	return ReviewsAll
}

// ReviewEvery is the minutes between looks for reviews: 5 unless set (1 to 60).
func (g Git) ReviewEvery() int {
	if g.ReviewMins <= 0 {
		return 5
	}
	return min(60, g.ReviewMins)
}

// Prompt is what the review agent is told.
func (g Git) Prompt() string {
	if strings.TrimSpace(g.ReviewPrompt) == "" {
		return DefaultReviewPrompt
	}
	return g.ReviewPrompt
}
