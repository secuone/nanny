package claude

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Entry struct {
	Role string // user | assistant | tool | result
	Text string
	At   time.Time
}

type Subagent struct {
	ID          string
	Type        string
	Description string
	Depth       int
	Parent      string // parentAgentId for nested agents
	Model       string
	Background  bool
	Finished    bool
	Running     bool
	Worktree    string
	LastTool    string
	LastText    string
	Modified    time.Time
	Started     time.Time
	Path        string
	Children    []*Subagent
}

// Transcript returns the last n conversational entries of a transcript.
func Transcript(path string, n int) []Entry {
	return readEntries(path, n, 512<<10, false)
}

// entryCache memoises parsed tails by (path, n, size, mtime); refreshes every
// couple of seconds would otherwise re-read hundreds of subagent files.
var (
	entryMu    sync.Mutex
	entryCache = map[string]entryCacheVal{}
)

type entryCacheVal struct {
	size  int64
	mtime time.Time
	es    []Entry
}

func readEntries(path string, n int, window int64, sidechain bool) []Entry {
	st, err := os.Stat(path)
	if err != nil {
		return nil
	}
	key := fmt.Sprintf("%s|%d|%d", path, n, window)
	entryMu.Lock()
	c, ok := entryCache[key]
	entryMu.Unlock()
	if ok && c.size == st.Size() && c.mtime.Equal(st.ModTime()) {
		return c.es
	}
	es := parseEntries(path, n, window, sidechain)
	// big tool outputs can push all conversation out of the window: widen it
	for len(es) < min(n, 12) && window < st.Size() && window < 16<<20 {
		window *= 4
		es = parseEntries(path, n, window, sidechain)
	}
	entryMu.Lock()
	if len(entryCache) > 4000 {
		entryCache = map[string]entryCacheVal{}
	}
	entryCache[key] = entryCacheVal{st.Size(), st.ModTime(), es}
	entryMu.Unlock()
	return es
}

func parseEntries(path string, n int, window int64, sidechain bool) []Entry {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	st, _ := f.Stat()
	off := st.Size() - window
	if off < 0 {
		off = 0
	}
	var out []Entry
	for _, l := range lines(readRange(f, off, st.Size()-off), off > 0) {
		var r rec
		if len(l) == 0 || json.Unmarshal(l, &r) != nil || r.Message == nil || r.IsMeta {
			continue
		}
		if r.IsSidechain != sidechain {
			continue
		}
		at, _ := time.Parse(time.RFC3339Nano, r.Timestamp)
		switch r.Type {
		case "user":
			if t := promptText(r.Message.Content); t != "" && !r.IsCompact {
				out = append(out, Entry{"user", t, at})
			}
		case "assistant":
			out = append(out, assistantEntries(r.Message.Content, at)...)
		}
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out
}

type block struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

func assistantEntries(raw json.RawMessage, at time.Time) []Entry {
	var blocks []block
	if json.Unmarshal(raw, &blocks) != nil {
		return nil
	}
	var out []Entry
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if t := strings.TrimSpace(b.Text); t != "" {
				out = append(out, Entry{"assistant", t, at})
			}
		case "tool_use":
			out = append(out, Entry{"tool", ShortTool(b.Name) + " " + toolArg(b.Name, b.Input), at})
		}
	}
	return out
}

// toolArg picks the most informative input field for a one-line tool summary.
func toolArg(name string, in json.RawMessage) string {
	var m map[string]any
	if json.Unmarshal(in, &m) != nil {
		return ""
	}
	for _, k := range []string{"description", "command", "file_path", "pattern", "url", "query", "prompt", "skill", "subject"} {
		if v, ok := m[k].(string); ok && v != "" {
			v = strings.ReplaceAll(v, home, "~")
			return oneLine(v)
		}
	}
	return ""
}

// ShortTool turns mcp__server__tool into server:tool.
func ShortTool(n string) string {
	if strings.HasPrefix(n, "mcp__") {
		p := strings.SplitN(strings.TrimPrefix(n, "mcp__"), "__", 2)
		if len(p) == 2 {
			srv := p[0]
			if i := strings.LastIndex(srv, "_"); i >= 0 {
				srv = srv[i+1:]
			}
			return srv + ":" + p[1]
		}
	}
	return n
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// Subagents loads the subagent tree of a session transcript.
func Subagents(transcript string) []*Subagent {
	dir := filepath.Join(strings.TrimSuffix(transcript, ".jsonl"), "subagents")
	metas, _ := filepath.Glob(filepath.Join(dir, "*.meta.json"))
	all := map[string]*Subagent{}
	var list []*Subagent
	for _, mp := range metas {
		bt, err := os.Stat(mp)
		if err != nil {
			continue
		}
		m, ok := readMeta(mp, bt)
		if !ok {
			continue
		}
		id := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(mp), "agent-"), ".meta.json")
		jp := filepath.Join(dir, "agent-"+id+".jsonl")
		sa := &Subagent{ID: id, Type: m.AgentType, Description: m.Description, Depth: m.SpawnDepth, Parent: m.ParentAgentID,
			Model: m.Model, Background: m.RequestShape == "background", Path: jp, Worktree: m.WorktreePath}
		if st, err := os.Stat(jp); err == nil {
			sa.Modified = st.ModTime()
		}
		sa.Started = bt.ModTime()
		sa.Finished = m.Finished != nil && *m.Finished
		sa.LastTool, sa.LastText = lastActivity(jp)
		if !sa.Finished {
			// old metas never got "finished"; treat quiet ones as done
			sa.Running = time.Since(sa.Modified) < 3*time.Minute && !strings.HasPrefix(sa.LastTool, "SubagentHandback")
			sa.Finished = !sa.Running
		}
		all[id] = sa
		list = append(list, sa)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Started.Before(list[j].Started) })
	var roots []*Subagent
	for _, sa := range list {
		if p, ok := all[sa.Parent]; ok && sa.Parent != "" {
			p.Children = append(p.Children, sa)
		} else {
			roots = append(roots, sa)
		}
	}
	return roots
}

