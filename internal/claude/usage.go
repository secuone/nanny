package claude

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Tokens is token spend split the way the API bills it.
type Tokens struct {
	In, Out, CacheW, CacheR int64
	CacheW1h                int64 // the part of CacheW written with the 1-hour TTL
}

func (t Tokens) add(o Tokens) Tokens {
	return Tokens{t.In + o.In, t.Out + o.Out, t.CacheW + o.CacheW, t.CacheR + o.CacheR, t.CacheW1h + o.CacheW1h}
}

func (t Tokens) sub(o Tokens) Tokens {
	return Tokens{t.In - o.In, t.Out - o.Out, t.CacheW - o.CacheW, t.CacheR - o.CacheR, t.CacheW1h - o.CacheW1h}
}

// Sum is every token, in and out.
func (t Tokens) Sum() int64 { return t.In + t.Out + t.CacheW + t.CacheR }

// Ctx is the context a turn sent: input, cache writes and cache reads.
func (t Tokens) Ctx() int64 { return t.In + t.CacheW + t.CacheR }

// Usage is what a session spent, read from message.usage of assistant entries.
type Usage struct {
	Main  map[string]Tokens // main chain, by model
	Subs  map[string]Tokens // its subagents, by model
	Ctx   int64             // context of the last main-chain turn: input + cache write + cache read
	Peak  int64             // largest context seen
	Model string            // model of the last main-chain turn
}

// Total is the tokens of the session and its subagents.
func (u Usage) Total() Tokens {
	var t Tokens
	for _, m := range []map[string]Tokens{u.Main, u.Subs} {
		for _, v := range m {
			t = t.add(v)
		}
	}
	return t
}

// Cost is the estimated USD of the main chain; SubCost of its subagents.
func (u Usage) Cost() float64    { return costOf(u.Main) }
func (u Usage) SubCost() float64 { return costOf(u.Subs) }

// Window is the context window: 1M for [1m] models or sessions that ran past 200k.
func (u Usage) Window() int64 {
	if strings.HasSuffix(u.Model, "[1m]") || u.Peak > 200_000 {
		return 1_000_000
	}
	return 200_000
}

// Pct is the share of the context window in use, 0-100.
func (u Usage) Pct() int {
	if u.Ctx <= 0 {
		return 0
	}
	return int(min(100, u.Ctx*100/u.Window()))
}

// ---------------------------------------------------------------- prices

// Price is USD per million tokens.
type Price struct {
	Input      float64 `toml:"input"`
	Output     float64 `toml:"output"`
	CacheWrite float64 `toml:"cache_write"` // 5-minute cache writes
	CacheRead  float64 `toml:"cache_read"`
}

// Families are the current models' list prices; older generations below.
var Families = map[string]Price{
	"fable":  {10, 50, 12.5, 0.25},
	"opus":   {4, 20, 5, 0.20},
	"sonnet": {2, 10, 2.5, 0.20},
	"haiku":  {0.10, 0.50, 0.125, 0.01},
}

// older generations priced differently from their family's current model
var legacy = []struct {
	prefix string
	p      Price
}{
	{"claude-opus-4-2025", Price{15, 75, 18.75, 1.5}}, // Opus 4
	{"claude-opus-4-0", Price{15, 75, 18.75, 1.5}},
	{"claude-opus-4-1", Price{15, 75, 18.75, 1.5}},
	{"claude-opus-4", Price{5, 25, 6.25, 0.5}},
	{"claude-opus-5-2", Price{5, 25, 6.25, 0.5}}, // dated Opus 5
	{"claude-sonnet-4", Price{3, 15, 3.75, 0.3}},
	{"claude-haiku-4", Price{1, 5, 1.25, 0.1}},
}

var (
	priceMu   sync.RWMutex
	overrides = map[string]Price{}
)

// SetPrices overrides family prices (config [prices.<family>]); zero fields keep the default.
func SetPrices(m map[string]Price) {
	priceMu.Lock()
	defer priceMu.Unlock()
	overrides = map[string]Price{}
	for k, v := range m {
		overrides[strings.ToLower(k)] = v
	}
}

// Family is opus, sonnet, haiku or fable ("" when unknown).
func Family(model string) string {
	m := strings.ToLower(model)
	for _, f := range []string{"fable", "mythos", "opus", "sonnet", "haiku"} {
		if strings.Contains(m, f) {
			if f == "mythos" {
				return "fable"
			}
			return f
		}
	}
	return ""
}

