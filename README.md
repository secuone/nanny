<div align="center">

# ◆ nanny

**A terminal workspace for coding agents that actually keeps an eye on them.**

Spaces, tabs and split panes with real terminals, like a multiplexer, plus everything a
multiplexer can't see: each agent's **subagents**, which ones are **waiting for you**, every
past **session** (and the worktree it lived in), your **skills**, and the background stuff
(cron loops, background jobs, other assistants). With sounds, popups and one-key resume.

`Go` · single static binary · no daemon · your agents, at a glance

</div>

```
 ◆ nanny   ⊞ all spaces ⠋ acme-api 2   ● web-checkout   › dotfiles  +                ● 1 NEED YOU  ⠋ 1 working  ↳ 2 subs  ○ 1 idle  ♪  13:42
  1 ⠋ claude ⊞2   2 › shell   +                                                             ~/dev/acme-api/.claude/worktrees/PAY-412 ⎇ feature/PAY-412
                             │╭─ ⠋ Partial refunds for split payments ────────────────╮╭─ › zsh ──────────────────────────────────────────────╮
  SPACES                   3 ││ ● Both adapters expose refund(amount). The ledger     ││ ❯ make test                                          │
  ⊞ all spaces                   ││   needs a compensating entry per split. Writing the   ││ PHPUnit 11.2 by Sebastian Bergmann                   │
 1⠋ acme-api               2 ││   use case now.                                       ││ ...............................                      │
 2● web-checkout           1 ││                                                       ││ OK (31 tests, 88 assertions)                         │
 3› dotfiles                 ││ ⚙ Edit src/Payments/RefundSplitPayment.php            ││                                                      │
                             ││ ⚙ Bash vendor/bin/phpunit --filter Refund             ││ ❯                                                    │
  AGENTS                   2 ││                                                       ││                                                      │
  ⠋ Partial refunds for s… ↳2││ ✻ Running tests… (esc to interrupt)                   ││                                                      │
    ◆ acme-api               ││ ──────────────────────────────────────────────────    ││                                                      │
    ├ ⠋ Explore Map payment… ││ >                                                     ││                                                      │
    ├ ⠋ general… Run the fu… │╰───────────────────────────────────────────────────────╯╰──────────────────────────────────────────────────────╯
 ctrl+b + c claude · s shell · v - split · n p tab · z zoom · x close · w spaces · f find · 0 all spaces · ? help   alt+1-9 space · wheel scroll
```

> Try it on fake data first: `nanny demo` builds a throwaway `$HOME` with sessions,
> subagents and git worktrees and opens nanny on it, with a stand-in agent waiting on a
> permission prompt you can answer from Live. Your real files aren't touched.

---

## Why

Running many agents at once is great until you lose track:

- *"Which tab is the refunds work? Is it stuck on a permission prompt?"*
- *"It spawned six subagents… which are still running, and what are they doing?"*
- *"I had a great session about this last week, in some worktree… where?"*
- *"Which of my 13 worktrees are dirty, which have sessions, which can go?"*

nanny is a terminal workspace (spaces → tabs → panes) built around those questions.

## Features

### Spaces, tabs and panes
- **Spaces** are your projects. Each has folders, its own tabs, and a rolled-up status
  (working / needs you / idle) in the top bar and sidebar, and an optional icon (🚀 🐘 🧪…):
  right-click a space to rename it, pick its icon, move or delete it.
- **Real terminals** in tabs and split panes: Claude, a shell, anything. Full colors,
  scrollback (mouse wheel), zoom.
- **Discover** (`D`) finds every place you work in: folders where agents ran, git repos
  under your code roots, open workspaces. Tick the ones you want as spaces.
  Or create them by hand (`N`), add/remove folders, rename, reorder, delete (`W`).
- **Detach / reattach**, like tmux: quitting nanny leaves every pane running in the
  background, so agents keep working. Reopen nanny and they come back where they are,
  scrollback included. `prefix Q` (or `k` in the quit dialog) quits and stops them all.
- **Restore**: panes that were stopped (reboot, `kill-server`) come back too: same spaces
  and tabs, every Claude pane reopened with `claude --resume <its session>`.
