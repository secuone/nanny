package skills

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The rest of the harness Claude Code runs with, next to skills, commands and
// agents: hooks, MCP servers, plugins, settings files and CLAUDE.md memory,
// for the user and for each project.
const (
	Hook     Kind = "hook"
	MCP      Kind = "mcp"
	Plugin   Kind = "plugin"
	Settings Kind = "settings"
	Memory   Kind = "memory"
)

// Harness reports whether k is part of the harness rather than a skill,
// command or agent (those move between dirs to turn off; these don't).
func (k Kind) Harness() bool { return k != Skill && k != Command && k != Agent }

// ScanHarness reads the user's and each project's harness config.
func ScanHarness(home string, projects []string) []Item {
	var out []Item
	cd := claudeDir(home)
	for _, f := range []string{"settings.json", "settings.local.json"} {
		out = append(out, scanSettings(filepath.Join(cd, f), "user", "")...)
	}
	out = append(out, scanClaudeJSON(filepath.Join(home, ".claude.json"), projects)...)
	out = append(out, memoryItem(filepath.Join(cd, "CLAUDE.md"), "user", "")...)
	for _, p := range projects {
		owner := filepath.Base(p)
		for _, f := range []string{"settings.json", "settings.local.json"} {
			out = append(out, scanSettings(filepath.Join(p, ".claude", f), "project", owner)...)
		}
		out = append(out, scanMCPFile(filepath.Join(p, ".mcp.json"), "project", owner)...)
		for _, f := range []string{"CLAUDE.md", "CLAUDE.local.md", filepath.Join(".claude", "CLAUDE.md")} {
			out = append(out, memoryItem(filepath.Join(p, f), "project", owner)...)
		}
	}
	return out
}

func stat(it *Item) {
	if st, err := os.Stat(it.Path); err == nil {
		it.Size, it.Modified = st.Size(), st.ModTime()
	}
}

