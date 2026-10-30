// Package prompts mines the most used prompts from Claude Code's history and
// fills {placeholders} in saved ones.
package prompts

import (
	"bufio"
	"encoding/json"
	"io"
	"math"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Used is one prompt (or a group of near-identical ones) typed more than once.
type Used struct {
	Text     string // the newest wording
	Count    int
	Last     time.Time
	Project  string // folder it was typed in most
	Projects map[string]int
	score    float64
}

// Command is a bare /command and how often it was run.
type Command struct {
	Name  string
	Count int
	Last  time.Time
}

// History is what Mine found.
type History struct {
	Prompts  []Used
	Commands []Command
	Entries  int
}

const (
	minCount = 2   // a prompt typed once isn't "used"
	maxKept  = 300 // ranked prompts kept
	halfLife = 30 * 24 * time.Hour
)

var (
	wsRe     = regexp.MustCompile(`\s+`)
	pastedRe = regexp.MustCompile(`\[(Pasted text|Image) #\d+`)
	cache    struct {
		sync.Mutex
		path string
		size int64
		mod  time.Time
		h    History
	}
)

// Mine reads a history.jsonl, cached by size and mtime.
func Mine(path string) (History, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return History{}, err
	}
	cache.Lock()
	defer cache.Unlock()
	if cache.path == path && cache.size == fi.Size() && cache.mod.Equal(fi.ModTime()) {
		return cache.h, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return History{}, err
	}
	defer f.Close()
	h := mine(f, time.Now())
	cache.path, cache.size, cache.mod, cache.h = path, fi.Size(), fi.ModTime(), h
	return h, nil
}

func mine(f io.Reader, now time.Time) History {
	groups := map[string]*Used{}
	cmds := map[string]*Command{}
	var h History
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 256*1024), 8<<20)
	for sc.Scan() {
		var e struct {
			Display   string `json:"display"`
			Timestamp int64  `json:"timestamp"`
			Project   string `json:"project"`
		}
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		h.Entries++
		t := time.UnixMilli(e.Timestamp)
		text := Normalize(e.Display)
		if strings.HasPrefix(text, "/") {
			if name := strings.Fields(text)[0]; !strings.Contains(name[1:], "/") {
				c := cmds[name]
				if c == nil {
					c = &Command{Name: name}
					cmds[name] = c
				}
				c.Count++
				if t.After(c.Last) {
					c.Last = t
				}
				continue
			}
		}
		if trivial(text) {
			continue
		}
		k := Key(text)
		u := groups[k]
		if u == nil {
			u = &Used{Projects: map[string]int{}}
			groups[k] = u
		}
		u.Count++
		if !t.Before(u.Last) {
			u.Last, u.Text = t, text
		}
		if e.Project != "" {
			u.Projects[e.Project]++
		}
		// every use counts 1, recent ones up to half more: count leads, recency breaks near-ties
		u.score += 1 + 0.5*math.Pow(0.5, float64(now.Sub(t))/float64(halfLife))
	}
	for _, u := range groups {
		if u.Count < minCount {
			continue
		}
		best := 0
		for p, n := range u.Projects {
			if n > best || n == best && p < u.Project {
				u.Project, best = p, n
			}
		}
		h.Prompts = append(h.Prompts, *u)
	}
	sort.Slice(h.Prompts, func(i, j int) bool {
		a, b := h.Prompts[i], h.Prompts[j]
		if a.score != b.score {
			return a.score > b.score
		}
		return a.Last.After(b.Last)
	})
	if len(h.Prompts) > maxKept {
		h.Prompts = h.Prompts[:maxKept]
	}
	for _, c := range cmds {
		h.Commands = append(h.Commands, *c)
	}
	sort.Slice(h.Commands, func(i, j int) bool {
		a, b := h.Commands[i], h.Commands[j]
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		return a.Name < b.Name
	})
	return h
}

// Normalize collapses whitespace.
func Normalize(s string) string { return strings.TrimSpace(wsRe.ReplaceAllString(s, " ")) }

// trivial skips acknowledgements ("ok", "sigue"), shell escapes and pasted blobs
// whose text history doesn't keep.
func trivial(s string) bool {
	if strings.HasPrefix(s, "!") || pastedRe.MatchString(s) {
		return true
	}
	return len([]rune(s)) < 16 || len(strings.Fields(s)) < 3
}

// Key groups near-duplicates: case, punctuation and numbers (ticket ids,
// line numbers) don't matter, nor does anything past the first 100 letters.
func Key(s string) string {
	var b strings.Builder
	space := false
	n := 0
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r):
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteRune(r)
			space = false
			n++
		case unicode.IsDigit(r):
			if !strings.HasSuffix(b.String(), "#") {
				if space && b.Len() > 0 {
					b.WriteByte(' ')
				}
				b.WriteByte('#')
			}
			space = false
		default:
			space = true
		}
		if n >= 100 {
			break
		}
	}
	return b.String()
}

var phRe = regexp.MustCompile(`\{([A-Za-z][A-Za-z0-9_ -]{0,30})\}`)

// Placeholders lists the distinct {names} in text, in order.
func Placeholders(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range phRe.FindAllStringSubmatch(text, -1) {
		if n := strings.TrimSpace(m[1]); !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

// Fill replaces each {name} with its value; names without one stay as they are.
func Fill(text string, vals map[string]string) string {
	return phRe.ReplaceAllStringFunc(text, func(m string) string {
		if v, ok := vals[strings.TrimSpace(m[1:len(m)-1])]; ok && v != "" {
			return v
		}
		return m
	})
}
