package ui

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/secuone/nanny/internal/config"
	"github.com/secuone/nanny/internal/jira"
	"github.com/secuone/nanny/internal/secret"
)

// The first-run guide that connects Jira and GitHub, shown over the screen
// like the welcome. It opens when the welcome closes, once; Settings →
// Connections, the ≡ menu and the palette bring it back.

const (
	setupIntro = iota
	setupJira
	setupGitHub
	setupSummary
)

const tokenPage = "https://id.atlassian.com/manage-profile/security/api-tokens"

type setupWizard struct {
	step, field       int
	url, email, token textinput.Model
	busy              bool // testing the connection
	note              string
	noteErr           bool
	connected         bool
	ghBusy            bool
	login             string
	ghNote            string
}

type setupJiraMsg struct {
	user       jira.User
	url, email string
	err        error
}

type setupGHMsg struct {
	login string
	err   error
}

func setupDonePath() string { return filepath.Join(config.StateDir(), "setup-done") }

func setupDone() bool {
	_, err := os.Stat(setupDonePath())
	return err == nil
}

// maybeSetup opens the wizard when it was never done; the welcome calls it as it closes.
func (m *Model) maybeSetup() {
	if os.Getenv("NANNY_NO_WELCOME") != "" || setupDone() {
		return
	}
	m.openSetup(setupIntro)
}

// finishSetup closes the wizard for good: it won't open by itself again.
func (m *Model) finishSetup() {
	m.setup = nil
	p := setupDonePath()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = os.WriteFile(p, nil, 0o644)
}

func (m *Model) openSetup(step int) {
	mk := func(prompt, ph, v string) textinput.Model {
		in := newInput()
		in.Prompt, in.Placeholder = prompt, ph
		in.SetValue(v)
		in.CharLimit = 300
		return in
	}
	email := m.cfg.Jira.Email
	if email == "" {
		email = gitEmail()
	}
	w := &setupWizard{step: step,
		url:   mk("URL    › ", "https://your-company.atlassian.net", m.cfg.Jira.URL),
		email: mk("Email  › ", "you@your-company.com", email),
		token: mk("Token  › ", T("API token"), ""),
	}
	w.token.EchoMode = textinput.EchoPassword
	w.token.EchoCharacter = '•'
	m.setup = w
	w.enter(m)
}

// enter is what a step does as it opens.
func (w *setupWizard) enter(m *Model) tea.Cmd {
	switch w.step {
	case setupJira:
		w.field = 0
		if w.url.Value() != "" {
			w.field = 1
		}
		if w.email.Value() != "" && w.field == 1 {
			w.field = 2
		}
		w.focus()
		return textinput.Blink
	case setupGitHub, setupSummary:
		if !w.ghBusy {
			w.ghBusy = true
			return ghLoginCmd()
		}
	}
	return nil
}

func (w *setupWizard) focus() {
	for i, in := range []*textinput.Model{&w.url, &w.email, &w.token} {
		if i == w.field && w.step == setupJira {
			in.Focus()
		} else {
			in.Blur()
		}
	}
}

func (w *setupWizard) input() *textinput.Model {
	return []*textinput.Model{&w.url, &w.email, &w.token}[w.field]
}

// gitEmail is the email git commits with, a good guess for the Jira account.
func gitEmail() string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "config", "user.email").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ghLoginCmd asks gh who is logged in.
func ghLoginCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "gh", "api", "user", "--jq", ".login").Output()
		login := strings.TrimSpace(string(out))
		if err != nil || login == "" {
			return setupGHMsg{err: errors.New("not logged in")}
		}
		return setupGHMsg{login: login}
	}
}

// testJiraCmd asks who the token is, and keeps it where only you can read it when it works.
func testJiraCmd(url, email, token string) tea.Cmd {
	return func() tea.Msg {
		cfg := config.Jira{URL: url, Email: email}
		base := cfg.BaseURL()
		ctx, cancel := jiraCtx()
		defer cancel()
		u, err := newJiraClient(base, email, token).Myself(ctx)
		if err != nil {
			return setupJiraMsg{err: err}
		}
		cfg.URL = base
		if err := secret.Set(secret.JiraService, cfg.Account(), token); err != nil {
			return setupJiraMsg{err: err}
		}
		return setupJiraMsg{user: u, url: base, email: email}
	}
}