// scanSettings turns a settings file into one Settings item (model,
// permissions, env names) plus one Hook item per hook command and one Plugin
// item per enabled/disabled plugin.
func scanSettings(path, scope, owner string) []Item {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var s struct {
		Model       string `json:"model"`
		Permissions struct {
			Allow, Deny, Ask      []string
			DefaultMode           string `json:"defaultMode"`
			AdditionalDirectories []string
		} `json:"permissions"`
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Type, Command string
				Timeout       int
			} `json:"hooks"`
		} `json:"hooks"`
		EnabledPlugins map[string]bool           `json:"enabledPlugins"`
		Env            map[string]string         `json:"env"`
		StatusLine     *struct{ Command string } `json:"statusLine"`
		OutputStyle    string                    `json:"outputStyle"`
	}
	if json.Unmarshal(b, &s) != nil {
		return []Item{{Kind: Settings, Name: filepath.Base(path), Scope: scope, Owner: owner, Path: path, Root: path,
			Description: "not valid JSON"}}
	}
	set := Item{Kind: Settings, Name: filepath.Base(path), Scope: scope, Owner: owner, Path: path, Root: path}
	stat(&set)
	var sum []string
	if s.Model != "" {
		sum = append(sum, "model "+s.Model)
		set.Detail = append(set.Detail, "model: "+s.Model)
	}
	p := s.Permissions
	if n := len(p.Allow) + len(p.Deny) + len(p.Ask); n > 0 {
		sum = append(sum, fmt.Sprintf("%d allow · %d deny · %d ask", len(p.Allow), len(p.Deny), len(p.Ask)))
	}
	if p.DefaultMode != "" {
		set.Detail = append(set.Detail, "permission mode: "+p.DefaultMode)
	}
	for _, r := range p.Allow {
		set.Detail = append(set.Detail, "allow: "+r)
	}
	for _, r := range p.Deny {
		set.Detail = append(set.Detail, "deny: "+r)
	}
	for _, r := range p.Ask {
		set.Detail = append(set.Detail, "ask: "+r)
	}
	for _, d := range p.AdditionalDirectories {
		set.Detail = append(set.Detail, "extra dir: "+d)
	}
	if len(s.Env) > 0 { // names only: values are often keys and tokens
		var names []string
		for k := range s.Env {
			names = append(names, k)
		}
		sort.Strings(names)
		sum = append(sum, fmt.Sprintf("%d env", len(names)))
		set.Detail = append(set.Detail, "env: "+strings.Join(names, " "))
	}
	if s.StatusLine != nil && s.StatusLine.Command != "" {
		set.Detail = append(set.Detail, "status line: "+s.StatusLine.Command)
	}
	if s.OutputStyle != "" {
		set.Detail = append(set.Detail, "output style: "+s.OutputStyle)
	}
	out := []Item{}
	nHooks := 0
	events := make([]string, 0, len(s.Hooks))
	for ev := range s.Hooks {
		events = append(events, ev)
	}
	sort.Strings(events)
	for _, ev := range events {
		for _, g := range s.Hooks[ev] {
			for _, h := range g.Hooks {
				nHooks++
				name := ev
				if g.Matcher != "" {
					name += " · " + g.Matcher
				}
				it := Item{Kind: Hook, Name: name, Description: h.Command, Scope: scope, Owner: owner, Path: path, Root: path,
					Detail: []string{"event: " + ev, "matcher: " + firstNonEmpty(g.Matcher, "(every tool)"), "type: " + h.Type, "command: " + h.Command}}
				if h.Timeout > 0 {
					it.Detail = append(it.Detail, fmt.Sprintf("timeout: %ds", h.Timeout))
				}
				it.Detail = append(it.Detail, "from: "+filepath.Base(path))
				stat(&it)
				out = append(out, it)
			}
		}
	}
	if nHooks > 0 {
		sum = append(sum, fmt.Sprintf("%d hooks", nHooks))
	}
	plugins := make([]string, 0, len(s.EnabledPlugins))
	for id := range s.EnabledPlugins {
		plugins = append(plugins, id)
	}
	sort.Strings(plugins)
	for _, id := range plugins {
		name, market, _ := strings.Cut(id, "@")
		it := Item{Kind: Plugin, Name: name, Description: "from " + market, Scope: scope, Owner: owner, Path: path, Root: path,
			Disabled: !s.EnabledPlugins[id], Detail: []string{"plugin: " + id, "marketplace: " + market, "set in: " + filepath.Base(path)}}
		stat(&it)
		out = append(out, it)
	}
	if len(plugins) > 0 {
		sum = append(sum, fmt.Sprintf("%d plugins", len(plugins)))
	}
	set.Description = strings.Join(sum, " · ")
	if set.Description == "" {
		set.Description = "no settings"
	}
	return append([]Item{set}, out...)
}

