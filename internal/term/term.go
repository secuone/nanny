// Package term runs programs in pseudo-terminals and keeps a virtual screen for
// each one, so nanny can show real shells and agents inside its own layout.
package term

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

var paneSeq atomic.Int64

// Pane is one program running in a pty with its emulated screen. The pty lives
// either in a holder process that outlives nanny (hold.go) or here (local).
type Pane struct {
	ID    string
	Cwd   string
	Argv  []string
	Agent string // "claude" when the pane runs Claude Code, else ""
	adopt bool   // attached with no size: it took the program's
	// ReadOnly panes show a log: keys never reach the program. Shell is what
	// s opens next to one (a shell inside the container whose log it is).
	ReadOnly bool
	Shell    []string

	mu        sync.Mutex
	emu       *vt.SafeEmulator
	be        backend
	w, h      int
	title     string
	exited    atomic.Bool
	exitCode  int
	lastOut   atomic.Int64 // unix nano of last output
	bell      atomic.Bool
	quiet     atomic.Bool   // drop emulator replies while replaying old output
	quietTill atomic.Int64  // …and until then (unix ns): the replay's answers drain late
	scroll    int           // lines scrolled back (0 = live)
	seq       atomic.Uint64 // bumped whenever the emulator's screen may have changed
	memoMu    sync.Mutex
	memoAt    time.Time // when memoVal was computed
	memoVal   string
	notify    func()
	closeOnce sync.Once
}

// backend is where the pty lives.
type backend interface {
	write(b []byte)
	resize(w, h int)
	pid() int
	fgPid() int
	socket() string
	kill()   // stop the program
	detach() // let go of it (a local pty dies with it)
}

func newPane(cwd string, argv []string, w, h int, notify func()) *Pane {
	if w < 10 {
		w = 10
	}
	if h < 3 {
		h = 3
	}
	if len(argv) == 0 {
		sh := os.Getenv("SHELL")
		if sh == "" {
			sh = "/bin/sh"
		}
		argv = []string{sh}
	}
	p := &Pane{
		ID:     fmt.Sprintf("p%d", paneSeq.Add(1)),
		Cwd:    cwd,
		Argv:   argv,
		w:      w,
		h:      h,
		emu:    vt.NewSafeEmulator(w, h),
		notify: notify,
	}
	if argv[0] == "claude" || strings.HasSuffix(argv[0], "/claude") {
		p.Agent = "claude"
	}
	p.emu.SetScrollbackSize(5000)
	p.emu.SetCallbacks(vt.Callbacks{
		Bell:  func() { p.bell.Store(true) },
		Title: func(t string) { p.mu.Lock(); p.title = t; p.mu.Unlock() },
	})
	p.lastOut.Store(time.Now().UnixNano())
	return p
}

// sessionVars mark a process as running inside a Claude Code session; a claude
// started with them inherited won't save its transcript, so panes drop them.
var sessionVars = map[string]bool{"CLAUDECODE": true, "CLAUDE_PID": true, "CLAUDE_CODE_ENTRYPOINT": true,
	"CLAUDE_CODE_SESSION_ID": true, "CLAUDE_CODE_CHILD_SESSION": true, "CLAUDE_CODE_SESSION_ATTENDED": true,
	"CLAUDE_CODE_MESSAGING_SOCKET": true, "CLAUDE_CODE_MESSAGING_TOKEN": true, "CLAUDE_CODE_EXECPATH": true}

// baseEnv is nanny's environment plus extra, minus the Claude session markers.
func baseEnv(extra ...string) []string {
	var out []string
	for _, e := range append(os.Environ(), extra...) {
		if k, _, _ := strings.Cut(e, "="); !sessionVars[k] {
			out = append(out, e)
		}
	}
	return out
}

func paneEnv(p *Pane, env []string) []string {
	return append([]string{"TERM=xterm-256color", "COLORTERM=truecolor", "NANNY_PANE=" + p.ID}, env...)
}

// Start launches argv (or the user's shell when empty) in cwd. It runs under a
// holder so it keeps going after nanny quits; if that fails it runs locally.
func Start(cwd string, argv []string, w, h int, env []string, notify func()) (*Pane, error) {
	p := newPane(cwd, argv, w, h, notify)
	env = paneEnv(p, env)
	if os.Getenv("NANNY_NO_HOLD") == "" && os.Getenv(holderEnv) == "" {
		if sock, err := spawnHolder(cwd, p.Argv, p.w, p.h, env); err == nil {
			if err = p.attach(sock, 3*time.Second); err == nil {
				return p, nil
			}
			_ = Kill(sock)
		}
	}
	return p, p.startLocal(env)
}

