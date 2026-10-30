// Package classroom serves your agents as a 3D classroom in the browser: each
// agent is a teacher with her class, each subagent a kid that walks to the
// books, the computers or the lab by the tool it uses, and finished ones go
// out to play. nanny pushes snapshots; the page polls them.
package classroom

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net"
	"net/http"
	"sync"
	"time"
)

//go:embed web
var web embed.FS

// Kid is a subagent.
type Kid struct {
	ID       string    `json:"id"`
	Type     string    `json:"type"`
	Desc     string    `json:"desc"`
	Tool     string    `json:"tool"` // last tool call, "Bash git status"
	Text     string    `json:"text"` // last thing it said
	Running  bool      `json:"running"`
	Started  time.Time `json:"started"`
	Modified time.Time `json:"modified"`
}

// Class is an agent and its subagents.
type Class struct {
	ID      string  `json:"id"`
	Title   string  `json:"title"`
	Project string  `json:"project"`
	Branch  string  `json:"branch"`
	State   string  `json:"state"` // working, blocked, done, idle
	Lesson  string  `json:"lesson"`
	Prompt  string  `json:"prompt"` // the question waiting for you
	Where   string  `json:"where"`  // space/tab when it runs in nanny
	Cost    float64 `json:"cost"`
	Context int     `json:"context"` // % of the context window used
	Kids    []Kid   `json:"kids"`
}

// Snapshot is what the page draws.
type Snapshot struct {
	Classes []Class           `json:"classes"`
	Words   map[string]string `json:"words"` // the page's labels, translated
	Font    string            `json:"font"`  // CSS font-family for the page; "" keeps its own
	At      time.Time         `json:"at"`
}

// Server serves the page and the latest snapshot on localhost.
type Server struct {
	url   string
	token string
	mu    sync.Mutex
	snap  []byte
}

// Start listens on a free localhost port. The page and its data need a random
// token, so no other site or user on the machine can read your agents.
func Start() (*Server, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	s := &Server{token: hex.EncodeToString(b), snap: []byte(`{"classes":[]}`)}
	s.url = "http://" + ln.Addr().String() + "/" + s.token + "/"
	files, _ := fs.Sub(web, "web")
	mux := http.NewServeMux()
	mux.Handle("/"+s.token+"/", http.StripPrefix("/"+s.token, http.FileServerFS(files)))
	mux.HandleFunc("/"+s.token+"/state.json", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		b := s.snap
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(b)
	})
	host := ln.Addr().String()
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Host != host { // no DNS rebinding
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			mux.ServeHTTP(w, r)
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { _ = srv.Serve(ln) }()
	return s, nil
}

// URL is the page to open.
func (s *Server) URL() string { return s.url }

// Set replaces the snapshot the page polls.
func (s *Server) Set(snap Snapshot) {
	snap.At = time.Now()
	b, err := json.Marshal(snap)
	if err != nil {
		return
	}
	s.mu.Lock()
	s.snap = b
	s.mu.Unlock()
}
