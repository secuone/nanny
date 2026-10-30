package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/secuone/nanny/internal/config"
	"github.com/secuone/nanny/internal/insights"
)

// spyInsights counts the indexing runs and still does them.
func spyInsights(t *testing.T) *atomic.Int64 {
	t.Helper()
	var n atomic.Int64
	real := insightsRefresh
	insightsRefresh = func(root, cache string, since time.Time, prev *insights.Snapshot, prog *insights.Progress) (*insights.Snapshot, error) {
		n.Add(1)
		return real(root, cache, since, prev, prog)
	}
	t.Cleanup(func() { insightsRefresh = real })
	return &n
}

func insTick(m tea.Model) tea.Model {
	m, c := m.Update(tickMsg(time.Now()))
	return drain(m, c, 0)
}

// With the view off, or not on screen, nothing is read: not on a tick, not on I.
func TestInsightsOffNotScanned(t *testing.T) {
	n := spyInsights(t)
	m := boot(t, 150, 42).(Model)
	m = insTick(m).(Model)
	m = insTick(key(m, "R")).(Model)
	if n.Load() != 0 {
		t.Fatalf("Insights is not on screen but %d runs happened", n.Load())
	}
	m.cfg.SetOff(append(m.cfg.OffList(m.cfg.ActiveProfile()), "view.insights"))
	mm := insTick(key(m, "I"))
	mm = insTick(mm)
	if n.Load() != 0 || mm.(Model).onTab(tabInsights) {
		t.Errorf("view.insights is off: runs %d, on screen %v", n.Load(), mm.(Model).onTab(tabInsights))
	}
}

// Opening the view indexes once; a tick before 30 seconds does not read again.
func TestInsightsIndexesOnOpen(t *testing.T) {
	n := spyInsights(t)
	m := boot(t, 150, 42)
	opened, c := m.(Model).switchTab(tabInsights)
	if !opened.(Model).featBusy() {
		t.Error("busy while indexing, so frames run for the spinner")
	}
	m = drain(opened, c, 0)
	if m.(Model).featBusy() || n.Load() != 1 {
		t.Fatalf("after opening: busy %v, runs %d", m.(Model).featBusy(), n.Load())
	}
	m = insTick(m)
	if n.Load() != 1 {
		t.Errorf("a fresh index is not read again: %d runs", n.Load())
	}
	m.(Model).ins.at = time.Now().Add(-time.Minute)
	if insTick(m); n.Load() != 2 {
		t.Errorf("after 30 s the view indexes again: %d runs", n.Load())
	}
}

// The demo transcripts give a cost and a sparkline.
func TestInsightsSparkline(t *testing.T) {
	m := key(boot(t, 150, 42), "I")
	out := fits(t, m, 150, 42, "Insights")
	for _, want := range []string{"▁", "█", "≈$", "Cost per day", "Sessions"} {
		if !strings.Contains(out, want) {
			t.Errorf("Insights lacks %q:\n%s", want, out)
		}
	}
}

// Every section fits every size, in every language, and is reached with the arrows.
func TestInsightsSectionsFit(t *testing.T) {
	for _, lang := range []string{"en", "fr", "de"} {
		for _, size := range [][2]int{{150, 42}, {100, 30}, {80, 24}} {
			m := key(bootIn(t, lang, size[0], size[1]), "I")
			for i := range insSecNames {
				if got := m.(Model).ins.sect; got != i {
					t.Fatalf("section %d is %d", i, got)
				}
				fits(t, m, size[0], size[1], lang+" "+insSecNames[i])
				m = key(m, "j")
				m = key(m, "right")
			}
		}
	}
}

func TestInsightsRangeAndScope(t *testing.T) {
	m := key(boot(t, 150, 42), "I")
	if out := fits(t, m, 150, 42, "7d"); !strings.Contains(out, "Insights · 7d") {
		t.Errorf("default range is 7 days:\n%s", out)
	}
	for _, want := range []string{"30d", "90d", "today", "7d"} {
		m = key(m, "t")
		if out := fits(t, m, 150, 42, want); !strings.Contains(out, "Insights · "+want) {
			t.Errorf("t should show %s:\n%s", want, out)
		}
	}
	m = key(m, "]")
	if out := fits(t, m, 150, 42, "scoped"); !strings.Contains(out, "only ") {
		t.Errorf("] scopes to a project:\n%s", out)
	}
}

// A list's rows are chosen with j/k and the detail follows.
func TestInsightsListAndDetail(t *testing.T) {
	m := key(boot(t, 150, 42), "I")
	m = key(m, "right") // Cost: one row per day
	first := fits(t, m, 150, 42, "Cost")
	m = key(m, "j")
	second := fits(t, m, 150, 42, "Cost")
	if first == second {
		t.Error("j should move to the previous day")
	}
	m = key(key(m, "right"), "right") // Models
	if out := fits(t, m, 150, 42, "Models"); !strings.Contains(out, "Price per million tokens") {
		t.Errorf("a model's detail has its prices:\n%s", out)
	}
}

func toWeek(m tea.Model) tea.Model {
	for range 8 {
		m = key(m, "right")
	}
	return m
}

