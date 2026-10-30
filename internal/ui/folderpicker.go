package ui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/secuone/nanny/internal/discover"
)

// folderPicker asks for a folder: a path input with tab completion, and the
// folders you already work in (agent sessions, git repos, your spaces) to pick
// from or to filter by typing.
type folderPicker struct {
	title, body string
	in          textinput.Model
	known       []string // folders you work in, most useful first
	busy        bool
	cands       []string // what is listed now
	cur         int      // selected candidate; -1 until the arrows pick one
	err         string
	create      string // a folder that does not exist yet, waiting for a yes
	done        func(m *Model, dir string) tea.Cmd
}

type folderPlacesMsg []discover.Place

const maxCands = 60

// openFolderPicker shows the picker with start typed in; done gets the folder, checked to exist.
func (m *Model) openFolderPicker(title, body, start string, done func(m *Model, dir string) tea.Cmd) tea.Cmd {
	in := newInput()
	in.Prompt = "› "
	in.Placeholder = T("~/path/to/folder")
	in.SetValue(start)
	in.CursorEnd()
	in.Focus()
	f := &folderPicker{title: title, body: body, in: in, cur: -1, busy: true, done: done}
	f.known = m.knownFolders()
	f.refresh()
	m.fp = f
	return m.placesCmd()
}

// knownFolders are the folders of your spaces and of the repos you added, right away;
// the scan of sessions and repos adds the rest.
func (m *Model) knownFolders() []string {
	var out []string
	for _, sp := range m.spaces {
		out = append(out, sp.Paths...)
	}
	return uniqueDirs(append(out, m.cfg.ExtraRepos...))
}

func (m *Model) placesCmd() tea.Cmd {
	sessions := m.sessions
	roots := m.cfg.DiscoverRoots
	if len(roots) == 0 {
		roots = discover.DefaultRoots()
	} else {
		roots = append([]string{}, roots...)
		for i, r := range roots {
			roots[i] = expandPath(r)
		}
	}
	extra := m.knownFolders()
	return func() tea.Msg { return folderPlacesMsg(discover.Find(sessions, roots, extra)) }
}

