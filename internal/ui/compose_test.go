package ui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/secuone/nanny/internal/ws"
)

func manyPanesAt(t testing.TB, n, w, h int) Model {
	m := boot(t, w, h).(Model)
	m.spaces = append(m.spaces, ws.NewSpace("proj", t.TempDir()))
	m.cur = 0
	m.openTabIn(m.spaces[0], "shell", []string{"/bin/sh"})
	for i := 1; i < n; i++ {
		m.split(i%2 == 1, []string{"/bin/sh"})
	}
	t.Cleanup(func() {
		for _, p := range m.space().Tab().Panes {
			p.Close()
		}
	})
	return m
}

func TestJoinsMatchLipgloss(t *testing.T) {
	cases := [][]string{
		{},
		{"one"},
		{"a\nbb\nccc", "x"},
		{"tab\there", "next\tline\nmore"},
		{"crlf\r\nline", "plain\nlines"},
		{"日本語\nx", "😀 emoji\n👨‍👩‍👧 family", "ｗｉｄｅ"},
		{"", "a"},
		{"a", ""},
		{"", ""},
		{"\x1b[31mred\x1b[0m\n\x1b[1mbold\x1b[m x", "plain"},
		{"tall\n\n\n\n", "short"},
		{"héllo\ncafé", "é combining", "z"},
	}
	for _, c := range cases {
		var w widths
		for round := 0; round < 2; round++ {
			if got, want := w.joinTop(c...), lipgloss.JoinHorizontal(lipgloss.Top, c...); got != want {
				t.Errorf("joinTop(%q) = %q, want %q", c, got, want)
			}
			if got, want := w.joinLeft(c...), lipgloss.JoinVertical(lipgloss.Left, c...); got != want {
				t.Errorf("joinLeft(%q) = %q, want %q", c, got, want)
			}
			w.roll()
		}
	}
}

func bothFrames(m Model) (plain, fast string) {
	plainCompose = true
	m.vs.gen++
	m.vs.pending = true
	plain = m.render()
	plainCompose = false
	m.vs.gen++
	m.vs.pending = true
	fast = m.render()
	return plain, fast
}

func TestFrameSameAsLipgloss(t *testing.T) {
	defer func() { plainCompose = false }()
	for _, sz := range [][2]int{{150, 42}, {200, 50}, {90, 30}, {60, 20}} {
		m := boot(t, sz[0], sz[1]).(Model)
		for tb := tabLive; tb <= tabInsights; tb++ {
			m.tab = tb
			if a, b := bothFrames(m); a != b {
				t.Errorf("%v tab %d differs", sz, tb)
			}
		}
		for _, k := range []string{"?", "esc", "ctrl+k", "esc", "T", "esc"} {
			mm := key(m, k).(Model)
			if a, b := bothFrames(mm); a != b {
				t.Errorf("%v after %q differs", sz, k)
			}
		}
		pm := manyPanesAt(t, 10, sz[0], sz[1])
		pm.space().Tab().Panes[0].Note("héllo 😀 wide ｗｉｄｅ\ttab\r\n")
		if a, b := bothFrames(pm); a != b {
			t.Errorf("%v panes differ", sz)
		}
	}
}

func TestAppStyleIsPlain(t *testing.T) {
	m := manyPanesAt(t, 4, 100, 30)
	f := m.render()
	if !m.vs.widths.exact(f, m.w, m.h) {
		t.Fatal("the frame is not w×h")
	}
	if got := m.st.App.Width(m.w).Height(m.h).MaxHeight(m.h).Render(f); got != f {
		t.Error("the App style changes an exact frame: finish can no longer skip it")
	}
}

func BenchmarkRenderPaneOutput(b *testing.B) {
	m := manyPanesAt(b, 10, 150, 42)
	p := m.space().Tab().Panes[0]
	_ = m.render()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.Note("Thu Oct 30 13:40:25 CET 2026\r\n")
		m.vs.pending = true
		_ = m.render()
	}
}

// animate gives what the old search and a fresh replacer gave, whichever glyphs are in turn.
func TestAnimateSwapsPlaceholders(t *testing.T) {
	if !strings.HasPrefix(spinGlyph, placeholderPrefix) || !strings.HasPrefix(blinkGlyph, placeholderPrefix) {
		t.Fatal("the placeholders no longer share the prefix animate looks for")
	}
	m := boot(t, 100, 30).(Model)
	frames := []string{"plain ⠋ ● text", spinGlyph + " a " + blinkGlyph + " b " + spinGlyph, "é" + blinkGlyph}
	for n := 0; n < 24; n++ {
		m.frame = n
		for _, f := range frames {
			want := f
			if strings.ContainsAny(f, spinGlyph+blinkGlyph) {
				want = strings.NewReplacer(spinGlyph, spinner[n%len(spinner)], blinkGlyph, [2]string{"●", "○"}[n/4%2]).Replace(f)
			}
			if got := m.animate(f); got != want {
				t.Errorf("frame %d: animate(%q) = %q, want %q", n, f, got, want)
			}
		}
	}
}
