package ws

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/secuone/nanny/internal/term"
)

// TestMain doubles as the holder: term.Start re-execs the test binary as "nanny __hold".
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "__hold" {
		term.HoldMain(os.Args[2:])
	}
	dir, err := os.MkdirTemp("", "nanny-sock")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("NANNY_SOCK_DIR", dir)
	code := m.Run()
	term.KillAll()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func waitText(t *testing.T, p *term.Pane, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(p.Text(30), want) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("screen never showed %q:\n%s", want, p.Text(30))
}

func TestSaveRestoreReattach(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := t.TempDir()
	start := func(cwd string, argv []string) (*term.Pane, error) {
		return term.Start(cwd, argv, 60, 10, nil, nil)
	}
	attach := func(sock, cwd string, argv []string) (*term.Pane, error) {
		return term.Attach(sock, cwd, argv, 60, 10, nil)
	}

	s := NewSpace("proj", dir)
	tab := NewTab("shells")
	tab.Vertical = true
	live, err := start(dir, []string{"/bin/sh"})
	if err != nil {
		t.Fatal(err)
	}
	dead, err := start(dir, []string{"/bin/sh"})
	if err != nil {
		t.Fatal(err)
	}
	tab.Panes = []*term.Pane{live, dead}
	s.Tabs = []*Tab{tab}
	live.Write([]byte("echo kept-$((6*7))\r"))
	waitText(t, live, "kept-42")
	if err := Save([]*Space{s}, 0); err != nil {
		t.Fatal(err)
	}
	pid := live.Pid()
	live.Detach()
	dead.Close() // its holder goes away: restore relaunches it

	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(dead.Socket()); err != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("killed holder still has its socket")
		}
		time.Sleep(20 * time.Millisecond)
	}

	st, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Spaces[0].Tabs[0].Panes[0]; got.Socket != live.Socket() || got.Pid != pid {
		t.Fatalf("saved pane = %+v", got)
	}
	spaces := Restore(st, true, nil, start, attach)
	if len(spaces) != 1 || len(spaces[0].Tabs) != 1 || len(spaces[0].Tabs[0].Panes) != 2 {
		t.Fatalf("restored %d spaces", len(spaces))
	}
	rt := spaces[0].Tabs[0]
	defer func() {
		for _, p := range rt.Panes {
			p.Close()
		}
	}()
	if !rt.Vertical || rt.Label != "shells" {
		t.Errorf("tab = %+v", rt)
	}
	again, fresh := rt.Panes[0], rt.Panes[1]
	if again.Pid() != pid {
		t.Errorf("reattached pid = %d, want %d", again.Pid(), pid)
	}
	waitText(t, again, "kept-42")
	if fresh.Pid() == dead.Pid() || fresh.Socket() == dead.Socket() {
		t.Error("dead pane was not relaunched")
	}

	// restore off: running panes still come back, dead ones don't
	spaces = Restore(st, false, nil, start, attach)
	if len(spaces[0].Tabs) != 1 || len(spaces[0].Tabs[0].Panes) != 1 || spaces[0].Tabs[0].Panes[0].Pid() != pid {
		t.Fatalf("restore off: %+v", spaces[0].Tabs)
	}
	spaces[0].Tabs[0].Panes[0].Close()
}

func TestSpaceFolder(t *testing.T) {
	legacy := NewSpace("old")
	h, _ := os.UserHomeDir()
	if legacy.HasFolder() || legacy.Dir() != h {
		t.Errorf("a space without a folder works from home: %q", legacy.Dir())
	}
	blank := NewSpace("blank", "")
	if blank.HasFolder() || blank.Contains("/anything") {
		t.Error("an empty path is no folder and must not match everything")
	}
	s := NewSpace("p", "/a", "/b")
	s.SetFolder("/c")
	if s.Dir() != "/c" || strings.Join(s.Paths, ",") != "/c,/b" {
		t.Errorf("the first folder is replaced: %v", s.Paths)
	}
	s.SetFolder("/b")
	if strings.Join(s.Paths, ",") != "/b" {
		t.Errorf("no duplicates: %v", s.Paths)
	}
	legacy.SetFolder("/x")
	if !legacy.HasFolder() || legacy.Dir() != "/x" {
		t.Error("a folder can be set later")
	}
}

// A state saved before spaces had a folder loads, and its panes come back where they were.
func TestRestoreLegacySpaceAndPaneDirs(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := t.TempDir()
	if err := os.MkdirAll(StatePath()[:strings.LastIndex(StatePath(), "/")], 0o755); err != nil {
		t.Fatal(err)
	}
	raw := `{"spaces":[{"name":"old","tabs":[{"label":"t","panes":[{"cwd":"` + dir + `","argv":["/bin/sh"]},{"cwd":"` + dir + `","socket":"/gone/sock"}]}],"active":0}],"active":0}`
	if err := os.WriteFile(StatePath(), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	var started, attached []string
	spawn := func(cwd string, argv []string) (*term.Pane, error) {
		started = append(started, cwd)
		return term.Start(cwd, []string{"/bin/sh"}, 60, 10, nil, nil)
	}
	attach := func(sock, cwd string, argv []string) (*term.Pane, error) {
		attached = append(attached, cwd)
		return nil, os.ErrNotExist
	}
	spaces := Restore(st, true, nil, spawn, attach)
	if len(spaces) != 1 || spaces[0].HasFolder() {
		t.Fatalf("legacy space should load without a folder: %+v", spaces)
	}
	if len(attached) != 1 || attached[0] != dir {
		t.Errorf("reattach asked for %v, want the pane's own folder", attached)
	}
	if len(started) != 2 || started[0] != dir || started[1] != dir {
		t.Errorf("relaunched panes start in %v, want %s", started, dir)
	}
	if err := Save(spaces, 0); err != nil {
		t.Fatal(err)
	}
	again, err := Load()
	if err != nil || len(again.Spaces) != 1 {
		t.Fatalf("saved again: %v %+v", err, again)
	}
}
