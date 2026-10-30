package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/secuone/nanny/internal/claude"
	"github.com/secuone/nanny/internal/config"
	"github.com/secuone/nanny/internal/insights"
)

// insightsView is the Insights view (I): what your Claude usage costs and
// does, read from the local transcripts. Nothing is read while the view is
// off or not on screen; on screen it indexes when it opens and every 30
// seconds, and an index run only reads the bytes that are new.
type insightsView struct {
	featBase
	snap    *insights.Snapshot
	err     error
	sect    int
	rng     int // index into insRanges
	back    int // weeks back from this one, for the Week section
	cur     cursor
	running bool
	at      time.Time
	prog    insights.Progress
	done    string // what agg and wk were worked out for
	agg     insights.Agg
	wk      insights.Agg
}

type insightsMsg struct {
	snap *insights.Snapshot
	err  error
}

func (insightsMsg) bgMsg() {}

// insightsRefresh indexes the transcripts; tests put a spy in its place.
var insightsRefresh = insights.Update

const insightsEvery = 30 * time.Second

// the ranges t walks, 7d first
var insRanges = []int{7, 30, 90, 1}

const (
	secSummary = iota
	secCost
	secProjects
	secModels
	secTools
	secPerms
	secEdits
	secFriction
	secWeek
)

var insSecNames = []string{M("Summary"), M("Cost"), M("Projects"), M("Models"), M("Tools"), M("Permissions"), M("Edits"), M("Friction"), M("Week")}

func init() {
	settingsList = append(settingsList,
		setting{M("Insights"), M("Insights: history"), M("How many days of transcripts Insights reads (and keeps in its cache)."),
			func(c *config.Config) string { return Tf("%d days", c.Insights.HistoryDays()) },
			func(c *config.Config, d int) {
				c.Insights.Days = cycleInt([]int{30, 60, 90, 180, 365}, c.Insights.HistoryDays(), d)
			}, nil, nil},
		setting{M("Insights"), M("Expensive session over"), M("A session that costs more than this many dollars is listed under Friction."),
			func(c *config.Config) string { return fmt.Sprintf("$%.0f", c.Insights.ExpensiveLimit()) },
			func(c *config.Config, d int) {
				c.Insights.ExpensiveUSD = float64(cycleInt([]int{5, 10, 20, 50, 100, 200}, int(c.Insights.ExpensiveLimit()), d))
			}, nil, nil},
	)
}

// cycleInt steps through opts from the one nearest to cur.
func cycleInt(opts []int, cur, d int) int {
	i := 0
	for j, o := range opts {
		if o <= cur {
			i = j
		}
	}
	return opts[(i+d+len(opts))%len(opts)]
}

func insightsCache() string { return filepath.Join(config.StateDir(), "insights", "v1.gob") }

func (v *insightsView) secs(m *Model) []secDef {
	out := make([]secDef, len(insSecNames))
	for i, n := range insSecNames {
		out[i] = secDef{name: n, on: true}
	}
	return out
}

func (v *insightsView) sec() int { return v.sect }

func (v *insightsView) setSec(m *Model, i int) tea.Cmd {
	v.sect, v.cur = i, cursor{}
	return nil
}

func (v *insightsView) selKey(m *Model) string {
	return fmt.Sprint(v.sect, "/", v.cur.idx, "/", v.rng, "/", v.back)
}

func (v *insightsView) busy() bool { return v.running }

// due indexes when the view is on screen and the last run is old; it reads
// nothing while the view is off or elsewhere.
func (v *insightsView) due(m *Model, force bool) tea.Cmd {
	if v.running || !m.viewOn(tabInsights) || !m.onTab(tabInsights) {
		return nil
	}
	if !force && v.snap != nil && time.Since(v.at) < insightsEvery {
		return nil
	}
	return v.fetch(m, v.snap)
}

