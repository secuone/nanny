package ui

import (
	"fmt"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/secuone/nanny/internal/claude"
	"github.com/secuone/nanny/internal/term"
)

// answerBox answers a permission prompt of a nanny pane without switching to it.
type answerBox struct {
	ref   paneRef
	title string
	cur   int
}

// paneScreen is how many screen lines prompt parsing looks at.
const paneScreen = 60

// panePrompt parses the dialog on a pane's screen, if it is waiting on one.
// Parses are cached per pane until it prints again (or for a second at most),
// since every frame asks for each pane's state.
func panePrompt(p *term.Pane) *claude.Prompt {
	if p == nil || p.Exited() || p.Agent != "claude" {
		return nil
	}
	now := time.Now()
	out := now.Add(-p.IdleFor())
	promptMu.Lock()
	defer promptMu.Unlock()
	if c, ok := promptCache[p]; ok && now.Sub(c.at) < time.Second && c.out.Sub(out).Abs() < 5*time.Millisecond {
		return c.p
	}
	pr := claude.ParsePrompt(p.Text(paneScreen))
	promptCache[p] = cachedPrompt{now, out, pr}
	if len(promptCache) > 64 {
		for k := range promptCache {
			if k.Exited() {
				delete(promptCache, k)
			}
		}
	}
	return pr
}

type cachedPrompt struct {
	at, out time.Time
	p       *claude.Prompt
}

var (
	promptMu    sync.Mutex
	promptCache = map[*term.Pane]cachedPrompt{}
)

// agentState is the pane's screen state; a parsed dialog anywhere on screen
// counts as blocked (a short session leaves it high above the bottom rows).
func agentState(p *term.Pane) string {
	st := p.AgentState()
	if st == "idle" && panePrompt(p) != nil {
		return "blocked"
	}
	return st
}

// rowPrompt is the dialog a live agent row is blocked on.
func (r liveRow) prompt() *claude.Prompt {
	if r.sub != nil || r.local == nil {
		return nil
	}
	return panePrompt(r.local.pane)
}

func (m *Model) openAnswer(ref paneRef, title string) bool {
	p := panePrompt(ref.pane)
	if p == nil {
		return false
	}
	cur := p.Selected
	if y := p.Yes(); y != nil {
		cur = indexOf(p, y.Key)
	}
	m.answer = &answerBox{ref: ref, title: title, cur: cur}
	return true
}

func indexOf(p *claude.Prompt, k string) int {
	for i, o := range p.Options {
		if o.Key == k {
			return i
		}
	}
	return 0
}

// answerPane presses an option's digit in the pane; it re-reads the screen
// first so a prompt answered meanwhile is never answered twice.
func (m *Model) answerPane(ref paneRef, pick func(p *claude.Prompt) *claude.Option) {
	p := panePrompt(ref.pane)
	if p == nil {
		m.setStatus(T("it isn't waiting anymore"), false)
		return
	}
	o := pick(p)
	if o == nil {
		m.setStatus(T("that choice isn't offered here"), true)
		return
	}
	ref.pane.Write([]byte(p.Press(p.Index(o))))
	m.setStatus(Tf("answered “%s”", trunc(o.Label, 40))+" · "+ref.tab.Label, false)
}

func (m Model) answerKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	a := m.answer
	p := panePrompt(a.ref.pane)
	if p == nil {
		m.answer = nil
		m.setStatus(T("it isn't waiting anymore"), false)
		return m, nil
	}
	n := len(p.Options)
	done := func(pick func(p *claude.Prompt) *claude.Option) (tea.Model, tea.Cmd) {
		m.answer = nil
		m.answerPane(a.ref, pick)
		return m, nil
	}
	switch ks := k.String(); ks {
	case "esc", "q":
		m.answer = nil
	case "j", "down", "tab":
		a.cur = min(n-1, a.cur+1)
	case "k", "up", "shift+tab":
		a.cur = max(0, a.cur-1)
	case "enter":
		i := min(a.cur, n-1)
		return done(func(p *claude.Prompt) *claude.Option { return &p.Options[i] })
	case "y":
		return done((*claude.Prompt).Yes)
	case "a":
		return done((*claude.Prompt).Always)
	case "n":
		return done((*claude.Prompt).No)
	case "g", "o":
		m.answer = nil
		m.focusRef(a.ref)
	case "t", "m":
		m.answer = nil
		return m, m.tellInstead(a.ref)
	default:
		if len(ks) == 1 && ks[0] >= '1' && ks[0] <= '9' {
			return done(func(p *claude.Prompt) *claude.Option {
				for i, o := range p.Options {
					if o.Key == ks {
						return &p.Options[i]
					}
				}
				return nil
			})
		}
	}
	return m, nil
}

