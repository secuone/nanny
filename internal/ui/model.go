package ui

import (
	"fmt"
	"github.com/secuone/nanny/internal/classroom"
	"github.com/secuone/nanny/internal/i18n"
	"os"
	"reflect"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/secuone/nanny/internal/claude"
	"github.com/secuone/nanny/internal/config"
	"github.com/secuone/nanny/internal/gitwt"
	"github.com/secuone/nanny/internal/sources"
	"github.com/secuone/nanny/internal/term"
	"github.com/secuone/nanny/internal/ws"
)

type tab int

const (
	tabLive tab = iota
	tabSessions
	tabGit
	tabTasks
	tabPrompts
	tabSkills
	tabRadar
	tabSettings
	tabJira // after Settings: their keys are letters (E, I), the numbers are taken
	tabInsights
)

var tabNames = []string{M("Live"), M("Sessions"), M("Git & PRs"), M("Tasks"), M("Prompts"), M("Skills"), M("Monitor"), M("Settings"), M("Jira"), M("Insights")}

// ---------------------------------------------------------------- messages

type tickMsg time.Time
type frameMsg time.Time
type sessionsMsg []*claude.Session
type firstScanMsg []*claude.Session
type sourcesMsg []sources.Item
type reposMsg []gitwt.Repo
type subsMsg struct {
	path string
	subs []*claude.Subagent
}
type transcriptMsg struct {
	path    string
	entries []claude.Entry
}
type statusMsg struct {
	text string
	err  bool
}
type execDoneMsg struct{ err error }

// ---------------------------------------------------------------- model

type Model struct {
	cfg    config.Config
	st     Styles
	w, h   int
	tab    tab
	frame  int
	focusR bool // detail pane focused (scrolls)

	scanner  *claude.Scanner
	sessions []*claude.Session
	byID     map[string]*claude.Session
	items    []sources.Item
	repos    []gitwt.Repo
	loading  map[string]bool

	// cached on-demand detail
	class       *classroom.Server // the 3D classroom page, once opened
	subs        map[string][]*claude.Subagent
	transcripts map[string][]claude.Entry
	usage       map[string]claude.Usage // older sessions the scan doesn't tally
	prevBusy    map[string]bool

	live     liveTab
	sess     sessTab
	wt       wtTab
	radar    radarTab
	skills   skillsTab
	settings settingsTab
	gitv     *gitView // the views of features: pointers, the model is copied
	jira     *jiraView
	ins      *insightsView

	detailOff int
	offs      map[string]*int // list scroll offsets; a map so they survive the value-receiver View
	sourcesAt time.Time
	srcSig    string // the Monitor's sources as last collected, to see them come back the same
	srcSame   int    // how many times in a row they did
	// refreshing on change (watch.go)
	cw        *claude.Watcher // the watch on ~/.claude, nil without one
	scanning  bool            // a scan is on its way
	scanAt    time.Time       // when the last one started
	scanSoon  bool            // one is waiting for scanGap
	fullAt    time.Time       // the last full scan
	pend      map[string]bool // paths changed since the last scan
	pendAll   bool            // scan everything next
	trackDue  bool            // look at the panes' processes on the next tick
	trackAt   time.Time       // when they were looked at
	status    string
	statusErr bool
	statusAt  time.Time
	help      bool
	helpOff   int
	modal     *modal
	pal       *palette
	themes    *themePicker
	look      *lookPass // icons, borders and text weights picked in the config
	sidebar   bool
	wtOpen    map[*ws.Space]bool // worktrees opened under a space; folded by default
	spaceWT   spaceWTMsg         // each space's git worktrees, by its folder
	spaceWTAt time.Time
	gitState  gitStateMsg // git state of where spaces and agents are
	gitAt     time.Time
	prs       prsMsg // open PRs by repo and branch (gh)
	prsAt     time.Time
	scope     string // selected project in the dashboards ("" = all)
	projects  []space
	// workspaces: user spaces with terminal tabs; cur = -1 is the home dashboards
	spaces     []*ws.Space
	cur        int
	dash       bool // in a space: the views (m.tab, scoped to it) instead of its terminals
	prefix     bool
	paneCh     chan struct{}
	scopeSpace *ws.Space // dashboards scoped to a space's folders
	mgr        *manager
	cust       *customize // what is on and off (=)
	view       *viewer
	disc       *discovery
	fp         *folderPicker // asking for a folder (new space, set the folder of one)
	vs         *viewState    // render-time state that must survive value-receiver View
	alerts     *alertState
	showInbox  bool
	inbox      inbox
	answer     *answerBox // answering an agent's permission prompt
	task       *newTask   // the new-task form
	rev        *review    // full-screen changes of a checkout
	lib        *promptLib // full-screen prompt library

	tq      *taskQueue   // the task queue and its full-screen view
	menu    *ctxMenu     // right-click menu
	welcome *welcome     // the first-run guide
	setup   *setupWizard // connecting Jira and GitHub, over the screen
}

type viewState struct {
	sideRows  []sideRow
	sideMax   int // how far the sidebar's agents scroll
	sideSpMax int // how far its spaces scroll
	sideSplit int // screen row where its agents half starts
	sideOff   int // agents scrolled off the top of the sidebar
	sideSpOff int // spaces scrolled off the top of the sidebar
	// what the sidebar last scrolled into view, so the wheel doesn't fight it
	sideFollowSpace int
	sideFollowAgent string
	sideCurKey      string // the agent last walked to
	spaceLo         int    // first space chip drawn in the top bar
	spaceHi         int    // last one
	area            rect
	rects           []rect
	paneLines       map[*term.Pane]paneLines // what each pane of the open tab drew last
	bySession       map[string]paneRef
	byPid           map[int]paneRef
	dirty           bool
	spaceHits       []hit
	tabHits         []hit
	termHits        []hit // the terminal tabs strip of a space (row 2)
	secHits         []hit // the Monitor's sections (row 3)
	keyHits         []keyHit
	// mouse targets recorded while rendering
	listY0     int   // screen row of the dashboard list's first line
	listRows   []int // row index shown on each list line
	modalY     int   // screen row of the open modal's first list row
	modalStart int   // index of that row
	modalN     int   // rows shown
	clickAt    time.Time
	clickKey   string
	// hover, drag and the right-click menu
	hover      hoverAt
	drag       dragFrom
	menuHits   []menuHit
	menuBox    rect
	reuse      bool        // nothing on screen changed: View returns frame again
	noop       bool        // the last update changed nothing (a refresh that came back the same)
	pending    bool        // an update changed something since frame was drawn
	wait       bool        // a background refresh changed it: drawn at the flush
	flushing   bool        // a flush is on its way
	frame      string      // the last frame drawn
	cursor     *tea.Cursor // the real cursor of that frame: the focused pane's
	paneCursor *tea.Cursor // what viewPanes drew it at, before popups cover it
	raw        string      // the last frame with its spinners as placeholders
	anim       bool        // only the spinners moved since it was drawn
	press      pressAt     // where the left button went down, while it is down
	sel        selection   // text being selected with the mouse
	frameOn    bool        // a frame is on its way (none while nothing moves)
	drawnMin   int64       // the minute the last frame was drawn in (clock, "3m ago")
	monWas     bool        // the Monitor was on screen after the last update
	widths     widths      // how wide the lines of the last frames were
	gen        uint64      // counts the updates but pane output: the chrome is drawn again after each
	chrome     chromeCache // the header, footer and sidebar of the last frame

	swap    *strings.Replacer // puts the spinner and blink in a frame, for the glyphs in swapFor
	swapFor [2]string
}

type modal struct {
	title   string
	body    string
	input   *textinput.Model
	confirm func(m *Model, value string) tea.Cmd
	// optional second answer for yes/no questions
	altKey, altLabel string
	alt              func(m *Model) tea.Cmd
	keys             map[string]func(m *Model) tea.Cmd // more answers, by key: a choice that changes the question
	fill             *promptFill                       // a prompt for an agent: alt+1-9, ctrl+r
	quit             bool                              // the quit question: the quit key again answers yes
}

