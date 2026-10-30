package claude

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Hit is one full-text match inside a session transcript.
type Hit struct {
	Role    string
	Snippet string
}

var (
	userMark      = []byte(`"type":"user"`)
	assistantMark = []byte(`"type":"assistant"`)
)

// Search greps every main transcript for query (case-insensitive) in user and
// assistant messages. It returns up to maxPerSession hits keyed by session id.
func Search(sessions []*Session, query string, maxPerSession int) map[string][]Hit {
	q := bytes.ToLower([]byte(strings.TrimSpace(query)))
	out := map[string][]Hit{}
	if len(q) == 0 {
		return out
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	jobs := make(chan *Session)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for s := range jobs {
				if hits := searchFile(s.Path, q, string(q), maxPerSession); len(hits) > 0 {
					mu.Lock()
					out[s.ID] = hits
					mu.Unlock()
				}
			}
		}()
	}
	for _, s := range sessions {
		jobs <- s
	}
	close(jobs)
	wg.Wait()
	return out
}

func searchFile(path string, q []byte, qs string, max int) []Hit {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)
	var hits []Hit
	var big []byte
	for {
		line, err := r.ReadSlice('\n')
		if err == bufio.ErrBufferFull {
			// giant line (pasted blobs, tool output): accumulate it
			big = append(big, line...)
			continue
		}
		if big != nil {
			line = append(big, line...)
			big = nil
		}
		if len(line) > 0 && (bytes.Contains(line, userMark) || bytes.Contains(line, assistantMark)) && containsFold(line, q) {
			if h, ok := extractHit(line, qs); ok {
				hits = append(hits, h)
				if len(hits) >= max {
					return hits
				}
			}
		}
		if err != nil {
			break
		}
	}
	return hits
}

func containsFold(b, lowerQ []byte) bool {
	if bytes.Contains(b, lowerQ) {
		return true
	}
	return bytes.Contains(bytes.ToLower(b), lowerQ)
}

func extractHit(line []byte, q string) (Hit, bool) {
	var r rec
	if json.Unmarshal(line, &r) != nil || r.Message == nil || r.IsMeta || r.IsSidechain {
		return Hit{}, false
	}
	var texts []string
	if r.Type == "user" {
		if t := promptText(r.Message.Content); t != "" {
			texts = append(texts, t)
		}
	} else {
		for _, e := range assistantEntries(r.Message.Content, time.Time{}) {
			texts = append(texts, e.Text)
		}
	}
	for _, t := range texts {
		lt := strings.ToLower(t)
		if i := strings.Index(lt, q); i >= 0 {
			return Hit{Role: r.Type, Snippet: snippet(t, i, len(q))}, true
		}
	}
	return Hit{}, false
}

func snippet(t string, i, n int) string {
	start, end := i-60, i+n+100
	if start < 0 {
		start = 0
	}
	if end > len(t) {
		end = len(t)
	}
	for start > 0 && !utf8.RuneStart(t[start]) {
		start--
	}
	for end < len(t) && !utf8.RuneStart(t[end]) {
		end++
	}
	s := oneLine(t[start:end])
	if start > 0 {
		s = "…" + s
	}
	if end < len(t) {
		s += "…"
	}
	return s
}
