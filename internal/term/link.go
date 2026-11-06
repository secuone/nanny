package term

import (
	"regexp"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
)

var reURL = regexp.MustCompile(`(?i)\b(?:https?|file)://[^\s<>"'` + "`" + `]+`)

// LinkAt is the link under x, y of the pane's view (0-based, scroll
// included): an OSC 8 hyperlink, or a URL in the text, which may wrap onto the
// next rows. Empty when there is none.
func (p *Pane) LinkAt(x, y int) string {
	p.mu.Lock()
	w, h, scroll := p.w, p.h, p.scroll
	p.mu.Unlock()
	if x < 0 || y < 0 || x >= w || y >= h {
		return ""
	}
	cell := func(cx, cy int) *uv.Cell {
		if cy < scroll {
			return p.emu.ScrollbackCellAt(cx, p.emu.ScrollbackLen()-scroll+cy)
		}
		return p.emu.CellAt(cx, cy-scroll)
	}
	if c := cell(x, y); c != nil && c.Link.URL != "" {
		return c.Link.URL
	}
	full := func(cy int) bool { // the row runs to the edge: it may go on below
		c := cell(w-1, cy)
		return c != nil && strings.TrimSpace(c.Content) != ""
	}
	top, bot := y, y
	for top > 0 && full(top-1) {
		top--
	}
	for bot < h-1 && full(bot) {
		bot++
	}
	var b strings.Builder
	at := -1 // byte offset of the clicked cell
	for cy := top; cy <= bot; cy++ {
		for cx := 0; cx < w; cx++ {
			if cx == x && cy == y {
				at = b.Len()
			}
			c := cell(cx, cy)
			switch {
			case c == nil:
				b.WriteByte(' ')
			case c.Width == 0: // the right half of a wide character
			default:
				b.WriteString(c.Content)
			}
		}
	}
	return urlAt(b.String(), at)
}

// urlAt is the URL in s that covers byte offset at.
func urlAt(s string, at int) string {
	for _, loc := range reURL.FindAllStringIndex(s, -1) {
		u := trimURL(s[loc[0]:loc[1]])
		if at >= loc[0] && at < loc[0]+len(u) {
			return u
		}
	}
	return ""
}

// trimURL drops the punctuation that ends the sentence around a URL, and a
// closing bracket it didn't open.
func trimURL(u string) string {
	for u != "" {
		last := u[len(u)-1]
		switch {
		case strings.IndexByte(".,;:!?*", last) >= 0:
		case last == ')' && strings.Count(u, "(") < strings.Count(u, ")"):
		case last == ']' && strings.Count(u, "[") < strings.Count(u, "]"):
		default:
			return u
		}
		u = u[:len(u)-1]
	}
	return u
}
