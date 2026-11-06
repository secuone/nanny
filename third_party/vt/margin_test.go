package vt

import "testing"

// A program sets a scroll region for a taller screen than this one (a holder's
// ring replayed after a resize), then scrolls: it must not index past the buffer.
func TestMarginsPastTheScreenAreClamped(t *testing.T) {
	e := NewEmulator(80, 41)
	go func() {
		buf := make([]byte, 256)
		for {
			if _, err := e.Read(buf); err != nil {
				return
			}
		}
	}()
	defer e.Close()
	_, _ = e.Write([]byte("\x1b[1;42r\x1b[2S\x1b[?69h\x1b[1;120s\x1b[2S\x1b[5;42r\x1b[3T\x1b[42;1H\x1b[2M\x1b[2L"))
	if r := e.scr.scroll; r.Max.Y > 41 || r.Max.X > 80 {
		t.Errorf("scroll region %v past an 80x41 screen", r)
	}
}
