package ui

import (
	"path/filepath"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/secuone/nanny/internal/ws"
)

// The view bar (Live, Sessions, Tasks, Prompts…) is the same at home and in
// every space. At home it shows everything; in a space it is scoped to the
// space's folders, and "0 terminal" goes back to its panes.

// dashShown is true when the body shows the views rather than a space's panes.
func (m Model) dashShown() bool { return !m.inSpace() || m.dash }

// onTab is true when view t is on screen.
func (m Model) onTab(t tab) bool { return m.dashShown() && m.tab == t }

// libShown is true when the prompt library is on screen.
func (m Model) libShown() bool { return m.onTab(tabPrompts) && m.lib != nil }

// spaceScope is the space the views are scoped to: the current one, else the
// one picked from home (prefix f, the manager), if any.
// The global views (Prompts, Skills, Monitor, Settings, Nursery) show the same
// in every space.
func (m Model) spaceScope() *ws.Space {
	if m.inSpace() {
		if m.dash && globalView(int(m.tab)) {
			return nil
		}
		return m.space()
	}
	return m.scopeSpace
}

// scopeNote says, in a view's title, what the view is showing: all spaces,
// this space's repos, or — for a space opened on your home — everything.
func (m Model) scopeNote() string {
	sp := m.spaceScope()
	switch {
	case sp == nil && m.scope != "":
		return " · " + Tf("only %s", m.scope)
	case sp == nil:
		return " · " + T("all spaces")
	case sp.Broad():
		return " · " + T("all of ~")
	}
	var names []string
	for _, d := range sp.Dirs() {
		names = append(names, filepath.Base(d))
	}
	return " · " + Tf("only %s", trunc(strings.Join(names, ", "), 40))
}

// show puts view t on screen, inside the current space when there is one.
func (m *Model) show(t tab) {
	m.tab, m.focusR, m.detailOff = t, false, 0
	if m.inSpace() {
		m.dash = true
	}
}

// back leaves a view: to the terminals in a space, else to Live.
func (m *Model) back() {
	if m.inSpace() {
		m.dash = false
		m.layoutPanes()
		return
	}
	m.tab = m.homeTab()
}

// typing is true while a text box of the current view has the keys.
func (m Model) typing() bool {
	switch m.tab {
	case tabPrompts:
		return m.lib != nil && (m.lib.filter.Focused() || m.lib.ed != nil)
	case tabSessions:
		return m.sess.filter.Focused()
	case tabSkills:
		return m.skills.filter.Focused()
	case tabSettings:
		return m.settings.editing != nil
	}
	if f := m.feat(m.tab); f != nil {
		return f.typing()
	}
	return false
}

// viewSwitchKey moves between views: 1-9 pick one, tab cycles the bar, 0
// goes back to a space's terminals. ok is false for any other key.
func (m Model) viewSwitchKey(k tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	if m.typing() {
		return m, nil, false
	}
	switch ks := k.String(); ks {
	case "9": // the classroom, in a browser window
		return m, m.openClassroom(), true
	case "E":
		mm, cmd := m.switchTab(tabJira)
		return mm, cmd, true
	case "I":
		mm, cmd := m.switchTab(tabInsights)
		return mm, cmd, true
	case "1", "2", "3", "4", "5", "6", "7", "8":
		mm, cmd := m.switchTab(tab(ks[0] - '1'))
		return mm, cmd, true
	case "0":
		if m.inSpace() {
			m.back()
			return m, nil, true
		}
	case "tab", "shift+tab":
		bar := m.barTabs()
		if len(bar) == 0 {
			return m, nil, true
		}
		i := 0
		for j, t := range bar {
			if t == m.tab {
				i = j
			}
		}
		d := 1
		if ks == "shift+tab" {
			d = len(bar) - 1
		}
		mm, cmd := m.switchTab(bar[(i+d)%len(bar)])
		return mm, cmd, true
	}
	return m, nil, false
}

// ---------------------------------------------------------------- clickable keys

// Key hints ("n new", "⏎ resume") carry invisible markers around them while
// rendering; View strips them and records where each landed, so a click on a
// hint presses its key. The markers are APC strings: zero width for layout.
const (
	keyMark    = "\x1b_nk"
	keyMarkEnd = "\x1b\\"
)

type keyHit struct {
	y, x0, x1 int
	key       string
}

// clickable marks s as a click target that presses key.
func clickable(key, s string) string {
	if key == "" {
		return s
	}
	return keyMark + ";" + key + keyMarkEnd + s + keyMark + keyMarkEnd
}

