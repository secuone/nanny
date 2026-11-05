package vt

import "github.com/charmbracelet/x/ansi/parser"

// nanny: in UTF-8 a 0x9C byte inside a string sequence is part of a
// character (✳ is E2 9C B3, Ü is C3 9C), not the 8-bit String Terminator.
// Upstream ends the string there: Claude Code's title "✳ Claude Code" became
// "\xe2" and the rest leaked onto the screen. Strings end on BEL or ESC \,
// as in xterm, kitty or ghostty. The table is shared, so ansi.StringWidth and
// ansi.Truncate read such strings the same way.
func init() {
	for _, st := range []parser.State{parser.OscStringState, parser.DcsStringState} {
		parser.Table.AddOne(0x9C, st, parser.PutAction, st)
	}
	// SOS, PM and APC took no UTF-8 at all: its bytes read as C1 controls,
	// and a 0x9D in an APC started an OSC
	for _, st := range []parser.State{parser.SosStringState, parser.PmStringState, parser.ApcStringState} {
		parser.Table.AddRange(0x80, 0xFF, st, parser.PutAction, st)
	}
}
