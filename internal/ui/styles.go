package ui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/secuone/nanny/internal/config"
)

// Styles is rebuilt whenever the theme changes so the Settings tab can preview live.
type Styles struct {
	P config.Palette

	App, Header, Logo, Tab, TabActive, TabHover, TabSel, TabNum lipgloss.Style
	Btn, BtnHover                                               lipgloss.Style
	Panel, PanelFocus, PanelTitle, PanelTitleFocus              lipgloss.Style
	Row, RowSel, RowSelBlur, Dim, Muted, Text, Bold, Accent     lipgloss.Style
	Green, Yellow, Red, Blue, Peach, Teal, Pink                 lipgloss.Style
	Badge, Key, KeyDesc, Footer, Status, StatusErr, Label       lipgloss.Style
	Modal, Input                                                lipgloss.Style
	BorderOverlay, BorderAccent, BorderRed                      lipgloss.Style // pane borders, built once
}

func NewStyles(p config.Palette) Styles {
	c := func(h string) color.Color { return lipgloss.Color(h) }
	fg := func(h string) lipgloss.Style { return lipgloss.NewStyle().Foreground(c(h)) }
	s := Styles{P: p}
	s.App = lipgloss.NewStyle() // keep the terminal background; themes color the content
	s.Header = lipgloss.NewStyle().Padding(0, 1)
	s.Logo = lipgloss.NewStyle().Bold(true).Foreground(c(onColor(p, p.Accent))).Background(c(p.Accent)).Padding(0, 1)
	s.Tab = fg(p.Muted).Padding(0, 1)
	s.TabActive = lipgloss.NewStyle().Foreground(c(p.Text)).Background(c(p.Surface)).Bold(true).Padding(0, 1)
	s.TabHover = lipgloss.NewStyle().Foreground(c(onColor(p, p.Overlay))).Background(c(p.Overlay)).Padding(0, 1)
	s.TabSel = lipgloss.NewStyle().Foreground(c(onColor(p, p.Peach))).Background(c(p.Peach)).Bold(true).Padding(0, 1)
	s.TabNum = fg(p.Accent2)
	s.Btn = lipgloss.NewStyle().Foreground(c(p.Accent)).Background(c(p.Surface)).Padding(0, 1)
	s.BtnHover = lipgloss.NewStyle().Foreground(c(onColor(p, p.Accent))).Background(c(p.Accent)).Bold(true).Padding(0, 1)
	border := lipgloss.RoundedBorder()
	s.Panel = lipgloss.NewStyle().Border(border).BorderForeground(c(p.Overlay)).Padding(0, 1)
	s.BorderOverlay, s.BorderAccent, s.BorderRed = fg(p.Overlay), fg(p.Accent), fg(p.Red)
	s.PanelFocus = s.Panel.BorderForeground(c(p.Accent))
	s.PanelTitle = fg(p.Subtle).Bold(true)
	s.PanelTitleFocus = fg(p.Accent).Bold(true)
	s.Row = fg(p.Text)
	s.RowSel = lipgloss.NewStyle().Background(c(p.Surface)).Foreground(c(p.Text)).Bold(true)
	s.RowSelBlur = lipgloss.NewStyle().Background(c(p.Overlay)).Foreground(c(p.Text))
	s.Dim = fg(p.Overlay)
	s.Muted = fg(p.Muted)
	s.Text = fg(p.Text)
	s.Bold = fg(p.Text).Bold(true)
	s.Accent = fg(p.Accent).Bold(true)
	s.Green = fg(p.Green)
	s.Yellow = fg(p.Yellow)
	s.Red = fg(p.Red)
	s.Blue = fg(p.Blue)
	s.Peach = fg(p.Peach)
	s.Teal = fg(p.Teal)
	s.Pink = fg(p.Accent2)
	s.Badge = s.BadgeColor(p.Blue)
	s.Key = fg(p.Text)
	s.KeyDesc = fg(p.Muted)
	s.Footer = lipgloss.NewStyle().Padding(0, 1)
	s.Status = fg(p.Teal)
	s.StatusErr = fg(p.Red).Bold(true)
	s.Label = fg(p.Subtle)
	s.Modal = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(c(p.Accent2)).Padding(1, 2)
	s.Input = fg(p.Text)
	return s
}

// BadgeColor is a filled label on bg, with whichever of Bg/Text reads better on it
// (dark themes put Bg on bright fills; light themes need it the other way round).
func (s Styles) BadgeColor(bg string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(onColor(s.P, bg))).Background(lipgloss.Color(bg)).Padding(0, 1)
}

// Sel paints a whole row with st (RowSel/RowSelBlur), re-applying it after the
// resets of inner styled segments so the highlight doesn't stop at the first one.
func (s Styles) Sel(st lipgloss.Style, line string) string {
	if on, _, ok := strings.Cut(st.UnsetPadding().Render("x"), "x"); ok && on != "" {
		line = strings.ReplaceAll(strings.ReplaceAll(line, "\x1b[0m", "\x1b[0m"+on), "\x1b[m", "\x1b[m"+on)
	}
	return st.Render(line)
}

// Fg is a plain foreground style for a palette color.
func (s Styles) Fg(hex string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(hex))
}

func onColor(p config.Palette, bg string) string {
	if config.Contrast(p.Bg, bg) >= config.Contrast(p.Text, bg) {
		return p.Bg
	}
	return p.Text
}

// chip renders a tab label whose parts carry their own colors (a state glyph,
// a muted count): the tab's background and color come back after each of
// them, so the whole tab is lit, not just up to the first glyph.
func chip(st lipgloss.Style, label string) string {
	probe := st.UnsetPadding().Render("X")
	if i := strings.Index(probe, "X"); i > 0 {
		pre := probe[:i]
		label = strings.ReplaceAll(strings.ReplaceAll(label, "\x1b[0m", "\x1b[0m"+pre), "\x1b[m", "\x1b[m"+pre)
	}
	return st.Render(label)
}
