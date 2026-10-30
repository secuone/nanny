package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// cursor tracks a selection and scroll offset over n rows.
type cursor struct{ idx, off int }

func (c *cursor) clamp(n, h int) {
	if c.idx >= n {
		c.idx = n - 1
	}
	if c.idx < 0 {
		c.idx = 0
	}
	if h < 1 {
		h = 1
	}
	if c.idx < c.off {
		c.off = c.idx
	}
	if c.idx >= c.off+h {
		c.off = c.idx - h + 1
	}
	if c.off > n-h {
		c.off = max(0, n-h)
	}
}

func (c *cursor) move(d, n, h int) {
	c.idx += d
	c.clamp(n, h)
}

// panel draws a bordered box of exact outer size w×h with a title in the top border.
func (s Styles) panel(title string, body string, w, h int, focus bool) string {
	st := s.Panel
	ts := s.PanelTitle
	if focus {
		st = s.PanelFocus
		ts = s.PanelTitleFocus
	}
	inner := w - 4
	lines := strings.Split(body, "\n")
	if len(lines) > h-2 {
		lines = lines[:max(0, h-2)]
	}
	for i, l := range lines {
		lines[i] = fit(l, inner)
	}
	for len(lines) < h-2 {
		lines = append(lines, strings.Repeat(" ", max(0, inner)))
	}
	box := st.Width(w - 2).Render(strings.Join(lines, "\n"))
	if title == "" {
		return box
	}
	// splice the title into the top border
	rows := strings.SplitN(box, "\n", 2)
	bc := s.P.Overlay
	if focus {
		bc = s.P.Accent
	}
	b := s.Fg(bc)
	t := " " + ts.Render(title) + " "
	tw := lipgloss.Width(t)
	fill := w - 3 - tw
	if fill < 0 {
		return box
	}
	top := b.Render("╭─") + t + b.Render(strings.Repeat("─", fill)+"╮")
	if len(rows) < 2 {
		return top
	}
	return top + "\n" + rows[1]
}

// fit truncates or pads an ANSI string to exactly w cells.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	sw := lipgloss.Width(s)
	if sw > w {
		return ansi.Truncate(s, w, "…")
	}
	return s + strings.Repeat(" ", w-sw)
}

func trunc(s string, w int) string {
	s = oneLine(s)
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "…")
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	return strings.Join(strings.Fields(s), " ")
}

// wrap word-wraps plain text to w cells, keeping at most maxLines lines.
func wrap(s string, w, maxLines int) []string {
	if w < 4 {
		return nil
	}
	var out []string
	for _, para := range strings.Split(strings.ReplaceAll(s, "\r", ""), "\n") {
		para = strings.ReplaceAll(para, "\t", "  ")
		if strings.TrimSpace(para) == "" {
			if len(out) > 0 && out[len(out)-1] != "" {
				out = append(out, "")
			}
			continue
		}
		for _, l := range strings.Split(ansi.Wordwrap(para, w, ""), "\n") {
			out = append(out, ansi.Truncate(l, w, "…"))
		}
		if maxLines > 0 && len(out) >= maxLines {
			return append(out[:maxLines-1], "…")
		}
	}
	return out
}

func ago(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	default:
		return t.Format("Jan 06")
	}
}

func humanBytes(n int64) string {
	switch {
	case n > 1<<20:
		return fmt.Sprintf("%.1fM", float64(n)/(1<<20))
	case n > 1<<10:
		return fmt.Sprintf("%.0fK", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%dB", n)
}

func humanTokens(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	}
	return fmt.Sprintf("%d", n)
}

// kv renders "label  value" with a fixed label column.
func (s Styles) kv(label, value string, w int) string {
	return s.Label.Render(fit(label, 10)) + trunc(value, w-10)
}

var spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Frames are drawn with placeholders where things move: spinGlyph for the
// spinner, blinkGlyph for a ● that blinks. A frame that only animates swaps
// them in the last drawn frame instead of drawing it all again.
const (
	spinGlyph  = "\U00100000"
	blinkGlyph = "\U00100001"
)

// animate puts the current spinner and blink in a frame drawn with placeholders.
func (m Model) animate(frame string) string {
	if !strings.ContainsAny(frame, spinGlyph+blinkGlyph) {
		return frame
	}
	spin, blink := spinner, [2]string{"●", "○"}
	if m.look != nil {
		spin, blink = m.look.spin, m.look.blink
	}
	return strings.NewReplacer(spinGlyph, spin[m.frame%len(spin)], blinkGlyph, blink[m.frame/4%2]).Replace(frame)
}

// renderBlocks stacks variable-height row blocks, scrolling (by line) so the
// selected block stays visible. off is the persisted line offset.
func renderBlocks(blocks [][]string, sel int, off *int, h int) string {
	start := make([]int, len(blocks)+1)
	for i, b := range blocks {
		start[i+1] = start[i] + len(b)
	}
	total := start[len(blocks)]
	if sel >= 0 && sel < len(blocks) {
		if start[sel] < *off {
			*off = start[sel]
		}
		if start[sel+1] > *off+h {
			*off = start[sel+1] - h
		}
	}
	if *off > total-h {
		*off = max(0, total-h)
	}
	var out []string
	for _, b := range blocks {
		out = append(out, b...)
	}
	end := min(total, *off+h)
	if *off >= end {
		return ""
	}
	return strings.Join(out[*off:end], "\n")
}

func truncANSI(s string, w int) string        { return ansi.Truncate(s, w, "") }
func truncLeftANSI(s string, from int) string { return ansi.TruncateLeft(s, from, "") }

// hints renders key/description pairs in the one format every footer and hint line uses.
func (s Styles) hints(keys [][2]string) string {
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = clickable(k[0], s.Key.Render(k[0])+" "+s.KeyDesc.Render(k[1]))
	}
	return strings.Join(parts, s.Dim.Render(" · "))
}

// hintsFit drops pairs before the last one until the line fits in w cells.
func (s Styles) hintsFit(keys [][2]string, w int) string {
	for len(keys) > 1 && lipgloss.Width(s.hints(keys)) > w {
		keys = append(keys[:len(keys)-2:len(keys)-2], keys[len(keys)-1])
	}
	return fit(s.hints(keys), w)
}

// keyList renders "key  description" rows for empty states and getting-started cards.
func (s Styles) keyList(rows [][2]string, w int) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = "  " + clickable(r[0], s.Key.Render(fit(r[0], 5))+s.Text.Render(trunc(r[1], w-7)))
	}
	return out
}

// emptyState is a short muted message followed by what to do next.
func (s Styles) emptyState(msg string, rows [][2]string, w int) string {
	ls := []string{""}
	for _, l := range wrap(msg, w-2, 4) {
		ls = append(ls, " "+s.Muted.Render(l))
	}
	if len(rows) > 0 {
		ls = append(ls, "")
		ls = append(ls, s.keyList(rows, w)...)
	}
	return strings.Join(ls, "\n")
}

// listBlocks renders a block list and remembers which row each screen line
// shows, so a click can select it. y0 is the screen row of the first line.
func (m Model) listBlocks(blocks [][]string, sel int, key string, h, y0 int) string {
	out := renderBlocks(blocks, sel, m.off(key), h)
	var rowOf []int
	for i, b := range blocks {
		for range b {
			rowOf = append(rowOf, i)
		}
	}
	off := min(*m.off(key), len(rowOf))
	m.vs.listY0 = y0
	m.vs.listRows = rowOf[off:min(len(rowOf), off+h)]
	return out
}
