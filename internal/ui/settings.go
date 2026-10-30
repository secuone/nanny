package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/secuone/nanny/internal/config"
	"github.com/secuone/nanny/internal/i18n"
)

type setting struct {
	group string
	name  string
	help  string
	get   func(c *config.Config) string
	cycle func(c *config.Config, d int) // ←/→, nil for text settings
	set   func(c *config.Config, v string) error
	open  func(m *Model) tea.Cmd // ⏎ opens something else instead (Customize, a wizard…)
}

func boolS(b bool) string {
	if b {
		return T("on")
	}
	return T("off")
}

// langOptions are the Language setting's values: auto, then every language.
func langOptions() []string { return append([]string{"auto"}, i18n.Langs...) }

func cycleStr(opts []string, cur string, d int) string {
	i := 0
	for j, o := range opts {
		if o == cur {
			i = j
		}
	}
	return opts[(i+d+len(opts))%len(opts)]
}

func csv(v []string) string { return strings.Join(v, ", ") }

func splitCSV(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

var settingsList = []setting{
	{M("Look"), M("Theme"), M("Color theme. ←→ cycles, ⏎ or T opens the picker; tweak single colors under [colors] in the config file."),
		func(c *config.Config) string { return c.Theme },
		func(c *config.Config, d int) { c.Theme = cycleStr(config.ThemeNames(), c.Theme, d) }, nil, nil},
	{M("Look"), M("Language"), M("Interface language. auto follows $LC_ALL, $LC_MESSAGES or $LANG, and falls back to English."),
		func(c *config.Config) string {
			if c.Language == "" || c.Language == "auto" {
				return T("auto") + " · " + i18n.Name(i18n.Detect())
			}
			return i18n.Name(i18n.Resolve(c.Language))
		},
		func(c *config.Config, d int) {
			cur := c.Language
			if cur == "" {
				cur = "auto"
			}
			c.Language = cycleStr(langOptions(), cur, d)
		}, nil, nil},
	{M("Screen"), M("Screen profile"), M("What shows and what runs. Basic: the bare minimum. Full: everything. Yours: made in Customize (=). M walks them from anywhere; the word at the end of the top bar is the one in use."),
		func(c *config.Config) string { return profileLabel(c.ActiveProfile()) },
		func(c *config.Config, d int) { c.CycleProfile(d) }, nil, nil},
	{M("Screen"), M("Sidebar agents"), M("auto: a few with one line per agent, and left to Live without Extras. all: every agent, always. Agents in Customize turns them off."),
		func(c *config.Config) string { return T(firstOf(c.Screen.SidebarAgents, "auto")) },
		func(c *config.Config, d int) {
			c.Screen.SidebarAgents = cycleStr([]string{"auto", "all"}, firstOf(c.Screen.SidebarAgents, "auto"), d)
		}, nil, nil},
	{M("Screen"), customizeRow, M("Views, each Monitor source, the sidebar and the top bar, on and off. ⏎ or = opens it."),
		func(c *config.Config) string {
			if n := len(c.OffList(c.ActiveProfile())); n > 0 {
				return Tn(n, "%d thing off", "%d things off")
			}
			return T("everything on")
		}, nil, nil, func(m *Model) tea.Cmd { m.openCustomize(); return nil }},
	{M("Behaviour"), M("Open sessions in"), M("auto: a new tab in the session's space. inline: suspend nanny and run it in this terminal."),
		func(c *config.Config) string {
			return map[string]string{config.LaunchAuto: T("auto"), config.LaunchInline: T("inline")}[c.Launch]
		},
		func(c *config.Config, d int) {
			c.Launch = cycleStr([]string{config.LaunchAuto, config.LaunchInline}, c.Launch, d)
		}, nil, nil},
	{M("Behaviour"), M("Prefix key"), M("Prefix for pane commands inside spaces. Pick another one if your terminal or an outer multiplexer already uses it."),
		func(c *config.Config) string { return c.Prefix },
		func(c *config.Config, d int) {
			c.Prefix = cycleStr([]string{"ctrl+b", "ctrl+a", "ctrl+g", "ctrl+space", "ctrl+\\"}, c.Prefix, d)
		}, nil, nil},
	{M("Behaviour"), M("Restore on start"), M("Reopen your spaces and tabs; Claude panes resume their session."),
		func(c *config.Config) string { return boolS(c.Restore) },
		func(c *config.Config, d int) { c.Restore = !c.Restore }, nil, nil},
	{M("Behaviour"), M("Extra claude args"), M("Appended to every `claude --resume` / new claude, e.g. --model opus or --permission-mode auto."),
		func(c *config.Config) string { return c.ClaudeArgs }, nil,
		func(c *config.Config, v string) error { c.ClaudeArgs = v; return nil }, nil},
	{M("Behaviour"), M("Refresh every"), M("Seconds between live refreshes (transcripts are cached, so 1–2s is cheap)."),
		func(c *config.Config) string { return fmt.Sprintf("%ds", c.RefreshSecs) },
		func(c *config.Config, d int) { c.RefreshSecs = min(30, max(1, c.RefreshSecs+d)) }, nil, nil},
	{M("Behaviour"), M("Queue: parallel agents"), M("How many queued tasks run at once (u opens the task queue)."),
		func(c *config.Config) string { return strconv.Itoa(c.Queue.MaxParallel) },
		func(c *config.Config, d int) { c.Queue.MaxParallel = min(9, max(1, c.Queue.MaxParallel+d)) }, nil, nil},
	{M("Alerts"), M("Sound"), M("Master switch for sounds. S toggles it from anywhere."),
		func(c *config.Config) string { return boolS(c.Alerts.Sound) },
		func(c *config.Config, d int) { c.Alerts.Sound = !c.Alerts.Sound }, nil, nil},
	{M("Alerts"), M("When an agent finishes"), M("Alert when a live agent goes from working to idle (marked ◆ done until you look at it)."),
		func(c *config.Config) string { return boolS(c.Alerts.OnDone) },
		func(c *config.Config, d int) { c.Alerts.OnDone = !c.Alerts.OnDone }, nil, nil},
	{M("Alerts"), M("When it needs you"), M("Alert when an agent waits for a permission or an answer (needs a workspace with agent detection)."),
		func(c *config.Config) string { return boolS(c.Alerts.OnBlocked) },
		func(c *config.Config, d int) { c.Alerts.OnBlocked = !c.Alerts.OnBlocked }, nil, nil},
	{M("Alerts"), M("When a subagent ends"), M("Alert for every finished subagent (chatty with big fan-outs)."),
		func(c *config.Config) string { return boolS(c.Alerts.OnSubagent) },
		func(c *config.Config, d int) { c.Alerts.OnSubagent = !c.Alerts.OnSubagent }, nil, nil},
	{M("Alerts"), M("Popups"), M("In-app toasts in the top-right corner."),
		func(c *config.Config) string { return boolS(c.Alerts.Toast) },
		func(c *config.Config, d int) { c.Alerts.Toast = !c.Alerts.Toast }, nil, nil},
	{M("Alerts"), M("Desktop notifications"), M("notify-send (Linux) / osascript (macOS)."),
		func(c *config.Config) string { return boolS(c.Alerts.Desktop) },
		func(c *config.Config, d int) { c.Alerts.Desktop = !c.Alerts.Desktop }, nil, nil},
	{M("Alerts"), M("Done sound"), M("Sound file for finished agents (empty = system 'complete'). Enter to edit, T to test."),
		func(c *config.Config) string { return c.Alerts.SoundDone }, nil,
		func(c *config.Config, v string) error { c.Alerts.SoundDone = v; return nil }, nil},
	{M("Alerts"), M("Needs-you sound"), M("Sound file for blocked agents (empty = system 'dialog-warning')."),
		func(c *config.Config) string { return c.Alerts.SoundBlocked }, nil,
		func(c *config.Config, v string) error { c.Alerts.SoundBlocked = v; return nil }, nil},
	{M("Explorer"), M("Hide bot sessions"), M("Hide headless/automated sessions (sdk runs without a title, anything under ~/.cache)."),
		func(c *config.Config) string { return boolS(c.HideBots) },
		func(c *config.Config, d int) { c.HideBots = !c.HideBots }, nil, nil},
	{M("Explorer"), M("Max sessions"), M("Cap on how many sessions the explorer lists (newest first). 0 = no cap."),
		func(c *config.Config) string { return strconv.Itoa(c.MaxSessions) },
		func(c *config.Config, d int) { c.MaxSessions = max(0, c.MaxSessions+d*250) }, nil, nil},
	{M("Explorer"), M("Hide paths"), M("Comma-separated substrings; sessions whose folder contains any of them are hidden."),
		func(c *config.Config) string { return csv(c.HidePaths) }, nil,
		func(c *config.Config, v string) error { c.HidePaths = splitCSV(v); return nil }, nil},
	{M("Explorer"), M("Extra repos"), M("Comma-separated repo paths to show in Worktrees even if no session ran there."),
		func(c *config.Config) string { return csv(c.ExtraRepos) }, nil,
		func(c *config.Config, v string) error { c.ExtraRepos = splitCSV(v); return nil }, nil},
}

// customizeRow is the Settings row that opens Customize.
const customizeRow = "Customize"

type settingsTab struct {
	cur     cursor
	editing *textinput.Model
	off     int // lines scrolled off the top
}

func (t *settingsTab) key(m *Model, k tea.KeyMsg) tea.Cmd {
	n := len(settingsList)
	it := settingsList[t.cur.idx]
	if it.open != nil && (k.String() == "enter" || k.String() == " " || k.String() == "right" || k.String() == "l") {
		return it.open(m)
	}
	change := func(d int) tea.Cmd {
		if it.cycle == nil {
			return nil
		}
		it.cycle(&m.cfg, d)
		return m.saveConfig()
	}
	switch k.String() {
	case "j", "down":
		t.cur.move(1, n, 100)
	case "k", "up":
		t.cur.move(-1, n, 100)
	case "g", "home":
		t.cur.idx = 0
	case "G", "end":
		t.cur.idx = n - 1
	case "right", "l", " ":
		return change(1)
	case "left", "h":
		return change(-1)
	case "t":
		settingsList[0].cycle(&m.cfg, 1)
		return m.saveConfig()
	case "enter":
		if t.cur.idx == 0 { // Theme row opens the picker
			m.openThemes()
			return nil
		}
		if it.set != nil {
			in := textinput.New()
			in.Prompt = "› "
			in.SetValue(it.get(&m.cfg))
			in.CursorEnd()
			in.Focus()
			t.editing = &in
			return textinput.Blink
		}
		return change(1)
	case "e":
		return m.editConfig()
	}
	return nil
}

func (t *settingsTab) editKey(m *Model, k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		t.editing = nil
		return *m, nil
	case "enter":
		it := settingsList[t.cur.idx]
		if err := it.set(&m.cfg, strings.TrimSpace(t.editing.Value())); err != nil {
			m.setStatus(err.Error(), true)
		}
		t.editing = nil
		return *m, m.saveConfig()
	}
	in, cmd := t.editing.Update(k)
	t.editing = &in
	return *m, cmd
}

