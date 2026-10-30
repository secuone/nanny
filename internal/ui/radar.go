package ui

import (
	"fmt"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/secuone/nanny/internal/sources"
)

// radarTab is the Monitor: everything that isn't a Claude transcript, in
// sections — this machine, open ports, containers, background jobs, cron and
// timers, Hermes and the user's own watchers.
type radarTab struct {
	cur   cursor
	all   []sources.Item // everything collected
	items []sources.Item // the current section, with group headers
	head  []bool         // row is a group header
	sec   int
}

// monSec is a section of the Monitor: the groups it shows and the source
// source that fills it, turned on and off as "monitor.<src>" ("" = All).
type monSec struct {
	name   string
	groups []string
	src    string
}

var monSecs = []monSec{
	{M("All"), nil, ""},
	{M("Machine"), []string{"Today", "This machine"}, "machine"},
	{M("Ports"), []string{"Open ports"}, "ports"},
	{M("Containers"), []string{"Containers"}, "containers"},
	{M("Tilt"), []string{"Tilt"}, "tilt"},
	{M("Jobs"), []string{"Claude background jobs", "Loops", "opencode"}, "jobs"},
	{M("Cron"), []string{"Cron", "Timers"}, "cron"},
	{M("Hermes"), []string{"Hermes", "Hermes cron", "Hermes sessions"}, "hermes"},
	{M("Watchers"), nil, "watchers"},
}

const secAll, secWatchers = 0, 8

// secY is the screen row of the sections bar, one blank row under the views
// bar, like a space's terminal tabs.
const secY = termY

// secOf is the section an item group belongs to.
func secOf(group string) int {
	for i, sc := range monSecs {
		for _, g := range sc.groups {
			if g == group {
				return i
			}
		}
	}
	return secWatchers // [[watch]] entries carry their own group names
}

// monOn reports whether a Monitor source is turned on (and the Monitor too).
func (m Model) monOn(src string) bool {
	return m.cfg.On("view.monitor") && m.cfg.On("monitor."+src)
}

// secOn reports whether a section is turned on; All always is.
func (m *Model) secOn(i int) bool {
	return i == secAll || m.monOn(monSecs[i].src)
}

// monItems leaves out what belongs to a section turned off.
func (m *Model) monItems(items []sources.Item) []sources.Item {
	out := items[:0:0]
	for _, it := range items {
		if m.secOn(secOf(it.Group)) {
			out = append(out, it)
		}
	}
	return out
}

func (t *radarTab) count(sec int) int {
	n := 0
	for _, it := range t.all {
		if (sec == secAll && it.Group != "Today") || (sec != secAll && secOf(it.Group) == sec) {
			n++
		}
	}
	return n
}

func (t *radarTab) setSec(m *Model, d int) {
	step := 1
	if d < 0 {
		step = -1
	}
	t.sec = (t.sec + d + len(monSecs)) % len(monSecs)
	for !m.secOn(t.sec) { // what is off isn't there; All always is
		t.sec = (t.sec + step + len(monSecs)) % len(monSecs)
	}
	t.cur.idx = 0
	*m.off("radar") = 0
	t.rebuild(t.all)
}

func (t *radarTab) rebuild(all []sources.Item) {
	t.all = all
	t.items = t.items[:0]
	t.head = t.head[:0]
	last := ""
	for _, it := range all {
		if t.sec != secAll && secOf(it.Group) != t.sec {
			continue
		}
		if it.Group != last {
			t.items = append(t.items, sources.Item{Group: it.Group})
			t.head = append(t.head, true)
			last = it.Group
		}
		t.items = append(t.items, it)
		t.head = append(t.head, false)
	}
	t.cur.clamp(len(t.items), 1<<30)
	if t.cur.idx < len(t.head) && t.head[t.cur.idx] && t.cur.idx+1 < len(t.items) {
		t.cur.idx++
	}
}

func (t *radarTab) sel() *sources.Item {
	if t.cur.idx < 0 || t.cur.idx >= len(t.items) || t.head[t.cur.idx] {
		return nil
	}
	return &t.items[t.cur.idx]
}

