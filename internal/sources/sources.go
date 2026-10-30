// Package sources gathers non-Claude-transcript activity: hermes, opencode,
// Claude background jobs, the Claude daemon, cron-driven loops.
package sources

import (
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/secuone/nanny/internal/i18n"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Status string

const (
	Running Status = "running"
	Idle    Status = "idle"
	Blocked Status = "blocked"
	Done    Status = "done"
	Failed  Status = "failed"
	Off     Status = "off"
	Sched   Status = "scheduled"
)

type KV struct{ K, V string }

type Item struct {
	Group     string // section header, e.g. "Hermes", "Claude jobs"
	Name      string
	Status    Status
	Detail    string
	When      time.Time
	Fields    []KV
	Cwd       string
	SessionID string // Claude session to resume, when applicable
	LogPath   string // tail-able file
	Cmd       string // command to open it ("hermes", "opencode -s id"…)
	URL       string // opened in the browser (a dev server, a container)
	Kind      string // "container" or "tilt": the Monitor offers their own actions
	Ref       string // what those actions act on: the container or Tilt resource name
	Stopped   bool   // a container that isn't running, a Tilt resource turned off
}

var home, _ = os.UserHomeDir()

func hp(p ...string) string { return filepath.Join(append([]string{home}, p...)...) }

// Alive reports whether a pid exists.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

var (
	sqlMu    sync.Mutex
	sqlCache = map[string]sqlRows{}
)

type sqlRows struct {
	sig  string
	rows []map[string]any
}

// sqlite runs a read-only query through python3 (no cgo, no extra binary) and
// returns rows as maps; again only once the database (or its WAL) changed.
func sqlite(db, query string) []map[string]any {
	st, err := os.Stat(db)
	if err != nil {
		return nil
	}
	sig := fmt.Sprint(st.Size(), st.ModTime().UnixNano())
	if w, err := os.Stat(db + "-wal"); err == nil {
		sig += fmt.Sprint(" ", w.Size(), w.ModTime().UnixNano())
	}
	sqlMu.Lock()
	c, ok := sqlCache[db+"\x00"+query]
	sqlMu.Unlock()
	if ok && c.sig == sig {
		return c.rows
	}
	rows := sqliteRun(db, query)
	sqlMu.Lock()
	sqlCache[db+"\x00"+query] = sqlRows{sig, rows}
	sqlMu.Unlock()
	return rows
}

func sqliteRun(db, query string) []map[string]any {
	py := `import sys,json,sqlite3
c=sqlite3.connect('file:'+sys.argv[1]+'?mode=ro',uri=True,timeout=1)
c.row_factory=sqlite3.Row
print(json.dumps([dict(r) for r in c.execute(sys.argv[2])],default=str))`
	out, err := exec.Command("python3", "-I", "-c", py, db, query).Output()
	if err != nil {
		return nil
	}
	var rows []map[string]any
	_ = json.Unmarshal(out, &rows)
	return rows
}

func str(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', 2, 64)
	default:
		return fmt.Sprint(x)
	}
}

func num(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case string:
		f, _ := strconv.ParseFloat(x, 64)
		return f
	}
	return 0
}

