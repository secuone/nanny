package claude

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Watcher tells what changed under ~/.claude (transcripts, subagents, live
// processes, history) through inotify, so nothing has to be scanned again
// while nothing changes. It watches ~/.claude, sessions, projects, each
// project, each session's folder and its subagents.
type Watcher struct {
	C chan struct{} // gets a value when something changed; Take says what

	fd      int
	mu      sync.Mutex
	dirs    map[int]string
	changed map[string]bool
	lost    bool // events were dropped (queue overflow, a watch not added): scan everything
}

const watchMask = unix.IN_MODIFY | unix.IN_CREATE | unix.IN_DELETE | unix.IN_MOVED_TO | unix.IN_MOVED_FROM |
	unix.IN_CLOSE_WRITE | unix.IN_DELETE_SELF | unix.IN_ONLYDIR

// Watch starts watching; with an error (no inotify, out of watches) callers
// go on scanning on a timer.
func Watch() (*Watcher, error) {
	fd, err := unix.InotifyInit1(unix.IN_CLOEXEC)
	if err != nil {
		return nil, err
	}
	w := &Watcher{C: make(chan struct{}, 1), fd: fd, dirs: map[int]string{}, changed: map[string]bool{}}
	for _, d := range []string{Dir(), Dir("sessions"), Dir("projects")} {
		if err := w.add(d); err != nil && d != Dir("sessions") {
			_ = unix.Close(fd)
			return nil, err
		}
	}
	projects, _ := os.ReadDir(Dir("projects"))
	for _, p := range projects {
		if p.IsDir() {
			w.addTree(Dir("projects", p.Name()), 1)
		}
	}
	if w.lost { // out of watches: better scan on a timer than miss changes
		_ = unix.Close(fd)
		return nil, fmt.Errorf("inotify: not enough watches for %d folders", len(w.dirs))
	}
	go w.read()
	return w, nil
}

func (w *Watcher) add(dir string) error {
	wd, err := unix.InotifyAddWatch(w.fd, dir, watchMask)
	if err != nil {
		return err
	}
	w.mu.Lock()
	w.dirs[wd] = dir
	w.mu.Unlock()
	return nil
}

// addTree watches a project (depth 1), a session folder (2) and its
// subagents (3); nothing else under a session (tool results) matters.
func (w *Watcher) addTree(dir string, depth int) {
	if depth == 3 && filepath.Base(dir) != "subagents" {
		return
	}
	if err := w.add(dir); err != nil {
		w.mu.Lock()
		w.lost = true
		w.mu.Unlock()
		return
	}
	if depth == 3 {
		return
	}
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if e.IsDir() {
			w.addTree(filepath.Join(dir, e.Name()), depth+1)
		}
	}
}

// depth of a folder under projects: 1 a project, 2 a session, 3 below it.
func depth(dir string) int {
	rel, err := filepath.Rel(Dir("projects"), dir)
	if err != nil || strings.HasPrefix(rel, "..") {
		return -1
	}
	if rel == "." {
		return 0
	}
	return strings.Count(rel, string(filepath.Separator)) + 1
}

func (w *Watcher) read() {
	buf := make([]byte, 64<<10)
	for {
		n, err := unix.Read(w.fd, buf)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			return
		}
		w.mu.Lock()
		for off := 0; off+unix.SizeofInotifyEvent <= n; {
			ev := (*unix.InotifyEvent)(unsafe.Pointer(&buf[off]))
			name := ""
			if ev.Len > 0 {
				b := buf[off+unix.SizeofInotifyEvent : off+unix.SizeofInotifyEvent+int(ev.Len)]
				name = strings.TrimRight(string(b), "\x00")
			}
			off += unix.SizeofInotifyEvent + int(ev.Len)
			switch {
			case ev.Mask&unix.IN_Q_OVERFLOW != 0:
				w.lost = true
				continue
			case ev.Mask&unix.IN_IGNORED != 0:
				delete(w.dirs, int(ev.Wd))
				continue
			}
			dir, ok := w.dirs[int(ev.Wd)]
			if !ok {
				continue
			}
			if dir == Dir() && name != "history.jsonl" && name != "sessions" && name != "projects" {
				continue // the rest of ~/.claude (todos, debug, statsig…) changes all the time
			}
			p := filepath.Join(dir, name)
			w.changed[p] = true
			if ev.Mask&unix.IN_ISDIR != 0 && ev.Mask&(unix.IN_CREATE|unix.IN_MOVED_TO) != 0 {
				d := depth(p)
				w.mu.Unlock()
				switch {
				case p == Dir("sessions") || p == Dir("projects"):
					_ = w.add(p)
				case d >= 1 && d <= 3:
					w.addTree(p, d)
				}
				w.mu.Lock()
			}
		}
		w.mu.Unlock()
		select {
		case w.C <- struct{}{}:
		default:
		}
	}
}

// Take returns the paths changed since the last call; all is true when some
// changes may have been missed and everything should be looked at again.
func (w *Watcher) Take() (paths map[string]bool, all bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	paths, all = w.changed, w.lost
	w.changed, w.lost = map[string]bool{}, false
	return paths, all
}

// Watches is how many folders are watched.
func (w *Watcher) Watches() int { w.mu.Lock(); defer w.mu.Unlock(); return len(w.dirs) }

func (w *Watcher) Close() { _ = unix.Close(w.fd) }

// SessionOf is the transcript a changed path belongs to (itself, a subagent's
// file, a session folder), or "".
func SessionOf(p string) string {
	if depth(filepath.Dir(p)) < 1 {
		return ""
	}
	rel, _ := filepath.Rel(Dir("projects"), p)
	parts := strings.Split(rel, string(filepath.Separator))
	switch {
	case len(parts) == 2 && strings.HasSuffix(parts[1], ".jsonl"):
		return p
	case len(parts) >= 2:
		return Dir("projects", parts[0], strings.TrimSuffix(parts[1], ".jsonl")+".jsonl")
	}
	return ""
}
