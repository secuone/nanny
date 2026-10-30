package term

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestMain doubles as the holder: Start re-execs the test binary as "nanny __hold".
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "__hold" {
		HoldMain(os.Args[2:])
	}
	dir, err := os.MkdirTemp("", "nanny-sock")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("NANNY_SOCK_DIR", dir)
	code := m.Run()
	KillAll()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func alive(pid int) bool { return pid > 0 && syscall.Kill(pid, 0) == nil }

func gone(t *testing.T, pid int, sock string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(sock); err != nil && !alive(pid) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("pid %d / socket %s still around", pid, sock)
}

func TestHolderDetachReattach(t *testing.T) {
	p, err := Start(t.TempDir(), []string{"/bin/sh"}, 60, 10, []string{"PS1=$ "}, nil)
	if err != nil {
		t.Fatal(err)
	}
	sock, pid := p.Socket(), p.Pid()
	if sock == "" || filepath.Dir(sock) != SockDir() || pid == 0 {
		t.Fatalf("not under a holder: sock=%q pid=%d", sock, pid)
	}
	p.Write([]byte("echo before-$((40+2))\r"))
	waitFor(t, p, "before-42")
	if fg := p.ForegroundPid(); fg != pid {
		t.Errorf("foreground pid = %d, want shell %d", fg, pid)
	}
	p.Detach()
	time.Sleep(50 * time.Millisecond)
	if !alive(pid) {
		t.Fatal("detach killed the shell")
	}
	if hs := Holders(); len(hs) != 1 || hs[0].Pid != pid || hs[0].Attached {
		t.Fatalf("holders = %+v", hs)
	}

	q, err := Attach(sock, p.Cwd, p.Argv, 60, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, q, "before-42") // replayed
	if q.Pid() != pid {
		t.Errorf("reattached pid = %d, want %d", q.Pid(), pid)
	}
	q.Write([]byte("echo after-$((1+1))\r"))
	waitFor(t, q, "after-2")

	q.Close()
	gone(t, pid, sock)
	if _, err := Attach(sock, p.Cwd, p.Argv, 60, 10, nil); err == nil {
		t.Error("attached to a dead holder")
	}
}

func TestHolderKicksOldClient(t *testing.T) {
	p, err := Start(t.TempDir(), []string{"/bin/sh"}, 60, 10, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	q, err := Attach(p.Socket(), p.Cwd, p.Argv, 60, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	deadline := time.Now().Add(3 * time.Second)
	for !p.Exited() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !p.Exited() || q.Exited() {
		t.Fatalf("old exited=%v new exited=%v", p.Exited(), q.Exited())
	}
}

func TestHolderExitCode(t *testing.T) {
	p, err := Start(t.TempDir(), []string{"/bin/sh", "-c", "echo bye; sleep 0.2; exit 3"}, 60, 10, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	waitFor(t, p, "bye")
	deadline := time.Now().Add(5 * time.Second)
	for !p.Exited() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !p.Exited() || p.ExitCode() != 3 {
		t.Fatalf("exited=%v code=%d", p.Exited(), p.ExitCode())
	}
}

func TestStaleSocketRemoved(t *testing.T) {
	sock := filepath.Join(SockDir(), "stale.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	l.(*net.UnixListener).SetUnlinkOnClose(false)
	_ = l.Close() // the file stays, nobody listens
	if _, err := Attach(sock, "/", []string{"sh"}, 60, 10, nil); err == nil {
		t.Fatal("attached to a stale socket")
	}
	if _, err := os.Stat(sock); err == nil {
		t.Error("stale socket not removed")
	}
}

// Reattaching replays old output; the emulator's answers to the queries in it
// (cursor reports, DA, colours) must not be typed into the program.
func TestReplayAnswersDropped(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "typed")
	p, err := Start(dir, []string{"/bin/bash", "--norc", "--noprofile"}, 60, 10, []string{"PS1=$ "}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// queries, then drain their live answers, then record what gets typed
	p.Write([]byte(`for i in $(seq 40); do printf '\033[6n\033[c\033]11;?\007'; done; sleep 0.5; while read -t 0.3 -r x; do :; done; stty -echo; echo ready; exec cat > ` + out + "\r"))
	waitFor(t, p, "ready")
	sock, pid := p.Socket(), p.Pid()
	p.Detach()
	time.Sleep(50 * time.Millisecond)
	q, err := Attach(sock, p.Cwd, p.Argv, 60, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, q, "ready")
	time.Sleep(800 * time.Millisecond)
	q.Write([]byte("end\r\x04"))
	gone(t, pid, sock)
	b, _ := os.ReadFile(out)
	if s := string(b); s != "end\n" {
		t.Errorf("the replay's answers reached the program: %q", s)
	}
}

// Reattaching to a shell at its prompt leaves one prompt: no nudge makes it
// print it again, and the replay lands in a screen its own size.
func TestReattachKeepsOnePrompt(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("no zsh")
	}
	p, err := Start(t.TempDir(), []string{zsh, "-f"}, 60, 10, []string{"PS1=zsh-prompt> "}, nil)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, p, "zsh-prompt>")
	sock := p.Socket()
	t.Cleanup(func() { _ = Kill(sock) })
	p.Detach()
	for i, size := range [][2]int{{0, 0}, {0, 0}, {60, 10}, {60, 10}} {
		q, err := Attach(sock, p.Cwd, p.Argv, size[0], size[1], nil)
		if err != nil {
			t.Fatal(err)
		}
		waitFor(t, q, "zsh-prompt>")
		time.Sleep(300 * time.Millisecond) // a redraw would come now
		if w, h := q.Size(); w != 60 || h != 10 {
			t.Errorf("attach %d: size %dx%d, want the shell's 60x10", i, w, h)
		}
		if n := strings.Count(q.Text(30), "zsh-prompt>"); n != 1 {
			t.Fatalf("attach %d: %d prompts:\n%s", i, n, q.Text(30))
		}
		q.Detach()
		time.Sleep(50 * time.Millisecond)
	}
}