// fetch indexes in the background; prev nil reads the cache file, an empty
// snapshot reads every transcript again.
func (v *insightsView) fetch(m *Model, prev *insights.Snapshot) tea.Cmd {
	v.running, v.at = true, time.Now()
	root, cache := claude.Dir("projects"), insightsCache()
	since := claude.Today().AddDate(0, 0, 1-m.cfg.Insights.HistoryDays())
	prog := &v.prog
	return func() tea.Msg {
		s, err := insightsRefresh(root, cache, since, prev, prog)
		return insightsMsg{s, err}
	}
}

func (v *insightsView) update(m *Model, msg tea.Msg) (tea.Cmd, bool) {
	r, ok := msg.(insightsMsg)
	if !ok {
		return nil, false
	}
	v.running, v.err, v.done = false, r.err, ""
	if r.snap != nil && (r.err == nil || v.snap == nil) {
		v.snap = r.snap
	}
	return nil, true
}

func (v *insightsView) click(m *Model, row int) tea.Cmd {
	v.cur.idx = row
	return nil
}

func (v *insightsView) rangeLabel() string {
	if d := insRanges[v.rng]; d > 1 {
		return fmt.Sprintf("%dd", d)
	}
	return T("today")
}

func (v *insightsView) footer(m *Model) [][2]string {
	keys := [][2]string{{"←→", T("section")}, {"t", v.rangeLabel()}}
	switch v.sect {
	case secWeek:
		keys = append(keys, [2]string{",", T("previous week")}, [2]string{".", T("next week")}, [2]string{"x", T("export")}, [2]string{"y", T("copy")})
	case secFriction:
		keys = append(keys, [2]string{"⏎", T("open")})
	}
	return append(keys, [2]string{"r", T("reindex")})
}

func (v *insightsView) help() []helpSec {
	return []helpSec{{T("Insights"), [][2]string{
		{"← →", T("section: Summary · Cost · Projects · Models · Tools · Permissions · Edits · Friction · Week")},
		{"t", T("range: 7 days · 30 days · 90 days · today")},
		{"[ ]  \\", T("scope to a project · clear")},
		{", .", T("Week: the previous · the next week")},
		{"x  y", T("Week: write its JSON to a file · copy it")},
		{"⏎  v", T("Friction: open the expensive session")},
		{"r", T("read every transcript again")},
	}}}
}

func (v *insightsView) palette(m *Model) []paletteEntry {
	return []paletteEntry{
		{"action", T("Insights: export this week"), "x", func(m *Model) tea.Cmd { return v.exportWeek(m) }},
		{"action", T("Insights: copy this week's JSON"), "y", func(m *Model) tea.Cmd { return v.copyWeek(m) }},
	}
}

func (v *insightsView) key(m *Model, k tea.KeyPressMsg) tea.Cmd {
	p := v.page(m, 60)
	n, h := len(p.rows), m.bodyHeight()/2
	switch k.String() {
	case "j", "down":
		v.cur.move(1, n, h)
	case "k", "up":
		v.cur.move(-1, n, h)
	case "pgdown", "ctrl+d":
		v.cur.move(h, n, h)
	case "pgup", "ctrl+u":
		v.cur.move(-h, n, h)
	case "g", "home":
		v.cur.idx = 0
	case "G", "end":
		v.cur.idx = max(0, n-1)
	case "t":
		v.rng, v.cur = (v.rng+1)%len(insRanges), cursor{}
	case ",":
		v.back++
	case ".":
		v.back = max(0, v.back-1)
	case "x":
		return v.exportWeek(m)
	case "y":
		return v.copyWeek(m)
	case "r":
		return v.fetch(m, &insights.Snapshot{Files: map[string]*insights.File{}})
	case "enter", "v":
		if v.cur.idx >= 0 && v.cur.idx < n && p.rows[v.cur.idx].path != "" {
			r := p.rows[v.cur.idx]
			return m.openViewer(r.path, false, r.title, m.byID[r.id], nil)
		}
	}
	return nil
}

// ---------------------------------------------------------------- data

