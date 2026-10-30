package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/secuone/nanny/internal/config"
)

// themeEditor changes the colors of one of your themes, with all of nanny
// showing them as you go. Row 0 is the name, then one row per color.
type themeEditor struct {
	name  textinput.Model
	was   string // the theme being changed; "" for a new one
	p     config.Palette
	cur   cursor
	hex   *textinput.Model // typing a color
	dirty bool
	quit  bool // esc once with changes: esc again drops them
}

// tokenInfo says what each color paints.
var tokenInfo = map[string]string{
	"bg":      M("background"),
	"surface": M("selected row, tabs"),
	"overlay": M("borders, hover"),
	"text":    M("text"),
	"subtle":  M("secondary text"),
	"muted":   M("dim text, hints"),
	"accent":  M("accent: titles, buttons"),
	"accent2": M("second accent"),
	"green":   M("ok, working"),
	"yellow":  M("waiting, warnings"),
	"red":     M("errors, needs you"),
	"blue":    M("branches, info"),
	"peach":   M("lit tabs, highlights"),
	"teal":    M("worktrees, links"),
}

// editTheme opens the editor on a copy of from; was is the user theme being
// changed (it keeps its name unless you give it another), or "" for a new one.
func (m *Model) editTheme(from, was string) {
	in := newInput()
	in.Prompt = ""
	in.CharLimit = 32
	name := was
	if name == "" {
		name = config.FreeName(from)
	}
	in.SetValue(name)
	e := &themeEditor{name: in, was: was, p: config.Themes[from]}
	e.cur.idx = 1
	m.themes.ed = e
	m.themes.msg = ""
	m.paint(e.p)
}

// paint shows palette p all over nanny while it is being edited.
func (m *Model) paint(p config.Palette) {
	m.st = NewStyles(p)
}

func (e *themeEditor) token() string { return config.Tokens[e.cur.idx-1] }

func (e *themeEditor) set(hex string) {
	e.p.Set(e.token(), strings.ToLower(hex))
	e.dirty, e.quit = true, false
}

func (m Model) themeEditKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	t := m.themes
	e := t.ed
	n := len(config.Tokens) + 1
	if e.hex != nil {
		switch k.String() {
		case "esc":
			e.hex = nil
		case "enter":
			v := strings.TrimSpace(e.hex.Value())
			if !strings.HasPrefix(v, "#") {
				v = "#" + v
			}
			if len(v) == 4 { // #abc
				v = "#" + string([]byte{v[1], v[1], v[2], v[2], v[3], v[3]})
			}
			if config.Validate(withColor(e.p, e.token(), v)) != nil {
				t.say(T("colors are #rrggbb"), true)
				return m, nil
			}
			e.set(v)
			e.hex = nil
			t.msg = ""
			m.paint(e.p)
		default:
			in, cmd := e.hex.Update(k)
			e.hex = &in
			return m, cmd
		}
		return m, nil
	}
	if e.name.Focused() {
		switch k.String() {
		case "enter", "esc", "down", "tab":
			e.name.Blur()
			if k.String() != "esc" {
				e.cur.idx = 1
			}
			return m, nil
		}
		in, cmd := e.name.Update(k)
		e.name = in
		e.dirty, e.quit = true, false
		return m, cmd
	}
	if k.String() != "esc" {
		e.quit = false
	}
	switch k.String() {
	case "esc", "ctrl+c":
		if e.dirty && !e.quit {
			e.quit = true
			t.say(T("esc again drops the changes"), true)
			return m, nil
		}
		t.ed = nil
		t.msg = ""
		m.preview(t.names[t.cur.idx])
		return m, nil
	case "ctrl+s", "s":
		return m.saveEditedTheme()
	case "j", "down", "tab":
		e.cur.move(1, n, n)
	case "k", "up", "shift+tab":
		e.cur.move(-1, n, n)
	case "g", "home":
		e.cur.idx = 0
	case "G", "end":
		e.cur.idx = n - 1
	case "enter":
		if e.cur.idx == 0 {
			e.name.CursorEnd()
			return m, e.name.Focus()
		}
		in := newInput()
		in.Prompt = "#"
		in.CharLimit = 7
		in.SetValue(strings.TrimPrefix(e.p.Get(e.token()), "#"))
		in.CursorEnd()
		in.Focus()
		e.hex = &in
		return m, textinput.Blink
	case "left", "h", "right", "l":
		if e.cur.idx > 0 {
			step := 1
			if k.String() == "left" || k.String() == "h" {
				step = -1
			}
			e.set(nextPick(e.token(), e.p.Get(e.token()), step))
		}
	case "+", "=", "-", "_":
		if e.cur.idx > 0 {
			by := 0.08
			if k.String() == "-" || k.String() == "_" {
				by = -0.08
			}
			e.set(shade(e.p.Get(e.token()), by))
		}
	default:
		return m, nil
	}
	m.paint(e.p)
	return m, nil
}