func parseTime(s string) time.Time {
	for _, l := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.999999-07:00"} {
		if t, err := time.Parse(l, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// Collectors are the sources by name, in the order Collect runs them.
var Collectors = []struct {
	Name string
	Run  []func() []Item
}{
	{"machine", []func() []Item{Machine}},
	{"ports", []func() []Item{Ports}},
	{"containers", []func() []Item{Containers}},
	{"tilt", []func() []Item{Tilt}},
	{"watchers", nil}, // the user's own, run with their config
	{"hermes", []func() []Item{Hermes}},
	{"cron", []func() []Item{Timers}},
	{"jobs", []func() []Item{ClaudeJobs, PRLoop}},
	{"cron", []func() []Item{Cron}},
	{"jobs", []func() []Item{OpenCode}},
}

// Collect gathers the sources on says yes to; the others don't run at all.
// Each source is best-effort.
func Collect(on func(name string) bool, watches []Watch) []Item {
	var items []Item
	for _, c := range Collectors {
		if !on(c.Name) {
			continue
		}
		if c.Name == "watchers" {
			items = append(items, Watchers(watches)...)
		}
		for _, run := range c.Run {
			items = append(items, run()...)
		}
	}
	return items
}

// ---------------------------------------------------------------- hermes

func hermesHome() string {
	if h := os.Getenv("HERMES_HOME"); h != "" {
		return h
	}
	return hp(".hermes")
}

func Hermes() []Item {
	hh := hermesHome()
	if _, err := os.Stat(hh); err != nil {
		return nil
	}
	var items []Item
	var gs struct {
		Pid          int                       `json:"pid"`
		State        string                    `json:"gateway_state"`
		ActiveAgents int                       `json:"active_agents"`
		Platforms    map[string]map[string]any `json:"platforms"`
		UpdatedAt    string                    `json:"updated_at"`
		Version      string                    `json:"code_version"`
		ActiveWork   any                       `json:"active_work"`
	}
	gw := Item{Group: i18n.M("Hermes"), Name: "gateway", Status: Off, Cmd: "hermes", LogPath: filepath.Join(hh, "logs", "gateway.log")}
	if readJSON(filepath.Join(hh, "gateway_state.json"), &gs) == nil {
		gw.When = parseTime(gs.UpdatedAt)
		if Alive(gs.Pid) && gs.State == "running" {
			gw.Status = Idle
			if gs.ActiveAgents > 0 {
				gw.Status = Running
			}
		}
		var plats []string
		for name, p := range gs.Platforms {
			plats = append(plats, name+":"+str(p["state"]))
		}
		sort.Strings(plats)
		gw.Detail = fmt.Sprintf("%s · %d active agents · %s", gs.State, gs.ActiveAgents, strings.Join(plats, " "))
		gw.Fields = []KV{{i18n.M("version"), gs.Version}, {i18n.M("pid"), strconv.Itoa(gs.Pid)}, {i18n.M("state"), gs.State},
			{"agents", strconv.Itoa(gs.ActiveAgents)}, {"platforms", strings.Join(plats, ", ")}}
		if gs.ActiveWork != nil {
			b, _ := json.Marshal(gs.ActiveWork)
			gw.Fields = append(gw.Fields, KV{i18n.M("work"), string(b)})
		}
	}
	var hb struct {
		UpdatedAt string `json:"updated_at"`
		Mem       struct {
			RSS float64 `json:"rss_kib"`
		} `json:"mem"`
	}
	if readJSON(filepath.Join(hh, "state", "gateway.heartbeat"), &hb) == nil {
		gw.Fields = append(gw.Fields, KV{i18n.M("heartbeat"), hb.UpdatedAt}, KV{i18n.M("rss"), fmt.Sprintf("%.0f MiB", hb.Mem.RSS/1024)})
	}
	items = append(items, gw)

	// cron jobs
	var jobs struct {
		Jobs []map[string]any `json:"jobs"`
	}
	if readJSON(filepath.Join(hh, "cron", "jobs.json"), &jobs) == nil {
		for _, j := range jobs.Jobs {
			it := Item{Group: i18n.M("Hermes cron"), Name: str(j["name"]), Status: Sched, Cmd: "hermes cron list"}
			if en, _ := j["enabled"].(bool); !en {
				it.Status = Off
			}
			if str(j["last_status"]) != "" && str(j["last_status"]) != "ok" {
				it.Status = Failed
			}
			it.When = parseTime(str(j["last_run_at"]))
			it.Detail = fmt.Sprintf("%s → next %s · last %s", str(j["schedule_display"]), shortTime(str(j["next_run_at"])), str(j["last_status"]))
			it.Fields = []KV{{i18n.M("schedule"), str(j["schedule_display"])}, {i18n.M("next"), str(j["next_run_at"])},
				{"last run", str(j["last_run_at"])}, {"last", str(j["last_status"])}, {"deliver", str(j["deliver"])},
				{"failures", str(j["failure_streak"])}}
			if e := str(j["last_error"]); e != "" {
				it.Fields = append(it.Fields, KV{i18n.M("error"), e})
			}
			if lf, ok := j["last_failure"].(map[string]any); ok {
				it.Fields = append(it.Fields, KV{i18n.M("last fail"), str(lf["at"]) + " " + str(lf["detail"])})
			}
			items = append(items, it)
		}
	}

	// sessions
	rows := sqlite(filepath.Join(hh, "state.db"),
		`SELECT id, source, title, display_name, model, started_at, ended_at, end_reason, message_count, tool_call_count, input_tokens
		 FROM sessions ORDER BY COALESCE(ended_at, 9e18) DESC, started_at DESC LIMIT 25`)
	for _, r := range rows {
		it := Item{Group: i18n.M("Hermes sessions"), Name: firstNonEmpty(str(r["title"]), str(r["display_name"]), str(r["id"])),
			Status: Done, Cmd: "hermes --resume " + str(r["id"])}
		if r["ended_at"] == nil {
			it.Status = Idle
		}
		started := time.Unix(int64(num(r["started_at"])), 0)
		it.When = started
		if e := num(r["ended_at"]); e > 0 {
			it.When = time.Unix(int64(e), 0)
		}
		it.Detail = fmt.Sprintf("%s · %s msgs · %s", str(r["source"]), str(r["message_count"]), str(r["model"]))
		it.Fields = []KV{{i18n.M("id"), str(r["id"])}, {i18n.M("source"), str(r["source"])}, {i18n.M("chat"), str(r["display_name"])},
			{"model", str(r["model"])}, {"started", started.Format("2006-01-02 15:04")}, {"messages", str(r["message_count"])},
			{"tools", str(r["tool_call_count"])}, {"in tokens", str(r["input_tokens"])}, {"end", str(r["end_reason"])}}
		items = append(items, it)
	}
	return items
}

func shortTime(s string) string {
	t := parseTime(s)
	if t.IsZero() {
		return s
	}
	return t.Local().Format("Mon 15:04")
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// ---------------------------------------------------------------- claude bg jobs

func ClaudeJobs() []Item {
	dirs, _ := os.ReadDir(hp(".claude", "jobs"))
	var items []Item
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		var st struct {
			State     string `json:"state"`
			Detail    string `json:"detail"`
			Needs     string `json:"needs"`
			Name      string `json:"name"`
			SessionID string `json:"sessionId"`
			Cwd       string `json:"cwd"`
			Intent    string `json:"intent"`
			Tokens    int64  `json:"tokens"`
			UpdatedAt int64  `json:"updatedAt"`
			CreatedAt int64  `json:"createdAt"`
			Children  []struct {
				Href string `json:"href"`
				Kind string `json:"kind"`
			} `json:"children"`
			InFlight struct {
				Tasks int `json:"tasks"`
			} `json:"inFlight"`
		}
		dir := hp(".claude", "jobs", d.Name())
		if readJSON(filepath.Join(dir, "state.json"), &st) != nil {
			continue
		}
		it := Item{Group: i18n.M("Claude background jobs"), Name: firstNonEmpty(st.Name, d.Name()), Cwd: st.Cwd, SessionID: st.SessionID,
			Detail: firstNonEmpty(st.Detail, st.Intent), LogPath: filepath.Join(dir, "timeline.jsonl")}
		switch st.State {
		case "working":
			it.Status = Running
		case "blocked":
			it.Status = Blocked
		case "failed", "error":
			it.Status = Failed
		default:
			it.Status = Done
		}
		if st.UpdatedAt > 0 {
			it.When = time.UnixMilli(st.UpdatedAt)
		}
		it.Fields = []KV{{i18n.M("state"), st.State}, {i18n.M("detail"), st.Detail}, {i18n.M("session"), st.SessionID}, {i18n.M("cwd"), st.Cwd},
			{"tokens", strconv.FormatInt(st.Tokens, 10)}, {"in flight", strconv.Itoa(st.InFlight.Tasks)}}
		if st.Needs != "" {
			it.Fields = append(it.Fields, KV{i18n.M("needs"), st.Needs})
		}
		for _, c := range st.Children {
			it.Fields = append(it.Fields, KV{c.Kind, c.Href})
		}
		if st.Intent != "" {
			it.Fields = append(it.Fields, KV{i18n.M("intent"), st.Intent})
		}
		items = append(items, it)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].When.After(items[j].When) })
	var dm struct {
		Pid       int   `json:"supervisorPid"`
		WrittenAt int64 `json:"writtenAt"`
		Workers   map[string]any
	}
	if readJSON(hp(".claude", "daemon.status.json"), &dm) == nil {
		it := Item{Group: i18n.M("Claude background jobs"), Name: "claude daemon", Status: Off, When: time.UnixMilli(dm.WrittenAt),
			LogPath: hp(".claude", "daemon.log")}
		if Alive(dm.Pid) {
			it.Status = Idle
			if len(dm.Workers) > 0 {
				it.Status = Running
			}
		}
		it.Detail = fmt.Sprintf("supervisor pid %d · %d workers", dm.Pid, len(dm.Workers))
		it.Fields = []KV{{i18n.M("pid"), strconv.Itoa(dm.Pid)}, {i18n.M("workers"), strconv.Itoa(len(dm.Workers))}}
		items = append([]Item{it}, items...)
	}
	return items
}