func (v *insightsView) filter(m *Model) insights.Filter {
	return insights.Filter{Keep: m.inScope, Project: project, Expensive: m.cfg.Insights.ExpensiveLimit()}
}

// span is the days of the chosen range, ending today.
func (v *insightsView) span() (time.Time, time.Time) {
	to := claude.Today().Add(12 * time.Hour)
	return to.AddDate(0, 0, 1-insRanges[v.rng]), to
}

func (v *insightsView) weekName() string {
	return insights.WeekOf(time.Now().AddDate(0, 0, -7*v.back))
}

// data sums the range and the week; it is worked out again only when the
// snapshot, the range, the week or the scope change.
func (v *insightsView) data(m *Model) (insights.Agg, insights.Agg) {
	key := fmt.Sprintf("%v|%d|%s|%s|%p|%s", v.snap.Built.UnixNano(), v.rng, v.weekName(), m.scope, m.spaceScope(), claude.Today().Format("2006-01-02"))
	if key != v.done {
		f := v.filter(m)
		from, to := v.span()
		v.agg = v.snap.Range(from, to, f)
		wf, wt, _ := insights.WeekRange(v.weekName())
		v.wk = v.snap.Range(wf, wt, f)
		v.done = key
	}
	return v.agg, v.wk
}

// ---------------------------------------------------------------- export

func (v *insightsView) weekJSON(m *Model) ([]byte, error) {
	if v.snap == nil {
		return nil, fmt.Errorf("%s", T("nothing indexed yet"))
	}
	return insights.Week(v.snap, v.weekName(), v.filter(m), time.Now())
}

func (v *insightsView) exportDir(m *Model) string {
	dir := m.cfg.Insights.ExportDir
	switch {
	case dir == "":
		return home
	case dir == "~" || strings.HasPrefix(dir, "~/"):
		return filepath.Join(home, dir[1:])
	}
	return dir
}

func (v *insightsView) exportWeek(m *Model) tea.Cmd {
	b, err := v.weekJSON(m)
	if err == nil {
		dir := v.exportDir(m)
		if err = os.MkdirAll(dir, 0o755); err == nil {
			path := filepath.Join(dir, "nanny-insights-"+v.weekName()+".json")
			if err = os.WriteFile(path, append(b, '\n'), 0o600); err == nil {
				m.setStatus(Tf("week exported to %s", tilde(path)), false)
				return nil
			}
		}
	}
	m.setStatus(err.Error(), true)
	return nil
}

func (v *insightsView) copyWeek(m *Model) tea.Cmd {
	b, err := v.weekJSON(m)
	if err != nil {
		m.setStatus(err.Error(), true)
		return nil
	}
	return m.copy(string(b), T("week JSON"))
}

// ---------------------------------------------------------------- drawing

// insRow is one line of a list: a label, a bar, and what the row is about.
type insRow struct {
	label, mid, right string
	frac              float64 // the bar, 0-1; negative = none
	detail            []string
	path, id, title   string // a session: ⏎ opens its transcript
}

// insPage is what a section shows: lines on top, then a list of rows, and a
// detail for the selected row (or for the section when it has no rows).
type insPage struct {
	head   []string
	rows   []insRow
	detail []string
	dtitle string
}

func (v *insightsView) view(m *Model, w, h int) string {
	s := m.st
	title := T("Insights") + " · " + v.rangeLabel() + m.scopeNote()
	if v.running && v.snap != nil && v.prog.Total.Load() > 0 {
		title += " · " + spinGlyph + " " + Tf("indexing %d/%d", v.prog.Done.Load(), v.prog.Total.Load())
	}
	p := v.page(m, w-4)
	body := strings.Join(p.head, "\n")
	if len(p.rows) > 0 {
		v.cur.clamp(len(p.rows), bodyRows(h)-len(p.head))
		body += "\n" + v.list(m, p, w-4, bodyRows(h)-len(p.head), secY+2+gapOf(h)+len(p.head))
	}
	return s.panel(title, strings.TrimPrefix(body, "\n"), w, h, !m.focusR)
}

