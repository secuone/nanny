package jira

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func serve(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return New(s.URL+"/", "me@acme.com", "s3cret")
}

func body(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	b, _ := io.ReadAll(r.Body)
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("body %q: %v", b, err)
	}
	return m
}

func TestAuthHeaderAndMyself(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "me@acme.com" || p != "s3cret" || r.URL.Path != "/rest/api/3/myself" {
			t.Errorf("auth %v %q %q path %s", ok, u, p, r.URL.Path)
		}
		fmt.Fprint(w, `{"accountId":"abc","displayName":"Ada","emailAddress":"me@acme.com"}`)
	})
	u, err := c.Myself(context.Background())
	if err != nil || u.AccountID != "abc" || u.Name != "Ada" {
		t.Fatalf("%+v %v", u, err)
	}
}

func TestSearchPagesWithNextPageToken(t *testing.T) {
	var calls []map[string]any
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/search/jql" || r.Method != "POST" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		b := body(t, r)
		calls = append(calls, b)
		if b["nextPageToken"] == nil {
			fmt.Fprint(w, `{"issues":[{"id":"1","key":"PAY-1","fields":{"summary":"One","status":{"name":"In Progress","statusCategory":{"key":"indeterminate"}},
				"issuetype":{"name":"Bug"},"priority":{"name":"High"},"project":{"key":"PAY"},"labels":["a"],"updated":"2026-10-30T10:44:31.000+0100",
				"customfield_10020":[{"name":"S1","state":"closed"},{"name":"S2","state":"active"}]}}],"nextPageToken":"tok2","isLast":false}`)
			return
		}
		fmt.Fprint(w, `{"issues":[{"id":"2","key":"PAY-2","fields":{"summary":"Two","status":{"name":"Done","statusCategory":{"key":"done"}},"resolutiondate":"2026-10-29T09:00:00.000+0000"}}],"isLast":true}`)
	})
	is, err := c.Search(context.Background(), "assignee = currentUser()", "customfield_10020")
	if err != nil || len(is) != 2 {
		t.Fatalf("%v %v", is, err)
	}
	if len(calls) != 2 || calls[1]["nextPageToken"] != "tok2" || calls[0]["jql"] != "assignee = currentUser()" || calls[0]["maxResults"] != float64(100) {
		t.Errorf("calls: %v", calls)
	}
	a := is[0]
	if a.Key != "PAY-1" || a.StatusCat != "indeterminate" || a.Sprint != "S2" || a.SprintState != "active" || a.Type != "Bug" ||
		a.Priority != "High" || a.Project != "PAY" || a.URL != c.base+"/browse/PAY-1" || a.Updated.IsZero() || a.ID != "1" {
		t.Errorf("issue: %+v", a)
	}
	if is[1].Resolved.IsZero() || is[1].StatusCat != "done" {
		t.Errorf("done issue: %+v", is[1])
	}
}

func TestSearchStopsAtThreeHundred(t *testing.T) {
	n := 0
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		n++
		var is []string
		for i := range 100 {
			is = append(is, fmt.Sprintf(`{"id":"%d","key":"K-%d","fields":{}}`, n*1000+i, n*1000+i))
		}
		fmt.Fprintf(w, `{"issues":[%s],"nextPageToken":"more","isLast":false}`, strings.Join(is, ","))
	})
	got, err := c.Search(context.Background(), "x", "")
	if err != nil || len(got) != 300 || n != 3 {
		t.Errorf("%d issues, %d calls, %v", len(got), n, err)
	}
}

func TestCommentIsADF(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/issue/PAY-1/comment" || r.Method != "POST" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		doc := body(t, r)["body"].(map[string]any)
		if doc["type"] != "doc" || doc["version"] != float64(1) {
			t.Errorf("doc: %v", doc)
		}
		ps := doc["content"].([]any)
		if len(ps) != 2 {
			t.Fatalf("paragraphs: %v", ps)
		}
		p := ps[0].(map[string]any)
		txt := p["content"].([]any)[0].(map[string]any)
		if p["type"] != "paragraph" || txt["type"] != "text" || txt["text"] != "hello" {
			t.Errorf("paragraph: %v", p)
		}
		w.WriteHeader(201)
	})
	if err := c.Comment(context.Background(), "PAY-1", "hello\n\nworld"); err != nil {
		t.Fatal(err)
	}
}

