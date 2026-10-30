package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Snippet is a saved prompt the palette and the prompt library type into an agent's pane.
type Snippet struct {
	Name   string   `toml:"name"`
	Text   string   `toml:"text"`
	Tags   []string `toml:"tags,omitempty"`
	Pinned bool     `toml:"pinned,omitempty"`
	// usage lives in the state dir, not in the config file
	Uses int       `toml:"-"`
	Last time.Time `toml:"-"`
}

type snippetFile struct {
	Snippet []Snippet `toml:"snippet"`
}

// DefaultSnippets are used until snippets.toml exists.
var DefaultSnippets = []Snippet{
	{Name: "commit", Text: "Commit the current changes with a clear conventional commit message. Don't push."},
	{Name: "tests", Text: "Run the test suite and fix whatever fails."},
	{Name: "review", Text: "Review your changes for bugs, edge cases and missing tests before we go on."},
	{Name: "pr", Text: "Push the branch and open a pull request with a short summary of the changes."},
	{Name: "explain", Text: "Explain what you changed and why, in a few bullet points."},
	{Name: "ticket", Text: "Read {ticket} and plan the change step by step before writing any code."},
}

func SnippetsPath() string { return filepath.Join(Dir(), "snippets.toml") }

// StateDir is $XDG_STATE_HOME/nanny (default ~/.local/state/nanny).
func StateDir() string {
	if x := os.Getenv("XDG_STATE_HOME"); x != "" {
		return filepath.Join(x, "nanny")
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".local", "state", "nanny")
}

func promptStatsPath() string { return filepath.Join(StateDir(), "prompts.json") }

type promptStat struct {
	Uses int       `json:"uses"`
	Last time.Time `json:"last"`
}

func loadStats() map[string]promptStat {
	st := map[string]promptStat{}
	if b, err := os.ReadFile(promptStatsPath()); err == nil {
		_ = json.Unmarshal(b, &st)
	}
	return st
}

func saveStats(st map[string]promptStat) error {
	if err := os.MkdirAll(StateDir(), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(st, "", "  ")
	return os.WriteFile(promptStatsPath(), b, 0o644)
}

// loadRaw reads snippets.toml in file order, falling back to the defaults.
func loadRaw() []Snippet {
	var f snippetFile
	if _, err := toml.DecodeFile(SnippetsPath(), &f); err != nil {
		return append([]Snippet(nil), DefaultSnippets...)
	}
	var out []Snippet
	for _, s := range f.Snippet {
		if s.Name != "" && s.Text != "" {
			out = append(out, s)
		}
	}
	return out
}

// LoadSnippets reads the saved prompts with usage from the state dir:
// pinned first, then most used, then most recent.
func LoadSnippets() []Snippet {
	out := loadRaw()
	st := loadStats()
	for i := range out {
		if u, ok := st[out[i].Name]; ok {
			out[i].Uses, out[i].Last = u.Uses, u.Last
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Pinned != b.Pinned {
			return a.Pinned
		}
		if a.Uses != b.Uses {
			return a.Uses > b.Uses
		}
		return a.Last.After(b.Last)
	})
	return out
}

// SaveSnippets rewrites snippets.toml with ss.
func SaveSnippets(ss []Snippet) error {
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	clean := make([]Snippet, 0, len(ss))
	for _, s := range ss {
		if strings.TrimSpace(s.Name) != "" && strings.TrimSpace(s.Text) != "" {
			clean = append(clean, s)
		}
	}
	tmp := SnippetsPath() + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, _ = f.WriteString("# Saved prompts: P (or ctrl+k “Prompts…”) opens the library. {name} asks for a value on insert.\n\n")
	if err := toml.NewEncoder(f).Encode(snippetFile{clean}); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, SnippetsPath())
}

// EditSnippets applies fn to the saved prompts in file order and writes them back.
func EditSnippets(fn func([]Snippet) ([]Snippet, error)) error {
	ss, err := fn(loadRaw())
	if err != nil {
		return err
	}
	return SaveSnippets(ss)
}

// UseSnippet counts one use of the saved prompt name.
func UseSnippet(name string) error {
	st := loadStats()
	u := st[name]
	u.Uses++
	u.Last = time.Now()
	st[name] = u
	return saveStats(st)
}

// RenameSnippetStats moves a prompt's usage to a new name; to == "" drops it.
func RenameSnippetStats(from, to string) error {
	st := loadStats()
	u, ok := st[from]
	if !ok || from == to {
		return nil
	}
	delete(st, from)
	if to != "" {
		st[to] = u
	}
	return saveStats(st)
}

// EnsureSnippets writes the defaults to snippets.toml if it doesn't exist yet.
func EnsureSnippets() error {
	if _, err := os.Stat(SnippetsPath()); err == nil {
		return nil
	}
	return SaveSnippets(DefaultSnippets)
}
