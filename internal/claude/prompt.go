package claude

import (
	"regexp"
	"strings"
)

// Prompt is a choice dialog Claude Code shows on screen, usually a permission
// request ("Bash command … Do you want to proceed? ❯ 1. Yes").
type Prompt struct {
	Title    string   // "Bash command", "Edit file", "Fetch"…
	Detail   []string // what it asks about: the command, file, URL, description
	Question string   // "Do you want to proceed?"
	Options  []Option
	Selected int  // index of the option marked ❯
	arrows   bool // unnumbered menu: picked with arrows + enter
}

// Option is one choice; Key is its number (shown, and typed when numbered).
type Option struct {
	Key   string
	Label string
}

// Press is what to type to pick option i: its digit, or for unnumbered menus
// arrow keys from the cursor and enter.
func (p *Prompt) Press(i int) string {
	if !p.arrows {
		return p.Options[i].Key
	}
	mv, n := "\x1b[B", i-p.Selected
	if n < 0 {
		mv, n = "\x1b[A", -n
	}
	return strings.Repeat(mv, n) + "\r"
}

// Index is the position of o in Options, -1 if it isn't one of them.
func (p *Prompt) Index(o *Option) int {
	for i := range p.Options {
		if o != nil && p.Options[i].Key == o.Key {
			return i
		}
	}
	return -1
}

var (
	reOption = regexp.MustCompile(`^([❯›>]\s*)?(\d{1,2})[.)]\s+(\S.*)$`)
	reRule   = regexp.MustCompile(`^[╭╰─━┌└]`)
	reBorder = regexp.MustCompile(`^[\s╭╮╰╯─━╌┌┐└┘│┃|]*$`)
	reHint   = regexp.MustCompile(`(?i)enter to (confirm|select|continue)`)
)

// clean drops the box drawing around a dialog line.
func clean(l string) string {
	l = strings.TrimSpace(l)
	l = strings.TrimLeft(l, "│┃|")
	l = strings.TrimRight(l, "│┃|")
	return strings.TrimSpace(l)
}

// ParsePrompt finds the numbered dialog at the bottom of a pane's plain-text
// screen. It needs consecutive options starting at 1 and the ❯ cursor on one
// of them, so numbered lists in Claude's answers are never taken for a prompt.
func ParsePrompt(screen string) *Prompt {
	raw := strings.Split(strings.TrimRight(screen, "\n "), "\n")
	lines := make([]string, len(raw))
	for i, l := range raw {
		lines[i] = clean(l)
	}
	// the last option line, at most a few lines above the bottom (key hints)
	last := -1
	for i, n := len(lines)-1, 0; i >= 0 && n < 6; i-- {
		if lines[i] == "" || reBorder.MatchString(lines[i]) {
			continue
		}
		if reOption.MatchString(lines[i]) {
			last = i
			break
		}
		n++
	}
	if last < 0 {
		return parseMenu(raw, lines)
	}
	// walk up collecting options n, n-1 … 1; unnumbered lines are wrapped labels
	type opt struct {
		n     int
		label string
		sel   bool
	}
	var opts []opt
	var cont []string
	first := -1
	for i := last; i >= 0 && last-i < 24; i-- {
		l := lines[i]
		mm := reOption.FindStringSubmatch(l)
		if mm == nil {
			if l == "" || len(opts) == 0 {
				break
			}
			cont = append([]string{l}, cont...)
			continue
		}
		n := atoi(mm[2])
		if len(opts) > 0 && n != opts[0].n-1 {
			break
		}
		label := strings.Join(append([]string{mm[3]}, cont...), " ")
		cont = nil
		opts = append([]opt{{n, label, mm[1] != ""}}, opts...)
		first = i
		if n == 1 {
			break
		}
	}
	if len(opts) < 2 || opts[0].n != 1 {
		return nil
	}
	p := &Prompt{Selected: -1}
	for i, o := range opts {
		p.Options = append(p.Options, Option{Key: itoa(o.n), Label: strings.TrimSpace(o.label)})
		if o.sel {
			p.Selected = i
		}
	}
	if p.Selected < 0 {
		return nil
	}
	// the question sits right above the options
	q := first - 1
	for q >= 0 && lines[q] == "" {
		q--
	}
	if q >= 0 && strings.HasSuffix(lines[q], "?") {
		p.Question = lines[q]
		q--
	}
	// the dialog starts after a rule or box top at the left edge
	start := max(0, q-14)
	for i := q; i >= start; i-- {
		if reRule.MatchString(raw[i]) {
			start = i + 1
			break
		}
	}
	var body []string
	for i := start; i <= q; i++ {
		if l := lines[i]; l != "" && !reBorder.MatchString(l) && !strings.HasPrefix(l, "Tip:") {
			body = append(body, l)
		}
	}
	if len(body) > 0 {
		p.Title, body = body[0], body[1:]
	}
	if len(body) > 6 {
		body = body[:6]
	}
	p.Detail = body
	return p
}

