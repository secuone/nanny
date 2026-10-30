package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func pasteMsg(s string) tea.Msg { return tea.PasteMsg{Content: s} }

// What a pane gets for each key is what a terminal would send.
func TestKeyBytes(t *testing.T) {
	for in, want := range map[string]string{
		"a": "a", "A": "A", "ñ": "ñ", "enter": "\r", "tab": "\t", "shift+tab": "\x1b[Z",
		"backspace": "\x7f", "esc": "\x1b", "space": " ", "ctrl+b": "\x02", "ctrl+c": "\x03",
		"alt+1": "\x1b1", "alt+R": "\x1bR", "up": "\x1b[A", "shift+up": "\x1b[1;2A",
		"ctrl+left": "\x1b[1;5D", "alt+up": "\x1b[1;3A", "home": "\x1b[H", "pgdown": "\x1b[6~",
		"delete": "\x1b[3~", "f1": "\x1bOP", "f5": "\x1b[15~", "shift+enter": "\r", "alt+enter": "\x1b\r",
		"ctrl+space": "\x00", "ctrl+backspace": "\x08", "alt+shift+R": "\x1bR", "alt+x": "\x1bx",
		"ctrl+i": "\t", "ctrl+alt+b": "\x1b\x02", "ctrl+shift+right": "\x1b[1;6C", "shift+pgup": "\x1b[5;2~",
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

func TestKeyBytesIgnoresLockKeys(t *testing.T) {
	k := keyPress("up")
	k.Mod |= tea.ModCapsLock | tea.ModNumLock
	if got := string(keyBytes(k)); got != "\x1b[A" {
		t.Errorf("got %q", got)
	}
}

func TestKeyPressMatchesTheDecoder(t *testing.T) {
	for in, want := range map[string]string{
		"a": "a", "A": "A", "space": "space", " ": "space", "enter": "enter", "alt+R": "alt+shift+r",
		"shift+tab": "shift+tab", "ctrl+b": "ctrl+b", "ctrl+space": "ctrl+space", "alt+1": "alt+1", "ñ": "ñ",
	} {
		if got := keyPress(in).String(); got != want {
			t.Errorf("%q: String() is %q, want %q", in, got, want)
		}
	}
}

func TestCtrlSpaceIsAPrefix(t *testing.T) {
	m := manyPanes(t, 1)
	m.cfg.Prefix = "ctrl+space"
	mm, _ := m.Update(keyPress("ctrl+space"))
	if !mm.(Model).prefix {
		t.Error("ctrl+space did not start the prefix")
	}
}

func TestPasteIntoATextBox(t *testing.T) {
	m := boot(t, 150, 42)
	mm, _ := m.Update(keyPress("ctrl+k"))
	mm, _ = mm.Update(pasteMsg("jump here"))
	if got := mm.(Model).pal.input.Value(); got != "jump here" {
		t.Errorf("palette input is %q", got)
	}
}

func TestPasteElsewhereIsDropped(t *testing.T) {
	m := boot(t, 150, 42)
	mm, c := m.Update(pasteMsg("2"))
	if c != nil || mm.(Model).tab != m.(Model).tab {
		t.Error("a paste on a list acted as a key")
	}
}
