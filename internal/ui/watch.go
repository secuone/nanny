package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/secuone/nanny/internal/claude"
	"github.com/secuone/nanny/internal/config"
)

// Refreshing on change: an inotify watch on ~/.claude says which transcripts,
// subagents and live processes changed, and only those are read again. With
// no watch (no inotify, out of watches, NANNY_NO_WATCH) everything is scanned
// every tick as before; with one, a full scan still runs now and then in case
// an event was missed.

const (
	fullScanEvery = 30 * time.Second       // with a watch: the safety net
	scanGap       = 700 * time.Millisecond // at most one scan this often, however busy the agents are
	debounce      = 80 * time.Millisecond  // a burst of writes is one change
)

type watchMsg struct {
	w   *claude.Watcher
	err error
}

type claudeMsg struct{}

type scanSoonMsg struct{}

func watchCmd() tea.Cmd {
	if os.Getenv("NANNY_NO_WATCH") != "" {
		return nil
	}
	return func() tea.Msg {
		w, err := claude.Watch()
		return watchMsg{w, err}
	}
}

func waitClaude(w *claude.Watcher) tea.Cmd {
	return func() tea.Msg {
		<-w.C
		time.Sleep(debounce)
		return claudeMsg{}
	}
}

// debugf notes what nanny does in the background, with NANNY_DEBUG set.
func debugf(format string, a ...any) {
	if os.Getenv("NANNY_DEBUG") == "" {
		return
	}
	p := filepath.Join(config.StateDir(), "debug.log")
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	fmt.Fprintf(f, time.Now().Format("15:04:05.000 ")+format+"\n", a...)
	f.Close()
}

// kick starts a scan of what changed, unless one is running (it starts again
// when that one is back) or the last one was too recent (it waits a little).
func (m *Model) kick() tea.Cmd {
	if m.scanning || len(m.pend) == 0 && !m.pendAll {
		return nil
	}
	if wait := scanGap - time.Since(m.scanAt); wait > 0 {
		if m.scanSoon {
			return nil
		}
		m.scanSoon = true
		return tea.Tick(wait, func(time.Time) tea.Msg { return scanSoonMsg{} })
	}
	m.scanning, m.scanAt = true, time.Now()
	sc, changed, all := m.scanner, m.pend, m.pendAll
	m.pend, m.pendAll = nil, false
	if all {
		return func() tea.Msg { return sessionsMsg(sc.Scan()) }
	}
	return func() tea.Msg { return sessionsMsg(sc.ScanChanged(changed)) }
}

// claudeChanged takes what the watch saw: a scan for the sessions, and the
// subagents and transcripts on screen that belong to a changed session.
func (m *Model) claudeChanged() tea.Cmd {
	paths, all := m.cw.Take()
	m.trackDue = true
	if all {
		debugf("watch: events lost, scanning everything")
		m.pendAll = true
	}
	touched := map[string]bool{}
	for p := range paths {
		if m.pend == nil {
			m.pend = map[string]bool{}
		}
		m.pend[p] = true
		if s := claude.SessionOf(p); s != "" {
			touched[s] = true
		}
	}
	cmds := []tea.Cmd{waitClaude(m.cw), m.kick()}
	if all || len(touched) > 0 {
		in := func(path string) bool { return all || touched[path] || touched[claude.SessionOf(path)] }
		cmds = append(cmds, m.refreshDetailIf(in), m.classSubsIf(in))
		if m.view != nil && in(m.view.path) {
			cmds = append(cmds, viewerCmd(m.view.path, m.view.sub))
		}
	}
	return tea.Batch(cmds...)
}

// monitorShown is whether the Monitor is on screen (its sources then refresh often).
func (m Model) monitorShown() bool {
	return m.tab == tabRadar && m.cfg.On("view.monitor") && (!m.inSpace() || m.dash)
}

// sourcesDue is which Monitor sources to collect on this tick: all of them
// while the Monitor shows, only Hermes for the sidebar's dot now and then,
// or none.
func (m *Model) sourcesDue() tea.Cmd {
	since := time.Since(m.sourcesAt)
	switch {
	case m.monitorShown() && since > m.srcEvery():
		m.sourcesAt = time.Now()
		return m.sourcesCmd()
	case !m.monitorShown() && m.cfg.On("view.monitor") && m.sidebarOn() && m.cfg.On("sidebar.hermes") && m.monOn("hermes") && since > fullScanEvery:
		m.sourcesAt = time.Now()
		return m.sourcesOnly("hermes")
	}
	return nil
}

// srcEvery grows while the Monitor's sources keep coming back the same.
func (m Model) srcEvery() time.Duration {
	return min(4*time.Second<<min(m.srcSame, 2), 15*time.Second)
}

// runningSubs reads again the subagents of agents with some running: one
// that goes quiet is done after a while, with nothing written to say so.
func (m *Model) runningSubs() tea.Cmd {
	var cmds []tea.Cmd
	for _, r := range m.live.allRows {
		if r.agent() && r.sess != nil && r.run > 0 {
			cmds = append(cmds, subsCmd(r.sess.Path))
		}
	}
	return tea.Batch(cmds...)
}
