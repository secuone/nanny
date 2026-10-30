package ui

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/secuone/nanny/internal/claude"
	"github.com/secuone/nanny/internal/config"
)

func shellQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n'\"\\$`!*?[](){}<>|&;#~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// commandArgv splits simple commands; anything shell-ish runs through sh -c.
func commandArgv(command string) []string {
	if command == "" {
		return nil
	}
	if strings.ContainsAny(command, "|&;<>()$`\\\"'*?[]#~=") {
		return []string{"/bin/sh", "-c", command}
	}
	return strings.Fields(command)
}

// run executes command in cwd: a new tab in nanny's spaces (default), or by
// suspending the TUI (inline).
func (m *Model) run(cwd, label, command string) tea.Cmd {
	if cwd == "" {
		cwd, _ = os.UserHomeDir()
	}
	if m.cfg.Launch != config.LaunchInline {
		return m.openTab(cwd, label, commandArgv(command))
	}
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/sh"
	}
	var c *exec.Cmd
	if command == "" {
		c = exec.Command(sh)
	} else {
		c = exec.Command(sh, "-c", command)
	}
	c.Dir = cwd
	return tea.ExecProcess(c, func(err error) tea.Msg { return execDoneMsg{err} })
}

func (m *Model) resumeCmd(s *claude.Session, fork bool) string {
	cmd := "claude --resume " + s.ID
	if fork {
		cmd += " --fork-session"
	}
	if m.cfg.ClaudeArgs != "" {
		cmd += " " + m.cfg.ClaudeArgs
	}
	return cmd
}

func (m *Model) resume(s *claude.Session, fork bool) tea.Cmd {
	if s == nil {
		return nil
	}
	if s.Live != nil && !fork {
		// already running: jump to it instead of starting a duplicate
		if r, ok := m.localPane(s.ID); ok {
			m.focusRef(r)
			return nil
		}
		m.ask(T("Session is already running"),
			Tf("“%s” is live (pid %d) and isn't in a pane I can jump to.\nOpen a second copy anyway? (claude will fork it)", trunc(s.Title, 60), s.Live.Pid),
			func(m *Model, _ string) tea.Cmd { return m.run(s.ResumeCwd(), label(s), m.resumeCmd(s, true)) })
		return nil
	}
	if _, err := os.Stat(s.Cwd); err != nil && s.Cwd != "" {
		m.setStatus(Tf("folder is gone (%s) — resuming from ~", s.Cwd), true)
	}
	return m.run(s.ResumeCwd(), label(s), m.resumeCmd(s, fork))
}

func label(s *claude.Session) string {
	l := s.Title
	if s.Worktree != "" {
		l = s.Worktree + " · " + l
	}
	return trunc(l, 28)
}

func (m *Model) shell(cwd string) tea.Cmd {
	if _, err := os.Stat(cwd); err != nil {
		m.setStatus(T("folder doesn't exist: ")+cwd, true)
		return nil
	}
	return m.run(cwd, filepath.Base(cwd), "")
}

// copy puts text on the clipboard: native tool if present, else OSC 52.
func (m *Model) copy(text, what string) tea.Cmd {
	return func() tea.Msg {
		for _, c := range [][]string{{"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"xsel", "-ib"}} {
			if _, err := exec.LookPath(c[0]); err == nil {
				cmd := exec.Command(c[0], c[1:]...)
				cmd.Stdin = strings.NewReader(text)
				if cmd.Run() == nil {
					return statusMsg{Tf("copied %s", what), false}
				}
			}
		}
		if tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0); err == nil {
			fmt.Fprintf(tty, "\x1b]52;c;%s\x07", base64.StdEncoding.EncodeToString([]byte(text)))
			tty.Close()
			return statusMsg{Tf("copied %s", what) + " (osc52)", false}
		}
		return statusMsg{T("no clipboard available"), true}
	}
}

func (m *Model) editConfig() tea.Cmd {
	ed := os.Getenv("VISUAL")
	if ed == "" {
		ed = os.Getenv("EDITOR")
	}
	if ed == "" {
		ed = "nano"
	}
	c := exec.Command(ed, config.Path())
	return tea.ExecProcess(c, func(err error) tea.Msg { return configEditedMsg{err} })
}

type configEditedMsg struct{ err error }

// editSnippets opens snippets.toml in $EDITOR, writing the defaults first.
func (m *Model) editSnippets() tea.Cmd {
	if err := config.EnsureSnippets(); err != nil {
		m.setStatus(err.Error(), true)
		return nil
	}
	ed := os.Getenv("VISUAL")
	if ed == "" {
		ed = os.Getenv("EDITOR")
	}
	if ed == "" {
		ed = "nano"
	}
	return tea.ExecProcess(exec.Command(ed, config.SnippetsPath()), func(err error) tea.Msg { return execDoneMsg{err} })
}
