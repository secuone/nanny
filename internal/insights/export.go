package insights

import (
	"encoding/json"
	"fmt"
	"math"
	"time"
)

// WeekOf is the ISO week of t: "2026-W44".
func WeekOf(t time.Time) string {
	y, w := t.ISOWeek()
	return fmt.Sprintf("%d-W%02d", y, w)
}

// WeekRange is the Monday and the Sunday of an ISO week ("2026-W44").
func WeekRange(week string) (from, to time.Time, err error) {
	var y, w int
	if _, err = fmt.Sscanf(week, "%d-W%d", &y, &w); err != nil || w < 1 || w > 53 {
		return from, to, fmt.Errorf("insights: %q is not a week like 2026-W44", week)
	}
	jan4 := time.Date(y, 1, 4, 12, 0, 0, 0, time.Local)
	monday := jan4.AddDate(0, 0, -((int(jan4.Weekday())+6)%7)+(w-1)*7)
	return monday, monday.AddDate(0, 0, 6), nil
}

func r2(v float64) float64 { return math.Round(v*100) / 100 }

func hours(ms int64) float64 { return r2(ActiveHours(ms)) }

// Week is the JSON of one ISO week. Objects come out with their keys sorted
// (they are maps); lists keep a fixed order.
func Week(s *Snapshot, week string, f Filter, now time.Time) ([]byte, error) {
	from, to, err := WeekRange(week)
	if err != nil {
		return nil, err
	}
	a := s.Range(from, to, f)
	return json.MarshalIndent(weekDoc(a, week, now), "", "  ")
}

func weekDoc(a Agg, week string, now time.Time) map[string]any {
	t := a.Total
	days := make([]any, len(a.Days))
	for i, d := range a.Days {
		days[i] = map[string]any{"date": d.Date, "cost_usd": r2(d.Cost), "active_hours": hours(d.ActiveMs),
			"sessions": d.Sessions, "turns": d.Turns, "commits": d.Commits, "pull_requests": d.PRs}
	}
	var projects []any
	for _, p := range a.Projects[:min(len(a.Projects), 15)] {
		projects = append(projects, map[string]any{"name": p.Name, "cost_usd": r2(p.Cost), "sessions": p.Sessions,
			"active_hours": hours(p.ActiveMs), "commits": p.Commits, "pull_requests": p.PRs})
	}
	var models []any
	for _, m := range a.Models {
		models = append(models, map[string]any{"model": m.Model, "cost_usd": r2(m.Cost), "input_tokens": m.Tokens.In,
			"output_tokens": m.Tokens.Out, "cache_write_tokens": m.Tokens.CacheW, "cache_read_tokens": m.Tokens.CacheR})
	}
	var tools []any
	for _, tl := range a.Tools[:min(len(a.Tools), 20)] {
		tools = append(tools, map[string]any{"name": tl.Name, "calls": tl.Calls, "errors": tl.Errors, "denied": tl.Denied,
			"avg_ms": tl.AvgMs(), "max_ms": tl.MaxMs})
	}
	var expensive []any
	for _, ss := range a.Expensive {
		expensive = append(expensive, map[string]any{"id": ss.ID, "title": ss.Title, "project": ss.Project, "cost_usd": r2(ss.Cost)})
	}
	return map[string]any{
		"week":         week,
		"from":         a.From,
		"to":           a.To,
		"generated_at": now.Format(time.RFC3339),
		"totals": map[string]any{
			"cost_usd": r2(t.Cost), "active_hours": hours(t.ActiveMs), "sessions": t.Sessions, "turns": t.Turns,
			"commits": t.Commits, "pull_requests": t.PRs, "lines_added": t.LinesAdded, "lines_removed": t.LinesRemoved,
			"input_tokens": t.Tokens.In, "output_tokens": t.Tokens.Out, "cache_write_tokens": t.Tokens.CacheW, "cache_read_tokens": t.Tokens.CacheR,
		},
		"days":     days,
		"projects": orEmpty(projects),
		"models":   orEmpty(models),
		"tools":    orEmpty(tools),
		"permissions": map[string]any{"auto": a.Perm.Auto, "asked": a.Perm.Asked, "accepted": a.Perm.UserOK,
			"rejected": a.Perm.UserNo, "blocked": a.Perm.Blocked},
		"edits": map[string]any{"accepted": a.Edits.OK, "rejected": a.Edits.Rejected, "lines_added": a.Edits.Added, "lines_removed": a.Edits.Removed},
		"friction": map[string]any{"retries": a.Retries, "interrupts": a.Interrupts, "compactions": a.Compactions,
			"error_loops": a.ErrorLoops},
		"expensive_sessions": orEmpty(expensive),
	}
}

func orEmpty(v []any) []any {
	if v == nil {
		return []any{}
	}
	return v
}
