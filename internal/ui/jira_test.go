package ui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/secuone/nanny/internal/claude"
	"github.com/secuone/nanny/internal/config"
	"github.com/secuone/nanny/internal/gitwt"
	"github.com/secuone/nanny/internal/jira"
)

// fakeJiraAPI stands in for Jira and counts what it is asked.
type fakeJiraAPI struct {
	mu                                   sync.Mutex
	made, searches, myselfs, trans, devs int
	comments                             []string
	assigned                             []string
	transID                              string
	issues                               []jira.Issue
	prs                                  []jira.DevPR
	err                                  error
	user                                 jira.User
}

func (f *fakeJiraAPI) Myself(context.Context) (jira.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.myselfs++
	return f.user, f.err
}

func (f *fakeJiraAPI) Search(context.Context, string, string) ([]jira.Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.searches++
	return f.issues, f.err
}

func (f *fakeJiraAPI) Transitions(context.Context, string) ([]jira.Transition, error) {
	return []jira.Transition{{ID: "31", Name: "Start", To: "In Progress"}, {ID: "41", Name: "Close", To: "Done"}}, f.err
}

func (f *fakeJiraAPI) DoTransition(_ context.Context, _, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.trans++
	f.transID = id
	return f.err
}

func (f *fakeJiraAPI) Comment(_ context.Context, key, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.comments = append(f.comments, key+": "+text)
	return f.err
}

func (f *fakeJiraAPI) Assign(_ context.Context, key, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.assigned = append(f.assigned, key+"="+id)
	return f.err
}

func (f *fakeJiraAPI) AssignableUsers(context.Context, string, string) ([]jira.User, error) {
	return []jira.User{{AccountID: "u2", Name: "Bob"}}, f.err
}

func (f *fakeJiraAPI) SprintField(context.Context) (string, error) { return "customfield_10020", nil }

func (f *fakeJiraAPI) DevPRs(context.Context, string) ([]jira.DevPR, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.devs++
	return f.prs, f.err
}

func (f *fakeJiraAPI) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.searches + f.myselfs + f.trans + f.devs + len(f.comments) + len(f.assigned)
}

func sampleIssues() []jira.Issue {
	now := time.Now()
	return []jira.Issue{
		{Key: "PAY-1", ID: "10001", Summary: "Fix refund flow", Status: "In Progress", StatusCat: "indeterminate", Project: "PAY",
			Sprint: "Sprint 5", SprintState: "active", Updated: now, URL: "https://acme.atlassian.net/browse/PAY-1"},
		{Key: "PAY-2", ID: "10002", Summary: "Add invoice export", Status: "In Progress", StatusCat: "indeterminate", Project: "PAY",
			Sprint: "Sprint 5", SprintState: "active", Updated: now.Add(-time.Hour), URL: "https://acme.atlassian.net/browse/PAY-2"},
		{Key: "PAY-3", ID: "10003", Summary: "Clean up webhooks", Status: "To Do", StatusCat: "new", Project: "PAY",
			Sprint: "Sprint 6", SprintState: "future", Updated: now.Add(-2 * time.Hour), URL: "https://acme.atlassian.net/browse/PAY-3"},
		{Key: "OPS-4", ID: "10004", Summary: "Rotate keys", Status: "To Do", StatusCat: "new", Project: "OPS",
			Updated: now.Add(-3 * time.Hour), URL: "https://acme.atlassian.net/browse/OPS-4"},
		{Key: "PAY-5", ID: "10005", Summary: "Ship dashboards", Status: "Done", StatusCat: "done", Project: "PAY",
			Resolved: now.Add(-48 * time.Hour), Updated: now.Add(-48 * time.Hour), URL: "https://acme.atlassian.net/browse/PAY-5"},
		{Key: "PAY-6", ID: "10006", Summary: "Old thing", Status: "Done", StatusCat: "done", Project: "PAY",
			Resolved: now.Add(-20 * 24 * time.Hour), Updated: now.Add(-20 * 24 * time.Hour)},
	}
}

