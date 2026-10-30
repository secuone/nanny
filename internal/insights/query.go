package insights

import (
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/secuone/nanny/internal/claude"
)

const dayFmt = "2006-01-02"

// Filter picks and names what a query counts. The zero value counts
// everything and names a project by the last part of its folder.
type Filter struct {
	Keep      func(cwd string) bool
	Project   func(cwd string) string
	Expensive float64 // sessions that cost more than this are listed apart; 0 = none
}

func (f Filter) keep(cwd string) bool { return f.Keep == nil || f.Keep(cwd) }

func (f Filter) project(cwd string) string {
	switch {
	case f.Project != nil:
		return f.Project(cwd)
	case cwd == "":
		return "?"
	}
	return filepath.Base(cwd)
}

// Totals are the sums over a range.
type Totals struct {
	Cost                     float64
	Tokens                   claude.Tokens
	ActiveMs                 int64
	Sessions, Turns          int
	Commits, PRs             int
	LinesAdded, LinesRemoved int
}

// DayAgg is one day of a range (every day is there, quiet or not).
type DayAgg struct {
	Date string
	Totals
	ModelCost map[string]float64
	Models    map[string]claude.Tokens
}

// ProjAgg is what one project cost and did.
type ProjAgg struct {
	Name string
	Totals
}

// ModelAgg is one model's tokens and cost.
type ModelAgg struct {
	Model  string
	Tokens claude.Tokens
	Cost   float64
}

// ToolAgg is one tool's totals.
type ToolAgg struct {
	Name string
	Tool
}

// SessAgg is a session's cost, with its subagents.
type SessAgg struct {
	ID, Path, Title, Cwd, Project string
	Cost                          float64
	Tokens                        claude.Tokens
	ActiveMs                      int64
	Last                          int64 // unix ms
}

// Agg is a range, summed every way the views need it.
type Agg struct {
	From, To    string
	Days        []DayAgg
	Total       Totals
	Projects    []ProjAgg // by cost
	Models      []ModelAgg
	Tools       []ToolAgg // by calls
	Perm        Perm
	Edits       Edits
	Retries     int
	Interrupts  int
	Compactions int
	ErrorLoops  int
	Sessions    []SessAgg // by cost
	Expensive   []SessAgg
	ToolCalls   int
}

// Empty says nothing happened in the range.
func (a Agg) Empty() bool { return a.Total.Sessions == 0 && a.Total.Tokens.Sum() == 0 }

