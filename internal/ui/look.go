package ui

import (
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/secuone/nanny/internal/config"
)

// The look pass redraws a finished frame with the icon set, border style and
// text weights picked in the config. It only runs when one of them differs
// from the default, and never touches what a program prints in a pane: those
// lines are fenced with rawOn/rawOff, which the pass drops.

const (
	rawOn  = "\x1b_nanny:raw\x1b\\" // zero-width APC fences, never sent to the terminal
	rawOff = "\x1b_nanny:end\x1b\\"
)

// asciiIcons are plain ASCII stand-ins for the glyphs nanny draws. Box
// drawing belongs to the border styles. Wide glyphs are padded with spaces,
// so nothing moves.
var asciiIcons = map[rune]string{
	'·': "-", '⏎': "<", '…': ".", '“': `"`, '”': `"`, '→': ">", '●': "*", '⎇': "@", '✓': "v", '✔': "v",
	'—': "-", '–': "-", '−': "-", '↑': "^", '↓': "v", '←': "<", '◆': "*", '⚠': "!", '○': "o", '◌': "o",
	'×': "x", '✕': "x", '✗': "x", '▸': ">", '▶': ">", '›': ">", '‹': "<", '◂': "<", '❯': ">", '≡': "=",
	'☰': "=", '↳': "L", '✎': "e", '⊞': "#", '▣': "#", '⧉': "#", '⚙': "*", '♪': "~", '▾': "v", '★': "*",
	'⭐': "*", '•': "*", '▍': "|", '▌': "|", '▎': "|", '▏': "|", '≈': "~", '⇄': "=", '◔': "o", '◐': "o",
	'◷': "o", '☐': "o", '☑': "x", '⌖': "+", '⌕': "/", '⚓': "&", '▰': "#", '▱': "-", '⋮': ":", '⠿': ":",
	'☕': "c", '⏸': "=", '¶': "P", '⚡': "!", '🔥': "!", '🚀': "^", '👋': "hi", '🔔': "!", '🎉': "!",
	// the spinner
	'⠋': "|", '⠙': "/", '⠹': "-", '⠸': "\\", '⠼': "|", '⠴': "/", '⠦': "-", '⠧': "\\", '⠇': "|", '⠏': "/",
}

// nerdIcons swap some glyphs for Nerd Font icons (U+E000–U+F8FF and the
// Material Design plane); the rest stay as they are.
var nerdIcons = map[rune]string{
	'⎇': "", '✓': "", '✔': "", '✗': "", '✕': "", '⚠': "", '⚙': "",
	'⌕': "", '≡': "", '☰': "", '✎': "", '⧉': "", '◷': "", '▶': "",
	'⏸': "", '♪': "", '⏎': "\U000f0311", '↳': "", '⊞': "", '★': "", '⌖': "",
	'⚓': "", '☕': "", '🔥': "", '🚀': "", '🔔': "", '⚡': "",
}

// borderSets redraw the (rounded, light) box drawing nanny uses.
var borderSets = map[string]map[rune]string{
	"square": {'╭': "┌", '╮': "┐", '╰': "└", '╯': "┘"},
	"thick": {'╭': "┏", '╮': "┓", '╰': "┗", '╯': "┛", '┌': "┏", '┐': "┓", '└': "┗", '┘': "┛", '─': "━", '│': "┃",
		'├': "┣", '┤': "┫", '┬': "┳", '┴': "┻", '┼': "╋"},
	"double": {'╭': "╔", '╮': "╗", '╰': "╚", '╯': "╝", '┌': "╔", '┐': "╗", '└': "╚", '┘': "╝", '─': "═", '│': "║",
		'├': "╠", '┤': "╣", '┬': "╦", '┴': "╩", '┼': "╬"},
	"ascii": {'╭': "+", '╮': "+", '╰': "+", '╯': "+", '┌': "+", '┐': "+", '└': "`", '┘': "+", '─': "-", '│': "|",
		'├': "|", '┤': "+", '┬': "+", '┴': "+", '┼': "+"},
	"none": {'╭': " ", '╮': " ", '╰': " ", '╯': " ", '┌': " ", '┐': " ", '└': " ", '┘': " ", '─': " ", '│': " ",
		'├': " ", '┤': " ", '┬': " ", '┴': " ", '┼': " "},
}

type lookPass struct {
	on           bool
	ascii        bool
	glyph        map[rune]string
	bold, ital   bool // allowed
	under        bool
	spin         []string
	blink        [2]string
	rawOn, rawOf string
}

// iconSet is the icon set in use: auto picks ASCII on a terminal that can't
// show Unicode (the Linux console, a locale that isn't UTF-8).
func iconSet(l config.Look) string {
	switch l.Icons {
	case "unicode", "nerd", "ascii":
		return l.Icons
	}
	loc := os.Getenv("LC_ALL")
	if loc == "" {
		loc = os.Getenv("LC_CTYPE")
	}
	if loc == "" {
		loc = os.Getenv("LANG")
	}
	loc = strings.ToLower(loc)
	if os.Getenv("TERM") == "linux" || loc != "" && !strings.Contains(loc, "utf-8") && !strings.Contains(loc, "utf8") {
		return "ascii"
	}
	return "unicode"
}

