// Package claude reads Claude Code's on-disk state (~/.claude): transcripts,
// live processes and subagents. Transcripts can be hundreds of MB, so only the
// tail (and a small head) of each file is parsed, cached by (size, mtime). Token
// usage (usage.go) reads a file once, then only the bytes appended since.
package claude

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var home, _ = os.UserHomeDir()

// SetHome points the package at another home directory (tests, demo).
func SetHome(h string) { home = h }

func Dir(p ...string) string { return filepath.Join(append([]string{home, ".claude"}, p...)...) }

type Session struct {
	ID          string
	Path        string // transcript file
	Cwd         string
	Branch      string
	Title       string // best title: agent-name > ai-title > first prompt
	Name        string // agent-name
	AITitle     string
	FirstPrompt string
	LastPrompt  string
	Modified    time.Time
	Size        int64
	Model       string
	Kind        string // "bg" for background sessions
	Entrypoint  string
	Version     string
	Worktree    string // worktree name when the cwd is a linked worktree
	PRs         []string
	Cost        float64
	Messages    int
	LinesAdd    int
	LinesDel    int
	Subagents   int
	Usage       *Usage // tokens and context; read for live sessions and today's
	Live        *Proc
}

// Bot is true for automated sessions (pr-loop caches, headless runs) that clutter the explorer.
func (s *Session) Bot() bool {
	return strings.Contains(s.Cwd, "/.cache/") || (s.Entrypoint == "sdk-cli" && s.AITitle == "" && s.Name == "")
}

// Haystack is the text fuzzy filters match against.
func (s *Session) Haystack() string {
	cwd := s.Cwd
	if strings.HasPrefix(cwd, home) {
		cwd = "~" + cwd[len(home):]
	}
	return strings.Join([]string{s.Title, s.AITitle, s.Name, cwd, s.Branch, s.Worktree, s.FirstPrompt, s.ID}, " ")
}

// ResumeCwd is where `claude --resume` must run (transcripts are keyed by cwd).
func (s *Session) ResumeCwd() string {
	if s.Cwd != "" {
		if _, err := os.Stat(s.Cwd); err == nil {
			return s.Cwd
		}
	}
	return home
}