func (m *Model) saveConfig() tea.Cmd {
	m.applyConfig()
	if err := config.Save(m.cfg); err != nil {
		m.setStatus(T("save failed: ")+err.Error(), true)
		return nil
	}
	m.setStatus(T("saved")+" · "+tilde(config.Path()), false)
	return tea.Batch(m.scanCmd(), m.sourcesCmd())
}

func (t *settingsTab) view(m *Model, w, h int) string {
	s := m.st
	inner := w - 4
	var ls []string
	var rows []int // the setting on each line, -1 for headers and gaps
	sel := 0
	last := ""
	for i, it := range settingsList {
		if it.group != last {
			if last != "" {
				ls, rows = append(ls, ""), append(rows, -1)
			}
			ls, rows = append(ls, s.Accent.Render("▍"+T(it.group))), append(rows, -1)
			last = it.group
		}
		val := it.get(&m.cfg)
		if val == "" {
			val = s.Muted.Render("—")
		}
		var v string
		switch {
		case t.editing != nil && i == t.cur.idx:
			t.editing.Width = inner - 29
			v = t.editing.View()
		case it.cycle != nil:
			v = s.Muted.Render("‹ ") + s.Pink.Render(val) + s.Muted.Render(" ›")
		default:
			v = s.Text.Render(val) + s.Muted.Render("  ✎")
		}
		line := "  " + s.Text.Render(fit(T(it.name), 23)) + " " + v
		if i == t.cur.idx {
			line = s.Sel(s.RowSel, fit(line, inner))
			sel = len(ls)
		}
		ls, rows = append(ls, line), append(rows, i)
	}
	// the help of the selected one and the file stay below; the list scrolls
	foot := []string{""}
	for _, l := range wrap(T(settingsList[t.cur.idx].help), inner-2, 3) {
		foot = append(foot, s.Muted.Render("  "+l))
	}
	foot = append(foot, s.Muted.Render(trunc("  "+Tf("file: %s  ·  e opens it in $EDITOR", tilde(config.Path())), inner)))
	room := max(3, h-2-len(foot))
	t.off = max(0, min(t.off, sel-1, len(ls)-room))
	if sel >= t.off+room {
		t.off = sel - room + 1
	}
	end := min(len(ls), t.off+room)
	m.vs.listY0, m.vs.listRows = bodyY+1, rows[t.off:end]
	ls = append(ls[t.off:end], foot...)
	return s.panel(T("Settings"), strings.Join(ls, "\n"), w, h, true)
}