func TestTransitions(t *testing.T) {
	var posted map[string]any
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/issue/PAY-1/transitions" {
			t.Errorf("path %s", r.URL.Path)
		}
		if r.Method == "GET" {
			fmt.Fprint(w, `{"transitions":[{"id":"31","name":"Start","to":{"name":"In Progress"}},{"id":"41","name":"Close","to":{"name":"Done"}}]}`)
			return
		}
		posted = body(t, r)
		w.WriteHeader(204)
	})
	ts, err := c.Transitions(context.Background(), "PAY-1")
	if err != nil || len(ts) != 2 || ts[0].ID != "31" || ts[1].To != "Done" {
		t.Fatalf("%+v %v", ts, err)
	}
	if err := c.DoTransition(context.Background(), "PAY-1", "41"); err != nil {
		t.Fatal(err)
	}
	if tr, _ := posted["transition"].(map[string]any); tr["id"] != "41" {
		t.Errorf("posted %v", posted)
	}
}

func TestAssign(t *testing.T) {
	var got []map[string]any
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" || r.URL.Path != "/rest/api/3/issue/PAY-1/assignee" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		got = append(got, body(t, r))
		w.WriteHeader(204)
	})
	_ = c.Assign(context.Background(), "PAY-1", "abc")
	_ = c.Assign(context.Background(), "PAY-1", "")
	if got[0]["accountId"] != "abc" {
		t.Errorf("%v", got[0])
	}
	if v, ok := got[1]["accountId"]; !ok || v != nil {
		t.Errorf("unassign should send null: %v", got[1])
	}
}

func TestAssignableUsers(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("issueKey") != "PAY-1" || r.URL.Query().Get("query") != "ada l" {
			t.Errorf("query %v", r.URL.Query())
		}
		fmt.Fprint(w, `[{"accountId":"a1","displayName":"Ada L"}]`)
	})
	us, err := c.AssignableUsers(context.Background(), "PAY-1", "ada l")
	if err != nil || len(us) != 1 || us[0].AccountID != "a1" {
		t.Errorf("%+v %v", us, err)
	}
}

func TestUnauthorized(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) })
	if _, err := c.Myself(context.Background()); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("err = %v", err)
	}
}

func TestRateLimitedHonoursRetryAfter(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "42")
		w.WriteHeader(429)
	})
	_, err := c.Search(context.Background(), "x", "")
	var rl *RateLimitError
	if !errors.As(err, &rl) || rl.After != 42*time.Second {
		t.Errorf("err = %v", err)
	}
}

func TestErrorsNeverCarryTheToken(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		fmt.Fprint(w, `{"errorMessages":["bad jql"]}`)
	})
	_, err := c.Search(context.Background(), "x", "")
	if err == nil || !strings.Contains(err.Error(), "bad jql") || strings.Contains(err.Error(), "s3cret") {
		t.Errorf("err = %v", err)
	}
}

