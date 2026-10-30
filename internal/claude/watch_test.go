package claude

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// wait is what the watcher saw once it says something changed.
func wait(t *testing.T, w *Watcher, want string) map[string]bool {
	t.Helper()
	seen := map[string]bool{}
	deadline := time.After(time.Second)
	for !seen[want] {
		select {
		case <-w.C:
			ps, _ := w.Take()
			for p := range ps {
				seen[p] = true
			}
		case <-deadline:
			t.Fatalf("no event for %s within a second; saw %v", want, seen)
		}
	}
	return seen
}

func appendLine(t *testing.T, p, line string) {
	t.Helper()
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(line + "\n")
	f.Close()
}

// Transcripts, new projects, new session folders and their subagents are all
// seen, each one as soon as it is written; the rest of ~/.claude is ignored.
func TestWatch(t *testing.T) {
	demoHome(t)
	w, err := Watch()
	if err != nil {
		t.Skip("no inotify: ", err)
	}
	defer w.Close()
	ss := NewScanner().Scan()
	appendLine(t, ss[0].Path, `{"type":"user"}`)
	wait(t, w, ss[0].Path)

	proj := Dir("projects", "-new-project")
	os.MkdirAll(proj, 0o755)
	wait(t, w, proj)
	tr := filepath.Join(proj, "s1.jsonl")
	appendLine(t, tr, `{"type":"user"}`)
	wait(t, w, tr)
	sub := filepath.Join(proj, "s1", "subagents")
	os.MkdirAll(sub, 0o755)
	time.Sleep(50 * time.Millisecond) // the folder is watched once its creation is read
	agent := filepath.Join(sub, "agent-a1.jsonl")
	appendLine(t, agent, `{"type":"assistant"}`)
	wait(t, w, agent)
	if SessionOf(agent) != tr || SessionOf(tr) != tr || SessionOf(filepath.Join(proj, "s1")) != tr {
		t.Errorf("SessionOf: %q %q", SessionOf(agent), SessionOf(tr))
	}

	appendLine(t, Dir("todos.json"), "{}")
	appendLine(t, Dir("history.jsonl"), `{"display":"hi","sessionId":"s1"}`)
	if seen := wait(t, w, Dir("history.jsonl")); seen[Dir("todos.json")] {
		t.Error("the rest of ~/.claude should be ignored")
	}
}

// A scan of what changed sees the change and keeps the rest as it was.
func TestScanChanged(t *testing.T) {
	demoHome(t)
	sc := NewScanner()
	before := sc.Scan()
	p := before[len(before)-1].Path
	appendLine(t, p, `{"type":"user","message":{"role":"user","content":"more"}}`)
	after := sc.ScanChanged(map[string]bool{p: true})
	full := NewScanner().Scan()
	if len(after) != len(full) {
		t.Fatalf("%d sessions, a full scan has %d", len(after), len(full))
	}
	for i := range full {
		if after[i].ID != full[i].ID || after[i].Size != full[i].Size || !after[i].Modified.Equal(full[i].Modified) {
			t.Errorf("%s: %d vs %d", full[i].ID, after[i].Size, full[i].Size)
		}
	}
	os.Remove(p)
	if gone := sc.ScanChanged(map[string]bool{p: true}); len(gone) != len(full)-1 {
		t.Errorf("a removed transcript leaves: %d sessions", len(gone))
	}
}