type Proc struct {
	Pid        int    `json:"pid"`
	SessionID  string `json:"sessionId"`
	Cwd        string `json:"cwd"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Entrypoint string `json:"entrypoint"`
	Status     string `json:"status"` // busy | idle
	Version    string `json:"version"`
	ProcStart  string `json:"procStart"`
	StartedAt  int64  `json:"startedAt"`
	UpdatedAt  int64  `json:"updatedAt"`
}

func (p Proc) Started() time.Time { return time.UnixMilli(p.StartedAt) }

// LiveProcs lists running Claude Code processes, verified against /proc to avoid pid reuse.
func LiveProcs() []Proc {
	files, _ := filepath.Glob(Dir("sessions", "*.json"))
	var out []Proc
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var p Proc
		if json.Unmarshal(b, &p) != nil || p.Pid == 0 || syscall.Kill(p.Pid, 0) != nil {
			continue
		}
		if p.ProcStart != "" {
			if st, err := os.ReadFile("/proc/" + strconv.Itoa(p.Pid) + "/stat"); err == nil {
				// field 22 (starttime) counted after the ")" that closes comm
				if i := bytes.LastIndexByte(st, ')'); i > 0 {
					f := strings.Fields(string(st[i+1:]))
					if len(f) > 19 && f[19] != p.ProcStart {
						continue
					}
				}
			}
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt > out[j].StartedAt })
	return out
}

// ---------------------------------------------------------------- scanning

type cacheKey struct {
	size  int64
	mtime int64
}

type Scanner struct {
	mu      sync.Mutex
	cache   map[string]cacheEntry
	history map[string]string // sessionId -> first typed prompt
	histOff int64
	files   map[string]os.FileInfo // the transcripts as last seen
	usage   map[string]*Usage      // today's and live sessions' usage as last read
}

type cacheEntry struct {
	key cacheKey
	s   Session
}

func NewScanner() *Scanner {
	return &Scanner{cache: map[string]cacheEntry{}, history: map[string]string{}, usage: map[string]*Usage{}}
}

// Scan returns all sessions, newest first, deduplicated by session id.
func (sc *Scanner) Scan() []*Session { return sc.scan(true, nil) }

// ScanChanged is Scan when only the given paths (from a Watcher) changed:
// the other transcripts aren't even looked at.
func (sc *Scanner) ScanChanged(changed map[string]bool) []*Session {
	sc.mu.Lock()
	full := sc.files == nil
	sc.mu.Unlock()
	if full {
		return sc.scan(true, nil)
	}
	return sc.scan(true, changed)
}

// ScanFast is Scan without token usage, which a cold start would otherwise
// read whole transcripts for: the first screen comes up in a fraction of the
// time, and the next Scan fills the usage in.
func (sc *Scanner) ScanFast() []*Session { return sc.scan(false, nil) }

// stats lists the transcripts with their file info: all of them, or the
// last list with only the changed paths looked at again.
func (sc *Scanner) stats(changed map[string]bool) map[string]os.FileInfo {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if changed == nil || sc.files == nil {
		files, _ := filepath.Glob(Dir("projects", "*", "*.jsonl"))
		sc.files = make(map[string]os.FileInfo, len(files))
		for _, f := range files {
			if info, err := os.Stat(f); err == nil {
				sc.files[f] = info
			}
		}
	} else {
		var look []string
		for p := range changed {
			switch {
			case strings.HasSuffix(p, ".jsonl") && filepath.Dir(filepath.Dir(p)) == Dir("projects"):
				look = append(look, p)
			case depth(p) == 1: // a project folder came or went
				files, _ := filepath.Glob(filepath.Join(p, "*.jsonl"))
				look = append(look, files...)
				for f := range sc.files {
					if filepath.Dir(f) == p {
						look = append(look, f)
					}
				}
			}
		}
		for _, p := range look {
			if info, err := os.Stat(p); err == nil {
				sc.files[p] = info
			} else {
				delete(sc.files, p)
			}
		}
	}
	out := make(map[string]os.FileInfo, len(sc.files))
	for k, v := range sc.files {
		out[k] = v
	}
	return out
}

// usageOf is a session's usage, read again only when its transcript or a
// subagent of it changed (or on a full scan).
func (sc *Scanner) usageOf(path string, dirty map[string]bool) *Usage {
	sc.mu.Lock()
	u, ok := sc.usage[path]
	sc.mu.Unlock()
	if ok && dirty != nil && !dirty[path] {
		return u
	}
	nu := UsageOf(path)
	sc.mu.Lock()
	sc.usage[path] = &nu
	sc.mu.Unlock()
	return &nu
}

func (sc *Scanner) scan(usage bool, changed map[string]bool) []*Session {
	sc.loadHistory()
	var dirty map[string]bool // sessions whose usage may have changed
	if changed != nil {
		dirty = map[string]bool{}
		for p := range changed {
			if s := SessionOf(p); s != "" {
				dirty[s] = true
			}
		}
	}
	infos := sc.stats(changed)
	files := make([]string, 0, len(infos))
	for f := range infos {
		files = append(files, f)
	}
	type job struct {
		path string
		info os.FileInfo
	}
	jobs := make(chan job)
	results := make(chan Session, len(files))
	today := Today()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				k := cacheKey{j.info.Size(), j.info.ModTime().UnixNano()}
				sc.mu.Lock()
				ce, ok := sc.cache[j.path]
				sc.mu.Unlock()
				var s Session
				if ok && ce.key == k {
					s = ce.s
				} else {
					s = parseSession(j.path, j.info)
					sc.mu.Lock()
					sc.cache[j.path] = cacheEntry{k, s}
					sc.mu.Unlock()
				}
				if usage && !s.Modified.Before(today) {
					s.Usage = sc.usageOf(s.Path, dirty)
				}
				results <- s
			}
		}()
	}
	go func() {
		for _, f := range files {
			jobs <- job{f, infos[f]}
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	byID := map[string]*Session{}
	for s := range results {
		s := s
		if s.ID == "" {
			continue
		}
		if prev, ok := byID[s.ID]; ok && prev.Modified.After(s.Modified) {
			continue
		}
		byID[s.ID] = &s
	}
	live := map[string]Proc{}
	for _, p := range LiveProcs() {
		live[p.SessionID] = p
	}
	out := make([]*Session, 0, len(byID))
	for _, s := range byID {
		if s.FirstPrompt == "" {
			s.FirstPrompt = sc.history[s.ID]
		}
		if s.Title == "" {
			s.Title = s.FirstPrompt
		}
		if p, ok := live[s.ID]; ok {
			p := p
			s.Live = &p
			if usage && s.Usage == nil {
				s.Usage = sc.usageOf(s.Path, dirty)
			}
			if p.Name != "" && s.Name == "" {
				s.Name = p.Name
			}
			if s.Title == "" { // started, nothing asked yet (an SDK run, a fresh claude)
				s.Title = s.Name
			}
			if s.Cwd == "" {
				s.Cwd = p.Cwd
			}
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Modified.After(out[j].Modified) })
	return out
}

// Today is local midnight.
func Today() time.Time {
	y, m, d := time.Now().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
}

// loadHistory incrementally reads history.jsonl for first prompts.
func (sc *Scanner) loadHistory() {
	f, err := os.Open(Dir("history.jsonl"))
	if err != nil {
		return
	}
	defer f.Close()
	if st, _ := f.Stat(); st != nil && st.Size() < sc.histOff {
		sc.histOff = 0
	}
	_, _ = f.Seek(sc.histOff, 0)
	r := bufio.NewReaderSize(f, 256*1024)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			break // partial trailing line is re-read next time
		}
		sc.histOff += int64(len(line))
		var h struct {
			Display   string `json:"display"`
			SessionID string `json:"sessionId"`
		}
		if json.Unmarshal(line, &h) == nil && h.SessionID != "" && h.Display != "" {
			if _, ok := sc.history[h.SessionID]; !ok && !strings.HasPrefix(h.Display, "/") {
				sc.history[h.SessionID] = h.Display
			}
		}
	}
}

// rec is the union of the transcript record fields we care about.
type rec struct {
	Type        string  `json:"type"`
	Subtype     string  `json:"subtype"`
	SessionID   string  `json:"sessionId"`
	Cwd         string  `json:"cwd"`
	GitBranch   string  `json:"gitBranch"`
	Version     string  `json:"version"`
	Timestamp   string  `json:"timestamp"`
	IsMeta      bool    `json:"isMeta"`
	IsSidechain bool    `json:"isSidechain"`
	IsCompact   bool    `json:"isCompactSummary"`
	SessionKind string  `json:"sessionKind"`
	Entrypoint  string  `json:"entrypoint"`
	AITitle     string  `json:"aiTitle"`
	AgentName   string  `json:"agentName"`
	LastPrompt  string  `json:"lastPrompt"`
	PRURL       string  `json:"prUrl"`
	Relocated   string  `json:"relocatedCwd"`
	MsgCount    int     `json:"messageCount"`
	TotalCost   float64 `json:"totalCostUSD"`
	LinesAdded  int     `json:"totalLinesAdded"`
	LinesDel    int     `json:"totalLinesRemoved"`
	WTSession   *struct {
		Name string `json:"worktreeName"`
		Path string `json:"worktreePath"`
	} `json:"worktreeSession"`
	Message *struct {
		Role    string          `json:"role"`
		Model   string          `json:"model"`
		ID      string          `json:"id"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

func readRange(f *os.File, off, n int64) []byte {
	buf := make([]byte, n)
	k, _ := f.ReadAt(buf, off)
	return buf[:k]
}

// lines splits a chunk into JSON lines; if dropFirst the (likely partial) first line is skipped.
func lines(b []byte, dropFirst bool) [][]byte {
	if dropFirst {
		if i := bytes.IndexByte(b, '\n'); i >= 0 {
			b = b[i+1:]
		} else {
			return nil
		}
	}
	return bytes.Split(b, []byte{'\n'})
}

const (
	headBytes = 96 << 10
	tailBytes = 768 << 10
)

func parseSession(path string, info os.FileInfo) Session {
	s := Session{
		ID:       strings.TrimSuffix(filepath.Base(path), ".jsonl"),
		Path:     path,
		Modified: info.ModTime(),
		Size:     info.Size(),
	}
	f, err := os.Open(path)
	if err != nil {
		return s
	}
	defer f.Close()

	// head: first prompt + initial cwd
	for _, l := range lines(readRange(f, 0, headBytes), false) {
		var r rec
		if len(l) == 0 || json.Unmarshal(l, &r) != nil {
			continue
		}
		if s.Cwd == "" && r.Cwd != "" {
			s.Cwd = r.Cwd
		}
		if s.FirstPrompt == "" && r.Type == "user" && !r.IsMeta && !r.IsCompact && r.Message != nil {
			if t := promptText(r.Message.Content); t != "" && !(strings.HasPrefix(t, "/") && !strings.Contains(t, " ")) {
				s.FirstPrompt = t
			}
		}
		if s.Cwd != "" && s.FirstPrompt != "" {
			break
		}
	}

	// tail: last-wins metadata
	off := s.Size - tailBytes
	if off < 0 {
		off = 0
	}
	prs := map[string]bool{}
	for _, l := range lines(readRange(f, off, s.Size-off), off > 0) {
		if len(l) == 0 {
			continue
		}
		var r rec
		if json.Unmarshal(slim(l), &r) != nil {
			continue
		}
		switch r.Type {
		case "ai-title":
			s.AITitle = r.AITitle
		case "agent-name":
			s.Name = r.AgentName
		case "last-prompt":
			if r.LastPrompt != "" {
				s.LastPrompt = r.LastPrompt
			}
		case "pr-link":
			if r.PRURL != "" && !prs[r.PRURL] {
				prs[r.PRURL] = true
				s.PRs = append(s.PRs, r.PRURL)
			}
		case "relocated":
			if r.Relocated != "" {
				s.Cwd = r.Relocated
			}
		case "worktree-state":
			if r.WTSession != nil {
				s.Worktree = r.WTSession.Name
			}
		case "cost-state":
			s.Cost, s.LinesAdd, s.LinesDel = r.TotalCost, r.LinesAdded, r.LinesDel
		case "system":
			if r.Subtype == "turn_duration" && r.MsgCount > 0 {
				s.Messages = r.MsgCount
			}
		case "user", "assistant":
			if r.IsSidechain {
				continue
			}
			if r.Cwd != "" {
				s.Cwd = r.Cwd
			}
			if r.GitBranch != "" && r.GitBranch != "HEAD" {
				s.Branch = r.GitBranch
			}
			if r.Version != "" {
				s.Version = r.Version
			}
			if r.SessionKind != "" {
				s.Kind = r.SessionKind
			}
			if r.Entrypoint != "" {
				s.Entrypoint = r.Entrypoint
			}
			if r.Message != nil && r.Message.Model != "" && r.Message.Model != "<synthetic>" {
				s.Model = r.Message.Model
			}
		}
	}
	if s.Worktree == "" {
		s.Worktree = worktreeName(s.Cwd)
	}
	s.Title = firstNonEmpty(s.Name, s.AITitle, s.FirstPrompt)
	if subs, _ := filepath.Glob(filepath.Join(strings.TrimSuffix(path, ".jsonl"), "subagents", "*.meta.json")); len(subs) > 0 {
		s.Subagents = len(subs)
	}
	return s
}

// The tail of a transcript is read for the metadata around its messages, not
// the messages: decoding (validating) every message's content and every tool
// output was most of a scan. slim keeps only the fields the tail reads, found
// by skipping over the values in between, and the message's model.
var (
	tailKeys = map[string]bool{"type": true, "subtype": true, "aiTitle": true, "agentName": true, "lastPrompt": true,
		"prUrl": true, "relocatedCwd": true, "worktreeSession": true, "totalCostUSD": true, "totalLinesAdded": true,
		"totalLinesRemoved": true, "messageCount": true, "isSidechain": true, "cwd": true, "gitBranch": true,
		"version": true, "sessionKind": true, "entrypoint": true, "message": true}
	messageKeys = map[string]bool{"model": true}
)

// slim is line l with only the fields the tail reads, or l itself when it
// can't be walked.
func slim(l []byte) []byte {
	if out := pick(l, tailKeys); out != nil {
		return out
	}
	return l
}

// pick copies the fields of object b named in keep (a "message" keeps only
// messageKeys); nil when b is not an object it can walk.
func pick(b []byte, keep map[string]bool) []byte {
	i := skipWS(b, 0)
	if i >= len(b) || b[i] != '{' {
		return nil
	}
	out := []byte{'{'}
	for i = skipWS(b, i+1); i < len(b) && b[i] != '}'; {
		if b[i] != '"' {
			return nil
		}
		ke := stringEnd(b, i)
		if ke < 0 {
			return nil
		}
		key := b[i+1 : ke-1]
		v0 := skipWS(b, ke)
		if v0 >= len(b) || b[v0] != ':' {
			return nil
		}
		v0 = skipWS(b, v0+1)
		ve := valueEnd(b, v0)
		if ve < 0 {
			return nil
		}
		if keep[string(key)] {
			v := b[v0:ve]
			if string(key) == "message" && v[0] == '{' {
				if v = pick(v, messageKeys); v == nil {
					return nil
				}
			}
			if len(out) > 1 {
				out = append(out, ',')
			}
			out = append(append(append(out, b[i:ke]...), ':'), v...)
		}
		if i = skipWS(b, ve); i < len(b) && b[i] == ',' {
			i = skipWS(b, i+1)
		}
	}
	if i >= len(b) {
		return nil
	}
	return append(out, '}')
}

func skipWS(b []byte, i int) int {
	for i < len(b) && (b[i] == ' ' || b[i] == '\t' || b[i] == '\n' || b[i] == '\r') {
		i++
	}
	return i
}

// stringEnd is the index just past the string literal starting at b[i], or -1.
func stringEnd(b []byte, i int) int {
	for j := i + 1; j < len(b); {
		k := bytes.IndexByte(b[j:], '"')
		if k < 0 {
			return -1
		}
		q := j + k
		n := 0 // backslashes before the quote: an odd number escapes it
		for p := q - 1; p > i && b[p] == '\\'; p-- {
			n++
		}
		if n%2 == 0 {
			return q + 1
		}
		j = q + 1
	}
	return -1
}

// valueEnd is the index just past the JSON value starting at b[i], or -1.
func valueEnd(b []byte, i int) int {
	if i >= len(b) {
		return -1
	}
	switch b[i] {
	case '"':
		return stringEnd(b, i)
	case '{', '[':
		depth := 0
		for j := i; j < len(b); j++ {
			switch b[j] {
			case '"':
				e := stringEnd(b, j)
				if e < 0 {
					return -1
				}
				j = e - 1
			case '{', '[':
				depth++
			case '}', ']':
				if depth--; depth == 0 {
					return j + 1
				}
			}
		}
		return -1
	}
	j := i // a number, true, false or null
	for j < len(b) && b[j] != ',' && b[j] != '}' && b[j] != ']' && b[j] != ' ' && b[j] != '\n' && b[j] != '\r' && b[j] != '\t' {
		j++
	}
	if j == i {
		return -1
	}
	return j
}

var wtMarkers = []string{"/.claude/worktrees/", ".worktrees/", "/worktrees/"}

// HomeWorktree recognises tool-managed worktrees kept under the home folder,
// ~/.<tool>/worktrees/<repo>/<name>, and returns repo and name.
func HomeWorktree(cwd string) (repo, name string, ok bool) {
	h := home
	if h == "" || !strings.HasPrefix(cwd, h+"/.") {
		return "", "", false
	}
	p := strings.Split(cwd[len(h)+1:], "/")
	if len(p) < 4 || p[1] != "worktrees" || p[0] == ".claude" {
		return "", "", false
	}
	return p[2], p[3], true
}

func worktreeName(cwd string) string {
	if _, n, ok := HomeWorktree(cwd); ok {
		return n
	}
	for _, m := range wtMarkers {
		if i := strings.Index(cwd, m); i >= 0 {
			rest := cwd[i+len(m):]
			if j := strings.IndexByte(rest, '/'); j >= 0 {
				rest = rest[:j]
			}
			return rest
		}
	}
	if strings.Contains(cwd, "/pr-loop/wt-") {
		return filepath.Base(cwd)
	}
	return ""
}

// promptText extracts human text from a user message content (string or [{type:text}]).
func promptText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var str string
	if json.Unmarshal(raw, &str) == nil {
		return cleanPrompt(str)
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		for _, b := range blocks {
			if b.Type == "tool_result" {
				return ""
			}
			if b.Type == "text" {
				if t := cleanPrompt(b.Text); t != "" {
					return t
				}
			}
		}
	}
	return ""
}

// cleanPrompt drops harness-injected wrappers so titles show what the human typed.
func cleanPrompt(s string) string {
	t := strings.TrimSpace(s)
	if strings.HasPrefix(t, "<command-name>") {
		if i := strings.Index(t, "<command-name>"); i >= 0 {
			rest := t[i+len("<command-name>"):]
			if j := strings.Index(rest, "</command-name>"); j >= 0 {
				name := rest[:j]
				args := between(t, "<command-args>", "</command-args>")
				return strings.TrimSpace(name + " " + args)
			}
		}
	}
	if strings.HasPrefix(t, "<") && (strings.HasPrefix(t, "<local-command") || strings.HasPrefix(t, "<system-reminder") ||
		strings.HasPrefix(t, "<task-notification") || strings.HasPrefix(t, "<command-message")) {
		return ""
	}
	if strings.HasPrefix(t, "Caveat:") {
		return ""
	}
	return t
}

func between(s, a, b string) string {
	i := strings.Index(s, a)
	if i < 0 {
		return ""
	}
	s = s[i+len(a):]
	if j := strings.Index(s, b); j >= 0 {
		return s[:j]
	}
	return ""
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}
