package ui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/secuone/nanny/internal/i18n"
	"github.com/secuone/nanny/internal/queue"
)

// fillQueue puts one task of every status in the queue.
func fillQueue(m Model) {
	dir := filepath.Join(home, "dev", "acme-api")
	now := time.Now()
	run := queue.NewTask("Upgrade the Redis client and run the integration tests against a container", dir, queue.New, "redis")
	run.Start("acme-api/redis", now.Add(-3*time.Minute))
	done := queue.NewTask("Write the liquibase changeset with a rollback for this table.", dir, queue.Here, "")
	done.Start("acme-api/claude", now.Add(-time.Hour))
	done.Advance("working", now)
	done.Advance("idle", now)
	bad := queue.NewTask("Open the PR", filepath.Join(home, "gone"), queue.Worktree, "pr")
	bad.Fail("agent exited", now)
	m.tq.q.Tasks = []*queue.Task{
		queue.NewTask("Run the test suite and fix whatever fails, then show me the diff.", dir, queue.Here, ""),
		queue.NewTask("Check the CI logs of the last failed run and tell me what broke.", filepath.Join(home, "dev", "infra"), queue.Worktree, "ci-logs"),
		run, done, bad,
	}
}

// The queue view and its form fit every size in every language.
func TestQueueViewFits(t *testing.T) {
	for _, lang := range i18n.Langs {
		for _, size := range [][2]int{{150, 42}, {100, 30}, {80, 24}} {
			w, h := size[0], size[1]
			m := bootIn(t, lang, w, h).(Model)
			if out := fits(t, key(m, "u"), w, h, lang+" empty queue"); !strings.Contains(out, T("Nothing queued. Add tasks and nanny hands them to Claude agents, a few at a time.")[:12]) {
				t.Errorf("%s %dx%d: empty queue lacks its hint:\n%s", lang, w, h, out)
			}
			fillQueue(m)
			m = key(m, "u").(Model)
			out := fits(t, m, w, h, lang+" queue")
			for _, want := range []string{T("PENDING"), T("RUNNING"), T("DONE"), "☰ 3"} {
				if w >= 100 && !strings.Contains(out, want) {
					t.Errorf("%s %dx%d: queue view lacks %q:\n%s", lang, w, h, want, out)
				}
			}
			for _, k := range []string{"j", "j", "j", "j"} {
				m = key(m, k).(Model)
				fits(t, m, w, h, lang+" queue row")
			}
			m = key(m, "n").(Model)
			if m.task == nil || !m.task.queued {
				t.Fatalf("%s: n should open the queue form", lang)
			}
			if out := fits(t, m, w, h, lang+" queue form"); !strings.Contains(out, T("Queue a task")) {
				t.Errorf("%s %dx%d: form not shown:\n%s", lang, w, h, out)
			}
		}
	}
}

