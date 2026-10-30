# Features

[← back to README](../README.md)

- [Spaces, tabs and panes](#spaces-tabs-and-panes)
- [Live agents and subagents](#live-agents-and-subagents)
- [Sessions](#sessions)
- [Git and worktrees](#git-and-worktrees)
- [Monitor](#monitor)
- [Skills and prompts](#skills-and-prompts)
- [Tasks](#tasks)
- [Screen profiles and Customize](#screen-profiles-and-customize)
- [Themes and appearance](#themes-and-appearance)
- [3D classroom](#3d-classroom)
- [Command palette, mouse and welcome guide](#command-palette-mouse-and-welcome-guide)
- [Languages](#languages)
- [Jira and Insights](#jira-and-insights)
- [CLI](#cli)

Every key mentioned here is listed in [keys.md](keys.md).

## Spaces, tabs and panes

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
  Settings (and `E` Jira, `I` Insights) are on top in "all spaces" (everything) and in every space (scoped to its folders). In a
  space `0 terminal` (or `esc`) goes back to its panes, whose tabs sit right over them with
  `+ claude` / `+ shell` buttons, the folder and the branch.
- **Claude exits, you keep a terminal**: when Claude quits in a pane (you declined trusting the
  folder, `/exit`, `ctrl+c`) the pane turns into a shell in the same folder.

## Live agents and subagents

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
  overridable in the config (`[prices.<family>]`, see [configuration.md](configuration.md)).
- **Answer permission prompts without switching.** When an agent waits on "Do you want to
  proceed?", Live, the sidebar, the popup and the alerts inbox show what it asks (the command,
  file or URL) and the options read from its screen. `⏎` opens the answer box: `y` yes,
  `a` yes and don't ask again (when offered), `n` no, `1-9` any option, `g` go to the pane.
- **Broadcast**: pick agents with `space` in Live and `p` sends the same prompt to all of them.
- `.` in Live sends an idle agent its last prompt again.
- Alerts: sounds, popups and an alerts inbox (`A`) for done, blocked and (optionally) subagent
  events; `S` mutes the sounds. See `[alerts]` in [configuration.md](configuration.md).

## Sessions

- Every past Claude session, with the worktree it lived in. `v` reads one full screen
  (`j/k` scroll, `G` follow, `t` tools), `r` resumes it, `f` forks it, `o` opens a shell in its
  folder, `y` copies the resume command.
- `F` searches the full text of every conversation; `b` shows bots, `w` only worktrees,
  `L` only live ones.
- Sessions shows the same Usage block as Live for any past session: context, tokens, cost, model.
- From the shell: `nanny ls`, `nanny where`, `nanny resume`, `nanny search`
  (see [CLI](#cli)).

## Git and worktrees

- The Git view (tab `3`, formerly Worktrees) lists the worktrees of your repos: which are dirty,
  which have sessions, which can go. `C` changes · `n` new Claude · `a` add · `d` remove ·
  `p` prune.
- Branches say how they are: `⎇ main ●3 ↑2 ↓1` (changed files, commits to push, commits to pull;
  `✓` when there is nothing to do) under each space, on Live rows (only when there is something)
  and in details. Read from git, no network.
- Open pull requests are detected with `gh` (every 3 minutes, per repo): `#96✓✔` (checks ✓ ✗ ◌,
  approved ✔, changes requested ✎, drafts dimmed) under the space (click opens it), on the agent's
  Live row, on worktrees and in the details. `O` in Live opens the agent's PR. When no PR shows
  anywhere (Basic profile), `gh` isn't asked for them.
- The space and worktree menus open the PR, pull (fast-forward, when behind) and fetch.
- **Changes view** (`C` in Live, prefix `g` in a pane): the diff of an agent's folder. `t` switches
  uncommitted vs HEAD and whole branch vs its base, `c` asks the agent to commit, `w` opens the PR
  (`gh`), `y` copies the branch.

<!-- Git view -->

## Monitor

Tab `7`: everything that isn't a Claude conversation, in sections you move
through with `← →` or a click: All · Machine · Ports · Containers · Tilt · Jobs · Cron ·
Hermes · Watchers. A section turned off in Customize (`=`) isn't there, and its source isn't run.

- **Hermes Agent** (gateway, platforms, cron, sessions), **Claude background jobs** and the
  daemon, **every crontab entry** (its schedule in words, `L` follows the file it logs to),
  **systemd user timers** (next and last run), lock/log-based loops, **opencode** sessions.
- **Your own watchers**: any shell command becomes an entry (`[[watch]]`, see
  [configuration.md](configuration.md)).
- A **Today** summary: sessions touched, live and working agents, subagents, repos, cost.
- **This machine**: load, memory, swap, disk and how much the Claude processes take.
- **Open ports**: the TCP ports your own programs listen on (dev servers, databases, Tilt…) with
  the process, folder and command; `⏎` opens `http://localhost:port`, `y` copies it.
- **Containers**: Docker containers (running first, then stopped) with image, status and ports.
  `⏎` or a right-click opens their actions: logs in a read-only tab (`s` there opens a shell
  inside), shell inside, live stats, open in the browser, restart, stop, start, remove.
- **Tilt**: the resources of a running Tilt (failed first, then running, then disabled) with
  their build error and endpoints; actions: logs (read-only), open the endpoint or the Tilt UI,
  rebuild (`tilt trigger`), enable, disable. Resources that are updating show as running, not
  "needs you".
- Logs open **read-only**: what you type never reaches them; `k j`/`u d` scroll, `q` closes.
- A summary line on top: how many things need you, failed, run, are scheduled, ports, containers.
  The counts are clickable.

## Skills and prompts

- **Skills** (tab `6`): the catalog of your skills, commands and subagents, global and per repo,
  from `~/.claude/{skills,commands,agents}` and plugins. `space` enables or disables one,
  `⏎` or `e` edits it in `$EDITOR`, `n` creates a skill, command or subagent (global or in a repo),
  `c` copies one into a project's `.claude`, `t` filters by type (including the harness: hooks,
  MCP, plugins, CLAUDE.md, settings), `H` hides disabled ones, `p` copies the path.
- **Prompt library** (tab `5`, `P`, prefix `P`): saved prompts and your most used ones (read from
  `~/.claude/history.jsonl`, per folder). `⏎` inserts one (typed into the pane, or sent to the Live
  agent); `{name}` placeholders ask for a value first. Saved prompts live in
  `~/.config/nanny/snippets.toml` (see [configuration.md](configuration.md)). Keys are in
  [keys.md](keys.md#prompt-library).
- **Prompt boxes** (`p` on an agent, broadcast, new task, queue form): `alt+1-9` adds one of your
  top saved prompts, `ctrl+r` cycles the most used prompts of that folder that contain what you
  typed.

## Tasks

**Task queue** (tab `4`, `u`, prefix `U`): a backlog nanny hands to Claude agents
by itself. Each task has a prompt, a folder and a mode: `here` types it into an idle nanny agent
of that folder (never one waiting on a prompt; a new tab when the folder has no agent), `new`
opens a claude tab, `worktree` makes a git worktree first. With auto-dispatch on (`a`), pending
tasks start in queue order, at most `queue.max_parallel` at once (default 2), in the background;
a task is done when its agent has worked and gone idle. Pending / Running / Done sections, add
(`n`), edit, remove, reorder (`J/K`), run one now (`⏎`), retry, jump to its agent (`g`), a
`☰ 3 queued` header badge and an alert when a task ends and when the queue empties. `q` in the
prompt library and `+` in Live (the selected agent's folder) queue a task too. Kept in
`$XDG_STATE_HOME/nanny/queue.json`.

## Screen profiles and Customize

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

The ids a profile can turn off are in [configuration.md](configuration.md#screen-profiles).

## Themes and appearance

- 20 themes, dark and light, with a live-preview picker (`T`), plus per-color overrides and
  your own themes, made in a live editor and shared as files.
- Icon sets (Unicode, Nerd Font, pure ASCII), border styles, bold/italic/underline on or off.
- Making your own themes, and the icon and border options, are described in
  [configuration.md](configuration.md#themes).

## 3D classroom

`9`, prefix `a`, or the ≡ menu: your agents as a little 3D game in the
browser, served by nanny on localhost (three.js is built in; nothing is fetched from the net).
Each agent is a teacher in her classroom, writing her lesson (your last prompt) on the board,
waving when she needs you and having a coffee when idle; each subagent is a kid that walks to
the bookshelf (reads, searches), the computers (Bash), the lab (edits) or the globe (web) by
the tool it is using, and back to its desk to think. Newcomers come in through the door while
the teacher's note flies to their desk; finished ones raise a hand and go out to the yard to
play in a park (swings, slide, seesaws, merry-go-round, sandbox, football). Around the school, a little town. Click anyone to see what they do; drag to look around.

## Command palette, mouse and welcome guide

- **Command palette** (`ctrl+k`): jump to any agent, space, session, worktree or action.
- **Mouse**: click spaces, view tabs, terminal tabs, the `+` buttons, rows (double-click opens)
  and any key hint on screen (`n new`, `⏎ resume`, the footer…) to press its key. Hover a tab to
  see what is in it (panes, program, folder, branch, Claude session and context); right-click a
  tab, pane, space or row for its menu (rename, icon, split, zoom, move, review, copy, close);
  drag tabs and spaces to reorder them; double-click a tab to rename it, middle-click to close it.
- **≡ menu** (sidebar button, the ◆ logo, `m`, or prefix `m`): Settings, keys, theme, customize,
  reload config, rescan, manage/discover spaces, detach and quit-and-stop.
- **Welcome guide** on the first start: what nanny is, each view, the space and pane keys and the
  mouse, in four short pages (`ctrl+k` → Welcome guide brings it back).

## Languages

English, Spanish, French and German. It follows your locale (`$LC_ALL`,
`$LC_MESSAGES`, `$LANG`) or `language` in the config; Settings → Look → Language switches it live.

## Jira and Insights

<!-- Jira view and setup wizard -->
- **Jira** (`E`): your issues by status, by sprint and what you closed in the last 7 days, each one linked to its PR and its agent (`●` when alive). `←→` sections, `/` search, `f` filter (all, in progress, to do), `⏎` open in the browser, `t` move to another status, `c` comment, `a` assign (to me, nobody, search), `y`/`Y` copy the key/link, `O` its PR, `s` its agent, `v` its conversation. `n` starts work: a worktree named `{key}-{slug}` and a Claude task with the issue in its prompt; `b` makes only the branch. With `sidebar.jira` on, a space on a branch like `PAY-412-fix` shows `PAY-412 · In Progress` under it.
  Set it up in the first-run wizard, in Settings → Connections or from the `≡` menu and the palette: your site URL, your email and an [API token](https://id.atlassian.com/manage-profile/security/api-tokens). The token never goes to `config.toml`: it is kept in the keyring (`secret-tool`) or in a file only you can read (`~/.config/nanny/secrets/`), and `NANNY_JIRA_TOKEN` beats both. Nothing is asked of Jira while `view.jira` is off or there is no URL and token; the view on screen refreshes every `refresh_mins`, and every 10 minutes off screen only for the sidebar line.

<!-- Insights view -->
- **Insights** (`I`): what your Claude usage costs and does, read from the local transcripts, with nothing sent
  anywhere and nothing read while the view is off or not on screen. Sections (`←→`): Summary, Cost (per day),
  Projects, Models, Tools (calls, failures, speed), Permissions (by rules, asked, accepted, rejected, blocked),
  Edits (accepted, rejected, lines), Friction (API retries, interruptions, compactions, error loops, and the
  sessions that cost more than `expensive_usd`: `⏎` reads one) and Week. `t` walks 7 days · 30 days · 90 days ·
  today, `[ ]` scope to a project like everywhere, `,` `.` move through weeks, `x` writes the week as
  `nanny-insights-2026-W44.json` (keys in order, numbers rounded) and `y` copies it, `r` reads everything again.
  The first run reads every transcript of the last `days` (a few seconds for a year of use); after that only the
  new bytes are read, and the tally is kept in `~/.local/state/nanny/insights/v1.gob`.
  `nanny insights [--week 2026-W44] [--json]` prints the same week without the TUI.

## CLI

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

`nanny demo` builds a throwaway `$HOME` with sessions, subagents and git worktrees and opens
nanny on it, with a stand-in agent waiting on a permission prompt you can answer from Live.
Your real files aren't touched.