func New(cfg config.Config) Model {
	i18n.Set(cfg.Language)
	setPrices(cfg)
	m := Model{
		cfg:         cfg,
		st:          NewStyles(cfg.Resolve()),
		look:        newLook(cfg.Look),
		scanner:     claude.NewScanner(),
		byID:        map[string]*claude.Session{},
		loading:     map[string]bool{"sessions": true},
		subs:        map[string][]*claude.Subagent{},
		transcripts: map[string][]claude.Entry{},
		usage:       map[string]claude.Usage{},
		prevBusy:    map[string]bool{},
		offs:        map[string]*int{},
		sidebar:     true,
		vs:          &viewState{},
		alerts:      newAlertState(),
		cur:         -1,
		gitv:        newGitView(),
		jira:        newJiraView(),
		ins:         &insightsView{},

		tq:     loadQueue(),
		paneCh: make(chan struct{}, 1),
	}
	m.sess.filter = newInput()
	m.sess.filter.Prompt = "  "
	m.sess.filter.Placeholder = T("type to filter — title, repo, branch, prompt, id…")
	m.sess.hideBots = cfg.HideBots
	if (os.Getenv("TMUX") != "" || os.Getenv("ZELLIJ") != "" || os.Getenv("STY") != "") && m.prefixLabel() == "ctrl+b" {
		m.setStatus(T("inside another multiplexer: ctrl+b may be taken — change the prefix in Settings (8)"), false)
	}
	if firstRun() {
		m.welcome = &welcome{}
	}
	m.tab = m.homeTab()
	m.skills.filter = newInput()
	m.skills.filter.Prompt = "  "
	m.skills.filter.Placeholder = T("filter skills, commands, agents…")
	if st, err := ws.Load(); err == nil {
		m.spaces = ws.Restore(st, cfg.Restore, strings.Fields(cfg.ClaudeArgs), m.spawn, m.attach)
		if st.Active >= 0 && st.Active < len(m.spaces) && len(m.spaces[st.Active].Tabs) > 0 {
			m.cur = st.Active
		}
	}
	return m
}

func (m Model) Init() tea.Cmd {
	m.vs.frameOn = true
	return tea.Batch(m.firstScanCmd(), m.sourcesCmd(), tick(m.cfg), frame(), m.waitPane(), watchCmd())
}

func tick(c config.Config) tea.Cmd {
	return tea.Tick(time.Duration(c.RefreshSecs)*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func frame() tea.Cmd { return frameIn(180 * time.Millisecond) }

func frameIn(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return frameMsg(t) })
}

// animating reports whether something on screen moves by itself (spinners,
// a blinking alert, toasts); if not, frames slow down to one a
// second and the screen is drawn far less often.
func (m Model) animating() bool {
	if len(m.alerts.toasts) > 0 || m.vs.hover.kind != "" {
		return true
	}
	if m.featBusy() {
		return true
	}
	for _, r := range m.live.allRows {
		if st := r.state(); st == "working" || st == "blocked" || r.run > 0 {
			return true
		}
	}
	return false
}

// firstScanCmd lists the sessions without their token usage, so the first
// screen doesn't wait for whole transcripts to be read; a full scan follows.
func (m Model) firstScanCmd() tea.Cmd {
	sc := m.scanner
	return func() tea.Msg { return firstScanMsg(sc.ScanFast()) }
}

func (m Model) scanCmd() tea.Cmd {
	sc := m.scanner
	return func() tea.Msg { return sessionsMsg(sc.Scan()) }
}

func (m Model) sourcesCmd() tea.Cmd { return m.sourcesOnly("") }

// sourcesOnly collects one source ("" all that are on).
func (m Model) sourcesOnly(only string) tea.Cmd {
	var ws []sources.Watch
	for _, w := range m.cfg.Watch {
		ws = append(ws, sources.Watch{Name: w.Name, Group: w.Group, Command: w.Command, Log: w.Log, Cwd: w.Cwd, Open: w.Open})
	}
	// what is turned off isn't collected at all, nor anything with the Monitor off
	on := map[string]bool{}
	for _, sc := range monSecs {
		on[sc.src] = sc.src != "" && m.monOn(sc.src) && (only == "" || sc.src == only)
	}
	if only != "" {
		ws = nil
	}
	return func() tea.Msg { return sourcesMsg(sources.Collect(func(n string) bool { return on[n] }, ws)) }
}

func (m Model) reposCmd() tea.Cmd {
	seen := map[string]bool{}
	var paths []string
	for _, s := range m.sessions {
		if s.Bot() || s.Cwd == "" || seen[s.Cwd] {
			continue
		}
		seen[s.Cwd] = true
		paths = append(paths, s.Cwd)
	}
	for _, sp := range m.spaces {
		paths = append(paths, sp.Paths...)
	}
	paths = append(paths, m.cfg.ExtraRepos...)
	return func() tea.Msg { return reposMsg(gitwt.Discover(paths)) }
}

func subsCmd(path string) tea.Cmd {
	return func() tea.Msg { return subsMsg{path, claude.Subagents(path)} }
}

func transcriptCmd(path string, sub bool) tea.Cmd {
	return func() tea.Msg {
		if sub {
			return transcriptMsg{path, claude.SubTranscript(path, 60)}
		}
		return transcriptMsg{path, claude.Transcript(path, 60)}
	}
}

func (m Model) off(key string) *int {
	if p, ok := m.offs[key]; ok {
		return p
	}
	p := new(int)
	m.offs[key] = p
	return p
}

func (m *Model) applyConfig() {
	i18n.Set(m.cfg.Language)
	setPrices(m.cfg)
	m.st = NewStyles(m.cfg.Resolve())
	m.look = newLook(m.cfg.Look)
	m.sess.hideBots = m.cfg.HideBots
	if !m.viewOn(m.tab) { // the view on screen was turned off
		m.tab = m.homeTab()
	}
	m.items = m.monItems(m.items)
	m.radar.rebuild(m.items)
	if !m.radar.anyOn(m) || !m.secOn(m.radar.sec) {
		m.radar.sec = secAll
	}
	m.rebuild()
}

func (m *Model) setStatus(s string, err bool) {
	m.status, m.statusErr, m.statusAt = s, err, time.Now()
}

// ---------------------------------------------------------------- update

// coalesce draws what background refreshes change a moment later, once for
// all of them: a tick brings back sessions, subagents, usage and git state
// one after another, and each would draw the whole screen. Keys, the mouse
// and pane output still draw at once.
var coalesce = true

const flushIn = 50 * time.Millisecond

type flushMsg struct{}

func background(msg tea.Msg) bool {
	switch msg.(type) {
	case tickMsg, sessionsMsg, sourcesMsg, reposMsg, subsMsg, transcriptMsg, usageMsg, gitStateMsg, prsMsg, spaceWTMsg:
		return true
	}
	_, ok := msg.(bgMsg)
	return ok
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.vs.noop, m.vs.wait = false, false
	if _, ok := msg.(paneOutMsg); !ok {
		m.vs.gen++
	}
	mm, cmd := m.update(msg)
	if m, ok := mm.(Model); ok {
		// frames only while something moves; anything that starts moving starts them
		if !m.vs.frameOn && m.animating() {
			m.vs.frameOn = true
			cmd = tea.Batch(cmd, frame())
		}
		// the Monitor coming on screen (a space's terminals hidden…) gets fresh sources
		if shown := m.monitorShown(); shown != m.vs.monWas {
			m.vs.monWas = shown
			if shown && time.Since(m.sourcesAt) > 2*time.Second {
				m.sourcesAt = time.Now()
				cmd = tea.Batch(cmd, m.sourcesCmd())
				mm = m
			}
		}
	}
	if !m.vs.noop { // vs is shared across copies of the model
		m.vs.pending = true
		m.vs.wait = coalesce && background(msg)
		if m.vs.wait && !m.vs.flushing {
			m.vs.flushing = true
			cmd = tea.Batch(cmd, tea.Tick(flushIn, func(time.Time) tea.Msg { return flushMsg{} }))
		}
	}
	if m, ok := mm.(Model); ok && m.spaceScope() != m.tq.only {
		// another space (or home): the views follow it
		m.tq.only = m.spaceScope()
		m.refreshFocus()
		if sp := m.tq.only; sp != nil && sp.Broad() {
			m.setStatus(Tf("%s is all your home: cd into a repo in it to narrow its views", spaceLabel(sp)), false)
		}
		m.rebuild()
		m.skills.rebuild(&m)
		return m, tea.Batch(cmd, m.refreshDetail())
	}
	return mm, cmd
}

