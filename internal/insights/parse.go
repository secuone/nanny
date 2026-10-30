package insights

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/secuone/nanny/internal/claude"
)

// bytesRead counts what the parser read; the tests use it to see that only
// new bytes are read.
var bytesRead atomic.Int64

const (
	gapMax     = 5 * time.Minute
	pendingTTL = time.Hour
	cmdMax     = 300
	headLen    = 4 << 10
	maxURLs    = 20
)

type usageT struct {
	In     int64 `json:"input_tokens"`
	Out    int64 `json:"output_tokens"`
	CacheW int64 `json:"cache_creation_input_tokens"`
	CacheR int64 `json:"cache_read_input_tokens"`
	Cache  struct {
		W1h int64 `json:"ephemeral_1h_input_tokens"`
	} `json:"cache_creation"`
	Iterations []usageT `json:"iterations"`
}

type block struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	ToolUseID string `json:"tool_use_id"`
	IsError   bool   `json:"is_error"`
	Input     struct {
		Command string `json:"command"`
	} `json:"input"`
}

// content is message.content: a string (a typed prompt) or blocks.
type content []block

func (c *content) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || b[0] != '[' {
		return nil
	}
	return tolerant(json.Unmarshal(b, (*[]block)(c)))
}

// lineCount is the lines of a JSON string, without keeping it.
type lineCount int

func (n *lineCount) UnmarshalJSON(b []byte) error {
	if len(b) > 2 && b[0] == '"' {
		*n = lineCount(bytes.Count(b, []byte(`\n`)) + 1)
	}
	return nil
}

type toolResult struct {
	Type             string `json:"type"`
	Interrupted      bool   `json:"interrupted"`
	BackgroundTaskID string `json:"backgroundTaskId"`
	Stdout           string `json:"stdout"`
	Patch            []struct {
		Lines []string `json:"lines"`
	} `json:"structuredPatch"`
	Content lineCount `json:"content"`
}

// toolUseResult is an object for most tools and a plain string for the others.
type toolUseResult struct{ toolResult }

func (r *toolUseResult) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || b[0] != '{' {
		return nil
	}
	return tolerant(json.Unmarshal(b, &r.toolResult))
}

type rec struct {
	Type       string        `json:"type"`
	Subtype    string        `json:"subtype"`
	Timestamp  string        `json:"timestamp"`
	Cwd        string        `json:"cwd"`
	DurationMs int64         `json:"durationMs"`
	AITitle    string        `json:"aiTitle"`
	Denial     string        `json:"toolDenialKind"`
	Decision   *decision     `json:"permissionDecision"`
	Result     toolUseResult `json:"toolUseResult"`
	Message    *struct {
		ID      string  `json:"id"`
		Model   string  `json:"model"`
		Usage   *usageT `json:"usage"`
		Content content `json:"content"`
	} `json:"message"`
}

type decision struct {
	Decision string `json:"decision"`
	Source   string `json:"source"`
}

// tolerant drops type mismatches: a field that is sometimes a string and
// sometimes an object must not lose the rest of the line.
func tolerant(err error) error {
	var te *json.UnmarshalTypeError
	if errors.As(err, &te) {
		return nil
	}
	return err
}

var (
	commitRe = regexp.MustCompile(`(?m)^\[[^\]\n]+ [0-9a-f]{7,}\]`)
	// a quiet commit prints nothing: -q, -qm, -qam, --quiet
	quietRe = regexp.MustCompile(`git commit\b[^&;|\n]*\s(?:-[a-zA-Z]*q[a-zA-Z]*|--quiet)\b`)
	prURLRe = regexp.MustCompile(`https://github\.com/[^\s"'<>)]+/pull/\d+`)
)

var editTools = map[string]bool{"Edit": true, "MultiEdit": true, "Write": true, "NotebookEdit": true}

const (
	kindCommit      = "commit"
	kindQuietCommit = "commit-q"
	kindPR          = "pr"
)

// bashKind is what a command was for, from the whole command: the stored
// copy is cut short.
func bashKind(cmd string) string {
	switch {
	case strings.Contains(cmd, "gh pr create"):
		return kindPR
	case !strings.Contains(cmd, "git commit") || strings.Contains(cmd, "--dry-run"):
		return ""
	case quietRe.MatchString(cmd):
		return kindQuietCommit
	}
	return kindCommit
}

// toolKey groups MCP tools by server: mcp__github__x -> mcp__github.
func toolKey(name string) string {
	if rest, ok := strings.CutPrefix(name, "mcp__"); ok {
		srv, _, _ := strings.Cut(rest, "__")
		return "mcp__" + srv
	}
	return name
}

