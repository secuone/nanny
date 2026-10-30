package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// userThemes points the config at an empty folder and forgets the user
// themes when the test ends.
func userThemes(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Cleanup(func() { LoadThemes() })
}

// Your themes are files: saved, read back at start, renamed, shared and deleted.
func TestUserThemes(t *testing.T) {
	userThemes(t)
	p := Themes["nord"]
	p.Accent = "#FF00AA"
	if err := SaveTheme("mine", p); err != nil {
		t.Fatal(err)
	}
	if !Custom("mine") || Custom("nord") || Themes["mine"].Accent != "#ff00aa" {
		t.Fatalf("saved: %+v", Themes["mine"])
	}
	delete(Themes, "mine")
	if errs := LoadThemes(); len(errs) > 0 || Themes["mine"].Accent != "#ff00aa" || Themes["mine"].Bg != p.Bg {
		t.Fatalf("read back at start: %v %+v", errs, Themes["mine"])
	}
	c := Default()
	c.Theme = "mine"
	if c.Resolve().Accent != "#ff00aa" {
		t.Error("a user theme is used like any other")
	}

	if err := SaveTheme("nord", p); err != ErrBuilt {
		t.Errorf("a built-in theme can't be overwritten: %v", err)
	}
	for _, bad := range []string{"", "Has Caps", "../x", strings.Repeat("a", 33)} {
		if SaveTheme(bad, p) == nil {
			t.Errorf("%q is not a name", bad)
		}
	}
	bad := p
	bad.Red = "red"
	if SaveTheme("broken", bad) == nil {
		t.Error("colors must be hex")
	}

	if err := RenameTheme("mine", "ours"); err != nil || Custom("mine") || !Custom("ours") {
		t.Fatalf("rename: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ThemesDir(), "mine.toml")); !os.IsNotExist(err) {
		t.Error("rename leaves no old file behind")
	}

	// export to a folder and import it back: a free name is picked
	out, err := ExportTheme("ours", t.TempDir())
	if err != nil || filepath.Base(out) != "ours.toml" {
		t.Fatalf("export: %s %v", out, err)
	}
	got, err := ImportTheme(out)
	if err != nil || got != "ours-2" || Themes[got] != Themes["ours"] {
		t.Fatalf("import: %q %v", got, err)
	}
	// a file that only changes a few colors starts from its base
	short := filepath.Join(t.TempDir(), "My Sunset.toml")
	os.WriteFile(short, []byte("base = \"gruvbox\"\n[colors]\naccent = \"#ff5500\"\n"), 0o644)
	if got, err := ImportTheme(short); err != nil || got != "my-sunset" || Themes[got].Bg != Themes["gruvbox"].Bg || Themes[got].Accent != "#ff5500" {
		t.Fatalf("partial import: %q %v %+v", got, err, Themes[got])
	}
	notTheme := filepath.Join(t.TempDir(), "x.toml")
	os.WriteFile(notTheme, []byte("theme = \"nord\"\n"), 0o644)
	if _, err := ImportTheme(notTheme); err == nil {
		t.Error("a file without colors is not a theme")
	}

	// a broken file is skipped and reported, the rest still load
	os.WriteFile(filepath.Join(ThemesDir(), "junk.toml"), []byte("[colors]\nbg = \"nope\"\n"), 0o644)
	if errs := LoadThemes(); len(errs) != 1 || !Custom("ours") || Custom("junk") {
		t.Errorf("load: %v", errs)
	}

	if err := DeleteTheme("ours"); err != nil || Custom("ours") {
		t.Fatalf("delete: %v", err)
	}
	if DeleteTheme("nord") == nil {
		t.Error("built-in themes can't be deleted")
	}
	if FreeName("ours-2") != "ours-2-copy" || FreeName("nord-copy") != "nord-copy" {
		t.Errorf("free names: %s %s", FreeName("ours-2"), FreeName("nord-copy"))
	}
}

// Low contrast is spotted; the built-in themes have none.
func TestContrasts(t *testing.T) {
	for name, p := range Themes {
		if low := Contrasts(p); len(low) > 0 {
			t.Errorf("%s: %+v", name, low)
		}
	}
	p := Themes["nanny"]
	p.Text = p.Surface
	low := Contrasts(p)
	if len(low) == 0 || low[0].Fg != "text" {
		t.Errorf("text on its own background should be flagged: %+v", low)
	}
}
