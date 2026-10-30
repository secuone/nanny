package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/secuone/nanny/internal/claude"
	"github.com/secuone/nanny/internal/config"
)

// Context & cost meter: token usage read from transcripts, priced per model family.

// ctxWarn is the context share past which Claude Code is close to auto-compacting.
const ctxWarn = 85

type usageMsg struct {
	path string
	u    claude.Usage
}

func usageCmd(path string) tea.Cmd {
	return func() tea.Msg { return usageMsg{path, claude.UsageOf(path)} }
}

func setPrices(c config.Config) {
	m := map[string]claude.Price{}
	for k, p := range c.Prices {
		m[k] = claude.Price{Input: p.Input, Output: p.Output, CacheWrite: p.CacheWrite, CacheRead: p.CacheRead}
	}
	claude.SetPrices(m)
}

// usageOf is a session's usage: read by the scan, or on demand for older ones.
func (m Model) usageOf(ses *claude.Session) *claude.Usage {
	if ses == nil {
		return nil
	}
	if ses.Usage != nil {
		return ses.Usage
	}
	if u, ok := m.usage[ses.Path]; ok {
		return &u
	}
	return nil
}

func money(v float64) string { return fmt.Sprintf("≈$%.2f", v) }

func (s Styles) ctxStyle(pct int) lipgloss.Style {
	switch {
	case pct >= ctxWarn:
		return s.Red
	case pct >= 60:
		return s.Yellow
	}
	return s.Green
}

// gauge is a context bar of n cells and its percentage: ▰▰▰▱▱ 62%.
func (s Styles) gauge(u *claude.Usage, n int) string {
	pct := u.Pct()
	on := (pct*n + 50) / 100
	if pct > 0 && on == 0 {
		on = 1
	}
	st := s.ctxStyle(pct)
	return st.Render(strings.Repeat("▰", on)) + s.Dim.Render(strings.Repeat("▱", n-on)) + st.Render(fmt.Sprintf(" %d%%", pct))
}

// meter is the context gauge of an agent row at a level of detail:
// 2 gauge and cost, 1 gauge, 0 the percentage alone.
func (m Model) meter(u *claude.Usage, level int) string {
	if u == nil || u.Ctx == 0 {
		return ""
	}
	s := m.st
	switch level {
	case 0:
		return s.ctxStyle(u.Pct()).Render(fmt.Sprintf("%d%%", u.Pct()))
	case 1:
		return s.gauge(u, 5)
	}
	return s.gauge(u, 5) + " " + s.Muted.Render(money(u.Cost()+u.SubCost()))
}

// fitMeter is the most detailed meter no wider than room; the cost goes first.
func (m Model) fitMeter(u *claude.Usage, room int) string {
	for lvl := 2; lvl >= 0; lvl-- {
		if g := m.meter(u, lvl); g != "" && lipgloss.Width(g) <= room {
			return g
		}
	}
	return ""
}

// tok is a short token count: 840, 8.4k, 124k, 1.2M.
func tok(n int64) string {
	switch {
	case n >= 1_000_000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1e6), ".0") + "M"
	case n >= 10_000:
		return fmt.Sprintf("%.0fk", float64(n)/1e3)
	case n >= 1000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1e3), ".0") + "k"
	}
	return fmt.Sprint(n)
}

// usageLines is the Usage block of the Live and Sessions details.
func (m Model) usageLines(u *claude.Usage, w int) []string {
	s := m.st
	if u == nil || u.Total().Sum() == 0 {
		return nil
	}
	t := u.Total()
	kv := func(label, value string) string { return s.Label.Render(fit(label, 10)) + truncANSI(value, w-10) }
	dot := s.Dim.Render(" · ")
	ls := []string{"", s.Accent.Render(T("Usage"))}
	if u.Ctx > 0 {
		ls = append(ls, kv(T("context"), s.gauge(u, 10)+s.Muted.Render(" · "+tok(u.Ctx)+" / "+tok(u.Window()))))
	}
	ls = append(ls, kv(T("tokens"), s.Text.Render(Tf("input %s", tok(t.In)))+dot+s.Text.Render(Tf("output %s", tok(t.Out)))))
	ls = append(ls, kv(T("cache"), s.Muted.Render(Tf("write %s", tok(t.CacheW)))+dot+s.Muted.Render(Tf("read %s", tok(t.CacheR)))))
	cost := s.Bold.Render(money(u.Cost()))
	if len(u.Subs) > 0 {
		cost += dot + s.Muted.Render(Tf("subagents %s", money(u.SubCost())))
	}
	ls = append(ls, kv(T("cost"), cost))
	if u.Model != "" {
		ls = append(ls, kv(T("model"), s.Muted.Render(u.Model)))
	}
	return ls
}

// todaySpend is the estimated cost of every session modified today.
func (m Model) todaySpend() float64 {
	today, c := claude.Today(), 0.0
	for _, ses := range m.sessions {
		if u := ses.Usage; u != nil && !ses.Modified.Before(today) {
			c += u.Cost() + u.SubCost()
		}
	}
	return c
}

// detectContext raises one alert per session when its context crosses ctxWarn.
func (m *Model) detectContext() {
	a := m.alerts
	for _, r := range m.live.allRows {
		if r.sub != nil || r.sess == nil || r.sess.Usage == nil {
			continue
		}
		id := r.sess.ID
		if a.ctxWarned[id] || r.sess.Usage.Pct() < ctxWarn {
			continue
		}
		a.ctxWarned[id] = true
		if !a.primed { // already past it when nanny started
			continue
		}
		title, cwd := r.titleCwd()
		m.fire(alertEvent{at: time.Now(), kind: alertContext, title: title, project: project(cwd), key: r.key(),
			detail: Tf("context %d%% · auto-compact soon", r.sess.Usage.Pct())})
	}
}