type subMeta struct {
	AgentType     string `json:"agentType"`
	Description   string `json:"description"`
	SpawnDepth    int    `json:"spawnDepth"`
	RequestShape  string `json:"requestShape"`
	Model         string `json:"model"`
	Finished      *bool  `json:"finished"`
	ParentAgentID string `json:"parentAgentId"`
	WorktreePath  string `json:"worktreePath"`
}

// metas caches each subagent's meta.json by size and mtime: every live agent
// asks for its subagents every tick.
var (
	metasMu   sync.Mutex
	metaCache = map[string]metaVal{}
)

type metaVal struct {
	size, mtime int64
	m           subMeta
}

func readMeta(path string, st os.FileInfo) (subMeta, bool) {
	metasMu.Lock()
	c, ok := metaCache[path]
	metasMu.Unlock()
	if ok && c.size == st.Size() && c.mtime == st.ModTime().UnixNano() {
		return c.m, true
	}
	var m subMeta
	b, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(b, &m) != nil {
		return m, false
	}
	metasMu.Lock()
	if len(metaCache) > 20000 {
		metaCache = map[string]metaVal{}
	}
	metaCache[path] = metaVal{st.Size(), st.ModTime().UnixNano(), m}
	metasMu.Unlock()
	return m, true
}

// lastActs caches lastActivity by file size and mtime: a finished subagent's
// transcript never changes, and the detail asks again every refresh.
var (
	lastActsMu sync.Mutex
	lastActs   = map[string]lastAct{}
)

type lastAct struct {
	size, mtime int64
	tool, text  string
}

func lastActivity(path string) (tool, text string) {
	st, err := os.Stat(path)
	if err != nil {
		return "", ""
	}
	lastActsMu.Lock()
	c, ok := lastActs[path]
	lastActsMu.Unlock()
	if ok && c.size == st.Size() && c.mtime == st.ModTime().UnixNano() {
		return c.tool, c.text
	}
	tool, text = readLastActivity(path)
	lastActsMu.Lock()
	lastActs[path] = lastAct{st.Size(), st.ModTime().UnixNano(), tool, text}
	lastActsMu.Unlock()
	return tool, text
}

// readLastActivity walks a running subagent's transcript back from its end,
// decoding assistant lines only, in a window that grows until both the last
// tool and the last text are found: a running one is read again every tick,
// and its tool results can be megabytes long.
func readLastActivity(path string) (tool, text string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", ""
	}
	size := st.Size()
	var r struct {
		Type        string `json:"type"`
		IsMeta      bool   `json:"isMeta"`
		IsSidechain bool   `json:"isSidechain"`
		Message     *struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	// each round reads the chunk before the last one, up to 4 MB in all
	end := size
	for chunk := int64(32 << 10); ; chunk *= 4 {
		off := max(0, end-chunk)
		b := readRange(f, off, end-off)
		if off > 0 { // its first line starts before off: it goes in the next chunk
			i := bytes.IndexByte(b, '\n')
			if i < 0 {
				i = len(b) - 1
			}
			b, end = b[i+1:], off+int64(i)+1
		} else {
			end = 0
		}
		ls := bytes.Split(b, []byte{'\n'})
		for i := len(ls) - 1; i >= 0 && (tool == "" || text == ""); i-- {
			if !bytes.Contains(ls[i], []byte(`"assistant"`)) {
				continue
			}
			r.Type, r.IsMeta, r.IsSidechain, r.Message = "", false, false, nil
			if json.Unmarshal(ls[i], &r) != nil || r.Type != "assistant" || r.Message == nil || r.IsMeta || !r.IsSidechain {
				continue
			}
			es := assistantEntries(r.Message.Content, time.Time{})
			for j := len(es) - 1; j >= 0; j-- {
				if es[j].Role == "tool" && tool == "" {
					tool = es[j].Text
				}
				if es[j].Role == "assistant" && text == "" {
					text = es[j].Text
				}
			}
		}
		if tool != "" && text != "" || end == 0 || size-end >= 4<<20 {
			return tool, text
		}
	}
}

// FullTranscript reads up to the last 8 MB of a transcript (whole conversations
// for anything but the giants).
func FullTranscript(path string, sidechain bool) []Entry {
	return readEntries(path, 5000, 8<<20, sidechain)
}

// SubTranscript returns the last entries of one subagent.
func SubTranscript(path string, n int) []Entry { return readEntries(path, n, 256<<10, true) }

// Flatten walks a subagent tree depth-first, returning nodes with their depth.
func Flatten(roots []*Subagent) []FlatSub {
	var out []FlatSub
	var walk func([]*Subagent, int)
	walk = func(ns []*Subagent, d int) {
		for i, n := range ns {
			out = append(out, FlatSub{n, d, i == len(ns)-1})
			walk(n.Children, d+1)
		}
	}
	walk(roots, 0)
	return out
}

type FlatSub struct {
	*Subagent
	Level int
	Last  bool
}

func (s *Subagent) Status() string {
	if s.Running {
		return "running"
	}
	return "done"
}

func (s *Subagent) String() string { return fmt.Sprintf("%s %s", s.Type, s.Description) }
