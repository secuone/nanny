package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/secuone/nanny/internal/claude"
)

// runner runs commands the way Bubble Tea does (each on its own, results
// back into Update), but leaves timers out.
type runner struct{ msgs chan tea.Msg }

func newRunner() *runner { return &runner{msgs: make(chan tea.Msg, 256)} }

func (r *runner) launch(c tea.Cmd) {
	if c == nil {
		return
	}
	go func() {
		switch msg := c().(type) {
		case tea.BatchMsg:
			for _, cc := range msg {
				r.launch(cc)
			}
		case tickMsg, frameMsg, nil:
		default:
			r.msgs <- msg
		}
	}()
}

// until updates m with what comes back until done says so or d is over.
func (r *runner) until(m tea.Model, d time.Duration, done func(Model) bool) (tea.Model, bool) {
	deadline := time.After(d)
	for {
		select {
		case msg := <-r.msgs:
			var c tea.Cmd
			m, c = m.Update(msg)
			r.launch(c)
			if done(m.(Model)) {
				return m, true
			}
		case <-deadline:
			return m, false
		}
	}
}

func appendTo(t *testing.T, p, line string) int64 {
	t.Helper()
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(line + "\n")
	f.Close()
	st, _ := os.Stat(p)
	return st.Size()
}

func sizeOf(m Model, id string) int64 {
	if s := m.byID[id]; s != nil {
		return s.Size
	}
	return -1
}

// With a watch, a transcript written to and a new subagent show within a
// second, without waiting for a tick; and a tick with nothing moving
// doesn't even draw.
func TestWatchShowsChanges(t *testing.T) {
	m, sess := withOldSubs(t)
	w, err := claude.Watch()
	if err != nil {
		t.Skip("no inotify: ", err)
	}
	defer w.Close()
	var mm tea.Model = m
	mm, cmd := mm.Update(watchMsg{w: w})
	r := newRunner()
	r.launch(cmd)

	start := time.Now()
	size := appendTo(t, sess.Path, `{"type":"user","message":{"role":"user","content":"and more"}}`)
	mm, ok := r.until(mm, time.Second, func(m Model) bool { return sizeOf(m, sess.ID) == size })
	if !ok {
		t.Fatalf("the change to the transcript didn't show within a second (size %d, want %d)", sizeOf(mm.(Model), sess.ID), size)
	}
	t.Logf("transcript change shown in %v", time.Since(start).Round(time.Millisecond))

	// a new subagent of a live agent (its folder may be new too)
	dir := filepath.Join(sess.Path[:len(sess.Path)-len(".jsonl")], "subagents")
	os.MkdirAll(dir, 0o755)
	time.Sleep(50 * time.Millisecond)
	os.WriteFile(filepath.Join(dir, "agent-new1.meta.json"), []byte(`{"agentType":"Explore","description":"Fresh one"}`), 0o644)
	appendTo(t, filepath.Join(dir, "agent-new1.jsonl"), `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hi"}]}}`)
	start = time.Now()
	hasNew := func(m Model) bool {
		for _, s := range m.subs[sess.Path] {
			if s.ID == "new1" {
				return true
			}
		}
		return false
	}
	mm, ok = r.until(mm, time.Second, hasNew)
	if !ok {
		t.Fatal("a new subagent didn't show within a second")
	}
	t.Logf("new subagent shown in %v", time.Since(start).Round(time.Millisecond))
}

// Without a watch (no inotify), every tick scans as before.
func TestNoWatchScansEveryTick(t *testing.T) {
	m, sess := withOldSubs(t)
	size := appendTo(t, sess.Path, `{"type":"user","message":{"role":"user","content":"and more"}}`)
	var mm tea.Model = m
	mm, cmd := mm.Update(tickMsg(time.Now()))
	r := newRunner()
	r.launch(cmd)
	if _, ok := r.until(mm, time.Second, func(m Model) bool { return sizeOf(m, sess.ID) == size }); !ok {
		t.Fatal("a tick without a watch should scan")
	}
}

// With a watch and nothing moving, a tick does no scan and no drawing, until
// the safety-net full scan is due.
func TestWatchIdleTick(t *testing.T) {
	m := boot(t, 150, 42).(Model)
	w, err := claude.Watch()
	if err != nil {
		t.Skip("no inotify: ", err)
	}
	defer w.Close()
	m.cw = w
	for _, s := range m.sessions { // every agent waiting for you
		if s.Live != nil {
			s.Live.Status = "idle"
		}
	}
	for k := range m.subs {
		m.subs[k] = nil
	}
	m.rebuild()
	m.alerts.toasts = nil
	m.fullAt = time.Now()
	m.vs.drawnMin = time.Now().Unix() / 60
	if m.animating() {
		t.Fatal("the demo should be still now")
	}
	mm, _ := m.Update(tickMsg(time.Now()))
	if got := mm.(Model); got.scanning || !got.vs.noop && time.Now().Unix()/60 == got.vs.drawnMin {
		t.Errorf("an idle tick scanned (%v) or drew (noop %v)", got.scanning, got.vs.noop)
	}
	m.fullAt = time.Now().Add(-time.Minute)
	if mm, _ = m.Update(tickMsg(time.Now())); !mm.(Model).scanning {
		t.Error("the full scan is due: it should run")
	}

	// frames stop while nothing moves, and come back when something does
	mm, cmd := m.Update(frameMsg(time.Now()))
	if cmd != nil || mm.(Model).vs.frameOn {
		t.Error("with nothing moving, no more frames")
	}
	var busy []*claude.Session
	for _, s := range m.sessions {
		c := *s
		if c.Live != nil {
			l := *c.Live
			l.Status = "busy"
			c.Live = &l
		}
		busy = append(busy, &c)
	}
	if _, cmd = mm.Update(sessionsMsg(busy)); cmd == nil || !m.vs.frameOn {
		t.Error("an agent at work brings the frames back")
	}
}
