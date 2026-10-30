package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/pprof"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"text/tabwriter"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/secuone/nanny/internal/claude"
	"github.com/secuone/nanny/internal/config"
	"github.com/secuone/nanny/internal/demo"
	"github.com/secuone/nanny/internal/gitwt"
	"github.com/secuone/nanny/internal/term"
	"github.com/secuone/nanny/internal/ui"
)

var version = "dev"

const usage = `nanny — a terminal workspace that keeps an eye on your coding agents

Usage:
  nanny                      open the TUI
  nanny ls [query] [-n N] [-a]   list sessions (newest first, fuzzy filter)
  nanny where <query>        print the folder of the best matching session
  nanny resume <query> [-f]  cd into the session folder and run claude --resume (-f forks)
  nanny search <text>        full-text search inside every conversation
  nanny live                 list running agents
  nanny ps                   list panes running in the background
  nanny kill-server          stop every background pane
  nanny new [-w branch] [-C dir] <prompt>  start claude with a first prompt (-w: in a new worktree)
  nanny shell-init [zsh|bash|fish]  print shell helpers (sp = cd to a session folder)
  nanny insights [--week 2026-W44] [--json]  what Claude cost and did, from the transcripts
  nanny demo                 try it on a generated fake $HOME (no real data touched)
  nanny config               print the config file path
  nanny version

A <query> can be a session id (or prefix), a title, a repo, a branch or a worktree name.
`

func main() {
	if len(os.Args) < 2 {
		runTUI()
		return
	}
	cmd, args := os.Args[1], os.Args[2:]
	switch cmd {
	case "ls", "list":
		cmdList(args)
	case "where", "cd":
		cmdWhere(args)
	case "resume", "r":
		cmdResume(args)
	case "search", "grep":
		cmdSearch(args)
	case "live":
		cmdLive()
	case "ps":
		cmdPs()
	case "kill-server":
		fmt.Printf("stopped %d pane(s)\n", term.KillAll())
	case "__hold":
		term.HoldMain(args)
	case "new":
		cmdNew(args)
	case "shell-init":
		cmdShellInit(args)
	case "insights":
		cmdInsights(args)
	case "demo":
		cmdDemo()
	case "config":
		fmt.Println(config.Path())
	case "version", "--version", "-v":
		fmt.Println("nanny", version)
	case "help", "--help", "-h":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
}

func runTUI() {
	cfg := config.Load()
	if f := os.Getenv("NANNY_CPUPROFILE"); f != "" { // debugging: where the CPU goes
		if out, err := os.Create(f); err == nil {
			// NANNY_CPUPROFILE_AFTER=10s leaves the start-up out
			wait, _ := time.ParseDuration(os.Getenv("NANNY_CPUPROFILE_AFTER"))
			go func() { time.Sleep(wait); _ = pprof.StartCPUProfile(out) }()
			defer pprof.StopCPUProfile()
		}
	}
	fps := 30 // the screen at most this often: half the wake-ups of 60, still smooth (NANNY_FPS changes it)
	if n, err := strconv.Atoi(os.Getenv("NANNY_FPS")); err == nil {
		fps = n
	}
	opts := []tea.ProgramOption{tea.WithFPS(fps)}
	if f := os.Getenv("NANNY_OUTSTATS"); f != "" { // debugging: how much the screen writes
		if out, err := os.Create(f); err == nil {
			defer out.Close()
			c := &countingTTY{File: os.Stdout}
			go c.log(out)
			opts = append(opts, tea.WithOutput(c))
		}
	}
	p := tea.NewProgram(ui.New(cfg), opts...)
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "nanny:", err)
		os.Exit(1)
	}
}

// countingTTY is stdout that counts what the screen writes.
type countingTTY struct {
	*os.File
	bytes, writes atomic.Int64
}

func (c *countingTTY) Write(b []byte) (int, error) {
	c.bytes.Add(int64(len(b)))
	c.writes.Add(1)
	return c.File.Write(b)
}

func (c *countingTTY) log(out io.Writer) {
	const every = 5 * time.Second
	var lastB, lastW int64
	for range time.Tick(every) {
		b, w := c.bytes.Load(), c.writes.Load()
		db, dw := b-lastB, w-lastW
		lastB, lastW = b, w
		per := 0.0
		if dw > 0 {
			per = float64(db) / float64(dw)
		}
		fmt.Fprintf(out, "bytes=%d writes=%d KiB/s=%.2f bytes/write=%.0f\n", db, dw, float64(db)/1024/every.Seconds(), per)
	}
}