// ---------------------------------------------------------------- pr-loop

func PRLoop() []Item {
	cache := hp(".cache", "pr-loop")
	if _, err := os.Stat(cache); err != nil {
		return nil
	}
	it := Item{Group: i18n.M("Loops"), Name: "pr-loop", Status: Off, LogPath: filepath.Join(cache, "logs", "run.log")}
	cfg := map[string]string{}
	if f, err := os.Open(hp(".claude", "pr-loop", "enabled")); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if k, v, ok := strings.Cut(sc.Text(), "="); ok {
				cfg[strings.TrimSpace(k)] = strings.TrimSpace(v)
			}
		}
		f.Close()
		it.Status = Sched
	}
	readEpoch := func(n string) time.Time {
		b, _ := os.ReadFile(filepath.Join(cache, n))
		s, _ := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
		if s == 0 {
			return time.Time{}
		}
		return time.Unix(s, 0)
	}
	start, end := readEpoch("last-run"), readEpoch("last-end")
	if lockHeld(filepath.Join(cache, "run.lock")) {
		it.Status = Running
	}
	it.When = start
	it.Detail = fmt.Sprintf("every %smin · level %s · last start %s", cfg["interval"], cfg["level"], start.Format("15:04"))
	it.Fields = []KV{{i18n.M("enabled"), strconv.FormatBool(len(cfg) > 0)}, {i18n.M("interval"), cfg["interval"] + " min"}, {i18n.M("level"), cfg["level"]},
		{"last start", start.Format("2006-01-02 15:04:05")}, {"last end", end.Format("2006-01-02 15:04:05")}}
	if tail := lastLines(it.LogPath, 40); tail != nil {
		for i := len(tail) - 1; i >= 0; i-- {
			if strings.Contains(tail[i], "exit=") {
				it.Fields = append(it.Fields, KV{i18n.M("last exit"), strings.TrimSpace(tail[i])})
				if !strings.Contains(tail[i], "exit=0") && it.Status != Running {
					it.Status = Failed
				}
				break
			}
		}
	}
	return []Item{it}
}