- **Mouse selection**: drag to select text anywhere (inside a pane it stays in that pane) and let go to copy it.
- Prefix key (default `ctrl+b`, configurable) for pane commands; `alt+1-9` jumps between
  spaces without the prefix.
- **One view bar everywhere**: Live, Sessions, Git, Tasks, Prompts, Skills, Monitor and
  Settings (and `E` Jira, `I` Insights) are on top in “all spaces” (everything) and in every space (scoped to its folders). In a
  space `0 terminal` (or `esc`) goes back to its panes, whose tabs sit right over them with
  `+ claude` / `+ shell` buttons, the folder and the branch.
- **Claude exits, you keep a terminal**: when Claude quits in a pane (you declined trusting the
  folder, `/exit`, `ctrl+c`) the pane turns into a shell in the same folder.

### Agents and subagents, live
- Every running Claude Code process: inside nanny, in other terminals, or in background jobs.
- **Subagent trees** under each agent in the sidebar and in the Live view: type,
  description, running/finished, the tool it's using right now, nesting. Those that finished a
  while ago fold into one `✓ N finished subagents` row (`⏎` or a click unfolds them); the agent's
  detail lists every finished one, newest first.
- **Open any subagent** (`⏎`) in a full-screen live viewer of its conversation, with
  follow mode, scroll and a tools on/off toggle. Same for any session: `v` reads it, in every list.
- Send a prompt to an agent (`p`), jump to its pane (`⏎`), see what's on its screen (`s`).
- `!` cycles to the **next agent that needs you**.
- **Context & cost meter**: every agent row shows how full its context is (`▰▰▰▱▱ 62%`,
  green, yellow past 60%, red past 85%) and its estimated cost (`≈$0.84`, subagents
  included). The detail pane adds a Usage block (context used / window, tokens in, out and
  cache, cost of the session and of its subagents, model), Sessions shows the same for any
  past session, and the header shows `≈$12.40 today`. An agent crossing 85% raises one alert:
  it is close to auto-compact. Costs are estimates from list prices per model family,
  overridable in the config (`[prices.<family>]`).
- **Answer permission prompts without switching.** When an agent waits on "Do you want to
  proceed?", Live, the sidebar, the popup and the alerts inbox show what it asks (the command,
  file or URL) and the options read from its screen. `⏎` opens the answer box: `y` yes,
  `a` yes and don't ask again (when offered), `n` no, `1-9` any option, `g` go to the pane.
