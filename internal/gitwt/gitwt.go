// Package gitwt discovers git repositories and their worktrees.
package gitwt

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Worktree struct {
	Path     string
	Branch   string
	Head     string
	Main     bool // the primary checkout
	Locked   bool
	Prunable bool
	Missing  bool // directory no longer exists
	Dirty    int  // modified/untracked files
	Ahead    int
	Behind   int
	LastMsg  string
	LastWhen time.Time
}

type Repo struct {
	Root      string // path of the main worktree
	Name      string
	Worktrees []Worktree
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	return strings.TrimRight(string(out), "\n"), err
}

// RepoRoot returns the main worktree path for any path inside a repo (or worktree).
func RepoRoot(dir string) string {
	for d := dir; d != "/" && d != "."; d = filepath.Dir(d) {
		if _, err := os.Stat(d); err == nil {
			common, err := git(d, "rev-parse", "--path-format=absolute", "--git-common-dir")
			if err != nil {
				return ""
			}
			return filepath.Dir(common)
		}
	}
	return ""
}

// Discover resolves the set of repos for the given paths (deduped by root) and lists worktrees concurrently.
func Discover(paths []string) []Repo {
	roots := map[string]bool{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, p := range paths {
		wg.Add(1)
		sem <- struct{}{}
		go func(p string) {
			defer wg.Done()
			defer func() { <-sem }()
			if r := RepoRoot(p); r != "" && !strings.Contains(r, "/.cache/") {
				mu.Lock()
				roots[r] = true
				mu.Unlock()
			}
		}(p)
	}
	wg.Wait()

	repos := make([]Repo, 0, len(roots))
	for r := range roots {
		wg.Add(1)
		sem <- struct{}{}
		go func(r string) {
			defer wg.Done()
			defer func() { <-sem }()
			rp := Repo{Root: r, Name: filepath.Base(r), Worktrees: List(r)}
			mu.Lock()
			repos = append(repos, rp)
			mu.Unlock()
		}(r)
	}
	wg.Wait()
	sort.Slice(repos, func(i, j int) bool {
		if len(repos[i].Worktrees) != len(repos[j].Worktrees) {
			return len(repos[i].Worktrees) > len(repos[j].Worktrees)
		}
		return repos[i].Name < repos[j].Name
	})
	return repos
}

// List parses `git worktree list --porcelain` and enriches each entry.
func List(root string) []Worktree {
	out, err := git(root, "worktree", "list", "--porcelain")
	if err != nil {
		return nil
	}
	var wts []Worktree
	var cur *Worktree
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			wts = append(wts, Worktree{Path: strings.TrimPrefix(line, "worktree ")})
			cur = &wts[len(wts)-1]
		case cur == nil:
		case strings.HasPrefix(line, "HEAD "):
			cur.Head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case line == "detached":
			cur.Branch = "(detached)"
		case strings.HasPrefix(line, "locked"):
			cur.Locked = true
		case strings.HasPrefix(line, "prunable"):
			cur.Prunable = true
		}
	}
	if len(wts) > 0 {
		wts[0].Main = true
	}
	var wg sync.WaitGroup
	for i := range wts {
		wg.Add(1)
		go func(w *Worktree) {
			defer wg.Done()
			enrich(w)
		}(&wts[i])
	}
	wg.Wait()
	return wts
}

func enrich(w *Worktree) {
	if _, err := os.Stat(w.Path); err != nil {
		w.Missing = true
		return
	}
	if s, err := git(w.Path, "status", "--porcelain"); err == nil && s != "" {
		w.Dirty = len(strings.Split(s, "\n"))
	}
	if s, err := git(w.Path, "rev-list", "--left-right", "--count", "@{upstream}...HEAD"); err == nil {
		f := strings.Fields(s)
		if len(f) == 2 {
			w.Behind, _ = strconv.Atoi(f[0])
			w.Ahead, _ = strconv.Atoi(f[1])
		}
	}
	if s, err := git(w.Path, "log", "-1", "--format=%ct\x1f%s"); err == nil {
		if p := strings.SplitN(s, "\x1f", 2); len(p) == 2 {
			ts, _ := strconv.ParseInt(p[0], 10, 64)
			w.LastWhen = time.Unix(ts, 0)
			w.LastMsg = p[1]
		}
	}
}

// Add creates a new worktree with a new branch next to the repo under .worktrees/<name>.
func Add(root, name, base string) (string, error) {
	path := worktreePath(root, name)
	args := []string{"worktree", "add", "-b", name, path}
	if base != "" {
		args = append(args, base)
	}
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", &CmdErr{strings.TrimSpace(string(out))}
	}
	return path, nil
}

// worktreePath is where a worktree named name goes: next to the repo.
func worktreePath(root, name string) string {
	return filepath.Join(filepath.Dir(root), filepath.Base(root)+".worktrees", name)
}

// AddExisting checks out a branch that exists in a new worktree next to the repo.
func AddExisting(root, branch string) (string, error) {
	path := worktreePath(root, branch)
	out, err := exec.Command("git", "-C", root, "worktree", "add", path, branch).CombinedOutput()
	if err != nil {
		return "", &CmdErr{strings.TrimSpace(string(out))}
	}
	return path, nil
}

// AddDetached makes a worktree on the repo's HEAD with no branch, for gh to
// put a pull request in.
func AddDetached(root, name string) (string, error) {
	path := worktreePath(root, name)
	out, err := exec.Command("git", "-C", root, "worktree", "add", "--detach", path).CombinedOutput()
	if err != nil {
		return "", &CmdErr{strings.TrimSpace(string(out))}
	}
	return path, nil
}

// Remove removes a worktree (never the main one). force discards local changes.
func Remove(root, path string, force bool) error {
	args := []string{"-C", root, "worktree", "remove", path}
	if force {
		args = append(args, "--force")
	}
	out, err := exec.Command("git", args...).CombinedOutput()
	if err != nil {
		return &CmdErr{strings.TrimSpace(string(out))}
	}
	return nil
}

func Prune(root string) error {
	out, err := exec.Command("git", "-C", root, "worktree", "prune").CombinedOutput()
	if err != nil {
		return &CmdErr{strings.TrimSpace(string(out))}
	}
	return nil
}

type CmdErr struct{ Msg string }

func (e *CmdErr) Error() string { return e.Msg }
