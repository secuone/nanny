package insights

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/secuone/nanny/internal/claude"
)

var update = flag.Bool("update", false, "rewrite the golden files")

const (
	sess    = "3921019f-abd6-4018-888e-465c45e9971c"
	opus    = "claude-opus-5-5"
	sonnet  = "claude-sonnet-5-5"
	project = "-home-dev-acme-api"
	day     = "2026-10-27"
)

var epoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func zone(t *testing.T, loc *time.Location) {
	t.Helper()
	old := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = old })
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	b, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// projects builds ~/.claude/projects with the session and its subagent.
func projects(t *testing.T) (root, main string) {
	t.Helper()
	root = filepath.Join(t.TempDir(), "projects")
	main = filepath.Join(root, project, sess+".jsonl")
	copyFile(t, "testdata/session.jsonl", main)
	copyFile(t, "testdata/subagent.jsonl", filepath.Join(root, project, sess, "subagents", "agent-a1.jsonl"))
	return root, main
}

func index(t *testing.T) (*Snapshot, Agg) {
	t.Helper()
	zone(t, time.UTC)
	root, _ := projects(t)
	s, err := Refresh(root, epoch, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	d := time.Date(2026, 10, 27, 12, 0, 0, 0, time.Local)
	return s, s.Range(d, d, Filter{Expensive: 0.0001})
}

// A message logged once per content block counts once.
func TestDedupByMessageID(t *testing.T) {
	_, a := index(t)
	var op, so claude.Tokens
	for _, m := range a.Models {
		switch m.Model {
		case opus:
			op = m.Tokens
		case sonnet:
			so = m.Tokens
		}
	}
	// 8 opus messages in the session (msg1 is on two lines) and 3 in the subagent
	if op.In != 910+300 || op.Out != 455+150 {
		t.Errorf("opus tokens: %+v", op)
	}
	if so.In != 100 || so.Out != 50 {
		t.Errorf("sonnet tokens: %+v", so)
	}
}

func TestCostByDayAndModel(t *testing.T) {
	_, a := index(t)
	day := a.Days[0]
	want := 0.0
	for m, tk := range day.Models {
		want += claude.CostOf(m, tk)
	}
	if want == 0 || math.Abs(day.Cost-want) > 1e-9 || math.Abs(a.Total.Cost-want) > 1e-9 {
		t.Errorf("cost %v, total %v, want %v", day.Cost, a.Total.Cost, want)
	}
	if day.ModelCost[sonnet] <= 0 || day.ModelCost[opus] <= day.ModelCost[sonnet] {
		t.Errorf("by model: %v", day.ModelCost)
	}
}

func TestSessionsAndActiveTime(t *testing.T) {
	_, a := index(t)
	if a.Total.Sessions != 1 || len(a.Sessions) != 1 {
		t.Fatalf("the subagent belongs to its session: %+v", a.Sessions)
	}
	ss := a.Sessions[0]
	if ss.Title != "fix the api" || ss.Project != "acme-api" || !strings.HasSuffix(ss.Path, sess+".jsonl") || strings.Contains(ss.Path, "subagents") {
		t.Errorf("session: %+v", ss)
	}
	if a.Total.Turns != 2 || a.Total.ActiveMs != 180000 {
		t.Errorf("turn_duration: %d turns, %d ms", a.Total.Turns, a.Total.ActiveMs)
	}
	if len(a.Expensive) != 1 {
		t.Errorf("expensive: %v", a.Expensive)
	}
}

// Without turn_duration the time is the sum of the gaps under 5 minutes.
func TestActiveTimeFromGaps(t *testing.T) {
	zone(t, time.UTC)
	root := filepath.Join(t.TempDir(), "projects")
	copyFile(t, "testdata/subagent.jsonl", filepath.Join(root, project, "old.jsonl"))
	s, _ := Refresh(root, epoch, nil, nil)
	d := time.Date(2026, 10, 27, 12, 0, 0, 0, time.Local)
	a := s.Range(d, d, Filter{})
	if a.Total.ActiveMs != 60000 { // 09:30→09:31; 09:31→09:40 is a pause
		t.Errorf("gaps: %d ms", a.Total.ActiveMs)
	}
}

func TestToolsDurationAndErrors(t *testing.T) {
	_, a := index(t)
	tools := map[string]Tool{}
	for _, tl := range a.Tools {
		tools[tl.Name] = tl.Tool
	}
	b := tools["Bash"]
	if b.Calls != 9 || b.Errors != 3 || b.Denied != 2 || b.Timed != 6 || b.DurMs != 30000 || b.MaxMs != 10000 {
		t.Errorf("Bash: %+v", b) // the background call and the denied ones are not timed
	}
	if e := tools["Edit"]; e.Calls != 2 || e.Denied != 1 || e.Timed != 1 {
		t.Errorf("Edit: %+v", e)
	}
	if g, ok := tools["mcp__github"]; !ok || g.Calls != 1 {
		t.Errorf("MCP tools group by server: %v", tools)
	}
	if b.AvgMs() != 5000 || math.Abs(b.ErrRate()-3.0/9) > 1e-9 {
		t.Errorf("avg %d, rate %v", b.AvgMs(), b.ErrRate())
	}
	if a.ErrorLoops != 1 {
		t.Errorf("three failures in a row are a loop: %d", a.ErrorLoops)
	}
}

// The five real shapes: accepted by config, accepted by the user, rejected by
// the user (which arrives with source config), blocked by auto mode, blocked by a rule.
func TestPermissionClassification(t *testing.T) {
	_, a := index(t)
	if want := (Perm{Auto: 2, Asked: 2, UserOK: 1, UserNo: 1, Blocked: 2}); a.Perm != want {
		t.Errorf("permissions %+v, want %+v", a.Perm, want)
	}
}

func TestEditsLines(t *testing.T) {
	_, a := index(t)
	// the Edit patch adds 2 and removes 1; the Write creates 3 lines; one edit was rejected
	if want := (Edits{OK: 2, Rejected: 1, Added: 5, Removed: 1}); a.Edits != want {
		t.Errorf("edits %+v, want %+v", a.Edits, want)
	}
	if a.Total.LinesAdded != 5 || a.Total.LinesRemoved != 1 {
		t.Errorf("lines: %+v", a.Total)
	}
}

func TestCommitAndPRDetection(t *testing.T) {
	s, a := index(t)
	if a.Total.Commits != 2 || a.Total.PRs != 2 {
		t.Errorf("commits %d, PRs %d", a.Total.Commits, a.Total.PRs)
	}
	for _, f := range s.Files {
		if f.Sub {
			continue
		}
		if d := f.Days[day]; len(d.PRURLs) != 1 || d.PRURLs[0] != "https://github.com/acme/api/pull/42" {
			t.Errorf("urls: %v", d.PRURLs)
		}
	}
}

func TestFriction(t *testing.T) {
	_, a := index(t)
	if a.Retries != 1 || a.Interrupts != 1 || a.Compactions != 1 {
		t.Errorf("friction: retries %d, interrupts %d, compactions %d", a.Retries, a.Interrupts, a.Compactions)
	}
}

// The day is the local one: the same lines fall on another date in another zone.
func TestTimeZone(t *testing.T) {
	zone(t, time.FixedZone("west", -10*3600))
	root, _ := projects(t)
	s, _ := Refresh(root, epoch, nil, nil)
	f := s.Files[filepath.Join(root, project, sess+".jsonl")]
	if _, ok := f.Days["2026-10-26"]; !ok || len(f.Days) != 1 {
		t.Errorf("days in UTC-10: %v", keys(f.Days))
	}
	d := time.Date(2026, 10, 26, 12, 0, 0, 0, time.Local)
	if a := s.Range(d, d, Filter{}); a.Total.Sessions != 1 || a.Total.Cost == 0 {
		t.Errorf("range in UTC-10: %+v", a.Total)
	}
}

func keys(m map[string]*Day) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func appendTo(t *testing.T, path, text string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := io.WriteString(f, text); err != nil {
		t.Fatal(err)
	}
}

const turnLine = `{"type":"system","subtype":"turn_duration","durationMs":1000,"timestamp":"2026-10-27T10:00:00.000Z","sessionId":"x"}` + "\n"

// Appended lines are the only bytes read; a half-written line waits; a
// transcript that shrinks is read again from the start.
func TestIncrementalRead(t *testing.T) {
	zone(t, time.UTC)
	root, main := projects(t)
	st, _ := os.Stat(main)
	sub, _ := os.Stat(filepath.Join(root, project, sess, "subagents", "agent-a1.jsonl"))

	before := bytesRead.Load()
	s1, err := Refresh(root, epoch, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := bytesRead.Load() - before; got != st.Size()+sub.Size() {
		t.Errorf("first read: %d bytes, want %d", got, st.Size()+sub.Size())
	}

	before = bytesRead.Load()
	s2, _ := Refresh(root, epoch, s1, nil)
	if got := bytesRead.Load() - before; got != 0 || s2.Changed() {
		t.Errorf("nothing changed, but %d bytes read", got)
	}

	appendTo(t, main, turnLine)
	before = bytesRead.Load()
	s3, _ := Refresh(root, epoch, s2, nil)
	if got := bytesRead.Load() - before; got != int64(len(turnLine)) || !s3.Changed() {
		t.Errorf("appended %d bytes, read %d", len(turnLine), got)
	}
	d := time.Date(2026, 10, 27, 12, 0, 0, 0, time.Local)
	if a := s3.Range(d, d, Filter{}); a.Total.Turns != 3 || a.Total.ActiveMs != 181000 {
		t.Errorf("after appending: %+v", a.Total)
	}
	if a := s1.Range(d, d, Filter{}); a.Total.Turns != 2 {
		t.Errorf("the previous snapshot must not change: %+v", a.Total)
	}

	half := turnLine[:40]
	appendTo(t, main, half)
	s4, _ := Refresh(root, epoch, s3, nil)
	if a := s4.Range(d, d, Filter{}); a.Total.Turns != 3 {
		t.Errorf("a partial line must wait: %+v", a.Total)
	}
	appendTo(t, main, turnLine[40:])
	before = bytesRead.Load()
	s5, _ := Refresh(root, epoch, s4, nil)
	if a := s5.Range(d, d, Filter{}); a.Total.Turns != 4 {
		t.Errorf("the completed line counts: %+v", a.Total)
	}
	if got := bytesRead.Load() - before; got != int64(len(turnLine)) {
		t.Errorf("read %d bytes, the line is %d", got, len(turnLine))
	}

	b, _ := os.ReadFile(main)
	short := b[:bytes.IndexByte(b, '\n')+1]
	if err := os.WriteFile(main, short, 0o644); err != nil {
		t.Fatal(err)
	}
	before = bytesRead.Load()
	s6, _ := Refresh(root, epoch, s5, nil)
	if got := bytesRead.Load() - before; got != int64(len(short)) {
		t.Errorf("a shorter file is read from the start: %d of %d", got, len(short))
	}
	if a := s6.Range(d, d, Filter{}); a.Total.Turns != 0 || a.Total.Sessions != 1 {
		t.Errorf("after shrinking: %+v", a.Total)
	}
}

func TestCachePersists(t *testing.T) {
	zone(t, time.UTC)
	root, main := projects(t)
	cache := filepath.Join(t.TempDir(), "insights", "v1.gob")
	s1, err := Update(root, cache, epoch, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cache); err != nil {
		t.Fatalf("no cache file: %v", err)
	}
	before := bytesRead.Load()
	s2, err := Update(root, cache, epoch, nil, nil) // a new process: loads the file
	if err != nil {
		t.Fatal(err)
	}
	if got := bytesRead.Load() - before; got != 0 {
		t.Errorf("a loaded cache should read nothing, read %d bytes", got)
	}
	d := time.Date(2026, 10, 27, 12, 0, 0, 0, time.Local)
	a1, a2 := s1.Range(d, d, Filter{}), s2.Range(d, d, Filter{})
	if a1.Total != a2.Total || a2.Total.Sessions != 1 {
		t.Errorf("cache differs: %+v / %+v", a1.Total, a2.Total)
	}
	appendTo(t, main, turnLine)
	before = bytesRead.Load()
	if _, err := Update(root, cache, epoch, nil, nil); err != nil || bytesRead.Load()-before != int64(len(turnLine)) {
		t.Errorf("only the new line should be read: %v", err)
	}
	_ = os.WriteFile(cache, []byte("not a gob"), 0o644)
	if s, _ := Load(cache); s != nil {
		t.Error("a broken cache is no cache")
	}
	if err := Save(cache, &Snapshot{Version: parserVersion + 1, Files: map[string]*File{}}); err != nil {
		t.Fatal(err)
	}
	if s, _ := Load(cache); s != nil {
		t.Error("a cache from another parser version is thrown away")
	}
}

func TestOnlyRecentFiles(t *testing.T) {
	zone(t, time.UTC)
	root, main := projects(t)
	old := time.Now().AddDate(0, 0, -200)
	if err := os.Chtimes(main, old, old); err != nil {
		t.Fatal(err)
	}
	s, _ := Refresh(root, time.Now().AddDate(0, 0, -90), nil, nil)
	if _, ok := s.Files[main]; ok || len(s.Files) != 1 {
		t.Errorf("files older than the history are not read: %d files", len(s.Files))
	}
}

func TestScopeFilter(t *testing.T) {
	s, _ := index(t)
	d := time.Date(2026, 10, 27, 12, 0, 0, 0, time.Local)
	if a := s.Range(d, d, Filter{Keep: func(cwd string) bool { return strings.HasSuffix(cwd, "other") }}); !a.Empty() {
		t.Errorf("nothing is in scope: %+v", a.Total)
	}
	a := s.Range(d.AddDate(0, 0, -2), d, Filter{Project: func(string) string { return "p" }})
	if len(a.Days) != 3 || a.Projects[0].Name != "p" || a.Days[2].Cost == 0 || a.Days[0].Cost != 0 {
		t.Errorf("days are dense: %+v", a.Days)
	}
}

func TestWeekRange(t *testing.T) {
	from, to, err := WeekRange("2026-W44")
	if err != nil || from.Format(dayFmt) != "2026-10-26" || to.Format(dayFmt) != "2026-11-01" {
		t.Errorf("2026-W44: %v %v %v", from, to, err)
	}
	if got := WeekOf(time.Date(2026, 10, 27, 0, 0, 0, 0, time.Local)); got != "2026-W44" {
		t.Errorf("week of: %s", got)
	}
	for _, bad := range []string{"", "2026-44", "2026-W00", "x"} {
		if _, _, err := WeekRange(bad); err == nil {
			t.Errorf("%q should not parse", bad)
		}
	}
}

func TestWeekJSONGolden(t *testing.T) {
	s, _ := index(t)
	now := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	got, err := Week(s, "2026-W44", Filter{Expensive: 0.0001}, now)
	if err != nil {
		t.Fatal(err)
	}
	golden := "testdata/week.golden.json"
	if *update {
		if err := os.WriteFile(golden, append(got, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(want)) != string(got) {
		t.Errorf("the week JSON changed (go test -update):\n%s", got)
	}
	checkSorted(t, got)
	if !strings.Contains(string(got), `"generated_at": "2026-10-30T09:00:00Z"`) {
		t.Error("no generated_at")
	}
}

// checkSorted walks the JSON and fails when an object's keys are out of order.
func checkSorted(t *testing.T, b []byte) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(b))
	var walk func(path string)
	walk = func(path string) {
		tok, err := dec.Token()
		if err != nil {
			return
		}
		switch tok {
		case json.Delim('{'):
			prev := ""
			for dec.More() {
				k, _ := dec.Token()
				key := k.(string)
				if key < prev {
					t.Errorf("%s: %q after %q", path, key, prev)
				}
				prev = key
				walk(path + "." + key)
			}
			_, _ = dec.Token()
		case json.Delim('['):
			for dec.More() {
				walk(path + "[]")
			}
			_, _ = dec.Token()
		}
	}
	walk("$")
}

func TestBashKind(t *testing.T) {
	for cmd, want := range map[string]string{
		"git commit -m x":                  kindCommit,
		`git add . && git commit -qam "x"`: kindQuietCommit,
		"git commit --quiet -F msg":        kindQuietCommit,
		"git commit --dry-run":             "",
		"git status && git log":            "",
		"gh pr create --fill":              kindPR,
		"git commit -m q && echo -q":       kindCommit,
		"git -C x commit -m y":             "",
	} {
		if got := bashKind(cmd); got != want {
			t.Errorf("%q: %q, want %q", cmd, got, want)
		}
	}
}
