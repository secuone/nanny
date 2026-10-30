package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/secuone/nanny/internal/claude"
)

// The demo's live agents show a context gauge and cost, the detail a Usage block,
// the header today's spend.
func TestUsageMeter(t *testing.T) {
	m := boot(t, 150, 42)
	out := fits(t, m, 150, 42, "live")
	for _, w := range []string{"▰", "62%", "88%", "≈$", "today", "Usage", "context", "124k / 200k", "subagents ≈$"} {
		if !strings.Contains(out, w) {
			t.Errorf("live view lacks %q:\n%s", w, out)
		}
	}
	// at 80 columns the cost goes first, the gauge stays
	m = boot(t, 80, 24)
	out = fits(t, m, 80, 24, "live")
	if !strings.Contains(out, "62%") || !strings.Contains(out, "88%") {
		t.Errorf("80 columns lost the gauge:\n%s", out)
	}
	// basic mode keeps it on the one-line rows
	m = key(m, "M")
	out = fits(t, m, 80, 24, "basic")
	if !strings.Contains(out, "62%") {
		t.Errorf("basic row lacks the gauge:\n%s", out)
	}
	if strings.Contains(out, "today") {
		t.Error("basic header shows no spend")
	}
	// Sessions detail: an old session's usage is read on demand
	m = key(key(key(m, "M"), "2"), "G")
	if out = fits(t, m, 80, 24, "sessions"); !strings.Contains(out, "≈$") {
		t.Errorf("sessions detail lacks the cost:\n%s", out)
	}
}

// Crossing the auto-compact threshold raises one alert per session.
func TestContextAlertOnce(t *testing.T) {
	m := boot(t, 150, 42).(Model)
	var ses *claude.Session
	for _, s := range m.sessions {
		if s.Live != nil && s.Usage != nil && s.Usage.Pct() < ctxWarn {
			ses = s
		}
	}
	if ses == nil {
		t.Fatal("demo has no live session under the threshold")
	}
	n := len(m.alerts.events)
	u := *ses.Usage
	u.Ctx = u.Window() * 9 / 10
	ses.Usage = &u
	m.rebuild()
	m.rebuild()
	if got := len(m.alerts.events) - n; got != 1 || m.alerts.events[0].kind != alertContext {
		t.Fatalf("want one context alert, got %d: %+v", got, m.alerts.events)
	}
	if !strings.Contains(ansi.Strip(m.View()), "auto-compact") {
		t.Error("toast not rendered")
	}
}