// preview renders a palette swatch and a fake agent card in the current theme.
func (t *settingsTab) preview(m *Model, w, h int) string {
	s := m.st
	p := s.P
	inner := w - 4
	sw := func(name, hex string) string {
		return lipgloss.NewStyle().Background(lipgloss.Color(hex)).Render("    ") + " " + s.Muted.Render(fit(name, 9)+hex)
	}
	var ls []string
	ls = append(ls, s.Bold.Render(m.cfg.Theme), "")
	pairs := [][2]string{{"bg", p.Bg}, {"surface", p.Surface}, {"overlay", p.Overlay}, {"text", p.Text}, {"muted", p.Muted},
		{"accent", p.Accent}, {"accent2", p.Accent2}, {"green", p.Green}, {"yellow", p.Yellow}, {"red", p.Red},
		{"blue", p.Blue}, {"peach", p.Peach}, {"teal", p.Teal}}
	if inner < 48 { // too narrow for labels: one strip of chips
		var chips []string
		for _, pr := range pairs {
			chips = append(chips, lipgloss.NewStyle().Background(lipgloss.Color(pr[1])).Render("  "))
		}
		pairs = nil
		ls = append(ls, trunc(strings.Join(chips, " "), inner))
	}
	for i := 0; i < len(pairs); i += 2 {
		l := sw(pairs[i][0], pairs[i][1])
		if i+1 < len(pairs) {
			l = fit(l, 24) + "  " + sw(pairs[i+1][0], pairs[i+1][1])
		}
		ls = append(ls, l)
	}
	ls = append(ls, "", s.Accent.Render(T("Preview")), "")
	demo := m.agentLines(liveRow{}, inner)
	demo[0] = m.stateGlyph("working") + " " + s.Bold.Render(T("Refactor checkout flow")) + "   " + m.stateLabel("working")
	demo[1] = "  " + s.Peach.Render("acme-api") + s.Dim.Render(" · ") + s.Teal.Render("⎇ PAY-412") + s.Dim.Render(" · ") + s.Muted.Render("opus-5-5")
	ls = append(ls, s.Sel(s.RowSel, fit(demo[0], inner)), s.Sel(s.RowSel, fit(demo[1], inner)))
	ls = append(ls, s.Dim.Render("  ├─ ")+m.stateGlyph("running")+" "+s.Muted.Render("Explore")+" "+s.Text.Render(T("Map payment adapters")))
	ls = append(ls, s.Dim.Render("  └─ ")+m.stateGlyph("finished")+" "+s.Muted.Render("Plan")+" "+s.Text.Render(T("Design the migration")))
	ls = append(ls, "", m.stateGlyph("blocked")+" "+s.Bold.Render("Liquibase changeset")+"   "+m.stateLabel("blocked"))
	ls = append(ls, "", s.Accent.Render("❯ ")+s.Bold.Render(T("can you resume the PAY-412 work?")), s.Teal.Render("● ")+s.Text.Render(T("Sure — picking up from the refunds PR.")),
		"  "+s.Muted.Render("⚙ Bash")+" "+s.Muted.Render("git status"))
	return s.panel(T("Theme preview"), strings.Join(ls, "\n"), w, h, false)
}

func firstOf(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