func (m Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.layoutPanes()
		return m, nil

	case paneOutMsg:
		return m, m.waitPane()

	case viewerMsg:
		if m.view != nil && m.view.path == msg.path {
			m.view.entries = msg.entries
		}
		return m, nil

	case skillsMsg:
		m.skills.all = msg
		m.skills.loaded = true
		m.skills.rebuild(&m)
		return m, nil

	case folderPlacesMsg:
		if m.fp != nil {
			m.fp.busy = false
			for _, p := range msg {
				m.fp.known = append(m.fp.known, p.Path)
			}
			m.fp.known = uniqueDirs(m.fp.known)
			if m.fp.create == "" && m.fp.cur < 0 {
				m.fp.refresh()
			}
		}
		return m, nil

	case discoverMsg:
		if m.disc != nil {
			m.disc.busy = false
			m.disc.places = msg
		}
		return m, nil

	case flushMsg: // draws what background refreshes left pending
		m.vs.flushing = false
		if !m.vs.pending {
			m.vs.noop = true
		}
		return m, nil

	case frameMsg:
		m.frame++
		toasts := len(m.alerts.toasts)
		if m.frame%8 == 0 {
			m.pruneToasts()
		}
		if len(m.alerts.toasts) == toasts { // only the spinners moved: View swaps them in
			m.vs.noop, m.vs.anim = true, true
		}
		if !m.animating() {
			m.vs.frameOn = false
			return m, nil
		}
		return m, frame()

	case tickMsg:
		// with a watch, a Claude starting or ending in a pane writes under
		// ~/.claude: the pane processes are looked at then, and now and then
		busy := m.animating()
		if m.cw == nil || busy || m.trackDue || time.Since(m.trackAt) > 10*time.Second {
			m.trackAgents()
			m.trackDue, m.trackAt = false, time.Now()
		}
		m.ringBells()
		if m.vs.dirty {
			m.saveSpaces()
		}
		refocused := m.refreshFocus()
		if refocused { // a space's panes moved: its views follow them
			m.rebuild()
			m.skills.rebuild(&m)
		}
		// with a watch, a change is read when it happens: a tick only does the
		// time-based work (subagents go quiet, a running clock) while something
		// moves, and a full scan now and then
		if m.cw == nil || time.Since(m.fullAt) > fullScanEvery {
			m.pendAll, m.fullAt = true, time.Now()
		}
		cmds := []tea.Cmd{tick(m.cfg), m.kick(), m.dispatch()}
		switch {
		case m.cw == nil:
			cmds = append(cmds, m.refreshDetail(), m.classSubs())
			if m.view != nil {
				cmds = append(cmds, viewerCmd(m.view.path, m.view.sub))
			}
		case busy: // what changes with time alone: a subagent that went quiet is done
			cmds = append(cmds, m.runningSubs())
		}
		every := gitStateEvery
		if m.cw != nil && !busy { // nobody at work: branches change far less
			every = gitStateIdle
		}
		if time.Since(m.gitAt) > every || time.Since(m.gitAt) > 2*time.Second && m.spaceMoved() {
			cmds = append(cmds, m.gitStateCmd())
		}
		if !m.prsAt.IsZero() && time.Since(m.prsAt) > prsEvery {
			cmds = append(cmds, m.prsCmd())
		}
		if m.sidebarOn() && m.cfg.On("sidebar.worktrees") && (time.Since(m.spaceWTAt) > 20*time.Second || (time.Since(m.spaceWTAt) > 2*time.Second && m.spaceMoved())) {
			cmds = append(cmds, m.spaceWTCmd())
		}
		cmds = append(cmds, m.sourcesDue(), m.featDue(false))
		if m.cw != nil && !busy && !refocused && time.Now().Unix()/60 == m.vs.drawnMin {
			m.vs.noop = true // nothing on screen moved: not even drawn again
		}
		return m, tea.Batch(cmds...)

	case watchMsg:
		m.vs.noop = true
		if msg.err != nil {
			debugf("watch: %v; scanning every %ds instead", msg.err, m.cfg.RefreshSecs)
			return m, nil
		}
		debugf("watch: %d folders", msg.w.Watches())
		m.cw = msg.w
		return m, waitClaude(m.cw)

	case claudeMsg:
		m.vs.noop = true // what it starts redraws when it is back
		return m, m.claudeChanged()

	case scanSoonMsg:
		m.vs.noop = true
		m.scanSoon = false
		return m, m.kick()

	case firstScanMsg:
		mm, cmd := m.update(sessionsMsg(msg))
		m = mm.(Model)
		m.pendAll, m.fullAt = true, time.Now()
		return m, tea.Batch(cmd, m.kick())

	case sessionsMsg:
		m.scanning = false
		again := m.kick() // what changed while it was on its way
		if !m.loading["sessions"] && sameSessions(m.sessions, msg) {
			m.vs.noop = true
			return m, again
		}
		m.loading["sessions"] = false
		m.notifyFinished(msg)
		m.sessions = msg
		m.byID = make(map[string]*claude.Session, len(msg))
		for _, s := range msg {
			m.byID[s.ID] = s
		}
		m.rebuild()
		if m.repos == nil && !m.loading["repos"] {
			m.loading["repos"] = true
			return m, tea.Batch(again, m.reposCmd(), m.refreshDetail())
		}
		return m, tea.Batch(again, m.refreshDetail())

	case sourcesMsg:
		if sig := fmt.Sprint([]sources.Item(msg)); sig == m.srcSig {
			m.srcSame++
			m.vs.noop = true
			return m, nil
		} else {
			m.srcSig, m.srcSame = sig, 0
		}
		m.items = m.monItems(append([]sources.Item{m.overview()}, msg...))
		m.radar.rebuild(m.items)
		return m, nil

	case reposMsg:
		m.loading["repos"] = false
		m.repos = msg
		m.wt.rebuild(&m)
		return m, nil

	// refreshes come every tick for every agent; most change nothing, and then
	// neither the views are rebuilt nor the screen drawn again
	case subsMsg:
		if old, ok := m.subs[msg.path]; ok && sameSubs(old, msg.subs) {
			m.vs.noop = true
			return m, nil
		}
		m.subs[msg.path] = msg.subs
		m.rebuild()
		return m, nil

	case transcriptMsg:
		if old, ok := m.transcripts[msg.path]; ok && sameEntries(old, msg.entries) {
			m.vs.noop = true
			return m, nil
		}
		m.transcripts[msg.path] = msg.entries
		return m, nil

	case usageMsg:
		m.usage[msg.path] = msg.u
		return m, nil

	case statusMsg:
		m.setStatus(msg.text, msg.err)
		return m, nil

	case deepMsg:
		if msg.q == m.sess.deepQ {
			m.sess.deepBusy = false
			m.sess.deep = msg.hits
			m.sess.cur.idx = 0
			m.sess.rebuild(&m)
			m.setStatus(Tn(len(msg.hits), "“%[2]s” found in %[1]d session", "“%[2]s” found in %[1]d sessions", msg.q), false)
		}
		return m, m.refreshDetail()

	case wtChangedMsg:
		m.setStatus(msg.text, false)
		m.loading["repos"] = true
		return m, tea.Batch(m.reposCmd(), m.spaceWTCmd(), m.gitStateCmd())

	case gitStateMsg:
		first := m.gitState == nil
		if fmt.Sprint(msg) == fmt.Sprint(m.gitState) {
			m.vs.noop = true
			return m, nil
		}
		m.gitState = msg
		if first || time.Since(m.prsAt) > prsEvery { // the PRs of the repos just seen
			return m, m.prsCmd()
		}
		return m, nil

	case prsMsg:
		if m.prs == nil {
			m.prs = prsMsg{}
		}
		for root, prs := range msg {
			m.prs[root] = prs
		}
		return m, nil

	case spaceWTMsg:
		m.spaceWT = msg
		return m, nil

	case configEditedMsg:
		m.cfg = config.Load()
		m.applyConfig()
		m.setStatus(T("config reloaded"), false)
		if errs := config.LoadThemes(); len(errs) > 0 {
			m.setStatus(T("theme file skipped: ")+errs[0].Error(), true)
		}
		return m, nil

	case execDoneMsg:
		if msg.err != nil {
			m.setStatus(T("command exited: ")+msg.err.Error(), true)
		}
		return m, tea.Batch(m.scanCmd())

	case tea.MouseMsg:
		return m.mouse(toMouseEv(msg))

	case tea.PasteMsg:
		return m.paste(msg.Content)

	case tea.KeyPressMsg:
		return m.key(msg)

	case taskReadyMsg:
		return m, m.taskReady(msg)
	}
	if cmd, ok := m.featUpdate(msg); ok {
		return m, cmd
	}
	if cmd, ok := m.setupUpdate(msg); ok {
		return m, cmd
	}
	if cmd, ok := m.reviewUpdate(msg); ok {
		return m, cmd
	}
	if cmd, ok := m.promptsUpdate(msg); ok {
		return m, cmd
	}
	if cmd, ok := m.queueUpdate(msg); ok {
		return m, cmd
	}
	return m, nil
}

