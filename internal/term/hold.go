package term

// A holder is a tiny per-pane process (nanny __hold) that owns the pty, so the
// program keeps running after nanny quits. nanny talks to it over a unix socket
// with frames [type byte][uint32 len][payload]; one client at a time, a new
// attach kicks the old one. The VT emulator stays in nanny: on attach the holder
// replays its last ~2 MB of output into a fresh screen.

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/creack/pty"
)

const (
	// client → holder
	fInput  = 'i'
	fResize = 'r' // uint16 cols, uint16 rows
	fKill   = 'k'
	fHello  = 'h' // attach: become the client, get info + replay
	fQuery  = 'q' // info only, the attached client stays
	// holder → client
	fInfo   = 'I' // HolderInfo JSON
	fReplay = 'R'
	fOutput = 'o'
	fExit   = 'x' // int32 exit code

	ringMax   = 2 << 20
	holderEnv = "NANNY_HOLDER"
)

var sockSeq atomic.Int64

// HolderInfo describes a running holder.
type HolderInfo struct {
	Socket   string    `json:"-"`
	Pid      int       `json:"pid"`
	Cwd      string    `json:"cwd"`
	Argv     []string  `json:"argv"`
	Since    time.Time `json:"since"`
	Attached bool      `json:"attached"`
	Cols     int       `json:"cols,omitempty"` // the program's terminal size now
	Rows     int       `json:"rows,omitempty"`
}

// SockDir is where holder sockets live (NANNY_SOCK_DIR overrides; demo and
// machines without XDG_RUNTIME_DIR use the state dir).
func SockDir() string {
	if d := os.Getenv("NANNY_SOCK_DIR"); d != "" {
		return d
	}
	if x := os.Getenv("XDG_RUNTIME_DIR"); x != "" && os.Getenv("NANNY_DEMO") == "" {
		return filepath.Join(x, "nanny")
	}
	st := os.Getenv("XDG_STATE_HOME")
	if st == "" {
		h, _ := os.UserHomeDir()
		st = filepath.Join(h, ".local", "state")
	}
	return filepath.Join(st, "nanny", "sock")
}

// writeFrame sends one frame whose payload is the parts back to back, without copying them.
func writeFrame(w io.Writer, t byte, parts ...[]byte) error {
	n := 0
	for _, p := range parts {
		n += len(p)
	}
	hdr := make([]byte, 5)
	hdr[0] = t
	binary.BigEndian.PutUint32(hdr[1:], uint32(n))
	bufs := append(net.Buffers{hdr}, parts...)
	_, err := bufs.WriteTo(w)
	return err
}

func readFrame(r io.Reader) (byte, []byte, error) {
	var hdr [5]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, nil, err
	}
	n := binary.BigEndian.Uint32(hdr[1:])
	if n > 64<<20 {
		return 0, nil, errors.New("frame too large")
	}
	p := make([]byte, n)
	_, err := io.ReadFull(r, p)
	return hdr[0], p, err
}

func sizeFrame(w, h int) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint16(b, uint16(w))
	binary.BigEndian.PutUint16(b[2:], uint16(h))
	return b
}

// tpgid reads the foreground process group of pid's terminal from /proc.
func tpgid(pid int) int {
	if pid <= 0 {
		return 0
	}
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0
	}
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return 0
	}
	f := strings.Fields(s[i+1:]) // state ppid pgrp session tty_nr tpgid…
	if len(f) < 6 {
		return 0
	}
	n, _ := strconv.Atoi(f[5])
	return max(n, 0)
}

// ---------------------------------------------------------------- holder side

// HoldMain runs a holder: nanny __hold <socket> <cols> <rows> <cwd> -- argv...
func HoldMain(args []string) {
	if len(args) < 6 || args[4] != "--" {
		fmt.Fprintln(os.Stderr, "usage: nanny __hold <socket> <cols> <rows> <cwd> -- argv...")
		os.Exit(2)
	}
	restoreProcsEnv()
	debug.SetGCPercent(25) // a holder idles: keep its heap near the ring
	debug.SetMemoryLimit(8 << 20)
	w, _ := strconv.Atoi(args[1])
	h, _ := strconv.Atoi(args[2])
	os.Exit(hold(args[0], w, h, args[3], args[5:]))
}

// savedProcsEnv carries the user's GOMAXPROCS through the holder's forced one.
const savedProcsEnv = "NANNY_HOLD_GOMAXPROCS"

