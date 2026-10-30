package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/secuone/nanny/internal/term"
	"github.com/secuone/nanny/internal/ws"
)

const demoBlocked = "1b2c3d4e-5f60-4718-293a-4b5c6d7e8f90"
const demoIdle = "2c3d4e5f-6071-4829-3a4b-5c6d7e8f9012"

// fits checks the rendered frame is exactly w×h.
func fits(t *testing.T, m tea.Model, w, h int, what string) string {
	t.Helper()
	out := viewOf(m)
	lines := strings.Split(out, "\n")
	if len(lines) != h {
		t.Errorf("%dx%d %s: %d lines", w, h, what, len(lines))
	}
	for i, l := range lines {
		if lw := lipgloss.Width(l); lw > w {
			t.Errorf("%dx%d %s line %d is %d wide: %q", w, h, what, i, lw, ansi.Strip(l))
		}
	}
	return ansi.Strip(out)
}

// withAgents boots the demo with the stand-in claude running in nanny panes.
func withAgents(t *testing.T, w, h int, ids ...string) Model {
	t.Helper()
	m := boot(t, w, h).(Model)
	t.Setenv("HOME", home)
	t.Setenv("PATH", filepath.Join(home, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, id := range ids {
		cwd := filepath.Join(home, "dev", "infra")
		if id == demoBlocked {
			cwd = filepath.Join(home, "dev", "acme-api", ".claude", "worktrees", "OPS-88")
		}
		sp := ws.NewSpace(filepath.Base(cwd), cwd)
		p, err := m.spawn(cwd, []string{"claude", "--resume", id})
		if err != nil {
			t.Fatal(err)
		}
		tb := ws.NewTab("agent " + id[:4])
		tb.Panes = []*term.Pane{p}
		sp.Tabs = append(sp.Tabs, tb)
		m.spaces = append(m.spaces, sp)
	}
	t.Cleanup(func() {
		for _, r := range m.allPanes() {
			r.pane.Close()
		}
	})
	return m
}

// settle rescans until cond holds (agents register through ~/.claude/sessions).
func settle(t *testing.T, m Model, cond func(Model) bool) Model {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		m.trackAgents()
		m = drain(m, m.scanCmd(), 0).(Model)
		if cond(m) {
			return m
		}
		time.Sleep(50 * time.Millisecond)
	}
	for _, r := range m.allPanes() {
		t.Logf("pane %s state %s:\n%s", r.pane.Argv, r.pane.AgentState(), r.pane.Text(60))
	}
	for _, r := range m.live.rows {
		t.Logf("row %s local=%v", r.key(), r.local != nil)
	}
	t.Fatal("condition never held")
	return m
}

func screenHas(t *testing.T, p *term.Pane, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(p.Text(60), want) {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("pane never showed %q:\n%s", want, p.Text(60))
}

func blockedRow(m Model) int {
	for i, r := range m.live.rows {
		if r.sess != nil && r.sess.ID == demoBlocked && r.sub == nil && r.local != nil && r.prompt() != nil {
			return i
		}
	}
	return -1
}

// A permission prompt shows in Live, the sidebar and a popup, and y answers it in the pane.
func TestAnswerPermissionPrompt(t *testing.T) {
	m := withAgents(t, 150, 42, demoBlocked)
	m = settle(t, m, func(m Model) bool { return blockedRow(m) >= 0 })
	m.live.cur.idx = blockedRow(m)
	out := fits(t, m, 150, 42, "live blocked")
	for _, want := range []string{"⚠ Bash · docker compose run", "Waiting for you", "Do you want to proceed?", "⏎ answer"} {
		if !strings.Contains(out, want) {
			t.Errorf("live view lacks %q", want)
		}
	}
	mm := key(m, "enter").(Model)
	if mm.answer == nil {
		t.Fatal("⏎ on a blocked agent should open the answer popup")
	}
	for _, size := range [][2]int{{150, 42}, {100, 30}, {80, 24}} {
		mm.w, mm.h = size[0], size[1]
		out = fits(t, mm, size[0], size[1], "answer popup")
		if !strings.Contains(out, "don't ask again") {
			t.Errorf("%dx%d popup lacks the options", size[0], size[1])
		}
	}
	mm.w, mm.h = 150, 42
	p := mm.answer.ref.pane
	mm = key(mm, "y").(Model)
	if mm.answer != nil {
		t.Error("y should close the popup")
	}
	screenHas(t, p, "OK (12 tests")
	if panePrompt(p) != nil {
		t.Error("prompt still parsed after answering")
	}
}

// Space picks agents, p sends one prompt to all of them.
func TestBroadcastPrompt(t *testing.T) {
	m := withAgents(t, 150, 42, demoIdle, demoIdle)
	m = settle(t, m, func(m Model) bool {
		n := 0
		for _, r := range m.live.rows {
			if r.local != nil {
				n++
			}
		}
		return n >= 1
	})
	panes := m.allPanes()
	for _, r := range panes {
		screenHas(t, r.pane, "────")
	}
	for i, r := range m.live.rows {
		if r.local != nil {
			m.live.cur.idx = i
			m = key(m, " ").(Model)
		}
	}
	if len(m.live.marks) == 0 {
		t.Fatal("space should pick the agent")
	}
	if out := fits(t, m, 150, 42, "picked"); !strings.Contains(out, "picked") || !strings.Contains(out, "send to") {
		t.Error("picked agents should show in the title and footer")
	}
	m = key(m, "p").(Model)
	if m.modal == nil {
		t.Fatal("p with picks should ask for the prompt")
	}
	m.modal.input.SetValue("run the tests") // typing blinks the cursor: each key waits a tick
	m = key(m, "enter").(Model)
	if len(m.live.marks) != 0 {
		t.Error("sending should clear the picks")
	}
	// both panes run the same session id, so Live lists one; the picked one got it
	got := 0
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && got == 0 {
		for _, r := range panes {
			if strings.Contains(r.pane.Text(60), "Got it: run the tests") {
				got++
			}
		}
		time.Sleep(30 * time.Millisecond)
	}
	if got == 0 {
		t.Error("no pane received the broadcast")
	}
}

// n opens the new-task form; ⏎ starts claude with the prompt, in a fresh worktree when asked.
func TestNewTask(t *testing.T) {
	m := withAgents(t, 150, 42)
	m = key(m, "n").(Model)
	if m.task == nil {
		t.Fatal("n should open the new-task form")
	}
	for _, size := range [][2]int{{150, 42}, {100, 30}, {80, 24}} {
		m.w, m.h = size[0], size[1]
		if out := fits(t, m, size[0], size[1], "new task"); !strings.Contains(out, "New task") {
			t.Errorf("%dx%d: form not shown", size[0], size[1])
		}
	}
	m.w, m.h = 150, 42
	// filter the folder list to infra
	m = key(m, "shift+tab").(Model)
	m = key(m, "shift+tab").(Model)
	if m.task.field != fieldWhere {
		t.Fatalf("shift+tab twice from the prompt should reach the folder, got field %d", m.task.field)
	}
	m = key(m, "i").(Model)
	m.task.where.SetValue("infra")
	m.task.filter()
	if d := m.task.dir(); d == nil || filepath.Base(d.path) != "infra" {
		t.Fatalf("folder filter picked %+v", d)
	}
	m.task.name.SetValue("Cache layer")
	m.task.prompt.SetValue("speed up the deploy")
	m = key(m, "shift+tab").(Model)
	if m.task.field != fieldWT {
		t.Fatalf("shift+tab from the folder should wrap to the worktree box, got %d", m.task.field)
	}
	m = key(m, " ").(Model)
	if !m.task.wt || m.task.branch() != "cache-layer" {
		t.Fatalf("worktree toggle / branch: %v %q", m.task.wt, m.task.branch())
	}
	m = key(m, "enter").(Model)
	if !m.inSpace() {
		t.Fatal("the task should open in a space")
	}
	tb := m.space().Tab()
	p := tb.Pane()
	if tb.Label != "Cache layer" || !strings.HasSuffix(p.Cwd, filepath.Join("infra.worktrees", "cache-layer")) {
		t.Errorf("tab %q in %s", tb.Label, p.Cwd)
	}
	if a := p.Argv; a[len(a)-1] != "speed up the deploy" {
		t.Errorf("argv %q", a)
	}
	screenHas(t, p, "> speed up the deploy")
	if !m.space().Contains(p.Cwd) {
		t.Error("the new worktree should belong to the infra space")
	}
}

// `nanny demo` restores two agent panes, one of them blocked on a prompt.
func TestDemoLayout(t *testing.T) {
	boot(t, 100, 30)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".state"))
	st, err := ws.Load()
	if err != nil || len(st.Spaces) != 2 || st.Active != -1 {
		t.Fatalf("demo layout: %v %+v", err, st)
	}
	if fi, err := os.Stat(filepath.Join(home, "bin", "claude")); err != nil || fi.Mode()&0o111 == 0 {
		t.Error("demo stand-in claude missing or not executable")
	}
}