func (v *insightsView) detail(m *Model, w, h int) string {
	p := v.page(m, w-4)
	ls := p.detail
	if len(p.rows) > 0 && v.cur.idx < len(p.rows) {
		ls = p.rows[v.cur.idx].detail
	}
	return m.st.panel(p.dtitle, m.detailScroll(ls, h), w, h, m.focusR)
}

// list draws the rows: label, bar, middle and right columns.
func (v *insightsView) list(m *Model, p insPage, inner, room, y0 int) string {
	s := m.st
	lw, mw, rw := 0, 0, 0
	for _, r := range p.rows {
		lw, mw, rw = max(lw, lipglossW(r.label)), max(mw, lipglossW(r.mid)), max(rw, lipglossW(r.right))
	}
	lw = min(lw, max(10, inner/3))
	bw := inner - 1 - lw - 1 - rw - 1
	if mw > 0 {
		bw -= mw + 1
	}
	blocks := make([][]string, len(p.rows))
	for i, r := range p.rows {
		bar := ""
		if bw >= 4 && r.frac >= 0 {
			bar = s.Teal.Render(hbar(r.frac, 1, bw)) + " "
		}
		mid := ""
		if mw > 0 {
			mid = s.Muted.Render(fmt.Sprintf("%*s", mw, r.mid)) + " "
		}
		line := " " + s.Text.Render(fit(r.label, lw)) + " " + bar + mid + s.Bold.Render(fmt.Sprintf("%*s", rw, r.right))
		if i == v.cur.idx {
			st := s.RowSel
			if m.focusR {
				st = s.RowSelBlur
			}
			line = s.Sel(st, fit(line, inner))
		}
		blocks[i] = []string{line}
	}
	return m.listBlocks(blocks, v.cur.idx, "ins"+strconv.Itoa(v.sect), room, y0)
}

// page builds the section on screen for a panel w cells wide.
func (v *insightsView) page(m *Model, w int) insPage {
	s := m.st
	switch {
	case v.snap == nil && v.err != nil:
		return insPage{head: []string{"", " " + s.Red.Render(trunc(Tf("couldn't read the transcripts: %v", v.err), w-2))}}
	case v.snap == nil:
		msg := T("indexing…")
		if t := v.prog.Total.Load(); v.running && t > 0 {
			msg = Tf("indexing %d/%d", v.prog.Done.Load(), t)
		}
		return insPage{head: []string{"", " " + s.Muted.Render(spinGlyph+" "+msg)}}
	}
	a, wk := v.data(m)
	if v.sect == secWeek {
		return v.weekPage(s, wk, w)
	}
	if a.Empty() {
		return insPage{head: strings.Split(s.emptyState(T("No Claude activity in this range."), [][2]string{{"t", T("try a longer range")}, {"[ ]", T("or all projects")}}, w), "\n")}
	}
	switch v.sect {
	case secCost:
		return costPage(s, a, w)
	case secProjects:
		return projectsPage(s, a, w)
	case secModels:
		return modelsPage(s, a, w)
	case secTools:
		return toolsPage(s, a, w)
	case secPerms:
		return permsPage(s, a, w)
	case secEdits:
		return editsPage(s, a, w)
	case secFriction:
		return v.frictionPage(m, a, w)
	}
	return summaryPage(s, a, w)
}

func kvw(s Styles, label, value string, w int) string {
	return " " + s.Label.Render(fit(label, 14)) + truncANSI(value, max(0, w-15))
}

func insDur(ms int64) string {
	min := ms / 60000
	if min < 60 {
		return fmt.Sprintf("%dm", min)
	}
	return fmt.Sprintf("%dh %02dm", min/60, min%60)
}

func insMs(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	return fmt.Sprintf("%.1fs", float64(ms)/1000)
}

func pctOf(a, b int) string {
	if b == 0 {
		return "0%"
	}
	return fmt.Sprintf("%d%%", (a*100+b/2)/b)
}

