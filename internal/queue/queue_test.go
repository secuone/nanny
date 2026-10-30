package queue

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveLoad(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	if q, err := Load(); err != nil || len(q.Tasks) != 0 || q.Auto {
		t.Fatalf("missing file should be an empty queue: %+v %v", q, err)
	}
	a := NewTask("run the tests", "/src/api", Here, "")
	b := NewTask("write docs", "/src/web", Worktree, "docs")
	if a.ID == b.ID {
		t.Fatal("ids must differ")
	}
	b.Start("web/docs", time.Now())
	b.Session = "s1"
	q := Queue{Auto: true, Tasks: []*Task{a, b}}
	if err := Save(q); err != nil {
		t.Fatal(err)
	}
	if Path() != filepath.Join(dir, "nanny", "queue.json") {
		t.Errorf("path %s", Path())
	}
	if _, err := os.Stat(Path() + ".tmp"); !os.IsNotExist(err) {
		t.Error("the temp file should be renamed away")
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !got.Auto || len(got.Tasks) != 2 {
		t.Fatalf("loaded %+v", got)
	}
	g := got.Find(b.ID)
	if g == nil || g.Status != Running || g.Session != "s1" || g.Mode != Worktree || g.Name != "docs" || g.Where != "web/docs" {
		t.Errorf("task round trip: %+v", g)
	}
	if err := os.WriteFile(Path(), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil {
		t.Error("a broken file should be an error")
	}
}

func TestMoveAndRemove(t *testing.T) {
	a, b, c := NewTask("a", "/x", New, ""), NewTask("b", "/x", New, ""), NewTask("c", "/x", New, "")
	b.Start("", time.Now())
	q := Queue{Tasks: []*Task{a, b, c}}
	if !q.Move(c.ID, -1) || q.Tasks[0] != c || q.Tasks[2] != a {
		t.Errorf("moving up should skip the running task: %v %v %v", q.Tasks[0].Prompt, q.Tasks[1].Prompt, q.Tasks[2].Prompt)
	}
	if q.Move(c.ID, -1) {
		t.Error("the first pending task can't go up")
	}
	if q.Move(b.ID, 1) {
		t.Error("only pending tasks move")
	}
	q.Remove(b.ID)
	if len(q.Tasks) != 2 || q.Find(b.ID) != nil {
		t.Error("remove")
	}
}

// The dispatcher's state machine: running → done once the agent worked and
// went idle; never-busy agents get a grace period; gone agents fail.
func TestAdvance(t *testing.T) {
	t0 := time.Now()
	run := func() *Task { tk := NewTask("p", "/x", New, ""); tk.Start("", t0); return tk }
	cases := []struct {
		states []string
		at     time.Duration
		want   string
	}{
		{[]string{"starting", "working", "working", "idle"}, time.Second, Done},
		{[]string{"blocked", "idle"}, time.Second, Done},
		{[]string{"idle"}, time.Second, Running},
		{[]string{"idle"}, StartGrace + time.Second, Done},
		{[]string{"starting"}, time.Hour, Running},
		{[]string{"working", "working"}, time.Hour, Running},
		{[]string{"exited"}, time.Second, Failed},
		{[]string{""}, time.Second, Failed},
		{[]string{"working", ""}, time.Second, Done},
	}
	for _, c := range cases {
		tk := run()
		ended := false
		for _, st := range c.states {
			ended = tk.Advance(st, t0.Add(c.at))
		}
		if tk.Status != c.want || ended != (c.want != Running) {
			t.Errorf("%v at %s: %s (ended %v), want %s", c.states, c.at, tk.Status, ended, c.want)
		}
	}
	tk := run()
	tk.Advance("exited", t0)
	if tk.Err == "" || tk.Ended.IsZero() {
		t.Errorf("a failed task says why and when: %+v", tk)
	}
	tk.Retry()
	if tk.Status != Pending || tk.Err != "" || tk.Busy {
		t.Errorf("retry: %+v", tk)
	}
	if tk.Advance("idle", t0.Add(time.Hour)) {
		t.Error("pending tasks don't advance")
	}
}

// Step ends tasks with fake agent states, Room caps how many more may start.
func TestStepAndRoom(t *testing.T) {
	t0 := time.Now()
	a, b, c, d := NewTask("a", "/x", Here, ""), NewTask("b", "/x", New, ""), NewTask("c", "/x", New, ""), NewTask("d", "/x", New, "")
	a.Start("", t0)
	b.Start("", t0)
	q := Queue{Auto: true, Tasks: []*Task{a, b, c, d}}
	if q.Room(2) != 0 || q.Room(3) != 1 {
		t.Errorf("room %d %d", q.Room(2), q.Room(3))
	}
	if p := q.Pending(); len(p) != 2 || p[0] != c {
		t.Errorf("pending %v", p)
	}
	states := map[*Task]string{a: "working", b: "blocked"}
	if ended := q.Step(func(tk *Task) string { return states[tk] }, t0.Add(time.Second)); len(ended) != 0 {
		t.Errorf("busy agents keep running: %v", ended)
	}
	states[a] = "idle"
	ended := q.Step(func(tk *Task) string { return states[tk] }, t0.Add(2*time.Second))
	if len(ended) != 1 || ended[0] != a || a.Status != Done || q.Room(2) != 1 {
		t.Errorf("a should be done: %v %s room %d", ended, a.Status, q.Room(2))
	}
	if q.Idle() {
		t.Error("tasks are still pending")
	}
	q.Tasks = []*Task{a}
	if !q.Idle() {
		t.Error("only done tasks: idle")
	}
}
