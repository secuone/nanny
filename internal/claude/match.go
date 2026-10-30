package claude

import (
	"strings"

	"github.com/sahilm/fuzzy"
)

type hay []*Session

func (h hay) String(i int) string { return h[i].Haystack() }
func (h hay) Len() int            { return len(h) }

// Match ranks sessions for a query: an exact id (or ≥6-char prefix) wins;
// then sessions containing every whitespace-separated token (newest first);
// then fuzzy matches by score. Input order is assumed newest-first.
func Match(ss []*Session, q string) []*Session {
	q = strings.TrimSpace(q)
	if q == "" {
		return ss
	}
	for _, s := range ss {
		if s.ID == q || (len(q) >= 6 && strings.HasPrefix(s.ID, q)) {
			return []*Session{s}
		}
	}
	toks := strings.Fields(strings.ToLower(q))
	seen := map[*Session]bool{}
	var out []*Session
	for _, s := range ss {
		h := strings.ToLower(s.Haystack())
		ok := true
		for _, t := range toks {
			if !strings.Contains(h, t) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, s)
			seen[s] = true
		}
	}
	for _, r := range fuzzy.FindFrom(q, hay(ss)) {
		if s := ss[r.Index]; !seen[s] {
			out = append(out, s)
		}
	}
	return out
}