// lockHeld tries a non-blocking flock; failure means someone holds it.
func lockHeld(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return true
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false
}

func lastLines(path string, n int) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	st, _ := f.Stat()
	off := st.Size() - 16*1024
	if off < 0 {
		off = 0
	}
	buf := make([]byte, st.Size()-off)
	_, _ = f.ReadAt(buf, off)
	lines := strings.Split(strings.TrimRight(string(buf), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

// Tail exposes lastLines for the UI log preview.
func Tail(path string, n int) []string { return lastLines(path, n) }

// ---------------------------------------------------------------- cron

func Cron() []Item {
	if os.Getenv("STAMPEDE_DEMO") != "" { // crontab is per-user, not per-$HOME
		return nil
	}
	out, err := exec.Command("crontab", "-l").Output()
	if err != nil {
		return nil
	}
	var items []Item
	for _, l := range strings.Split(string(out), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		low := strings.ToLower(l)
		agent := strings.Contains(low, "claude") || strings.Contains(low, "hermes") || strings.Contains(low, "agent")
		f := strings.Fields(l)
		sched, cmd := "", l
		if strings.HasPrefix(l, "@") && len(f) > 1 {
			sched, cmd = f[0], strings.Join(f[1:], " ")
		} else if len(f) > 5 {
			sched, cmd = strings.Join(f[:5], " "), strings.Join(f[5:], " ")
		}
		name := filepath.Base(strings.Fields(cmd)[len(strings.Fields(cmd))-1])
		for _, w := range strings.Fields(cmd) {
			switch filepath.Base(w) {
			case "python", "python3", "bash", "sh", "zsh", "node", "php", "env", "uv":
				continue // the interpreter: the script after it names the job
			}
			if strings.Contains(w, "/") && !strings.HasPrefix(w, "-") && !strings.Contains(w, "=") {
				name = filepath.Base(filepath.Dir(w)) + "/" + filepath.Base(w)
				break
			}
		}
		when := Schedule(sched)
		it := Item{Group: i18n.M("Cron"), Name: name, Status: Sched, Detail: when + " · " + strings.ReplaceAll(cmd, home, "~"),
			Fields: []KV{{i18n.M("when"), when}, {i18n.M("schedule"), sched}, {i18n.M("command"), strings.ReplaceAll(cmd, home, "~")}}}
		if agent {
			it.Fields = append(it.Fields, KV{i18n.M("runs"), i18n.T("an agent")})
		}
		it.LogPath = cronLog(cmd)
		items = append(items, it)
	}
	return items
}

// ---------------------------------------------------------------- opencode

func OpenCode() []Item {
	rows := sqlite(hp(".local", "share", "opencode", "opencode.db"),
		`SELECT id, title, directory, model, tokens_input, tokens_output, cost, time_updated, parent_id
		 FROM session WHERE time_archived IS NULL AND parent_id IS NULL ORDER BY time_updated DESC LIMIT 15`)
	var items []Item
	for _, r := range rows {
		model := str(r["model"])
		var m struct {
			ID string `json:"id"`
		}
		if json.Unmarshal([]byte(model), &m) == nil && m.ID != "" {
			model = m.ID
		}
		it := Item{Group: i18n.M("opencode"), Name: firstNonEmpty(str(r["title"]), str(r["id"])), Status: Done,
			Cwd: str(r["directory"]), When: time.UnixMilli(int64(num(r["time_updated"]))),
			Cmd: "opencode -s " + str(r["id"])}
		it.Detail = fmt.Sprintf("%s · %s", model, strings.ReplaceAll(it.Cwd, home, "~"))
		it.Fields = []KV{{i18n.M("id"), str(r["id"])}, {i18n.M("dir"), it.Cwd}, {i18n.M("model"), model},
			{"tokens", str(r["tokens_input"]) + " in / " + str(r["tokens_output"]) + " out"}, {"cost", "$" + str(r["cost"])}}
		items = append(items, it)
	}
	return items
}

// ---------------------------------------------------------------- user watchers

// Watch mirrors config.Watch to keep this package free of the config import.
type Watch struct{ Name, Group, Command, Log, Cwd, Open string }

func expand(p string) string {
	if strings.HasPrefix(p, "~/") {
		return hp(p[2:])
	}
	return p
}

// Watchers runs every user command concurrently with a 5s timeout.
func Watchers(ws []Watch) []Item {
	items := make([]Item, len(ws))
	done := make(chan struct{}, len(ws))
	for i, w := range ws {
		go func(i int, w Watch) {
			defer func() { done <- struct{}{} }()
			g := w.Group
			if g == "" {
				g = i18n.M("Watchers")
			}
			it := Item{Group: g, Name: w.Name, Status: Idle, When: time.Now(), Cwd: expand(w.Cwd), LogPath: expand(w.Log), Cmd: w.Open}
			if w.Command != "" {
				cmd := exec.Command("sh", "-c", w.Command)
				cmd.Dir = it.Cwd
				type res struct {
					out []byte
					err error
				}
				ch := make(chan res, 1)
				go func() { o, e := cmd.CombinedOutput(); ch <- res{o, e} }()
				select {
				case r := <-ch:
					out := strings.TrimSpace(string(r.out))
					lines := strings.Split(out, "\n")
					it.Detail = lines[0]
					if r.err != nil {
						it.Status = Failed
						it.Fields = append(it.Fields, KV{i18n.M("error"), r.err.Error()})
					}
					if len(lines) > 1 {
						it.Fields = append(it.Fields, KV{i18n.M("output"), strings.Join(lines[:min(len(lines), 12)], "\n")})
					}
				case <-time.After(5 * time.Second):
					_ = cmd.Process.Kill()
					it.Status, it.Detail = Failed, "timed out after 5s"
				}
				it.Fields = append([]KV{{i18n.M("command"), w.Command}}, it.Fields...)
			}
			items[i] = it
		}(i, w)
	}
	for range ws {
		<-done
	}
	return items
}