// notifyFinished flashes a status when a live session goes busy → idle.
func (m *Model) notifyFinished(next []*claude.Session) {
	for _, s := range next {
		if s.Live == nil {
			continue
		}
		busy := s.Live.Status == "busy"
		if was, ok := m.prevBusy[s.ID]; ok && was && !busy {
			m.setStatus("✓ "+Tf("%s finished its turn", trunc(s.Title, 50)), false)
		}
		m.prevBusy[s.ID] = busy
	}
}

func (m *Model) rebuild() {
	m.live.rebuild(m)
	m.detectAlerts()
	m.live.rebuild(m) // re-apply unseen flags set by detectAlerts
	m.buildSpaces()
	m.sess.rebuild(m)
	m.wt.rebuild(m)
	m.pushClassroom()
}

// refreshDetail loads whatever the detail pane of the current selection needs.
func (m *Model) refreshDetail() tea.Cmd { return m.refreshDetailIf(nil) }

// refreshDetailIf is refreshDetail for the paths in (all with nil) only.
func (m *Model) refreshDetailIf(in func(path string) bool) tea.Cmd {
	ok := func(p string) bool { return in == nil || in(p) }
	var cmds []tea.Cmd
	switch m.tab {
	case tabLive:
		for _, r := range m.live.rows {
			if r.sess != nil && r.agent() && ok(r.sess.Path) {
				cmds = append(cmds, subsCmd(r.sess.Path))
			}
		}
		if r := m.live.sel(); r != nil {
			if r.sub != nil && ok(r.sub.Path) {
				cmds = append(cmds, transcriptCmd(r.sub.Path, true))
			} else if r.sub == nil && r.sess != nil && ok(r.sess.Path) {
				cmds = append(cmds, transcriptCmd(r.sess.Path, false))
			}
		}
	case tabSessions:
		if s := m.sess.sel(); s != nil && ok(s.Path) {
			cmds = append(cmds, transcriptCmd(s.Path, false))
			if s.Usage == nil {
				cmds = append(cmds, usageCmd(s.Path))
			}
			if s.Subagents > 0 {
				cmds = append(cmds, subsCmd(s.Path))
			}
		}
	}
	return tea.Batch(cmds...)
}

// sameSessions is whether a scan came back with nothing that shows changed.
func sameSessions(a, b []*claude.Session) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, y := a[i], b[i]
		if x.ID != y.ID || x.Path != y.Path || x.Size != y.Size || !x.Modified.Equal(y.Modified) ||
			x.Title != y.Title || x.Name != y.Name || x.Cwd != y.Cwd || x.Branch != y.Branch ||
			(x.Live == nil) != (y.Live == nil) || x.Live != nil && *x.Live != *y.Live ||
			(x.Usage == nil) != (y.Usage == nil) || x.Usage != y.Usage && !reflect.DeepEqual(*x.Usage, *y.Usage) {
			return false
		}
	}
	return true
}

func (m Model) bodyHeight() int { return max(3, m.h-bodyY-1) }

// popupOpen is true while a popup, menu or full-screen overlay has the keys.
func (m Model) popupOpen() bool {
	return m.menu != nil || m.modal != nil || m.task != nil || m.answer != nil || m.pal != nil || m.themes != nil ||
		m.showInbox || m.help || m.welcome != nil || m.setup != nil || m.view != nil || m.rev != nil ||
		m.disc != nil || m.mgr != nil || m.cust != nil || m.fp != nil
}

// paste goes to the terminal that has the keys, bracketed, or to the text box that has them; anywhere else it is dropped.
func (m Model) paste(s string) (tea.Model, tea.Cmd) {
	if m.inSpace() && !m.dash && !m.popupOpen() && !m.prefix {
		if t := m.space().Tab(); t != nil && t.Pane() != nil {
			if p := t.Pane(); !p.Exited() && !p.ReadOnly {
				p.Write([]byte("\x1b[200~" + s + "\x1b[201~"))
			}
		}
		return m, nil
	}
	if m.popupOpen() || m.typing() {
		return m.key(tea.KeyPressMsg{Code: tea.KeyExtended, Text: s})
	}
	return m, nil
}

func (m Model) key(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.vs.hover = hoverAt{} // typing hides the tooltip until the mouse moves
	m.vs.sel = selection{} // and the selection
	if qk := m.cfg.QuitKeyName(); qk != "" && k.String() == qk && !m.holdsText() {
		if md := m.modal; md != nil && md.quit {
			m.modal = nil
			return m, md.confirm(&m, "")
		}
		m.menu, m.pal, m.help, m.prefix = nil, nil, false, false
		return m.confirmQuit(false)
	}
	if m.menu != nil {
		return m.menuKey(k)
	}
	if m.modal != nil {
		return m.modalKey(k)
	}
	if m.task != nil {
		return m.taskKey(k)
	}
	if m.answer != nil {
		return m.answerKey(k)
	}
	if m.pal != nil {
		return m.paletteKey(k)
	}
	if m.themes != nil {
		return m.themeKey(k)
	}
	if m.showInbox {
		return m.inboxKey(k)
	}
	if m.help {
		return m.helpKey(k)
	}
	if m.welcome != nil {
		return m.welcomeKey(k)
	}
	if m.setup != nil {
		return m.setupKey(k)
	}
	if m.view != nil {
		return m.viewerKey(k)
	}
	if m.rev != nil {
		return m.reviewKey(k)
	}
	if m.libShown() && m.lib.ed != nil {
		return m.editorKey(k)
	}
	if m.fp != nil {
		return m.folderKey(k)
	}
	if m.disc != nil {
		return m.discoveryKey(k)
	}
	if m.mgr != nil {
		return m.managerKey(k)
	}
	if m.cust != nil {
		return m.customizeKey(k)
	}
	if k.String() == "=" && (!m.inSpace() || m.dash || m.prefix) && !m.typing() {
		m.prefix = false
		m.openCustomize()
		return m, nil
	}
	if m.prefix && k.String() == "T" { // prefix T: theme picker, home or space
		m.prefix = false
		m.openThemes()
		return m, nil
	}
	if mm, cmd, ok := m.navKey(k.String()); ok {
		return mm, cmd
	}
	if m.inSpace() && !m.dash {
		return m.paneKey(k)
	}
	if m.prefix {
		m.prefix = false
		return m.prefixCmd(k.String())
	}
	if k.String() == m.prefixLabel() {
		m.prefix = true
		return m, nil
	}
	if mm, cmd, ok := m.viewSwitchKey(k); ok {
		return mm, cmd
	}
	switch m.tab {
	case tabPrompts:
		if m.lib == nil {
			return m, m.openPrompts()
		}
		return m.promptsKey(k)
	case tabTasks:
		return m.queueKey(k)
	}
	// the filter box swallows printable keys
	if m.tab == tabSessions && m.sess.filter.Focused() {
		return m.sess.filterKey(&m, k)
	}
	if m.tab == tabSkills && m.skills.filter.Focused() {
		return m.skills.filterKey(&m, k)
	}
	if m.tab == tabSettings && m.settings.editing != nil {
		return m.settings.editKey(&m, k)
	}
	if f := m.feat(m.tab); f != nil && f.typing() { // its text box swallows printable keys
		return m.featKey(f, k)
	}
	switch k.String() {
	case "ctrl+c", "q":
		return m.confirmQuit(false)
	case "W":
		m.openManager()
		return m, nil
	case "D":
		return m, m.discoverCmd()
	case "alt+1", "alt+2", "alt+3", "alt+4", "alt+5", "alt+6", "alt+7", "alt+8", "alt+9":
		if i := int(k.String()[4] - '1'); i < len(m.spaces) {
			m.cur = i
			m.layoutPanes()
		}
		return m, nil
	case "alt+right", "alt+]":
		m.cycleSpace(1)
		return m, nil
	case "alt+left", "alt+[":
		m.cycleSpace(-1)
		return m, nil
	case "?":
		m.help = true
		return m, nil
	case "m":
		m.appMenu(1, bodyY)
		return m, nil
	case "B":
		m.sidebar = !m.sidebar
		return m, nil
	case "0": // all spaces: the first space's terminals, as "0 terminal" in the bar
		if !m.inSpace() && len(m.spaces) > 0 {
			m.cur = 0
			m.layoutPanes()
			return m, m.refreshDetail()
		}
	case "M":
		return m, m.cycleProfile(1)
	case "T":
		m.openThemes()
		return m, nil
	case "S":
		return m.toggleSound()
	case "A":
		m.showInbox = true
		m.inbox.cur = 0
		return m, nil
	case "]":
		m.cycleScope(1)
		return m, m.refreshDetail()
	case "[":
		m.cycleScope(-1)
		return m, m.refreshDetail()
	case "\\":
		m.scope, m.scopeSpace = "", nil
		m.rebuild()
		m.setStatus(T("scope: all projects"), false)
		return m, m.refreshDetail()
	case "ctrl+k", ":":
		return m, m.openPalette("")
	case "N":
		return m, m.newSpacePrompt()
	case "P":
		return m.switchTab(tabPrompts)
	case "u":
		return m.switchTab(tabTasks)
	case "!":
		cmd := m.nextAttention()
		return m, cmd
	case "ctrl+r", "R":
		return m, m.rescan()
	case "right", "l":
		if m.tab == tabRadar && k.String() == "right" && !m.focusR {
			m.radar.setSec(&m, 1)
			return m, nil
		}
		if f := m.feat(m.tab); f != nil && k.String() == "right" && !m.focusR && len(f.secs(&m)) > 1 {
			return m, m.stepSec(f, 1)
		}
		if m.tab != tabSettings && !m.noDetail() {
			m.focusR = true
			return m, nil
		}
	case "left", "h":
		if m.tab == tabRadar && k.String() == "left" && !m.focusR {
			m.radar.setSec(&m, -1)
			return m, nil
		}
		if f := m.feat(m.tab); f != nil && k.String() == "left" && !m.focusR && len(f.secs(&m)) > 1 {
			return m, m.stepSec(f, -1)
		}
		if m.tab != tabSettings {
			if m.focusR {
				m.focusR = false
				return m, nil
			}
		}
	case "esc":
		if m.inSpace() && !m.focusR && len(m.live.marks) == 0 {
			m.back()
			return m, nil
		}
		if m.tab == tabLive && m.live.only != "" && !m.focusR {
			m.live.only = ""
			m.live.rebuild(&m)
			return m, m.refreshDetail()
		}
		m.focusR = false
		m.detailOff = 0
		m.live.marks = nil
		return m, nil
	}
	if m.focusR && m.tab != tabSettings {
		switch k.String() {
		case "j", "down":
			m.detailOff++
		case "k", "up":
			m.detailOff = max(0, m.detailOff-1)
		case "pgdown", "ctrl+d":
			m.detailOff += m.bodyHeight() / 2
		case "pgup", "ctrl+u":
			m.detailOff = max(0, m.detailOff-m.bodyHeight()/2)
		case "g", "home":
			m.detailOff = 0
		default:
			goto tabKeys
		}
		return m, nil
	}
tabKeys:
	var cmd tea.Cmd
	before := m.selKey()
	switch m.tab {
	case tabLive:
		cmd = m.live.key(&m, k)
	case tabSessions:
		cmd = m.sess.key(&m, k)
	case tabRadar:
		cmd = m.radar.key(&m, k)
	case tabSkills:
		cmd = m.skills.key(&m, k)
	case tabSettings:
		cmd = m.settings.key(&m, k)
	default:
		if f := m.feat(m.tab); f != nil {
			cmd = f.key(&m, k)
		}
	}
	if m.selKey() != before {
		m.detailOff = 0
		return m, tea.Batch(cmd, m.refreshDetail())
	}
	return m, cmd
}

