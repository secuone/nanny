package ui

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/secuone/nanny/internal/gitwt"
)

// review is a full-screen view of a checkout's changes: files on the left,
// colored hunks on the right, with the usual next steps one key away.
type review struct {
	dir      string
	branch   string
	branchUp bool // diff the whole branch against its base, not just uncommitted work
	baseName string
	baseSHA  string
	files    []gitwt.Change
	commits  []string
	cur      int
	diff     []string
	off      int
	focusR   bool
	loaded   bool
	err      string
}

type reviewMsg struct {
	dir      string
	branchUp bool
	baseName string
	baseSHA  string
	files    []gitwt.Change
	commits  []string
	err      string
}

type reviewDiffMsg struct {
	dir, path string
	lines     []string
}

func (m *Model) openReview(dir string) tea.Cmd {
	if dir == "" {
		return nil
	}
	if root := gitwt.RepoRoot(dir); root == "" {
		m.setStatus(T("not a git repository: ")+tilde(dir), true)
		return nil
	}
	m.rev = &review{dir: dir, branch: branchOf(dir)}
	return reviewCmd(dir, false, true)
}

// reviewCmd loads the file list; auto switches to the branch diff when the
// checkout has no uncommitted work.
func reviewCmd(dir string, branchUp, auto bool) tea.Cmd {
	return func() tea.Msg {
		name, sha := gitwt.Base(dir)
		msg := reviewMsg{dir: dir, branchUp: branchUp, baseName: name, baseSHA: sha}
		base := ""
		if branchUp {
			base = sha
		}
		files, err := gitwt.Changes(dir, base)
		if auto && !branchUp && err == nil && len(files) == 0 && sha != "" {
			msg.branchUp = true
			files, err = gitwt.Changes(dir, sha)
		}
		if err != nil {
			msg.err = strings.TrimSpace(err.Error())
		}
		msg.files = files
		msg.commits = gitwt.Commits(dir, sha, 8)
		return msg
	}
}

func (r *review) base() string {
	if r.branchUp {
		return r.baseSHA
	}
	return ""
}

func (r *review) diffCmd() tea.Cmd {
	if r.cur < 0 || r.cur >= len(r.files) {
		return nil
	}
	dir, base, c := r.dir, r.base(), r.files[r.cur]
	return func() tea.Msg { return reviewDiffMsg{dir, c.Path, gitwt.FileDiff(dir, base, c)} }
}

// reviewUpdate applies the review's async results; ok is false for other messages.
func (m *Model) reviewUpdate(msg tea.Msg) (tea.Cmd, bool) {
	r := m.rev
	switch msg := msg.(type) {
	case reviewMsg:
		if r == nil || r.dir != msg.dir {
			return nil, true
		}
		keep := ""
		if r.cur < len(r.files) {
			keep = r.files[r.cur].Path
		}
		r.loaded, r.err = true, msg.err
		r.branchUp, r.baseName, r.baseSHA = msg.branchUp, msg.baseName, msg.baseSHA
		r.files, r.commits = msg.files, msg.commits
		r.cur = 0
		for i, f := range r.files {
			if f.Path == keep {
				r.cur = i
			}
		}
		r.diff = nil
		return r.diffCmd(), true
	case reviewDiffMsg:
		if r != nil && r.dir == msg.dir && r.cur < len(r.files) && r.files[r.cur].Path == msg.path {
			r.diff = msg.lines
		}
		return nil, true
	}
	return nil, false
}

