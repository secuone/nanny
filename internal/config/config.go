// Package config loads and saves ~/.config/nanny/config.toml.
package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// LaunchMode decides where resumed sessions / shells are opened.
const (
	LaunchAuto   = "auto"   // a new tab in nanny's own spaces
	LaunchInline = "inline" // suspend nanny and run in this terminal
)

type Config struct {
	Theme         string             `toml:"theme"`
	Basic         bool               `toml:"basic,omitempty"`    // legacy: folded into a profile
	Density       string             `toml:"density,omitempty"`  // legacy: folded into a profile
	Profile       string             `toml:"profile"`            // the screen profile in use: basic, full or one of [profiles]
	Profiles      map[string]Profile `toml:"profiles,omitempty"` // your own screen profiles, by name
	Language      string             `toml:"language"`           // en, es, fr, de; "" or "auto" follows $LANG
	Launch        string             `toml:"launch"`
	ClaudeArgs    string             `toml:"claude_args"`    // extra args appended to `claude --resume`
	RefreshSecs   int                `toml:"refresh_secs"`   // live view refresh interval
	MaxSessions   int                `toml:"max_sessions"`   // cap for the explorer (newest first)
	HideBots      bool               `toml:"hide_bots"`      // hide non-interactive/bot sessions (pr-loop caches, -p runs)
	HidePaths     []string           `toml:"hide_paths"`     // substrings of cwd to hide from the explorer
	ExtraRepos    []string           `toml:"extra_repos"`    // repos to scan for worktrees besides those seen in sessions
	Prefix        string             `toml:"prefix"`         // prefix key for pane commands (default ctrl+b)
	Restore       bool               `toml:"restore"`        // reopen spaces/tabs on start, resuming Claude sessions
	NoState       bool               `toml:"-"`              // demo/tests: never write the layout
	DiscoverRoots []string           `toml:"discover_roots"` // folders scanned for git repos by Discover (default: ~/dev, ~/src, …)
	Alerts        Alerts             `toml:"alerts"`
	Queue         Queue              `toml:"queue"`
	Sources       *Sources           `toml:"sources,omitempty"` // legacy: read once and folded into [screen] off
	Watch         []Watch            `toml:"watch"`
	Colors        Palette            `toml:"colors"`           // per-key overrides on top of the theme
	Screen        Screen             `toml:"screen"`           // sidebar agents; the old off list
	Look          Look               `toml:"look"`             // icons, borders, bold/italic/underline, the 3D page's font
	Prices        map[string]Price   `toml:"prices,omitempty"` // per-family overrides of the cost estimate (opus, sonnet, haiku, fable)
	Jira          Jira               `toml:"jira,omitempty"`
	Git           Git                `toml:"git,omitempty"`
	Insights      Insights           `toml:"insights,omitempty"`
}

// Price is USD per million tokens; zero fields keep the built-in price.
type Price struct {
	Input      float64 `toml:"input,omitempty"`
	Output     float64 `toml:"output,omitempty"`
	CacheWrite float64 `toml:"cache_write,omitempty"` // 5-minute cache writes
	CacheRead  float64 `toml:"cache_read,omitempty"`
}

// Watch is a user-defined Radar entry: a command whose exit code is the status
// and whose first output line is the summary. Optional log file to tail.
type Watch struct {
	Name    string `toml:"name"`
	Group   string `toml:"group,omitempty"`
	Command string `toml:"command"`
	Log     string `toml:"log,omitempty"`
	Cwd     string `toml:"cwd,omitempty"`
	Open    string `toml:"open,omitempty"` // command run on enter
}

// Alerts controls what happens when an agent finishes, needs you, or a subagent ends.
type Alerts struct {
	Sound        bool   `toml:"sound"`         // master switch for sounds (S toggles)
	OnDone       bool   `toml:"on_done"`       // an agent finished its turn
	OnBlocked    bool   `toml:"on_blocked"`    // an agent is waiting for you (permission/question)
	OnSubagent   bool   `toml:"on_subagent"`   // a subagent finished
	Toast        bool   `toml:"toast"`         // in-app popup
	Desktop      bool   `toml:"desktop"`       // notify-send / osascript
	SoundDone    string `toml:"sound_done"`    // custom file; empty = system default
	SoundBlocked string `toml:"sound_blocked"` // custom file; empty = system default
	SoundSub     string `toml:"sound_subagent"`
}

// Queue tunes the task queue's dispatcher.
type Queue struct {
	MaxParallel int `toml:"max_parallel"` // queued tasks running at once
}

// Sources is the old way of turning Monitor sources off; Load folds it into
// Screen.Off.
type Sources struct {
	Claude  bool `toml:"claude"`
	Hermes  bool `toml:"hermes"`
	System  bool `toml:"system"`  // cron/timers/daemon/jobs
	Machine bool `toml:"machine"` // load, memory, open ports, containers
}

// Palette is a set of hex colors. Empty fields fall back to the theme.
type Palette struct {
	Bg      string `toml:"bg,omitempty"`
	Surface string `toml:"surface,omitempty"`
	Overlay string `toml:"overlay,omitempty"`
	Text    string `toml:"text,omitempty"`
	Subtle  string `toml:"subtle,omitempty"`
	Muted   string `toml:"muted,omitempty"`
	Accent  string `toml:"accent,omitempty"`
	Accent2 string `toml:"accent2,omitempty"`
	Green   string `toml:"green,omitempty"`
	Yellow  string `toml:"yellow,omitempty"`
	Red     string `toml:"red,omitempty"`
	Blue    string `toml:"blue,omitempty"`
	Peach   string `toml:"peach,omitempty"`
	Teal    string `toml:"teal,omitempty"`
}