// setupUpdate takes the messages of the wizard's own requests.
func (m *Model) setupUpdate(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case setupJiraMsg:
		return m.jiraTested(msg), true
	case setupGHMsg:
		if w := m.setup; w != nil {
			w.ghBusy, w.login, w.ghNote = false, msg.login, ""
			if msg.err != nil {
				w.ghNote = T("not logged in")
			}
		}
		return nil, true
	}
	return nil, false
}

func (m *Model) jiraTested(msg setupJiraMsg) tea.Cmd {
	w := m.setup
	if w != nil {
		w.busy = false
	}
	if msg.err != nil {
		if w != nil {
			w.note, w.noteErr = jiraErrText(msg.err), true
		}
		return nil
	}
	m.cfg.Jira.URL, m.cfg.Jira.Email = msg.url, msg.email
	if err := config.Save(m.cfg); err != nil {
		m.setStatus(T("save failed: ")+err.Error(), true)
	}
	if w != nil {
		w.note, w.noteErr, w.connected = Tf("✓ connected as %s", msg.user.Name), false, true
		w.token.SetValue("")
	}
	j := m.jira
	j.noToken, j.unauth, j.err, j.at, j.retryAt = false, false, "", time.Time{}, time.Time{}
	return j.due(m, true)
}

func (m Model) setupKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	w := m.setup
	ks := k.String()
	if ks == "esc" || ks == "ctrl+c" {
		m.finishSetup()
		return m, nil
	}
	switch w.step {
	case setupIntro:
		switch ks {
		case "s":
			m.finishSetup()
		case "enter", "right", "n", " ", "space", "tab":
			return m, m.setupGo(setupJira)
		}
		return m, nil
	case setupJira:
		return m.setupJiraKey(k)
	case setupGitHub:
		switch ks {
		case "enter":
			if w.login != "" {
				return m, m.setupGo(setupSummary)
			}
			cmd := m.openTab(home, "gh auth login", []string{"gh", "auth", "login"})
			return m, tea.Batch(cmd, m.setupGo(setupSummary))
		case "right", "n", "tab":
			return m, m.setupGo(setupSummary)
		case "left", "p":
			return m, m.setupGo(setupJira)
		case "r":
			return m, w.enter(&m)
		}
	case setupSummary:
		switch ks {
		case "enter":
			m.finishSetup()
		case "left", "p":
			return m, m.setupGo(setupGitHub)
		case "r":
			return m, w.enter(&m)
		}
	}
	return m, nil
}

func (m *Model) setupGo(step int) tea.Cmd {
	m.setup.step = step
	return m.setup.enter(m)
}

func (m Model) setupJiraKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	w := m.setup
	switch k.String() {
	case "ctrl+o":
		return m, m.openURL(tokenPage)
	case "ctrl+n":
		return m, m.setupGo(setupGitHub)
	case "ctrl+p":
		return m, m.setupGo(setupIntro)
	case "tab", "down":
		w.field = (w.field + 1) % 3
		w.focus()
		return m, nil
	case "shift+tab", "up":
		w.field = (w.field + 2) % 3
		w.focus()
		return m, nil
	case "enter":
		switch {
		case w.busy:
		case w.connected:
			return m, m.setupGo(setupGitHub)
		case w.field < 2:
			w.field++
			w.focus()
		case w.url.Value() == "" || w.email.Value() == "" || w.token.Value() == "":
			w.note, w.noteErr = T("URL, email and token are all needed"), true
		default:
			w.busy, w.note = true, ""
			return m, testJiraCmd(strings.TrimSpace(w.url.Value()), strings.TrimSpace(w.email.Value()), strings.TrimSpace(w.token.Value()))
		}
		return m, nil
	}
	in := w.input()
	var cmd tea.Cmd
	*in, cmd = in.Update(k)
	w.connected = false
	return m, cmd
}

