package config

import (
	"net/url"
	"strings"
	"time"
)

// Jira is the [jira] table. The API token is never here: it lives in the
// keyring or a private file (see internal/secret), or in $NANNY_JIRA_TOKEN.
type Jira struct {
	URL            string `toml:"url,omitempty"`             // https://acme.atlassian.net
	Email          string `toml:"email,omitempty"`           // the account the token belongs to
	JQL            string `toml:"jql,omitempty"`             // what to list; "" = yours and open, or closed this week
	SprintField    string `toml:"sprint_field,omitempty"`    // customfield_NNNNN, found by itself when empty
	BranchTemplate string `toml:"branch_template,omitempty"` // {key} and {slug}; default {key}-{slug}
	RefreshMins    int    `toml:"refresh_mins,omitempty"`    // while the view is on screen; default 2
}

// DefaultBranchTemplate names the branch of an issue you start.
const DefaultBranchTemplate = "{key}-{slug}"

// BaseURL is the site address without a trailing slash, https:// added when missing.
func (j Jira) BaseURL() string {
	u := strings.TrimRight(strings.TrimSpace(j.URL), "/")
	if u != "" && !strings.Contains(u, "://") {
		u = "https://" + u
	}
	return u
}

// Host is the site's host name ("acme.atlassian.net").
func (j Jira) Host() string {
	if p, err := url.Parse(j.BaseURL()); err == nil {
		return p.Host
	}
	return ""
}

// Account names the token in the keyring: email@host.
func (j Jira) Account() string { return strings.TrimSpace(j.Email) + "@" + j.Host() }

// Branch is the template of the branch name, the default when empty.
func (j Jira) Branch() string {
	if t := strings.TrimSpace(j.BranchTemplate); t != "" {
		return t
	}
	return DefaultBranchTemplate
}

// Refresh is how often the view on screen asks again.
func (j Jira) Refresh() time.Duration {
	if j.RefreshMins <= 0 {
		return 2 * time.Minute
	}
	return time.Duration(j.RefreshMins) * time.Minute
}