func Default() Config {
	return Config{
		Theme:       DefaultTheme,
		Language:    "auto",
		Launch:      LaunchAuto,
		Prefix:      "ctrl+b",
		Restore:     true,
		RefreshSecs: 2,
		MaxSessions: 2000,
		HideBots:    true,
		Alerts:      Alerts{Sound: true, OnDone: true, OnBlocked: true, OnSubagent: false, Toast: true},
		Queue:       Queue{MaxParallel: 2},
	}
}

func Dir() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "nanny")
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".config", "nanny")
}

func Path() string { return filepath.Join(Dir(), "config.toml") }

// Load returns defaults merged with the file, writing the file on first run.
func Load() Config {
	c := Default()
	_ = LoadThemes()
	if _, err := os.Stat(Path()); os.IsNotExist(err) {
		_ = Save(c)
		return c
	}
	_, _ = toml.DecodeFile(Path(), &c)
	c.migrate()
	if c.RefreshSecs < 1 {
		c.RefreshSecs = 2
	}
	if c.Queue.MaxParallel < 1 {
		c.Queue.MaxParallel = 2
	}
	if c.Launch != LaunchInline {
		c.Launch = LaunchAuto
	}
	return c
}

func Save(c Config) error {
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	f, err := os.Create(Path())
	if err != nil {
		return err
	}
	defer f.Close()
	_, _ = f.WriteString("# nanny config — edit here or from the Settings tab (key: 5)\n")
	_, _ = f.WriteString("# themes: " + joinThemes() + "\n")
	_, _ = f.WriteString("# custom Radar entries:\n#   [[watch]]\n#   name = \"docker\"\n#   command = \"docker ps --format '{{.Names}}' | wc -l | xargs printf '%s containers'\"\n#   log = \"~/some/app.log\"\n\n")
	return toml.NewEncoder(f).Encode(c)
}

// Look is how text is drawn: the terminal picks the font, nanny picks the
// glyphs, the borders and the text weights.
type Look struct {
	Icons       string `toml:"icons"`        // auto | unicode | nerd | ascii
	Borders     string `toml:"borders"`      // rounded | square | thick | double | ascii | none
	NoBold      bool   `toml:"no_bold"`      // never bold
	NoItalic    bool   `toml:"no_italic"`    // never italic
	NoUnderline bool   `toml:"no_underline"` // never underlined
	WebFont     string `toml:"web_font"`     // CSS font-family of the 3D page; "" keeps its own
}

// Icon sets and border styles.
var (
	IconSets     = []string{"auto", "unicode", "nerd", "ascii"}
	BorderStyles = []string{"rounded", "square", "thick", "double", "ascii", "none"}
)

// Screen picks bits of the screen on and off.
type Screen struct {
	SidebarAgents string `toml:"sidebar_agents"` // auto (a few in the compact layout) | all
	// legacy, folded into a profile when loaded
	Off       []string `toml:"off,omitempty"`
	HideCost  bool     `toml:"hide_cost,omitempty"`
	HideClock bool     `toml:"hide_clock,omitempty"`
	HideViews []string `toml:"hide_views,omitempty"`
}

// migrate folds the old switches (hide_views, hide_cost, hide_clock,
// [sources], [screen] off, density, basic) into a screen profile, so there
// is one place for what is off.
func (c *Config) migrate() {
	off := &c.Screen.Off
	set := func(id string) { *off = withOff(*off, id, false) }
	for _, v := range c.Screen.HideViews {
		set("view." + strings.ToLower(strings.TrimSpace(v)))
	}
	if c.Screen.HideCost {
		set("header.cost")
	}
	if c.Screen.HideClock {
		set("header.clock")
	}
	if s := c.Sources; s != nil {
		off := map[string]bool{"machine": !s.Machine, "ports": !s.Machine, "containers": !s.Machine, "tilt": !s.Machine,
			"jobs": !s.System, "cron": !s.System, "hermes": !s.Hermes}
		for _, id := range []string{"machine", "ports", "containers", "tilt", "jobs", "cron", "hermes"} {
			if off[id] {
				set("monitor." + id)
			}
		}
	}
	if c.Screen.SidebarAgents == "off" {
		set("sidebar.agents")
		c.Screen.SidebarAgents = ""
	}
	c.foldProfile()
	for name, p := range c.Profiles { // Worktrees became Git
		p.Off = renameOff(p.Off, "view.worktrees", "view.git")
		c.Profiles[name] = p
	}
	c.Screen.HideViews, c.Screen.HideCost, c.Screen.HideClock, c.Sources = nil, false, false, nil
	c.Screen.Off, c.Density, c.Basic = nil, "", false
}

// renameOff renames an id in an off list, in place.
func renameOff(off []string, old, id string) []string {
	i := slices.Index(off, old)
	if i < 0 {
		return off
	}
	off = slices.Clone(off)
	if slices.Contains(off, id) {
		return slices.Delete(off, i, i+1)
	}
	off[i] = id
	return off
}