// selKey identifies the current selection so detail scroll resets when it changes.
func (m Model) selKey() string {
	k := fmt.Sprintf("%d/%d/%d/%d/%d", m.tab, m.live.cur.idx, m.sess.cur.idx, m.radar.cur.idx, m.skills.cur.idx)
	if f := m.feat(m.tab); f != nil {
		k += "/" + f.selKey(&m)
	}
	return k
}

func (m Model) switchTab(t tab) (tea.Model, tea.Cmd) {
	if !m.viewOn(t) {
		m.setStatus(Tf("%s is turned off · = customizes", T(tabNames[t])), false)
		return m, nil
	}
	m.show(t)
	var cmds []tea.Cmd
	if t == tabPrompts && m.lib == nil {
		cmds = append(cmds, m.openPrompts())
	} else if t == tabPrompts {
		m.lib.reload()
	}
	if f := m.feat(t); f != nil {
		cmds = append(cmds, f.due(&m, true))
	}
	if t == tabRadar {
		cmds = append(cmds, m.sourcesCmd())
	}
	if t == tabSkills {
		cmds = append(cmds, m.skillsCmd())
	}
	cmds = append(cmds, m.refreshDetail())
	return m, tea.Batch(cmds...)
}

func (m Model) mouse(e mouseEv) (tea.Model, tea.Cmd) {
	if mm, cmd, ok := m.selectMouse(e); ok {
		return mm, cmd
	}
	if m.menu != nil {
		return m.menuMouse(e)
	}
	switch e.Action {
	case mouseMotion:
		return m.hoverMove(e)
	case mouseRelease:
		return m.dropTab(e)
	}
	m.vs.hover = hoverAt{}
	if m.welcome != nil || m.setup != nil {
		return m.welcomeMouse(e)
	}
	switch e.Button {
	case tea.MouseRight:
		return m.rightClick(e)
	case tea.MouseMiddle:
		return m.middleClose(e)
	}
	if e.Button == tea.MouseLeft {
		if mm, cmd, ok := m.keyClick(e.X, e.Y); ok {
			return mm, cmd
		}
	}
	if d := map[tea.MouseButton]int{tea.MouseWheelDown: 1, tea.MouseWheelUp: -1}[e.Button]; d != 0 && m.onProfileChip(e.X, e.Y) {
		return m, m.cycleProfile(d)
	}
	if mm, cmd, ok := m.overlayMouse(e); ok {
		return mm, cmd
	}
	leftW := m.sideW() + m.leftWidth()
	if e.Button == tea.MouseLeft && (e.Y == 0 || e.Y == viewY) {
		hits := m.vs.spaceHits
		if e.Y == viewY {
			hits = m.vs.tabHits
		}
		for _, h := range hits {
			if e.X < h.x0 || e.X >= h.x1 {
				continue
			}
			switch {
			case e.Y == 0 && h.idx == -2:
				return m, m.newSpacePrompt()
			case e.Y == 0 && h.idx == -3:
				m.appMenu(0, 1)
				return m, nil
			case e.Y == 0 && (h.idx == -4 || h.idx == -5):
				m.hiddenSpacesMenu(h.idx == -4, h.x0)
				return m, nil
			case e.Y == 0 && h.idx == m.cur && m.inSpace(): // the space you are in: its terminals
				m.back()
				return m, nil
			case e.Y == 0:
				m.vs.drag = dragFrom{"space", h.idx}
				m.cur = h.idx
				m.layoutPanes()
				return m, m.refreshDetail()
			case h.idx == tabTerm && !m.inSpace(): // from all spaces: the first space's terminals
				m.cur = 0
				m.layoutPanes()
				return m, m.refreshDetail()
			case h.idx == tabTerm:
				m.back()
				return m, nil
			}
			return m.switchTab(tab(h.idx))
		}
		return m, nil
	}
	if e.Button == tea.MouseLeft && e.Y == termY && m.inSpace() && !m.dash {
		sp := m.space()
		for _, h := range m.vs.termHits {
			if e.X < h.x0 || e.X >= h.x1 {
				continue
			}
			switch h.idx {
			case -2:
				return m, m.openTabIn(sp, "claude", append([]string{"claude"}, strings.Fields(m.cfg.ClaudeArgs)...))
			case -4:
				return m, m.openTabIn(sp, "shell", nil)
			}
			sp.Active = h.idx
			m.vs.drag = dragFrom{"term", h.idx}
			m.layoutPanes()
			if m.double(fmt.Sprintf("term/%d", h.idx)) {
				t := sp.Tabs[h.idx]
				m.rename(T("Rename tab"), t.Label, func(v string) { t.Label = v })
			}
			return m, nil
		}
		return m, nil
	}
	if m.inSpace() && !m.dash && e.X >= m.sideW() {
		t := m.space().Tab()
		if t == nil {
			return m, nil
		}
		for i, r := range m.vs.rects {
			if e.X >= r.x && e.X < r.x+r.w && e.Y >= r.y && e.Y < r.y+r.h {
				switch e.Button {
				case tea.MouseWheelUp:
					t.Panes[i].Scroll(3)
				case tea.MouseWheelDown:
					t.Panes[i].Scroll(-3)
				case tea.MouseLeft:
					t.Focus = i
				}
			}
		}
		return m, nil
	}
	if e.X < m.sideW() && e.Y >= bodyY && (e.Button == tea.MouseWheelUp || e.Button == tea.MouseWheelDown) {
		d := 3
		if e.Button == tea.MouseWheelUp {
			d = -3
		}
		if e.Y < m.vs.sideSplit { // each half scrolls on its own
			m.vs.sideSpOff = max(0, min(m.vs.sideSpOff+d, m.vs.sideSpMax))
		} else {
			m.vs.sideOff = max(0, min(m.vs.sideOff+d, m.vs.sideMax))
		}
		return m, nil
	}
	if e.X < m.sideW() && e.Y >= bodyY && e.Button == tea.MouseLeft {
		r, ok := m.sidebarHit(e.Y)
		switch {
		case !ok:
		case r.url != "":
			return m, m.openURL(r.url)
		case r.toggle != nil:
			if m.wtOpen == nil {
				m.wtOpen = map[*ws.Space]bool{}
			}
			m.wtOpen[r.toggle] = !m.wtOpen[r.toggle]
			return m, nil
		case r.wt != nil:
			m.worktreeMenu(m.spaces[r.space], *r.wt, e.X, e.Y+1)
			return m, nil
		case r.isSpace:
			m.cur = r.space
			m.layoutPanes()
			return m, m.refreshDetail()
		case r.scroll != 0:
			if e.Y < m.vs.sideSplit {
				m.vs.sideSpOff = max(0, min(m.vs.sideSpOff+r.scroll, m.vs.sideSpMax))
			} else {
				m.vs.sideOff = max(0, min(m.vs.sideOff+r.scroll, m.vs.sideMax))
			}
			return m, nil
		case r.sess != nil:
			return m, m.openAgent(r.sess, r.sub, r.local)
		}
		return m, nil
	}
	switch e.Button {
	case tea.MouseWheelUp, tea.MouseWheelDown:
		d := 1
		if e.Button == tea.MouseWheelUp {
			d = -1
		}
		if e.X >= leftW {
			m.detailOff = max(0, m.detailOff+d*3)
			return m, nil
		}
		key := keyPress("down")
		if d < 0 {
			key = keyPress("up")
		}
		m.focusR = false
		return m.key(key)
	case tea.MouseLeft:
		if m.hasSecs() && e.Y == secY { // the sections span both panes
			return m.listClick(e)
		}
		m.focusR = e.X >= leftW
		if !m.focusR && m.dashShown() {
			return m.listClick(e)
		}
	}
	return m, nil
}

