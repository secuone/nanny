// Package jira talks to Jira Cloud's REST API v3 with an email and an API token.
package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// User is a Jira account.
type User struct {
	AccountID string `json:"accountId"`
	Name      string `json:"displayName"`
	Email     string `json:"emailAddress"`
}

// Transition is a move an issue can make to another status.
type Transition struct {
	ID   string
	Name string
	To   string
}

// DevPR is a pull request that Jira's development panel links to an issue.
type DevPR struct {
	Name   string
	URL    string
	Status string
	Branch string
}

// API is what nanny asks of Jira; tests stand in for it.
type API interface {
	Myself(ctx context.Context) (User, error)
	Search(ctx context.Context, jql, sprintField string) ([]Issue, error)
	Transitions(ctx context.Context, key string) ([]Transition, error)
	DoTransition(ctx context.Context, key, id string) error
	Comment(ctx context.Context, key, text string) error
	Assign(ctx context.Context, key, accountID string) error // "" unassigns
	AssignableUsers(ctx context.Context, key, query string) ([]User, error)
	SprintField(ctx context.Context) (string, error)
	DevPRs(ctx context.Context, issueID string) ([]DevPR, error)
	Detail(ctx context.Context, key string) (Detail, error)
}

// ErrUnauthorized is a 401: the token or the email is wrong or expired.
var ErrUnauthorized = errors.New("jira: token rejected")

// RateLimitError is a 429; After is what Retry-After asked for.
type RateLimitError struct{ After time.Duration }

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("jira: rate limited, retry in %s", e.After.Round(time.Second))
}

// maxIssues caps what one search brings back.
const (
	pageSize  = 100
	maxIssues = 300
)

// Client is the REST client; it implements API.
type Client struct {
	base, email, token string
	hc                 *http.Client
}

// New makes a client for the site at baseURL (https://acme.atlassian.net).
func New(baseURL, email, token string) *Client {
	return &Client{base: strings.TrimRight(baseURL, "/"), email: email, token: token, hc: &http.Client{Timeout: 20 * time.Second}}
}

// do sends a request; out, when not nil, takes the JSON answer. The token is never in an error.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return err
	}
	if !secure(req.URL) {
		return errors.New("jira: the site must be https, the token would go in clear")
	}
	req.SetBasicAuth(c.email, c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return errors.New("jira: " + stripURL(err))
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return ErrUnauthorized
	case resp.StatusCode == http.StatusTooManyRequests:
		after, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
		return &RateLimitError{After: time.Duration(max(after, 1)) * time.Second}
	case resp.StatusCode >= 300:
		return fmt.Errorf("jira: %s: %s", resp.Status, message(data))
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

// stripURL keeps a transport error short and free of the address.
func stripURL(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err.Error()
	}
	return err.Error()
}

// message is what Jira says went wrong, from its error body.
func message(data []byte) string {
	var e struct {
		ErrorMessages []string          `json:"errorMessages"`
		Errors        map[string]string `json:"errors"`
	}
	if json.Unmarshal(data, &e) == nil {
		parts := e.ErrorMessages
		for _, v := range e.Errors {
			parts = append(parts, v)
		}
		if len(parts) > 0 {
			return strings.Join(parts, "; ")
		}
	}
	s := strings.TrimSpace(string(data))
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}

func (c *Client) Myself(ctx context.Context) (User, error) {
	var u User
	err := c.do(ctx, http.MethodGet, "/rest/api/3/myself", nil, &u)
	return u, err
}

func (c *Client) Transitions(ctx context.Context, key string) ([]Transition, error) {
	var r struct {
		Transitions []struct {
			ID, Name string
			To       struct{ Name string }
		}
	}
	if err := c.do(ctx, http.MethodGet, "/rest/api/3/issue/"+url.PathEscape(key)+"/transitions", nil, &r); err != nil {
		return nil, err
	}
	out := make([]Transition, 0, len(r.Transitions))
	for _, t := range r.Transitions {
		out = append(out, Transition{ID: t.ID, Name: t.Name, To: t.To.Name})
	}
	return out, nil
}

func (c *Client) DoTransition(ctx context.Context, key, id string) error {
	body := map[string]any{"transition": map[string]string{"id": id}}
	return c.do(ctx, http.MethodPost, "/rest/api/3/issue/"+url.PathEscape(key)+"/transitions", body, nil)
}

func (c *Client) Comment(ctx context.Context, key, text string) error {
	return c.do(ctx, http.MethodPost, "/rest/api/3/issue/"+url.PathEscape(key)+"/comment", map[string]any{"body": ADF(text)}, nil)
}

func (c *Client) Assign(ctx context.Context, key, accountID string) error {
	var id any
	if accountID != "" {
		id = accountID
	}
	return c.do(ctx, http.MethodPut, "/rest/api/3/issue/"+url.PathEscape(key)+"/assignee", map[string]any{"accountId": id}, nil)
}

func (c *Client) AssignableUsers(ctx context.Context, key, query string) ([]User, error) {
	var us []User
	q := url.Values{"issueKey": {key}, "query": {query}}
	err := c.do(ctx, http.MethodGet, "/rest/api/3/user/assignable/search?"+q.Encode(), nil, &us)
	return us, err
}

// SprintField finds the custom field that holds the sprint ("" when the site has none).
func (c *Client) SprintField(ctx context.Context) (string, error) {
	var fs []struct {
		ID     string `json:"id"`
		Schema struct {
			Custom string `json:"custom"`
		} `json:"schema"`
	}
	if err := c.do(ctx, http.MethodGet, "/rest/api/3/field", nil, &fs); err != nil {
		return "", err
	}
	for _, f := range fs {
		if f.Schema.Custom == "com.pyxis.greenhopper.jira:gl-sprint" {
			return f.ID, nil
		}
	}
	return "", nil
}

// ADF is plain text as an Atlassian Document: one paragraph per line.
func ADF(text string) map[string]any {
	var paras []any
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		paras = append(paras, map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": line}}})
	}
	if paras == nil {
		paras = []any{map[string]any{"type": "paragraph", "content": []any{}}}
	}
	return map[string]any{"type": "doc", "version": 1, "content": paras}
}

// secure says the token may go to u: https, or plain http to this machine (tests, a local proxy).
func secure(u *url.URL) bool {
	if u.Scheme == "https" {
		return true
	}
	h := u.Hostname()
	return u.Scheme == "http" && (h == "localhost" || h == "127.0.0.1" || h == "::1")
}
