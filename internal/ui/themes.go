package ui

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/secuone/nanny/internal/config"
)

// themePicker previews themes live as the cursor moves; enter keeps, esc
// reverts. Your own themes are made, changed and shared from here too.
type themePicker struct {
	names []string
	cur   cursor
	orig  string
	ask   string           // what the input line is for: rename, import, export
	in    *textinput.Model // the input line
	del   string           // asked to delete this one: x again does it
	msg   string           // the last thing that happened, or went wrong
	bad   bool
	ed    *themeEditor
	lk    *lookEditor
}

// themeOrder lists your own themes first, then dark ones, then light ones,
// each alphabetical.
func themeOrder() []string {
	n := config.ThemeNames()
	rank := func(s string) int {
		switch {
		case config.Custom(s):
			return 0
		case !config.Themes[s].Light():
			return 1
		}
		return 2
	}
	sort.SliceStable(n, func(i, j int) bool { return rank(n[i]) < rank(n[j]) })
	return n
}

func (m *Model) openThemes() {
	t := &themePicker{orig: m.cfg.Theme}
	t.reorder(m.cfg.Theme)
	m.themes = t
}

// reorder lists the themes again with the cursor on name.
func (t *themePicker) reorder(name string) {
	t.names = themeOrder()
	for i, n := range t.names {
		if n == name {
			t.cur.idx = i
		}
	}
}

func (t *themePicker) rows(h int) int { return max(3, min(len(t.names), h-14)) }

func (t *themePicker) say(s string, bad bool) { t.msg, t.bad = s, bad }

// themeErr says what went wrong with a theme in the user's language.
func themeErr(err error) string {
	switch {
	case errors.Is(err, config.ErrName):
		return T("names are a-z, 0-9, - and _ (32 at most)")
	case errors.Is(err, config.ErrTaken):
		return T("that name is taken")
	case errors.Is(err, config.ErrBuilt):
		return T("built-in themes can't be changed: duplicate it")
	case errors.Is(err, config.ErrNoFile), errors.Is(err, os.ErrNotExist):
		return T("not a theme file")
	}
	return err.Error()
}

// preview shows theme name all over nanny.
func (m *Model) preview(name string) {
	m.cfg.Theme = name
	m.st = NewStyles(m.cfg.Resolve())
}

func (m Model) themeKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	t := m.themes
	if t.ed != nil {
		return m.themeEditKey(k)
	}
	if t.lk != nil {
		return m.lookKey(k)
	}
	if t.in != nil {
		return m.themeAskKey(k)
	}
	n, h := len(t.names), t.rows(m.h)
	name := t.names[t.cur.idx]
	if k.String() != "x" {
		t.del = ""
	}
	switch k.String() {
	case "esc", "ctrl+c", "q", "T":
		m.preview(t.orig)
		m.themes = nil
		return m, nil
	case "enter":
		m.themes = nil
		if m.cfg.Theme == t.orig {
			return m, nil
		}
		cmd := m.saveConfig()
		m.setStatus(T("theme")+" · "+m.cfg.Theme, false)
		return m, cmd
	case "j", "down", "tab", "ctrl+n":
		t.cur.move(1, n, h)
	case "k", "up", "shift+tab", "ctrl+p":
		t.cur.move(-1, n, h)
	case "g", "home":
		t.cur.move(-n, n, h)
	case "G", "end":
		t.cur.move(n, n, h)
	case "n", "e":
		// a built-in one is never changed: you edit a copy of it
		if k.String() == "e" && config.Custom(name) {
			m.editTheme(name, name)
		} else {
			m.editTheme(name, "")
		}
		return m, textinput.Blink
	case "d":
		cp := config.FreeName(name)
		if err := config.SaveTheme(cp, config.Themes[name]); err != nil {
			t.say(themeErr(err), true)
			return m, nil
		}
		t.reorder(cp)
		t.say(Tf("copied to %s", cp), false)
	case "r":
		if !config.Custom(name) {
			t.say(themeErr(config.ErrBuilt), true)
			return m, nil
		}
		return m, t.askFor("rename", name)
	case "x", "delete":
		if !config.Custom(name) {
			t.say(themeErr(config.ErrBuilt), true)
			return m, nil
		}
		if t.del != name {
			t.del = name
			t.say(Tf("x again deletes %s", name), true)
			return m, nil
		}
		t.del = ""
		if err := config.DeleteTheme(name); err != nil {
			t.say(themeErr(err), true)
			return m, nil
		}
		if t.orig == name { // it was in use: back to the default
			t.orig = config.DefaultTheme
			m.cfg.Theme = config.DefaultTheme
			_ = config.Save(m.cfg)
		}
		t.names = themeOrder()
		t.cur.clamp(len(t.names), h)
		t.say(Tf("deleted %s", name), false)
	case "i":
		return m, t.askFor("import", "~/")
	case "o":
		return m, t.askFor("export", "~/"+name+".toml")
	case "a":
		m.openLook(false)
		return m, nil
	default:
		return m, nil
	}
	m.preview(t.names[t.cur.idx])
	return m, nil
}

