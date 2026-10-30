package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/secuone/nanny/internal/jira"
)

func readyDetail() jira.Detail {
	return jira.Detail{
		Description:  "Refunds fail when the card was\nissued abroad.\n\n• check the PSP\n• add a test",
		CommentTotal: 2,
		Comments: []jira.Comment{
			{Author: "Grace", Created: time.Now().Add(-2 * time.Hour), Body: "I can reproduce it"},
			{Author: "Linus", Created: time.Now().Add(-time.Hour), Body: "Fix is in review"},
		},
		Subtasks: []jira.Link{{Rel: "subtask", Key: "PAY-9", Summary: "Add logging", Status: "To Do"}},
		Links:    []jira.Link{{Rel: "blocks", Key: "PAY-7", Summary: "Launch", Status: "In Progress"}},
	}
}

func longDetail() jira.Detail {
	d := readyDetail()
	var ls []string
	for i := 0; i < 80; i++ {
		ls = append(ls, fmt.Sprintf("line %d of a long description that goes on", i))
	}
	d.Description = strings.Join(ls, "\n")
	return d
}

// jiraReady is a model on the Jira view whose first issue has been read.
func jiraReady(t *testing.T) (tea.Model, *fakeJiraAPI) {
	t.Helper()
	m, f := jiraBoot(t)
	f.details = map[string]jira.Detail{"PAY-1": readyDetail()}
	return key(m, "E"), f
}

func TestJiraDetailShowsDescriptionCommentsSubtasksAndLinks(t *testing.T) {
	m, f := jiraReady(t)
	if len(f.detailCalls) != 1 || f.detailCalls[0] != "PAY-1" {
		t.Fatalf("opening the view should read the selected issue once: %v", f.detailCalls)
	}
	out := plain(m)
	for _, w := range []string{"Description", "Refunds fail when the card was", "• check the PSP", "2 comments", "Grace", "I can reproduce it",
		"Linus", "Fix is in review", "Subtasks", "PAY-9", "Linked issues", "blocks", "PAY-7", "💬2"} {
		if !strings.Contains(out, w) {
			t.Errorf("the detail lacks %q:\n%s", w, out)
		}
	}
	fits(t, m, 150, 42, "jira reader")
}

func TestJiraDetailSaysLoadingThenNoDescription(t *testing.T) {
	m, _ := jiraBoot(t)
	m = key(m, "E")
	j := m.(Model).jira
	j.read, j.readAsk = map[string]jiraRead{}, map[string]bool{} // as if the answer hasn't come
	if out := plain(m); !strings.Contains(out, "reading the issue") {
		t.Errorf("it should say it is reading:\n%s", out)
	}
	m, f := jiraBoot(t)
	m = key(m, "E")
	if out := plain(m); !strings.Contains(out, "No description.") || !strings.Contains(out, "No comments.") || len(f.detailCalls) != 1 {
		t.Errorf("an empty issue says so:\n%s", out)
	}
}

func TestJiraDetailErrorShowsWithoutTheToken(t *testing.T) {
	m, f := jiraBoot(t)
	f.err = errors.New("jira: 500 Server Error: boom")
	m = key(m, "E")
	out := plain(m)
	if !strings.Contains(out, "boom") || strings.Contains(out, tokVal) {
		t.Errorf("the failure should show, the token never:\n%s", out)
	}
}