func (t *radarTab) key(m *Model, k tea.KeyMsg) tea.Cmd {
	n := len(t.items)
	h := m.bodyHeight()
	step := func(d int) {
		t.cur.move(d, n, h)
		if t.cur.idx >= 0 && t.cur.idx < n && t.head[t.cur.idx] {
			if t.cur.idx+d >= 0 && t.cur.idx+d < n {
				t.cur.idx += d
			} else {
				t.cur.idx -= d
			}
		}
	}
	if it := t.sel(); it != nil && it.Kind != "" { // a container or a Tilt resource
		if k.String() == "enter" {
			y := m.vs.listY0
			for i, r := range m.vs.listRows {
				if r == t.cur.idx {
					y += i + 1
					break
				}
			}
			m.itemMenu(m.sideW()+4, y)
			return nil
		}
		if cmd, ok := m.itemAction(k.String()); ok {
			return cmd
		}
	}
	switch k.String() {
	case "e":
		return m.editConfig()
	case "j", "down":
		step(1)
	case "k", "up":
		step(-1)
	case "g", "home":
		t.cur.idx = 0
		step(1)
	case "G", "end":
		t.cur.idx = n - 1
	case "enter":
		it := t.sel()
		if it == nil {
			return nil
		}
		if it.URL != "" {
			return m.openURL(it.URL)
		}
		if it.SessionID != "" {
			if s, ok := m.byID[it.SessionID]; ok {
				return m.resume(s, false)
			}
			return m.run(it.Cwd, trunc(it.Name, 24), "claude --resume "+it.SessionID)
		}
		if it.Cmd != "" {
			return m.run(it.Cwd, trunc(it.Name, 24), it.Cmd)
		}
		if it.LogPath != "" {
			return m.run(it.Cwd, T("log")+" "+trunc(it.Name, 20), "less +G "+shellQuote(it.LogPath))
		}
		// nothing to open: its detail is all there is, so go read it
		m.focusR = true
		m.setStatus(T("nothing to open here: its detail is on the right (h to come back)"), false)
	case "L":
		if it := t.sel(); it != nil && it.LogPath == "" && it.URL != "" && it.Cmd != "" { // a container: its logs
			return m.run(it.Cwd, T("log")+" "+trunc(it.Name, 20), it.Cmd)
		}
		if it := t.sel(); it != nil && it.LogPath != "" {
			return m.run(it.Cwd, T("log")+" "+trunc(it.Name, 20), "tail -n 200 -f "+shellQuote(it.LogPath))
		}
	case "o":
		if it := t.sel(); it != nil && it.Cwd != "" {
			return m.shell(it.Cwd)
		}
	case "y":
		if it := t.sel(); it != nil {
			switch {
			case it.URL != "":
				return m.copy(it.URL, T("address"))
			case it.SessionID != "":
				return m.copy(it.SessionID, T("session id"))
			case it.Cmd != "":
				return m.copy(it.Cmd, T("command"))
			}
		}
	}
	t.cur.clamp(n, h)
	return nil
}

func (m Model) itemGlyph(st sources.Status) string {
	s := m.st
	switch st {
	case sources.Running: // up and serving is a steady state: no spinner (dozens of them redrew the screen)
		return s.Yellow.Render("●")
	case sources.Blocked:
		return m.stateGlyph("blocked")
	case sources.Failed:
		return s.Red.Render("✗")
	case sources.Off:
		return s.Dim.Render("○")
	case sources.Sched:
		return s.Blue.Render("◷")
	case sources.Idle:
		return m.stateGlyph("idle")
	}
	return s.Muted.Render("✓")
}

// statusName is a Radar status as shown.
func statusName(st sources.Status) string {
	switch st {
	case sources.Running:
		return T("running")
	case sources.Idle:
		return T("idle")
	case sources.Blocked:
		return T("needs you")
	case sources.Done:
		return T("done")
	case sources.Failed:
		return T("failed")
	case sources.Off:
		return T("off")
	case sources.Sched:
		return T("scheduled")
	}
	return string(st)
}