// parseFile reads path from f.Off, up to its last complete line.
func parseFile(path string, f *File) error {
	fh, err := os.Open(path)
	if err != nil {
		return err
	}
	defer fh.Close()
	if _, err := fh.Seek(f.Off, io.SeekStart); err != nil {
		return err
	}
	r := bufio.NewReaderSize(fh, 256<<10)
	for {
		l, err := r.ReadBytes('\n')
		if err != nil {
			break // a partial last line is read again once it is complete
		}
		f.Off += int64(len(l))
		bytesRead.Add(int64(len(l)))
		f.line(l)
	}
	f.prune()
	return nil
}

func (f *File) prune() {
	for id, p := range f.Pending {
		if f.LastTS-p.At > pendingTTL.Milliseconds() {
			delete(f.Pending, id)
		}
	}
}

func (f *File) day(key string) *Day {
	d := f.Days[key]
	if d == nil {
		d = &Day{Tokens: map[string]claude.Tokens{}, Tools: map[string]*Tool{}}
		f.Days[key] = d
	}
	return d
}

func (d *Day) tool(name string) *Tool {
	t := d.Tools[name]
	if t == nil {
		t = &Tool{}
		d.Tools[name] = t
	}
	return t
}

func parseTS(s string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339Nano, s)
	return t, err == nil
}

// kinds that matter; every other line is dropped before decoding it
var kinds = [][]byte{[]byte(`"type":"assistant"`), []byte(`"type":"user"`), []byte(`"type":"system"`), []byte(`"type":"ai-title"`)}

func relevant(l []byte) (user bool, ok bool) {
	h := l[:min(len(l), headLen)]
	for i, k := range kinds {
		if bytes.Contains(h, k) {
			return i == 1, true
		}
	}
	return false, false
}

var tsKey = []byte(`"timestamp":"`)

// headTime is the timestamp of a line, read from its first bytes.
func headTime(l []byte) (time.Time, bool) {
	h := l[:min(len(l), headLen)]
	i := bytes.Index(h, tsKey)
	if i < 0 {
		return time.Time{}, false
	}
	v, _, _ := bytes.Cut(h[i+len(tsKey):], []byte(`"`))
	return parseTS(string(v))
}

// line adds one transcript line to the tally.
func (f *File) line(l []byte) {
	user, ok := relevant(l)
	if !ok {
		return
	}
	if user && !bytes.Contains(l, []byte(`"tool_result"`)) { // a typed prompt: only its time and an interruption matter
		if t, ok := headTime(l); ok {
			d := f.day(t.In(time.Local).Format("2006-01-02"))
			f.touch(d, t)
			if bytes.Contains(l, []byte(`[Request interrupted by user`)) {
				d.Interrupts++
			}
		}
		return
	}
	var rc rec
	if err := tolerant(json.Unmarshal(l, &rc)); err != nil {
		return
	}
	if rc.Type == "ai-title" {
		if rc.AITitle != "" {
			f.Title = rc.AITitle
		}
		return
	}
	t, ok := parseTS(rc.Timestamp)
	if !ok {
		return
	}
	if f.Cwd == "" {
		f.Cwd = rc.Cwd
	}
	d := f.day(t.In(time.Local).Format("2006-01-02"))
	f.touch(d, t)
	switch rc.Type {
	case "assistant":
		f.assistant(d, &rc, t)
	case "user":
		f.user(d, &rc, l, t)
	case "system":
		switch rc.Subtype {
		case "turn_duration":
			f.HasTD = true
			d.ActiveMs += rc.DurationMs
			d.Turns++
		case "api_error":
			d.Retries++
		case "compact_boundary":
			d.Compactions++
		}
	}
}

// touch notes a moment of activity: first and last of the day, and the gap
// since the previous one when it is short.
func (f *File) touch(d *Day, t time.Time) {
	ms := t.UnixMilli()
	if d.First == 0 || ms < d.First {
		d.First = ms
	}
	d.Last = max(d.Last, ms)
	if f.LastTS > 0 && ms > f.LastTS && ms-f.LastTS < gapMax.Milliseconds() {
		d.GapMs += ms - f.LastTS
	}
	f.LastTS = max(f.LastTS, ms)
}

