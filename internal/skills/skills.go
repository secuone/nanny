// Package skills catalogs Claude Code skills, slash commands and subagent
// definitions from the user dir, project dirs and installed plugins.
package skills

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Kind string

const (
	Skill   Kind = "skill"
	Command Kind = "command"
	Agent   Kind = "agent"
)

type Item struct {
	Kind        Kind
	Name        string
	Description string
	Scope       string // user | project | plugin
	Owner       string // project folder name or plugin id
	Path        string // SKILL.md / command.md / agent.md
	Root        string // the skill folder (skills) or the file itself
	Disabled    bool
	Uses        int
	LastUsed    time.Time
	Size        int64
	Modified    time.Time
	Detail      []string // harness items: what they set, "key: value" lines
}

// Editable reports whether nanny may move/rename it (plugins are managed by claude).
func (i Item) Editable() bool { return i.Scope != "plugin" }

// Invocation is how you call it from a prompt.
func (i Item) Invocation() string {
	switch i.Kind {
	case Agent:
		return "@" + i.Name
	}
	if i.Scope == "plugin" && i.Owner != "" {
		return "/" + strings.SplitN(i.Owner, "@", 2)[0] + ":" + i.Name
	}
	return "/" + i.Name
}

func claudeDir(home string) string { return filepath.Join(home, ".claude") }

// Scan collects everything. projects are folders whose .claude/ dir is inspected.
func Scan(home string, projects []string) []Item {
	var out []Item
	cd := claudeDir(home)
	out = append(out, scanBase(cd, "user", "")...)
	seen := map[string]bool{cd: true}
	for _, p := range projects {
		d := filepath.Join(p, ".claude")
		if seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, scanBase(d, "project", filepath.Base(p))...)
	}
	out = append(out, scanPlugins(cd)...)
	usage(home, out)
	out = append(out, ScanHarness(home, projects)...)
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].Kind != out[b].Kind {
			return kindRank(out[a].Kind) < kindRank(out[b].Kind)
		}
		if out[a].Uses != out[b].Uses {
			return out[a].Uses > out[b].Uses
		}
		return out[a].Name < out[b].Name
	})
	return out
}

func kindRank(k Kind) int {
	for i, o := range Kinds {
		if o == k {
			return i
		}
	}
	return len(Kinds)
}

// Kinds in the order they are listed.
var Kinds = []Kind{Skill, Command, Agent, Hook, MCP, Plugin, Memory, Settings}

func scanBase(base, scope, owner string) []Item {
	var out []Item
	for _, disabled := range []bool{false, true} {
		suf := ""
		if disabled {
			suf = "-disabled"
		}
		// skills/<name>/SKILL.md
		dirs, _ := os.ReadDir(filepath.Join(base, "skills"+suf))
		for _, d := range dirs {
			if !d.IsDir() {
				continue
			}
			root := filepath.Join(base, "skills"+suf, d.Name())
			md := filepath.Join(root, "SKILL.md")
			if it, ok := readItem(md, Skill, d.Name()); ok {
				it.Scope, it.Owner, it.Root, it.Disabled = scope, owner, root, disabled
				out = append(out, it)
			}
		}
		// commands/**/*.md → name with ":" for subfolders
		cdir := filepath.Join(base, "commands"+suf)
		_ = filepath.WalkDir(cdir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
				return nil
			}
			rel, _ := filepath.Rel(cdir, p)
			name := strings.ReplaceAll(strings.TrimSuffix(rel, ".md"), string(os.PathSeparator), ":")
			if it, ok := readItem(p, Command, name); ok {
				it.Scope, it.Owner, it.Root, it.Disabled = scope, owner, p, disabled
				out = append(out, it)
			}
			return nil
		})
		// agents/*.md
		as, _ := os.ReadDir(filepath.Join(base, "agents"+suf))
		for _, a := range as {
			if a.IsDir() || !strings.HasSuffix(a.Name(), ".md") {
				continue
			}
			p := filepath.Join(base, "agents"+suf, a.Name())
			if it, ok := readItem(p, Agent, strings.TrimSuffix(a.Name(), ".md")); ok {
				it.Scope, it.Owner, it.Root, it.Disabled = scope, owner, p, disabled
				out = append(out, it)
			}
		}
	}
	return out
}

