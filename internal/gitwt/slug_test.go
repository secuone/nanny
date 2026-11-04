package gitwt

import "testing"

func TestParseSlug(t *testing.T) {
	for url, want := range map[string]string{
		"https://github.com/o/r.git":       "o/r",
		"https://github.com/o/r":           "o/r",
		"https://github.com/o/r/":          "o/r",
		"git@github.com:o/r.git":           "o/r",
		"git@github.com:o/r":               "o/r",
		"ssh://git@github.com/o/r.git":     "o/r",
		"ssh://git@github.com:22/o/r":      "o/r",
		"https://tok@github.com/o/r.git\n": "o/r",
		"git@gitlab.com:o/r.git":           "",
		"/srv/git/r.git":                   "",
		"":                                 "",
	} {
		if got := ParseSlug(url); got != want {
			t.Errorf("ParseSlug(%q) = %q, want %q", url, got, want)
		}
	}
}
