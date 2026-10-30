package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func pasteMsg(s string) tea.Msg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s), Paste: true}
}

// What a pane gets for each key is what a terminal would send.
func TestKeyBytes(t *testing.T) {
	for in, want := range map[string]string{
		"a": "a", "A": "A", "ñ": "ñ", "enter": "\r", "tab": "\t", "shift+tab": "\x1b[Z",
		"backspace": "\x7f", "esc": "\x1b", "space": " ", "ctrl+b": "\x02", "ctrl+c": "\x03",
		"alt+1": "\x1b1", "alt+R": "\x1bR", "up": "\x1b[A", "shift+up": "\x1b[1;2A",
		"ctrl+left": "\x1b[1;5D", "alt+up": "\x1b\x1b[A", "home": "\x1b[H", "pgdown": "\x1b[6~",
		"delete": "\x1b[3~", "f1": "\x1bOP",
	} {
		if got := string(keyBytes(keyPress(in))); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestPasteGoesToThePane(t *testing.T) {
	m := manyPanes(t, 1)
	var tm tea.Model = m
	tm, _ = tm.Update(pasteMsg("paste-me"))
	p := tm.(Model).space().Tab().Pane()
	for i := 0; i < 50; i++ {
		if strings.Contains(p.Text(10), "^[[200~paste-me^[[201~") {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Errorf("the pane never saw the bracketed paste:\n%s", p.Text(10))
}