// StartLocal runs argv in a pty owned by this process (it dies with nanny).
func StartLocal(cwd string, argv []string, w, h int, env []string, notify func()) (*Pane, error) {
	p := newPane(cwd, argv, w, h, notify)
	return p, p.startLocal(paneEnv(p, env))
}

func (p *Pane) startLocal(env []string) error {
	cmd := exec.Command(p.Argv[0], p.Argv[1:]...)
	cmd.Dir = p.Cwd
	cmd.Env = baseEnv(env...)
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(p.w), Rows: uint16(p.h)})
	if err != nil {
		return err
	}
	p.be = &localBackend{f: f, cmd: cmd, p: p}

	// pty → emulator
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := f.Read(buf)
			if n > 0 {
				p.feed(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	go p.replies()
	// reap
	go func() {
		err := cmd.Wait()
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			p.exitCode = ee.ExitCode()
		}
		p.exited.Store(true)
		p.ping()
	}()
	return nil
}

// Note prints s on the pane's screen without sending it to the program.
func (p *Pane) Note(s string) { p.feed([]byte(s)) }

func (p *Pane) feed(b []byte) {
	_, _ = p.emu.Write(b)
	p.seq.Add(1)
	p.lastOut.Store(time.Now().UnixNano())
	p.ping()
}

