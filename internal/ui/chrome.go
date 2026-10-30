package ui

import (
	"strconv"
	"strings"
	"time"
)

// now is the clock the chrome shows, apart so tests can move it.
var now = time.Now

// chromeCache is the header, footer and sidebar of the last frame, and what
// they were drawn from.
type chromeCache struct{ key, header, footer, side string }

// chromeKey is everything the header, footer and sidebar are drawn from that
// can change without an update: the clock, the status timing out, and what
// the panes show of themselves. Every update but pane output bumps gen.
func (m Model) chromeKey(bh int) string {
	var b strings.Builder
	for _, n := range []int{int(m.vs.gen), m.w, m.h, bh, int(now().Unix() / 60)} {
		b.WriteString(strconv.Itoa(n))
		b.WriteByte('|')
	}
	b.WriteString(strconv.FormatBool(m.status != "" && time.Since(m.statusAt) < 8*time.Second))
	b.WriteString(strconv.FormatBool(m.prefix))
	for _, s := range m.spaces {
		b.WriteByte('|')
		b.WriteString(spaceWhere(s))
		for _, t := range s.Tabs {
			for _, p := range t.Panes {
				b.WriteByte('|')
				b.WriteString(m.paneState(p))
				b.WriteString(p.Title())
			}
		}
	}
	for _, r := range m.live.allRows {
		if r.local != nil {
			b.WriteByte('|')
			b.WriteString(agentState(r.local.pane))
		}
	}
	return b.String()
}
