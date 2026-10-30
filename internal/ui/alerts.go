package ui

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type alertKind string

const (
	alertDone       alertKind = "done"
	alertBlocked    alertKind = "blocked"
	alertSub        alertKind = "subagent"
	alertWorking    alertKind = "working"
	alertContext    alertKind = "context"     // context close to auto-compact
	alertQueue      alertKind = "queue"       // a queued task ended
	alertQueueEmpty alertKind = "queue-empty" // and nothing is left
)

type alertEvent struct {
	at      time.Time
	kind    alertKind
	title   string
	project string
	detail  string // what a blocked agent asks ("Bash · npm test")
	key     string // liveRow key to jump to
}

// alertState is shared (pointer) so transitions survive Model copies.
type alertState struct {
	last      map[string]string // row key -> last state
	unseen    map[string]bool   // agents that finished while you weren't looking
	events    []alertEvent
	toasts    []alertEvent
	primed    bool            // first scan only records state, never alerts
	ctxWarned map[string]bool // sessions already warned about their context
}

func newAlertState() *alertState {
	return &alertState{last: map[string]string{}, unseen: map[string]bool{}, ctxWarned: map[string]bool{}}
}

// detectAlerts compares every live row (unscoped) with its previous state.
func (m *Model) detectAlerts() {
	a := m.alerts
	seen := map[string]bool{}
	for _, r := range m.live.allRows {
		k := r.key()
		seen[k] = true
		st := r.rawState()
		prev, known := a.last[k]
		a.last[k] = st
		if !known || !a.primed || prev == st {
			continue
		}
		title, cwd := r.titleCwd()
		ev := alertEvent{at: time.Now(), title: title, project: project(cwd), key: k}
		switch {
		case r.sub != nil && prev == "running" && st == "done":
			ev.kind, ev.key = alertSub, "s:"+r.sess.ID
			ev.title = r.sub.Type + " · " + r.sub.Description
			if !m.cfg.Alerts.OnSubagent {
				continue
			}
		case r.sub == nil && st == "blocked":
			ev.kind = alertBlocked
			ev.detail = promptSummary(r.prompt())
			if !m.cfg.Alerts.OnBlocked {
				continue
			}
		case r.sub == nil && prev == "working" && (st == "idle" || st == "done"):
			ev.kind = alertDone
			if sel := m.live.sel(); m.tab != tabLive || sel == nil || sel.key() != k {
				a.unseen[k] = true
			}
			if !m.cfg.Alerts.OnDone {
				continue
			}
		case r.sub == nil && st == "working":
			delete(a.unseen, k)
			continue
		default:
			continue
		}
		m.fire(ev)
	}
	// agents that went away (process exited) leave the map
	for k := range a.last {
		if !seen[k] {
			delete(a.last, k)
			delete(a.unseen, k)
		}
	}
	m.detectContext()
	a.primed = true
}

func (m *Model) fire(ev alertEvent) {
	a := m.alerts
	a.events = append([]alertEvent{ev}, a.events...)
	if len(a.events) > 200 {
		a.events = a.events[:200]
	}
	if m.cfg.Alerts.Toast {
		a.toasts = append(a.toasts, ev)
		if len(a.toasts) > 4 {
			a.toasts = a.toasts[len(a.toasts)-4:]
		}
	}
	if m.cfg.Alerts.Sound {
		go playSound(m.soundFor(ev.kind))
	}
	head, body := ev.headline(), ev.title
	if ev.detail != "" {
		body += "\n" + ev.detail
	}
	if m.cfg.Alerts.Desktop {
		go desktopNotify(head, body)
	}
}

func (e alertEvent) headline() string {
	switch e.kind {
	case alertBlocked:
		return "● " + Tf("%s needs you", e.project)
	case alertDone:
		return "◆ " + Tf("%s finished", e.project)
	case alertSub:
		return "✓ " + T("subagent done") + " · " + e.project
	case alertContext:
		return "◔ " + Tf("%s near auto-compact", e.project)
	case alertQueue:
		return "☰ " + Tf("%s: queued task ended", e.project)
	case alertQueueEmpty:
		return "☰ " + T("queue empty")
	}
	return e.project
}