func (m Model) reviewKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	r := m.rev
	page := max(1, m.bodyHeight()-6)
	pick := func(i int) tea.Cmd {
		i = max(0, min(len(r.files)-1, i))
		if i == r.cur {
			return nil
		}
		r.cur, r.off, r.diff = i, 0, nil
		return r.diffCmd()
	}
	scroll := func(d int) { r.off = max(0, min(r.off+d, len(r.diff)-1)) }
	switch k.String() {
	case "esc", "q":
		if r.focusR {
			r.focusR = false
			return m, nil
		}
		m.rev = nil
	case "j", "down":
		if r.focusR {
			scroll(1)
			return m, nil
		}
		return m, pick(r.cur + 1)
	case "k", "up":
		if r.focusR {
			scroll(-1)
			return m, nil
		}
		return m, pick(r.cur - 1)
	case "g", "home":
		if r.focusR {
			r.off = 0
			return m, nil
		}
		return m, pick(0)
	case "G", "end":
		if r.focusR {
			r.off = max(0, len(r.diff)-page)
			return m, nil
		}
		return m, pick(len(r.files) - 1)
	case "l", "right", "enter":
		r.focusR = true
	case "h", "left":
		r.focusR = false
	case "pgdown", "ctrl+d", " ", "space":
		scroll(page)
	case "pgup", "ctrl+u", "b":
		scroll(-page)
	case "n": // next hunk
		for i := r.off + 1; i < len(r.diff); i++ {
			if strings.HasPrefix(r.diff[i], "@@") {
				r.off = i
				break
			}
		}
	case "N":
		for i := r.off - 1; i >= 0; i-- {
			if strings.HasPrefix(r.diff[i], "@@") {
				r.off = i
				break
			}
		}
	case "t":
		if r.baseSHA == "" {
			m.setStatus(T("no base branch to compare with"), true)
			return m, nil
		}
		r.loaded = false
		return m, reviewCmd(r.dir, !r.branchUp, false)
	case "r", "R":
		r.loaded = false
		return m, reviewCmd(r.dir, r.branchUp, false)
	case "y":
		if r.branch != "" {
			return m, m.copy(r.branch, T("branch")+" "+r.branch)
		}
	case "o":
		return m, m.shell(r.dir)
	case "w":
		return m, openPR(r.dir)
	case "c":
		return m, m.askCommit(r.dir)
	}
	return m, nil
}

// openPR opens the checkout's pull request in the browser through gh.
func openPR(dir string) tea.Cmd {
	return func() tea.Msg {
		if _, err := exec.LookPath("gh"); err != nil {
			return statusMsg{T("gh isn't installed: can't find the PR"), true}
		}
		c := exec.Command("gh", "pr", "view", "--web")
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			return statusMsg{"gh: " + trunc(strings.TrimSpace(string(out)), 80), true}
		}
		return statusMsg{T("opened the PR in your browser"), false}
	}
}

const commitPrompt = "Review the uncommitted changes in this checkout and commit them with a clear conventional commit message. Don't push."

// askCommit types a commit request into the agent working in dir, or starts one.
func (m *Model) askCommit(dir string) tea.Cmd {
	var ref *paneRef
	top := gitwt.Toplevel(dir)
	for _, r := range m.allPanes() {
		if r.pane.Agent == "claude" && !r.pane.Exited() && gitwt.Toplevel(r.pane.Cwd) == top {
			r := r
			ref = &r
			break
		}
	}
	if ref == nil {
		m.ask(T("Start claude to commit?"), Tf("No agent of yours is running in %s. Start one with:", tilde(dir))+"\n\n“"+commitPrompt+"”",
			func(m *Model, _ string) tea.Cmd {
				m.rev = nil
				return m.openTab(dir, T("commit")+" · "+filepath.Base(dir), m.claudeArgv(commitPrompt))
			})
		return nil
	}
	if panePrompt(ref.pane) != nil {
		m.setStatus(T("that agent is waiting on a prompt — answer it first"), true)
		return nil
	}
	m.ask(Tf("Ask %s to commit?", trunc(ref.tab.Label, 30)), T("Typed into its pane:")+"\n\n“"+commitPrompt+"”",
		func(m *Model, _ string) tea.Cmd {
			typeInto(ref.pane, commitPrompt)
			m.setStatus(Tf("asked %s to commit", ref.tab.Label), false)
			return nil
		})
	return nil
}

