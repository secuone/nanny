package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// widths remembers how wide each drawn line is: most lines of a frame are the
// same as the frame before, and measuring them is what drawing costs most.
type widths struct{ cur, old map[string]int }

func (c *widths) of(s string) int {
	if w, ok := c.cur[s]; ok {
		return w
	}
	w, ok := c.old[s]
	if !ok {
		w = ansi.StringWidth(s)
	}
	if c.cur == nil {
		c.cur = map[string]int{}
	}
	c.cur[s] = w
	return w
}

// roll forgets the lines the last frame did not draw.
func (c *widths) roll() { c.old, c.cur = c.cur, make(map[string]int, len(c.cur)) }

func (c *widths) lines(s string) ([]string, int) {
	if strings.IndexByte(s, '\t') >= 0 {
		s = strings.ReplaceAll(s, "\t", "    ")
	}
	if strings.IndexByte(s, '\r') >= 0 {
		s = strings.ReplaceAll(s, "\r\n", "\n")
	}
	ls := strings.Split(s, "\n")
	widest := 0
	for _, l := range ls {
		widest = max(widest, c.of(l))
	}
	return ls, widest
}

// joinTop is lipgloss.JoinHorizontal(lipgloss.Top, strs...) with remembered widths.
func (c *widths) joinTop(strs ...string) string {
	if len(strs) == 0 {
		return ""
	}
	if len(strs) == 1 {
		return strs[0]
	}
	blocks := make([][]string, len(strs))
	wid := make([]int, len(strs))
	h, size := 0, 0
	for i, s := range strs {
		blocks[i], wid[i] = c.lines(s)
		h = max(h, len(blocks[i]))
		size += len(s)
	}
	var b strings.Builder
	b.Grow(size + h*8)
	for y := 0; y < h; y++ {
		for j, bl := range blocks {
			l := ""
			if y < len(bl) {
				l = bl[y]
			}
			b.WriteString(l)
			pad(&b, wid[j]-c.of(l))
		}
		if y < h-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// joinLeft is lipgloss.JoinVertical(lipgloss.Left, strs...) with remembered widths.
func (c *widths) joinLeft(strs ...string) string {
	if len(strs) == 0 {
		return ""
	}
	if len(strs) == 1 {
		return strs[0]
	}
	blocks := make([][]string, len(strs))
	widest, size := 0, 0
	for i, s := range strs {
		var w int
		blocks[i], w = c.lines(s)
		widest = max(widest, w)
		size += len(s)
	}
	var b strings.Builder
	b.Grow(size + 256)
	for i, bl := range blocks {
		for j, l := range bl {
			b.WriteString(l)
			pad(&b, widest-c.of(l))
			if !(i == len(blocks)-1 && j == len(bl)-1) {
				b.WriteByte('\n')
			}
		}
	}
	return b.String()
}

// exact reports whether s is already h lines of exactly w columns, the frame
// the App style would give back unchanged.
func (c *widths) exact(s string, w, h int) bool {
	if strings.Count(s, "\n") != h-1 || strings.IndexByte(s, '\t') >= 0 || strings.IndexByte(s, '\r') >= 0 {
		return false
	}
	for l := range strings.SplitSeq(s, "\n") {
		if c.of(l) != w {
			return false
		}
	}
	return true
}

func pad(b *strings.Builder, n int) {
	for ; n > 0; n-- {
		b.WriteByte(' ')
	}
}

// plainCompose routes the layout back to lipgloss, so tests can compare both.
var plainCompose bool

func (m Model) joinTop(strs ...string) string {
	if plainCompose {
		return lipgloss.JoinHorizontal(lipgloss.Top, strs...)
	}
	return m.vs.widths.joinTop(strs...)
}

func (m Model) joinLeft(strs ...string) string {
	if plainCompose {
		return lipgloss.JoinVertical(lipgloss.Left, strs...)
	}
	return m.vs.widths.joinLeft(strs...)
}

// finish gives the frame its App style; one already w×h comes back as it is.
func (m Model) finish(out string) string {
	if !plainCompose && m.vs.widths.exact(out, m.w, m.h) {
		return out
	}
	return m.st.App.Width(m.w).Height(m.h).MaxHeight(m.h).Render(out)
}