// secBar is the row of sections on top of the Monitor.
func (t *radarTab) secBar(m *Model, x0, w int) string {
	secs := make([]secDef, len(monSecs))
	for i, sc := range monSecs {
		secs[i] = secDef{sc.name, t.count(i), m.secOn(i)} // turned off: not there at all (= customizes)
	}
	return m.sectionBar(secs, t.sec, x0, w)
}

// sectionBar is a row of sections with the one in use marked; it records where
// each one is so a click picks it (the hit index is the section's index).
func (m *Model) sectionBar(secs []secDef, cur, x0, w int) string {
	s := m.st
	m.vs.secHits = m.vs.secHits[:0]
	var parts []string
	x := x0
	for i, sc := range secs {
		if !sc.on {
			continue
		}
		n := ""
		if sc.n > 0 {
			n = fmt.Sprintf(" %d", sc.n)
		}
		r := s.Tab.Render(T(sc.name) + n)
		switch {
		case i == cur:
			r = s.TabSel.Render(T(sc.name) + n)
		case m.vs.hover == hoverAt{"sec", i, x}:
			r = s.TabHover.Render(T(sc.name) + n)
		}
		m.vs.secHits = append(m.vs.secHits, hit{x, x + lipglossW(r), i})
		x += lipglossW(r)
		parts = append(parts, r)
	}
	bar := strings.Join(parts, "")
	for room := 9; lipglossW(bar) > w && room >= 3; room-- { // shorter names first
		x = x0
		m.vs.secHits = m.vs.secHits[:0]
		parts = parts[:0]
		for i, sc := range secs {
			if !sc.on {
				continue
			}
			label := trunc(T(sc.name), room)
			if sc.n > 0 {
				label += fmt.Sprintf(" %d", sc.n)
			}
			r := s.Tab.Render(label)
			switch {
			case i == cur:
				r = s.TabSel.Render(label)
			case m.vs.hover == hoverAt{"sec", i, x}:
				r = s.TabHover.Render(label)
			}
			m.vs.secHits = append(m.vs.secHits, hit{x, x + lipglossW(r), i})
			x += lipglossW(r)
			parts = append(parts, r)
		}
		bar = strings.Join(parts, "")
	}
	if lipglossW(bar) > w { // narrow: the current section and arrows
		m.vs.secHits = m.vs.secHits[:0]
		at, n := 0, 0
		for i, sc := range secs {
			if sc.on {
				if n++; i == cur {
					at = n
				}
			}
		}
		return s.Muted.Render("← ") + s.TabActive.Render(T(secs[cur].name)) + s.Muted.Render(" →  "+fmt.Sprintf("%d/%d", at, n))
	}
	return bar
}

// offCount is how many sections are turned off.
func (t *radarTab) offCount(m *Model) int {
	n := 0
	for i := range monSecs {
		if !m.secOn(i) {
			n++
		}
	}
	return n
}

func (t *radarTab) anyOn(m *Model) bool { return t.offCount(m) < len(monSecs)-1 }

// secEmpty explains an empty section: nothing found, or how to add some.
func (t *radarTab) secEmpty(m *Model, w int) string {
	s := m.st
	switch {
	case t.sec == secAll && !t.anyOn(m):
		return s.emptyState(T("Everything in the Monitor is turned off."), [][2]string{{"=", T("customize what shows")}}, w)
	case t.sec == secAll && t.offCount(m) > 0:
		return s.emptyState(Tn(t.offCount(m), "Nothing yet; %d section is turned off.", "Nothing yet; %d sections are turned off."), [][2]string{{"=", T("customize what shows")}}, w)
	}
	msg := map[int]string{
		secAll:      T("Looking around…"),
		1:           T("Looking around…"),
		2:           T("None of your programs listens on a TCP port right now."),
		3:           T("No containers running (docker ps), or docker isn't installed."),
		4:           T("Tilt isn't running here (tilt get uiresources)."),
		5:           T("No Claude background jobs, loops or opencode sessions."),
		6:           T("No crontab entries and no systemd user timers."),
		7:           T("Hermes isn't running here (~/.hermes)."),
		secWatchers: T("Your own entries: any shell command, its first output line as the status. Add [[watch]] blocks to the config."),
	}[t.sec]
	var rows [][2]string
	if t.sec == secWatchers {
		rows = [][2]string{{"e", T("edit the config")}}
	}
	return s.emptyState(msg, rows, w)
}

