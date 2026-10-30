package gitwt

import (
	"strconv"
	"strings"
	"time"
)

// Branch is a local branch and where it stands.
type Branch struct {
	Name     string
	Tip      string // its last commit
	Upstream string // "" = never pushed
	Ahead    int    // commits not on the upstream
	Behind   int
	Gone     bool // its upstream was deleted
	Merged   bool // all of it is in the default branch
	When     time.Time
	Worktree string // where it is checked out, "" = nowhere
}

// DefaultBranch is what the repo's pull requests go to: origin's HEAD, else main or master.
func DefaultBranch(root string) string {
	if s, err := git(root, "symbolic-ref", "--short", "-q", "refs/remotes/origin/HEAD"); err == nil && s != "" {
		return strings.TrimPrefix(s, "origin/")
	}
	for _, c := range []string{"main", "master"} {
		if _, err := git(root, "rev-parse", "--verify", "-q", "refs/heads/"+c); err == nil {
			return c
		}
		if _, err := git(root, "rev-parse", "--verify", "-q", "refs/remotes/origin/"+c); err == nil {
			return c
		}
	}
	return ""
}

// Branches lists the local branches of the repo at root, the newest first.
func Branches(root string) []Branch {
	out, err := git(root, "for-each-ref", "--sort=-committerdate", "--format=%(refname:short)\x1f%(upstream:short)\x1f%(upstream:track)\x1f%(committerdate:unix)\x1f%(worktreepath)\x1f%(objectname)", "refs/heads")
	if err != nil || out == "" {
		return nil
	}
	merged := mergedInto(root)
	var bs []Branch
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, "\x1f")
		if len(f) < 6 {
			continue
		}
		b := Branch{Name: f[0], Upstream: f[1], Worktree: f[4], Tip: f[5], Merged: merged[f[0]]}
		b.Ahead, b.Behind, b.Gone = parseTrack(f[2])
		if ts, err := strconv.ParseInt(f[3], 10, 64); err == nil {
			b.When = time.Unix(ts, 0)
		}
		bs = append(bs, b)
	}
	return bs
}

// mergedInto is the branches that are all in the default branch, here or on
// origin; the default branch itself is not one.
func mergedInto(root string) map[string]bool {
	d := DefaultBranch(root)
	set := map[string]bool{}
	if d == "" {
		return set
	}
	for _, ref := range []string{d, "origin/" + d} {
		out, err := git(root, "branch", "--merged", ref, "--format=%(refname:short)")
		if err != nil {
			continue
		}
		for _, n := range strings.Split(out, "\n") {
			if n = strings.TrimSpace(n); n != "" {
				set[n] = true
			}
		}
	}
	delete(set, d)
	return set
}

// parseTrack reads "[ahead 1, behind 2]" or "[gone]".
func parseTrack(s string) (ahead, behind int, gone bool) {
	s = strings.Trim(s, "[]")
	for _, p := range strings.Split(s, ",") {
		f := strings.Fields(p)
		switch {
		case len(f) == 1 && f[0] == "gone":
			gone = true
		case len(f) == 2 && f[0] == "ahead":
			ahead, _ = strconv.Atoi(f[1])
		case len(f) == 2 && f[0] == "behind":
			behind, _ = strconv.Atoi(f[1])
		}
	}
	return
}

// DeleteBranch deletes a local branch. Without force, git refuses one that is
// not merged.
func DeleteBranch(root, name string, force bool) error {
	flag := "-d"
	if force {
		flag = "-D"
	}
	_, err := gitErr(root, "branch", flag, name)
	return err
}

// Push publishes a branch and sets its upstream.
func Push(dir, branch string) error {
	_, err := gitErr(dir, "push", "-u", "origin", branch)
	return err
}

// Slug is owner/name of the GitHub repo behind origin, "" if it is not one.
func Slug(root string) string {
	url, err := git(root, "remote", "get-url", "origin")
	if err != nil {
		return ""
	}
	return ParseSlug(url)
}

// ParseSlug is owner/name of a GitHub remote URL (https, git@host:o/r or
// ssh://, with or without .git), "" if it is not a GitHub one.
func ParseSlug(url string) string {
	url = strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(url), "/"), ".git")
	i := strings.LastIndexAny(url, ":/")
	if i < 0 {
		return ""
	}
	rest := url[:i]
	j := strings.LastIndexAny(rest, ":/")
	if j < 0 || !strings.Contains(url, "github.com") {
		return ""
	}
	return rest[j+1:] + "/" + url[i+1:]
}

// Slugs are the GitHub repos behind origin and upstream of the repo that
// holds dir (a worktree or a subfolder of it). ok is false when dir is not
// in a git repo.
func Slugs(dir string) (slugs []string, ok bool) {
	top := Toplevel(dir)
	if top == "" {
		return nil, false
	}
	for _, remote := range []string{"origin", "upstream"} {
		if url, err := git(top, "remote", "get-url", remote); err == nil {
			if s := ParseSlug(url); s != "" {
				slugs = append(slugs, s)
			}
		}
	}
	return slugs, true
}

// FetchBranch brings one branch of origin.
func FetchBranch(root, branch string) error {
	_, err := gitErr(root, "fetch", "origin", branch)
	return err
}
