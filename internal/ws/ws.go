// Package ws holds nanny's workspaces: spaces → tabs → panes, plus the
// on-disk layout used to restore them (panes still running under a holder are
// reattached; the rest come back relaunched, agents via claude --resume).
package ws

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/secuone/nanny/internal/term"
)

var seq atomic.Int64

func newID(prefix string) string { return fmt.Sprintf("%s%d", prefix, seq.Add(1)) }

type Space struct {
	ID     string
	Name   string
	Icon   string   // optional emoji shown before the name
	Paths  []string // folders that belong to this space; Paths[0] is where new tabs start
	Tabs   []*Tab
	Active int
	// Focus is where its panes are working now (the repos they cd'd into),
	// kept by the UI: a space opened on your home narrows to them.
	Focus []string
}

type Tab struct {
	ID       string
	Label    string
	Panes    []*term.Pane
	Vertical bool // panes side by side (true) or stacked (false)
	Focus    int
	Zoom     bool
	// SessionIDs remembers the Claude session of each pane for restore.
	SessionIDs map[string]string
}

func (s *Space) Dir() string {
	if len(s.Paths) > 0 {
		return s.Paths[0]
	}
	h, _ := os.UserHomeDir()
	return h
}

// Contains reports whether a folder belongs to the space.
func (s *Space) Contains(dir string) bool {
	for _, p := range s.Paths {
		if dir == p || strings.HasPrefix(dir, strings.TrimSuffix(p, "/")+"/") {
			return true
		}
	}
	return false
}

// Covers reports whether a folder is what the space is about, for the views
// scoped to it: its folders, but your home (or /) only counts when its panes
// aren't in any narrower place.
func (s *Space) Covers(dir string) bool {
	for _, p := range s.Dirs() {
		if dir == p || strings.HasPrefix(dir, strings.TrimSuffix(p, "/")+"/") {
			return true
		}
	}
	return false
}

// Dirs are the folders the space's views show.
func (s *Space) Dirs() []string {
	var out []string
	for _, p := range s.Paths {
		if !s.broad(p) {
			out = append(out, p)
		}
	}
	for _, f := range s.Focus {
		if !s.Contains(f) || s.broadOnly() {
			out = append(out, f)
		}
	}
	if len(out) == 0 {
		return s.Paths
	}
	return out
}

// Broad reports whether the space shows everything under your home: opened
// there and with no pane in a narrower place yet.
func (s *Space) Broad() bool {
	for _, d := range s.Dirs() {
		if s.broad(d) {
			return true
		}
	}
	return false
}

func (s *Space) broad(p string) bool {
	h, _ := os.UserHomeDir()
	return p == "/" || p == h || strings.TrimSuffix(p, "/") == h
}

// broadOnly: every folder of the space is home or /.
func (s *Space) broadOnly() bool {
	for _, p := range s.Paths {
		if !s.broad(p) {
			return false
		}
	}
	return true
}

func (s *Space) Tab() *Tab {
	if s.Active < 0 || s.Active >= len(s.Tabs) {
		return nil
	}
	return s.Tabs[s.Active]
}

func (t *Tab) Pane() *term.Pane {
	if t == nil || t.Focus < 0 || t.Focus >= len(t.Panes) {
		return nil
	}
	return t.Panes[t.Focus]
}

func NewSpace(name string, paths ...string) *Space {
	return &Space{ID: newID("s"), Name: name, Paths: paths}
}

func NewTab(label string) *Tab {
	return &Tab{ID: newID("t"), Label: label, SessionIDs: map[string]string{}}
}

// RemovePane drops a pane from the tab (the caller closes it).
func (t *Tab) RemovePane(p *term.Pane) {
	for i, q := range t.Panes {
		if q == p {
			t.Panes = append(t.Panes[:i], t.Panes[i+1:]...)
			delete(t.SessionIDs, p.ID)
			if t.Focus >= len(t.Panes) {
				t.Focus = len(t.Panes) - 1
			}
			return
		}
	}
}

// ---------------------------------------------------------------- persistence

type savedPane struct {
	Cwd       string   `json:"cwd"`
	Argv      []string `json:"argv,omitempty"`
	Agent     string   `json:"agent,omitempty"`
	SessionID string   `json:"session_id,omitempty"`
	Socket    string   `json:"socket,omitempty"` // holder keeping the pane alive
	Pid       int      `json:"pid,omitempty"`
	ReadOnly  bool     `json:"read_only,omitempty"`
	Shell     []string `json:"shell,omitempty"`
}

type savedTab struct {
	Label    string      `json:"label"`
	Vertical bool        `json:"vertical,omitempty"`
	Panes    []savedPane `json:"panes"`
}

type savedSpace struct {
	Name   string     `json:"name"`
	Icon   string     `json:"icon,omitempty"`
	Paths  []string   `json:"paths"`
	Tabs   []savedTab `json:"tabs"`
	Active int        `json:"active"`
}

