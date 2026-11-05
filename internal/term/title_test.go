package term

import (
	"strings"
	"testing"
)

func TestClaudeTitleKeepsItsSpinner(t *testing.T) {
	p, err := Start(t.TempDir(), []string{"sh", "-c", `printf '\033]0;✳ Claude Code\007ready'; exec cat`}, 60, 10, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	waitFor(t, p, "ready")
	if got := p.Title(); got != "✳ Claude Code" {
		t.Fatalf("title = %q", got)
	}
	if text := p.Text(10); strings.Contains(text, "Claude Code") {
		t.Fatalf("the title leaked onto the screen:\n%s", text)
	}
}

func TestCleanTitle(t *testing.T) {
	for in, want := range map[string]string{
		"✳ Claude Code":      "✳ Claude Code",
		"\xe2":               "",
		"a\x1b[2Jb\x9c\tc\n": "a[2Jbc",
		"Über ⠂ build":       "Über ⠂ build",
	} {
		if got := cleanTitle(in); got != want {
			t.Errorf("cleanTitle(%q) = %q, want %q", in, got, want)
		}
	}
}