// jiraBoot is a model with Jira set up against a fake that counts its calls.
func jiraBoot(t *testing.T) (tea.Model, *fakeJiraAPI) {
	t.Helper()
	f := &fakeJiraAPI{issues: sampleIssues(), user: jira.User{AccountID: "me1", Name: "Ada"}}
	old := newJiraClient
	newJiraClient = func(url, email, token string) jira.API {
		f.mu.Lock()
		f.made++
		f.mu.Unlock()
		if token != tokVal {
			t.Errorf("the client got token %q", token)
		}
		return f
	}
	t.Cleanup(func() { newJiraClient = old })
	var m tea.Model = boot(t, 150, 42)
	t.Setenv("NANNY_JIRA_TOKEN", tokVal)
	mm := m.(Model)
	mm.cfg.Jira = config.Jira{URL: "https://acme.atlassian.net", Email: "me@acme.com"}
	return mm, f
}

const tokVal = "tok-s3cret-9"

func plain(m tea.Model) string { return ansi.Strip(m.View()) }

func TestJiraUnconfiguredMakesNoCalls(t *testing.T) {
	f := &fakeJiraAPI{}
	old := newJiraClient
	newJiraClient = func(string, string, string) jira.API { f.made++; return f }
	t.Cleanup(func() { newJiraClient = old })
	m := key(boot(t, 150, 42), "E")
	if !strings.Contains(plain(m), "Connect Jira") {
		t.Errorf("an unconfigured Jira should offer to connect:\n%s", plain(m))
	}
	m, c := m.Update(tickMsg(time.Now()))
	m = drain(m, c, 0)
	m = key(m, "R")
	if f.made != 0 || f.calls() != 0 {
		t.Errorf("no URL: %d clients, %d calls", f.made, f.calls())
	}
	if m = key(m, "enter"); m.(Model).setup == nil || m.(Model).setup.step != setupJira {
		t.Error("⏎ on the empty view should open the wizard at Jira")
	}
}

func TestJiraNoTokenMakesNoRequests(t *testing.T) {
	m, f := jiraBoot(t)
	t.Setenv("NANNY_JIRA_TOKEN", "")
	m = key(m, "E")
	if f.made != 0 || f.calls() != 0 {
		t.Errorf("without a token nothing is asked: %d %d", f.made, f.calls())
	}
	if !strings.Contains(plain(m), "Connect Jira") {
		t.Error("without a token the view asks to connect")
	}
}

func TestJiraOffMakesNoCalls(t *testing.T) {
	m, f := jiraBoot(t)
	mm := m.(Model)
	mm.cfg.SetOff([]string{"view.jira"})
	m = mm
	m.(Model).jira.at = time.Now().Add(-time.Hour)
	m, c := m.Update(tickMsg(time.Now()))
	m = drain(m, c, 0)
	m = key(m, "R")
	m = key(m, "E")
	if f.made != 0 || f.calls() != 0 {
		t.Errorf("view.jira is off: %d clients, %d calls", f.made, f.calls())
	}
}

func TestJiraGroupsByStatusSprintAndDone(t *testing.T) {
	m, _ := jiraBoot(t)
	m = key(m, "E")
	j := m.(Model).jira
	heads := func() []string {
		var hs []string
		for _, r := range j.rows {
			if r.issue == nil {
				hs = append(hs, r.head)
			}
		}
		return hs
	}
	if got := strings.Join(heads(), "|"); got != "In Progress · 2|To Do · 2" {
		t.Errorf("by status: %s", got)
	}
	if j.sel() == nil || j.sel().Key != "PAY-1" {
		t.Errorf("the cursor should sit on the first issue, not a title: %+v", j.sel())
	}
	out := plain(m)
	for _, w := range []string{"By status 4", "By sprint 4", "Done 7d 1", "PAY-1", "Fix refund flow"} {
		if !strings.Contains(out, w) {
			t.Errorf("the view lacks %q:\n%s", w, out)
		}
	}
	m = key(m, "right")
	if got := strings.Join(heads(), "|"); got != "Sprint 5 · 2|Sprint 6 · 1|No sprint · 1" {
		t.Errorf("by sprint: %s", got)
	}
	m = key(m, "right")
	if len(j.rows) != 1 || j.rows[0].issue.Key != "PAY-5" {
		t.Errorf("done in 7 days: %+v", j.rows)
	}
	fits(t, m, 150, 42, "jira done")
}