func die(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "nanny: "+f+"\n", a...)
	os.Exit(1)
}

func sessions(all bool) []*claude.Session {
	ss := claude.NewScanner().Scan()
	if all {
		return ss
	}
	var out []*claude.Session
	for _, s := range ss {
		if !s.Bot() || s.Live != nil {
			out = append(out, s)
		}
	}
	return out
}

func tilde(p string) string {
	h, _ := os.UserHomeDir()
	if strings.HasPrefix(p, h) {
		return "~" + p[len(h):]
	}
	return p
}

func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func cut(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

func cmdList(args []string) {
	fs := flag.NewFlagSet("ls", flag.ExitOnError)
	n := fs.Int("n", 25, "max rows")
	all := fs.Bool("a", false, "include bot/headless sessions")
	_ = fs.Parse(reorder(args, "-n"))
	ss := claude.Match(sessions(*all), strings.Join(fs.Args(), " "))
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "AGE\t \tID\tPROJECT\tBRANCH/WT\tTITLE")
	for i, s := range ss {
		if i >= *n {
			break
		}
		live := " "
		if s.Live != nil {
			live = "●"
		}
		br := s.Branch
		if s.Worktree != "" {
			br = "⎇ " + s.Worktree
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", ago(s.Modified), live, s.ID[:8], cut(filepath.Base(s.Cwd), 24), cut(br, 22), cut(s.Title, 60))
	}
	tw.Flush()
}

// reorder moves flags before positional args so `ls foo -n 5` works;
// valued lists the flags that take a value.
func reorder(args []string, valued ...string) []string {
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "-") {
			flags = append(flags, args[i])
			if slices.Contains(valued, args[i]) && i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
		} else {
			pos = append(pos, args[i])
		}
	}
	return append(flags, pos...)
}

func best(args []string) *claude.Session {
	q := strings.Join(args, " ")
	if q == "" {
		die("need a query (id, title, repo, branch…)")
	}
	ms := claude.Match(sessions(true), q)
	if len(ms) == 0 {
		die("no session matches %q", q)
	}
	return ms[0]
}

func cmdWhere(args []string) {
	fmt.Println(best(args).ResumeCwd())
}

func cmdResume(args []string) {
	fork := false
	var rest []string
	for _, a := range args {
		if a == "-f" || a == "--fork" {
			fork = true
		} else {
			rest = append(rest, a)
		}
	}
	s := best(rest)
	cfg := config.Load()
	argv := []string{"claude", "--resume", s.ID}
	if fork {
		argv = append(argv, "--fork-session")
	}
	argv = append(argv, strings.Fields(cfg.ClaudeArgs)...)
	bin, err := exec.LookPath("claude")
	if err != nil {
		die("claude not found in PATH")
	}
	fmt.Fprintf(os.Stderr, "→ %s\n  %s\n", cut(s.Title, 70), tilde(s.ResumeCwd()))
	if err := os.Chdir(s.ResumeCwd()); err != nil {
		die("%v", err)
	}
	if err := syscall.Exec(bin, argv, os.Environ()); err != nil {
		die("%v", err)
	}
}

func cmdSearch(args []string) {
	q := strings.Join(args, " ")
	if q == "" {
		die("need some text to search for")
	}
	ss := sessions(true)
	hits := claude.Search(ss, q, 2)
	n := 0
	for _, s := range ss {
		hs, ok := hits[s.ID]
		if !ok {
			continue
		}
		n++
		fmt.Printf("\x1b[1m%s\x1b[0m  \x1b[2m%s · %s · %s\x1b[0m\n", cut(s.Title, 70), s.ID[:8], tilde(s.Cwd), ago(s.Modified))
		for _, h := range hs {
			fmt.Printf("   %s %s\n", map[string]string{"user": "❯", "assistant": "●"}[h.Role], cut(h.Snippet, 140))
		}
	}
	fmt.Fprintf(os.Stderr, "%d sessions match %q\n", n, q)
}

func cmdLive() {
	ss := claude.NewScanner().Scan()
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "STATUS\tPID\tUP\tID\tPROJECT\tSUBAGENTS\tTITLE")
	for _, s := range ss {
		if s.Live == nil {
			continue
		}
		run := 0
		subs := claude.Flatten(claude.Subagents(s.Path))
		for _, f := range subs {
			if f.Running {
				run++
			}
		}
		fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\t%d/%d\t%s\n", s.Live.Status, s.Live.Pid, ago(s.Live.Started()), s.ID[:8],
			cut(filepath.Base(s.Cwd), 24), run, len(subs), cut(s.Title, 60))
	}
	tw.Flush()
}