// restoreProcsEnv gives the shell the GOMAXPROCS nanny was started with, or none.
func restoreProcsEnv() {
	if v, ok := os.LookupEnv(savedProcsEnv); ok {
		_ = os.Setenv("GOMAXPROCS", v)
	} else {
		_ = os.Unsetenv("GOMAXPROCS")
	}
	_ = os.Unsetenv(savedProcsEnv)
}

// ring keeps the last ringMax bytes of output. It grows to that size once and
// then overwrites in place, so a quiet holder stays small and a busy one never allocates.
type ring struct {
	buf  []byte
	pos  int // where the next byte goes, once buf is full
	full bool
}

func (r *ring) write(p []byte) {
	if len(p) >= ringMax {
		p = p[len(p)-ringMax:]
	}
	if room := ringMax - len(r.buf); room > 0 {
		n := min(room, len(p))
		r.buf = append(r.buf, p[:n]...)
		p = p[n:]
	}
	if len(p) == 0 {
		return
	}
	n := copy(r.buf[r.pos:], p)
	copy(r.buf, p[n:])
	r.pos = (r.pos + len(p)) % ringMax
	r.full = true
}

// replay is the kept output, oldest first, as two slices; once the ring has
// wrapped it starts on a line boundary.
func (r *ring) replay() (a, b []byte) {
	if !r.full {
		return r.buf, nil
	}
	a, b = r.buf[r.pos:], r.buf[:r.pos]
	if i := bytes.IndexByte(a[:min(4096, len(a))], '\n'); i >= 0 {
		a = a[i+1:]
	}
	return a, b
}

type holder struct {
	mu     sync.Mutex
	ring   ring
	client net.Conn
	pty    *os.File
	cmd    *exec.Cmd
	info   HolderInfo
	done   bool
	code   int
	gone   atomic.Bool
	killed sync.Once
}

func hold(sock string, w, h int, cwd string, argv []string) int {
	_, _ = syscall.Setsid()
	_ = os.Unsetenv(holderEnv)
	// caught (not ignored) so the child gets default handlers back
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = cwd
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(max(w, 10)), Rows: uint16(max(h, 3))})
	if err != nil {
		fmt.Fprintln(os.Stderr, "nanny hold:", err)
		return 1
	}
	ln, err := net.Listen("unix", sock)
	if err != nil {
		_ = cmd.Process.Kill()
		return 1
	}
	_ = os.Chmod(sock, 0o600)
	hd := &holder{pty: f, cmd: cmd, info: HolderInfo{Pid: cmd.Process.Pid, Cwd: cwd, Argv: argv, Since: time.Now()}}

	go func() {
		for s := range sig {
			if s == syscall.SIGTERM {
				hd.kill()
			}
		}
	}()
	drained := make(chan struct{})
	go hd.readPty(drained)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go hd.serve(c)
		}
	}()

	err = cmd.Wait()
	hd.gone.Store(true)
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	}
	select { // let the last output through
	case <-drained:
	case <-time.After(200 * time.Millisecond):
	}
	_ = ln.Close()
	_ = os.Remove(sock)
	hd.mu.Lock()
	hd.done, hd.code = true, code
	if hd.client != nil {
		hd.send(fExit, binary.BigEndian.AppendUint32(nil, uint32(int32(code))))
		_ = hd.client.Close()
	}
	hd.mu.Unlock()
	return 0
}