// Range sums the days from..to (local, inclusive) of the transcripts f keeps.
func (s *Snapshot) Range(from, to time.Time, f Filter) Agg {
	fs, ts := from.Format(dayFmt), to.Format(dayFmt)
	a := Agg{From: fs, To: ts}
	idx := map[string]int{}
	for d := time.Date(from.Year(), from.Month(), from.Day(), 12, 0, 0, 0, time.Local); d.Format(dayFmt) <= ts && len(a.Days) < 800; d = d.AddDate(0, 0, 1) {
		idx[d.Format(dayFmt)] = len(a.Days)
		a.Days = append(a.Days, DayAgg{Date: d.Format(dayFmt), ModelCost: map[string]float64{}, Models: map[string]claude.Tokens{}})
	}
	if s == nil {
		return a
	}
	projects := map[string]*ProjAgg{}
	projSess := map[string]map[string]bool{}
	models := map[string]*ModelAgg{}
	tools := map[string]*Tool{}
	sessions := map[string]*SessAgg{}
	daySess := map[string]map[string]bool{}
	allSess := map[string]bool{}
	paths := make([]string, 0, len(s.Files))
	for p := range s.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, path := range paths {
		fl := s.Files[path]
		if !f.keep(fl.Cwd) {
			continue
		}
		proj := f.project(fl.Cwd)
		for date, d := range fl.Days {
			if date < fs || date > ts {
				continue
			}
			i := idx[date]
			da := &a.Days[i]
			ss := sessions[fl.Session]
			if ss == nil {
				ss = &SessAgg{ID: fl.Session, Cwd: fl.Cwd, Project: proj, Path: mainPath(path, fl)}
				sessions[fl.Session] = ss
			}
			if !fl.Sub {
				ss.Title, ss.Cwd, ss.Project = firstOf(fl.Title, ss.Title), firstOf(fl.Cwd, ss.Cwd), proj
			}
			ss.Last = max(ss.Last, d.Last)
			allSess[fl.Session] = true
			if daySess[date] == nil {
				daySess[date] = map[string]bool{}
			}
			daySess[date][fl.Session] = true
			if projSess[proj] == nil {
				projSess[proj] = map[string]bool{}
			}
			projSess[proj][fl.Session] = true
			pa := projects[proj]
			if pa == nil {
				pa = &ProjAgg{Name: proj}
				projects[proj] = pa
			}
			active, turns := int64(0), 0
			if !fl.Sub {
				active, turns = d.ActiveMs, d.Turns
				if !fl.HasTD {
					active = d.GapMs
				}
			}
			for mdl, tk := range d.Tokens {
				c := claude.CostOf(mdl, tk)
				da.Cost += c
				da.Tokens = addTok(da.Tokens, tk)
				da.ModelCost[mdl] += c
				da.Models[mdl] = addTok(da.Models[mdl], tk)
				pa.Cost += c
				pa.Tokens = addTok(pa.Tokens, tk)
				ss.Cost += c
				ss.Tokens = addTok(ss.Tokens, tk)
				m := models[mdl]
				if m == nil {
					m = &ModelAgg{Model: mdl}
					models[mdl] = m
				}
				m.Cost += c
				m.Tokens = addTok(m.Tokens, tk)
			}
			for _, t := range []*Totals{&da.Totals, &pa.Totals, &a.Total} {
				t.ActiveMs += active
				t.Turns += turns
				t.Commits += d.Commits
				t.PRs += d.PRs
				t.LinesAdded += d.Edits.Added
				t.LinesRemoved += d.Edits.Removed
			}
			ss.ActiveMs += active
			for name, t := range d.Tools {
				if tools[name] == nil {
					tools[name] = &Tool{}
				}
				tools[name].add(t)
			}
			a.Perm.add(d.Perm)
			a.Edits.add(d.Edits)
			a.Retries += d.Retries
			a.Interrupts += d.Interrupts
			a.Compactions += d.Compactions
			a.ErrorLoops += d.ErrorLoops
		}
	}
	for i := range a.Days {
		da := &a.Days[i]
		da.Sessions = len(daySess[da.Date])
		a.Total.Cost += da.Cost
		a.Total.Tokens = addTok(a.Total.Tokens, da.Tokens)
	}
	a.Total.Sessions = len(allSess)
	for name, p := range projects {
		p.Sessions = len(projSess[name])
		a.Projects = append(a.Projects, *p)
	}
	sort.Slice(a.Projects, func(i, j int) bool {
		return byCost(a.Projects[i].Cost, a.Projects[j].Cost, a.Projects[i].Name, a.Projects[j].Name)
	})
	for _, m := range models {
		a.Models = append(a.Models, *m)
	}
	sort.Slice(a.Models, func(i, j int) bool {
		return byCost(a.Models[i].Cost, a.Models[j].Cost, a.Models[i].Model, a.Models[j].Model)
	})
	for name, t := range tools {
		a.Tools = append(a.Tools, ToolAgg{name, *t})
		a.ToolCalls += t.Calls
	}
	sort.Slice(a.Tools, func(i, j int) bool {
		if a.Tools[i].Calls != a.Tools[j].Calls {
			return a.Tools[i].Calls > a.Tools[j].Calls
		}
		return a.Tools[i].Name < a.Tools[j].Name
	})
	for _, ss := range sessions {
		a.Sessions = append(a.Sessions, *ss)
	}
	sort.Slice(a.Sessions, func(i, j int) bool {
		return byCost(a.Sessions[i].Cost, a.Sessions[j].Cost, a.Sessions[i].ID, a.Sessions[j].ID)
	})
	for _, ss := range a.Sessions {
		if f.Expensive > 0 && ss.Cost > f.Expensive {
			a.Expensive = append(a.Expensive, ss)
		}
	}
	return a
}

func byCost(a, b float64, an, bn string) bool {
	if a != b {
		return a > b
	}
	return an < bn
}

func firstOf(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// mainPath is the transcript of a session: the file itself, or the parent of
// a subagent's.
func mainPath(path string, f *File) string {
	if !f.Sub {
		return path
	}
	return filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(path))), f.Session+".jsonl")
}

// AvgMs is the mean duration of a tool's calls that were timed.
func (t Tool) AvgMs() int64 {
	if t.Timed == 0 {
		return 0
	}
	return t.DurMs / int64(t.Timed)
}

// ErrRate is the share of a tool's calls that failed, 0-1.
func (t Tool) ErrRate() float64 {
	if t.Calls == 0 {
		return 0
	}
	return float64(t.Errors) / float64(t.Calls)
}

// ActiveHours is milliseconds as hours.
func ActiveHours(ms int64) float64 { return float64(ms) / 3.6e6 }

// Family is a model's family for display ("opus"), or the id when unknown.
func Family(model string) string {
	if f := claude.Family(model); f != "" {
		return f
	}
	return strings.TrimSuffix(model, "[1m]")
}
