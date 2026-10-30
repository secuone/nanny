// Package demo builds a fake $HOME full of plausible Claude Code sessions,
// subagents and git worktrees, so nanny can be shown off (screenshots,
// GIFs, first-run tours) without exposing anyone's real work.
package demo

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type sess struct {
	id, repo, wt, branch, title string
	ago                         time.Duration
	live, busy                  bool
	cost                        float64
	ctx                         int // context of the last turn, in tokens
	convo                       []turn
	subs                        []sub
}

type turn struct{ role, text string }

type sub struct {
	typ, desc, tool string
	running         bool
}

var sessions = []sess{
	{id: "5f0c2a1e-8d3b-4c61-9a57-1b2e3c4d5e6f", repo: "acme-api", wt: "PAY-412", branch: "feature/PAY-412-refunds", title: "Partial refunds for split payments",
		ago: 20 * time.Second, live: true, busy: true, cost: 0, ctx: 124_000,
		convo: []turn{{"user", "Implement partial refunds for split payments. Keep the ledger balanced and add tests."},
			{"assistant", "I'll map the payment adapters first, then design the ledger entries. Spawning two explorers in parallel."},
			{"tool", "Task Map payment adapters"}, {"tool", "Task Find ledger invariants"},
			{"assistant", "Both adapters expose `refund(amount)`. The ledger needs a compensating entry per split. Writing the use case now."},
			{"tool", "Edit src/Payments/RefundSplitPayment.php"}, {"tool", "Bash vendor/bin/phpunit --filter Refund"}},
		subs: []sub{{"Explore", "Map payment adapters and refund APIs", "Grep refund(", true},
			{"general-purpose", "Run the full test suite on PHP 8.3", "Bash make test", true},
			{"Explore", "Find ledger invariants", "Read src/Ledger/Entry.php", false}}},
	{id: "7a8b9c0d-1e2f-4a3b-8c4d-5e6f7a8b9c0d", repo: "web-checkout", branch: "main", title: "Migrate checkout to the new design system",
		ago: 3 * time.Minute, live: true, cost: 0, ctx: 176_000,
		convo: []turn{{"user", "Swap the old buttons and inputs for the design-system components on the checkout page."},
			{"assistant", "Done: 14 components replaced, snapshots updated, a11y labels preserved. Want me to open the PR?"}}},
	{id: "1b2c3d4e-5f60-4718-293a-4b5c6d7e8f90", repo: "acme-api", wt: "OPS-88", branch: "chore/OPS-88-redis-7", title: "Upgrade Redis client to v7",
		ago: 2 * time.Hour, cost: 1.84, ctx: 61_000, convo: []turn{{"user", "Upgrade predis to v7 and fix whatever breaks."}, {"assistant", "Upgraded; two deprecated calls fixed. PR #231 is open."}},
		subs: []sub{{"Explore", "List every Redis call site", "", false}}},
	{id: "2c3d4e5f-6071-4829-3a4b-5c6d7e8f9012", repo: "infra", branch: "main", title: "Why is the staging deploy slow?",
		ago: 26 * time.Hour, cost: 0.62, ctx: 38_000, convo: []turn{{"user", "Staging deploys take 14 minutes. Find out why."}, {"assistant", "The image rebuilds composer deps every time: the cache layer is invalidated by a COPY . . before install."}}},
	{id: "3d4e5f60-7182-493a-4b5c-6d7e8f901234", repo: "web-checkout", wt: "UX-19", branch: "feature/UX-19-address-autocomplete", title: "Address autocomplete with debounce",
		ago: 3 * 24 * time.Hour, cost: 2.31, ctx: 92_000, convo: []turn{{"user", "Add address autocomplete to the shipping form."}, {"assistant", "Implemented with a 250ms debounce and keyboard navigation."}}},
	{id: "4e5f6071-8293-4a4b-5c6d-7e8f90123456", repo: "acme-api", branch: "main", title: "Liquibase changeset for the refunds table",
		ago: 5 * 24 * time.Hour, cost: 0.95, ctx: 47_000, convo: []turn{{"user", "Write the liquibase changeset for refunds with a rollback."}, {"assistant", "Added 2026-10-refunds.xml with createTable + rollback dropTable."}}},
	{id: "5f607182-93a4-4b5c-6d7e-8f9012345678", repo: "dotfiles", branch: "main", title: "Tidy up zsh startup time",
		ago: 9 * 24 * time.Hour, cost: 0.31, ctx: 22_000, convo: []turn{{"user", "My shell takes 900ms to start, fix it."}, {"assistant", "Lazy-loaded nvm and compinit; startup is now 120ms."}}},
}

