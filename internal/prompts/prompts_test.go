package prompts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T, now time.Time) string {
	t.Helper()
	day := 24 * time.Hour
	type e struct {
		display, project string
		ago              time.Duration
	}
	es := []e{
		{"Run the  tests and\nfix whatever fails", "/dev/api", 40 * day},
		{"run the tests and fix whatever fails.", "/dev/api", 30 * day},
		{"Run the tests and fix whatever fails", "/dev/web", 20 * day},
		{"review PAY-412 and plan the change", "/dev/api", 2 * day},
		{"Review PAY-77 and plan the change", "/dev/api", 1 * day},
		{"write the changelog entry for this", "/dev/web", 90 * day},
		{"write the changelog entry for this", "/dev/web", 89 * day},
		{"write the changelog entry for this", "/dev/web", 88 * day},
		{"only once, never repeated here", "/dev/api", day},
		{"/clear", "/dev/api", day},
		{"/clear ", "/dev/api", day},
		{"/mcp", "/dev/api", day},
		{"sigue", "/dev/api", day},
		{"sigue", "/dev/api", day},
		{"[Pasted text #1 +4 lines] and fix it", "/dev/api", day},
		{"[Pasted text #1 +4 lines] and fix it", "/dev/api", day},
		{"!git status && git log", "/dev/api", day},
		{"!git status && git log", "/dev/api", day},
	}
	var lines []string
	for _, x := range es {
		b, _ := json.Marshal(map[string]any{"display": x.display, "project": x.project, "timestamp": now.Add(-x.ago).UnixMilli(), "pastedContents": map[string]any{}})
		lines = append(lines, string(b))
	}
	lines = append(lines, "not json")
	p := filepath.Join(t.TempDir(), "history.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestMine(t *testing.T) {
	now := time.Now()
	p := fixture(t, now)
	h, err := Mine(p)
	if err != nil {
		t.Fatal(err)
	}
	if h.Entries != 18 {
		t.Errorf("entries %d", h.Entries)
	}
	var got []string
	for _, u := range h.Prompts {
		got = append(got, u.Text)
	}
	// count first; among 3 uses the recent ones win; the 2-use group keeps its newest wording
	want := []string{"Run the tests and fix whatever fails", "write the changelog entry for this", "Review PAY-77 and plan the change"}
	if len(got) != 3 {
		t.Fatalf("prompts %q", got)
	}
	if got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("ranking %q", got)
	}
	if u := h.Prompts[0]; u.Count != 3 || u.Project != "/dev/api" || u.Projects["/dev/web"] != 1 {
		t.Errorf("top prompt %+v", u)
	}
	if len(h.Commands) != 2 || h.Commands[0].Name != "/clear" || h.Commands[0].Count != 2 {
		t.Errorf("commands %+v", h.Commands)
	}
	// same size and mtime: served from the cache
	if h2, _ := Mine(p); len(h2.Prompts) != len(h.Prompts) || &h2.Prompts[0] != &h.Prompts[0] {
		t.Error("second read wasn't cached")
	}
}

func TestKeyGroupsNearDuplicates(t *testing.T) {
	if Key("Review PAY-412, please!") != Key("review pay-7 please") {
		t.Errorf("%q vs %q", Key("Review PAY-412, please!"), Key("review pay-7 please"))
	}
	if Key("run the tests") == Key("run the linters") {
		t.Error("different prompts grouped")
	}
}

func TestPlaceholders(t *testing.T) {
	text := "Read {ticket} and fix {file}; then rerun {ticket}. Leave {} and {\"json\": 1} alone."
	if ph := Placeholders(text); strings.Join(ph, ",") != "ticket,file" {
		t.Fatalf("placeholders %q", ph)
	}
	got := Fill(text, map[string]string{"ticket": "PAY-412", "file": ""})
	if got != "Read PAY-412 and fix {file}; then rerun PAY-412. Leave {} and {\"json\": 1} alone." {
		t.Errorf("fill %q", got)
	}
}
