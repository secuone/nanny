package claude

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/secuone/nanny/internal/demo"
)

func demoHome(t *testing.T) []*Session {
	t.Helper()
	dir := t.TempDir()
	if err := demo.Build(dir); err != nil {
		t.Fatalf("demo: %v", err)
	}
	SetHome(dir)
	return NewScanner().Scan()
}

func TestScan(t *testing.T) {
	ss := demoHome(t)
	if len(ss) != 7 {
		t.Fatalf("want 7 sessions, got %d", len(ss))
	}
	if ss[0].Title != "Partial refunds for split payments" {
		t.Errorf("newest first: got %q", ss[0].Title)
	}
	live, wts := 0, 0
	for _, s := range ss {
		if s.Live != nil {
			live++
		}
		if s.Worktree != "" {
			wts++
		}
		if s.Cwd == "" || s.Model == "" || s.FirstPrompt == "" {
			t.Errorf("%s: missing cwd/model/first prompt: %+v", s.ID, s)
		}
	}
	if live != 2 || wts != 3 {
		t.Errorf("live=%d (want 2) worktrees=%d (want 3)", live, wts)
	}
	if ss[0].Worktree != "PAY-412" || ss[0].Branch != "feature/PAY-412-refunds" {
		t.Errorf("worktree/branch: %q %q", ss[0].Worktree, ss[0].Branch)
	}
}

func TestSubagents(t *testing.T) {
	ss := demoHome(t)
	subs := Flatten(Subagents(ss[0].Path))
	if len(subs) != 3 {
		t.Fatalf("want 3 subagents, got %d", len(subs))
	}
	running := 0
	for _, s := range subs {
		if s.Running {
			running++
			if s.LastTool == "" {
				t.Errorf("running subagent %s has no last tool", s.Description)
			}
		}
	}
	if running != 2 {
		t.Errorf("want 2 running, got %d", running)
	}
}

func TestTranscript(t *testing.T) {
	ss := demoHome(t)
	es := Transcript(ss[0].Path, 50)
	if len(es) < 5 || es[0].Role != "user" {
		t.Fatalf("unexpected transcript: %+v", es)
	}
	if !strings.HasPrefix(es[len(es)-1].Text, "Bash ") {
		t.Errorf("last entry should be a Bash tool call, got %q", es[len(es)-1].Text)
	}
}

func TestMatch(t *testing.T) {
	ss := demoHome(t)
	for q, want := range map[string]string{
		"PAY-412":     "Partial refunds for split payments",
		"redis":       "Upgrade Redis client to v7",
		"checkout ux": "Address autocomplete with debounce",
		ss[3].ID[:8]:  ss[3].Title,
		"zsh startup": "Tidy up zsh startup time",
	} {
		got := Match(ss, q)
		if len(got) == 0 || got[0].Title != want {
			t.Errorf("Match(%q) = %v, want %q first", q, titles(got), want)
		}
	}
}

func TestSearch(t *testing.T) {
	ss := demoHome(t)
	hits := Search(ss, "LEDGER", 3)
	if len(hits) != 1 {
		t.Fatalf("want ledger in 1 session, got %d", len(hits))
	}
	for _, hs := range hits {
		if !strings.Contains(strings.ToLower(hs[0].Snippet), "ledger") {
			t.Errorf("snippet lacks match: %q", hs[0].Snippet)
		}
	}
}

func TestShortTool(t *testing.T) {
	if got := ShortTool("mcp__claude_ai_Atlassian__getJiraIssue"); got != "Atlassian:getJiraIssue" {
		t.Errorf("got %q", got)
	}
	if got := ShortTool("Bash"); got != "Bash" {
		t.Errorf("got %q", got)
	}
}

func titles(ss []*Session) []string {
	var out []string
	for _, s := range ss {
		out = append(out, s.Title)
	}
	return out
}

// slim keeps the fields a transcript's tail reads, skipping the content in
// between (escaped quotes, braces inside strings, nested arrays), and decodes
// the same as the whole line.
func TestSlimLine(t *testing.T) {
	l := []byte(`{"parentUuid":null,"isSidechain":false,"type":"assistant","message":{"model":"claude-opus-5-5","id":"m1","content":[{"type":"text","text":"a \"quoted\" {brace} [x] \\"}],"usage":{"input_tokens":3}},"toolUseResult":{"stdout":"}}]]\"","n":[1,2,{"a":null}]},"cwd":"/x","gitBranch":"main","version":"2.1","entrypoint":"cli","n":-1.5e3,"ok":true}`)
	var full, part rec
	if json.Unmarshal(l, &full) != nil {
		t.Fatal("bad fixture")
	}
	s := slim(l)
	if len(s) >= len(l) || json.Unmarshal(s, &part) != nil {
		t.Fatalf("slim: %s", s)
	}
	if part.Type != "assistant" || part.Cwd != "/x" || part.GitBranch != "main" || part.Version != "2.1" || part.Entrypoint != "cli" || part.Message == nil || part.Message.Model != full.Message.Model {
		t.Errorf("fields lost: %s", s)
	}
	if bad := []byte(`{"type":"user","message":{"content":"unterminated`); string(slim(bad)) != string(bad) {
		t.Error("a line it can't walk is left as it is")
	}
}
