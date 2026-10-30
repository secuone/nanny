package jira

import "strings"

// JQLPrefix starts a typed JQL query in the search box.
const JQLPrefix = "jql:"

// QuoteJQL is s as a JQL string literal: backslashes and quotes escaped,
// line breaks and other control characters turned into spaces.
func QuoteJQL(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '\\' || r == '"':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// SearchJQL is the query a search box asks for: "jql:" and the rest as typed,
// anything else as a text search over every issue you can see. ok is false
// for an empty search.
func SearchJQL(input string) (jql string, ok bool) {
	in := strings.TrimSpace(input)
	if len(in) >= len(JQLPrefix) && strings.EqualFold(in[:len(JQLPrefix)], JQLPrefix) {
		in = strings.TrimSpace(in[len(JQLPrefix):])
		return in, in != ""
	}
	if in == "" {
		return "", false
	}
	return "text ~ " + QuoteJQL(in) + " ORDER BY updated DESC", true
}
