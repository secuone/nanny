package gh

import (
	"context"
	"strconv"
	"strings"
)

// Merge methods, as the Merge call takes them.
const (
	Squash      = "squash"
	MergeCommit = "merge"
	Rebase      = "rebase"
)

// MergeArgs is the gh call that merges a PR. It never forces: no --admin, so
// a PR that is not ready waits (auto) or is refused.
func MergeArgs(repo string, n int, method string, auto bool) []string {
	switch method {
	case MergeCommit, Rebase:
	default:
		method = Squash
	}
	args := []string{"pr", "merge", strconv.Itoa(n), "--repo", repo, "--" + method}
	if auto {
		args = append(args, "--auto")
	}
	return args
}

// Merge merges pull request n of repo; auto asks GitHub to merge it when its
// requirements are met.
func Merge(ctx context.Context, repo string, n int, method string, auto bool) error {
	_, err := Exec(ctx, MergeArgs(repo, n, method, auto)...)
	return err
}

// DiffCmd is the command that shows a PR's diff in a pager, to run in a tab.
func DiffCmd(repo string, n int) string {
	return "gh pr diff " + strconv.Itoa(n) + " -R " + shellWord(repo) + " --color=always | less -R"
}

func shellWord(s string) string {
	if strings.ContainsAny(s, " \t\n'\"\\$`!*?[](){}<>|&;#~") {
		return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
	}
	return s
}

// Checkout puts pull request n of repo in the checkout at dir (for a fork:
// gh sets the remote up).
func Checkout(ctx context.Context, dir, repo string, n int) error {
	_, err := ExecDir(ctx, dir, "pr", "checkout", strconv.Itoa(n), "-R", repo)
	return err
}