// n queues a task through the form; J/K reorder, a toggles auto, d removes,
// and everything lands in queue.json under the temp state dir.
func TestQueueForm(t *testing.T) {
	m := key(withAgents(t, 150, 42), "u").(Model) // the stand-in claude, should anything start
	if !m.onTab(tabTasks) {
		t.Fatal("u should open the task queue")
	}
	add := func(prompt, dir string) {
		t.Helper()
		m = key(m, "n").(Model)
		m.task.where.SetValue(dir)
		m.task.filter()
		m.task.prompt.SetValue(prompt)
		m = key(m, "enter").(Model)
	}
	add("first task", "infra")
	add("second task", "infra")
	if len(m.tq.q.Tasks) != 2 || m.tq.q.Tasks[0].Prompt != "first task" || filepath.Base(m.tq.q.Tasks[0].Dir) != "infra" || m.tq.q.Tasks[0].Mode != queue.New {
		t.Fatalf("queued %+v", m.tq.q.Tasks)
	}
	if m.tq.selTask().Prompt != "second task" {
		t.Error("a new task should be selected")
	}
	m = key(m, "K").(Model)
	if m.tq.q.Tasks[0].Prompt != "second task" {
		t.Error("K should move the task up")
	}
	q, err := queue.Load()
	if err != nil || len(q.Tasks) != 2 || q.Auto || q.Tasks[0].Prompt != "second task" {
		t.Fatalf("queue.json: %+v %v", q, err)
	}
	if !strings.HasPrefix(queue.Path(), home) {
		t.Fatalf("tests must not write the real state: %s", queue.Path())
	}
	// e edits the mode: space cycles here → new → worktree
	m = key(m, "e").(Model)
	if m.task == nil || m.task.edit == "" {
		t.Fatal("e should edit the pending task")
	}
	m.task.field = fieldWT
	m = key(m, " ").(Model)
	m = key(m, "enter").(Model)
	if got := m.tq.selTask(); got.Mode != queue.Worktree || len(m.tq.q.Tasks) != 2 {
		t.Errorf("edit: %+v", got)
	}
	if out := fits(t, m, 150, 42, "queue"); !strings.Contains(out, "☰ 2 queued") {
		t.Errorf("header lacks the badge:\n%s", out)
	}
	m = key(m, "d").(Model)
	m = key(m, "y").(Model)
	if len(m.tq.q.Tasks) != 1 {
		t.Error("d then y should remove the task")
	}
	m = key(m, "a").(Model)
	if q, _ := queue.Load(); !m.tq.q.Auto || !q.Auto || m.tq.q.Count(queue.Running) != 1 {
		t.Errorf("a should turn auto-dispatch on and start the task: %+v", q)
	}
	if m = key(m, "esc").(Model); m.onTab(tabTasks) {
		t.Error("esc should close the queue")
	}
	// q in the prompt library queues the selected prompt
	m = key(m, "P").(Model)
	m = key(m, "q").(Model)
	if m.task == nil || !m.task.queued || m.task.prompt.Value() == "" {
		t.Fatal("q in the library should open the queue form with the prompt")
	}
	m = key(m, "esc").(Model)
	m = key(m, "esc").(Model)
	// + in Live queues for the selected agent's folder, mode here
	m = key(m, "1").(Model)
	m = key(m, "+").(Model)
	if m.task == nil || m.task.mode != queue.Here || m.task.dir() == nil || m.task.dir().path != m.live.sel().sess.Cwd {
		t.Fatalf("+ should queue in the agent's folder: %+v", m.task)
	}
}

// The dispatcher hands a "here" task to the idle agent of its folder, waits
// for it to work and go idle, then marks it done and alerts; a folder whose
// only agent is blocked on a prompt gets nothing.
func TestDispatchHere(t *testing.T) {
	m := withAgents(t, 150, 42, demoIdle, demoBlocked)
	m = settle(t, m, func(m Model) bool {
		n := 0
		for _, r := range m.allPanes() {
			if r.pane.Agent == "claude" {
				n++
			}
		}
		return n == 2
	})
	var idle, blocked *paneRef
	for _, r := range m.allPanes() {
		r := r
		if strings.Contains(r.pane.Cwd, "OPS-88") {
			blocked = &r
		} else {
			idle = &r
		}
	}
	screenHas(t, idle.pane, "────")
	screenHas(t, blocked.pane, "Do you want to proceed?")
	tk := queue.NewTask("rerun the deploy", idle.pane.Cwd, queue.Here, "")
	stuck := queue.NewTask("bump predis", blocked.pane.Cwd, queue.Here, "")
	m.tq.q.Tasks = []*queue.Task{stuck, tk}
	m.tq.q.Auto = true
	m.dispatch()
	if tk.Status != queue.Running || m.tq.panes[tk.ID] != idle.pane {
		t.Fatalf("the idle agent should get the task: %+v", tk)
	}
	if stuck.Status != queue.Pending {
		t.Errorf("never dispatch to a blocked agent: %+v", stuck)
	}
	screenHas(t, idle.pane, "Got it: rerun the deploy")
	deadline := time.Now().Add(8 * time.Second)
	for tk.Status == queue.Running && time.Now().Before(deadline) {
		m.trackAgents()
		m = drain(m, m.scanCmd(), 0).(Model)
		m.dispatch()
		time.Sleep(100 * time.Millisecond)
	}
	if tk.Status != queue.Done || !tk.Busy {
		t.Fatalf("the task should be done after the agent worked: %+v\n%s", tk, idle.pane.Text(30))
	}
	if len(m.alerts.events) == 0 || m.alerts.events[0].kind != alertQueue {
		t.Errorf("a finished task should alert: %+v", m.alerts.events)
	}
	if stuck.Status != queue.Pending {
		t.Errorf("the blocked folder's task should still wait: %+v", stuck)
	}
	// g jumps to the agent the task went to
	m.tq.sel = tk.ID
	m = key(m, "u").(Model)
	m = key(m, "g").(Model)
	if m.onTab(tabTasks) || !m.inSpace() || m.space().Tab().Pane() != idle.pane {
		t.Error("g should go to the task's agent")
	}
}