func (m Model) viewReview(w, h int) string {
	s := m.st
	r := m.rev
	inner := w - 4
	var add, del int
	for _, f := range r.files {
		add += f.Add
		del += f.Del
	}
	mode := s.Yellow.Render(T("uncommitted")) + s.Muted.Render(" vs HEAD")
	if r.branchUp {
		mode = s.Blue.Render(T("branch")) + s.Muted.Render(" vs "+r.baseName)
	}
	total := s.Green.Render(fmt.Sprintf("+%d", add)) + " " + s.Red.Render(fmt.Sprintf("−%d", del)) + s.Muted.Render(" "+Tn(len(r.files), "in %d file", "in %d files"))
	headL := s.Bold.Render(filepath.Base(r.dir))
	if r.branch != "" {
		headL += " " + s.Blue.Render("⎇ "+r.branch)
	}
	headL += s.Muted.Render("  " + tilde(r.dir))
	head := []string{fit(headL, inner-lipglossW(total)-1) + " " + total, mode}
	if len(r.commits) > 0 {
		head[1] += s.Muted.Render("  ·  "+Tn(len(r.commits), "%d commit ahead: ", "%d commits ahead: ")) + s.Text.Render(trunc(r.commits[0], inner/2))
	}
	head = append(head, s.Dim.Render(strings.Repeat("─", inner)))
	room := bodyRows(h) - len(head)

	// file list | diff
	lw := min(44, max(24, inner/3))
	dw := inner - lw - 3
	var left []string
	switch {
	case !r.loaded:
		left = []string{s.Muted.Render(spinGlyph + " " + T("reading git…"))}
	case r.err != "":
		left = wrap(r.err, lw, 6)
	case len(r.files) == 0:
		left = []string{s.Green.Render("✓ " + T("nothing to review")), ""}
		left = append(left, wrap(T("No changes here. t compares the branch with its base."), lw, 4)...)
	}
	start := max(0, min(r.cur-room/2, len(r.files)-room))
	for i := start; r.loaded && i < min(len(r.files), start+room); i++ {
		f := r.files[i]
		st := map[string]func(...string) string{"M": s.Yellow.Render, "A": s.Green.Render, "?": s.Green.Render, "D": s.Red.Render, "R": s.Blue.Render}[f.Status]
		if st == nil {
			st = s.Muted.Render
		}
		num := s.Green.Render(fmt.Sprintf("+%d", f.Add)) + s.Red.Render(fmt.Sprintf(" −%d", f.Del))
		if f.Binary {
			num = s.Muted.Render("bin")
		}
		name := filepath.Base(f.Path)
		dir := filepath.Dir(f.Path)
		label := s.Text.Render(name)
		if dir != "." {
			label += s.Muted.Render(" " + dir)
		}
		l := fit(st(f.Status)+" "+trunc(label, lw-3-lipglossW(num)), lw-lipglossW(num)) + num
		if i == r.cur {
			sel := s.RowSel
			if r.focusR {
				sel = s.RowSelBlur
			}
			l = s.Sel(sel, fit(l, lw))
		}
		left = append(left, l)
	}
	var right []string
	if r.cur < len(r.files) {
		f := r.files[r.cur]
		name := f.Path
		if f.Old != "" {
			name = f.Old + " → " + f.Path
		}
		right = append(right, s.Bold.Render(trunc(name, dw)))
		if r.diff == nil {
			right = append(right, s.Muted.Render("…"))
		}
		off := min(r.off, max(0, len(r.diff)-1))
		for _, l := range r.diff[min(off, len(r.diff)):] {
			if len(right) >= room {
				break
			}
			if dl, ok := m.diffLine(l, dw); ok {
				right = append(right, dl)
			}
		}
	}
	sep := s.Dim.Render("│")
	if r.focusR {
		sep = s.Accent.Render("│")
	}
	body := make([]string, room)
	for i := range body {
		l, rr := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			rr = right[i]
		}
		body[i] = fit(l, lw) + " " + sep + " " + fit(rr, dw)
	}
	return s.panel(T("Changes"), strings.Join(append(head, body...), "\n"), w, h, true)
}

// diffLine colors one unified-diff line; git's file headers are dropped.
func (m Model) diffLine(l string, w int) (string, bool) {
	s := m.st
	l = strings.ReplaceAll(l, "\t", "    ")
	switch {
	case strings.HasPrefix(l, "diff --git"), strings.HasPrefix(l, "index "), strings.HasPrefix(l, "--- "), strings.HasPrefix(l, "+++ "):
		return "", false
	case strings.HasPrefix(l, "@@"):
		return s.Teal.Render(clip(l, w)), true
	case strings.HasPrefix(l, "+"):
		return s.Green.Render(clip(l, w)), true
	case strings.HasPrefix(l, "-"):
		return s.Red.Render(clip(l, w)), true
	case strings.HasPrefix(l, "new file"), strings.HasPrefix(l, "deleted file"), strings.HasPrefix(l, "rename "),
		strings.HasPrefix(l, "similarity"), strings.HasPrefix(l, "Binary"), strings.HasPrefix(l, "old mode"), strings.HasPrefix(l, "new mode"):
		return s.Muted.Render(clip(l, w)), true
	}
	return s.Text.Render(clip(l, w)), true
}

// clip truncates a line to w cells without collapsing its indentation.
func clip(l string, w int) string {
	if lipglossW(l) <= w {
		return l
	}
	return ansi.Truncate(l, w, "…")
}