func (h *holder) readPty(done chan struct{}) {
	defer close(done)
	buf := make([]byte, 32<<10)
	for {
		n, err := h.pty.Read(buf)
		if n > 0 {
			h.mu.Lock()
			h.ring.write(buf[:n])
			if h.client != nil {
				h.send(fOutput, buf[:n])
			}
			h.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

// send writes to the attached client (h.mu held); a stuck or gone client is dropped.
func (h *holder) send(t byte, parts ...[]byte) {
	_ = h.client.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if err := writeFrame(h.client, t, parts...); err != nil {
		_ = h.client.Close()
		h.client = nil
	}
}

func (h *holder) infoJSON() []byte {
	in := h.info
	in.Attached = h.client != nil
	if ws, err := pty.GetsizeFull(h.pty); err == nil {
		in.Cols, in.Rows = int(ws.Cols), int(ws.Rows)
	}
	b, _ := json.Marshal(in)
	return b
}

func (h *holder) serve(c net.Conn) {
	defer func() {
		h.mu.Lock()
		if h.client == c {
			h.client = nil
		}
		h.mu.Unlock()
		_ = c.Close()
	}()
	r := bufio.NewReader(c)
	for {
		t, p, err := readFrame(r)
		if err != nil {
			return
		}
		switch t {
		case fHello:
			h.mu.Lock()
			if h.client != nil && h.client != c {
				_ = h.client.Close()
			}
			h.client = c
			h.send(fInfo, h.infoJSON())
			if h.client != nil {
				a, b := h.ring.replay()
				h.send(fReplay, a, b)
			}
			if h.client != nil && h.done {
				h.send(fExit, binary.BigEndian.AppendUint32(nil, uint32(int32(h.code))))
			}
			h.mu.Unlock()
			// nudge full-screen programs to redraw for the new client; not a
			// shell at its prompt, which would print the prompt once more
			if pg := tpgid(h.info.Pid); pg > 0 && !isShell(pg) {
				_ = syscall.Kill(-pg, syscall.SIGWINCH)
			}
		case fQuery:
			h.mu.Lock()
			info := h.infoJSON()
			h.mu.Unlock()
			_ = writeFrame(c, fInfo, info)
			return
		case fInput:
			_, _ = h.pty.Write(p)
		case fResize:
			if len(p) == 4 {
				_ = pty.Setsize(h.pty, &pty.Winsize{Cols: binary.BigEndian.Uint16(p), Rows: binary.BigEndian.Uint16(p[2:])})
			}
		case fKill:
			h.kill()
		}
	}
}

// kill hangs up the program's process group, then SIGKILLs it after 2s.
func (h *holder) kill() {
	h.killed.Do(func() {
		pid := h.info.Pid
		_ = syscall.Kill(-pid, syscall.SIGHUP)
		_ = syscall.Kill(pid, syscall.SIGHUP)
		time.AfterFunc(2*time.Second, func() {
			if !h.gone.Load() {
				_ = syscall.Kill(-pid, syscall.SIGKILL)
				_ = h.cmd.Process.Kill()
			}
		})
	})
}

// ---------------------------------------------------------------- nanny side

// spawnHolder starts a detached holder for argv and waits for its socket.
func spawnHolder(cwd string, argv []string, w, h int, env []string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	dir := SockDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	sock := filepath.Join(dir, fmt.Sprintf("%d-%d.sock", os.Getpid(), sockSeq.Add(1)))
	_ = os.Remove(sock)
	args := append([]string{"nanny", "__hold", sock, strconv.Itoa(w), strconv.Itoa(h), cwd, "--"}, argv...)
	cmd := &exec.Cmd{Path: exe, Args: args, Dir: "/"}
	cmd.Env = append(baseEnv(env...), holderEnv+"=1")
	if v, ok := os.LookupEnv("GOMAXPROCS"); ok {
		cmd.Env = append(cmd.Env, savedProcsEnv+"="+v)
	}
	cmd.Env = append(cmd.Env, "GOMAXPROCS=1") // a holder only copies bytes: one P, far fewer wake-ups
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return "", err
	}
	dead := make(chan struct{})
	go func() { _ = cmd.Wait(); close(dead) }() // reap whenever it ends
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(sock); err == nil {
			return sock, nil
		}
		select {
		case <-dead:
			return "", errors.New("holder exited")
		case <-time.After(5 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			return "", errors.New("holder did not start")
		}
	}
}

// dial connects to a holder, retrying for up to wait. A socket nobody listens
// on is stale and gets removed.
func dial(sock string, wait time.Duration) (net.Conn, error) {
	deadline := time.Now().Add(wait)
	for {
		c, err := net.DialTimeout("unix", sock, time.Second)
		if err == nil {
			return c, nil
		}
		if time.Now().After(deadline) {
			if errors.Is(err, syscall.ECONNREFUSED) {
				_ = os.Remove(sock)
			}
			return nil, err
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// isShell says whether process pid is a shell (a resize makes it redraw its prompt).
func isShell(pid int) bool {
	b, _ := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
	switch strings.TrimSpace(string(b)) {
	case "zsh", "bash", "fish", "sh", "dash", "ksh", "tcsh", "nu":
		return true
	}
	return false
}

// Attach reconnects to a running holder and replays its output into a new
// pane. With no size (w or h 0) the pane takes the program's own, so nothing
// is resized until the pane is laid out.
func Attach(sock, cwd string, argv []string, w, h int, notify func()) (*Pane, error) {
	p := newPane(cwd, argv, w, h, notify)
	p.adopt = w <= 0 || h <= 0
	return p, p.attach(sock, 0)
}

type holdBackend struct {
	sock string
	conn net.Conn
	wmu  sync.Mutex
	cpid int
}

func (b *holdBackend) send(t byte, p []byte) {
	b.wmu.Lock()
	_ = writeFrame(b.conn, t, p)
	b.wmu.Unlock()
}

func (b *holdBackend) write(d []byte)  { b.send(fInput, d) }
func (b *holdBackend) resize(w, h int) { b.send(fResize, sizeFrame(w, h)) }
func (b *holdBackend) pid() int        { return b.cpid }
func (b *holdBackend) fgPid() int      { return tpgid(b.cpid) }
func (b *holdBackend) socket() string  { return b.sock }
func (b *holdBackend) detach()         { _ = b.conn.Close() }
func (b *holdBackend) kill()           { b.send(fKill, nil); _ = b.conn.Close() }

func (p *Pane) attach(sock string, wait time.Duration) error {
	c, err := dial(sock, wait)
	if err != nil {
		return err
	}
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	r := bufio.NewReaderSize(c, 64<<10)
	var in HolderInfo
	if err := writeFrame(c, fHello, nil); err != nil {
		_ = c.Close()
		return err
	}
	t, pl, err := readFrame(r)
	if err == nil && t != fInfo {
		err = errors.New("holder: bad hello")
	}
	if err == nil {
		err = json.Unmarshal(pl, &in)
	}
	if err != nil {
		_ = c.Close()
		return err
	}
	_ = c.SetDeadline(time.Time{})
	// the replay goes into a screen the size it was written for: a shell's
	// prompt redraws (cursor up, clear line) only land right at that width
	if in.Cols >= 10 && in.Rows >= 3 {
		p.emu.Resize(in.Cols, in.Rows)
		p.seq.Add(1)
		if p.adopt {
			p.mu.Lock()
			p.w, p.h = in.Cols, in.Rows
			p.mu.Unlock()
		}
	}
	b := &holdBackend{sock: sock, conn: c, cpid: in.Pid}
	p.be = b
	p.quiet.Store(true) // until the replay is in
	go p.replies()
	go func() {
		for {
			t, pl, err := readFrame(r)
			if err != nil {
				if !p.exited.Load() {
					p.exitCode = -1
					p.exited.Store(true)
					p.ping()
				}
				return
			}
			switch t {
			case fReplay:
				_, _ = p.emu.Write(pl)
				w, h := p.Size()
				p.emu.Resize(w, h)
				p.seq.Add(1)
				p.quietTill.Store(time.Now().Add(400 * time.Millisecond).UnixNano())
				p.quiet.Store(false)
				p.bell.Store(false)
				p.ping()
			case fOutput:
				p.feed(pl)
			case fExit:
				if len(pl) == 4 {
					p.exitCode = int(int32(binary.BigEndian.Uint32(pl)))
				}
				p.exited.Store(true)
				p.ping()
			}
		}
	}()
	b.resize(p.w, p.h)
	return nil
}

// Holders lists running holders in SockDir, removing stale sockets.
func Holders() []HolderInfo {
	ents, _ := os.ReadDir(SockDir())
	var out []HolderInfo
	for _, e := range ents {
		if !strings.HasSuffix(e.Name(), ".sock") {
			continue
		}
		sock := filepath.Join(SockDir(), e.Name())
		c, err := dial(sock, 0)
		if err != nil {
			continue
		}
		_ = c.SetDeadline(time.Now().Add(time.Second))
		var in HolderInfo
		if writeFrame(c, fQuery, nil) == nil {
			if t, pl, err := readFrame(c); err == nil && t == fInfo && json.Unmarshal(pl, &in) == nil {
				in.Socket = sock
				out = append(out, in)
			}
		}
		_ = c.Close()
	}
	return out
}

// Kill stops the program behind a holder socket.
func Kill(sock string) error {
	c, err := dial(sock, 0)
	if err != nil {
		return err
	}
	defer c.Close()
	return writeFrame(c, fKill, nil)
}

// KillAll stops every holder and waits (up to 5s) for them to go; returns how many.
func KillAll() int {
	hs := Holders()
	for _, h := range hs {
		_ = Kill(h.Socket)
	}
	deadline := time.Now().Add(5 * time.Second)
	for _, h := range hs {
		for time.Now().Before(deadline) {
			if _, err := os.Stat(h.Socket); err != nil {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	return len(hs)
}