func scanPlugins(cd string) []Item {
	var inst struct {
		Plugins map[string][]struct {
			Scope       string `json:"scope"`
			InstallPath string `json:"installPath"`
		} `json:"plugins"`
	}
	b, err := os.ReadFile(filepath.Join(cd, "plugins", "installed_plugins.json"))
	if err != nil || json.Unmarshal(b, &inst) != nil {
		return nil
	}
	enabled := enabledPlugins(cd)
	var out []Item
	seen := map[string]bool{}
	ids := make([]string, 0, len(inst.Plugins))
	for id := range inst.Plugins {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		for _, e := range inst.Plugins[id] {
			if seen[e.InstallPath] {
				continue
			}
			seen[e.InstallPath] = true
			for _, it := range scanBase(e.InstallPath, "plugin", id) {
				if !it.Disabled {
					if on, known := enabled[id]; known && !on {
						it.Disabled = true
					}
					out = append(out, it)
				}
			}
		}
	}
	return out
}

func enabledPlugins(cd string) map[string]bool {
	var s struct {
		Enabled map[string]bool `json:"enabledPlugins"`
	}
	b, _ := os.ReadFile(filepath.Join(cd, "settings.json"))
	_ = json.Unmarshal(b, &s)
	return s.Enabled
}

// readItem parses YAML-ish frontmatter (name, description) from a markdown file.
func readItem(path string, k Kind, fallback string) (Item, bool) {
	st, err := os.Stat(path)
	if err != nil {
		return Item{}, false
	}
	it := Item{Kind: k, Name: fallback, Path: path, Size: st.Size(), Modified: st.ModTime()}
	f, err := os.Open(path)
	if err != nil {
		return it, true
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	inFM, lines := false, 0
	var descMulti bool
	var firstPara string
	for sc.Scan() {
		l := sc.Text()
		lines++
		if lines == 1 && strings.TrimSpace(l) == "---" {
			inFM = true
			continue
		}
		if inFM {
			if strings.TrimSpace(l) == "---" {
				inFM = false
				continue
			}
			if descMulti {
				if strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t") {
					it.Description = strings.TrimSpace(it.Description + " " + strings.TrimSpace(l))
					continue
				}
				descMulti = false
			}
			key, val, ok := strings.Cut(l, ":")
			if !ok {
				continue
			}
			val = strings.Trim(strings.TrimSpace(val), `"'`)
			switch strings.TrimSpace(key) {
			case "name":
				if val != "" {
					it.Name = val
				}
			case "description":
				if val == ">" || val == "|" || val == ">-" || val == "|-" || val == "" {
					descMulti = true
				} else {
					it.Description = val
				}
			}
			continue
		}
		if it.Description == "" && firstPara == "" {
			if t := strings.TrimSpace(strings.TrimLeft(l, "# ")); t != "" {
				firstPara = t
			}
		}
		if lines > 60 {
			break
		}
	}
	if it.Description == "" {
		it.Description = firstPara
	}
	return it, true
}

// usage counts "/name" invocations in the prompt history.
func usage(home string, items []Item) {
	f, err := os.Open(filepath.Join(claudeDir(home), "history.jsonl"))
	if err != nil {
		return
	}
	defer f.Close()
	type u struct {
		n    int
		last time.Time
	}
	counts := map[string]*u{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 256*1024), 8<<20)
	for sc.Scan() {
		var h struct {
			Display   string `json:"display"`
			Timestamp int64  `json:"timestamp"`
		}
		if json.Unmarshal(sc.Bytes(), &h) != nil || !strings.HasPrefix(h.Display, "/") {
			continue
		}
		name := strings.TrimPrefix(strings.Fields(h.Display + " ")[0], "/")
		c := counts[name]
		if c == nil {
			c = &u{}
			counts[name] = c
		}
		c.n++
		if t := time.UnixMilli(h.Timestamp); t.After(c.last) {
			c.last = t
		}
	}
	for i := range items {
		key := strings.TrimPrefix(items[i].Invocation(), "/")
		if c, ok := counts[key]; ok && items[i].Kind != Agent {
			items[i].Uses, items[i].LastUsed = c.n, c.last
		}
	}
}