func withColor(p config.Palette, token, hex string) config.Palette {
	p.Set(token, hex)
	return p
}

// saveEditedTheme writes the theme, renamed if its name changed, and puts it on.
func (m Model) saveEditedTheme() (tea.Model, tea.Cmd) {
	t := m.themes
	e := t.ed
	name := strings.TrimSpace(e.name.Value())
	if name != e.was {
		if err := config.ValidName(name); err != nil {
			t.say(themeErr(err), true)
			return m, nil
		}
		if _, taken := config.Themes[name]; taken {
			t.say(themeErr(config.ErrTaken), true)
			return m, nil
		}
	}
	if err := config.SaveTheme(name, e.p); err != nil {
		t.say(themeErr(err), true)
		return m, nil
	}
	if e.was != "" && name != e.was {
		_ = config.DeleteTheme(e.was)
		if t.orig == e.was {
			t.orig = name
		}
	}
	t.ed = nil
	t.reorder(name)
	m.preview(name)
	t.say(Tf("saved %s · ⏎ to use it", name), false)
	return m, nil
}

// nextPick is the next color for token among every theme's, by brightness.
func nextPick(token, cur string, step int) string {
	seen := map[string]bool{}
	var all []string
	for _, p := range config.Themes {
		if c := p.Get(token); !seen[c] {
			seen[c] = true
			all = append(all, c)
		}
	}
	if !seen[cur] {
		all = append(all, cur)
	}
	sort.Slice(all, func(i, j int) bool {
		li, lj := config.Contrast(all[i], "#000000"), config.Contrast(all[j], "#000000")
		if li != lj {
			return li < lj
		}
		return all[i] < all[j]
	})
	for i, c := range all {
		if c == cur {
			return all[(i+step+len(all))%len(all)]
		}
	}
	return cur
}

// shade mixes hex toward white (by > 0) or black (by < 0).
func shade(hex string, by float64) string {
	var r, g, b int
	if _, err := fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &b); err != nil {
		return hex
	}
	mix := func(c int) int {
		if by > 0 {
			return c + int(float64(255-c)*by+0.5)
		}
		return c - int(float64(c)*-by+0.5)
	}
	return fmt.Sprintf("#%02x%02x%02x", mix(r), mix(g), mix(b))
}

