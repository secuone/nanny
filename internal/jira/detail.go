package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"
)

// Comment is one comment of an issue, its body already as text.
type Comment struct {
	Author  string
	Created time.Time
	Body    string
}

// Link is another issue: a subtask or one the issue is linked to (Rel says how).
type Link struct {
	Rel, Key, Summary, Status string
}

// Detail is what the reader shows of an issue beyond the list: the
// description, the comments, the subtasks and the links.
type Detail struct {
	Description  string
	Comments     []Comment
	CommentTotal int
	Subtasks     []Link
	Links        []Link
}

// maxComments is how many comments one detail keeps (the newest ones).
const maxComments = 20

// Detail reads the description, comments, subtasks and links of an issue.
func (c *Client) Detail(ctx context.Context, key string) (Detail, error) {
	var r struct {
		Fields struct {
			Description json.RawMessage
			Comment     struct {
				Total    int
				Comments []struct {
					Author  struct{ DisplayName string }
					Created string
					Body    json.RawMessage
				}
			}
			Subtasks   []linkIssue
			IssueLinks []struct {
				Type struct{ Inward, Outward string }
				In   *linkIssue `json:"inwardIssue"`
				Out  *linkIssue `json:"outwardIssue"`
			}
		}
	}
	q := url.Values{"fields": {"description,comment,subtasks,issuelinks"}}
	if err := c.do(ctx, http.MethodGet, "/rest/api/3/issue/"+url.PathEscape(key)+"?"+q.Encode(), nil, &r); err != nil {
		return Detail{}, err
	}
	f := r.Fields
	d := Detail{Description: ADFText(f.Description), CommentTotal: max(f.Comment.Total, len(f.Comment.Comments))}
	for _, cm := range f.Comment.Comments {
		d.Comments = append(d.Comments, Comment{Author: cm.Author.DisplayName, Created: stamp(cm.Created), Body: ADFText(cm.Body)})
	}
	if len(d.Comments) > maxComments {
		d.Comments = d.Comments[len(d.Comments)-maxComments:]
	}
	for _, s := range f.Subtasks {
		d.Subtasks = append(d.Subtasks, s.link("subtask"))
	}
	for _, l := range f.IssueLinks {
		switch {
		case l.Out != nil:
			d.Links = append(d.Links, l.Out.link(l.Type.Outward))
		case l.In != nil:
			d.Links = append(d.Links, l.In.link(l.Type.Inward))
		}
	}
	return d, nil
}

type linkIssue struct {
	Key    string
	Fields struct {
		Summary string
		Status  struct{ Name string }
	}
}

func (l linkIssue) link(rel string) Link {
	return Link{Rel: rel, Key: l.Key, Summary: l.Fields.Summary, Status: l.Fields.Status.Name}
}