func TestJiraFilterAndSearch(t *testing.T) {
	m, _ := jiraBoot(t)
	m = key(m, "E")
	j := m.(Model).jira
	m = key(m, "f")
	if n := len(j.list(jiraSecStatus)); n != 2 {
		t.Errorf("f → in progress: %d issues", n)
	}
	m = key(m, "f")
	if n := len(j.list(jiraSecStatus)); n != 2 || j.list(jiraSecStatus)[0].StatusCat != "new" {
		t.Errorf("f again → to do: %d issues", n)
	}
	m = key(m, "f")
	m = key(m, "/")
	if !j.typing() {
		t.Fatal("/ should open the search box")
	}
	m = key(m, "refund")
	if len(j.rows) == 0 || j.sel() == nil || j.sel().Key != "PAY-1" || len(j.list(jiraSecStatus)) != 1 {
		t.Errorf("search should keep PAY-1 only: %+v", j.list(jiraSecStatus))
	}
	m = key(m, "enter")
	if j.typing() {
		t.Error("⏎ should leave the box")
	}
	m = key(m, "/")
	m = key(m, "esc")
	if len(j.list(jiraSecStatus)) != 4 || j.typing() {
		t.Error("esc should clear the search")
	}
}

func TestJiraTransition(t *testing.T) {
	m, f := jiraBoot(t)
	m = key(m, "E")
	before := f.searches
	m = key(m, "t")
	if m.(Model).menu == nil {
		t.Fatal("t should open the transitions menu")
	}
	if out := plain(m); !strings.Contains(out, "Close → Done") {
		t.Errorf("the menu lacks the transition:\n%s", out)
	}
	m = key(m, "2")
	if f.transID != "41" || f.trans != 1 {
		t.Errorf("DoTransition got %q (%d calls)", f.transID, f.trans)
	}
	if f.searches != before+1 {
		t.Errorf("the list should be asked again after a move: %d → %d", before, f.searches)
	}
	if !strings.Contains(m.(Model).status, "PAY-1 moved to Done") {
		t.Errorf("status: %q", m.(Model).status)
	}
}

func TestJiraComment(t *testing.T) {
	m, f := jiraBoot(t)
	m = key(m, "E")
	m = key(m, "c")
	if m.(Model).modal == nil {
		t.Fatal("c should ask for the comment")
	}
	m = key(m, "on it")
	m = key(m, "enter")
	if len(f.comments) != 1 || f.comments[0] != "PAY-1: on it" {
		t.Errorf("comments: %v", f.comments)
	}
}

func TestJiraAssign(t *testing.T) {
	m, f := jiraBoot(t)
	m = key(m, "E")
	m = key(m, "a")
	if m.(Model).menu == nil {
		t.Fatal("a should open the assign menu")
	}
	m = key(m, "1")
	if len(f.assigned) != 1 || f.assigned[0] != "PAY-1=me1" {
		t.Errorf("assigned to me: %v", f.assigned)
	}
	m = key(m, "a")
	key(m, "2")
	if f.assigned[len(f.assigned)-1] != "PAY-1=" {
		t.Errorf("unassign: %v", f.assigned)
	}
}

func TestJiraOpenInBrowser(t *testing.T) {
	m, _ := jiraBoot(t)
	m = key(m, "E")
	m = key(m, "enter")
	if st := m.(Model).status; st != "opened https://acme.atlassian.net/browse/PAY-1" {
		t.Errorf("status: %q", st)
	}
}

func TestJiraStartWork(t *testing.T) {
	m, _ := jiraBoot(t)
	m = key(m, "E")
	m = key(m, "n")
	task := m.(Model).task
	if task == nil {
		t.Fatal("n should open the new-task form")
	}
	if !strings.Contains(task.name.Value(), "PAY-1") || !task.wt {
		t.Errorf("name %q, worktree %v", task.name.Value(), task.wt)
	}
	if got := task.branch(); got != "PAY-1-fix-refund-flow" {
		t.Errorf("branch %q", got)
	}
	if p := task.prompt.Value(); !strings.Contains(p, "PAY-1") || !strings.Contains(p, "https://acme.atlassian.net/browse/PAY-1") {
		t.Errorf("prompt %q", p)
	}
}

