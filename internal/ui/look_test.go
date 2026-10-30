package ui

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/secuone/nanny/internal/classroom"
	"github.com/secuone/nanny/internal/config"
)

func withLook(t *testing.T, w, h int, l config.Look) tea.Model {
	t.Helper()
	m := boot(t, w, h).(Model)
	m.cfg.Look = l
	m.applyConfig()
	return m
}

// Pure ASCII: every view draws with plain characters (letters of the
// language aside) and nothing moves.
func TestLookASCII(t *testing.T) {
	for _, size := range [][2]int{{150, 42}, {80, 24}} {
		m := withLook(t, size[0], size[1], config.Look{Icons: "ascii", Borders: "ascii"})
		for _, k := range []string{"1", "2", "3", "4", "5", "6", "7", "8", "?", "T"} {
			m = key(m, k)
			out := fits(t, m, size[0], size[1], "ascii "+k)
			for _, r := range ansi.Strip(out) {
				if r > 0x7e && !unicode.IsLetter(r) && r != '\n' {
					t.Errorf("view %s: %q is not ASCII", k, r)
					break
				}
			}
			if k == "?" || k == "T" {
				m = key(m, "esc")
			}
		}
	}
}

// Each glyph's stand-in takes the cells the glyph took.
func TestLookKeepsWidths(t *testing.T) {
	for _, icons := range config.IconSets {
		for _, b := range config.BorderStyles {
			p := newLook(config.Look{Icons: icons, Borders: b})
			for r, s := range p.glyph {
				if ansi.StringWidth(s) != ansi.StringWidth(string(r)) {
					t.Errorf("%s/%s: %q → %q changes width", icons, b, r, s)
				}
			}
		}
	}
	if newLook(config.Look{Icons: "unicode"}).on {
		t.Error("the default look must not cost a pass")
	}
}

// Nerd Font icons, borders and weights; a program's own output is left alone.
func TestLookPass(t *testing.T) {
	p := newLook(config.Look{Icons: "nerd", Borders: "double", NoBold: true, NoUnderline: true})
	got := p.apply("╭─⎇ ✓╮" + p.fenceOn() + "╭─⎇ ✓╮\x1b[1mbold" + p.fenceOff() + "\x1b[1;38;5;4;4mx\x1b[0m")
	want := "╔═ ╗" + "╭─⎇ ✓╮\x1b[1mbold" + "\x1b[38;5;4mx\x1b[0m"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	if strings.Contains(newLook(config.Look{}).fenceOn(), "\x1b") {
		t.Error("no fences when the pass is off: they'd reach the terminal")
	}
}

// From the ≡ menu: ←→ show a choice at once, ⏎ keeps it, esc goes back;
// the 3D page gets its font.
func TestLookEditor(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var m tea.Model = boot(t, 120, 40)
	mm := m.(Model)
	mm.openLook(true)
	m = mm
	if out := ansi.Strip(viewOf(m)); !strings.Contains(out, "Appearance") || !strings.Contains(out, "terminal's") {
		t.Fatalf("the appearance editor:\n%s", out)
	}
	m = key(m, "j") // borders
	m = key(m, "right")
	if m.(Model).cfg.Look.Borders != "square" || !strings.Contains(viewOf(m), "┌") {
		t.Fatalf("→ shows square borders at once: %q", m.(Model).cfg.Look.Borders)
	}
	m = key(m, "esc")
	if m.(Model).themes != nil || m.(Model).cfg.Look.Borders != "" {
		t.Fatal("esc puts it back and closes")
	}
	mm = m.(Model)
	mm.openLook(true)
	m = mm
	m = key(m, "right") // icons: unicode
	m = key(m, "right") // nerd
	for i := 0; i < 5; i++ {
		m = key(m, "j")
	}
	m = key(m, "right") // a font
	m = key(m, "k")     // ⏎ on the font row types one
	m = key(m, "enter")
	saved := config.Load().Look
	if m.(Model).themes != nil || saved.Icons != "nerd" || saved.WebFont != webFonts[1] {
		t.Fatalf("⏎ keeps it: %+v", saved)
	}
	mm = m.(Model)
	s, err := classroom.Start()
	if err != nil {
		t.Fatal(err)
	}
	mm.class = s
	mm.pushClassroom()
	r, err := http.Get(s.URL() + "state.json")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	var snap classroom.Snapshot
	if json.Unmarshal(b, &snap); snap.Font != webFonts[1] {
		t.Errorf("the 3D page gets the font: %q", snap.Font)
	}
}