func (m Model) modalKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	md := m.modal
	if md.fill != nil && md.input != nil && m.fillKey(md.fill, k.String(), md.input) {
		return m, nil
	}
	if md.alt != nil && md.input == nil && k.String() == md.altKey {
		m.modal = nil
		return m, md.alt(&m)
	}
	if f, ok := md.keys[k.String()]; ok && md.input == nil {
		return m, f(&m)
	}
	switch k.String() {
	case "esc", "ctrl+c":
		m.modal = nil
		return m, nil
	case "enter":
		val := ""
		if md.input != nil {
			val = strings.TrimSpace(md.input.Value())
		}
		m.modal = nil
		return m, md.confirm(&m, val)
	case "y", "Y":
		if md.input == nil {
			m.modal = nil
			return m, md.confirm(&m, k.String())
		}
	case "n", "N":
		if md.input == nil {
			m.modal = nil
			return m, nil
		}
	}
	if md.input != nil {
		in, cmd := md.input.Update(k)
		md.input = &in
		return m, cmd
	}
	return m, nil
}

func (m *Model) ask(title, body string, fn func(m *Model, v string) tea.Cmd) {
	m.modal = &modal{title: title, body: body, confirm: fn}
}

func (m *Model) prompt(title, body, placeholder string, fn func(m *Model, v string) tea.Cmd) {
	in := newInput()
	in.Placeholder = placeholder
	in.Prompt = "› "
	in.Focus()
	m.modal = &modal{title: title, body: body, input: &in, confirm: fn}
}

// ---------------------------------------------------------------- view

func (m Model) sideW() int {
	if m.sidebarOn() {
		return sidebarW
	}
	return 0
}

func (m Model) mainW() int { return m.w - m.sideW() }

func (m Model) leftWidth() int {
	mw := m.mainW()
	lw := mw * 52 / 100
	if m.tab == tabSettings {
		lw = mw * 50 / 100
	}
	return max(min(lw, 90), min(44, mw))
}

func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.Cursor = m.vs.cursor
	v.AltScreen = true
	v.MouseMode = tea.MouseModeAllMotion
	v.WindowTitle = "nanny"
	return v
}

func (m Model) render() string {
	if m.w == 0 {
		return ""
	}
	if m.vs.wait && m.vs.frame != "" {
		return m.vs.frame
	}
	if (m.vs.reuse || m.vs.noop && !m.vs.pending) && m.vs.frame != "" {
		if m.vs.anim {
			m.vs.frame = m.vs.sel.paint(m.animate(m.vs.raw))
		}
		m.vs.reuse, m.vs.noop, m.vs.anim = false, false, false
		return m.vs.frame
	}
	m.vs.reuse, m.vs.noop, m.vs.pending, m.vs.anim = false, false, false, false
	m.vs.paneCursor = nil
	m.vs.widths.roll()
	m.vs.drawnMin = time.Now().Unix() / 60
	bh := m.bodyHeight()
	cc := &m.vs.chrome
	key := m.chromeKey(bh)
	redraw := cc.key != key
	if redraw {
		*cc = chromeCache{key: key}
		cc.header = m.viewHeader() + "\n\n" + fit(m.viewTabBar(), m.w) + "\n" // blank rows around the views bar
		cc.footer = m.viewFooter()
	}
	lw := m.leftWidth()
	rw := m.mainW() - lw
	var left, right string
	m.vs.listRows = nil
	switch {
	case m.view != nil:
		left = m.viewViewer(m.mainW(), bh)
	case m.rev != nil:
		left = m.viewReview(m.mainW(), bh)
	case m.inSpace() && !m.dash:
		left = m.viewTermTabs(m.mainW()) + "\n" + m.viewPanes(m.mainW(), bh-1)
	case m.onTab(tabPrompts):
		if m.lib == nil {
			left = m.st.panel(T("Prompts"), "", m.mainW(), bh, true)
		} else {
			left = m.viewPrompts(m.mainW(), bh)
		}
	case m.onTab(tabTasks):
		left = m.viewQueue(m.mainW(), bh)
	case m.noDetail():
		left, _ = m.dashboard(m.mainW(), 0, bh)
	default:
		left, right = m.dashboard(lw, rw, bh)
	}
	body := m.joinTop(left, right)
	if m.sidebarOn() {
		if redraw {
			cc.side = m.viewSidebar(bh)
		}
		body = m.joinTop(cc.side, body)
	}
	out := m.joinLeft(cc.header, body, cc.footer)
	m.vs.modalN = 0
	var box string
	tipShown := false
	switch {
	case m.help:
		box = m.viewHelp()
	case m.pal != nil:
		box = m.viewPalette()
	case m.themes != nil:
		box = m.viewThemes()
	case m.showInbox:
		box = m.viewInbox()
	case m.modal != nil:
		box = m.viewModal()
	case m.libShown() && m.lib.ed != nil:
		box = m.viewPromptEditor()
	case m.task != nil:
		box = m.viewTask()
	case m.answer != nil:
		box = m.viewAnswer()
	case m.fp != nil:
		box = m.viewFolderPicker()
	case m.disc != nil:
		box = m.viewDiscovery()
	case m.mgr != nil:
		box = m.viewManager()
	case m.cust != nil:
		box = m.viewCustomize()
	case m.welcome != nil:
		box = m.viewWelcome()
	case m.setup != nil:
		box = m.viewSetup()
	}
	if box != "" {
		out, _ = harvestKeys(out) // what is under a popup can't be clicked
		out = m.overlay(out, box)
		// list rows start after the modal's border and top padding
		m.vs.modalY += max(0, (m.h-strings.Count(box, "\n")-1)/3) + 2
	}
	if t := m.viewToasts(); t != "" {
		out = m.overlayAt(out, t, m.w-lipgloss.Width(t)-1, 2)
	}
	if m.menu != nil {
		out, _ = harvestKeys(out)
		mb, x, y := m.viewMenu()
		out = m.overlayAt(out, mb, x, y)
	} else if box == "" && m.vs.hover.kind != "" {
		if tip, x, y := m.viewTooltip(); tip != "" {
			out = m.overlayAt(out, tip, x, y)
			tipShown = true
		}
	}
	out, m.vs.keyHits = harvestKeys(m.finish(out))
	if m.help || m.menu != nil {
		m.vs.keyHits = nil
	}
	out = m.look.apply(out)
	m.vs.raw = out
	out = m.vs.sel.paint(m.animate(out))
	m.vs.frame = out
	m.vs.cursor = m.vs.paneCursor
	if box != "" || m.menu != nil || m.help || m.view != nil || m.rev != nil || m.vs.sel.on || tipShown {
		m.vs.cursor = nil
	}
	return out
}