func TestJiraBranchTemplate(t *testing.T) {
	m, _ := jiraBoot(t)
	mm := m.(Model)
	mm.cfg.Jira.BranchTemplate = "feature/{key}_{slug}"
	is := sampleIssues()[0]
	if got := mm.jira.branchFor(&mm, &is); got != "feature/PAY-1_fix-refund-flow" {
		t.Errorf("branch %q", got)
	}
}

func TestJiraLinksToPRAndAgent(t *testing.T) {
	m, _ := jiraBoot(t)
	mm := m.(Model)
	mm.prs = prsMsg{"/r/acme": {"PAY-1-fix-refund-flow": gitwt.PR{Number: 12, Title: "Fix refunds", URL: "https://github.com/o/r/pull/12", Checks: "pass"}}}
	mm.sessions = []*claude.Session{
		{ID: "a", Branch: "PAY-1-fix-refund-flow", Title: "refunds", Live: &claude.Proc{}, Modified: time.Now()},
		{ID: "b", Branch: "PAY-10-other", Modified: time.Now()},
	}
	m = key(mm, "E")
	j := m.(Model).jira
	if p, ok := j.prOf(&mm, "PAY-1"); !ok || p.Number != 12 {
		t.Errorf("PR of PAY-1: %+v %v", p, ok)
	}
	if _, ok := j.prOf(&mm, "PAY-10"); ok {
		t.Error("PAY-10 has no PR: PAY-1 is not a prefix match")
	}
	if ag := j.agentOf(&mm, "PAY-1"); ag == nil || ag.ID != "a" {
		t.Errorf("agent of PAY-1: %+v", ag)
	}
	if ag := j.agentOf(&mm, "PAY-2"); ag != nil {
		t.Errorf("PAY-2 has no agent: %+v", ag)
	}
	out := plain(m)
	if !strings.Contains(out, "#12") || !strings.Contains(out, "●") {
		t.Errorf("the row should show the PR and the live agent:\n%s", out)
	}
}

func TestJiraDevStatusFallbackAndSilentFailure(t *testing.T) {
	m, f := jiraBoot(t)
	m = key(m, "E")
	mm := m.(Model)
	j := mm.jira
	f.prs = []jira.DevPR{{Name: "Fix refunds", URL: "https://github.com/o/r/pull/9", Branch: "x"}}
	cmd := j.devFetch(&mm, "PAY-1")
	m = drain(mm, cmd, 0)
	if p, ok := j.prOf(&mm, "PAY-1"); !ok || p.URL != "https://github.com/o/r/pull/9" {
		t.Errorf("dev-status PR: %+v %v", p, ok)
	}
	f.err = errors.New("404")
	mm = m.(Model)
	j.devAsked = map[string]bool{}
	m = drain(mm, j.devFetch(&mm, "PAY-1"), 0)
	if !j.devOff || m.(Model).statusErr {
		t.Errorf("a failing dev-status should switch itself off without a word: off=%v status=%q", j.devOff, m.(Model).status)
	}
}

func TestJiraRefreshRhythm(t *testing.T) {
	m, f := jiraBoot(t)
	m = key(m, "E")
	j := m.(Model).jira
	tick := func() { // what the tick asks of the features
		mm := m.(Model)
		m = drain(mm, mm.featDue(false), 0)
	}
	if f.searches != 1 {
		t.Fatalf("opening the view asks once: %d", f.searches)
	}
	tick()
	if f.searches != 1 {
		t.Errorf("a fresh list isn't asked again: %d", f.searches)
	}
	j.at = time.Now().Add(-3 * time.Minute)
	tick()
	if f.searches != 2 {
		t.Errorf("on screen it refreshes after refresh_mins: %d", f.searches)
	}
	m = key(m, "1") // away from the view: the sidebar line alone, every 10 minutes
	j.at = time.Now().Add(-3 * time.Minute)
	tick()
	if f.searches != 2 {
		t.Errorf("off screen it waits: %d", f.searches)
	}
	j.at = time.Now().Add(-11 * time.Minute)
	tick()
	if f.searches != 3 {
		t.Errorf("off screen it asks after 10 minutes: %d", f.searches)
	}
	mm := m.(Model)
	mm.cfg.SetOff([]string{"sidebar.jira"})
	m = mm
	j.at = time.Now().Add(-time.Hour)
	tick()
	if f.searches != 3 {
		t.Errorf("without the sidebar line nothing is fetched off screen: %d", f.searches)
	}
}

