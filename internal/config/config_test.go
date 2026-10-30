package config

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// The old switches are read once and land in a profile of yours; saving
// writes only the profile, and it reads back the same.
func TestOffMigratesAndPersists(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	_ = os.MkdirAll(Dir(), 0o755)
	old := "density = \"full\"\n[sources]\nhermes = false\nmachine = true\nsystem = true\n[screen]\nhide_clock = true\nhide_views = [\"Skills\"]\n"
	if err := os.WriteFile(Path(), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	c := Load()
	want := []string{"view.skills", "header.clock", "monitor.hermes"}
	if c.Profile != "mine" || !reflect.DeepEqual(c.OffList("mine"), want) {
		t.Fatalf("profile %q off = %v, want mine %v", c.Profile, c.OffList("mine"), want)
	}
	if c.On("monitor.hermes") || !c.On("monitor.tilt") {
		t.Error("On should read the profile in use")
	}
	c.SetOn("view.live", false)
	c.SetOn("view.skills", true)
	if err := Save(c); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(Path())
	for _, k := range []string{"hide_views", "[sources]", "density", "basic", "\noff"} {
		if strings.Contains(string(b), k) {
			t.Errorf("legacy key %q written back:\n%s", k, b)
		}
	}
	if got := Load(); got.Profile != "mine" || !reflect.DeepEqual(got.OffList("mine"), []string{"header.clock", "monitor.hermes", "view.live"}) {
		t.Errorf("reloaded %q off = %v", got.Profile, got.OffList("mine"))
	}
}

// Old densities: full with nothing off is the built-in full, minimal and
// normal become a profile of yours with the layout to match.
func TestDensityMigrates(t *testing.T) {
	for in, want := range map[string]struct {
		profile string
		off     []string
	}{
		"density = \"full\"":   {"full", nil},
		"basic = true":         {"mine", []string{LayoutDetail, LayoutExtras}},
		"density = \"normal\"": {"mine", []string{LayoutExtras}},
		"density = \"minimal\"\n[screen]\noff = [\"monitor.cron\"]": {"mine", []string{"monitor.cron", LayoutDetail, LayoutExtras}},
		"theme = \"nord\"": {"full", nil},
	} {
		c := Default()
		if _, err := toml.Decode(in, &c); err != nil {
			t.Fatal(err)
		}
		c.migrate()
		if c.ActiveProfile() != want.profile || !reflect.DeepEqual(c.OffList(c.ActiveProfile()), want.off) {
			t.Errorf("%s: %q %v, want %q %v", in, c.ActiveProfile(), c.OffList(c.ActiveProfile()), want.profile, want.off)
		}
	}
}

// The built-in profiles never change: a change makes your own copy. Yours
// are made, renamed, cycled and deleted.
func TestProfiles(t *testing.T) {
	c := Default()
	if c.ActiveProfile() != ProfileFull || !c.On("view.monitor") {
		t.Fatal("a new config shows everything")
	}
	c.Profile = ProfileBasic
	if c.On(LayoutDetail) || c.On("view.monitor") || !c.On("view.live") || !c.On("header.cost") {
		t.Error("basic: one line per agent, no Monitor, Live and the spend on")
	}
	c.SetOn("view.monitor", true)
	if c.Profile != "mine" || !c.On("view.monitor") || c.On(LayoutDetail) || len(c.OffList(ProfileBasic)) != len(basicOff) {
		t.Errorf("a change to basic should land in a copy: %q %v", c.Profile, c.OffList(c.Profile))
	}
	if _, err := c.NewProfile("full", ProfileFull); err == nil {
		t.Error("a built-in name is taken")
	}
	if _, err := c.NewProfile("../x", ProfileFull); err == nil {
		t.Error("a bad name is refused")
	}
	if n, err := c.NewProfile("tilt only", ProfileFull); err != nil || n != "tilt only" || c.Profile != n {
		t.Fatalf("new: %q %v", n, err)
	}
	if _, err := c.RenameProfile(ProfileBasic, "x"); err == nil {
		t.Error("built-in profiles can't be renamed")
	}
	if err := c.DeleteProfile(ProfileFull); err == nil {
		t.Error("built-in profiles can't be deleted")
	}
	if n, err := c.RenameProfile("tilt only", "tilt"); err != nil || c.Profile != n {
		t.Errorf("rename: %v", err)
	}
	if got := c.ProfileNames(); !reflect.DeepEqual(got, []string{"basic", "full", "mine", "tilt"}) {
		t.Errorf("names: %v", got)
	}
	if c.CycleProfile(1) != ProfileBasic || c.CycleProfile(-1) != "tilt" {
		t.Error("cycle wraps around")
	}
	if c.DeleteProfile("tilt"); c.ActiveProfile() != ProfileFull {
		t.Error("deleting the one in use goes back to full")
	}
}

// Worktrees became Git: the old id is renamed in your profiles and in the legacy switches.
func TestWorktreesBecameGit(t *testing.T) {
	for in, want := range map[string][]string{
		"profile = \"mine\"\n[profiles.mine]\noff = [\"view.worktrees\", \"header.clock\"]": {"view.git", "header.clock"},
		"profile = \"mine\"\n[profiles.mine]\noff = [\"view.worktrees\", \"view.git\"]":     {"view.git"},
		"density = \"full\"\n[screen]\nhide_views = [\"Worktrees\"]":                        {"view.git"},
	} {
		c := Default()
		if _, err := toml.Decode(in, &c); err != nil {
			t.Fatal(err)
		}
		c.migrate()
		if got := c.OffList(c.ActiveProfile()); !reflect.DeepEqual(got, want) {
			t.Errorf("%q: off = %v, want %v", in, got, want)
		}
	}
	c := Default()
	c.Profile = ProfileBasic
	for _, id := range []string{"view.git", "view.jira", "view.insights", "sidebar.jira", "header.reviews"} {
		if c.On(id) {
			t.Errorf("%s should be off in basic", id)
		}
	}
}