func (m Model) viewAnswer() string {
	s := m.st
	a := m.answer
	w := min(84, m.w-4)
	inner := w - 6
	var b strings.Builder
	b.WriteString(s.Red.Render("● "+T("needs you")) + s.Muted.Render("  "+trunc(a.title, inner-14)) + "\n")
	b.WriteString(s.Muted.Render(trunc("◆ "+a.ref.space.Name+"/"+a.ref.tab.Label, inner)) + "\n\n")
	p := panePrompt(a.ref.pane)
	if p == nil {
		b.WriteString(s.Green.Render("✓ "+T("it isn't waiting anymore")) + "\n\n" + s.hints([][2]string{{"esc", T("close")}}))
		return s.Modal.Width(w + 2).Render(b.String())
	}
	b.WriteString(strings.Join(m.promptLines(p, inner, 4), "\n"))
	b.WriteString("\n\n")
	a.cur = min(a.cur, len(p.Options)-1)
	for i, o := range p.Options {
		l := " " + s.Key.Render(o.Key) + "  " + s.Text.Render(trunc(o.Label, inner-5))
		if i == a.cur {
			l = s.Sel(s.RowSel, fit(s.Accent.Render("❯")+l, inner))
		} else {
			l = " " + l
		}
		b.WriteString(l + "\n")
	}
	keys := [][2]string{{"⏎", T("choose")}}
	if p.Yes() != nil {
		keys = append(keys, [2]string{"y", T("yes")})
	}
	if p.Always() != nil {
		keys = append(keys, [2]string{"a", T("always")})
	}
	if p.No() != nil {
		keys = append(keys, [2]string{"n", T("no")})
	}
	keys = append(keys, [2]string{"t", T("tell it something else")}, [2]string{"g", T("go to pane")}, [2]string{"esc", T("close")})
	b.WriteString("\n" + s.hintsFit(keys, inner))
	return s.Modal.Width(w + 2).Render(b.String())
}

// promptLines renders a prompt's title, what it asks about and the question.
func (m Model) promptLines(p *claude.Prompt, w, maxDetail int) []string {
	s := m.st
	var ls []string
	head := s.Yellow.Render("⚠ " + trunc(p.Title, w-2))
	if p.Title == "" {
		head = s.Yellow.Render("⚠ " + T("waiting for an answer"))
	}
	ls = append(ls, head)
	for i, d := range p.Detail {
		if i >= maxDetail {
			break
		}
		st := s.Muted
		if i == 0 {
			st = s.Bold
		}
		ls = append(ls, "  "+st.Render(trunc(d, w-2)))
	}
	if p.Question != "" {
		ls = append(ls, "  "+s.Text.Render(trunc(p.Question, w-2)))
	}
	return ls
}

// promptSummary is the short form used in rows, toasts and the inbox.
func promptSummary(p *claude.Prompt) string {
	if p == nil {
		return ""
	}
	t := strings.TrimSuffix(strings.TrimSuffix(p.Title, " command"), " file")
	if t == "" {
		return p.Subject()
	}
	return fmt.Sprintf("%s · %s", t, p.Subject())
}

// tellInstead declines the prompt and types a message for the agent instead,
// so you can steer it ("use the staging DB", "run only the unit tests").
func (m *Model) tellInstead(ref paneRef) tea.Cmd {
	m.prompt(T("Tell it what to do instead"), T("Declines the request and sends your message to the agent."), T("e.g. don't touch the DB, mock it in the test"),
		func(m *Model, v string) tea.Cmd {
			if v == "" {
				return nil
			}
			pane := ref.pane
			if p := panePrompt(pane); p != nil {
				if o := p.Tell(); o != nil {
					pane.Write([]byte(p.Press(p.Index(o))))
				} else {
					pane.Write([]byte("\x1b"))
				}
			}
			label := ref.tab.Label
			return tea.Tick(700*time.Millisecond, func(time.Time) tea.Msg {
				pane.Write([]byte("\x1b[200~" + v + "\x1b[201~"))
				pane.Write([]byte("\r"))
				return statusMsg{Tf("told %s: %s", label, trunc(v, 40)), false}
			})
		})
	return nil
}