func TestJiraDetailIsDebouncedAndCached(t *testing.T) {
	m, f := jiraReady(t)
	f.details["PAY-2"], f.details["PAY-3"] = readyDetail(), readyDetail()
	f.detailCalls = nil
	for i := 0; i < 2; i++ {
		m, _ = m.Update(keyPress("j")) // moving: the ticks are never delivered
	}
	j := m.(Model).jira
	if j.sel().Key != "PAY-3" || len(f.detailCalls) != 0 {
		t.Fatalf("moving the cursor must not read: sel=%s calls=%v", j.sel().Key, f.detailCalls)
	}
	mm := m.(Model)
	m = drain(m, func() tea.Msg { return jiraDetWantMsg{"PAY-2"} }, 0) // the tick of a place it left
	if len(f.detailCalls) != 0 {
		t.Errorf("a stale tick must read nothing: %v", f.detailCalls)
	}
	m = drain(m, func() tea.Msg { return jiraDetWantMsg{"PAY-3"} }, 0)
	m = drain(m, func() tea.Msg { return jiraDetWantMsg{"PAY-3"} }, 0)
	if len(f.detailCalls) != 1 || f.detailCalls[0] != "PAY-3" {
		t.Errorf("the settled issue is read once: %v", f.detailCalls)
	}
	mm = m.(Model)
	if cmd := j.settleRead(&mm); cmd != nil {
		t.Error("a kept version is not asked again")
	}
	for i := range j.issues {
		if j.issues[i].Key == "PAY-3" {
			j.issues[i].Updated = time.Now().Add(time.Minute)
		}
	}
	j.rebuild(&mm)
	if cmd := j.settleRead(&mm); cmd == nil {
		t.Error("an issue updated in Jira is read again")
	}
}

func TestJiraDetailWaitsAboutAQuarterOfASecond(t *testing.T) {
	if jiraDetSettle < 200*time.Millisecond || jiraDetSettle > 300*time.Millisecond {
		t.Errorf("debounce is %s", jiraDetSettle)
	}
}

func TestJiraDetailScrolls(t *testing.T) {
	m, f := jiraBoot(t)
	f.details = map[string]jira.Detail{"PAY-1": longDetail()}
	m = key(m, "E")
	if strings.Contains(plain(m), "line 70 of") {
		t.Fatal("the end of a long description is below the fold")
	}
	m = key(m, "l")
	for i := 0; i < 70; i++ {
		m = key(m, "j")
	}
	if m.(Model).detailOff == 0 || !strings.Contains(plain(m), "line 70 of") {
		t.Errorf("l then j should scroll the reader (off=%d)", m.(Model).detailOff)
	}
}

func TestJiraStatusFilterWalksCategoriesThenStatuses(t *testing.T) {
	m, _ := jiraBoot(t)
	m = key(m, "E")
	j := m.(Model).jira
	count := func() string {
		var ks []string
		for _, is := range j.list(jiraSecStatus) {
			ks = append(ks, is.Key)
		}
		return strings.Join(ks, ",")
	}
	want := []struct{ name, keys string }{
		{"to do", "PAY-3,OPS-4"},
		{"in progress", "PAY-1,PAY-2"},
		{"done", "PAY-5,PAY-6"},
		{"In Progress", "PAY-1,PAY-2"},
		{"To Do", "PAY-3,OPS-4"},
		{"Done", "PAY-5,PAY-6"},
	}
	for _, w := range want {
		m = key(m, "f")
		if j.filterName() != w.name || count() != w.keys {
			t.Errorf("filter %q: shows %q (want %q)", j.filterName(), count(), w.keys)
		}
	}
	m = key(m, "f")
	if j.filter != "" || len(j.list(jiraSecStatus)) != 4 {
		t.Errorf("after the last status comes all again: %q", j.filter)
	}
	m = key(m, "f")
	m = key(m, "f")
	if out := plain(m); !strings.Contains(out, "in progress ✕") {
		t.Errorf("the active filter should be a chip:\n%s", out)
	}
	m = key(m, "F")
	if j.filter != "" || !strings.Contains(plain(m), "⚑ all") {
		t.Error("F clears the filter and the chip says all")
	}
}

func TestJiraFilterChipIsClickable(t *testing.T) {
	m, _ := jiraBoot(t)
	m = key(m, "E")
	viewOf(m)
	mm := m.(Model)
	var hit *keyHit
	for i := range mm.vs.keyHits {
		if mm.vs.keyHits[i].key == "f" {
			hit = &mm.vs.keyHits[i]
		}
	}
	if hit == nil {
		t.Fatal("the section bar should have a clickable filter chip")
	}
	m, _, _ = mm.keyClick(hit.x0, hit.y)
	if m.(Model).jira.filter != filterNew {
		t.Errorf("a click should press f: %q", m.(Model).jira.filter)
	}
}