type mcpServer struct {
	Type    string            `json:"type"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	URL     string            `json:"url"`
	Env     map[string]string `json:"env"`
	Headers map[string]string `json:"headers"`
}

func mcpItems(servers map[string]mcpServer, path, scope, owner, how string) []Item {
	names := make([]string, 0, len(servers))
	for n := range servers {
		names = append(names, n)
	}
	sort.Strings(names)
	var out []Item
	for _, n := range names {
		sv := servers[n]
		typ := firstNonEmpty(sv.Type, "stdio")
		if sv.Type == "" && sv.URL != "" {
			typ = "http"
		}
		what := strings.TrimSpace(sv.Command + " " + strings.Join(maskArgs(sv.Args), " "))
		if sv.URL != "" {
			what = maskURL(sv.URL)
		}
		it := Item{Kind: MCP, Name: n, Description: typ + " · " + what, Scope: scope, Owner: owner, Path: path, Root: path,
			Detail: []string{"transport: " + typ}}
		if sv.URL != "" {
			it.Detail = append(it.Detail, "url: "+maskURL(sv.URL))
		} else {
			it.Detail = append(it.Detail, "command: "+what)
		}
		for _, kv := range []map[string]string{sv.Env, sv.Headers} { // names only: values are secrets
			var ks []string
			for k := range kv {
				ks = append(ks, k)
			}
			sort.Strings(ks)
			if len(ks) > 0 {
				it.Detail = append(it.Detail, "env/headers: "+strings.Join(ks, " "))
			}
		}
		it.Detail = append(it.Detail, "set in: "+how)
		stat(&it)
		out = append(out, it)
	}
	return out
}

// scanClaudeJSON reads the MCP servers claude keeps in ~/.claude.json: the
// user's, and per project for the projects given.
func scanClaudeJSON(path string, projects []string) []Item {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var d struct {
		MCPServers map[string]mcpServer `json:"mcpServers"`
		Projects   map[string]struct {
			MCPServers map[string]mcpServer `json:"mcpServers"`
		} `json:"projects"`
	}
	if json.Unmarshal(b, &d) != nil {
		return nil
	}
	out := mcpItems(d.MCPServers, path, "user", "", "~/.claude.json (claude mcp add -s user)")
	for _, p := range projects {
		if pr, ok := d.Projects[p]; ok {
			out = append(out, mcpItems(pr.MCPServers, path, "project", filepath.Base(p), "~/.claude.json (claude mcp add, local to "+filepath.Base(p)+")")...)
		}
	}
	return out
}

func scanMCPFile(path, scope, owner string) []Item {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var d struct {
		MCPServers map[string]mcpServer `json:"mcpServers"`
	}
	if json.Unmarshal(b, &d) != nil {
		return nil
	}
	return mcpItems(d.MCPServers, path, scope, owner, ".mcp.json (shared with the repo)")
}

// memoryItem is a CLAUDE.md file, described by its first heading or line.
func memoryItem(path, scope, owner string) []Item {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	it := Item{Kind: Memory, Name: filepath.Base(path), Scope: scope, Owner: owner, Path: path, Root: path}
	if rel := filepath.Base(filepath.Dir(path)); rel == ".claude" && scope == "project" {
		it.Name = ".claude/" + it.Name
	}
	sc := bufio.NewScanner(f)
	lines := 0
	for sc.Scan() {
		lines++
		if l := strings.TrimSpace(strings.TrimLeft(sc.Text(), "#")); it.Description == "" && l != "" {
			it.Description = l
		}
	}
	it.Description = fmt.Sprintf("%d lines · %s", lines, it.Description)
	stat(&it)
	return []Item{it}
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

var secretWords = []string{"key", "token", "secret", "password", "passwd", "auth", "credential"}

func secretName(s string) bool {
	s = strings.ToLower(s)
	for _, w := range secretWords {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

// maskArgs hides the values of arguments that look like keys or tokens
// (--api-key X, --token=X, API_KEY=X), so the screen never shows them.
func maskArgs(args []string) []string {
	out := make([]string, len(args))
	hideNext := false
	for i, a := range args {
		switch {
		case hideNext && !strings.HasPrefix(a, "-"):
			out[i] = "•••"
			hideNext = false
			continue
		case strings.Contains(a, "="):
			if k, _, _ := strings.Cut(a, "="); secretName(k) {
				out[i] = k + "=•••"
				continue
			}
		case strings.HasPrefix(a, "-") && secretName(a):
			hideNext = true
		}
		out[i] = a
	}
	return out
}

// maskURL drops a URL's query values that look like keys or tokens.
func maskURL(u string) string {
	base, q, ok := strings.Cut(u, "?")
	if !ok {
		return u
	}
	parts := strings.Split(q, "&")
	for i, p := range parts {
		if k, _, _ := strings.Cut(p, "="); secretName(k) {
			parts[i] = k + "=•••"
		}
	}
	return base + "?" + strings.Join(parts, "&")
}