func TestJiraUnauthorizedStopsAndSaysSo(t *testing.T) {
	m, f := jiraBoot(t)
	f.err = jira.ErrUnauthorized
	m = key(m, "E")
	if out := plain(m); !strings.Contains(out, "token rejected") {
		t.Errorf("a 401 should say so:\n%s", out)
	}
	j := m.(Model).jira
	j.at = time.Now().Add(-time.Hour)
	m, c := m.Update(tickMsg(time.Now()))
	drain(m, c, 0)
	if f.searches != 1 {
		t.Errorf("a rejected token isn't tried again by itself: %d", f.searches)
	}
}

func TestJiraRateLimitWaits(t *testing.T) {
	m, f := jiraBoot(t)
	f.err = &jira.RateLimitError{After: time.Minute}
	m = key(m, "E")
	j := m.(Model).jira
	if j.retryAt.Before(time.Now().Add(50 * time.Second)) {
		t.Errorf("retry at %v", j.retryAt)
	}
	f.err = nil
	j.at = time.Now().Add(-time.Hour)
	m = key(m, "r")
	m = key(m, "R")
	if f.searches != 1 {
		t.Errorf("Retry-After should be respected: %d searches", f.searches)
	}
}

func TestJiraSidebarLineAndHooks(t *testing.T) {
	m, _ := jiraBoot(t)
	m = key(m, "E")
	mm := m.(Model)
	text, url := mm.jiraSide("PAY-1-fix-refund-flow", 30)
	if s := ansi.Strip(text); s != "PAY-1 · In Progress" || url != "https://acme.atlassian.net/browse/PAY-1" {
		t.Errorf("side line %q %q", s, url)
	}
	if text, _ := mm.jiraSide("OPS-99-new", 30); ansi.Strip(text) != "OPS-99" {
		t.Errorf("an issue not in the cache still shows its key: %q", ansi.Strip(text))
	}
	if text, _ := mm.jiraSide("main", 30); text != "" {
		t.Errorf("no key, no line: %q", text)
	}
	mm.cfg.SetOff([]string{"sidebar.jira"})
	if text, _ := mm.jiraSide("PAY-1-x", 30); text != "" {
		t.Error("sidebar.jira off hides the line")
	}
	if jiraBrowseURL == nil || jiraBrowseURL(&mm, "PAY-7") != "https://acme.atlassian.net/browse/PAY-7" {
		t.Error("jiraBrowseURL should be set")
	}
	n, ok := jiraClosedSince(&mm, time.Now().Add(-7*24*time.Hour))
	if !ok || n != 1 {
		t.Errorf("closed this week: %d %v", n, ok)
	}
}

func TestJiraViewsFitInEveryLanguage(t *testing.T) {
	for _, lang := range []string{"en", "es", "fr", "de"} {
		f := &fakeJiraAPI{issues: sampleIssues()}
		old := newJiraClient
		newJiraClient = func(string, string, string) jira.API { return f }
		m := bootIn(t, lang, 100, 30)
		t.Setenv("NANNY_JIRA_TOKEN", tokVal)
		mm := m.(Model)
		mm.cfg.Jira = config.Jira{URL: "https://acme.atlassian.net", Email: "me@acme.com"}
		m = key(mm, "E")
		for _, k := range []string{"right", "right", "left", "left", "/", "esc", "t", "esc"} {
			fits(t, m, 100, 30, "jira "+lang+" "+k)
			m = key(m, k)
		}
		newJiraClient = old
	}
}

// ---------------------------------------------------------------- wizard