func TestJiraFilterAppliesToSearchResults(t *testing.T) {
	m, f := jiraBoot(t)
	f.found = []jira.Issue{
		{Key: "ABC-1", ID: "1", Summary: "Other one", Status: "To Do", StatusCat: "new", Updated: time.Now()},
		{Key: "ABC-2", ID: "2", Summary: "Other two", Status: "Done", StatusCat: "done", Updated: time.Now()},
	}
	m = key(m, "E")
	m = key(m, "e")
	m = key(m, "other")
	m = key(m, "enter")
	j := m.(Model).jira
	if len(j.list(jiraSecSearch)) != 2 {
		t.Fatalf("results: %+v", j.list(jiraSecSearch))
	}
	m = key(m, "f") // to do
	if l := j.list(jiraSecSearch); len(l) != 1 || l[0].Key != "ABC-1" {
		t.Errorf("the filter works on the results: %+v", l)
	}
}

func TestJiraSearchAllOfJiraByText(t *testing.T) {
	m, f := jiraBoot(t)
	f.found = []jira.Issue{{Key: "ABC-1", ID: "1", Summary: `He said "no" \ yes`, Status: "To Do", StatusCat: "new", Updated: time.Now(), URL: "https://acme.atlassian.net/browse/ABC-1"}}
	m = key(m, "E")
	m = key(m, "e")
	if m.(Model).modal == nil {
		t.Fatal("e should ask what to search for")
	}
	m = key(m, `say "hi" \ now`)
	m = key(m, "enter")
	want := `text ~ "say \"hi\" \\ now" ORDER BY updated DESC`
	if got := f.jqls[len(f.jqls)-1]; got != want {
		t.Errorf("jql %q, want %q", got, want)
	}
	j := m.(Model).jira
	if j.section != jiraSecSearch || j.sel() == nil || j.sel().Key != "ABC-1" {
		t.Fatalf("results should show in the Search section: sec=%d sel=%v", j.section, j.sel())
	}
	out := plain(m)
	for _, w := range []string{"Search 1", "ABC-1", `⌕ say "hi"`} {
		if !strings.Contains(out, w) {
			t.Errorf("the view lacks %q:\n%s", w, out)
		}
	}
	m = key(m, "F")
	if j.remote != nil || j.section != jiraSecStatus || strings.Contains(plain(m), "Search 1") {
		t.Error("F is the way back from the search")
	}
	if len(j.list(jiraSecStatus)) != 4 {
		t.Error("the list of your issues is back")
	}
}

func TestJiraSearchWithJQLPrefixRunsItAsTyped(t *testing.T) {
	m, f := jiraBoot(t)
	f.found = []jira.Issue{{Key: "OPS-9", ID: "9", Summary: "Found by jql", Status: "To Do", StatusCat: "new", Updated: time.Now()}}
	m = key(m, "E")
	m = key(m, "/")
	m = key(m, `jql: project = OPS AND text ~ "a b"`)
	m = key(m, "enter")
	if got := f.jqls[len(f.jqls)-1]; got != `project = OPS AND text ~ "a b"` {
		t.Errorf("jql %q", got)
	}
	j := m.(Model).jira
	if j.section != jiraSecSearch || j.search.Value() != "" || len(j.list(jiraSecSearch)) != 1 {
		t.Errorf("sec=%d box=%q results=%d", j.section, j.search.Value(), len(j.list(jiraSecSearch)))
	}
}

func TestJiraTypingJQLDoesNotHideTheList(t *testing.T) {
	m, _ := jiraBoot(t)
	m = key(m, "E")
	j := m.(Model).jira
	m = key(m, "/")
	m = key(m, "jql: pro")
	if len(j.list(jiraSecStatus)) != 4 {
		t.Error("while typing a jql: query the list stays as it is")
	}
}