// ---------------------------------------------------------------- actions

// Toggle enables/disables a user or project item by moving it between
// skills/ and skills-disabled/ (same for commands and agents).
func Toggle(it Item) (Item, error) {
	if !it.Editable() {
		return it, errors.New("plugin items are managed with `claude plugin` (enable/disable the plugin)")
	}
	kindDir := map[Kind]string{Skill: "skills", Command: "commands", Agent: "agents"}[it.Kind]
	from := it.Root
	base := filepath.Dir(from)
	if it.Kind == Command { // commands may be nested: climb to the commands dir
		for filepath.Base(base) != kindDir && filepath.Base(base) != kindDir+"-disabled" && base != "/" {
			base = filepath.Dir(base)
		}
	}
	rel, _ := filepath.Rel(base, from)
	parent := filepath.Dir(base)
	target := filepath.Join(parent, kindDir+"-disabled", rel)
	if it.Disabled {
		target = filepath.Join(parent, kindDir, rel)
	}
	if _, err := os.Stat(target); err == nil {
		return it, fmt.Errorf("%s already exists", target)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return it, err
	}
	if err := os.Rename(from, target); err != nil {
		return it, err
	}
	it.Disabled = !it.Disabled
	it.Root = target
	if it.Kind == Skill {
		it.Path = filepath.Join(target, "SKILL.md")
	} else {
		it.Path = target
	}
	return it, nil
}

// NewSkill scaffolds ~/.claude/skills/<name>/SKILL.md (or a project's .claude).
func NewSkill(base, name, description string) (string, error) {
	dir := filepath.Join(base, "skills", name)
	if _, err := os.Stat(dir); err == nil {
		return "", fmt.Errorf("skill %q already exists", name)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if description == "" {
		description = "Describe when Claude should use this skill."
	}
	body := fmt.Sprintf(`---
name: %s
description: %s
---

# %s

## When to use

## Steps

1.
`, name, description, name)
	p := filepath.Join(dir, "SKILL.md")
	return p, os.WriteFile(p, []byte(body), 0o644)
}

// NewItem scaffolds a skill, a slash command or a subagent under base (a
// .claude dir) and returns the file to edit.
func NewItem(base string, k Kind, name string) (string, error) {
	switch k {
	case Command, Agent:
		dir := filepath.Join(base, map[Kind]string{Command: "commands", Agent: "agents"}[k])
		p := filepath.Join(dir, name+".md")
		if _, err := os.Stat(p); err == nil {
			return "", fmt.Errorf("%s %q already exists", k, name)
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
		body := fmt.Sprintf("---\ndescription: Describe what /%s does.\n---\n\n", name)
		if k == Agent {
			body = fmt.Sprintf("---\nname: %s\ndescription: Describe when Claude should hand work to this agent.\ntools: Read, Grep, Glob, Bash\n---\n\nYou are %s. ", name, name)
		}
		return p, os.WriteFile(p, []byte(body), 0o644)
	}
	return NewSkill(base, name, "")
}

// CopyTo copies an item into another .claude dir (e.g. share a user skill with a project).
func CopyTo(it Item, destClaudeDir string) (string, error) {
	kindDir := map[Kind]string{Skill: "skills", Command: "commands", Agent: "agents"}[it.Kind]
	dst := filepath.Join(destClaudeDir, kindDir, filepath.Base(it.Root))
	if _, err := os.Stat(dst); err == nil {
		return "", fmt.Errorf("%s already exists", dst)
	}
	return dst, copyTree(it.Root, dst)
}

func copyTree(src, dst string) error {
	st, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		b, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, b, st.Mode().Perm())
	}
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		t := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(t, 0o755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(t, b, info.Mode().Perm())
	})
}