func TestSetupOpensAfterWelcomeAndCanBeSkippedForGood(t *testing.T) {
	t.Setenv("NANNY_NO_WELCOME", "")
	m := boot(t, 100, 30).(Model)
	m.closeWelcome()
	if m.setup == nil || m.setup.step != setupIntro {
		t.Fatal("the wizard should open as the welcome closes")
	}
	fits(t, m, 100, 30, "setup intro")
	mm := key(m, "s").(Model)
	if mm.setup != nil {
		t.Fatal("s skips everything")
	}
	if _, err := os.Stat(filepath.Join(config.StateDir(), "setup-done")); err != nil {
		t.Errorf("skipping should be remembered: %v", err)
	}
	mm.closeWelcome()
	if mm.setup != nil {
		t.Error("once done it doesn't come back by itself")
	}
}

func TestSetupEscMarksDoneAtEveryStep(t *testing.T) {
	for step := setupIntro; step <= setupSummary; step++ {
		m := boot(t, 100, 30).(Model)
		m.openSetup(step)
		fits(t, m, 100, 30, "setup step")
		m = key(m, "esc").(Model)
		if m.setup != nil || !setupDone() {
			t.Errorf("step %d: esc should close and remember", step)
		}
	}
}

func TestSetupOpensFromSettingsMenuAndPalette(t *testing.T) {
	m := key(boot(t, 150, 42), "8").(Model)
	for i, s := range settingsList {
		if s.name == "Jira" {
			m.settings.cur.idx = i
		}
	}
	m2 := key(m, "enter").(Model)
	if m2.setup == nil || m2.setup.step != setupJira {
		t.Error("Settings → Connections → Jira should open the wizard at Jira")
	}
	var found bool
	for _, e := range m.paletteEntries() {
		found = found || e.label == "Setup wizard…"
	}
	if !found {
		t.Error("the palette should offer the wizard")
	}
}

func TestSetupConnectsAndKeepsTheTokenOutOfTheConfig(t *testing.T) {
	m, f := jiraBoot(t)
	mm := m.(Model)
	mm.cfg.Jira = config.Jira{}
	t.Setenv("NANNY_JIRA_TOKEN", "")
	mm.openSetup(setupJira)
	w := mm.setup
	w.url.SetValue("acme.atlassian.net")
	w.email.SetValue("me@acme.com")
	w.token.SetValue(tokVal)
	w.field = 2
	t.Setenv("NANNY_JIRA_TOKEN", "")
	m = key(mm, "enter")
	w = m.(Model).setup
	if !w.connected || !strings.Contains(w.note, "Ada") {
		t.Fatalf("not connected: %q err=%v", w.note, w.noteErr)
	}
	fits(t, m, 150, 42, "setup connected")
	p := filepath.Join(config.Dir(), "secrets", "jira-acme.atlassian.net")
	st, err := os.Stat(p)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("token file: %v %v", st, err)
	}
	if b, _ := os.ReadFile(p); strings.TrimSpace(string(b)) != tokVal {
		t.Errorf("token file holds %q", b)
	}
	cfg, _ := os.ReadFile(config.Path())
	if strings.Contains(string(cfg), tokVal) || !strings.Contains(string(cfg), "https://acme.atlassian.net") {
		t.Errorf("config.toml should name the site and never the token:\n%s", cfg)
	}
	if j := m.(Model).cfg.Jira; j.URL != "https://acme.atlassian.net" || j.Email != "me@acme.com" {
		t.Errorf("config: %+v", j)
	}
	if strings.Contains(plain(m), tokVal) || strings.Contains(m.(Model).status, tokVal) {
		t.Error("the token is never shown")
	}
	if f.myselfs != 1 {
		t.Errorf("connecting asks who you are once: %d", f.myselfs)
	}
}

func TestSetupRejectedTokenSaysSo(t *testing.T) {
	m, f := jiraBoot(t)
	f.err = jira.ErrUnauthorized
	mm := m.(Model)
	mm.openSetup(setupJira)
	mm.setup.url.SetValue("https://acme.atlassian.net")
	mm.setup.email.SetValue("me@acme.com")
	mm.setup.token.SetValue(tokVal)
	mm.setup.field = 2
	m = key(mm, "enter")
	w := m.(Model).setup
	if w.connected || !w.noteErr || !strings.Contains(w.note, "token rejected") {
		t.Errorf("note %q err=%v connected=%v", w.note, w.noteErr, w.connected)
	}
}