func (f *File) assistant(d *Day, rc *rec, t time.Time) {
	m := rc.Message
	if m == nil {
		return
	}
	if u := m.Usage; u != nil && m.Model != "" && m.Model != "<synthetic>" {
		tk := tokensOf(u)
		if tk.Sum() == 0 {
			for i := range u.Iterations {
				tk = addTok(tk, tokensOf(&u.Iterations[i]))
			}
		}
		if tk.Sum() > 0 {
			if m.ID != "" && m.ID == f.LastID && m.Model == f.LastModel { // one line per content block, usage repeated
				ld := f.day(f.LastDay)
				ld.Tokens[m.Model] = subTok(ld.Tokens[m.Model], f.LastTok)
			}
			d.Tokens[m.Model] = addTok(d.Tokens[m.Model], tk)
			f.LastID, f.LastModel, f.LastTok, f.LastDay = m.ID, m.Model, tk, t.In(time.Local).Format("2006-01-02")
		}
	}
	for _, b := range m.Content {
		if b.Type != "tool_use" || b.ID == "" {
			continue
		}
		d.tool(toolKey(b.Name)).Calls++
		cmd, kind := "", ""
		if b.Name == "Bash" {
			cmd, kind = b.Input.Command, bashKind(b.Input.Command)
			if len(cmd) > cmdMax {
				cmd = cmd[:cmdMax]
			}
		}
		f.Pending[b.ID] = pend{b.Name, t.UnixMilli(), cmd, kind, t.In(time.Local).Format("2006-01-02")}
	}
}

func (f *File) user(d *Day, rc *rec, l []byte, t time.Time) {
	f.permission(d, rc)
	interrupted := rc.Result.Interrupted || bytes.Contains(l, []byte(`[Request interrupted by user`))
	if rc.Message != nil {
		for _, b := range rc.Message.Content {
			if b.Type != "tool_result" {
				continue
			}
			p, known := f.Pending[b.ToolUseID]
			delete(f.Pending, b.ToolUseID)
			pd := d
			if known && p.Day != "" {
				pd = f.day(p.Day)
			}
			f.result(pd, rc, b, p, known, t)
		}
	}
	if interrupted {
		d.Interrupts++
	}
}

// permission files how a tool request was settled.
func (f *File) permission(d *Day, rc *rec) {
	switch {
	case rc.Denial == "user-rejected":
		d.Perm.Asked++
		d.Perm.UserNo++
	case rc.Denial == "automode-blocked" || rc.Denial == "permission-rule":
		d.Perm.Blocked++
	case rc.Decision == nil:
	case rc.Decision.Decision == "accept" && strings.HasPrefix(rc.Decision.Source, "user"):
		d.Perm.Asked++
		d.Perm.UserOK++
	case rc.Decision.Decision == "accept":
		d.Perm.Auto++
	default:
		d.Perm.Blocked++
	}
}

// result settles one tool_result against the tool_use it answers.
func (f *File) result(d *Day, rc *rec, b block, p pend, known bool, t time.Time) {
	denied := rc.Denial != ""
	if !known {
		return
	}
	tl := d.tool(toolKey(p.Name))
	switch {
	case denied:
		tl.Denied++
	case b.IsError:
		tl.Errors++
		f.errRun(d, toolKey(p.Name))
	default:
		f.ErrTool, f.ErrRun = "", 0
	}
	if !denied && rc.Result.BackgroundTaskID == "" && t.UnixMilli() >= p.At {
		ms := t.UnixMilli() - p.At
		tl.Timed++
		tl.DurMs += ms
		tl.MaxMs = max(tl.MaxMs, ms)
	}
	if editTools[p.Name] {
		f.edit(d, rc, b, denied)
	}
	if denied || b.IsError {
		return
	}
	switch {
	case p.Kind == kindCommit && commitRe.MatchString(rc.Result.Stdout),
		p.Kind == kindQuietCommit && !strings.Contains(rc.Result.Stdout, "nothing to commit"):
		d.Commits++
	case p.Kind == kindPR:
		if u := prURLRe.FindString(rc.Result.Stdout); u != "" {
			d.PRs++
			if len(d.PRURLs) < maxURLs {
				d.PRURLs = append(d.PRURLs, u)
			}
		}
	case p.Name == "mcp__github__create_pull_request":
		d.PRs++
	}
}

// errRun counts failures in a row of one tool: three is a loop.
func (f *File) errRun(d *Day, tool string) {
	if f.ErrTool != tool {
		f.ErrTool, f.ErrRun = tool, 0
	}
	if f.ErrRun++; f.ErrRun == 3 {
		d.ErrorLoops++
	}
}

func (f *File) edit(d *Day, rc *rec, b block, denied bool) {
	switch {
	case denied:
		d.Edits.Rejected++
	case !b.IsError:
		d.Edits.OK++
		for _, h := range rc.Result.Patch {
			for _, ln := range h.Lines {
				switch {
				case strings.HasPrefix(ln, "+"):
					d.Edits.Added++
				case strings.HasPrefix(ln, "-"):
					d.Edits.Removed++
				}
			}
		}
		if rc.Result.Type == "create" {
			d.Edits.Added += int(rc.Result.Content)
		}
	}
}

func tokensOf(u *usageT) claude.Tokens {
	return claude.Tokens{In: u.In, Out: u.Out, CacheW: u.CacheW, CacheR: u.CacheR, CacheW1h: u.Cache.W1h}
}
