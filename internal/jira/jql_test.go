package jira

import "testing"

func TestQuoteJQLEscapes(t *testing.T) {
	for in, want := range map[string]string{
		`plain`:         `"plain"`,
		`say "hi"`:      `"say \"hi\""`,
		`a\b`:           `"a\\b"`,
		`end\`:          `"end\\"`,
		"two\nlines\t!": `"two lines !"`,
		`" OR 1=1 --`:   `"\" OR 1=1 --"`,
	} {
		if got := QuoteJQL(in); got != want {
			t.Errorf("QuoteJQL(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestSearchJQL(t *testing.T) {
	for _, c := range []struct {
		in, jql string
		ok      bool
	}{
		{"refund flow", `text ~ "refund flow" ORDER BY updated DESC`, true},
		{`a "quoted" \ word`, `text ~ "a \"quoted\" \\ word" ORDER BY updated DESC`, true},
		{`jql: project = PAY AND status = "Done"`, `project = PAY AND status = "Done"`, true},
		{`JQL:project = OPS`, `project = OPS`, true},
		{"jql:   ", "", false},
		{"   ", "", false},
	} {
		jql, ok := SearchJQL(c.in)
		if jql != c.jql || ok != c.ok {
			t.Errorf("SearchJQL(%q) = %q, %v; want %q, %v", c.in, jql, ok, c.jql, c.ok)
		}
	}
}