var repeats = []struct {
	repo, text string
	n          int
}{
	{"acme-api", "Run the test suite and fix whatever fails, then show me the diff.", 9},
	{"acme-api", "Review your changes for bugs and missing tests before we go on.", 6},
	{"web-checkout", "Commit with a conventional message and open a draft PR.", 5},
	{"infra", "Check the CI logs of the last failed run and tell me what broke.", 4},
	{"acme-api", "Write the liquibase changeset with a rollback for this table.", 3},
	{"acme-api", "/clear", 7},
	{"web-checkout", "/review", 3},
}

var nonAlnum = regexp.MustCompile(`[^A-Za-z0-9]`)

// Build creates the fake home under dir and returns the session count.
func Build(dir string) error {
	now := time.Now()
	dev := filepath.Join(dir, "dev")
	repos := map[string]bool{}
	for _, s := range sessions {
		repos[s.repo] = true
	}
	for r := range repos {
		if err := gitRepo(filepath.Join(dev, r)); err != nil {
			return err
		}
	}
	var hist []string
	for i, s := range sessions {
		cwd := filepath.Join(dev, s.repo)
		if s.wt != "" {
			cwd = filepath.Join(cwd, ".claude", "worktrees", s.wt)
			_ = run(filepath.Join(dev, s.repo), "git", "worktree", "add", "-q", "-b", s.branch, cwd)
		}
		proj := filepath.Join(dir, ".claude", "projects", nonAlnum.ReplaceAllString(cwd, "-"))
		_ = os.MkdirAll(proj, 0o755)
		path := filepath.Join(proj, s.id+".jsonl")
		start := now.Add(-s.ago).Add(-time.Duration(len(s.convo)) * time.Minute)
		var lines []string
		add := func(v any) { b, _ := json.Marshal(v); lines = append(lines, string(b)) }
		for j, t := range s.convo {
			ts := start.Add(time.Duration(j) * time.Minute).UTC().Format(time.RFC3339Nano)
			base := map[string]any{"sessionId": s.id, "cwd": cwd, "gitBranch": s.branch, "version": "2.1.300",
				"entrypoint": "cli", "timestamp": ts, "isSidechain": false, "uuid": fmt.Sprintf("u-%d-%d", i, j)}
			switch t.role {
			case "user":
				base["type"] = "user"
				base["message"] = map[string]any{"role": "user", "content": t.text}
				if j == 0 {
					hist = append(hist, mustJSON(map[string]any{"display": t.text, "timestamp": start.UnixMilli(), "project": cwd, "sessionId": s.id}))
				}
			case "assistant":
				base["type"] = "assistant"
				base["message"] = map[string]any{"role": "assistant", "model": "claude-opus-5-5", "id": fmt.Sprintf("m%d%d", i, j),
					"content": []map[string]any{{"type": "text", "text": t.text}}, "usage": usage(s.ctx*(j+1)/len(s.convo), 900)}
			case "tool":
				name, arg, _ := strings.Cut(t.text, " ")
				key := "command"
				switch name {
				case "Edit", "Read":
					key = "file_path"
				case "Task":
					key = "description"
				}
				base["type"] = "assistant"
				base["message"] = map[string]any{"role": "assistant", "model": "claude-opus-5-5", "id": fmt.Sprintf("m%d%d", i, j),
					"content": []map[string]any{{"type": "tool_use", "name": name, "input": map[string]any{key: arg}}}, "usage": usage(s.ctx*(j+1)/len(s.convo), 300)}
			}
			add(base)
		}
		add(map[string]any{"type": "system", "subtype": "turn_duration", "messageCount": len(s.convo) * 3, "sessionId": s.id})
		add(map[string]any{"type": "ai-title", "aiTitle": s.title, "sessionId": s.id})
		if s.cost > 0 {
			add(map[string]any{"type": "cost-state", "totalCostUSD": s.cost, "totalLinesAdded": int(s.cost * 120), "totalLinesRemoved": int(s.cost * 40)})
		}
		if s.wt != "" {
			add(map[string]any{"type": "worktree-state", "worktreeSession": map[string]any{"worktreeName": s.wt, "worktreePath": cwd}})
		}
		if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			return err
		}
		_ = os.Chtimes(path, now.Add(-s.ago), now.Add(-s.ago))
		writeSubs(strings.TrimSuffix(path, ".jsonl"), s, now)
		if s.live {
			status := "idle"
			if s.busy {
				status = "busy"
			}
			sd := filepath.Join(dir, ".claude", "sessions")
			_ = os.MkdirAll(sd, 0o755)
			// nanny execs itself into the demo, so its own pid stays alive;
			// procStart is left empty so the pid-reuse check is skipped
			pid := os.Getpid()
			_ = os.WriteFile(filepath.Join(sd, fmt.Sprintf("%d-%d.json", pid, i)), []byte(mustJSON(map[string]any{
				"pid": pid, "sessionId": s.id, "cwd": cwd, "status": status, "kind": "interactive", "entrypoint": "cli",
				"startedAt": now.Add(-s.ago - 40*time.Minute).UnixMilli(), "version": "2.1.300"})), 0o644)
		}
	}
	if err := stageAgents(dir, dev); err != nil {
		return err
	}
	// prompts typed again and again, for the prompt library's Most used
	for i, r := range repeats {
		for k := 0; k < r.n; k++ {
			at := now.Add(-time.Duration(k*29+i*7) * time.Hour)
			hist = append(hist, mustJSON(map[string]any{"display": r.text, "timestamp": at.UnixMilli(), "project": filepath.Join(dev, r.repo),
				"sessionId": sessions[(i+k)%len(sessions)].id, "pastedContents": map[string]any{}}))
		}
	}
	return os.WriteFile(filepath.Join(dir, ".claude", "history.jsonl"), []byte(strings.Join(hist, "\n")+"\n"), 0o644)
}

