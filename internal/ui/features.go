package ui

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Views that bring their own state, keys and data (Git, Jira, Insights) plug
// in through featView; model.go, views.go, mouse.go and the rest only ask it.
// A feature's state hangs off a pointer in Model (the model is passed by value).
// Features fetch only from due(), which the tick, opening the view and R call,
// and say busy() while a request is on its way so frames run for the spinner.

// secDef is one section on a view's bar (like the Monitor's): on = shown,
// n = the count next to its name (0 = none).
type secDef struct {
	name string
	n    int
	on   bool
}

// bgMsg marks a message that comes back from the background: it is drawn at
// the next flush with the others, not at once (see background).
type bgMsg interface{ bgMsg() }

type featView interface {
	// sections: the bar under the views bar; none = no bar
	secs(m *Model) []secDef
	sec() int
	setSec(m *Model, i int) tea.Cmd
	// the view: list left, detail right
	view(m *Model, w, h int) string
	detail(m *Model, w, h int) string
	key(m *Model, k tea.KeyPressMsg) tea.Cmd
	click(m *Model, row int) tea.Cmd // a click on a list row (-1: none); a double click then presses ⏎
	footer(m *Model) [][2]string
	help() []helpSec
	palette(m *Model) []paletteEntry
	badge(m *Model) string   // on the top bar; "" = nothing
	barNote(m *Model) string // right-aligned on the sections bar; "" = nothing
	// due is called on every tick, when the view opens (force) and on R
	// (force); it returns the fetch if one is needed, nil when off or fresh
	due(m *Model, force bool) tea.Cmd
	update(m *Model, msg tea.Msg) (tea.Cmd, bool) // its own messages; true = taken
	typing() bool                                 // a text box has the keys
	busy() bool                                   // a request is on its way
	selKey(m *Model) string                       // the selection, to reset the detail scroll
}

// featFake lets a test stand in for a feature; nothing else sets it.
var featFake = map[tab]featView{}

// feat is the feature behind a view, nil for the other views.
func (m Model) feat(t tab) featView {
	if f, ok := featFake[t]; ok {
		return f
	}
	switch t {
	case tabGit:
		if m.gitv != nil {
			return m.gitv
		}
	case tabJira:
		if m.jira != nil {
			return m.jira
		}
	case tabInsights:
		if m.ins != nil {
			return m.ins
		}
	}
	return nil
}

// feats are all of them, in the order of the views.
func (m Model) feats() []featView {
	var out []featView
	for _, t := range []tab{tabGit, tabJira, tabInsights} {
		if f := m.feat(t); f != nil {
			out = append(out, f)
		}
	}
	return out
}

// featSecs are the sections of the view on screen, nil without a bar.
func (m Model) featSecs() []secDef {
	if f := m.feat(m.tab); f != nil && m.dashShown() && m.viewOn(m.tab) {
		return f.secs(&m)
	}
	return nil
}

// hasSecs says whether a bar of sections shows under the views bar.
func (m Model) hasSecs() bool { return m.onTab(tabRadar) || len(m.featSecs()) > 0 }

// stepSec moves a feature to the next (d=1) or previous (d=-1) section that is on.
func (m *Model) stepSec(f featView, d int) tea.Cmd {
	secs := f.secs(m)
	n := len(secs)
	for i := 1; i <= n; i++ {
		if j := ((f.sec()+d*i)%n + n) % n; secs[j].on {
			return f.setSec(m, j)
		}
	}
	return nil
}

// featKey gives a key to a feature; the detail scroll resets when the selection moves.
func (m Model) featKey(f featView, k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	before := m.selKey()
	cmd := f.key(&m, k)
	if m.selKey() != before {
		m.detailOff = 0
		return m, tea.Batch(cmd, m.refreshDetail())
	}
	return m, cmd
}

// featUpdate offers a message to every feature, in the order of the views.
func (m *Model) featUpdate(msg tea.Msg) (tea.Cmd, bool) {
	for _, f := range m.feats() {
		if cmd, ok := f.update(m, msg); ok {
			return cmd, true
		}
	}
	return nil, false
}

// featDue asks every feature what to fetch on this tick.
func (m *Model) featDue(force bool) tea.Cmd {
	var cmds []tea.Cmd
	for _, f := range m.feats() {
		cmds = append(cmds, f.due(m, force))
	}
	return tea.Batch(cmds...)
}

// featBusy is true while any feature waits for an answer.
func (m Model) featBusy() bool {
	for _, f := range m.feats() {
		if f.busy() {
			return true
		}
	}
	return false
}

// featBase does nothing for every part of featView that a view can leave out;
// a feature embeds it and writes the rest.
type featBase struct{}

func (featBase) secs(*Model) []secDef                   { return nil }
func (featBase) sec() int                               { return 0 }
func (featBase) setSec(*Model, int) tea.Cmd             { return nil }
func (featBase) click(*Model, int) tea.Cmd              { return nil }
func (featBase) footer(*Model) [][2]string              { return nil }
func (featBase) help() []helpSec                        { return nil }
func (featBase) palette(*Model) []paletteEntry          { return nil }
func (featBase) badge(*Model) string                    { return "" }
func (featBase) barNote(*Model) string                  { return "" }
func (featBase) due(*Model, bool) tea.Cmd               { return nil }
func (featBase) update(*Model, tea.Msg) (tea.Cmd, bool) { return nil, false }
func (featBase) typing() bool                           { return false }
func (featBase) busy() bool                             { return false }
func (featBase) selKey(*Model) string                   { return "" }

// secKey is the key hint that opens a view on one of its sections: "@sec:<tab>:<i>".
func secKey(t tab, i int) string { return "@sec:" + strconv.Itoa(int(t)) + ":" + strconv.Itoa(i) }

// showSec opens a view on a section, from a click on a badge.
func (m Model) showSec(spec string) (tea.Model, tea.Cmd) {
	ts, is, _ := strings.Cut(spec, ":")
	t, err1 := strconv.Atoi(ts)
	i, err2 := strconv.Atoi(is)
	if err1 != nil || err2 != nil || t < 0 || t >= len(tabNames) {
		return m, nil
	}
	mm, cmd := m.switchTab(tab(t))
	m = mm.(Model)
	if f := m.feat(tab(t)); f != nil && m.viewOn(tab(t)) {
		cmd = tea.Batch(cmd, f.setSec(&m, i))
	}
	return m, cmd
}

// tabKey is the key that opens a view: its number, or a letter for the later ones.
func tabKey(t tab) string {
	switch t {
	case tabJira:
		return "E"
	case tabInsights:
		return "I"
	}
	return strconv.Itoa(int(t) + 1)
}

// jiraKeyRe finds an issue key (PAY-412) in a branch or a title.
var jiraKeyRe = regexp.MustCompile(`\b[A-Z][A-Z0-9]{1,9}-\d+\b`)

// Hooks between features, nil until the feature that owns them sets them
// (Jira does); the ones that read them check for nil.
var (
	jiraBrowseURL   func(m *Model, key string) string
	jiraClosedSince func(m *Model, since time.Time) (int, bool)
)