func (m *Model) soundFor(k alertKind) string {
	c := m.cfg.Alerts
	switch k {
	case alertBlocked:
		return firstFile(c.SoundBlocked, "/usr/share/sounds/freedesktop/stereo/dialog-warning.oga", "/System/Library/Sounds/Sosumi.aiff")
	case alertSub:
		return firstFile(c.SoundSub, "/usr/share/sounds/freedesktop/stereo/message.oga", "/System/Library/Sounds/Pop.aiff")
	}
	return firstFile(c.SoundDone, "/usr/share/sounds/freedesktop/stereo/complete.oga", "/System/Library/Sounds/Glass.aiff")
}

func firstFile(paths ...string) string {
	for _, p := range paths {
		if p == "" {
			continue
		}
		if strings.HasPrefix(p, "~/") {
			p = home + p[1:]
		}
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// playSound uses whatever player exists; falls back to the terminal bell.
func playSound(file string) {
	if file != "" {
		players := [][]string{{"pw-play"}, {"paplay"}, {"afplay"}, {"canberra-gtk-play", "-f"}, {"ffplay", "-nodisp", "-autoexit", "-loglevel", "quiet"}, {"aplay", "-q"}}
		for _, p := range players {
			if _, err := exec.LookPath(p[0]); err == nil {
				if exec.Command(p[0], append(p[1:], file)...).Run() == nil {
					return
				}
			}
		}
	}
	if tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0); err == nil {
		_, _ = tty.WriteString("\a")
		tty.Close()
	}
}

func desktopNotify(title, body string) {
	if runtime.GOOS == "darwin" {
		_ = exec.Command("osascript", "-e", fmt.Sprintf("display notification %q with title %q", body, title)).Run()
		return
	}
	if _, err := exec.LookPath("notify-send"); err == nil {
		_ = exec.Command("notify-send", "-a", "nanny", title, body).Run()
	}
}

// ---------------------------------------------------------------- views

const toastTTL = 7 * time.Second

func (m Model) viewToasts() string {
	s := m.st
	var boxes []string
	for _, t := range m.alerts.toasts {
		if time.Since(t.at) > toastTTL {
			continue
		}
		col := s.P.Blue
		switch t.kind {
		case alertBlocked:
			col = s.P.Red
		case alertSub:
			col = s.P.Teal
		case alertContext:
			col = s.P.Yellow
		case alertQueue, alertQueueEmpty:
			col = s.P.Accent
		}
		head := lipgloss.NewStyle().Foreground(lipgloss.Color(col)).Bold(true).Render(trunc(t.headline(), 38))
		body := s.Text.Render(trunc(t.title, 38))
		if t.detail != "" {
			body += "\n" + s.Yellow.Render(trunc("⚠ "+t.detail, 38))
		}
		hint := s.Muted.Render(T("A inbox · ! jump"))
		if t.kind == alertBlocked {
			hint = s.Muted.Render(T("A inbox · ! then ⏎ answers"))
		}
		boxes = append(boxes, lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(col)).
			Padding(0, 1).Width(44).Render(head+"\n"+body+"\n"+hint))
	}
	return strings.Join(boxes, "\n")
}

func (m *Model) pruneToasts() {
	a := m.alerts
	keep := a.toasts[:0]
	for _, t := range a.toasts {
		if time.Since(t.at) <= toastTTL {
			keep = append(keep, t)
		}
	}
	a.toasts = keep
}

// inbox lists recent alert events; enter jumps to the agent.
type inbox struct{ cur int }

