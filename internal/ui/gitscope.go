package ui

import (
	"path/filepath"
	"strings"

	"github.com/secuone/nanny/internal/gitwt"
	"github.com/secuone/nanny/internal/ws"
)

// The PR lists follow the same scope as the rest of the views: with a space
// selected they show the PRs of its repo, with All spaces every one. The lists
// are filtered from what gh already answered; switching space asks nothing.

// gitScope is the repo a space works in, as GitHub names it.
type gitScope struct {
	all   bool     // no space selected (or home): nothing is filtered
	repo  bool     // the space is in a git repo
	slugs []string // owner/name behind origin and upstream
	label string   // what the title says: the repo's name
}

func (sc gitScope) has(repo string) bool {
	for _, s := range sc.slugs {
		if strings.EqualFold(s, repo) {
			return true
		}
	}
	return false
}

// scope is the scope of the view now. A space's remotes are read once and kept
// until r.
func (g *gitView) scope(m *Model) gitScope {
	sp := m.spaceScope()
	if sp == nil || sp.Broad() {
		return gitScope{all: true}
	}
	key := scopeKey(sp)
	if sc, ok := g.scopes[key]; ok {
		return sc
	}
	sc := spaceRepoScope(sp)
	if g.scopes == nil {
		g.scopes = map[string]gitScope{}
	}
	g.scopes[key] = sc
	return sc
}

func scopeKey(sp *ws.Space) string {
	if sp == nil || sp.Broad() {
		return ""
	}
	return strings.Join(sp.Dirs(), "\x00")
}

// followScope drops what was read for another scope: the branches are read
// again for this one.
func (g *gitView) followScope(m *Model) {
	if key := scopeKey(m.spaceScope()); key != g.scopeAt {
		g.scopeAt = key
		g.br.loaded = false
	}
}

func spaceRepoScope(sp *ws.Space) gitScope {
	sc := gitScope{label: spaceLabel(sp)}
	for _, d := range sp.Dirs() {
		slugs, ok := gitwt.Slugs(d)
		if !ok {
			continue
		}
		if !sc.repo {
			sc.label = filepath.Base(gitwt.Toplevel(d))
		}
		sc.repo = true
		for _, s := range slugs {
			if !sc.has(s) {
				sc.slugs = append(sc.slugs, s)
			}
		}
	}
	if len(sc.slugs) > 0 {
		sc.label = shortRepo(sc.slugs[0])
	}
	return sc
}

// scopeTitle is what the title of a list says about its scope.
func (g *gitView) scopeTitle(m *Model) string {
	if sc := g.scope(m); !sc.all {
		return " · " + trunc(sc.label, 30)
	}
	return m.scopeNote()
}

// refilter keeps in a list the PRs of the scope.
func (g *gitView) refilter(m *Model, l *prList) {
	sc := g.scope(m)
	if sc.all {
		l.items = l.all
		return
	}
	l.items = l.items[:0:0]
	for _, p := range l.all {
		if sc.has(p.Repo) {
			l.items = append(l.items, p)
		}
	}
}

// allSpaces is the key that leaves the space for All spaces.
func (g *gitView) allSpaces(m *Model) {
	m.cur = -1
	m.show(tabGit)
}

// scopedEmpty is the empty state of a list when a space is selected, "" when
// nothing is filtered.
func (g *gitView) scopedEmpty(m *Model, reviews bool, w int) string {
	sc := g.scope(m)
	if sc.all {
		return ""
	}
	s := m.st
	var msg string
	switch {
	case !sc.repo:
		return s.emptyState(Tf("%s is not a git repository.", sc.label), [][2]string{{"a", T("see all spaces")}}, w)
	case len(sc.slugs) == 0:
		msg = Tf("%s has no GitHub remote.", sc.label)
	case reviews:
		msg = Tf("Nothing is waiting for your review in %s.", sc.label)
	default:
		msg = Tf("No open PRs in %s.", sc.label)
	}
	return s.emptyState(msg, [][2]string{{"a", T("see all spaces")}}, w)
}
