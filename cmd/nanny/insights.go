package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/secuone/nanny/internal/claude"
	"github.com/secuone/nanny/internal/config"
	"github.com/secuone/nanny/internal/insights"
)

// cmdInsights prints what Claude cost and did in a week, from the transcripts
// (the cache is shared with the Insights view).
func cmdInsights(args []string) {
	fs := flag.NewFlagSet("insights", flag.ExitOnError)
	week := fs.String("week", insights.WeekOf(time.Now()), "ISO week, like 2026-W44")
	asJSON := fs.Bool("json", false, "print the week as JSON")
	_ = fs.Parse(args)
	from, to, err := insights.WeekRange(*week)
	if err != nil {
		die("%v", err)
	}
	cfg := config.Load()
	prices := map[string]claude.Price{}
	for k, p := range cfg.Prices {
		prices[k] = claude.Price{Input: p.Input, Output: p.Output, CacheWrite: p.CacheWrite, CacheRead: p.CacheRead}
	}
	claude.SetPrices(prices)
	since := claude.Today().AddDate(0, 0, 1-cfg.Insights.HistoryDays())
	cache := filepath.Join(config.StateDir(), "insights", "v1.gob")
	snap, err := insights.Update(claude.Dir("projects"), cache, since, nil, nil)
	if err != nil {
		die("%v", err)
	}
	f := insights.Filter{Expensive: cfg.Insights.ExpensiveLimit()}
	if *asJSON {
		b, err := insights.Week(snap, *week, f, time.Now())
		if err != nil {
			die("%v", err)
		}
		fmt.Println(string(b))
		return
	}
	printWeek(snap.Range(from, to, f), *week)
}

func printWeek(a insights.Agg, week string) {
	t := a.Total
	fmt.Printf("\x1b[1m%s\x1b[0m  %s – %s\n\n", week, a.From, a.To)
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "cost\t≈$%.2f\n", t.Cost)
	fmt.Fprintf(tw, "tokens\tin %d · out %d · cache write %d · cache read %d\n", t.Tokens.In, t.Tokens.Out, t.Tokens.CacheW, t.Tokens.CacheR)
	fmt.Fprintf(tw, "active\t%.1f h in %d sessions, %d turns\n", insights.ActiveHours(t.ActiveMs), t.Sessions, t.Turns)
	fmt.Fprintf(tw, "claude made\t%d commits, %d PRs, +%d -%d lines\n", t.Commits, t.PRs, t.LinesAdded, t.LinesRemoved)
	fmt.Fprintf(tw, "permissions\t%d by rules, %d asked (%d accepted, %d rejected), %d blocked\n", a.Perm.Auto, a.Perm.Asked, a.Perm.UserOK, a.Perm.UserNo, a.Perm.Blocked)
	fmt.Fprintf(tw, "edits\t%d accepted, %d rejected\n", a.Edits.OK, a.Edits.Rejected)
	fmt.Fprintf(tw, "friction\t%d retries, %d interrupts, %d compactions, %d error loops\n", a.Retries, a.Interrupts, a.Compactions, a.ErrorLoops)
	_ = tw.Flush()
	fmt.Println()
	tw = tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, d := range a.Days {
		fmt.Fprintf(tw, "%s\t≈$%.2f\t%.1f h\t%d sessions\n", d.Date, d.Cost, insights.ActiveHours(d.ActiveMs), d.Sessions)
	}
	_ = tw.Flush()
	if len(a.Projects) > 0 {
		fmt.Println()
		tw = tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		for _, p := range a.Projects[:min(len(a.Projects), 8)] {
			fmt.Fprintf(tw, "%s\t≈$%.2f\t%d sessions\n", p.Name, p.Cost, p.Sessions)
		}
		_ = tw.Flush()
	}
}
