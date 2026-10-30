package ui

import (
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/secuone/nanny/internal/config"
)

// lookEditor picks the icon set, the borders, the text weights and the 3D
// page's font, showing them all over nanny as you go. It lives in the theme
// picker (a); from the ≡ menu it opens alone.
type lookEditor struct {
	cur  cursor
	orig config.Look
	font *textinput.Model
	only bool // opened on its own: esc closes the picker too
}

// webFonts are families most systems have; ⏎ types any other.
var webFonts = []string{"", "system-ui, sans-serif", "Helvetica, Arial, sans-serif", "Verdana, sans-serif",
	"Georgia, serif", "\"Comic Sans MS\", \"Comic Neue\", cursive", "ui-monospace, \"JetBrains Mono\", monospace"}

const lookRows = 6 // icons, borders, bold, italic, underline, 3D font

func (m *Model) openLook(only bool) {
	if m.themes == nil {
		m.openThemes()
	}
	m.themes.lk = &lookEditor{orig: m.cfg.Look, only: only}
	m.themes.msg = ""
}

// setLook shows look l at once.
func (m *Model) setLook(l config.Look) {
	m.cfg.Look = l
	m.look = newLook(l)
}

func cycle(list []string, cur string, step int) string {
	i := slices.Index(list, cur)
	if i < 0 {
		i = 0
		if step < 0 {
			i = 1
		}
	}
	return list[(i+step+len(list))%len(list)]
}

func (m Model) lookKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	t := m.themes
	e := t.lk
	l := m.cfg.Look
	if e.font != nil {
		switch k.String() {
		case "esc":
			e.font = nil
		case "enter":
			l.WebFont = strings.TrimSpace(e.font.Value())
			e.font = nil
			m.setLook(l)
		default:
			in, cmd := e.font.Update(k)
			e.font = &in
			return m, cmd
		}
		return m, nil
	}
	step := 1
	switch k.String() {
	case "esc", "ctrl+c", "q":
		m.setLook(e.orig)
		t.lk = nil
		if e.only {
			m.themes = nil
		}
		return m, nil
	case "j", "down", "tab":
		e.cur.move(1, lookRows, lookRows)
		return m, nil
	case "k", "up", "shift+tab":
		e.cur.move(-1, lookRows, lookRows)
		return m, nil
	case "enter":
		if e.cur.idx == 5 {
			in := newInput()
			in.Prompt = "› "
			in.SetValue(l.WebFont)
			in.CursorEnd()
			in.Focus()
			e.font = &in
			return m, textinput.Blink
		}
		t.lk = nil
		if e.only {
			m.themes = nil
		}
		if l == e.orig {
			return m, nil
		}
		return m, m.saveConfig()
	case "left", "h":
		step = -1
	case "right", "l", " ", "space":
	default:
		return m, nil
	}
	switch e.cur.idx {
	case 0:
		l.Icons = cycle(config.IconSets, l.Icons, step)
	case 1:
		l.Borders = cycle(config.BorderStyles, l.Borders, step)
	case 2:
		l.NoBold = !l.NoBold
	case 3:
		l.NoItalic = !l.NoItalic
	case 4:
		l.NoUnderline = !l.NoUnderline
	case 5:
		l.WebFont = cycle(webFonts, l.WebFont, step)
	}
	m.setLook(l)
	return m, nil
}

func (m Model) viewLook() string {
	s := m.st
	e := m.themes.lk
	l := m.cfg.Look
	w := min(66, m.w-4)
	inner := w - 6
	onOff := func(off bool) string {
		if off {
			return T("off")
		}
		return T("on")
	}
	icons := l.Icons
	if icons == "" || icons == "auto" {
		icons = "auto · " + iconSet(l)
	}
	borders := l.Borders
	if borders == "" {
		borders = "rounded"
	}
	font := l.WebFont
	if font == "" {
		font = T("the page's own")
	}
	// what each row looks like, drawn through the look being picked
	sample := []string{
		"⎇ main ✓ ⚠ ● ○ ⏎ ▸ ⧉ ⌕ ≡ ⚙ " + spinner[0],
		"╭──┬──╮ ├─ │ ╰──┴──╯",
		s.Bold.Render(T("bold")),
		lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color(s.P.Text)).Render(T("italic")),
		lipgloss.NewStyle().Underline(true).Foreground(lipgloss.Color(s.P.Text)).Render(T("underlined")),
		"",
	}
	rows := [][2]string{{T("Icons"), icons}, {T("Borders"), borders}, {T("Bold"), onOff(l.NoBold)},
		{T("Italic"), onOff(l.NoItalic)}, {T("Underline"), onOff(l.NoUnderline)}, {T("3D page font"), font}}
	var b strings.Builder
	b.WriteString(s.Accent.Render(T("Appearance")) + "\n\n")
	for i, r := range rows {
		val := s.Text.Render("‹ " + r[1] + " ›")
		if i == 5 && e.font != nil {
			val = e.font.View()
		}
		line := " " + s.Muted.Render(fit(r[0], 14)) + fit(val, 26) + " " + sample[i]
		line = fit(line, inner)
		if i == e.cur.idx {
			line = s.Sel(s.RowSel, line)
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\n" + s.Muted.Render(wrapTo(T("The font itself is your terminal's: change it in its settings (Ghostty, kitty, WezTerm…). nanny picks the glyphs, so pick Nerd Font icons if your font is one."), inner)) + "\n\n")
	key := func(k, d string) string { return s.Key.Render(k) + s.KeyDesc.Render(" "+d+"  ") }
	b.WriteString(key("←→", T("change")) + key("⏎", T("keep")) + key("esc", T("revert")))
	return s.Modal.Width(w).Render(b.String())
}

// wrapTo breaks text into lines of at most w columns.
func wrapTo(text string, w int) string {
	var out []string
	line := ""
	for _, word := range strings.Fields(text) {
		if line != "" && lipgloss.Width(line+" "+word) > w {
			out = append(out, line)
			line = word
			continue
		}
		if line != "" {
			line += " "
		}
		line += word
	}
	return strings.Join(append(out, line), "\n")
}