- **Broadcast**: pick agents with `space` in Live and `p` sends the same prompt to all of them.
- **Task queue** (tab `4`, `u`, prefix `U`): a backlog nanny hands to Claude agents
  by itself. Each task has a prompt, a folder and a mode: `here` types it into an idle nanny agent
  of that folder (never one waiting on a prompt; a new tab when the folder has no agent), `new`
  opens a claude tab, `worktree` makes a git worktree first. With auto-dispatch on (`a`), pending
  tasks start in queue order, at most `queue.max_parallel` at once (default 2), in the background;
  a task is done when its agent has worked and gone idle. Pending / Running / Done sections, add
  (`n`), edit, remove, reorder (`J/K`), run one now (`⏎`), retry, jump to its agent (`g`), a
  `☰ 3 queued` header badge and an alert when a task ends and when the queue empties. `q` in the
  prompt library and `+` in Live (the selected agent's folder) queue a task too. Kept in
  `$XDG_STATE_HOME/nanny/queue.json`.
- **Prompt boxes** (`p` on an agent, broadcast, new task, queue form): `alt+1-9` adds one of your
  top saved prompts, `ctrl+r` cycles the most used prompts of that folder that contain what you
  typed. `.` in Live sends an idle agent its last prompt again.
- **3D classroom** (`9`, prefix `a`, or the ≡ menu): your agents as a little 3D game in the
  browser, served by nanny on localhost (three.js is built in; nothing is fetched from the net).
  Each agent is a teacher in her classroom, writing her lesson (your last prompt) on the board,
  waving when she needs you and having a coffee when idle; each subagent is a kid that walks to
  the bookshelf (reads, searches), the computers (Bash), the lab (edits) or the globe (web) by
  the tool it is using, and back to its desk to think. Newcomers come in through the door while
  the teacher's note flies to their desk; finished ones raise a hand and go out to the yard to
  play in a park (swings, slide, seesaws, merry-go-round, sandbox, football). Around the school, a little town. Click anyone to see what they do; drag to look around.
- **Customize** (`=`, the ≡ menu or Settings): a tree of everything that can go — each view,
  each Monitor source, the branch, PR, worktrees, agents and Hermes dot of the sidebar, the counts,
  queue, cost, sound and clock of the top bar. `space` or a click switches a node (a parent switches
  all under it; ◩ means part of it is off), `← →` fold. Presets: `a` everything · `e` essentials ·
  `t` Monitor with Tilt only · `d` Monitor with ports, containers and Tilt · `n` no Monitor. What is
  off is gone: off the views bar, its number and the palette; a Monitor source turned off isn't
  collected at all. It applies at once to the screen profile in use (below) and is saved in the config.
- **Screen profiles**: **Basic** is the bare minimum — Live and Sessions, one line per agent, who
  needs you, the spend and the context gauges; the Monitor and the rest aren't even collected.
  **Full** is everything. Your own are sets of Customize switches: in Customize `tab` walks the
  profiles, `N` makes one from the one in use, `R` renames it, `X X` deletes it; a change to a
  built-in one goes to a copy of yours. `M` walks them from anywhere, and the quiet word at the end
  of the top bar is the one in use — a click lists them, the wheel over it walks them. Also in the
  ≡ menu, the palette and Settings.
- **Monitor** (tab `7`): everything that isn't a Claude conversation, in sections you move
  through with `← →` or a click — All · Machine · Ports · Containers · Tilt · Jobs · Cron ·
  Hermes · Watchers. A section turned off in Customize (`=`) isn't there, and its source isn't run.
- **Hermes Agent** (gateway, platforms, cron, sessions), **Claude background jobs** and the
  daemon, **every crontab entry** (its schedule in words, `L` follows the file it logs to),
  **systemd user timers** (next and last run), lock/log-based loops, **opencode** sessions.
- **Your own watchers**: any shell command becomes an entry (see the config section).
- A **Today** summary: sessions touched, live and working agents, subagents, repos, cost.
- **This machine**: load, memory, swap, disk and how much the Claude processes take.
- **Open ports**: the TCP ports your own programs listen on (dev servers, databases, Tilt…) with
  the process, folder and command; `⏎` opens `http://localhost:port`, `y` copies it.
- **Containers**: Docker containers (running first, then stopped) with image, status and ports.
  `⏎` or a right-click opens their actions: logs in a read-only tab (`s` there opens a shell
  inside), shell inside, live stats, open in the browser, restart, stop, start, remove.
- **Tilt**: the resources of a running Tilt (failed first, then running, then disabled) with
  their build error and endpoints; actions: logs (read-only), open the endpoint or the Tilt UI,
  rebuild (`tilt trigger`), enable, disable.
- Logs open **read-only**: what you type never reaches them; `k j`/`u d` scroll, `q` closes.
- A summary line on top: how many things need you, failed, run, are scheduled, ports, containers.

### Everywhere
- **Command palette** (`ctrl+k`): jump to any agent, space, session, worktree or action.
- 20 themes, dark and light, with a live-preview picker (`T`), plus per-color overrides and
  your own themes, made in a live editor and shared as files.
- Icon sets (Unicode, Nerd Font, pure ASCII), border styles, bold/italic/underline on or off.
- **Mouse**: click spaces, view tabs, terminal tabs, the `+` buttons, rows (double-click opens)
  and any key hint on screen (`n new`, `⏎ resume`, the footer…) to press its key. Hover a tab to
  see what is in it (panes, program, folder, branch, Claude session and context); right-click a
  tab, pane, space or row for its menu (rename, icon, split, zoom, move, review, copy, close);
  drag tabs and spaces to reorder them; double-click a tab to rename it, middle-click to close it.
- **Welcome guide** on the first start: what nanny is, each view, the space and pane keys and the
  mouse, in four short pages (`ctrl+k` → Welcome guide brings it back).
- **Prompt library** (tab `5`, `P`, prefix `P`): see below.
- **Languages**: English, Spanish, French and German. It follows your locale (`$LC_ALL`,
  `$LC_MESSAGES`, `$LANG`) or `language` in the config; Settings → Look → Language switches it live.

<!-- Git view -->

- **Jira** (`E`): your issues by status, by sprint and what you closed in the last 7 days, each one linked to its PR and its agent (`●` when alive). `←→` sections, `/` search, `f` filter (all, in progress, to do), `⏎` open in the browser, `t` move to another status, `c` comment, `a` assign (to me, nobody, search), `y`/`Y` copy the key/link, `O` its PR, `s` its agent, `v` its conversation. `n` starts work: a worktree named `{key}-{slug}` and a Claude task with the issue in its prompt; `b` makes only the branch. With `sidebar.jira` on, a space on a branch like `PAY-412-fix` shows `PAY-412 · In Progress` under it.
  Set it up in the first-run wizard, in Settings → Connections or from the `≡` menu and the palette: your site URL, your email and an [API token](https://id.atlassian.com/manage-profile/security/api-tokens). The token never goes to `config.toml`: it is kept in the keyring (`secret-tool`) or in a file only you can read (`~/.config/nanny/secrets/`), and `NANNY_JIRA_TOKEN` beats both. Nothing is asked of Jira while `view.jira` is off or there is no URL and token; the view on screen refreshes every `refresh_mins`, and every 10 minutes off screen only for the sidebar line.

<!-- Insights view -->

### CLI

```sh
nanny ls checkout -n 10        # list sessions matching "checkout"
nanny where PAY-412            # print the folder of the best match
nanny resume PAY-412           # cd there and `claude --resume` it (-f forks)
nanny search "ledger"          # full-text search, grep-style
nanny live                     # running agents with subagent counts
nanny ps                       # panes running in the background
nanny kill-server              # stop every background pane
nanny new -w fix-login "Fix the login redirect"   # new worktree + branch, claude with that first prompt
eval "$(nanny shell-init zsh)" # adds: sp <query> (cd to a session's folder), spr <query>
```

## Install

```sh
# release binary (falls back to `go install` if there's none for your platform)
curl -fsSL https://raw.githubusercontent.com/secuone/nanny/main/scripts/install.sh | sh

# or with Go ≥ 1.25
go install github.com/secuone/nanny/cmd/nanny@latest

# or from a clone
make install            # → ~/.local/bin/nanny
```

Optional shell helpers:

```sh
echo 'eval "$(nanny shell-init zsh)"' >> ~/.zshrc     # bash: shell-init bash · fish: shell-init fish | source
```

Requirements: Linux or macOS and `git`. Optional extras:
- `python3`: reads the Hermes and opencode SQLite databases read-only.
- `pw-play`, `paplay` or `afplay`: sounds (otherwise the terminal bell).
- `notify-send`: desktop alerts.
- `wl-copy` or `xclip`: clipboard (otherwise OSC 52).

## Keys

**Inside a space**: prefix (`ctrl+b`) then:

| Key | Action |
| --- | --- |
| `c` / `s` | new Claude tab / new shell tab |
| `C` / `g` | new task (folder, prompt, worktree) / review this pane's changes |
| `i` / `0` | this space's views (Live, Sessions, Tasks… scoped to it) / back to the terminal |
| `P` / `U` | prompt library / task queue of this space |
| `a` | the 3D classroom, in the browser |
| `v` / `-` / `V` | split right / split down / split right with Claude |
| `n` `p` `1-9` | next / previous / go to tab |
| `h` `l` (arrows) | move between panes |
| `z` / `x` / `X` | zoom pane / close pane / close tab |
| `,` / `R` | rename tab / rename space |
| `r` / `f` | resume the latest session of this space / its sessions |
| `]` `[` `0` | next / previous space / all spaces |
| `w` `N` `D` | manage spaces / new space / discover places |
| `u` `d` (wheel) | scroll back / forward |
| `K` `:` | command palette |
| `A` / `S` / `?` | alerts / mute / help |
| `T` | theme picker |
| `q` / `Q` | quit, panes keep running (reattach on next start) / quit and stop every pane |
| `ctrl+b` `ctrl+b` | send the prefix key to the program |

`alt+1-9`, `alt+←/→` and `alt+0` switch spaces without the prefix.

**Views** (all spaces, or a space after prefix `i`): `1-8` Live · Sessions · Git · Tasks · Prompts · Skills · Monitor · Settings, `E` Jira, `I` Insights, `9` the 3D classroom, `tab` next,
`0` back to the space's terminal,
`ctrl+k` palette, `P` prompt library, `u` task queue, `!` next agent that needs you, `[ ]` scope to a project (`\` clears), `B` sidebar, `M` next screen profile (Basic · Full · yours),
`A` alerts, `S` mute, `T` themes, `W`/`N`/`D` spaces, `?` the full list.

The same key does the same thing in every list:

| Key | Action |
| --- | --- |
| `⏎` | main action: go to the agent · resume · edit · open |
| `v` | read the conversation full screen (`j/k` scroll, `G` follow, `t` tools) |
| `r` / `f` | resume / fork |
| `o` | shell in its folder |
| `y` | copy (resume command, path, `/invocation`) |
| `/` · `esc` | filter · clear and go back |
| `j/k` `g/G` `J/K` | move · top/bottom · next/previous agent, repo or group |
| click · double-click | select · open |

View extras: Live `⏎` answers a waiting agent · `p` prompt · `space` pick agents then `p` sends to all ·
`n` new task · `+` queue a task in its folder · `.` repeat its last prompt · `C` changes · `s` screen mirror · `a` finished subagents; Sessions `F` full-text
search · `b`/`w`/`L` bots, worktrees, live; Git (Worktrees) `C` changes · `n` new Claude · `a` add · `d` remove · `p` prune;
Skills `space` on/off · `n` new (skill, command or subagent; global or in a repo) · `t` type, including the harness (hooks, MCP, plugins, CLAUDE.md, settings) · `c` copy to a project · `t` type · `H` hide disabled; Monitor `← →` sections · `L` follow log; `=` customizes what shows.

Prompt library: `⏎` insert · `/` filter · `J/K` Saved or Most used · `s` save · `n` new · `e` edit ·
`d` delete · `p` pin · `y` copy · `q` add to the task queue · `esc` back; in the editor `tab` field · `ctrl+s` save · `ctrl+e` `$EDITOR`.

Task queue: `n` add · `e` edit · `d` remove · `J/K` reorder · `⏎` run now (running: go to it) · `a` auto-dispatch ·
`g` go to its agent · `r` retry · `c` clear finished · `esc` back. In any prompt box `alt+1-9` saved prompt · `ctrl+r` most used.

Answer box: `y` yes · `a` always · `n` no · `1-9` option · `g` go to the pane · `esc` close.
Changes view: `j/k` file · `l` into the diff (`h` back) · `space`/`b` page · `n`/`N` hunk · `t` uncommitted or
branch · `c` ask to commit · `w` open PR · `y` copy branch · `o` shell · `r` refresh · `esc` back.

## Configuration

`~/.config/nanny/config.toml` is created on first run. Everything in it can also be
edited from the **Settings** tab. `T` (or prefix `T` inside a space, or "Theme…" in the
palette) opens the theme picker: it previews each theme as you move, `⏎` keeps it, `esc`
reverts. Light themes are meant for terminals with a light background. See
[`docs/config.example.toml`](docs/config.example.toml).

**Your own themes.** In the picker, `n` starts a new theme from the one under the cursor
and `e` edits one of yours (a built-in one is never changed: you edit a copy). The editor
lists every color with what it paints; `⏎` types a hex (`#ff00aa` or `f0a`), `←→` walk
through the other themes' values for that color, `+`/`-` make it lighter or darker, and
all of nanny shows it as you go. It warns when text gets hard to read (contrast too low).
`s` saves. Back in the picker: `d` duplicates, `r` renames, `x` twice deletes, `o` exports
a theme to a file or folder and `i` imports one. Your themes are files in
`~/.config/nanny/themes/<name>.toml`, read at start and on "Reload config"; one only needs
the colors it changes:

```toml
base = "gruvbox"     # the rest comes from here (nanny when left out)
[colors]
accent = "#ff5500"
```

**Icons, borders, fonts.** `a` in the theme picker (or "Appearance…" in the ≡ menu) picks
the icon set — `unicode`, `nerd` (Nerd Font icons for branches, checks, warnings, keys…) or
`ascii` (plain ASCII everywhere; `auto` picks it on the Linux console or a non-UTF-8
locale) — the borders (rounded, square, thick, double, ascii, none), bold/italic/underline
on or off, and the font of the 3D page. What a program prints in a pane is never changed.
The terminal's own font is the terminal's: nanny can't change it reliably (xterm's OSC 50
is the only escape for it and Ghostty, kitty, WezTerm or GNOME Terminal ignore it), so set it
in your terminal.

```toml
theme = "nanny"              # dark: catppuccin-mocha, tokyo-night, dracula, nord, gruvbox, rose-pine, kanagawa, one-dark, solarized-dark, everforest, github-dark, monokai, ayu, high-contrast
                             # light: nanny-light, catppuccin-latte, one-light, solarized-light, github-light
language = "auto"            # en, es, fr, de; auto follows $LC_ALL / $LC_MESSAGES / $LANG (English if unsupported)
prefix = "ctrl+b"            # pane command prefix
restore = true               # reopen spaces & resume Claude panes on start
claude_args = ""             # appended to every claude you start/resume, e.g. "--model opus"
discover_roots = ["~/dev"]   # where Discover looks for git repos (default: ~/dev, ~/src, ~/code, …)
hide_bots = true
profile = "full"             # screen profile: basic (bare minimum) · full (everything) · one of yours
                             # ([profiles.<name>] below; made in Customize =, walked with M)

[screen]
sidebar_agents = "auto"      # auto (a few in the one-line layout) · all

[profiles.tilt]              # one of yours: what it turns off
off = ["view.prompts", "view.skills", "monitor.machine", "monitor.ports", "monitor.containers",
       "monitor.jobs", "monitor.cron", "monitor.hermes", "monitor.watchers"]
                         # what the profile turns off, by id:
                         #   layout:  layout.detail (off: one line per agent) layout.extras (model, gauge always, no-repeat off)
                         #   views:   view.live view.sessions view.git view.tasks view.prompts view.skills view.monitor view.jira view.insights
                         #   Monitor: monitor.machine monitor.ports monitor.containers monitor.tilt
                         #            monitor.jobs monitor.cron monitor.hermes monitor.watchers (off = not collected)
                         #   sidebar: sidebar.branch sidebar.pr sidebar.worktrees sidebar.agents sidebar.hermes sidebar.jira
                         #   top bar: header.counts header.queue header.cost header.sound header.clock header.reviews
                         # the old density, basic and [screen] off are read once into a profile "mine"

[look]
icons = "auto"               # auto · unicode · nerd · ascii
borders = "rounded"          # rounded · square · thick · double · ascii · none
no_bold = false
no_italic = false
no_underline = false
web_font = ""                # CSS font-family of the 3D page, e.g. "Georgia, serif"

[alerts]
sound = true
on_done = true
on_blocked = true
on_subagent = false
toast = true
desktop = false
sound_done = ""              # custom file; empty = system "complete"
sound_blocked = ""           # custom file; empty = system "dialog-warning"

[queue]
max_parallel = 2             # queued tasks running at once

[[watch]]                    # your own Monitor entries
name = "staging health"
command = "curl -fsS https://staging.example.com/health"
```

Layout state lives in `~/.local/state/nanny/workspace.json`, the task queue in `queue.json` next to it. Saved prompts live in
`~/.config/nanny/snippets.toml`:

```toml
[[snippet]]
name = "tests"
text = "Run the test suite and fix whatever fails."
pinned = true

[[snippet]]
name = "ticket"
text = "Read {ticket} and plan the change step by step before writing any code."
tags = ["plan"]
```

## How it works

Each pane's pty is owned by a tiny holder process (`nanny __hold`, idle at 0% CPU) that
outlives nanny, like dtach. nanny talks to it over a unix socket in
`$XDG_RUNTIME_DIR/nanny/` (or `~/.local/state/nanny/sock/`): input, resizes and kill go
in, output comes out. The holder keeps the last ~2 MB of output and replays it into a
fresh screen when nanny reattaches; it exits when its program does. Sockets nobody
answers on are cleaned up.

nanny renders each pane with an in-memory VT emulator, and *reads* what your tools
already write:

| Source | What |
| --- | --- |
| `~/.claude/projects/*/*.jsonl` | sessions: tail and head per file, cached by size and mtime |
| `~/.claude/projects/*/<id>/subagents/` | subagent trees: metadata plus transcript tails |
| `message.usage` in those transcripts | context & cost meter: live and today's sessions, read once then only appended bytes |
| `~/.claude/sessions/<pid>.json` | live Claude processes, mapped to panes through the process tree |
| pane screens | needs-you / working detection, and the permission dialog's options ("❯ 1. Yes") |
| `~/.claude/{skills,commands,agents}`, plugins | the skills catalog |
| `~/.claude/history.jsonl` | most used prompts and skill usage, cached by size and mtime |
| `~/.claude/jobs`, `~/.hermes`, crontab, systemd timers, /proc | Monitor |
| the Docker socket, Tilt's API (`~/.tilt-dev/config`) | Monitor: containers and Tilt resources, asked directly (`docker ps` / `tilt get` only when there is no socket or config) |
| `git worktree list` | worktrees |

Measured on a real machine (72 projects, 326 sessions, 824 MB of transcripts):

| Operation | Time |
| --- | --- |
| Cold scan | ≈ 350 ms |
| Rescan | ≈ 5 ms |
| 128 subagents (warm) | ≈ 7 ms |
| Full-text search | ≈ 0.6 s |

Nothing is read again until it changes: an inotify watch on `~/.claude` (projects,
session folders, subagents, live processes, history) says which transcripts changed, and
only those are read, within a second. A full scan still runs every 30 s in case an event
was missed; without inotify (or out of watches) nanny scans every `refresh_secs` instead.
The Monitor's sources are only collected while the Monitor is on screen, and less often
while they keep coming back the same. With nothing moving, nothing is drawn: no frames,
the clock and "3m ago" once a minute. The screen is flushed at most 30 times a second
(`NANNY_FPS` changes it); `NANNY_DEBUG=1` logs what the watch does to
`~/.local/state/nanny/debug.log`.

Idle, nanny takes under 1% CPU (0.4–0.7% here; ~1.5% with the Monitor on screen) and writes nothing to the terminal;
with agents at work, 2–3%.

Nothing is sent anywhere.

## Development

```sh
make run      # build + launch
make demo     # launch on the fake $HOME
make test     # tests use a generated fixture home and real ptys
make lint
```

Packages:

| Package | What it holds |
| --- | --- |
| `cmd/nanny` | CLI |
| `internal/term` | pty + emulator panes |
| `internal/ws` | spaces, tabs, persistence, process tree |
| `internal/claude` | sessions, subagents, search, matching |
| `internal/skills` | skills catalog |
| `internal/prompts` | most used prompts from history, placeholders |
| `internal/discover` | Discover |
| `internal/gitwt` | worktrees |
| `internal/sources` | Monitor |
| `internal/ui` | Bubble Tea views |
| `internal/i18n` | UI translations (`T`, `Tf`, `Tn`), one table per language |
| `internal/demo` | fixture home |
| `internal/mux` | optional external-workspace socket client |

Contributions welcome, see [CONTRIBUTING.md](CONTRIBUTING.md). Roadmap ideas:
- detach/attach with a background server
- Codex, Gemini and opencode as first-class live agents
- per-session cost charts
- session tags and pins

## License

MIT