// A snippet picked in the palette is typed into the focused pane.
func TestSnippetIntoPane(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := withAgents(t, 150, 42, demoIdle)
	m.cur = len(m.spaces) - 1
	m.layoutPanes()
	p := m.space().Tab().Pane()
	screenHas(t, p, "────")
	mm, _ := m.Update(keyPress("ctrl+b")) // in a space the palette is prefix :
	m = key(mm, ":").(Model)
	m.pal.input.SetValue("snippet tests")
	m.pal.filter()
	if e := m.pal.entries[m.pal.shown[0]]; e.kind != "snippet" {
		t.Fatalf("top result %q", e.label)
	}
	m = key(m, "enter").(Model)
	screenHas(t, p, "Run the test suite")
	if out := fits(t, m, 150, 42, "space"); !strings.Contains(out, "snippet") {
		t.Error("status should say the snippet was typed")
	}
}

// C on a worktree opens its changes; t switches to the whole branch.
func TestReviewChanges(t *testing.T) {
	m := boot(t, 150, 42)
	m = key(m, "3")
	mm := m.(Model)
	for i, r := range mm.wt.rows {
		if r.wt != nil && filepath.Base(r.wt.Path) == "PAY-412" {
			mm.wt.cur.idx = i
		}
	}
	m = key(mm, "C")
	r := m.(Model).rev
	if r == nil || !r.loaded {
		t.Fatal("C should open the changes view")
	}
	names := func(r *review) string {
		var ns []string
		for _, f := range r.files {
			ns = append(ns, f.Status+" "+f.Path)
		}
		return strings.Join(ns, ", ")
	}
	if r.branchUp || !strings.Contains(names(r), "M README.md") || !strings.Contains(names(r), "? src/Payments/RefundSplitPayment.php") {
		t.Errorf("uncommitted files: %s", names(r))
	}
	for _, size := range [][2]int{{150, 42}, {100, 30}, {80, 24}} {
		mm := m.(Model)
		mm.w, mm.h = size[0], size[1]
		out := fits(t, mm, size[0], size[1], "changes")
		if !strings.Contains(out, "Changes") || !strings.Contains(out, "README.md") {
			t.Errorf("%dx%d changes view incomplete:\n%s", size[0], size[1], out)
		}
	}
	if out := ansi.Strip(viewOf(m)); !strings.Contains(out, "+Split payments refund each part") {
		t.Errorf("diff hunk not shown:\n%s", out)
	}
	m = key(m, "t")
	r = m.(Model).rev
	if !r.branchUp || !strings.Contains(names(r), "src/Ledger/Entry.php") || len(r.commits) != 1 {
		t.Errorf("branch mode: %s · commits %v", names(r), r.commits)
	}
	m = key(m, "esc")
	if m.(Model).rev != nil {
		t.Error("esc should close the changes view")
	}
}