func newLook(l config.Look) *lookPass {
	p := &lookPass{bold: !l.NoBold, ital: !l.NoItalic, under: !l.NoUnderline, spin: spinner, blink: [2]string{"●", "○"}}
	set := iconSet(l)
	g := map[rune]string{}
	add := func(from map[rune]string) {
		for r, s := range from {
			g[r] = s
		}
	}
	switch set {
	case "ascii":
		add(asciiIcons)
		p.ascii = true
		p.spin = []string{"|", "/", "-", "\\"}
		p.blink = [2]string{"*", "o"}
	case "nerd":
		add(nerdIcons)
	}
	add(borderSets[l.Borders])
	// keep every cell where it was: pad what is narrower than the glyph
	for r, s := range g {
		gw, sw := ansi.StringWidth(string(r)), ansi.StringWidth(s)
		if sw < gw {
			g[r] = s + strings.Repeat(" ", gw-sw)
		} else if sw > gw {
			g[r] = ansi.Truncate(s, gw, "")
		}
	}
	p.glyph = g
	p.on = len(g) > 0 || p.ascii || !p.bold || !p.ital || !p.under
	if p.on {
		p.rawOn, p.rawOf = rawOn, rawOff
	}
	return p
}

// apply redraws frame with the look; fences are dropped.
func (p *lookPass) apply(frame string) string {
	if p == nil || !p.on {
		return frame
	}
	var b strings.Builder
	b.Grow(len(frame))
	raw := false
	for i := 0; i < len(frame); {
		c := frame[i]
		if c == 0x1b && i+1 < len(frame) {
			n := seqLen(frame[i:])
			seq := frame[i : i+n]
			i += n
			switch {
			case seq == rawOn:
				raw = true
			case seq == rawOff:
				raw = false
			case !raw && seq[1] == '[' && seq[len(seq)-1] == 'm':
				b.WriteString(p.sgr(seq))
			default:
				b.WriteString(seq)
			}
			continue
		}
		r, n := utf8.DecodeRuneInString(frame[i:])
		i += n
		if raw || r < 0x80 {
			b.WriteString(frame[i-n : i])
			continue
		}
		if s, ok := p.glyph[r]; ok {
			b.WriteString(s)
			continue
		}
		if p.ascii {
			switch {
			case unicode.Is(unicode.Mn, r) || r == 0x200d || unicode.Is(unicode.Cf, r):
				continue // joiners and variation selectors of emoji
			case unicode.IsSymbol(r) || unicode.IsPunct(r) || unicode.Is(unicode.So, r):
				b.WriteString("*" + strings.Repeat(" ", max(0, ansi.StringWidth(string(r))-1)))
				continue
			}
		}
		b.WriteString(frame[i-n : i])
	}
	return b.String()
}

// seqLen is the length of the escape sequence at the start of s.
func seqLen(s string) int {
	switch s[1] {
	case '[': // CSI: parameters, then a final byte @–~
		for j := 2; j < len(s); j++ {
			if s[j] >= 0x40 && s[j] <= 0x7e {
				return j + 1
			}
		}
		return len(s)
	case ']', '_', 'P', '^': // OSC, APC, DCS, PM: up to BEL or ST
		for j := 2; j < len(s); j++ {
			if s[j] == 0x07 {
				return j + 1
			}
			if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
				return j + 2
			}
		}
		return len(s)
	}
	return 2
}

// sgr drops bold, italic and underline from a style sequence when they are off.
func (p *lookPass) sgr(seq string) string {
	if p.bold && p.ital && p.under {
		return seq
	}
	params := strings.Split(seq[2:len(seq)-1], ";")
	out := params[:0:0]
	for j := 0; j < len(params); j++ {
		v := params[j]
		switch {
		case v == "38" || v == "48" || v == "58": // colors: their numbers are not styles
			k := j + 1
			if k < len(params) && params[k] == "5" {
				k += 1
			} else if k < len(params) && params[k] == "2" {
				k += 3
			}
			out = append(out, params[j:min(k+1, len(params))]...)
			j = k
			continue
		case v == "1" && !p.bold, v == "3" && !p.ital, (v == "4" || strings.HasPrefix(v, "4:")) && !p.under:
			continue
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		return ""
	}
	return "\x1b[" + strings.Join(out, ";") + "m"
}

// fenceOn and fenceOff fence a program's own output from the pass (nothing
// when the pass is off).
func (p *lookPass) fenceOn() string {
	if p == nil {
		return ""
	}
	return p.rawOn
}

func (p *lookPass) fenceOff() string {
	if p == nil {
		return ""
	}
	return p.rawOf
}