// New-tab tasks start in the background up to queue.max_parallel; the rest wait.
func TestDispatchCap(t *testing.T) {
	m := withAgents(t, 150, 42)
	dir := filepath.Join(home, "dev", "infra")
	for _, p := range []string{"one", "two", "three"} {
		m.tq.q.Tasks = append(m.tq.q.Tasks, queue.NewTask(p, dir, queue.New, ""))
	}
	m.dispatch()
	if m.tq.q.Count(queue.Running) != 0 {
		t.Fatal("nothing starts with auto-dispatch off")
	}
	m.tq.q.Auto = true
	m.dispatch()
	if r, p := m.tq.q.Count(queue.Running), m.tq.q.Count(queue.Pending); r != 2 || p != 1 {
		t.Fatalf("max_parallel 2: %d running, %d pending", r, p)
	}
	if m.inSpace() {
		t.Error("dispatching must not switch you into the space")
	}
	p := m.tq.panes[m.tq.q.Tasks[0].ID]
	if p == nil || p.Argv[len(p.Argv)-1] != "one" {
		t.Fatalf("the task's pane runs claude with its prompt: %+v", p)
	}
	screenHas(t, p, "> one")
	// ⏎ runs the last one now, whatever the cap
	m.tq.sel = m.tq.q.Tasks[2].ID
	m = key(m, "u").(Model)
	m = key(m, "enter").(Model)
	if m.tq.q.Count(queue.Running) != 3 {
		t.Error("⏎ should start the task at once")
	}
}

// alt+1-9 types a saved prompt into a prompt box, ctrl+r cycles the folder's most used ones.
func TestPromptFill(t *testing.T) {
	m := boot(t, 150, 42).(Model)
	in := textinput.New()
	f := &promptFill{dir: filepath.Join(home, "dev", "infra")}
	if !m.fillKey(f, "ctrl+r", &in) || !strings.HasPrefix(in.Value(), "Check the CI logs") {
		t.Errorf("ctrl+r should bring the folder's most used prompt: %q", in.Value())
	}
	in.SetValue("")
	f = &promptFill{dir: filepath.Join(home, "dev", "acme-api")}
	m.fillKey(f, "ctrl+r", &in)
	first := in.Value()
	m.fillKey(f, "ctrl+r", &in)
	if in.Value() == first || in.Value() == "" {
		t.Error("ctrl+r again should cycle to the next prompt")
	}
	in.SetValue("liquibase")
	m.fillKey(f, "x", &in)
	m.fillKey(f, "ctrl+r", &in)
	if !strings.HasPrefix(in.Value(), "Write the liquibase changeset") {
		t.Errorf("ctrl+r should match what was typed: %q", in.Value())
	}
	in.SetValue("")
	if !m.fillKey(f, "alt+1", &in) || in.Value() == "" {
		t.Error("alt+1 should insert the top saved prompt")
	}
	if m.fillKey(f, "a", &in) {
		t.Error("plain keys aren't shortcuts")
	}
	// the p box on a Live agent has them, and shows them
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'.'}})
	m = mm.(Model)
	if m.modal == nil && !strings.Contains(m.status, "outside nanny") && !strings.Contains(m.status, "no prompt") {
		t.Errorf(". should ask to repeat the last prompt or say why not: %q", m.status)
	}
}
