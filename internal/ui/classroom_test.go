package ui

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/secuone/nanny/internal/classroom"
	"github.com/secuone/nanny/internal/claude"
)

// withOldSubs gives the first live agent one running subagent and two that
// finished an hour ago.
func withOldSubs(t *testing.T) (Model, *claude.Session) {
	t.Helper()
	m := boot(t, 150, 42).(Model)
	var sess *claude.Session
	for _, r := range m.live.rows {
		if r.agent() && r.sess != nil {
			sess = r.sess
			break
		}
	}
	if sess == nil {
		t.Fatal("demo has no live agent")
	}
	old := time.Now().Add(-time.Hour)
	m.subs[sess.Path] = []*claude.Subagent{
		{ID: "run", Type: "Explore", Description: "Map refunds", Running: true, Started: time.Now(), Modified: time.Now()},
		{ID: "o1", Type: "Plan", Description: "Old plan", Started: old, Modified: old},
		{ID: "o2", Type: "Explore", Description: "Older map", Started: old, Modified: old.Add(-time.Minute)},
	}
	m.rebuild()
	return m, sess
}

func liveKeys(m Model) string {
	var ks []string
	for _, r := range m.live.rows {
		ks = append(ks, r.key())
	}
	return strings.Join(ks, " ")
}

// Subagents that finished a while ago fold into one row under their agent;
// ⏎ or a click unfolds them. The fold is Live's alone: the sidebar and the
// agent counts never see it.
func TestLiveFoldsFinishedSubagents(t *testing.T) {
	m, sess := withOldSubs(t)
	ks := liveKeys(m)
	if !strings.Contains(ks, "sub:run") || strings.Contains(ks, "sub:o1") || !strings.Contains(ks, "fold:"+sess.ID) {
		t.Fatalf("running shown, old ones folded: %s", ks)
	}
	if out := ansi.Strip(m.View()); !strings.Contains(out, "2 finished subagents") || !strings.Contains(out, "Old plan") {
		t.Errorf("the fold says how many and the last one:\n%s", out)
	}
	for _, r := range m.live.allRows {
		if r.fold > 0 {
			t.Fatal("the sidebar must not get the fold row")
		}
	}
	agents := countAgents(m.live.rows)
	for i, r := range m.live.rows {
		if r.fold > 0 {
			m.live.cur.idx = i
		}
	}
	enter := tea.KeyMsg{Type: tea.KeyEnter}
	m.live.key(&m, enter) // its commands would reload the demo's own subagents
	if ks := liveKeys(m); !strings.Contains(ks, "sub:o1") || !strings.Contains(ks, "sub:o2") || !strings.Contains(ks, "fold:"+sess.ID) {
		t.Fatalf("⏎ unfolds them, the fold stays to fold them back: %s", ks)
	}
	if r := m.live.sel(); r == nil || r.fold == 0 {
		t.Error("the cursor stays on the fold")
	}
	if countAgents(m.live.rows) != agents {
		t.Error("a fold is not an agent")
	}
	m.live.key(&m, enter)
	if ks := liveKeys(m); strings.Contains(ks, "sub:o1") {
		t.Errorf("⏎ again folds them: %s", ks)
	}
}

// The detail of an agent lists every finished subagent, newest first.
func TestLiveDetailListsFinished(t *testing.T) {
	m, sess := withOldSubs(t)
	for i, r := range m.live.rows {
		if r.agent() && r.sess == sess {
			m.live.cur.idx = i
		}
	}
	out := ansi.Strip(m.live.detail(&m, 70, 40))
	i, j := strings.Index(out, "Old plan"), strings.Index(out, "Older map")
	if !strings.Contains(out, "2 finished") || i < 0 || j < i {
		t.Errorf("finished ones, newest first:\n%s", out)
	}
}

// The 3D classroom page gets every live agent with all its subagents.
func TestClassroomSnapshot(t *testing.T) {
	m, sess := withOldSubs(t)
	s, err := classroom.Start()
	if err != nil {
		t.Fatal(err)
	}
	m.class = s
	m.pushClassroom()
	r, err := http.Get(s.URL() + "state.json")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	var snap classroom.Snapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		t.Fatal(err)
	}
	var cls *classroom.Class
	for i := range snap.Classes {
		if snap.Classes[i].ID == sess.ID {
			cls = &snap.Classes[i]
		}
	}
	if cls == nil || len(cls.Kids) != 3 || cls.Title == "" || cls.State == "" {
		t.Fatalf("class with its three kids: %+v", cls)
	}
	if snap.Words["recess"] == "" {
		t.Error("the page's words are sent translated")
	}
}