func (m Model) viewThemeEditor() string {
	t := m.themes
	e := t.ed
	s := m.st
	p := e.p
	w := min(66, m.w-4)
	inner := w - 6
	var b strings.Builder
	title := T("New theme")
	if e.was != "" {
		title = T("Edit theme")
	}
	b.WriteString(s.Accent.Render(title) + "\n\n")

	// the rows scroll when the terminal is short; the sample goes first
	sample := m.h >= 30
	hh := max(3, min(len(config.Tokens)+1, m.h-12))
	if sample {
		hh = max(3, min(len(config.Tokens)+1, m.h-18))
	}
	e.cur.clamp(len(config.Tokens)+1, hh)
	for i := e.cur.off; i < e.cur.off+hh && i <= len(config.Tokens); i++ {
		var line string
		if i == 0 {
			name := s.Text.Render(e.name.Value())
			if e.name.Focused() {
				name = e.name.View()
			}
			line = " " + s.Muted.Render(fit(T("name"), 14)) + name
		} else {
			tok := config.Tokens[i-1]
			hex := p.Get(tok)
			val := s.Text.Render(hex)
			if e.hex != nil && e.cur.idx == i {
				val = e.hex.View()
			}
			line = " " + lipgloss.NewStyle().Background(lipgloss.Color(hex)).Render("    ") + " " + s.Muted.Render(fit(tok, 8)) +
				fit(val, 9) + " " + s.Muted.Render(trunc(T(tokenInfo[tok]), inner-25))
		}
		if e.cur.idx == i {
			line = s.Sel(s.RowSel, fit(line, inner))
		}
		b.WriteString(line + "\n")
	}
	if sample {
		b.WriteString(themeSample(s, p, inner))
	}
	b.WriteString("\n")
	switch low := config.Contrasts(p); {
	case t.msg != "":
		st := s.Muted
		if t.bad {
			st = s.Red
		}
		b.WriteString(st.Render(trunc(t.msg, inner)) + "\n")
	case len(low) > 0:
		var bits []string
		for _, c := range low {
			bits = append(bits, fmt.Sprintf("%s/%s %.1f", c.Fg, c.Bg, c.Ratio))
		}
		b.WriteString(s.Yellow.Render(trunc("⚠ "+T("hard to read:")+" "+strings.Join(bits, " · "), inner)) + "\n")
	default:
		b.WriteString(s.Green.Render("✓ "+T("good contrast")) + "\n")
	}
	key := func(k, d string) string { return s.Key.Render(k) + s.KeyDesc.Render(" "+d+"  ") }
	b.WriteString(key("⏎", T("type a color")) + key("←→", T("other themes' colors")) + key("+-", T("lighter/darker")) + "\n" +
		key("s", T("save")) + key("esc", T("cancel")))
	return s.Modal.Width(w).Render(b.String())
}

// themeSample is a bit of nanny in palette p, on its own background.
func themeSample(s Styles, p config.Palette, inner int) string {
	var b strings.Builder
	on := func(fg, back, txt string) string {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(fg)).Background(lipgloss.Color(back)).Render(txt)
	}
	pad := func(l string, back string) string {
		l = ansi.Truncate(l, inner, "")
		return l + lipgloss.NewStyle().Background(lipgloss.Color(back)).Render(strings.Repeat(" ", max(0, inner-lipgloss.Width(l))))
	}
	b.WriteString("\n" + s.Muted.Render(T("preview")) + "\n")
	b.WriteString(pad(on(p.Accent, p.Bg, " nanny ")+on(p.Peach, p.Bg, "▎")+on(p.Bg, p.Peach, " Live ")+on(p.Subtle, p.Bg, " Sessions ")+
		on(p.Muted, p.Bg, " 12:04 "), p.Bg) + "\n")
	b.WriteString(pad(on(p.Accent, p.Surface, " ▸ ")+on(p.Text, p.Surface, "refunds  ")+on(p.Green, p.Surface, "● "+T("working")+" ")+
		on(p.Blue, p.Surface, "⎇ main "), p.Surface) + "\n")
	b.WriteString(pad(on(p.Text, p.Bg, "   checkout ")+on(p.Red, p.Bg, "● "+T("needs you")+" ")+on(p.Yellow, p.Bg, "◐ "+T("waiting")+" ")+
		on(p.Teal, p.Bg, "├ feat-x"), p.Bg) + "\n")
	b.WriteString(pad(on(p.Muted, p.Bg, "   "+T("dim text, hints")+" ")+on(p.Overlay, p.Bg, "──── ")+on(p.Accent2, p.Bg, "★"), p.Bg) + "\n")
	return b.String()
}