func (t *radarTab) view(m *Model, w, h int) string {
	s := m.st
	inner := w - 4
	title := T("Monitor")
	if n := t.count(secAll); n > 0 {
		title += fmt.Sprintf(" · %d", n)
	}
	if len(t.items) == 0 {
		return s.panel(title, t.summary(m, inner)+"\n"+t.secEmpty(m, inner), w, h, !m.focusR)
	}
	blocks := make([][]string, len(t.items))
	for i, it := range t.items {
		if t.head[i] {
			b := []string{}
			if i > 0 {
				b = append(b, "")
			}
			blocks[i] = append(b, s.Accent.Render("▍"+T(it.Group)))
			continue
		}
		right := s.Muted.Render(ago(it.When))
		l1 := "  " + m.itemGlyph(it.Status) + " " + s.Bold.Render(trunc(it.Name, inner-12))
		l1 = fit(l1, inner-lipglossW(right)) + right
		l2 := "    " + s.Muted.Render(trunc(it.Detail, inner-4))
		lines := []string{l1}
		if it.Detail != "" {
			lines = append(lines, l2)
		}
		if i == t.cur.idx {
			st := s.RowSel
			if m.focusR {
				st = s.RowSelBlur
			}
			for j := range lines {
				lines[j] = s.Sel(st, fit(lines[j], inner))
			}
		}
		blocks[i] = lines
	}
	if off := t.offCount(m); t.sec == secAll && off > 0 { // so nothing is missing silently
		blocks = append(blocks, []string{"", s.Dim.Render(trunc(Tn(off, "%d section turned off · = customizes", "%d sections turned off · = customizes"), inner))})
	}
	return s.panel(title, t.summary(m, inner)+"\n"+m.listBlocks(blocks, t.cur.idx, "radar", h-3, secY+3), w, h, !m.focusR)
}

// summary is the line on top of the Radar: how many things are in each state.
func (t *radarTab) summary(m *Model, w int) string {
	s := m.st
	count := map[sources.Status]int{}
	groups := map[string]int{}
	for i, it := range t.items {
		if t.head[i] || it.Group == "Today" || it.Group == "This machine" {
			continue
		}
		count[it.Status]++
		groups[it.Group]++
	}
	var parts []string
	// each count is clickable: a state jumps to the next item in it, ports
	// and containers open their section
	add := func(n int, target, glyph, label string) {
		if n > 0 {
			parts = append(parts, clickable(monJumpKey+target, glyph+" "+s.Text.Render(fmt.Sprint(n))+" "+s.Muted.Render(label)))
		}
	}
	add(count[sources.Blocked], string(sources.Blocked), m.itemGlyph(sources.Blocked), T("need you"))
	add(count[sources.Failed], string(sources.Failed), m.itemGlyph(sources.Failed), T("failed"))
	add(count[sources.Running], string(sources.Running), m.itemGlyph(sources.Running), T("running"))
	add(count[sources.Sched], string(sources.Sched), m.itemGlyph(sources.Sched), T("scheduled"))
	add(groups["Open ports"], "Open ports", s.Teal.Render("⇄"), T("ports"))
	add(groups["Containers"], "Containers", s.Blue.Render("▣"), T("containers"))
	if len(parts) == 0 {
		return s.Muted.Render(trunc(T("all quiet")+" · "+T("← → sections"), w))
	}
	return trunc(strings.Join(parts, s.Dim.Render("  ·  ")), w)
}

