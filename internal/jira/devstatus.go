package jira

import (
	"context"
	"net/http"
	"net/url"
)

// DevPRs asks the development panel for the pull requests of an issue by its
// id. It is an undocumented API: callers treat any error as "none".
func (c *Client) DevPRs(ctx context.Context, issueID string) ([]DevPR, error) {
	q := url.Values{"issueId": {issueID}, "applicationType": {"GitHub"}, "dataType": {"pullrequest"}}
	var r struct {
		Detail []struct {
			PullRequests []struct {
				Name, URL, Status string
				Source            struct{ Branch string }
			}
		}
	}
	if err := c.do(ctx, http.MethodGet, "/rest/dev-status/latest/issue/detail?"+q.Encode(), nil, &r); err != nil {
		return nil, err
	}
	var out []DevPR
	for _, d := range r.Detail {
		for _, p := range d.PullRequests {
			out = append(out, DevPR{Name: p.Name, URL: p.URL, Status: p.Status, Branch: p.Source.Branch})
		}
	}
	return out, nil
}