func TestJiraLocalSearchFindsTheAssignee(t *testing.T) {
	m, _ := jiraBoot(t)
	m = key(m, "E")
	j := m.(Model).jira
	for i := range j.issues {
		if j.issues[i].Key == "OPS-4" {
			j.issues[i].Assignee = "Ada Lovelace"
		}
	}
	m = key(m, "/")
	m = key(m, "lovelace")
	if l := j.list(jiraSecStatus); len(l) != 1 || l[0].Key != "OPS-4" {
		t.Errorf("search by assignee: %+v", l)
	}
}

func TestJiraSearchErrorKeepsTheListAndHidesTheToken(t *testing.T) {
	m, f := jiraBoot(t)
	m = key(m, "E")
	f.err = errors.New("jira: 400 Bad Request: the JQL is wrong")
	m = key(m, "e")
	m = key(m, "jql: nonsense")
	m = key(m, "enter")
	out := plain(m)
	if !strings.Contains(out, "the JQL is wrong") || strings.Contains(out, tokVal) || m.(Model).jira.remote != nil {
		t.Errorf("a failed search says why and changes nothing:\n%s", out)
	}
}

func TestJiraSortByPriorityAndStatus(t *testing.T) {
	m, _ := jiraBoot(t)
	m = key(m, "E")
	j := m.(Model).jira
	for i := range j.issues {
		switch j.issues[i].Key {
		case "PAY-1":
			j.issues[i].Priority = "Low"
		case "PAY-2":
			j.issues[i].Priority = "Highest"
		}
	}
	m = key(m, "o")
	if l := j.list(jiraSecStatus); j.sortName() != "priority" || l[0].Key != "PAY-2" {
		t.Errorf("by priority: %s first (%s)", l[0].Key, j.sortName())
	}
	m = key(m, "o")
	if l := j.list(jiraSecStatus); j.sortName() != "status" || l[0].StatusCat != "indeterminate" {
		t.Errorf("by status: %+v", l[0])
	}
	m = key(m, "o")
	if l := j.list(jiraSecStatus); j.sortName() != "updated" || l[0].Key != "PAY-1" {
		t.Errorf("back to updated: %s", l[0].Key)
	}
	_ = m
}

func TestJiraFooterAndHelpListTheNewKeys(t *testing.T) {
	m, _ := jiraBoot(t)
	m = key(m, "E")
	j := m.(Model).jira
	mm := m.(Model)
	has := func(hs [][2]string, k string) bool {
		for _, h := range hs {
			if h[0] == k {
				return true
			}
		}
		return false
	}
	for _, k := range []string{"/", "e", "f", "o", "t", "c", "a", "n"} {
		if !has(j.footer(&mm), k) {
			t.Errorf("the footer lacks %q", k)
		}
	}
	if has(j.footer(&mm), "F") {
		t.Error("F is only shown when there is something to clear")
	}
	j.filter = filterNew
	if !has(j.footer(&mm), "F") {
		t.Error("F should show with a filter on")
	}
	var help strings.Builder
	for _, sec := range j.help() {
		for _, r := range sec.rows {
			help.WriteString(r[0] + " " + r[1] + "\n")
		}
	}
	for _, w := range []string{"f  o  F", "/  e", "t  c  a"} {
		if !strings.Contains(help.String(), w) {
			t.Errorf("help lacks %q", w)
		}
	}
}

func TestJiraExistingActionsStillWorkAfterReading(t *testing.T) {
	m, f := jiraReady(t)
	m = key(m, "c")
	m = key(m, "thanks")
	m = key(m, "enter")
	if len(f.comments) != 1 || f.comments[0] != "PAY-1: thanks" {
		t.Errorf("comments: %v", f.comments)
	}
	m = key(m, "t")
	if m.(Model).menu == nil {
		t.Error("t should still open transitions")
	}
}