func writeSubs(dir string, s sess, now time.Time) {
	if len(s.subs) == 0 {
		return
	}
	sd := filepath.Join(dir, "subagents")
	_ = os.MkdirAll(sd, 0o755)
	for k, sb := range s.subs {
		id := fmt.Sprintf("a%016x", k+1)
		meta := map[string]any{"agentType": sb.typ, "description": sb.desc, "spawnDepth": 1, "requestShape": "background"}
		if !sb.running {
			meta["finished"] = true
		}
		_ = os.WriteFile(filepath.Join(sd, "agent-"+id+".meta.json"), []byte(mustJSON(meta)), 0o644)
		var lines []string
		ts := now.Add(-time.Minute).UTC().Format(time.RFC3339Nano)
		lines = append(lines, mustJSON(map[string]any{"type": "user", "isSidechain": true, "agentId": id, "timestamp": ts,
			"message": map[string]any{"role": "user", "content": sb.desc}}))
		model := "claude-haiku-5-5"
		if sb.typ != "Explore" {
			model = "claude-sonnet-5-5"
		}
		lines = append(lines, mustJSON(map[string]any{"type": "assistant", "isSidechain": true, "agentId": id, "timestamp": ts,
			"message": map[string]any{"role": "assistant", "model": model, "id": id + "-1", "usage": usage(18_000, 400),
				"content": []map[string]any{{"type": "text", "text": "On it — " + strings.ToLower(sb.desc) + "."}}}}))
		if sb.tool != "" {
			name, arg, _ := strings.Cut(sb.tool, " ")
			lines = append(lines, mustJSON(map[string]any{"type": "assistant", "isSidechain": true, "agentId": id, "timestamp": ts,
				"message": map[string]any{"role": "assistant", "model": model, "id": id + "-2", "usage": usage(31_000, 250),
					"content": []map[string]any{{"type": "tool_use", "name": name, "input": map[string]any{"command": arg}}}}}))
		}
		p := filepath.Join(sd, "agent-"+id+".jsonl")
		_ = os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
		if !sb.running {
			_ = os.Chtimes(p, now.Add(-s.ago-10*time.Minute), now.Add(-s.ago-10*time.Minute))
		}
	}
}

// usage is a plausible message.usage for a turn with ctx tokens of context:
// mostly cache reads, a fresh slice written to the cache, a few raw input tokens.
func usage(ctx, out int) map[string]any {
	w := min(ctx/5, 24_000)
	return map[string]any{"input_tokens": 6, "cache_creation_input_tokens": w, "cache_read_input_tokens": ctx - w - 6, "output_tokens": out}
}

func gitRepo(path string) error {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}
	_ = os.WriteFile(filepath.Join(path, "README.md"), []byte("# "+filepath.Base(path)+"\n"), 0o644)
	for _, c := range [][]string{{"git", "init", "-q", "-b", "main"}, {"git", "add", "."},
		{"git", "-c", "user.name=demo", "-c", "user.email=demo@example.com", "commit", "-q", "-m", "chore: initial commit"}} {
		if err := run(path, c[0], c[1:]...); err != nil {
			return err
		}
	}
	return nil
}

func run(dir, name string, args ...string) error {
	c := exec.Command(name, args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
	return c.Run()
}

func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