func dayLabel(date string) string {
	if t, err := time.ParseInLocation("2006-01-02", date, time.Local); err == nil {
		return t.Format("Mon 02 Jan")
	}
	return date
}

func tokenLines(s Styles, t claude.Tokens, w int) []string {
	dot := s.Dim.Render(" · ")
	return []string{
		kvw(s, T("tokens"), s.Text.Render(Tf("input %s", tok(t.In)))+dot+s.Text.Render(Tf("output %s", tok(t.Out))), w),
		kvw(s, T("cache"), s.Muted.Render(Tf("write %s", tok(t.CacheW)))+dot+s.Muted.Render(Tf("read %s", tok(t.CacheR))), w),
	}
}

func totalsLines(s Styles, t insights.Totals, w int) []string {
	ls := []string{kvw(s, T("Cost"), s.Bold.Render(money(t.Cost)), w)}
	ls = append(ls, tokenLines(s, t.Tokens, w)...)
	return append(ls,
		kvw(s, T("Active time"), s.Text.Render(insDur(t.ActiveMs)), w),
		kvw(s, T("Sessions"), s.Text.Render(fmt.Sprint(t.Sessions))+s.Muted.Render(" · "+Tn(t.Turns, "%d turn", "%d turns")), w),
		kvw(s, T("Claude made"), s.Text.Render(Tn(t.Commits, "%d commit", "%d commits"))+s.Muted.Render(" · "+Tn(t.PRs, "%d PR", "%d PRs")), w),
	)
}

func series(a insights.Agg, f func(d insights.DayAgg) float64) []float64 {
	out := make([]float64, len(a.Days))
	for i, d := range a.Days {
		out[i] = f(d)
	}
	return out
}

func summaryPage(s Styles, a insights.Agg, w int) insPage {
	head := append([]string{""}, totalsLines(s, a.Total, w)...)
	if n := len(a.Days); n > 1 {
		sw := min(n, w-2)
		head = append(head, "",
			" "+s.Accent.Render(T("Cost per day")), " "+s.Peach.Render(spark(series(a, func(d insights.DayAgg) float64 { return d.Cost }), sw)),
			" "+s.Accent.Render(T("Active time per day")), " "+s.Teal.Render(spark(series(a, func(d insights.DayAgg) float64 { return float64(d.ActiveMs) }), sw)),
			" "+s.Muted.Render(fit(dayLabel(a.Days[0].Date), max(0, sw-lipglossW(dayLabel(a.Days[n-1].Date))))+dayLabel(a.Days[n-1].Date)))
	}
	d := insPage{head: head, dtitle: T("Where it went")}
	d.detail = append([]string{"", s.Accent.Render(T("Projects"))}, topLines(s, a, w, 5, func(i int) (string, float64) { return a.Projects[i].Name, a.Projects[i].Cost }, len(a.Projects))...)
	d.detail = append(d.detail, "", s.Accent.Render(T("Models")))
	d.detail = append(d.detail, topLines(s, a, w, 4, func(i int) (string, float64) { return insights.Family(a.Models[i].Model), a.Models[i].Cost }, len(a.Models))...)
	if n := len(a.Expensive); n > 0 {
		d.detail = append(d.detail, "", s.Yellow.Render(Tn(n, "%d expensive session", "%d expensive sessions")), s.Muted.Render(T("listed under Friction")))
	}
	return d
}

// topLines are the first n of a list by cost, each with a bar.
func topLines(s Styles, a insights.Agg, w, n int, at func(i int) (string, float64), total int) []string {
	var out []string
	top := 0.0
	if total > 0 {
		_, top = at(0)
	}
	bw := max(4, w-26)
	for i := 0; i < min(n, total); i++ {
		name, c := at(i)
		out = append(out, s.Text.Render(fit(name, 14))+" "+s.Teal.Render(hbar(c, top, bw))+" "+s.Bold.Render(fmt.Sprintf("%9s", money(c))))
	}
	return out
}