// harvestKeys strips the markers from a rendered frame and returns where each
// target landed.
func harvestKeys(out string) (string, []keyHit) {
	if !strings.Contains(out, keyMark) {
		return out, nil
	}
	lines := strings.Split(out, "\n")
	var hits []keyHit
	for y, l := range lines {
		if !strings.Contains(l, keyMark) {
			continue
		}
		var b strings.Builder
		var open *keyHit
		closeAt := func() {
			if open != nil {
				if open.x1 = ansi.StringWidth(b.String()); open.x1 > open.x0 {
					hits = append(hits, *open)
				}
				open = nil
			}
		}
		for {
			i := strings.Index(l, keyMark)
			if i < 0 {
				b.WriteString(l)
				break
			}
			b.WriteString(l[:i])
			l = l[i+len(keyMark):]
			j := strings.Index(l, keyMarkEnd)
			if j < 0 {
				break
			}
			body := l[:j]
			l = l[j+len(keyMarkEnd):]
			closeAt()
			if strings.HasPrefix(body, ";") {
				open = &keyHit{y: y, x0: ansi.StringWidth(b.String()), key: body[1:]}
			}
		}
		closeAt()
		lines[y] = b.String()
	}
	return strings.Join(lines, "\n"), hits
}

// hintKey is the key press a hint label stands for; ok is false for labels
// that name several keys ("j/k", "1-9", "←→").
func hintKey(label string) (tea.KeyMsg, bool) {
	if f := strings.Fields(label); len(f) > 1 {
		label = f[0] // "v -", "n p": the first
	}
	switch strings.ToLower(label) {
	case "⏎", "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}, true
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}, true
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}, true
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}, true
	}
	ctrl := ""
	switch {
	case strings.HasPrefix(label, "^") && utf8.RuneCountInString(label) == 2:
		ctrl = label[1:]
	case strings.HasPrefix(label, "ctrl+") && len(label) == 6:
		ctrl = label[5:]
	}
	if c := strings.ToLower(ctrl); c != "" && c[0] >= 'a' && c[0] <= 'z' {
		return tea.KeyMsg{Type: tea.KeyCtrlA + tea.KeyType(c[0]-'a')}, true
	}
	if utf8.RuneCountInString(label) == 1 {
		r, _ := utf8.DecodeRuneInString(label)
		if strings.ContainsRune("←→↑↓", r) {
			return tea.KeyMsg{}, false
		}
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}, true
	}
	return tea.KeyMsg{}, false
}

// keyClick presses the hint under the pointer, if any. Over a space's
// terminals hints are prefix commands: a plain key would go to the pane.
func (m Model) keyClick(x, y int) (tea.Model, tea.Cmd, bool) {
	for i := len(m.vs.keyHits) - 1; i >= 0; i-- {
		h := m.vs.keyHits[i]
		if y != h.y || x < h.x0 || x >= h.x1 {
			continue
		}
		if target, ok := strings.CutPrefix(h.key, monJumpKey); ok {
			m.monJump(target)
			return m, nil, true
		}
		if h.key == appMenuKey {
			m.appMenu(h.x0, y+1)
			return m, nil, true
		}
		if h.key == profileChipKey {
			m.profileMenu(h.x0, y+1)
			return m, nil, true
		}
		if spec, ok := strings.CutPrefix(h.key, "@sec:"); ok { // a view on one of its sections
			mm, cmd := m.showSec(spec)
			return mm, cmd, true
		}
		if st, ok := strings.CutPrefix(h.key, "@only:"); ok {
			mm, cmd := m.showOnly(st)
			return mm, cmd, true
		}
		k, ok := hintKey(h.key)
		if !ok {
			return m, nil, false
		}
		if (m.inSpace() && !m.dash || m.prefix) && m.noModal() {
			m.prefix = false
			mm, cmd := m.prefixCmd(k.String())
			return mm, cmd, true
		}
		mm, cmd := m.key(k)
		return mm, cmd, true
	}
	return m, nil, false
}

// showOnly opens Live with only the agents in one state, from a click on the
// header counts; a second click on the same count shows them all again.
func (m Model) showOnly(st string) (tea.Model, tea.Cmd) {
	if !m.viewOn(tabLive) {
		m.toLive() // says it is off
		return m, nil
	}
	if m.live.only == st && m.dash && m.onTab(tabLive) {
		st = ""
	}
	m.live.only = st
	if m.inSpace() {
		m.dash = true
	}
	mm, cmd := m.switchTab(tabLive)
	m = mm.(Model)
	m.focusR = false
	m.live.cur.idx = 0
	m.live.rebuild(&m)
	return m, cmd
}

// noModal is true when nothing pops up over the screen.
func (m Model) noModal() bool {
	return !m.help && m.pal == nil && m.themes == nil && !m.showInbox && m.modal == nil && m.task == nil &&
		m.answer == nil && m.disc == nil && m.mgr == nil && m.cust == nil && m.menu == nil && m.welcome == nil && m.setup == nil && !(m.libShown() && m.lib.ed != nil)
}