func (m Model) viewInbox() string {
	s := m.st
	w := min(90, m.w-6)
	inner := w - 6
	var b strings.Builder
	sound := s.Green.Render("♪ " + T("sound on"))
	if !m.cfg.Alerts.Sound {
		sound = s.Red.Render("✕ " + T("muted"))
	}
	b.WriteString(s.Accent.Render(T("Alerts")) + s.Muted.Render("  "+Tn(len(m.alerts.events), "%d event", "%d events")+" · ") + sound + "\n\n")
	rows := min(16, m.h-10)
	if len(m.alerts.events) == 0 {
		for _, l := range wrap(T("Nothing yet. Finished agents, agents waiting for you and (optionally) subagents show up here, with a sound and a popup. Choose which ones in Settings (8)."), inner, 4) {
			b.WriteString(s.Muted.Render(l) + "\n")
		}
	}
	start := 0
	if m.inbox.cur >= rows {
		start = m.inbox.cur - rows + 1
	}
	m.vs.modalY, m.vs.modalStart, m.vs.modalN = 2, start, max(0, min(len(m.alerts.events), start+rows)-start)
	for i := start; i < min(len(m.alerts.events), start+rows); i++ {
		e := m.alerts.events[i]
		g := s.Blue.Render("◆")
		switch e.kind {
		case alertBlocked:
			g = s.Red.Render("●")
		case alertSub:
			g = s.Teal.Render("✓")
		case alertContext:
			g = s.Yellow.Render("◔")
		case alertQueue, alertQueueEmpty:
			g = s.Accent.Render("☰")
		}
		when := s.Muted.Render(e.at.Format("15:04:05"))
		what := s.Text.Render(trunc(e.title, inner-32))
		if e.detail != "" {
			what = s.Yellow.Render(trunc("⚠ "+e.detail, inner-32))
		}
		line := g + " " + s.Muted.Render(fit(trunc(e.project, 18), 18)) + " " + what
		line = fit(line, inner-9) + " " + when
		if i == m.inbox.cur {
			line = s.Sel(s.RowSel, fit(line, inner))
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\n" + s.hintsFit([][2]string{{"⏎", T("jump · answer")}, {"c", T("clear")}, {"S", T("mute/unmute")}, {"esc", T("close")}}, inner))
	return s.Modal.Width(w + 2).Render(b.String())
}

func (m Model) inboxKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	n := len(m.alerts.events)
	switch k.String() {
	case "esc", "A", "q":
		m.showInbox = false
	case "j", "down":
		m.inbox.cur = min(max(0, n-1), m.inbox.cur+1)
	case "k", "up":
		m.inbox.cur = max(0, m.inbox.cur-1)
	case "g", "home":
		m.inbox.cur = 0
	case "G", "end":
		m.inbox.cur = max(0, n-1)
	case "c":
		m.alerts.events = nil
		m.inbox.cur = 0
	case "S":
		return m.toggleSound()
	case "enter":
		m.showInbox = false
		if m.inbox.cur < n && m.viewOn(tabLive) {
			key := m.alerts.events[m.inbox.cur].key
			m.cur, m.tab = -1, tabLive
			m.scope, m.scopeSpace = "", nil
			m.rebuild()
			for i, r := range m.live.rows {
				if r.key() == key {
					m.live.cur.idx = i
					// still waiting: answer it right here
					if r.local != nil && r.sess != nil && r.state() == "blocked" {
						m.openAnswer(*r.local, r.sess.Title)
					}
				}
			}
			return m, m.refreshDetail()
		}
	}
	return m, nil
}

func (m Model) toggleSound() (tea.Model, tea.Cmd) {
	m.cfg.Alerts.Sound = !m.cfg.Alerts.Sound
	cmd := m.saveConfig()
	if m.cfg.Alerts.Sound {
		m.setStatus("♪ "+T("sound on"), false)
		go playSound(m.soundFor(alertDone))
	} else {
		m.setStatus("✕ "+T("sound muted (S to unmute)"), false)
	}
	return m, cmd
}

// ringBells turns a bell in a pane you're not looking at into an alert.
func (m *Model) ringBells() {
	for _, r := range m.allPanes() {
		if !r.pane.TakeBell() {
			continue
		}
		visible := false
		if sp := m.space(); sp == r.space && sp.Tab() == r.tab {
			visible = true
		}
		if visible {
			continue
		}
		m.fire(alertEvent{at: time.Now(), kind: alertDone, title: r.tab.Label + " · " + paneTitle(r.pane), project: r.space.Name})
	}
}