type State struct {
	Spaces []savedSpace `json:"spaces"`
	Active int          `json:"active"` // index into Spaces (-1 = home)
	Saved  time.Time    `json:"saved"`
}

func StatePath() string {
	if x := os.Getenv("XDG_STATE_HOME"); x != "" {
		return filepath.Join(x, "nanny", "workspace.json")
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".local", "state", "nanny", "workspace.json")
}

// Save snapshots the layout. Agent panes keep their Claude session id so a
// restore resumes the same conversation.
func Save(spaces []*Space, active int) error {
	st := State{Active: active, Saved: time.Now()}
	for _, s := range spaces {
		ss := savedSpace{Name: s.Name, Icon: s.Icon, Paths: s.Paths, Active: s.Active}
		for _, t := range s.Tabs {
			stb := savedTab{Label: t.Label, Vertical: t.Vertical}
			for _, p := range t.Panes {
				if p.Exited() {
					continue
				}
				sp := savedPane{Cwd: p.Cwd, Agent: p.Agent, SessionID: t.SessionIDs[p.ID], Socket: p.Socket(), Pid: p.Pid(), ReadOnly: p.ReadOnly, Shell: p.Shell}
				if p.Agent == "" {
					sp.Argv = p.Argv
				}
				stb.Panes = append(stb.Panes, sp)
			}
			if len(stb.Panes) > 0 {
				ss.Tabs = append(ss.Tabs, stb)
			}
		}
		st.Spaces = append(st.Spaces, ss)
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(StatePath()), 0o755); err != nil {
		return err
	}
	tmp := StatePath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, StatePath())
}

func Load() (State, error) {
	var st State
	b, err := os.ReadFile(StatePath())
	if err != nil {
		return st, err
	}
	err = json.Unmarshal(b, &st)
	return st, err
}

// Restore rebuilds spaces from a saved state. Panes whose holder is still alive
// are reattached with attach (always: they are running); the rest are relaunched
// with spawn when restorePanes is true, otherwise only spaces and folders come back.
func Restore(st State, restorePanes bool, claudeArgs []string,
	spawn func(cwd string, argv []string) (*term.Pane, error),
	attach func(sock, cwd string, argv []string) (*term.Pane, error)) []*Space {
	var out []*Space
	for _, ss := range st.Spaces {
		s := NewSpace(ss.Name, ss.Paths...)
		s.Icon = ss.Icon
		for _, stb := range ss.Tabs {
			t := NewTab(stb.Label)
			t.Vertical = stb.Vertical
			for _, sp := range stb.Panes {
				argv := sp.Argv
				if sp.Agent == "claude" {
					argv = []string{"claude"}
					if sp.SessionID != "" {
						argv = append(argv, "--resume", sp.SessionID)
					}
					argv = append(argv, claudeArgs...)
				}
				var p *term.Pane
				if sp.Socket != "" && attach != nil {
					if q, err := attach(sp.Socket, sp.Cwd, argv); err == nil {
						p = q
						p.Agent = sp.Agent
					}
				}
				if p == nil {
					if !restorePanes {
						continue
					}
					if _, err := os.Stat(sp.Cwd); err != nil {
						continue
					}
					q, err := spawn(sp.Cwd, argv)
					if err != nil {
						continue
					}
					p = q
				}
				p.ReadOnly, p.Shell = sp.ReadOnly, sp.Shell
				t.Panes = append(t.Panes, p)
				if sp.SessionID != "" {
					t.SessionIDs[p.ID] = sp.SessionID
				}
			}
			if len(t.Panes) > 0 {
				s.Tabs = append(s.Tabs, t)
			}
		}
		s.Active = min(ss.Active, max(0, len(s.Tabs)-1))
		out = append(out, s)
	}
	return out
}

// ---------------------------------------------------------------- process tree

// Descendants maps every pid to its children, read from /proc in one pass.
func ProcChildren() map[int][]int {
	kids := map[int][]int{}
	ents, _ := os.ReadDir("/proc")
	for _, e := range ents {
		pid := atoiSafe(e.Name())
		if pid == 0 {
			continue
		}
		b, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue
		}
		s := string(b)
		i := strings.LastIndexByte(s, ')')
		if i < 0 {
			continue
		}
		f := strings.Fields(s[i+1:])
		if len(f) > 1 {
			pp := atoiSafe(f[1])
			kids[pp] = append(kids[pp], pid)
		}
	}
	return kids
}

// Tree returns pid and all its descendants.
func Tree(pid int, kids map[int][]int) []int {
	out := []int{pid}
	for i := 0; i < len(out) && i < 512; i++ {
		out = append(out, kids[out[i]]...)
	}
	return out
}

func atoiSafe(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}
