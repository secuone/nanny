// Package discover finds the places you work in: folders where agents ran,
// git repositories under your usual code roots, and open workspaces.
package discover

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/secuone/nanny/internal/claude"
	"github.com/secuone/nanny/internal/gitwt"
)

type Place struct {
	Name     string
	Path     string // repo root (or folder)
	Sessions int
	Live     int
	Last     time.Time
	Sources  []string // "agents", "git", "workspace"
}

// DefaultRoots are scanned for git repos when the user doesn't configure any.
func DefaultRoots() []string {
	h, _ := os.UserHomeDir()
	var out []string
	for _, d := range []string{"dev", "src", "code", "projects", "work", "repos", "git", "Desktop", "Documents", "workspace", "go/src"} {
		p := filepath.Join(h, d)
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			out = append(out, p)
		}
	}
	return out
}

// Find merges every signal into places keyed by repo root, busiest first.
func Find(sessions []*claude.Session, roots []string, extra []string) []Place {
	by := map[string]*Place{}
	add := func(root, src string) *Place {
		p, ok := by[root]
		if !ok {
			p = &Place{Name: filepath.Base(root), Path: root}
			by[root] = p
		}
		for _, s := range p.Sources {
			if s == src {
				return p
			}
		}
		p.Sources = append(p.Sources, src)
		return p
	}

	// 1) where agents ran: resolve each distinct folder to its repo root once
	rootOf := map[string]string{}
	for _, s := range sessions {
		if s.Cwd == "" || s.Bot() || skip(s.Cwd) {
			continue
		}
		r, ok := rootOf[s.Cwd]
		if !ok {
			r = gitwt.RepoRoot(s.Cwd)
			if r == "" {
				if st, err := os.Stat(s.Cwd); err == nil && st.IsDir() {
					r = s.Cwd
				}
			}
			rootOf[s.Cwd] = r
		}
		if r == "" || isHome(r) {
			continue
		}
		p := add(r, "agents")
		p.Sessions++
		if s.Live != nil {
			p.Live++
		}
		if s.Modified.After(p.Last) {
			p.Last = s.Modified
		}
	}

	// 2) git repos under the code roots (shallow walk)
	for _, root := range roots {
		walkRepos(root, 4, func(repo string) {
			p := add(repo, "git")
			if st, err := os.Stat(filepath.Join(repo, ".git")); err == nil && st.ModTime().After(p.Last) && p.Last.IsZero() {
				p.Last = st.ModTime()
			}
		})
	}

	// 3) extra folders (open workspaces, configured repos)
	for _, e := range extra {
		if r := gitwt.RepoRoot(e); r != "" {
			add(r, "workspace")
		} else if e != "" {
			add(e, "workspace")
		}
	}

	out := make([]Place, 0, len(by))
	for _, p := range by {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Live != b.Live {
			return a.Live > b.Live
		}
		if (a.Sessions > 0) != (b.Sessions > 0) {
			return a.Sessions > 0
		}
		return a.Last.After(b.Last)
	})
	return out
}

var skipDirs = map[string]bool{"node_modules": true, "vendor": true, ".cache": true, ".git": true, "dist": true,
	"build": true, "target": true, ".venv": true, "venv": true, "__pycache__": true, ".claude": true}

func walkRepos(root string, depth int, found func(string)) {
	base := strings.Count(filepath.Clean(root), string(os.PathSeparator))
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if strings.Count(path, string(os.PathSeparator))-base > depth || skipDirs[d.Name()] || (strings.HasPrefix(d.Name(), ".") && path != root) {
			return filepath.SkipDir
		}
		if st, err := os.Stat(filepath.Join(path, ".git")); err == nil && st.IsDir() {
			found(path)
			return filepath.SkipDir
		}
		return nil
	})
}

func skip(p string) bool {
	return strings.Contains(p, "/.cache/") || strings.HasPrefix(p, os.TempDir()) || strings.Contains(p, "/.claude/projects")
}

func isHome(p string) bool {
	h, _ := os.UserHomeDir()
	return p == h || p == "/"
}
