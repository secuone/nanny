package insights

import (
	"encoding/gob"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Update brings the cache at cache up to date with the transcripts under
// root and saves it when something new was read. prev is what the caller
// already has; nil loads the cache file.
func Update(root, cache string, since time.Time, prev *Snapshot, prog *Progress) (*Snapshot, error) {
	if prev == nil {
		prev, _ = Load(cache)
	}
	s, err := Refresh(root, since, prev, prog)
	if err == nil && s.changed {
		err = Save(cache, s)
	}
	return s, err
}

// Load reads the cache; a missing, broken or older-parser file is no cache.
func Load(path string) (*Snapshot, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var s Snapshot
	if err := gob.NewDecoder(f).Decode(&s); err != nil {
		return nil, err
	}
	if s.Version != parserVersion || s.Files == nil {
		return nil, errors.New("insights: cache from another parser")
	}
	return &s, nil
}

// Save writes the cache to a temporary file and renames it over path.
func Save(path string, s *Snapshot) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".v1-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := gob.NewEncoder(tmp).Encode(s); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

type job struct {
	path string
	size int64
	mod  int64
	old  *File
}

// Refresh lists the transcripts under root changed since `since` and reads
// the new part of each one, in parallel; the ones that did not change are
// not opened. prev is never modified: a view may be reading it.
func Refresh(root string, since time.Time, prev *Snapshot, prog *Progress) (*Snapshot, error) {
	next := &Snapshot{Version: parserVersion, Files: map[string]*File{}, Built: time.Now()}
	if prev == nil {
		prev = &Snapshot{Files: map[string]*File{}}
	}
	var jobs []job
	err := listTranscripts(root, since, func(path string, size, mod int64) {
		old := prev.Files[path]
		if old != nil && old.Size == size && old.Mod == mod {
			next.Files[path] = old
			return
		}
		jobs = append(jobs, job{path, size, mod, old})
	})
	if err != nil {
		return next, err
	}
	for p := range prev.Files {
		if _, ok := next.Files[p]; !ok {
			next.changed = true // one left the window or was deleted
		}
	}
	if prog != nil {
		prog.Done.Store(0)
		prog.Total.Store(int64(len(jobs)))
	}
	cut := since.Format("2006-01-02")
	var mu sync.Mutex
	ch := make(chan job)
	var wg sync.WaitGroup
	for range max(1, runtime.NumCPU()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range ch {
				f := read(j)
				for d := range f.Days {
					if d < cut {
						delete(f.Days, d)
					}
				}
				mu.Lock()
				next.Files[j.path] = f
				mu.Unlock()
				if prog != nil {
					prog.Done.Add(1)
				}
			}
		}()
	}
	for _, j := range jobs {
		ch <- j
	}
	close(ch)
	wg.Wait()
	next.changed = next.changed || len(jobs) > 0
	return next, nil
}

// read is a copy of the file's tally with its new lines added; a transcript
// that shrank is read again from the start.
func read(j job) *File {
	var f *File
	if j.old != nil && j.size >= j.old.Off && j.size >= j.old.Size {
		f = j.old.clone()
	} else {
		f = newFile(j.path)
	}
	_ = parseFile(j.path, f)
	f.Size, f.Mod = j.size, j.mod
	return f
}

func newFile(path string) *File {
	f := &File{Pending: map[string]pend{}, Days: map[string]*Day{}}
	dir := filepath.Dir(path)
	if filepath.Base(dir) == "subagents" {
		f.Sub = true
		f.Session = filepath.Base(filepath.Dir(dir))
	} else {
		f.Session = strings.TrimSuffix(filepath.Base(path), ".jsonl")
	}
	return f
}

// listTranscripts calls fn for every session and subagent transcript under
// root modified since `since`: <project>/<id>.jsonl and
// <project>/<id>/subagents/agent-*.jsonl.
func listTranscripts(root string, since time.Time, fn func(path string, size, mod int64)) error {
	projects, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	visit := func(dir string, es []os.DirEntry) {
		for _, e := range es {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			if info, err := e.Info(); err == nil && !info.ModTime().Before(since) {
				fn(filepath.Join(dir, e.Name()), info.Size(), info.ModTime().UnixNano())
			}
		}
	}
	for _, p := range projects {
		if !p.IsDir() {
			continue
		}
		dir := filepath.Join(root, p.Name())
		es, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		visit(dir, es)
		for _, e := range es {
			if !e.IsDir() {
				continue
			}
			sd := filepath.Join(dir, e.Name(), "subagents")
			if subs, err := os.ReadDir(sd); err == nil {
				visit(sd, subs)
			}
		}
	}
	return nil
}
