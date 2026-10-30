package claude

import "testing"

const boxedBash = `● I'll run the unit tests before touching the ledger.

╭──────────────────────────────────────────────────────────────────────╮
│ Bash command                                                         │
│                                                                      │
│   npm test -- --watch=false                                          │
│   Run the unit tests                                                 │
│                                                                      │
│ Do you want to proceed?                                              │
│ ❯ 1. Yes                                                             │
│   2. Yes, and don't ask again for npm test commands in               │
│   /home/me/dev/acme-api                                              │
│   3. No, and tell Claude what to do differently (esc)                │
╰──────────────────────────────────────────────────────────────────────╯
`

const ruledEdit = `● Now the use case.

────────────────────────────────────────────────────────────────────────
 Edit file
 ╭────────────────────────────────────────────────────────────────╮
 │ src/Payments/RefundSplitPayment.php                            │
 │                                                                │
 │ 12 -        return $this->ledger->refund($amount);             │
 │ 12 +        return $this->ledger->refundSplit($split, $amount);│
 ╰────────────────────────────────────────────────────────────────╯
 Do you want to make this edit to RefundSplitPayment.php?
   1. Yes
 ❯ 2. Yes, allow all edits during this session (shift+tab)
   3. No, and tell Claude what to do differently (esc)

 Esc to cancel
`

const twoOptions = `────────────────────────────────────────
 Create file
 notes/todo.md
 Do you want to create todo.md?
 ❯ 1. Yes
   2. No
`

// A numbered list in Claude's own answer has no ❯ cursor: not a prompt.
const answerList = `● Two ways to fix it:
  1. Cache the composer layer
  2. Split the Dockerfile in stages

>
  ? for shortcuts
`

func TestParsePromptBoxedBash(t *testing.T) {
	p := ParsePrompt(boxedBash)
	if p == nil {
		t.Fatal("no prompt found")
	}
	if p.Title != "Bash command" || p.Question != "Do you want to proceed?" {
		t.Errorf("title %q question %q", p.Title, p.Question)
	}
	if len(p.Detail) != 2 || p.Detail[0] != "npm test -- --watch=false" || p.Subject() != "npm test -- --watch=false" {
		t.Errorf("detail %q", p.Detail)
	}
	if len(p.Options) != 3 || p.Selected != 0 {
		t.Fatalf("options %+v selected %d", p.Options, p.Selected)
	}
	if want := "Yes, and don't ask again for npm test commands in /home/me/dev/acme-api"; p.Options[1].Label != want {
		t.Errorf("wrapped option label = %q", p.Options[1].Label)
	}
	if p.Yes().Key != "1" || p.Always().Key != "2" || p.No().Key != "3" {
		t.Errorf("yes/always/no = %v %v %v", p.Yes(), p.Always(), p.No())
	}
}

func TestParsePromptEditWithDiff(t *testing.T) {
	p := ParsePrompt(ruledEdit)
	if p == nil {
		t.Fatal("no prompt found")
	}
	if p.Title != "Edit file" || p.Subject() != "src/Payments/RefundSplitPayment.php" {
		t.Errorf("title %q subject %q", p.Title, p.Subject())
	}
	if p.Question != "Do you want to make this edit to RefundSplitPayment.php?" {
		t.Errorf("question %q", p.Question)
	}
	if p.Selected != 1 || p.Always() == nil || p.Always().Key != "2" {
		t.Errorf("selected %d always %v", p.Selected, p.Always())
	}
}

func TestParsePromptTwoOptions(t *testing.T) {
	p := ParsePrompt(twoOptions)
	if p == nil {
		t.Fatal("no prompt found")
	}
	if p.Title != "Create file" || p.Subject() != "notes/todo.md" {
		t.Errorf("title %q subject %q", p.Title, p.Subject())
	}
	if p.Always() != nil || p.Yes().Key != "1" || p.No().Key != "2" {
		t.Errorf("options %+v", p.Options)
	}
}

func TestParsePromptIgnoresAnswers(t *testing.T) {
	for name, s := range map[string]string{"list": answerList, "empty": "", "shell": "$ ls\nfoo bar\n$ "} {
		if p := ParsePrompt(s); p != nil {
			t.Errorf("%s: unexpected prompt %+v", name, p)
		}
	}
}

func TestParseTrustMenu(t *testing.T) {
	scr := `────────────────────────────────────────
 Accessing workspace:

 /tmp/realprobe

 Quick safety check: Is this a project you created or one you trust?

 Claude Code'll be able to read, edit, and execute files here.

 ❯ No, exit
   Yes, I trust this folder

 Enter to confirm · Esc to cancel
`
	p := ParsePrompt(scr)
	if p == nil || len(p.Options) != 2 || p.Selected != 0 || p.Title != "Accessing workspace:" {
		t.Fatalf("got %+v", p)
	}
	if got := p.Press(p.Index(p.Yes())); got != "\x1b[B\r" {
		t.Fatalf("press yes = %q", got)
	}
	if got := p.Press(p.Index(p.No())); got != "\r" {
		t.Fatalf("press no = %q", got)
	}
}