// parseMenu reads an unnumbered menu ("❯ No, exit" / "  Yes, I trust this
// folder") sitting right above an "Enter to confirm" hint.
func parseMenu(raw, lines []string) *Prompt {
	hint := -1
	for i, n := len(lines)-1, 0; i >= 0 && n < 4; i-- {
		if lines[i] == "" || reBorder.MatchString(lines[i]) {
			continue
		}
		if reHint.MatchString(lines[i]) {
			hint = i
			break
		}
		n++
	}
	if hint < 0 {
		return nil
	}
	end := hint - 1
	for end >= 0 && lines[end] == "" {
		end--
	}
	// options share the label column of the ❯ line
	col, top := -1, end
	for i := end; i >= 0 && end-i < 10 && lines[i] != ""; i-- {
		if r := []rune(raw[i]); col < 0 {
			for j, c := range r {
				if c == '❯' || c == '›' {
					col = j
					break
				}
			}
		}
		top = i
	}
	if col < 0 {
		return nil
	}
	p := &Prompt{Selected: -1, arrows: true}
	for i := top; i <= end; i++ {
		r := []rune(raw[i])
		if len(r) <= col+2 {
			return nil
		}
		lead, label := string(r[:col+1]), strings.TrimSpace(string(r[col+1:]))
		sel := strings.ContainsAny(lead, "❯›")
		if strings.TrimLeft(lead, " │┃|❯›") != "" || label == "" {
			return nil // not a menu row
		}
		if sel {
			p.Selected = len(p.Options)
		}
		p.Options = append(p.Options, Option{Key: itoa(len(p.Options) + 1), Label: label})
	}
	if len(p.Options) < 2 || p.Selected < 0 {
		return nil
	}
	var body []string
	for i := top - 1; i >= 0 && top-i <= 14; i-- {
		if reRule.MatchString(raw[i]) {
			break
		}
		if l := lines[i]; l != "" && !reBorder.MatchString(l) {
			body = append([]string{l}, body...)
		}
	}
	if len(body) > 0 {
		p.Title, body = body[0], body[1:]
	}
	if len(body) > 6 {
		body = body[:6]
	}
	p.Detail = body
	return p
}

// Yes is the plain approve option.
func (p *Prompt) Yes() *Option {
	for i, o := range p.Options {
		if l := strings.ToLower(o.Label); strings.HasPrefix(l, "yes") && !always(l) {
			return &p.Options[i]
		}
	}
	return nil
}

// Always is the approve-and-don't-ask-again option, when offered.
func (p *Prompt) Always() *Option {
	for i, o := range p.Options {
		if l := strings.ToLower(o.Label); strings.HasPrefix(l, "yes") && always(l) {
			return &p.Options[i]
		}
	}
	return nil
}

// No is the deny option, when offered.
func (p *Prompt) No() *Option {
	for i, o := range p.Options {
		if strings.HasPrefix(strings.ToLower(o.Label), "no") {
			return &p.Options[i]
		}
	}
	return nil
}

// Tell is the "no, and tell Claude what to do instead" option, else plain no.
func (p *Prompt) Tell() *Option {
	for i, o := range p.Options {
		if l := strings.ToLower(o.Label); strings.Contains(l, "tell") || strings.Contains(l, "differently") {
			return &p.Options[i]
		}
	}
	return p.No()
}

// Subject is a one-line summary: the command or file it asks about.
func (p *Prompt) Subject() string {
	if len(p.Detail) > 0 {
		return p.Detail[0]
	}
	if p.Question != "" {
		return p.Question
	}
	return p.Title
}

func always(l string) bool {
	for _, w := range []string{"don't ask again", "don’t ask again", "always", "allow all", "during this session", "for this session"} {
		if strings.Contains(l, w) {
			return true
		}
	}
	return false
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}