// cmdPs lists panes running in the background (one holder each).
func cmdPs() {
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "PID\tUP\tATTACHED\tFOLDER\tCOMMAND\tSOCKET")
	for _, h := range term.Holders() {
		att := "-"
		if h.Attached {
			att = "yes"
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\n", h.Pid, ago(h.Since), att, tilde(h.Cwd), cut(strings.Join(h.Argv, " "), 50), tilde(h.Socket))
	}
	tw.Flush()
}

// cmdNew starts claude with a first prompt, optionally in a fresh worktree.
func cmdNew(args []string) {
	fs := flag.NewFlagSet("new", flag.ExitOnError)
	branch := fs.String("w", "", "create a git worktree on this new branch and start there")
	dir := fs.String("C", ".", "folder to start in")
	_ = fs.Parse(reorder(args, "-w", "-C"))
	cwd, err := filepath.Abs(*dir)
	if err != nil {
		die("%v", err)
	}
	if *branch != "" {
		root := gitwt.RepoRoot(cwd)
		if root == "" {
			die("%s is not in a git repository", tilde(cwd))
		}
		path, err := gitwt.Add(root, *branch, gitwt.Head(cwd))
		if err != nil {
			die("%v", err)
		}
		fmt.Fprintf(os.Stderr, "⎇ %s  %s\n", *branch, tilde(path))
		cwd = path
	}
	argv := append([]string{"claude"}, strings.Fields(config.Load().ClaudeArgs)...)
	if p := strings.TrimSpace(strings.Join(fs.Args(), " ")); p != "" {
		argv = append(argv, p)
	}
	bin, err := exec.LookPath("claude")
	if err != nil {
		die("claude not found in PATH")
	}
	if err := os.Chdir(cwd); err != nil {
		die("%v", err)
	}
	if err := syscall.Exec(bin, argv, os.Environ()); err != nil {
		die("%v", err)
	}
}

func cmdShellInit(args []string) {
	sh := "zsh"
	if len(args) > 0 {
		sh = args[0]
	}
	switch sh {
	case "fish":
		fmt.Print(`# nanny helpers — add to ~/.config/fish/config.fish:  nanny shell-init fish | source
function sp --description "cd to the folder of a Claude session"
    set -l d (nanny where $argv); and cd $d
end
function spr --description "resume a Claude session from anywhere"
    nanny resume $argv
end
`)
	default:
		fmt.Print(`# nanny helpers — add to your ~/.` + sh + `rc:  eval "$(nanny shell-init ` + sh + `)"
sp()  { local d; d="$(nanny where "$@")" && cd "$d"; }   # cd to a session's folder
spr() { nanny resume "$@"; }                              # resume a session from anywhere
alias st=nanny
`)
	}
}

// cmdDemo builds a throwaway $HOME and re-execs nanny inside it.
func cmdDemo() {
	dir := filepath.Join(os.TempDir(), "nanny-demo")
	// panes left running by the last demo live in its own socket dir
	_ = os.Setenv("NANNY_SOCK_DIR", filepath.Join(dir, ".state", "nanny", "sock"))
	term.KillAll()
	_ = os.Unsetenv("NANNY_SOCK_DIR")
	_ = os.RemoveAll(dir)
	if err := demo.Build(dir); err != nil {
		die("building demo home: %v", err)
	}
	cfg := config.Default()
	cfg.Watch = []config.Watch{{Name: "api health", Group: "Watchers", Command: "echo '200 OK · p95 84ms'"}}
	_ = os.MkdirAll(filepath.Join(dir, ".config", "nanny"), 0o755)
	_ = os.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	_ = config.Save(cfg)
	// bin/ holds a stand-in claude so the demo's panes have agents to answer
	env := []string{"HOME=" + dir, "XDG_CONFIG_HOME=" + filepath.Join(dir, ".config"), "XDG_STATE_HOME=" + filepath.Join(dir, ".state"), "NANNY_DEMO=1",
		"PATH=" + filepath.Join(dir, "bin") + string(os.PathListSeparator) + os.Getenv("PATH")}
	for _, e := range os.Environ() {
		k, _, _ := strings.Cut(e, "=")
		switch k {
		case "HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "HERMES_HOME", "PATH":
			continue
		}
		env = append(env, e)
	}
	self, err := os.Executable()
	if err != nil {
		die("%v", err)
	}
	if err := syscall.Exec(self, []string{"nanny"}, env); err != nil {
		die("%v", err)
	}
}