// x writes the week to export_dir, keys in order.
func TestInsightsExport(t *testing.T) {
	dir := t.TempDir()
	m := boot(t, 150, 42).(Model)
	m.cfg.Insights.ExportDir = dir
	mm := toWeek(key(m, "I"))
	if !mm.(Model).ins.weekPage(mm.(Model).st, insights.Agg{}, 60).detailHas("nanny-insights-") {
		t.Fatal("the Week section names its file")
	}
	mm = key(mm, "x")
	file := filepath.Join(dir, "nanny-insights-"+mm.(Model).ins.weekName()+".json")
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("no export: %v", err)
	}
	if out := fits(t, mm, 150, 42, "export"); !strings.Contains(out, "week exported to") {
		t.Errorf("no status:\n%s", out)
	}
	dec := json.NewDecoder(strings.NewReader(string(b)))
	_, _ = dec.Token()
	prev := ""
	for dec.More() {
		k, _ := dec.Token()
		if key := k.(string); key < prev {
			t.Errorf("%q comes after %q", key, prev)
		} else {
			prev = key
		}
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			t.Fatal(err)
		}
	}
	if prev != "week" {
		t.Errorf("last key %q", prev)
	}
	if out := fits(t, key(mm, "y"), 150, 42, "copy"); !strings.Contains(out, "copied week JSON") {
		t.Errorf("y copies:\n%s", out)
	}
	if out := fits(t, key(key(mm, ","), ","), 150, 42, "back"); !strings.Contains(out, "W") {
		t.Errorf("another week:\n%s", out)
	}
}

// Friction lists the sessions over the limit and ⏎ opens one.
func TestInsightsExpensiveSession(t *testing.T) {
	m := boot(t, 150, 42).(Model)
	m.cfg.Insights.ExpensiveUSD = 0.0001
	mm := key(key(m, "I"), "t") // 30 days
	for range 7 {
		mm = key(mm, "right")
	}
	out := fits(t, mm, 150, 42, "Friction")
	if !strings.Contains(out, "Sessions over $0") || !strings.Contains(out, "⏎ reads the conversation") {
		t.Fatalf("expensive sessions:\n%s", out)
	}
	mm = key(mm, "enter")
	if mm.(Model).view == nil || !strings.HasSuffix(mm.(Model).view.path, ".jsonl") {
		t.Error("⏎ opens the transcript")
	}
}

func TestInsightsSettingsRows(t *testing.T) {
	var c config.Config
	for _, s := range settingsList {
		switch s.name {
		case "Insights: history":
			s.cycle(&c, 1)
			if c.Insights.HistoryDays() != 180 {
				t.Errorf("history: %d", c.Insights.HistoryDays())
			}
		case "Expensive session over":
			s.cycle(&c, -1)
			if c.Insights.ExpensiveLimit() != 10 {
				t.Errorf("expensive: %v", c.Insights.ExpensiveLimit())
			}
		}
	}
}

func TestSparkAndHbar(t *testing.T) {
	if got := spark([]float64{0, 1, 2, 4}, 8); got != "▁▂▄█" && got != "▁▃▅█" {
		t.Errorf("spark: %q", got)
	}
	if got := spark([]float64{0, 0}, 4); got != "▁▁" {
		t.Errorf("spark of nothing: %q", got)
	}
	if got := []rune(spark(make([]float64, 90), 30)); len(got) != 30 {
		t.Errorf("90 values fold into 30 cells: %d", len(got))
	}
	folded := []rune(spark([]float64{0, 0, 9, 0}, 2))
	if folded[0] != '▁' || folded[1] != '█' {
		t.Errorf("folding keeps the peak: %q", string(folded))
	}
	for v, want := range map[float64]string{0: "     ", 1: "█████", 0.5: "██▌  ", 0.1: "▌    ", 0.01: "▏    ", 2: "█████"} {
		if got := hbar(v, 1, 5); got != want {
			t.Errorf("hbar(%v) = %q, want %q", v, got, want)
		}
		if lipgloss.Width(hbar(v, 1, 5)) != 5 {
			t.Errorf("hbar(%v) is not 5 wide", v)
		}
	}
	if hbar(1, 0, 3) != "   " || hbar(1, 1, 0) != "" {
		t.Error("hbar with no scale or no room")
	}
}

// With the ASCII icons the charts are _.-=# and |.
func TestChartsInASCII(t *testing.T) {
	p := newLook(config.Look{Icons: "ascii"})
	if got := p.apply(spark([]float64{0, 1, 3, 5, 7}, 5)); got != "_.=##" {
		t.Errorf("ascii spark: %q", got)
	}
	for _, r := range "▁▂▃▄▅▆▇█▉▊▋▌▍▎▏" {
		if got := p.apply(string(r)); got == string(r) {
			t.Errorf("%q has no ASCII stand-in", r)
		}
	}
}

// detailHas says whether a page's detail mentions s (the tests ask).
func (p insPage) detailHas(s string) bool {
	for _, l := range p.detail {
		if strings.Contains(l, s) {
			return true
		}
	}
	return false
}