func uniqueDirs(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range in {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// pathLike says the text is a path being typed, not a name to look for.
func pathLike(v string) bool {
	return strings.HasPrefix(v, "~") || strings.HasPrefix(v, "/") || strings.HasPrefix(v, ".")
}

// completeDir lists the folders that continue typed, spelled the way it was
// typed (so ~ stays ~): typed up to its last slash, then each match and a slash.
func completeDir(typed string) []string {
	head, prefix := typed, ""
	if i := strings.LastIndexByte(typed, '/'); i >= 0 {
		head, prefix = typed[:i+1], typed[i+1:]
	} else if typed == "~" {
		head = "~/"
	} else {
		head, prefix = "", typed
	}
	dir := head
	if dir == "" {
		dir = "./"
	}
	ents, err := os.ReadDir(expandPath(dir))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		name := e.Name()
		if !strings.HasPrefix(name, prefix) || strings.HasPrefix(name, ".") && !strings.HasPrefix(prefix, ".") {
			continue
		}
		if st, err := os.Stat(filepath.Join(expandPath(dir), name)); err != nil || !st.IsDir() {
			continue
		}
		out = append(out, head+name+"/")
		if len(out) >= maxCands {
			break
		}
	}
	sort.Strings(out)
	return out
}

// filterKnown keeps the folders whose path holds the letters of q in order; names that start with q first.
func filterKnown(known []string, q string) []string {
	q = strings.ToLower(q)
	var first, rest []string
	for _, p := range known {
		low := strings.ToLower(tilde(p))
		if q != "" && !subsequence(low, q) {
			continue
		}
		if q != "" && strings.HasPrefix(strings.ToLower(filepath.Base(p)), q) {
			first = append(first, p)
		} else {
			rest = append(rest, p)
		}
		if len(first)+len(rest) >= maxCands {
			break
		}
	}
	return append(first, rest...)
}

func subsequence(s, q string) bool {
	i := 0
	for _, r := range s {
		if i < len(q) && rune(q[i]) == r {
			i++
		}
	}
	return i == len(q)
}

// refresh lists what matches what is typed: the folders that continue a path, or your known folders.
func (f *folderPicker) refresh() {
	v := strings.TrimSpace(f.in.Value())
	if pathLike(v) {
		f.cands = completeDir(v)
	} else {
		f.cands = filterKnown(f.known, v)
	}
	f.cur = -1
	f.err = ""
}

// complete is tab: the folder picked, else what all the matches share, else the first match.
func (f *folderPicker) complete() {
	v := strings.TrimSpace(f.in.Value())
	if len(f.cands) == 0 {
		return
	}
	pick, whole := "", true
	switch {
	case f.cur >= 0:
		pick = f.cands[f.cur]
	case len(f.cands) == 1:
		pick = f.cands[0]
	case pathLike(v) && len(commonPrefix(f.cands)) > len(v):
		pick, whole = commonPrefix(f.cands), false
	default:
		f.cur = 0
		return
	}
	if whole && !strings.HasSuffix(pick, "/") {
		pick = tilde(pick) + "/"
	}
	f.in.SetValue(pick)
	f.in.CursorEnd()
	f.refresh()
}

func commonPrefix(ss []string) string {
	p := ss[0]
	for _, s := range ss[1:] {
		for !strings.HasPrefix(s, p) {
			p = p[:len(p)-1]
		}
	}
	return p
}

// choose checks the folder typed (or picked) and hands it to done.
func (m *Model) fpChoose() tea.Cmd {
	f := m.fp
	v := strings.TrimSpace(f.in.Value())
	if f.cur >= 0 && f.cur < len(f.cands) {
		v = f.cands[f.cur]
	}
	if v == "" {
		f.err = T("choose a folder first")
		return nil
	}
	dir := expandPath(v)
	st, err := os.Stat(dir)
	switch {
	case err != nil && os.IsNotExist(err):
		f.create = dir
		return nil
	case err != nil || !st.IsDir():
		f.err = T("not a folder: ") + v
		return nil
	}
	m.fp = nil
	return f.done(m, dir)
}

func (m Model) folderKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	f := m.fp
	ks := k.String()
	if f.create != "" {
		switch ks {
		case "y", "Y", "enter":
			if err := os.MkdirAll(f.create, 0o755); err != nil {
				f.err, f.create = T("could not create the folder: ")+err.Error(), ""
				return m, nil
			}
			f.create = ""
			return m, m.fpChoose()
		case "n", "N", "esc":
			f.create = ""
		}
		return m, nil
	}
	switch ks {
	case "esc", "ctrl+c":
		m.fp = nil
	case "enter":
		return m, m.fpChoose()
	case "tab":
		f.complete()
	case "down", "ctrl+n":
		f.cur = min(len(f.cands)-1, f.cur+1)
	case "up", "ctrl+p":
		f.cur = max(-1, f.cur-1)
	default:
		in, cmd := f.in.Update(k)
		f.in = in
		f.refresh()
		return m, cmd
	}
	return m, nil
}

func (m Model) viewFolderPicker() string {
	s, f := m.st, m.fp
	w := min(84, m.w-6)
	inner := w - 6
	var b strings.Builder
	b.WriteString(s.Accent.Render(f.title) + "\n")
	if f.body != "" {
		b.WriteString(s.Muted.Render(fit(f.body, inner)) + "\n")
	}
	b.WriteString("\n" + f.in.View() + "\n")
	switch {
	case f.create != "":
		b.WriteString("\n" + s.Yellow.Render(Tf("%s does not exist · create it?", tilde(f.create))) + "\n\n")
		b.WriteString(s.hintsFit([][2]string{{"y", T("create")}, {"n", T("back")}}, inner))
		return s.Modal.Width(w + 2).Render(b.String())
	case f.err != "":
		b.WriteString(s.Red.Render(fit(f.err, inner)) + "\n")
	default:
		b.WriteString("\n")
	}
	label := T("known folders")
	if pathLike(strings.TrimSpace(f.in.Value())) {
		label = T("folders here")
	}
	if f.busy && len(f.known) == 0 {
		label += " · " + T("scanning…")
	}
	b.WriteString(s.Muted.Render(label) + "\n")
	rows := max(3, min(10, m.h-18))
	start := 0
	if f.cur >= rows {
		start = f.cur - rows + 1
	}
	for i := start; i < min(len(f.cands), start+rows); i++ {
		line := " " + s.Peach.Render(trunc(tilde(f.cands[i]), inner-2))
		if i == f.cur {
			line = s.Sel(s.RowSel, fit(line, inner))
		}
		b.WriteString(line + "\n")
	}
	if len(f.cands) == 0 {
		b.WriteString(s.Muted.Render(" "+T("no folder matches")) + "\n")
	}
	b.WriteString("\n" + s.hintsFit([][2]string{{"tab", T("complete")}, {"↑↓", T("pick")}, {"⏎", T("use")}, {"esc", T("cancel")}}, inner))
	return s.Modal.Width(w + 2).Render(b.String())
}
