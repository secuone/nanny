package ui

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/secuone/nanny/internal/claude"
	"github.com/secuone/nanny/internal/term"
)

// TestRealClaude drives a real claude (haiku) through a permission prompt.
// Opt-in, it spends tokens: NANNY_REAL_DIR=<scratch folder> go test -run RealClaude ./internal/ui
func TestRealClaude(t *testing.T) {
	dir := os.Getenv("NANNY_REAL_DIR")
	if dir == "" {
		t.Skip("set NANNY_REAL_DIR to run against a real claude")
	}
	_ = os.RemoveAll(dir + "/probe_dir")
	p, err := term.Start(dir, []string{"claude", "--model", "haiku", "--permission-mode", "default",
		"Use the Bash tool to run exactly this command and nothing else: sleep 6 && mkdir -p probe_dir && echo ok > probe_dir/f.txt"}, 120, 40, nil, func() {})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { p.Close() }()
	p.Agent = "claude"
	seen := map[string]bool{}
	for end := time.Now().Add(150 * time.Second); time.Now().Before(end); time.Sleep(300 * time.Millisecond) {
		st := agentState(p)
		seen[st] = true
		if pr := claude.ParsePrompt(p.Text(60)); pr != nil {
			y := pr.Yes()
			if y == nil {
				t.Fatalf("no yes option in %+v", pr)
			}
			p.Write([]byte(pr.Press(pr.Index(y))))
			time.Sleep(2 * time.Second)
			continue
		}
		if _, err := os.Stat(dir + "/probe_dir/f.txt"); err == nil && st == "idle" {
			if !seen["working"] || !seen["blocked"] {
				t.Fatalf("states seen: %v", seen)
			}
			// detach and reattach: the agent keeps its screen
			sock := p.Socket()
			p.Detach()
			q, err := term.Attach(sock, dir, nil, 120, 40, func() {})
			if err != nil {
				t.Fatal(err)
			}
			p = q
			time.Sleep(time.Second)
			if !strings.Contains(q.Text(60), "probe_dir") {
				t.Fatalf("reattached screen lost the conversation:\n%s", q.Text(40))
			}
			return
		}
	}
	t.Fatalf("timeout (states %v); screen:\n%s", seen, p.Text(40))
}

// TestRealClaudeTell declines a permission prompt with "tell it instead" and
// checks the agent got the message and never ran the command.
func TestRealClaudeTell(t *testing.T) {
	dir := os.Getenv("NANNY_REAL_DIR")
	if dir == "" {
		t.Skip("set NANNY_REAL_DIR to run against a real claude")
	}
	_ = os.RemoveAll(dir + "/tell_dir")
	p, err := term.StartLocal(dir, []string{"claude", "--model", "haiku", "--permission-mode", "default",
		"Use the Bash tool to run exactly: mkdir tell_dir"}, 120, 40, nil, func() {})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { p.Close() }()
	p.Agent = "claude"
	told := false
	for end := time.Now().Add(120 * time.Second); time.Now().Before(end); time.Sleep(300 * time.Millisecond) {
		scr := p.Text(80)
		if pr := claude.ParsePrompt(scr); pr != nil && !told {
			if strings.Contains(strings.ToLower(pr.Title), "trust") || pr.Title == "Accessing workspace:" {
				p.Write([]byte(pr.Press(pr.Index(pr.Yes()))))
				time.Sleep(2 * time.Second)
				continue
			}
			o := pr.Tell()
			p.Write([]byte(pr.Press(pr.Index(o))))
			time.Sleep(700 * time.Millisecond)
			p.Write([]byte("\x1b[200~Do not run anything. Just reply with the single word PINEAPPLE.\x1b[201~"))
			p.Write([]byte("\r"))
			told = true
			continue
		}
		if told && strings.Count(scr, "PINEAPPLE") >= 2 && agentState(p) == "idle" {
			if _, err := os.Stat(dir + "/tell_dir"); err == nil {
				t.Fatal("the command ran anyway")
			}
			return
		}
	}
	t.Fatalf("timeout (told=%v); screen:\n%s", told, p.Text(40))
}
