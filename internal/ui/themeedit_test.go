package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/secuone/nanny/internal/config"
)

func typeText(m tea.Model, s string) tea.Model {
	for _, r := range s { // no key(): the cursor's blink would wait
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

func backspaces(m tea.Model, n int) tea.Model {
	for i := 0; i < n; i++ {
		m, _ = m.Update(keyPress("backspace"))
	}
	return m
}

// From the theme picker: a new theme from the one under the cursor, its
// colors changed with all of nanny showing them, saved as a file, put on,
// renamed, exported, imported and deleted.
func TestThemeEditor(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Cleanup(func() { config.LoadThemes() })
	m := key(boot(t, 120, 40), "T")
	base := m.(Model).cfg.Theme
	m = key(m, "n")
	ed := m.(Model).themes.ed
	if ed == nil || !strings.Contains(ansi.Strip(viewOf(m)), "New theme") {
		t.Fatalf("n should open the editor:\n%s", ansi.Strip(viewOf(m)))
	}
	// accent is the 7th color: type one in
	for i := 0; i < 6; i++ {
		m = key(m, "j")
	}
	m = key(m, "enter")
	m = backspaces(m, 6)
	m = typeText(m, "f0a")
	m = key(m, "enter")
	if got := m.(Model).st.P.Accent; got != "#ff00aa" {
		t.Fatalf("a typed color shows at once all over nanny: %q", got)
	}
	m = key(m, "+")
	if got := m.(Model).st.P.Accent; got == "#ff00aa" || config.Contrast(got, "#ffffff") >= config.Contrast("#ff00aa", "#ffffff") {
		t.Errorf("+ makes it lighter: %s", got)
	}
	m = key(m, "right")
	if m.(Model).themes.ed.p.Accent == m.(Model).st.P.Accent && m.(Model).st.P.Accent == "#ff00aa" {
		t.Error("→ picks another theme's accent")
	}
	// a bad color is refused
	m = key(m, "enter")
	m = backspaces(m, 6)
	m = typeText(m, "zz")
	m = key(m, "enter")
	if m.(Model).themes.ed.hex == nil || !m.(Model).themes.bad {
		t.Error("a bad color is refused and stays to be fixed")
	}
	m = key(m, "esc")
	// the name: row 0
	m = key(m, "g")
	m = key(m, "enter")
	m = backspaces(m, 40)
	m = typeText(m, "sunset")
	m = key(m, "enter")
	// low contrast is pointed out
	m = key(m, "j")
	m = key(m, "j")
	m = key(m, "j") // text
	m = key(m, "enter")
	m = backspaces(m, 6)
	m = typeText(m, config.Themes[base].Surface[1:])
	m = key(m, "enter")
	if out := ansi.Strip(viewOf(m)); !strings.Contains(out, "hard to read") || !strings.Contains(out, "text/surface") {
		t.Errorf("low contrast should be pointed out:\n%s", out)
	}
	for i, l := range strings.Split(viewOf(m), "\n") {
		if w := lipgloss.Width(l); w > 120 {
			t.Errorf("editor line %d is %d wide", i, w)
		}
	}
	m = key(m, "s")
	if mm := m.(Model); mm.themes.ed != nil || !config.Custom("sunset") || mm.cfg.Theme != "sunset" {
		t.Fatalf("s saves it and previews it: %+v", mm.themes.msg)
	}
	if _, err := os.Stat(filepath.Join(config.ThemesDir(), "sunset.toml")); err != nil {
		t.Fatal("saved as a file:", err)
	}
	if !strings.Contains(ansi.Strip(viewOf(m)), "yours") {
		t.Error("your themes are marked in the picker")
	}
	m = key(m, "enter")
	if config.Load().Theme != "sunset" || !config.Custom("sunset") {
		t.Fatal("⏎ puts it on and it is there on the next start")
	}

	// rename, export, import, delete
	m = key(m, "T")
	if m.(Model).themes.names[m.(Model).themes.cur.idx] != "sunset" {
		t.Fatal("the picker opens on the theme in use")
	}
	m = key(m, "r")
	m = backspaces(m, 40)
	m = typeText(m, "dusk")
	m = key(m, "enter")
	if !config.Custom("dusk") || config.Custom("sunset") || config.Load().Theme != "dusk" {
		t.Fatal("rename keeps it in use under its new name")
	}
	dir := t.TempDir()
	m = key(m, "o")
	m = backspaces(m, 80)
	m = typeText(m, dir)
	m = key(m, "enter")
	if _, err := os.Stat(filepath.Join(dir, "dusk.toml")); err != nil {
		t.Fatal("o exports it:", err)
	}
	m = key(m, "i")
	m = backspaces(m, 80)
	m = typeText(m, filepath.Join(dir, "dusk.toml"))
	m = key(m, "enter")
	if !config.Custom("dusk-2") {
		t.Fatalf("i imports it under a free name: %s", m.(Model).themes.msg)
	}
	// built-in ones are never changed
	m = key(m, "G")
	m = key(m, "x")
	if !m.(Model).themes.bad {
		t.Error("a built-in theme can't be deleted")
	}
	m = key(m, "g")
	name := m.(Model).themes.names[0]
	m = key(m, "x")
	m = key(m, "x")
	if config.Custom(name) {
		t.Errorf("x twice deletes %s", name)
	}
	m = key(m, "esc")
	fits(t, m, 120, 40, "after themes")
}

// The editor fits short terminals: its rows scroll.
func TestThemeEditorFits(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, size := range [][2]int{{80, 24}, {100, 30}, {150, 42}} {
		m := key(key(boot(t, size[0], size[1]), "T"), "n")
		for i := 0; i < 16; i++ {
			fits(t, m, size[0], size[1], "theme editor")
			m = key(m, "j")
		}
	}
}