// PriceOf is the price of a model id; an override beats the table, unknown models
// fall back to their family and then to opus.
func PriceOf(model string) Price {
	fam := Family(model)
	if fam == "" {
		fam = "opus"
	}
	p := Families[fam]
	id := strings.TrimSuffix(strings.ToLower(model), "[1m]")
	if i := strings.Index(id, "claude-"); i > 0 { // us.anthropic.claude-…
		id = id[i:]
	}
	for _, l := range legacy {
		if strings.HasPrefix(id, l.prefix) || id+"-2" == l.prefix { // bare claude-opus-5 too
			p = l.p
			break
		}
	}
	priceMu.RLock()
	o, ok := overrides[fam]
	priceMu.RUnlock()
	if ok {
		for _, f := range []struct{ dst, src *float64 }{{&p.Input, &o.Input}, {&p.Output, &o.Output}, {&p.CacheWrite, &o.CacheWrite}, {&p.CacheRead, &o.CacheRead}} {
			if *f.src > 0 {
				*f.dst = *f.src
			}
		}
	}
	return p
}

func costOf(m map[string]Tokens) float64 {
	c := 0.0
	for mdl, t := range m {
		p := PriceOf(mdl)
		// 1-hour cache writes cost twice the input price, 5-minute ones CacheWrite
		c += (float64(t.In)*p.Input + float64(t.Out)*p.Output + float64(t.CacheW-t.CacheW1h)*p.CacheWrite +
			float64(t.CacheW1h)*2*p.Input + float64(t.CacheR)*p.CacheRead) / 1e6
	}
	return c
}

// ---------------------------------------------------------------- parsing

// usageFile is the running tally of one transcript; only appended bytes are read.
type usageFile struct {
	off    int64
	size   int64
	lastID string // a message is logged once per content block, usage repeated
	lastU  Tokens
	lastM  string
	by     map[string]Tokens
	ctx    int64
	peak   int64
	model  string
}

// usageEntry locks one file, so a cold read of a big transcript doesn't stall the others.
type usageEntry struct {
	mu sync.Mutex
	f  usageFile
}

var (
	usageMu    sync.Mutex
	usageFiles = map[string]*usageEntry{}
)

type usageRec struct {
	Type        string `json:"type"`
	IsSidechain bool   `json:"isSidechain"`
	Message     *struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage *struct {
			apiUsage
			Iterations []apiUsage `json:"iterations"` // the real numbers when the top level is still zero
		} `json:"usage"`
	} `json:"message"`
}

// parseUsageRec reads what readUsage needs from a transcript line without
// decoding all of it: an assistant line can be megabytes of content (a file it
// wrote), and validating that dominated nanny's CPU. The message's model and id
// come before its content and its usage after it, so they are cut out and only
// the usage object is decoded. Anything unexpected falls back to the full decode.
func parseUsageRec(l []byte) (usageRec, bool) {
	var rc usageRec
	head, _, ok := bytes.Cut(l, []byte(`"content":`))
	u := lastIndexTail(l, []byte(`"usage":{`), 64<<10) // it comes last, after the content
	if !ok || u < len(head) || !bytes.Contains(head, []byte(`"message":{`)) {
		return rc, json.Unmarshal(l, &rc) == nil
	}
	if !bytes.Contains(head, []byte(`"role":"assistant"`)) || !bytes.Contains(head, []byte(`"model":"`)) {
		return rc, json.Unmarshal(l, &rc) == nil // written in another order
	}
	field := func(key string) string {
		_, v, ok := bytes.Cut(head[bytes.Index(head, []byte(`"message":{`)):], []byte(`"`+key+`":"`))
		if !ok {
			return ""
		}
		v, _, _ = bytes.Cut(v, []byte(`"`))
		return string(v)
	}
	rc.Type = "assistant"
	rc.IsSidechain = bytes.Contains(head, []byte(`"isSidechain":true`))
	rc.Message = &struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage *struct {
			apiUsage
			Iterations []apiUsage `json:"iterations"`
		} `json:"usage"`
	}{ID: field("id"), Model: field("model")}
	obj := jsonObject(l[u+len(`"usage":`):])
	if obj == nil || json.Unmarshal(obj, &rc.Message.Usage) != nil {
		return rc, json.Unmarshal(l, &rc) == nil
	}
	return rc, true
}

