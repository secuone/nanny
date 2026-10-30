package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Issue is what nanny shows of a Jira issue.
type Issue struct {
	Key, ID, Summary string
	Status           string
	StatusCat        string // new | indeterminate | done
	Type, Priority   string
	Project          string
	Sprint           string
	SprintState      string // active | future | closed
	Labels           []string
	Assignee         string
	Reporter         string
	Created          time.Time
	Updated          time.Time
	Resolved         time.Time
	URL              string
}

// DefaultJQL is your open issues and what you closed this week.
const DefaultJQL = "assignee = currentUser() AND (statusCategory != Done OR resolved >= -7d) ORDER BY updated DESC"

type rawIssue struct {
	Key    string
	ID     string
	Fields map[string]json.RawMessage
}

func (r rawIssue) name(field string) string {
	var v struct{ Name string }
	_ = json.Unmarshal(r.Fields[field], &v)
	return v.Name
}

// person is the display name of a user field (assignee, reporter).
func (r rawIssue) person(field string) string {
	var v struct {
		Name string `json:"displayName"`
	}
	_ = json.Unmarshal(r.Fields[field], &v)
	return v.Name
}

// stamp reads Jira's "2026-10-30T10:44:31.000+0100" (and plain RFC 3339).
func stamp(s string) time.Time {
	for _, l := range []string{"2006-01-02T15:04:05.000-0700", time.RFC3339, "2006-01-02T15:04:05-0700"} {
		if t, err := time.Parse(l, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func (c *Client) issue(r rawIssue, sprintField string) Issue {
	is := Issue{Key: r.Key, ID: r.ID, Status: r.name("status"), Type: r.name("issuetype"), Priority: r.name("priority"),
		URL: c.base + "/browse/" + r.Key}
	_ = json.Unmarshal(r.Fields["summary"], &is.Summary)
	_ = json.Unmarshal(r.Fields["labels"], &is.Labels)
	var st struct {
		StatusCategory struct{ Key string } `json:"statusCategory"`
	}
	_ = json.Unmarshal(r.Fields["status"], &st)
	is.StatusCat = st.StatusCategory.Key
	var pr struct{ Key, Name string }
	_ = json.Unmarshal(r.Fields["project"], &pr)
	is.Project = pr.Key
	var s string
	_ = json.Unmarshal(r.Fields["updated"], &s)
	is.Updated = stamp(s)
	s = ""
	_ = json.Unmarshal(r.Fields["created"], &s)
	is.Created = stamp(s)
	is.Assignee, is.Reporter = r.person("assignee"), r.person("reporter")
	s = ""
	_ = json.Unmarshal(r.Fields["resolutiondate"], &s)
	is.Resolved = stamp(s)
	if sprintField != "" {
		is.Sprint, is.SprintState = pickSprint(r.Fields[sprintField])
	}
	return is
}

// pickSprint chooses the sprint to show among those an issue went through:
// the active one, else the next, else the last.
func pickSprint(raw json.RawMessage) (name, state string) {
	var sp []struct{ Name, State string }
	if json.Unmarshal(raw, &sp) != nil || len(sp) == 0 {
		return "", ""
	}
	rank := map[string]int{"active": 0, "future": 1, "closed": 2}
	sort.SliceStable(sp, func(i, j int) bool { return rank[sp[i].State] < rank[sp[j].State] })
	best := sp[0]
	if best.State == "closed" {
		best = sp[len(sp)-1]
	}
	return best.Name, best.State
}

// Search runs a JQL query on /search/jql (the old /search is retired), a page
// at a time with nextPageToken, up to maxIssues.
func (c *Client) Search(ctx context.Context, jql, sprintField string) ([]Issue, error) {
	fields := []string{"summary", "status", "issuetype", "priority", "project", "labels", "updated", "resolutiondate", "assignee", "reporter", "created"}
	if sprintField != "" {
		fields = append(fields, sprintField)
	}
	var out []Issue
	token := ""
	for len(out) < maxIssues {
		body := map[string]any{"jql": jql, "fields": fields, "maxResults": pageSize}
		if token != "" {
			body["nextPageToken"] = token
		}
		var page struct {
			Issues        []rawIssue
			NextPageToken string
			IsLast        bool
		}
		if err := c.do(ctx, http.MethodPost, "/rest/api/3/search/jql", body, &page); err != nil {
			return nil, err
		}
		for _, r := range page.Issues {
			out = append(out, c.issue(r, sprintField))
		}
		if page.IsLast || page.NextPageToken == "" || len(page.Issues) == 0 {
			break
		}
		token = page.NextPageToken
	}
	if len(out) > maxIssues {
		out = out[:maxIssues]
	}
	return out, nil
}

// Matches reports whether every word of q is in the issue's key, summary, status, project, sprint, type, assignee or labels.
func (is Issue) Matches(q string) bool {
	hay := strings.ToLower(strings.Join(append([]string{is.Key, is.Summary, is.Status, is.Project, is.Sprint, is.Type, is.Assignee}, is.Labels...), " "))
	for _, w := range strings.Fields(strings.ToLower(q)) {
		if !strings.Contains(hay, w) {
			return false
		}
	}
	return true
}