// featDashboard is a feature's view: its sections bar over both panes, when
// it has one, then the list and the detail.
func (m Model) featDashboard(lw, rw, bh int) (left, right string) {
	f := m.feat(m.tab)
	secs := f.secs(&m)
	if len(secs) == 0 {
		m.vs.secHits = m.vs.secHits[:0]
		left = f.view(&m, lw, bh)
		if rw > 0 {
			right = f.detail(&m, rw, bh)
		}
		return left, right
	}
	bar := " " + m.st.Muted.Render("←→ ") + m.sectionBar(secs, f.sec(), m.sideW()+4, lw+rw-5)
	if n := f.barNote(&m); n != "" {
		if gap := lw + rw - lipgloss.Width(bar) - lipgloss.Width(n) - 2; gap >= 1 {
			bar += strings.Repeat(" ", gap) + n
		}
	}
	left = f.view(&m, lw, bh-1)
	if rw > 0 {
		left = lipgloss.JoinHorizontal(lipgloss.Top, left, f.detail(&m, rw, bh-1))
	}
	return fit(bar, lw+rw) + "\n" + left, ""
}

func (m Model) dashboard(lw, rw, bh int) (left, right string) {
	switch m.tab {
	case tabLive:
		left = m.live.view(&m, lw, bh)
	case tabSessions:
		left = m.sess.view(&m, lw, bh)
	case tabGit, tabJira, tabInsights:
		return m.featDashboard(lw, rw, bh)
	case tabRadar: // the sections bar spans both panes
		bar := " " + m.radar.secBar(&m, m.sideW()+1, lw+rw-2)
		left = m.radar.view(&m, lw, bh-1)
		if rw > 0 {
			left = lipgloss.JoinHorizontal(lipgloss.Top, left, m.radar.detail(&m, rw, bh-1))
		}
		return fit(bar, lw+rw) + "\n" + left, ""
	case tabSkills:
		left, right = m.skills.view(&m, lw, bh), m.skills.detail(&m, rw, bh)
	case tabSettings:
		left, right = m.settings.view(&m, lw, bh), m.settings.preview(&m, rw, bh)
	}
	if rw > 0 {
		switch m.tab {
		case tabLive:
			right = m.live.detail(&m, rw, bh)
		case tabSessions:
			right = m.sess.detail(&m, rw, bh)
		}
	}
	return left, right
}

// detailScroll applies the right-pane scroll offset to a list of lines.
func (m Model) detailScroll(ls []string, h int) string {
	inner := bodyRows(h)
	off := min(m.detailOff, max(0, len(ls)-inner))
	if off > 0 {
		ls = ls[off:]
	}
	return strings.Join(ls, "\n")
}

// detailTail keeps the header (ls[:headN]) and the newest part of the body when
// everything doesn't fit, so live activity is always visible without scrolling.
func (m Model) detailTail(ls []string, headN, h int) string {
	room := bodyRows(h)
	if m.detailOff > 0 || len(ls) <= room || headN > room-6 {
		return m.detailScroll(ls, h)
	}
	body := ls[headN:]
	keep := room - headN - 1
	return strings.Join(append(append(ls[:headN:headN], m.st.Dim.Render("  "+T("⋮ older above — l then k/j to scroll"))), body[len(body)-keep:]...), "\n")
}

func (m Model) viewHeader() string {
	s := m.st
	left := m.viewSpaceTabs(m.w - 45) // the right side takes about that
	// fleet summary: what needs you first, then what's moving
	busy, idle, blocked, done, subsRunning := 0, 0, 0, 0, 0
	for _, r := range m.live.allRows {
		if r.sub != nil {
			if r.sub.Running {
				subsRunning++
			}
			continue
		}
		switch r.state() {
		case "working":
			busy++
		case "blocked":
			blocked++
		case "done":
			done++
		default:
			idle++
		}
	}
	var parts []string
	if m.basic() {
		return m.basicHeader(left, busy, idle, blocked)
	}
	if blocked > 0 {
		st := s.BadgeColor(s.P.Red).Bold(true)
		parts = append(parts, clickable("@only:blocked", st.Render(blinkGlyph+" "+Tn(blocked, "%d NEEDS YOU", "%d NEED YOU"))))
	}
	counts := m.cfg.On("header.counts")
	if done > 0 && counts {
		parts = append(parts, clickable("@only:done", s.Blue.Render(fmt.Sprintf("◆ %d", done))+s.Muted.Render(" "+T("done"))))
	}
	// zero counts stay quiet so the eye goes to what is happening
	count := func(st lipgloss.Style, glyph string, n int, what string) string {
		if n == 0 {
			return s.Dim.Render(fmt.Sprintf("%s %d %s", glyph, n, what))
		}
		return st.Render(fmt.Sprintf("%s %d", glyph, n)) + s.Muted.Render(" "+what)
	}
	work := "⠿"
	if busy > 0 {
		work = spinGlyph
	}
	// below full, a count of nothing isn't shown at all
	for _, c := range []struct {
		only, glyph, what string
		st                lipgloss.Style
		n                 int
	}{{"working", work, T("working"), s.Yellow, busy}, {"subs", "↳", T("subs"), s.Teal, subsRunning}, {"idle", "○", T("idle"), s.Green, idle}} {
		if counts && (c.n > 0 || m.full()) {
			parts = append(parts, clickable("@only:"+c.only, count(c.st, c.glyph, c.n, c.what)))
		}
	}
	snd := s.Green.Render("♪")
	if !m.cfg.Alerts.Sound {
		snd = s.Muted.Render("✕♪")
	}
	for _, f := range m.feats() { // what a view wants you to see from anywhere: reviews waiting…
		if b := f.badge(&m); b != "" {
			parts = append(parts, b)
		}
	}
	var tail []string
	if m.cfg.On("header.sound") {
		tail = append(tail, snd)
	}
	clock := ""
	if m.cfg.On("header.clock") {
		clock = s.Muted.Render(now().Format("15:04"))
		tail = append(tail, clock)
	}
	right := strings.Join(append(parts, tail...), "  ") + " "
	// queued tasks first, then today's spend, each only when it fits
	if qb := m.queueBadge(); qb != "" && m.cfg.On("header.queue") && m.w-lipgloss.Width(left)-lipgloss.Width(right)-lipgloss.Width(qb) > 2 {
		right = qb + "  " + right
	}
	if c := m.todaySpend(); c > 0 && m.cfg.On("header.cost") {
		spend := s.Text.Render(money(c)) + s.Muted.Render(" "+T("today"))
		n := len(right) - len(strings.Join(tail, "  ")) - 1 // before the sound and the clock
		withSpend := right[:n] + spend + "  " + right[n:]
		if m.w-lipgloss.Width(left)-lipgloss.Width(withSpend) >= 1 {
			right = withSpend
		}
	}
	if lipgloss.Width(left)+lipgloss.Width(right) >= m.w { // more on the right than guessed
		left = m.viewSpaceTabs(m.w - lipgloss.Width(right) - 1)
	}
	return m.headerLine(left, right, clock)
}