func costPage(s Styles, a insights.Agg, w int) insPage {
	top := 0.0
	for _, d := range a.Days {
		top = max(top, d.Cost)
	}
	p := insPage{dtitle: T("Day")}
	for i := len(a.Days) - 1; i >= 0; i-- {
		d := a.Days[i]
		det := append([]string{"", s.Accent.Render(dayLabel(d.Date))}, totalsLines(s, d.Totals, w)...)
		det = append(det, "", s.Accent.Render(T("By model")))
		for _, m := range sortedModels(d.ModelCost) {
			det = append(det, s.Text.Render(fit(insights.Family(m), 14))+" "+s.Bold.Render(money(d.ModelCost[m]))+s.Muted.Render(" · "+tok(d.Models[m].Sum())))
		}
		p.rows = append(p.rows, insRow{label: dayLabel(d.Date), frac: d.Cost / max(top, 1e-9), right: money(d.Cost), detail: det})
	}
	return p
}

func sortedModels(m map[string]float64) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if m[out[j]] > m[out[i]] || m[out[j]] == m[out[i]] && out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func projectsPage(s Styles, a insights.Agg, w int) insPage {
	p := insPage{dtitle: T("Project")}
	for _, pr := range a.Projects {
		det := append([]string{"", s.Accent.Render(pr.Name)}, totalsLines(s, pr.Totals, w)...)
		det = append(det, "", s.Accent.Render(T("Costliest sessions")))
		n := 0
		for _, ss := range a.Sessions {
			if ss.Project == pr.Name && n < 5 {
				det = append(det, s.Text.Render(trunc(firstOf(ss.Title, ss.ID), w-12))+" "+s.Muted.Render(money(ss.Cost)))
				n++
			}
		}
		p.rows = append(p.rows, insRow{label: pr.Name, frac: pr.Cost / max(a.Projects[0].Cost, 1e-9), mid: Tn(pr.Sessions, "%d session", "%d sessions"), right: money(pr.Cost), detail: det})
	}
	return p
}

func modelsPage(s Styles, a insights.Agg, w int) insPage {
	p := insPage{dtitle: T("Model")}
	for _, m := range a.Models {
		pr := claude.PriceOf(m.Model)
		det := []string{"", s.Accent.Render(m.Model), kvw(s, T("Cost"), s.Bold.Render(money(m.Cost))+s.Muted.Render(" · "+pctOf(int(m.Cost*100), int(a.Total.Cost*100))), w)}
		det = append(det, tokenLines(s, m.Tokens, w)...)
		det = append(det, "", s.Accent.Render(T("Price per million tokens")),
			kvw(s, T("tokens"), s.Text.Render(Tf("input $%.2f", pr.Input))+s.Dim.Render(" · ")+s.Text.Render(Tf("output $%.2f", pr.Output)), w),
			kvw(s, T("cache"), s.Muted.Render(Tf("write $%.2f", pr.CacheWrite))+s.Dim.Render(" · ")+s.Muted.Render(Tf("read $%.2f", pr.CacheRead)), w))
		p.rows = append(p.rows, insRow{label: insights.Family(m.Model), frac: m.Cost / max(a.Models[0].Cost, 1e-9), mid: pctOf(int(m.Cost*100), int(a.Total.Cost*100)), right: money(m.Cost), detail: det})
	}
	return p
}

