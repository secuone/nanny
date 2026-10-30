package config

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// Screen profiles: what shows on screen and what runs. Two are built in —
// basic, the bare minimum, and full, everything — and you make your own,
// each a list of the parts turned off (the ids of Customize, = in nanny).

const (
	ProfileBasic = "basic"
	ProfileFull  = "full"
)

// Profile is one of your own screen setups.
type Profile struct {
	Off []string `toml:"off"` // what is turned off, by id: "view.monitor", "monitor.tilt", "header.clock"…
}

// Layout ids: how much each row says, on top of what shows.
const (
	LayoutDetail = "layout.detail" // detail pane, subagent rows, the full top bar; off: one line per agent
	LayoutExtras = "layout.extras" // model, context gauge always, agents in the sidebar on Live too, every footer key
)

// basicOff is what the basic profile turns off: it keeps the agents, who
// needs you, the spend and the context gauges, and sessions to resume.
var basicOff = []string{
	LayoutDetail, LayoutExtras,
	"view.git", "view.tasks", "view.prompts", "view.skills", "view.monitor", "view.jira", "view.insights",
	"sidebar.pr", "sidebar.worktrees", "sidebar.hermes", "sidebar.jira",
	"header.queue", "header.clock", "header.reviews",
}

// BuiltinProfile says whether a profile is one of nanny's own.
func BuiltinProfile(name string) bool { return name == ProfileBasic || name == ProfileFull }

// ProfileNames are the built-in profiles, then yours by name.
func (c Config) ProfileNames() []string {
	out := []string{ProfileBasic, ProfileFull}
	var mine []string
	for n := range c.Profiles {
		if !BuiltinProfile(n) {
			mine = append(mine, n)
		}
	}
	sort.Strings(mine)
	return append(out, mine...)
}

// ActiveProfile is the profile in use; an unknown one falls back to full.
func (c Config) ActiveProfile() string {
	if _, ok := c.Profiles[c.Profile]; ok && !BuiltinProfile(c.Profile) || c.Profile == ProfileBasic {
		return c.Profile
	}
	return ProfileFull
}

// OffList is what a profile turns off.
func (c Config) OffList(name string) []string {
	switch name {
	case ProfileBasic:
		return basicOff
	case ProfileFull:
		return nil
	}
	return c.Profiles[name].Off
}

// On reports whether a part of nanny (a Customize id) is on in the profile in use.
func (c Config) On(id string) bool { return !slices.Contains(c.OffList(c.ActiveProfile()), id) }

// SetOn turns a part on or off in the profile in use. The built-in ones
// never change: the first change makes your own copy and switches to it.
func (c *Config) SetOn(id string, on bool) {
	name := c.editable()
	p := c.Profiles[name]
	p.Off = withOff(p.Off, id, on)
	c.Profiles[name] = p
}

// SetOff replaces what the profile in use turns off (copying a built-in one).
func (c *Config) SetOff(off []string) {
	name := c.editable()
	c.Profiles[name] = Profile{Off: slices.Clone(off)}
}

// editable is the profile in use, or a fresh copy of it when it is built in.
func (c *Config) editable() string {
	if c.Profiles == nil {
		c.Profiles = map[string]Profile{}
	}
	cur := c.ActiveProfile()
	if !BuiltinProfile(cur) {
		return cur
	}
	name := c.freeName("mine")
	c.Profiles[name] = Profile{Off: slices.Clone(c.OffList(cur))}
	c.Profile = name
	return name
}

func (c Config) freeName(base string) string {
	name := base
	for i := 2; c.hasProfile(name); i++ {
		name = fmt.Sprintf("%s-%d", base, i)
	}
	return name
}

func (c Config) hasProfile(name string) bool {
	_, ok := c.Profiles[name]
	return ok || BuiltinProfile(name)
}

var profileName = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N} _.-]{0,23}$`)

// cleanName checks a name for one of your profiles.
func (c Config) cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case !profileName.MatchString(name):
		return "", fmt.Errorf("letters, digits, space, - _ . (up to 24)")
	case c.hasProfile(name):
		return "", fmt.Errorf("%q already exists", name)
	}
	return name, nil
}

// NewProfile makes a profile of yours from another one and switches to it;
// an empty name picks a free one.
func (c *Config) NewProfile(name, from string) (string, error) {
	if name == "" {
		name = c.freeName("mine")
	}
	name, err := c.cleanName(name)
	if err != nil {
		return "", err
	}
	if c.Profiles == nil {
		c.Profiles = map[string]Profile{}
	}
	c.Profiles[name] = Profile{Off: slices.Clone(c.OffList(from))}
	c.Profile = name
	return name, nil
}

// RenameProfile renames one of yours.
func (c *Config) RenameProfile(old, name string) (string, error) {
	if BuiltinProfile(old) {
		return "", fmt.Errorf("%q is built in", old)
	}
	p, ok := c.Profiles[old]
	if !ok {
		return "", fmt.Errorf("no profile %q", old)
	}
	name, err := c.cleanName(name)
	if err != nil {
		return "", err
	}
	delete(c.Profiles, old)
	c.Profiles[name] = p
	if c.Profile == old {
		c.Profile = name
	}
	return name, nil
}

// DeleteProfile removes one of yours; in use, nanny goes back to full.
func (c *Config) DeleteProfile(name string) error {
	if BuiltinProfile(name) {
		return fmt.Errorf("%q is built in", name)
	}
	delete(c.Profiles, name)
	if c.Profile == name {
		c.Profile = ProfileFull
	}
	return nil
}

// CycleProfile moves to the next (d=1) or previous (d=-1) profile.
func (c *Config) CycleProfile(d int) string {
	names := c.ProfileNames()
	i := slices.Index(names, c.ActiveProfile())
	c.Profile = names[((i+d)%len(names)+len(names))%len(names)]
	return c.Profile
}

// foldProfile turns the old density and off list into a profile: the
// built-in one when they match it, else one of yours called "mine".
func (c *Config) foldProfile() {
	if c.Profile != "" || c.Density == "" && !c.Basic && len(c.Screen.Off) == 0 {
		return
	}
	off := slices.Clone(c.Screen.Off)
	level := c.Density
	if level == "" && c.Basic {
		level = "minimal"
	}
	switch level {
	case "full":
		if len(off) == 0 {
			c.Profile = ProfileFull
			return
		}
	case "minimal":
		off = withOff(withOff(off, LayoutDetail, false), LayoutExtras, false)
	default: // normal: no repeats, short footers
		off = withOff(off, LayoutExtras, false)
	}
	if c.Profiles == nil {
		c.Profiles = map[string]Profile{}
	}
	name := c.freeName("mine")
	c.Profiles[name] = Profile{Off: off}
	c.Profile = name
}

func withOff(off []string, id string, on bool) []string {
	var out []string
	for _, o := range off {
		if o != id {
			out = append(out, o)
		}
	}
	if !on {
		out = append(out, id)
	}
	return out
}
