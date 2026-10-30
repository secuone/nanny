package ui

import (
	"fmt"
	"time"

	"github.com/secuone/nanny/internal/sources"
)

// overview summarises today's Claude activity as the first Radar entry.
func (m Model) overview() sources.Item {
	y, mo, d := time.Now().Date()
	day := time.Date(y, mo, d, 0, 0, 0, 0, time.Local)
	var touched, live, busy, bots, subs int
	var cost float64
	repos := map[string]bool{}
	for _, s := range m.sessions {
		if s.Live != nil {
			live++
			if s.Live.Status == "busy" {
				busy++
			}
		}
		if s.Modified.Before(day) {
			continue
		}
		touched++
		cost += s.Cost
		subs += s.Subagents
		if s.Bot() {
			bots++
		}
		repos[project(s.Cwd)] = true
	}
	it := sources.Item{Group: M("Today"), Name: Tn(touched, "%d session touched today", "%d sessions touched today"), Status: sources.Idle, When: time.Now()}
	if busy > 0 {
		it.Status = sources.Running
	}
	it.Detail = Tf("%d live · %d working · %d subagents · %d repos · $%.2f", live, busy, subs, len(repos), cost)
	it.Fields = []sources.KV{
		{K: M("sessions"), V: Tf("%d (%d automated)", touched, bots)},
		{K: M("live"), V: Tf("%d (%d working)", live, busy)},
		{K: M("subagents"), V: fmt.Sprint(subs)},
		{K: M("repos"), V: fmt.Sprint(len(repos))},
		{K: M("cost"), V: Tf("$%.2f (closed sessions only)", cost)},
		{K: M("all time"), V: Tn(len(m.sessions), "%d session", "%d sessions")},
	}
	return it
}