func toolsPage(s Styles, a insights.Agg, w int) insPage {
	p := insPage{dtitle: T("Tool")}
	for _, t := range a.Tools {
		det := []string{"", s.Accent.Render(t.Name),
			kvw(s, T("Calls"), s.Bold.Render(fmt.Sprint(t.Calls)), w),
			kvw(s, T("Failed"), s.Text.Render(fmt.Sprint(t.Errors))+s.Muted.Render(" · "+pctOf(t.Errors, t.Calls)), w),
			kvw(s, T("Denied"), s.Text.Render(fmt.Sprint(t.Denied)), w),
			kvw(s, T("Average"), s.Text.Render(insMs(t.AvgMs())), w),
			kvw(s, T("Slowest"), s.Text.Render(insMs(t.MaxMs)), w)}
		if strings.HasPrefix(t.Name, "mcp__") {
			det = append(det, "", s.Muted.Render(T("every tool of this MCP server")))
		}
		p.rows = append(p.rows, insRow{label: t.Name, frac: float64(t.Calls) / float64(a.Tools[0].Calls), mid: Tn(t.Calls, "%d call", "%d calls"), right: pctOf(t.Errors, t.Calls) + " " + T("failed"), detail: det})
	}
	return p
}

// countLine is "label  n  share  bar" for the static sections.
func countLine(s Styles, label string, n, of, w int, st lipgloss.Style) string {
	bw := max(4, w-34)
	return " " + s.Text.Render(fit(label, 22)) + s.Bold.Render(fmt.Sprintf("%6d", n)) + s.Muted.Render(fmt.Sprintf("%5s ", pctOf(n, of))) + st.Render(hbar(float64(n), float64(max(of, 1)), bw))
}

func permsPage(s Styles, a insights.Agg, w int) insPage {
	pm := a.Perm
	all := pm.Auto + pm.Asked + pm.Blocked
	p := insPage{dtitle: T("Permissions")}
	p.head = []string{"",
		countLine(s, T("Approved by rules"), pm.Auto, all, w, s.Green),
		countLine(s, T("Permissions asked"), pm.Asked, all, w, s.Yellow),
		countLine(s, "  "+T("you accepted"), pm.UserOK, all, w, s.Green),
		countLine(s, "  "+T("you rejected"), pm.UserNo, all, w, s.Red),
		countLine(s, T("Blocked"), pm.Blocked, all, w, s.Red)}
	p.detail = []string{"", s.Accent.Render(T("Reading it")),
		kvw(s, T("Tool calls"), s.Text.Render(fmt.Sprint(a.ToolCalls)), w),
		kvw(s, T("Asked"), s.Text.Render(fmt.Sprintf("%.1f", per100(pm.Asked, a.ToolCalls)))+s.Muted.Render(" "+T("per 100 tool calls")), w),
		"", s.Muted.Render(T("Asked: Claude Code stopped to ask you.")),
		s.Muted.Render(T("Blocked: auto mode or a rule said no.")),
		s.Muted.Render(T("Fewer questions with the same safety: add the")),
		s.Muted.Render(T("ones you always accept to your allow rules."))}
	return p
}

func per100(n, of int) float64 {
	if of == 0 {
		return 0
	}
	return float64(n) * 100 / float64(of)
}

func editsPage(s Styles, a insights.Agg, w int) insPage {
	e := a.Edits
	all := e.OK + e.Rejected
	p := insPage{dtitle: T("Edits")}
	p.head = []string{"",
		countLine(s, T("Edits accepted"), e.OK, all, w, s.Green),
		countLine(s, T("Edits rejected"), e.Rejected, all, w, s.Red), "",
		kvw(s, T("Lines added"), s.Green.Render(fmt.Sprintf("+%d", e.Added)), w),
		kvw(s, T("Lines removed"), s.Red.Render(fmt.Sprintf("-%d", e.Removed)), w)}
	p.detail = []string{"", s.Accent.Render(T("By tool"))}
	for _, t := range a.Tools {
		if t.Name == "Edit" || t.Name == "MultiEdit" || t.Name == "Write" || t.Name == "NotebookEdit" {
			p.detail = append(p.detail, kvw(s, t.Name, s.Text.Render(Tn(t.Calls, "%d call", "%d calls"))+s.Muted.Render(" · "+Tf("%d denied", t.Denied)), w))
		}
	}
	return append2(p, "", s.Muted.Render(T("Lines come from the diffs Claude Code logs;")), s.Muted.Render(T("a new file counts all its lines as added.")))
}

