// Package insights reads what Claude Code did from the local transcripts:
// tokens and cost, active time, tools, permissions, edits, commits and
// friction, per day. Each transcript is read once, then only its new bytes;
// the tally is cached on disk.
package insights

import (
	"sync/atomic"
	"time"

	"github.com/secuone/nanny/internal/claude"
)

// parserVersion changes whenever the parser counts something differently: a
// cache written by another version is thrown away.
const parserVersion = 1

// Tool is how one tool (or one MCP server) did on a day.
type Tool struct {
	Calls, Errors, Denied, Timed int
	DurMs, MaxMs                 int64
}

// Perm is how permission requests were settled.
type Perm struct{ Auto, Asked, UserOK, UserNo, Blocked int }

// Edits are the file edits Claude made.
type Edits struct{ OK, Rejected, Added, Removed int }

// Day is everything one transcript did on one local day.
type Day struct {
	Tokens                                       map[string]claude.Tokens // by model; the cost is worked out when asked (prices may change)
	ActiveMs                                     int64                    // from turn_duration
	GapMs                                        int64                    // the same, from gaps under 5 minutes, for transcripts without turn_duration
	Turns                                        int
	First, Last                                  int64 // unix ms
	Tools                                        map[string]*Tool
	Perm                                         Perm
	Edits                                        Edits
	Commits, PRs                                 int
	PRURLs                                       []string
	Retries, Interrupts, Compactions, ErrorLoops int
}

type pend struct {
	Name string
	At   int64
	Cmd  string // the start of a Bash command
	Kind string // what a Bash command was for: a commit, a PR
	Day  string
}

// File is the tally of one transcript.
type File struct {
	Size, Mod, Off int64 // size and mtime seen; Off = bytes read (no partial last line)
	Session, Cwd   string
	Title          string
	Sub            bool // a subagent's transcript: counts for its parent session
	HasTD          bool // has turn_duration lines
	LastID         string
	LastModel      string
	LastDay        string
	LastTok        claude.Tokens
	LastTS         int64
	ErrTool        string
	ErrRun         int
	Pending        map[string]pend
	Days           map[string]*Day // "2006-01-02", local time
}

// Snapshot is every transcript's tally, by path.
type Snapshot struct {
	Version int
	Files   map[string]*File
	Built   time.Time
	changed bool
}

// Changed says whether the last Refresh read anything new.
func (s *Snapshot) Changed() bool { return s != nil && s.changed }

// Progress is how far an index run is: files read of those that changed.
type Progress struct{ Done, Total atomic.Int64 }

func (t *Tool) add(o *Tool) {
	t.Calls += o.Calls
	t.Errors += o.Errors
	t.Denied += o.Denied
	t.Timed += o.Timed
	t.DurMs += o.DurMs
	t.MaxMs = max(t.MaxMs, o.MaxMs)
}

func (p *Perm) add(o Perm) {
	p.Auto += o.Auto
	p.Asked += o.Asked
	p.UserOK += o.UserOK
	p.UserNo += o.UserNo
	p.Blocked += o.Blocked
}

func (e *Edits) add(o Edits) {
	e.OK += o.OK
	e.Rejected += o.Rejected
	e.Added += o.Added
	e.Removed += o.Removed
}

func addTok(a, b claude.Tokens) claude.Tokens {
	return claude.Tokens{In: a.In + b.In, Out: a.Out + b.Out, CacheW: a.CacheW + b.CacheW, CacheR: a.CacheR + b.CacheR, CacheW1h: a.CacheW1h + b.CacheW1h}
}

func subTok(a, b claude.Tokens) claude.Tokens {
	return claude.Tokens{In: a.In - b.In, Out: a.Out - b.Out, CacheW: a.CacheW - b.CacheW, CacheR: a.CacheR - b.CacheR, CacheW1h: a.CacheW1h - b.CacheW1h}
}

func (f *File) clone() *File {
	c := *f
	c.Pending = make(map[string]pend, len(f.Pending))
	for k, v := range f.Pending {
		c.Pending[k] = v
	}
	c.Days = make(map[string]*Day, len(f.Days))
	for k, d := range f.Days {
		c.Days[k] = d.clone()
	}
	return &c
}

func (d *Day) clone() *Day {
	c := *d
	c.Tokens = make(map[string]claude.Tokens, len(d.Tokens))
	for k, v := range d.Tokens {
		c.Tokens[k] = v
	}
	c.Tools = make(map[string]*Tool, len(d.Tools))
	for k, v := range d.Tools {
		t := *v
		c.Tools[k] = &t
	}
	c.PRURLs = append([]string(nil), d.PRURLs...)
	return &c
}
