package demo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// The demo restores two spaces whose panes run a stand-in for claude
// (bin/claude, put first on PATH by `nanny demo`): one agent waits on a
// permission prompt, the other is idle. Both answer what you type, so the
// prompt answering, broadcast and new-task flows can be tried for real.
const (
	blockedID = "1b2c3d4e-5f60-4718-293a-4b5c6d7e8f90" // Upgrade Redis client to v7
	idleID    = "2c3d4e5f-6071-4829-3a4b-5c6d7e8f9012" // Why is the staging deploy slow?
)

const fakeClaude = `#!/bin/sh
# nanny demo: a stand-in for claude, so the demo has agents to talk to
id=""; first=""
while [ $# -gt 0 ]; do
  case "$1" in
    --resume|-r) id="$2"; shift 2 ;;
    -*) shift ;;
    *) first="$1"; shift ;;
  esac
done
f="$HOME/.claude/sessions/demo-$$.json"
state() {
  [ -n "$id" ] || return 0
  printf '{"pid":%d,"sessionId":"%s","cwd":"%s","status":"%s","kind":"interactive","entrypoint":"cli","startedAt":%s000}\n' \
    $$ "$id" "$PWD" "$1" "$(date +%s)" > "$f"
}
trap 'rm -f "$f"; exit 0' EXIT INT TERM HUP
o='\033[38;5;209m'; d='\033[2m'; b='\033[1m'; g='\033[32m'; r='\033[0m'
say() { printf "${o}●${r} %s\n\n" "$1"; }
done_() { printf "${g}  ⎿  %s${r}\n\n" "$1"; }

ask() {
  printf '\033[2J\033[H'
  say "I'll bump predis to v7 and run the Redis integration tests against a real container."
  printf "${d}────────────────────────────────────────────────────────────${r}\n"
  printf " ${b}Bash command${r}\n\n"
  printf "   docker compose run --rm api vendor/bin/phpunit --group redis\n"
  printf "   ${d}Run the Redis integration tests${r}\n\n"
  printf " Do you want to proceed?\n"
  printf " ${o}❯${r} 1. Yes\n"
  printf "   2. Yes, and don't ask again for docker compose run commands in this project\n"
  printf "   3. No, and tell Claude what to do differently (esc)\n"
}

blocked() {
  state busy
  stty -icanon -echo min 1 2>/dev/null
  while :; do
    ask
    k=$(dd bs=1 count=1 2>/dev/null)
    case "$k" in
      1|2)
        printf '\033[2J\033[H'
        say "I'll bump predis to v7 and run the Redis integration tests against a real container."
        printf "${o}●${r} Bash(docker compose run --rm api vendor/bin/phpunit --group redis)\n"
        done_ "OK (12 tests, 40 assertions)"
        [ "$k" = 2 ] && printf "${d}  (won't ask again for docker compose run in this project)${r}\n\n"
        say "All Redis tests pass on predis v7. Want me to open the PR?"
        break ;;
      3|"$(printf '\033')")
        printf '\033[2J\033[H'
        printf "${d}  ⎿  Interrupted · tell Claude what to do instead${r}\n\n"
        break ;;
    esac
  done
  stty sane 2>/dev/null
}

case "$id" in
  ` + blockedID + `) blocked ;;
  ` + idleID + `)
    printf '\033[2J\033[H'
    say "The image rebuilds composer deps every time: the cache layer is invalidated by a COPY . . before install." ;;
  *)
    printf '\033[2J\033[H'
    printf "${d}(nanny demo: a stand-in for claude)${r}\n\n" ;;
esac
state idle
if [ -n "$first" ]; then
  printf "${b}> %s${r}\n\n" "$first"
  say "On it. (In the demo nothing really runs; a real claude would start working here.)"
fi
while :; do
  printf "${d}────────────────────────────────────────${r}\n> "
  IFS= read -r line || exit 0
  line=$(printf '%s' "$line" | tr -d '\033\r' | sed 's/\[20[01]~//g')
  [ -n "$line" ] || continue
  state busy
  printf "\n"
  say "Got it: $line"
  printf "${d}✻ Working… (esc to interrupt)${r}\n"
  sleep 2
  printf "\033[1A\033[2K"
  say "Done. (In the demo nothing really runs.)"
  state idle
done
`

// stageAgents writes the stand-in claude, a restorable layout and some
// uncommitted work for the Changes view.
func stageAgents(dir, dev string) error {
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(fakeClaude), 0o755); err != nil {
		return err
	}
	acme := filepath.Join(dev, "acme-api")
	ops := filepath.Join(acme, ".claude", "worktrees", "OPS-88")
	infra := filepath.Join(dev, "infra")
	pane := func(cwd, id string) map[string]any {
		return map[string]any{"cwd": cwd, "agent": "claude", "session_id": id}
	}
	st := map[string]any{"active": -1, "spaces": []map[string]any{
		{"name": "acme-api", "paths": []string{acme}, "active": 0, "tabs": []map[string]any{
			{"label": "OPS-88 · redis v7", "panes": []map[string]any{pane(ops, blockedID)}}}},
		{"name": "infra", "paths": []string{infra}, "active": 0, "tabs": []map[string]any{
			{"label": "staging deploy", "panes": []map[string]any{pane(infra, idleID)}}}},
	}}
	b, _ := json.MarshalIndent(st, "", "  ")
	sd := filepath.Join(dir, ".state", "nanny")
	if err := os.MkdirAll(sd, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(sd, "workspace.json"), b, 0o644); err != nil {
		return err
	}
	// PAY-412: one commit on the branch plus uncommitted work
	pay := filepath.Join(acme, ".claude", "worktrees", "PAY-412")
	files := map[string]string{
		"src/Ledger/Entry.php": "<?php\n\nfinal class Entry\n{\n    public function __construct(\n        public readonly string $account,\n        public readonly int $amount,\n    ) {}\n\n    public function reverse(): self\n    {\n        return new self($this->account, -$this->amount);\n    }\n}\n",
	}
	for p, c := range files {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(pay, p)), 0o755)
		_ = os.WriteFile(filepath.Join(pay, p), []byte(c), 0o644)
	}
	_ = run(pay, "git", "add", ".")
	_ = run(pay, "git", "-c", "user.name=demo", "-c", "user.email=demo@example.com", "commit", "-q", "-m", "feat(ledger): reversible entries")
	_ = os.MkdirAll(filepath.Join(pay, "src", "Payments"), 0o755)
	_ = os.WriteFile(filepath.Join(pay, "src", "Payments", "RefundSplitPayment.php"), []byte(strings.Join([]string{
		"<?php", "", "final class RefundSplitPayment", "{",
		"    public function __invoke(Split $split, int $amount): void",
		"    {",
		"        foreach ($split->parts($amount) as $part) {",
		"            $this->gateway->refund($part->payment, $part->amount);",
		"            $this->ledger->add((new Entry($part->account, $part->amount))->reverse());",
		"        }",
		"    }", "}", ""}, "\n")), 0o644)
	_ = os.WriteFile(filepath.Join(pay, "README.md"), []byte("# acme-api\n\nPayments API.\n\n## Refunds\n\nSplit payments refund each part and book a reversing ledger entry.\n"), 0o644)
	return nil
}