func (t *radarTab) detail(m *Model, w, h int) string {
	s := m.st
	it := t.sel()
	inner := w - 4
	if it == nil {
		return s.panel(T("Detail"), "", w, h, m.focusR)
	}
	var ls []string
	for _, l := range wrap(it.Name, inner, 2) {
		ls = append(ls, s.Bold.Render(l))
	}
	ls = append(ls, m.itemGlyph(it.Status)+" "+s.Text.Render(statusName(it.Status))+s.Muted.Render("  · "+T(it.Group)), "")
	for _, kv := range it.Fields {
		if kv.V == "" {
			continue
		}
		vl := wrap(kv.V, inner-12, 6)
		for i, l := range vl {
			k := ""
			if i == 0 {
				k = T(kv.K)
			}
			ls = append(ls, s.Label.Render(fit(k, 12))+s.Text.Render(l))
		}
	}
	var acts [][2]string
	switch {
	case it.Kind != "": // a container or a Tilt resource: its own actions, one a line
		rows := [][2]string{{"⏎", T("actions")}}
		for _, a := range m.itemActions(*it) {
			if a.label != "" && a.key != "⏎" {
				rows = append(rows, [2]string{a.key, a.label})
			}
		}
		ls = append(append(ls, ""), s.keyList(rows, inner)...)
	case it.URL != "":
		acts = append(acts, [2]string{"⏎", T("open in the browser")}, [2]string{"y", T("copy address")})
		if it.Cmd != "" {
			acts = append(acts, [2]string{"L", T("follow log")})
		}
	case it.SessionID != "":
		acts = append(acts, [2]string{"⏎", T("resume the claude session")})
	case it.Cmd != "":
		acts = append(acts, [2]string{"⏎", T("run") + " " + it.Cmd})
	case it.LogPath != "":
		acts = append(acts, [2]string{"⏎", T("read the log")})
	}
	if it.LogPath != "" {
		acts = append(acts, [2]string{"L", T("follow log")})
	}
	if it.Cwd != "" {
		acts = append(acts, [2]string{"o", T("shell")})
	}
	if len(acts) > 0 {
		ls = append(ls, "", s.hintsFit(acts, inner))
	}
	if it.LogPath != "" {
		ls = append(ls, "", s.Accent.Render(T("Log")+" · "+tilde(it.LogPath)))
		for _, l := range sources.Tail(it.LogPath, 30) {
			ls = append(ls, s.Muted.Render(trunc(l, inner)))
		}
	}
	return s.panel(T(it.Group), m.detailScroll(ls, h), w, h, m.focusR)
}

// openURL opens an address in the default browser.
func (m *Model) openURL(url string) tea.Cmd {
	return func() tea.Msg {
		for _, c := range []string{"xdg-open", "open"} {
			if _, err := exec.LookPath(c); err == nil {
				if exec.Command(c, url).Start() == nil {
					return statusMsg{Tf("opened %s", url), false}
				}
			}
		}
		return statusMsg{T("no browser opener found (xdg-open)"), true}
	}
}

// monJumpKey marks a clickable count on top of the Monitor.
const monJumpKey = "@mon:"

// monJump follows a click on a Monitor count: a group opens its section; a
// state selects the next item in it (in its section when this one hasn't it).
func (m *Model) monJump(target string) {
	t := &m.radar
	for _, sc := range monSecs {
		if len(sc.groups) == 1 && sc.groups[0] == target {
			t.setSec(m, secOf(target)-t.sec)
			return
		}
	}
	st := sources.Status(target)
	find := func() bool {
		n := len(t.items)
		for d := 1; d <= n; d++ {
			i := (t.cur.idx + d) % n
			if !t.head[i] && t.items[i].Status == st && t.items[i].Group != "Today" && t.items[i].Group != "This machine" {
				t.cur.idx = i
				return true
			}
		}
		return false
	}
	if !find() && t.sec != secAll {
		t.setSec(m, secAll-t.sec)
		find()
	}
	m.focusR = false
}