func append2(p insPage, ls ...string) insPage {
	p.detail = append(p.detail, ls...)
	return p
}

func (v *insightsView) frictionPage(m *Model, a insights.Agg, w int) insPage {
	s := m.st
	p := insPage{dtitle: T("Session")}
	p.head = []string{"",
		kvw(s, T("API retries"), s.Text.Render(fmt.Sprint(a.Retries)), w),
		kvw(s, T("Interrupted"), s.Text.Render(fmt.Sprint(a.Interrupts)), w),
		kvw(s, T("Compactions"), s.Text.Render(fmt.Sprint(a.Compactions)), w),
		kvw(s, T("Error loops"), s.Text.Render(fmt.Sprint(a.ErrorLoops))+s.Muted.Render(" · "+T("3 failures in a row of one tool")), w), ""}
	limit := m.cfg.Insights.ExpensiveLimit()
	if len(a.Expensive) == 0 {
		p.head = append(p.head, " "+s.Muted.Render(Tf("No session cost more than $%.0f.", limit)))
		return p
	}
	p.head = append(p.head, " "+s.Accent.Render(Tf("Sessions over $%.0f", limit)))
	for _, ss := range a.Expensive {
		title := firstOf(ss.Title, ss.ID)
		if ses := m.byID[ss.ID]; ses != nil && ses.Title != "" {
			title = ses.Title
		}
		det := []string{"", s.Accent.Render(trunc(title, w)), kvw(s, T("Cost"), s.Bold.Render(money(ss.Cost)), w)}
		det = append(det, tokenLines(s, ss.Tokens, w)...)
		det = append(det, kvw(s, T("Active time"), s.Text.Render(insDur(ss.ActiveMs)), w),
			kvw(s, T("project"), s.Text.Render(ss.Project), w),
			kvw(s, T("folder"), s.Muted.Render(tilde(ss.Cwd)), w),
			kvw(s, T("last used"), s.Muted.Render(ago(time.UnixMilli(ss.Last))), w), "",
			s.Muted.Render(T("⏎ reads the conversation")))
		p.rows = append(p.rows, insRow{label: title, frac: -1, mid: ss.Project, right: money(ss.Cost), detail: det, path: ss.Path, id: ss.ID, title: title})
	}
	return p
}

func (v *insightsView) weekPage(s Styles, a insights.Agg, w int) insPage {
	name := v.weekName()
	from, to, _ := insights.WeekRange(name)
	p := insPage{dtitle: T("Week")}
	p.head = []string{"", " " + s.Accent.Render(name) + s.Muted.Render("  "+from.Format("2 Jan")+" – "+to.Format("2 Jan"))}
	p.head = append(p.head, totalsLines(s, a.Total, w)...)
	top := 0.0
	for _, d := range a.Days {
		top = max(top, d.Cost)
	}
	p.head = append(p.head, "")
	for _, d := range a.Days {
		p.head = append(p.head, " "+s.Text.Render(fit(dayLabel(d.Date), 11))+s.Peach.Render(hbar(d.Cost, top, max(4, w-26)))+" "+s.Bold.Render(fmt.Sprintf("%9s", money(d.Cost))))
	}
	file := "nanny-insights-" + name + ".json"
	p.detail = []string{"", s.Accent.Render(T("Export")),
		s.Muted.Render(T("The week as JSON, keys in order, numbers rounded:")),
		s.Text.Render(file), "",
		s.Key.Render("x") + " " + s.Text.Render(T("write it to the export folder")),
		s.Key.Render("y") + " " + s.Text.Render(T("copy it")),
		s.Key.Render(",") + " " + s.Key.Render(".") + " " + s.Text.Render(T("another week")), "",
		s.Muted.Render(T("export_dir in [insights] sets the folder; ~ by default."))}
	if a.Empty() {
		p.detail = append(p.detail, "", s.Yellow.Render(T("Nothing happened this week.")))
	}
	return p
}
