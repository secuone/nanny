package gitwt

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"
)

// PR is an open pull request of a branch, as gh reports it.
type PR struct {
	Number int
	Title  string
	URL    string
	Branch string
	Draft  bool
	Review string // APPROVED | CHANGES_REQUESTED | REVIEW_REQUIRED | ""
	Checks string // pass | fail | pending | "" (none)
}

// HasGH reports whether the gh CLI is installed.
func HasGH() bool {
	_, err := exec.LookPath("gh")
	return err == nil
}

// OpenPRs lists the open pull requests of the repo at root, by head branch.
func OpenPRs(root string) (map[string]PR, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", "pr", "list", "--state", "open", "--limit", "200",
		"--json", "number,title,url,headRefName,isDraft,reviewDecision,statusCheckRollup")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Number         int    `json:"number"`
		Title          string `json:"title"`
		URL            string `json:"url"`
		HeadRefName    string `json:"headRefName"`
		IsDraft        bool   `json:"isDraft"`
		ReviewDecision string `json:"reviewDecision"`
		Checks         []struct {
			Status     string `json:"status"`     // check runs: QUEUED | IN_PROGRESS | COMPLETED
			Conclusion string `json:"conclusion"` // SUCCESS | FAILURE | …
			State      string `json:"state"`      // status contexts: SUCCESS | FAILURE | PENDING | ERROR
		} `json:"statusCheckRollup"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, err
	}
	prs := make(map[string]PR, len(raw))
	for _, r := range raw {
		p := PR{Number: r.Number, Title: r.Title, URL: r.URL, Branch: r.HeadRefName, Draft: r.IsDraft, Review: r.ReviewDecision}
		for _, c := range r.Checks {
			v := c.Conclusion
			if v == "" {
				v = c.State
			}
			switch {
			case v == "FAILURE" || v == "ERROR" || v == "TIMED_OUT" || v == "CANCELLED" || v == "ACTION_REQUIRED":
				p.Checks = "fail"
			case p.Checks != "fail" && (c.Status == "QUEUED" || c.Status == "IN_PROGRESS" || v == "PENDING" || v == "EXPECTED"):
				p.Checks = "pending"
			case p.Checks == "":
				p.Checks = "pass"
			}
		}
		if _, ok := prs[p.Branch]; !ok { // several PRs from one branch: the newest (listed first)
			prs[p.Branch] = p
		}
	}
	return prs, nil
}

// Pull fast-forwards the checkout at dir from its upstream.
func Pull(dir string) error {
	_, err := gitErr(dir, "pull", "--ff-only")
	return err
}

// Fetch updates the remote-tracking branches of the repo at dir.
func Fetch(dir string) error {
	_, err := gitErr(dir, "fetch", "--prune")
	return err
}

// gitErr runs git in dir with the output as the error's message.
func gitErr(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(cmd.Environ(), "GIT_TERMINAL_PROMPT=0") // never wait on a password prompt
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", &CmdErr{strings.TrimSpace(string(out))}
	}
	return string(out), nil
}

// Status is a checkout's state against its upstream: dirty files, commits ahead
// and behind (after its last fetch). Cheap: no network.
func Status(dir string) (dirty, ahead, behind int, ok bool) {
	w := Worktree{Path: dir}
	enrich(&w)
	return w.Dirty, w.Ahead, w.Behind, !w.Missing
}