// lastIndexTail is bytes.LastIndex, looking in the last n bytes first.
func lastIndexTail(b, sep []byte, n int) int {
	if len(b) > n {
		if i := bytes.LastIndex(b[len(b)-n:], sep); i >= 0 {
			return len(b) - n + i
		}
	}
	return bytes.LastIndex(b, sep)
}

// jsonObject is the object b starts with, up to its closing brace, or nil.
func jsonObject(b []byte) []byte {
	depth, str, esc := 0, false, false
	for i, c := range b {
		switch {
		case esc:
			esc = false
		case str:
			esc, str = c == '\\', c != '"'
		case c == '"':
			str = true
		case c == '{' || c == '[':
			depth++
		case c == '}' || c == ']':
			if depth--; depth == 0 {
				return b[:i+1]
			}
		}
	}
	return nil
}

type apiUsage struct {
	In     int64 `json:"input_tokens"`
	CacheW int64 `json:"cache_creation_input_tokens"`
	CacheR int64 `json:"cache_read_input_tokens"`
	Out    int64 `json:"output_tokens"`
	Cache  struct {
		W1h int64 `json:"ephemeral_1h_input_tokens"`
	} `json:"cache_creation"`
}

func (a apiUsage) tokens() Tokens { return Tokens{a.In, a.Out, a.CacheW, a.CacheR, a.Cache.W1h} }

// scanUsage brings the tally of path up to date and returns a copy.
func scanUsage(path string) usageFile {
	st, err := os.Stat(path)
	if err != nil {
		return usageFile{}
	}
	usageMu.Lock()
	e, ok := usageFiles[path]
	if !ok {
		e = &usageEntry{f: usageFile{by: map[string]Tokens{}}}
		usageFiles[path] = e
	}
	usageMu.Unlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	uf := &e.f
	if st.Size() < uf.size { // rewritten: start over
		*uf = usageFile{by: map[string]Tokens{}}
	}
	if st.Size() > uf.off {
		readUsage(path, uf)
	}
	uf.size = st.Size()
	cp := *uf
	cp.by = make(map[string]Tokens, len(uf.by))
	for k, v := range uf.by {
		cp.by[k] = v
	}
	return cp
}

func readUsage(path string, uf *usageFile) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	if _, err := f.Seek(uf.off, io.SeekStart); err != nil {
		return
	}
	r := bufio.NewReaderSize(f, 256<<10)
	for {
		l, err := r.ReadBytes('\n')
		if err != nil {
			return // a partial trailing line is read again next time
		}
		uf.off += int64(len(l))
		// only an assistant's messages are billed; the role comes in the first
		// few hundred bytes, so a line of megabytes of tool output isn't scanned
		if h := l[:min(len(l), 8<<10)]; !bytes.Contains(h, []byte(`"role":"assistant"`)) && !bytes.Contains(h, []byte(`"type":"assistant"`)) {
			continue
		}
		rc, ok := parseUsageRec(l)
		if !ok || rc.Type != "assistant" || rc.Message == nil || rc.Message.Usage == nil {
			continue
		}
		m, u := rc.Message, rc.Message.Usage
		if m.Model == "" || m.Model == "<synthetic>" {
			continue
		}
		t, ctx := u.tokens(), u.tokens().Ctx()
		if t.Sum() == 0 {
			for _, it := range u.Iterations {
				t = t.add(it.tokens())
				ctx = it.tokens().Ctx()
			}
		}
		if t.Sum() == 0 { // interrupted or not yet billed
			continue
		}
		if m.ID != "" && m.ID == uf.lastID && m.Model == uf.lastM {
			uf.by[m.Model] = uf.by[m.Model].sub(uf.lastU)
		}
		uf.by[m.Model] = uf.by[m.Model].add(t)
		uf.lastID, uf.lastU, uf.lastM = m.ID, t, m.Model
		if rc.IsSidechain {
			continue
		}
		uf.ctx = ctx
		uf.peak = max(uf.peak, uf.ctx)
		uf.model = m.Model
	}
}

// UsageOf tallies a session transcript and its subagents' transcripts.
func UsageOf(transcript string) Usage {
	mf := scanUsage(transcript)
	u := Usage{Main: mf.by, Subs: map[string]Tokens{}, Ctx: mf.ctx, Peak: mf.peak, Model: mf.model}
	subs, _ := filepath.Glob(filepath.Join(strings.TrimSuffix(transcript, ".jsonl"), "subagents", "agent-*.jsonl"))
	for _, p := range subs {
		for k, v := range scanUsage(p).by {
			u.Subs[k] = u.Subs[k].add(v)
		}
	}
	return u
}