// replies forwards emulator answers (cursor reports, DA…) to the program.
// Answers to replayed output are dropped — a read started while quiet, or
// done before the replay settles — or they'd be typed into the shell.
func (p *Pane) replies() {
	buf := make([]byte, 4096)
	for {
		q := p.quiet.Load()
		n, err := p.emu.Read(buf)
		if n > 0 && !q && !p.quiet.Load() && time.Now().UnixNano() > p.quietTill.Load() {
			p.be.write(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

func (p *Pane) ping() {
	if p.notify != nil {
		p.notify()
	}
}

// Write sends input bytes to the program. Any input snaps back to the live screen.
func (p *Pane) Write(b []byte) {
	if p.exited.Load() || p.be == nil {
		return
	}
	p.mu.Lock()
	p.scroll = 0
	p.mu.Unlock()
	p.be.write(b)
}

func (p *Pane) Resize(w, h int) {
	if w < 10 || h < 3 {
		return
	}
	p.mu.Lock()
	if w == p.w && h == p.h {
		p.mu.Unlock()
		return
	}
	p.w, p.h = w, h
	p.mu.Unlock()
	p.emu.Resize(w, h)
	p.seq.Add(1)
	if p.be != nil {
		p.be.resize(w, h)
	}
}

// Seq changes whenever the screen may have: a cache of what View drew is good while it holds.
func (p *Pane) Seq() uint64 { return p.seq.Load() }

// Memo is what compute returned within the last ttl, so a frame doesn't redo
// it for every pane. compute runs outside the lock.
func (p *Pane) Memo(ttl time.Duration, compute func() string) string {
	p.memoMu.Lock()
	v, at := p.memoVal, p.memoAt
	p.memoMu.Unlock()
	if !at.IsZero() && time.Since(at) < ttl {
		return v
	}
	v = compute()
	p.memoMu.Lock()
	p.memoVal, p.memoAt = v, time.Now()
	p.memoMu.Unlock()
	return v
}

func (p *Pane) Size() (int, int) { p.mu.Lock(); defer p.mu.Unlock(); return p.w, p.h }

func (p *Pane) Title() string { p.mu.Lock(); defer p.mu.Unlock(); return p.title }

func (p *Pane) Exited() bool { return p.exited.Load() }

func (p *Pane) ExitCode() int { return p.exitCode }

// Pid is the process started in the pane (shell or agent).
func (p *Pane) Pid() int {
	if p.be == nil {
		return 0
	}
	return p.be.pid()
}

// ForegroundPid is the pty's foreground process group (what's running "now").
func (p *Pane) ForegroundPid() int {
	if p.be == nil {
		return 0
	}
	return p.be.fgPid()
}

// Socket is the holder socket keeping the pane alive ("" for a local pty).
func (p *Pane) Socket() string {
	if p.be == nil {
		return ""
	}
	return p.be.socket()
}

// IdleFor reports how long the pane has produced no output.
func (p *Pane) IdleFor() time.Duration {
	return time.Since(time.Unix(0, p.lastOut.Load()))
}

// TakeBell returns true once per bell rung by the program.
func (p *Pane) TakeBell() bool { return p.bell.Swap(false) }

// Scroll moves the viewport into scrollback (positive = up).
func (p *Pane) Scroll(d int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.scroll = max(0, min(p.scroll+d, p.emu.ScrollbackLen()))
}

func (p *Pane) Scrolled() int { p.mu.Lock(); defer p.mu.Unlock(); return p.scroll }

// Close terminates the program (SIGHUP like a closing terminal, then SIGKILL).
func (p *Pane) Close() {
	p.closeOnce.Do(func() {
		if p.be != nil {
			p.be.kill()
		}
		_ = p.emu.Close()
	})
}

// Detach lets go of the pane; under a holder the program keeps running.
func (p *Pane) Detach() {
	p.closeOnce.Do(func() {
		if p.be != nil {
			p.be.detach()
		}
		_ = p.emu.Close()
	})
}

// View renders the screen (or scrollback) as w×h ANSI lines; cursor drawn when focused.
func (p *Pane) View(focused bool) []string {
	p.mu.Lock()
	scroll, h := p.scroll, p.h
	p.mu.Unlock()
	screen := strings.Split(p.emu.Render(), "\n")
	if len(screen) > h {
		screen = screen[:h]
	}
	for len(screen) < h {
		screen = append(screen, "")
	}
	if scroll > 0 {
		sb := p.emu.Scrollback()
		n := sb.Len()
		var lines []string
		for i := n - scroll; i < n; i++ {
			if i >= 0 {
				lines = append(lines, sb.Line(i).Render())
			}
		}
		lines = append(lines, screen...)
		return lines[:h]
	}
	if focused && !p.emu.CursorHidden() && !p.exited.Load() {
		pos := p.emu.CursorPosition()
		if pos.Y >= 0 && pos.Y < len(screen) {
			ch := " "
			if c := p.emu.CellAt(pos.X, pos.Y); c != nil && c.Content != "" {
				ch = c.Content
			}
			l := screen[pos.Y]
			left := ansi.Truncate(l, pos.X, "")
			if lw := ansi.StringWidth(left); lw < pos.X {
				left += strings.Repeat(" ", pos.X-lw)
			}
			right := ansi.TruncateLeft(l, pos.X+ansi.StringWidth(ch), "")
			screen[pos.Y] = left + "\x1b[7m" + ch + "\x1b[27m" + right
		}
	}
	return screen
}

// Text returns the plain text of the last n screen lines (for state detection).
func (p *Pane) Text(n int) string {
	lines := strings.Split(ansi.Strip(p.emu.Render()), "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1] // apps that don't fill the screen
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// ---------------------------------------------------------------- local pty

type localBackend struct {
	f   *os.File
	cmd *exec.Cmd
	p   *Pane
}

func (b *localBackend) write(d []byte) { _, _ = b.f.Write(d) }

func (b *localBackend) resize(w, h int) {
	_ = pty.Setsize(b.f, &pty.Winsize{Cols: uint16(w), Rows: uint16(h)})
}

func (b *localBackend) pid() int { return b.cmd.Process.Pid }

func (b *localBackend) fgPid() int {
	pgid, err := unixTcgetpgrp(int(b.f.Fd()))
	if err != nil {
		return 0
	}
	return pgid
}

func (b *localBackend) socket() string { return "" }

func (b *localBackend) kill() {
	if !b.p.exited.Load() {
		_ = syscall.Kill(-b.cmd.Process.Pid, syscall.SIGHUP)
		_ = b.cmd.Process.Signal(syscall.SIGHUP)
		go func(proc *os.Process) {
			time.Sleep(2 * time.Second)
			if !b.p.exited.Load() {
				_ = proc.Kill()
			}
		}(b.cmd.Process)
	}
	_ = b.f.Close()
}

func (b *localBackend) detach() { b.kill() }

// ---------------------------------------------------------------- agent state

var (
	reWorking = regexp.MustCompile(`(?i)esc to interrupt|ctrl\+c to interrupt|…\s*\(\d+[smh]`)
	reBlocked = regexp.MustCompile(`(?i)do you want to (proceed|make this edit|create|run)|❯\s*1\.\s*yes|would you like to|allow .* to|\(y/n\)|\[y/n\]|enter to (select|confirm)|press enter to continue`)
)

// AgentState guesses idle | working | blocked from what's on screen.
func (p *Pane) AgentState() string {
	if p.exited.Load() {
		return "exited"
	}
	tail := p.Text(14)
	if reBlocked.MatchString(tail) {
		return "blocked"
	}
	if reWorking.MatchString(tail) || p.IdleFor() < 1500*time.Millisecond && p.Agent == "claude" && strings.Contains(tail, "…") {
		return "working"
	}
	return "idle"
}
