package gitwt

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Change is one file in a diff.
type Change struct {
	Path   string
	Old    string // rename source
	Status string // M A D R ? (untracked)
	Add    int
	Del    int
	Binary bool
}

func gitRaw(dir string, args ...string) ([]byte, error) {
	return exec.Command("git", append([]string{"-C", dir, "-c", "core.quotepath=off"}, args...)...).Output()
}

// Base finds what a checkout branched from: origin's default branch, else
// main or master. It returns the ref name and its merge-base with HEAD, or
// empty strings when HEAD is that branch or nothing fits.
func Base(dir string) (name, sha string) {
	cands := []string{}
	if s, err := git(dir, "rev-parse", "--abbrev-ref", "origin/HEAD"); err == nil && s != "" && s != "origin/HEAD" {
		cands = append(cands, s)
	}
	cands = append(cands, "main", "master", "origin/main", "origin/master")
	head, _ := git(dir, "rev-parse", "HEAD")
	for _, c := range cands {
		ref, err := git(dir, "rev-parse", "--verify", "-q", c)
		if err != nil || ref == "" {
			continue
		}
		mb, err := git(dir, "merge-base", "HEAD", c)
		if err != nil || mb == "" || mb == head && ref == head {
			continue
		}
		return c, mb
	}
	return "", ""
}

// Changes lists files changed between base (a commit; "" = HEAD) and the
// working tree, untracked files included.
func Changes(dir, base string) ([]Change, error) {
	if base == "" {
		base = "HEAD"
	}
	ns, err := gitRaw(dir, "diff", "--name-status", "-M", "-z", base)
	if err != nil {
		return nil, err
	}
	num, _ := gitRaw(dir, "diff", "--numstat", "-M", "-z", base)
	var out []Change
	f := strings.Split(strings.TrimSuffix(string(ns), "\x00"), "\x00")
	for i := 0; i+1 < len(f); {
		c := Change{Status: f[i][:1]}
		if c.Status == "R" || c.Status == "C" {
			if i+2 >= len(f) {
				break
			}
			c.Old, c.Path = f[i+1], f[i+2]
			i += 3
		} else {
			c.Path = f[i+1]
			i += 2
		}
		out = append(out, c)
	}
	// numstat -z: "add\tdel\tpath\0" or, for renames, "add\tdel\t\0old\0new\0"
	n := strings.Split(string(num), "\x00")
	k := 0
	for i := 0; i < len(n) && k < len(out); i++ {
		parts := strings.SplitN(n[i], "\t", 3)
		if len(parts) < 3 {
			continue
		}
		if parts[0] == "-" {
			out[k].Binary = true
		}
		out[k].Add, _ = strconv.Atoi(parts[0])
		out[k].Del, _ = strconv.Atoi(parts[1])
		if parts[2] == "" {
			i += 2
		}
		k++
	}
	if u, err := gitRaw(dir, "ls-files", "--others", "--exclude-standard", "-z"); err == nil {
		for _, p := range strings.Split(string(u), "\x00") {
			if p == "" {
				continue
			}
			c := Change{Path: p, Status: "?"}
			if b, err := readHead(filepath.Join(dir, p)); err == nil {
				if bytes.IndexByte(b, 0) >= 0 {
					c.Binary = true
				} else {
					c.Add = bytes.Count(b, []byte("\n"))
				}
			}
			out = append(out, c)
		}
	}
	return out, nil
}

// FileDiff returns the unified diff of one change (an untracked file shows as all added).
func FileDiff(dir, base string, c Change) []string {
	if c.Status == "?" {
		if c.Binary {
			return []string{"Binary file, untracked"}
		}
		b, err := readHead(filepath.Join(dir, c.Path))
		if err != nil {
			return []string{err.Error()}
		}
		out := []string{"new file (untracked) " + c.Path, "@@ -0,0 +1," + strconv.Itoa(c.Add) + " @@"}
		for _, l := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") {
			out = append(out, "+"+l)
		}
		return out
	}
	if base == "" {
		base = "HEAD"
	}
	args := []string{"diff", "--no-color", "--no-ext-diff", "-M", base, "--"}
	if c.Old != "" {
		args = append(args, c.Old)
	}
	b, err := gitRaw(dir, append(args, c.Path)...)
	if err != nil {
		return []string{err.Error()}
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

// Commits lists "sha subject" for commits in base..HEAD, newest first.
func Commits(dir, base string, n int) []string {
	if base == "" {
		return nil
	}
	s, err := git(dir, "log", "--format=%h %s", "-n", strconv.Itoa(n), base+"..HEAD")
	if err != nil || s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// Head is the commit checked out in dir ("" outside a repo).
func Head(dir string) string {
	s, _ := git(dir, "rev-parse", "HEAD")
	return s
}

// Toplevel is the root of the checkout (worktree) containing dir.
func Toplevel(dir string) string {
	s, _ := git(dir, "rev-parse", "--show-toplevel")
	return s
}

// readHead reads up to 512 KB of a file.
func readHead(p string) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b := make([]byte, 512<<10)
	n, _ := io.ReadFull(f, b)
	return b[:n], nil
}