// footerKeys are the 5-7 most useful keys of the current view; ? lists the rest.
func (m Model) footerKeys() [][2]string {
	if m.view != nil {
		return [][2]string{{"esc", T("back")}, {"j/k", T("scroll")}, {"G", T("follow")}, {"t", T("tools")}, {"⏎", T("go to agent")}, {"g", T("top")}, {"?", T("help")}}
	}
	if m.rev != nil {
		return [][2]string{{"esc", T("back")}, {"j/k", T("file")}, {"l", T("diff")}, {"t", T("uncommitted/branch")}, {"c", T("ask to commit")},
			{"w", T("open PR")}, {"y", T("copy branch")}, {"?", T("help")}}
	}
	if m.inSpace() && !m.dash || m.prefix {
		keys := [][2]string{{"c", "claude"}, {"s", T("shell")}, {"v -", T("split")}, {"n p", T("tab")}, {"z", T("zoom")}, {"x", T("close")},
			{"i", T("views")}, {"w", T("spaces")}, {"0", T("all spaces")}, {"?", T("help")}}
		if !m.sidebar {
			keys = append(keys[:len(keys)-1], [2]string{"B", T("show sidebar")}, keys[len(keys)-1])
		}
		return keys
	}
	if m.libShown() {
		if m.lib.ed != nil {
			return [][2]string{{"ctrl+s", T("save")}, {"tab", T("field")}, {"ctrl+e", "$EDITOR"}, {"esc", T("cancel")}}
		}
		return [][2]string{{"⏎", T("insert")}, {"/", T("filter")}, {"s", T("save")}, {"n", T("new")}, {"e", T("edit")}, {"d", T("delete")},
			{"y", T("copy")}, {"p", T("pin")}, {"q", T("queue")}, {"esc", T("back")}, {"?", T("help")}}
	}
	if m.onTab(tabTasks) {
		return [][2]string{{"⏎", T("run now · go to")}, {"n", T("add")}, {"e", T("edit")}, {"d", T("delete")}, {"J/K", T("reorder")},
			{"a", T("auto")}, {"r", T("retry")}, {"esc", T("back")}, {"?", T("help")}}
	}
	var keys [][2]string
	switch m.tab {
	case tabLive:
		keys = [][2]string{{"⏎", T("go to · answer")}, {"v", T("read")}, {"p", T("prompt")}, {"n", T("new task")}, {"space", T("pick")}, {"!", T("needs you")}}
		if n := len(m.live.marks); n > 0 {
			keys = [][2]string{{"p", Tf("send to %d", n)}, {"space", T("pick")}, {"esc", T("unpick")}}
		}
	case tabSessions:
		keys = [][2]string{{"⏎", T("resume")}, {"/", T("filter")}, {"F", T("search text")}, {"v", T("read")}, {"f", T("fork")}}
	case tabRadar:
		keys = [][2]string{{"⏎", T("open")}, {"o", T("shell")}, {"L", T("follow log")}, {"y", T("copy")}}
		if it := m.radar.sel(); it != nil && it.Kind != "" { // a container or a Tilt resource
			keys = [][2]string{{"⏎", T("actions")}, {"L", T("follow log")}, {"y", T("copy")}}
		}
	case tabSkills:
		keys = [][2]string{{"⏎", T("edit")}, {"/", T("filter")}, {"space", T("on/off")}, {"n", T("new")}, {"t", T("type")}}
	case tabSettings:
		keys = [][2]string{{"←→", T("change")}, {"⏎", T("edit")}, {"e", "$EDITOR"}, {"t", T("next theme")}, {"T", T("themes")}}
	default:
		if f := m.feat(m.tab); f != nil {
			keys = f.footer(&m)
		}
	}
	// short footers unless everything is on: the rest is in ? and on screen
	if n := map[bool]int{true: 3, false: 4}[m.basic()]; !m.full() && len(keys) > n {
		keys = keys[:n]
	}
	if m.inSpace() {
		keys = append(keys, [2]string{"0", T("terminal")})
	}
	if !m.sidebar { // hidden with B: say how to get it back
		keys = append(keys, [2]string{"B", T("show sidebar")})
	}
	return append(keys, [2]string{"^k", T("jump")}, [2]string{"?", T("help")})
}

func (m Model) viewFooter() string {
	s := m.st
	lead := " "
	if m.inSpace() && !m.dash || m.prefix {
		px := m.prefixLabel()
		lead = s.Muted.Render(" "+px+" +") + " "
		if m.prefix {
			lead = " " + s.BadgeColor(s.P.Accent).Render(px) + " "
		}
	}
	note := ""
	if m.inSpace() && !m.dash && m.view == nil {
		note = "alt+1-9 " + T("space") + " · " + T("wheel scroll")
	}
	if qk := m.cfg.QuitKeyName(); qk != "" {
		if note != "" {
			note += " · "
		}
		note += keyShort(qk) + " " + T("quit")
	}
	right := ""
	if note != "" {
		right = s.Muted.Render(note + " ")
	}
	if m.status != "" && time.Since(m.statusAt) < 8*time.Second {
		st := s.Status
		if m.statusErr {
			st = s.StatusErr
		}
		right = st.Render(trunc(m.status, m.w/2)) + " "
	}
	room := m.w - lipgloss.Width(lead) - lipgloss.Width(right) - 1
	keys := s.hintsFit(m.footerKeys(), max(0, room))
	return fit(lead+keys+" "+right, m.w)
}

// overlay composites box centered over base, dimming nothing but keeping context visible.
func (m Model) overlay(base string, box string) string {
	bw := lipgloss.Width(box)
	n := strings.Count(base, "\n") + 1
	return m.overlayAt(base, box, max(0, (m.w-bw)/2), max(0, (n-strings.Count(box, "\n")-1)/3))
}

func (m Model) overlayAt(base, box string, x, y int) string {
	bl := strings.Split(base, "\n")
	ol := strings.Split(box, "\n")
	bw := lipgloss.Width(box)
	for i, l := range ol {
		row := y + i
		if row >= len(bl) {
			break
		}
		under := bl[row]
		left := ansi.Truncate(under, x, "")
		if w := lipgloss.Width(left); w < x {
			left += strings.Repeat(" ", x-w)
		}
		right := ansi.TruncateLeft(under, x+bw, "")
		if x <= 3 && x+bw >= m.w-3 { // nearly full width: no stray borders in the margins
			left, right = strings.Repeat(" ", x), strings.Repeat(" ", max(0, m.w-x-bw))
		}
		bl[row] = left + m.look.fenceOff() + "\x1b[0m" + l + "\x1b[0m" + right
	}
	return strings.Join(bl, "\n")
}

func (m Model) viewModal() string {
	s := m.st
	md := m.modal
	w := min(70, m.w-4)
	var b strings.Builder
	b.WriteString(s.Accent.Render(md.title) + "\n\n")
	for _, l := range wrap(md.body, w-6, 12) {
		b.WriteString(s.Text.Render(l) + "\n")
	}
	if md.input != nil {
		md.input.SetWidth(w - 10)
		hs := [][2]string{{"⏎", T("confirm")}}
		if md.fill != nil {
			hs = append(hs, [2]string{"alt+1-9", T("saved")}, [2]string{"^r", T("most used")})
		}
		b.WriteString("\n" + md.input.View() + "\n\n" + s.hintsFit(append(hs, [2]string{"esc", T("cancel")}), w-6))
	} else {
		hs := [][2]string{{"y", T("yes")}}
		if md.alt != nil {
			hs = append(hs, [2]string{md.altKey, strings.TrimSpace(md.altLabel)})
		}
		b.WriteString("\n" + s.hints(append(hs, [2]string{"n/esc", T("no")})))
	}
	return s.Modal.Width(w + 2).Render(b.String())
}

// sameSubs reports whether two subagent trees show the same.
func sameSubs(a, b []*claude.Subagent) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, y := a[i], b[i]
		if x.ID != y.ID || x.Running != y.Running || x.Finished != y.Finished || x.LastTool != y.LastTool ||
			x.LastText != y.LastText || !x.Modified.Equal(y.Modified) || !sameSubs(x.Children, y.Children) {
			return false
		}
	}
	return true
}

// sameEntries reports whether two transcript tails are the same.
func sameEntries(a, b []claude.Entry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// rescan reads every session, source and repo again from scratch.
func (m *Model) rescan() tea.Cmd {
	m.setStatus(T("refreshing…"), false)
	m.loading["repos"] = true
	m.scanner = claude.NewScanner()
	return tea.Batch(m.scanCmd(), m.sourcesCmd(), m.reposCmd(), m.featDue(true))
}

// keyShort writes a key the way footers do: ctrl+q is ^q.
func keyShort(k string) string {
	if c, ok := strings.CutPrefix(k, "ctrl+"); ok && len(c) == 1 {
		return "^" + c
	}
	return k
}

// holdsText is true while something on screen is taking typed text, so the quit key is left to it.
func (m Model) holdsText() bool {
	if m.task != nil || m.setup != nil || ((m.dash || !m.inSpace()) && m.typing()) {
		return true
	}
	return m.fp != nil || m.modal != nil && m.modal.input != nil
}
