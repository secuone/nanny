package config

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
)

func TestJiraDefaultsAndHelpers(t *testing.T) {
	j := Jira{URL: " acme.atlassian.net/ ", Email: " me@acme.com "}
	if j.BaseURL() != "https://acme.atlassian.net" || j.Host() != "acme.atlassian.net" || j.Account() != "me@acme.com@acme.atlassian.net" {
		t.Errorf("%q %q %q", j.BaseURL(), j.Host(), j.Account())
	}
	if j.Branch() != "{key}-{slug}" || j.Refresh() != 2*time.Minute {
		t.Errorf("%q %v", j.Branch(), j.Refresh())
	}
	j.BranchTemplate, j.RefreshMins = "{key}", 5
	if j.Branch() != "{key}" || j.Refresh() != 5*time.Minute {
		t.Errorf("%q %v", j.Branch(), j.Refresh())
	}
	if (Jira{}).BaseURL() != "" || (Jira{}).Host() != "" {
		t.Error("an unset Jira has no address")
	}
}

func TestJiraTableRoundTripsWithoutAToken(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c := Default()
	c.Jira = Jira{URL: "https://acme.atlassian.net", Email: "me@acme.com", SprintField: "customfield_10020"}
	if err := Save(c); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(Path())
	if !strings.Contains(string(b), "[jira]") || strings.Contains(strings.ToLower(string(b)), "token") {
		t.Errorf("config:\n%s", b)
	}
	var back Config
	if _, err := toml.Decode(string(b), &back); err != nil || back.Jira != c.Jira {
		t.Errorf("%+v %v", back.Jira, err)
	}
}