// askFor opens the input line for what (rename, import, export).
func (t *themePicker) askFor(what, value string) tea.Cmd {
	in := textinput.New()
	in.Prompt = "› "
	in.SetValue(value)
	in.CursorEnd()
	in.Focus()
	t.ask, t.in, t.msg = what, &in, ""
	return textinput.Blink
}

func (m Model) themeAskKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	t := m.themes
	switch k.String() {
	case "esc", "ctrl+c":
		t.in = nil
		return m, nil
	case "enter":
	default:
		in, cmd := t.in.Update(k)
		t.in = &in
		return m, cmd
	}
	v := strings.TrimSpace(t.in.Value())
	name := t.names[t.cur.idx]
	switch t.ask {
	case "rename":
		if err := config.RenameTheme(name, v); err != nil {
			t.say(themeErr(err), true)
			return m, nil
		}
		if t.orig == name {
			t.orig = v
			m.cfg.Theme = v
			_ = config.Save(m.cfg)
		}
		t.reorder(v)
		t.say(Tf("renamed to %s", v), false)
	case "import":
		got, err := config.ImportTheme(v)
		if err != nil {
			t.say(themeErr(err), true)
			return m, nil
		}
		t.reorder(got)
		t.say(Tf("imported as %s", got), false)
	case "export":
		path, err := config.ExportTheme(name, v)
		if err != nil {
			t.say(themeErr(err), true)
			return m, nil
		}
		t.say(Tf("saved to %s", tilde(path)), false)
	}
	t.in = nil
	m.preview(t.names[t.cur.idx])
	return m, nil
}

// themeSwatch is a tiny sample of a theme on its own background.
func themeSwatch(p config.Palette) string {
	bg := lipgloss.Color(p.Bg)
	cell := func(fg, s string) string {
		return lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color(fg)).Render(s)
	}
	out := cell(p.Text, " Aa ")
	for _, c := range []string{p.Accent, p.Accent2, p.Green, p.Yellow, p.Red, p.Blue, p.Peach, p.Teal} {
		out += cell(c, "●") + cell(p.Text, " ")
	}
	return out
}

func (m Model) viewThemes() string {
	t := m.themes
	if t.ed != nil {
		return m.viewThemeEditor()
	}
	if t.lk != nil {
		return m.viewLook()
	}
	s := m.st
	w := min(60, m.w-4)
	inner := w - 6
	h := t.rows(m.h)
	t.cur.clamp(len(t.names), h)
	var b strings.Builder
	b.WriteString(s.Accent.Render(T("Theme")) + s.Muted.Render(fmt.Sprintf("  %d/%d", t.cur.idx+1, len(t.names))) + "\n\n")
	for i := t.cur.off; i < min(len(t.names), t.cur.off+h); i++ {
		name := t.names[i]
		p := config.Themes[name]
		tag := T("dark")
		if p.Light() {
			tag = T("light")
		}
		if config.Custom(name) {
			tag = T("yours")
		}
		mark := "  "
		if name == t.orig {
			mark = s.Muted.Render("• ")
		}
		label := fit(mark+s.Text.Render(trunc(name, inner-29)), inner-27) + s.Muted.Render(fit(tag, 6))
		if i == t.cur.idx {
			label = s.Sel(s.RowSel, fit(s.Accent.Render("▸ ")+s.Bold.Render(trunc(name, inner-29)), inner-27)+s.Muted.Render(fit(tag, 6)))
		}
		b.WriteString(label + themeSwatch(p) + "\n")
	}
	b.WriteString("\n")
	switch {
	case t.in != nil:
		q := map[string]string{"rename": T("New name"), "import": T("Theme file to import"), "export": T("Export to (file or folder)")}[t.ask]
		b.WriteString(s.Muted.Render(q) + "\n" + t.in.View() + "\n")
	case t.msg != "":
		st := s.Muted
		if t.bad {
			st = s.Red
		}
		b.WriteString(st.Render(trunc(t.msg, inner)) + "\n")
	default:
		b.WriteString("\n")
	}
	key := func(k, d string) string { return s.Key.Render(k) + s.KeyDesc.Render(" "+d+"  ") }
	b.WriteString(key("↑↓", T("preview")) + key("⏎", T("keep")) + key("esc", T("revert")) + "\n" +
		key("n", T("new")) + key("e", T("edit")) + key("d", T("duplicate")) + key("r", T("rename")) + key("x", T("delete")) + "\n" +
		key("i", T("import")) + key("o", T("export")) + key("a", T("icons, borders, fonts")))
	return s.Modal.Width(w).Render(b.String())
}