func TestSprintFieldDiscovery(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"id":"summary","schema":{"type":"string"}},{"id":"customfield_10020","name":"Sprint","custom":true,
			"schema":{"custom":"com.pyxis.greenhopper.jira:gl-sprint"}},{"id":"customfield_1","schema":{"custom":"other"}}]`)
	})
	if f, err := c.SprintField(context.Background()); err != nil || f != "customfield_10020" {
		t.Errorf("%q %v", f, err)
	}
	c = serve(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `[]`) })
	if f, _ := c.SprintField(context.Background()); f != "" {
		t.Errorf("none expected, got %q", f)
	}
}

func TestDevPRs(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("issueId") != "10001" || q.Get("applicationType") != "GitHub" || q.Get("dataType") != "pullrequest" {
			t.Errorf("query %v", q)
		}
		fmt.Fprint(w, `{"detail":[{"pullRequests":[{"name":"Fix it","url":"https://github.com/o/r/pull/7","status":"OPEN","source":{"branch":"PAY-1-fix"}}]}]}`)
	})
	prs, err := c.DevPRs(context.Background(), "10001")
	if err != nil || len(prs) != 1 || prs[0].URL != "https://github.com/o/r/pull/7" || prs[0].Branch != "PAY-1-fix" {
		t.Errorf("%+v %v", prs, err)
	}
}

func TestMatches(t *testing.T) {
	is := Issue{Key: "PAY-1", Summary: "Fix refund", Status: "To Do", Labels: []string{"backend"}}
	if !is.Matches("refund pay") || !is.Matches("BACKEND") || is.Matches("nope") {
		t.Error("Matches is wrong")
	}
}

// The token never goes over plain http to another machine.
func TestPlainHTTPRefused(t *testing.T) {
	c := New("http://jira.example.com", "me@x", "secret-token")
	_, err := c.Myself(context.Background())
	if err == nil || strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("plain http should be refused without the token: %v", err)
	}
}

func TestDetailReadsDescriptionCommentsAndLinks(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/issue/PAY-1" || !strings.Contains(r.URL.Query().Get("fields"), "comment") {
			t.Errorf("%s ?%s", r.URL.Path, r.URL.RawQuery)
		}
		fmt.Fprint(w, `{"fields":{"description":{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Refunds fail"}]}]},
		 "comment":{"total":2,"comments":[
		  {"author":{"displayName":"Ada"},"created":"2026-10-29T09:00:00.000+0000","body":{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"looking"}]}]}},
		  {"author":{"displayName":"Bob"},"created":"2026-10-30T09:00:00.000+0000","body":{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"fixed"}]}]}}]},
		 "subtasks":[{"key":"PAY-9","fields":{"summary":"Sub","status":{"name":"To Do"}}}],
		 "issuelinks":[{"type":{"inward":"is blocked by","outward":"blocks"},"outwardIssue":{"key":"PAY-7","fields":{"summary":"Later","status":{"name":"Done"}}}},
		  {"type":{"inward":"is blocked by","outward":"blocks"},"inwardIssue":{"key":"PAY-8","fields":{"summary":"First","status":{"name":"To Do"}}}}]}}`)
	})
	d, err := c.Detail(context.Background(), "PAY-1")
	if err != nil || d.Description != "Refunds fail" || d.CommentTotal != 2 || len(d.Comments) != 2 {
		t.Fatalf("%+v %v", d, err)
	}
	if d.Comments[1].Author != "Bob" || d.Comments[1].Body != "fixed" || d.Comments[1].Created.IsZero() {
		t.Errorf("comment: %+v", d.Comments[1])
	}
	if len(d.Subtasks) != 1 || d.Subtasks[0].Key != "PAY-9" || len(d.Links) != 2 || d.Links[0].Rel != "blocks" || d.Links[1].Rel != "is blocked by" {
		t.Errorf("links: %+v %+v", d.Subtasks, d.Links)
	}
}

func TestDetailErrorsNeverCarryTheToken(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) { http.Error(w, `{"errorMessages":["nope"]}`, 404) })
	_, err := c.Detail(context.Background(), "PAY-1")
	if err == nil || strings.Contains(err.Error(), "s3cret") {
		t.Errorf("err: %v", err)
	}
}

func TestSearchBringsAssigneeAndCreated(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		b := body(t, r)
		if f, _ := json.Marshal(b["fields"]); !strings.Contains(string(f), "assignee") {
			t.Errorf("fields: %s", f)
		}
		fmt.Fprint(w, `{"issues":[{"id":"1","key":"PAY-1","fields":{"summary":"x","assignee":{"displayName":"Ada"},"reporter":{"displayName":"Bob"},"created":"2026-10-01T10:00:00.000+0000"}}],"isLast":true}`)
	})
	is, err := c.Search(context.Background(), "x", "")
	if err != nil || len(is) != 1 || is[0].Assignee != "Ada" || is[0].Reporter != "Bob" || is[0].Created.IsZero() || !is[0].Matches("ada") {
		t.Fatalf("%+v %v", is, err)
	}
}