func (m Model) viewSetup() string {
	s := m.st
	w := m.setup
	width := min(84, m.w-4)
	inner := width - 6
	var b strings.Builder
	para := func(style lipgloss.Style, texts ...string) {
		for _, t := range texts {
			for _, l := range wrap(t, inner, 4) {
				b.WriteString(style.Render(l) + "\n")
			}
			b.WriteString("\n")
		}
	}
	nav := clickable("esc", s.Key.Render("esc")+" "+s.KeyDesc.Render(T("close")))
	switch w.step {
	case setupIntro:
		b.WriteString(s.Accent.Render(T("Setup wizard")) + "\n\n")
		para(s.Text, T("Two connections make nanny much more useful: Jira, to see your issues and start work on them, and GitHub, for pull requests and reviews."),
			T("Both are optional, and you can come back to this from Settings (8) → Connections."))
		nav = clickable("enter", s.Key.Render("⏎")+" "+s.KeyDesc.Render(T("start"))) + s.Dim.Render(" · ") +
			clickable("s", s.Key.Render("s")+" "+s.KeyDesc.Render(T("skip all"))) + s.Dim.Render(" · ") + nav
	case setupJira:
		b.WriteString(s.Accent.Render(T("1/3 · Jira")) + "\n\n")
		para(s.Text, T("Your site, your email and an API token. The token is kept in your keyring or a private file, never in the config."))
		for _, in := range []textinput.Model{w.url, w.email, w.token} {
			b.WriteString(in.View() + "\n")
		}
		b.WriteString("\n")
		switch {
		case w.busy:
			b.WriteString(s.Muted.Render(spinGlyph+" "+T("connecting…")) + "\n\n")
		case w.note != "" && w.noteErr:
			b.WriteString(s.Red.Render(trunc("✗ "+w.note, inner)) + "\n\n")
		case w.note != "":
			b.WriteString(s.Green.Render(w.note) + "\n\n")
		}
		go_ := T("test and save")
		if w.connected {
			go_ = T("next")
		}
		nav = clickable("enter", s.Key.Render("⏎")+" "+s.KeyDesc.Render(go_)) + s.Dim.Render(" · ") +
			clickable("ctrl+o", s.Key.Render("^o")+" "+s.KeyDesc.Render(T("get a token"))) + s.Dim.Render(" · ") +
			clickable("ctrl+n", s.Key.Render("^n")+" "+s.KeyDesc.Render(T("skip"))) + s.Dim.Render(" · ") + nav
	case setupGitHub:
		b.WriteString(s.Accent.Render(T("2/3 · GitHub")) + "\n\n")
		para(s.Text, T("nanny asks gh for your pull requests and reviews. It needs to be logged in."))
		act := T("log in")
		switch {
		case w.ghBusy:
			b.WriteString(s.Muted.Render(spinGlyph+" "+T("asking gh…")) + "\n\n")
		case w.login != "":
			b.WriteString(s.Green.Render(Tf("✓ connected as %s", w.login)) + "\n\n")
			act = T("next")
		default:
			b.WriteString(s.Yellow.Render(T("gh isn't logged in: ⏎ runs `gh auth login` in a new tab.")) + "\n\n")
		}
		nav = clickable("enter", s.Key.Render("⏎")+" "+s.KeyDesc.Render(act)) + s.Dim.Render(" · ") +
			clickable("n", s.Key.Render("n")+" "+s.KeyDesc.Render(T("skip"))) + s.Dim.Render(" · ") + nav
	case setupSummary:
		b.WriteString(s.Accent.Render(T("3/3 · Done")) + "\n\n")
		jiraLine := s.Muted.Render(T("not connected"))
		if m.cfg.Jira.BaseURL() != "" {
			jiraLine = s.Green.Render("✓ " + m.cfg.Jira.Host())
		}
		ghLine := s.Muted.Render(T("not logged in"))
		if w.login != "" {
			ghLine = s.Green.Render("✓ " + w.login)
		} else if w.ghBusy {
			ghLine = s.Muted.Render(spinGlyph)
		}
		b.WriteString(s.Key.Render(fit("Jira", 10)) + jiraLine + "\n")
		b.WriteString(s.Key.Render(fit("GitHub", 10)) + ghLine + "\n\n")
		para(s.Muted, T("Settings (8) → Connections opens this again."))
		nav = clickable("enter", s.Key.Render("⏎")+" "+s.KeyDesc.Render(T("start")))
	}
	b.WriteString(nav)
	return s.Modal.Width(width).Render(b.String())
}
