# Changelog

## Unreleased

- **Every space has a folder.** New space (`N`) asks for the folder first, with a path input that completes with `tab` (`~` works), the folders you already work in (agent sessions, git repos, your spaces) to filter or pick, a clear error when the path isn't a folder and an offer to create one that doesn't exist; the name defaults to the folder's and can be edited. Shells, Claude tabs and splits always start in the space's folder (they used to follow the focused pane). Prefix `F`, `f` in the spaces manager and the space menu change it: panes already open stay where they are. Spaces saved without a folder load as before, show `no folder · set` in the sidebar, the space header and the manager (a click sets it) and keep starting panes in your home.

- **Install and compatibility documented.** New [docs/install.md](docs/install.md): requirements (Linux only for now: nanny reads `/proc` and uses inotify, so macOS doesn't build yet; Windows is not supported), terminals, fonts, optional tools, install, update, uninstall and where nanny keeps its files. `scripts/install.sh` now says when there is no release, builds `main` by cloning when it falls back (`go install …@latest` can't work with the `replace` in `go.mod`), refuses unsupported platforms and tells you when `BIN_DIR` isn't on your `PATH`.
- **Git & PRs has an Overview** (the new first section, and the default inside a space): the branch, its state, its PR and the worktrees of each space, from git alone. Prefix `G` and a click on the `⎇ branch` in the terminal strip or the sidebar open it; the section bar shows the `gh` sign-in state, and the empty states offer `i` (sign in), `s` (setup) and `o` (open cli.github.com). The view is now called Git & PRs (it was Git, and Worktrees before).
- **Sidebar navigation**: open an agent from any of its sidebar lines, follow and page the sidebar with the keys, walk the agents with `alt+↑/↓` from anywhere, and the top bar shows a window of your spaces when they don't all fit.
- **Many panes stay cheap**: with 10 panes, CPU went from 50% to 4%. Pane segments are stitched per row, holders run with `GOMAXPROCS=1` and a fixed ring buffer, the vendored terminal emulator (`third_party/vt`) has a 256 KiB parser buffer instead of 4 MiB, pane state and rendered lines are cached, and output of unfocused panes redraws at most every 50 ms.

- **`ctrl+q` quits from anywhere**, inside a space's terminal too. It asks first (panes keep running, `k` stops them) and `ctrl+q` again answers yes. It shows as `^q quit` in the footer; the palette has "Quit nanny" and "Go to space" for every space, and the ≡ menu says "Quit nanny" instead of "Detach". `quit_key` in the config (and Settings) changes it; `"none"` hands `ctrl+q` back to the panes. The welcome guide and `?` list it, with `alt+↑/↓` and prefix `G`, and the Git & PRs tooltip says what the view holds.

- **Bubble Tea v2, Lip Gloss v2 and Bubbles v2** (building needs Go 1.26). The screen is drawn by the v2 cell-diff renderer: only the cells that changed are written. With 10 panes and one printing the terminal output went from 6 to 0.2 KiB/s (4 panes: 10.7 to 0.3 KiB/s) and idle stays at 0 KiB/s; CPU while a pane prints is about 0.7 to 1 point higher (the whole frame is still built each time) and the client takes 5 to 8 MiB more memory. The focused pane's cursor is now the terminal's own cursor, with the shape and blinking the program asks for, and hides under the prefix, popups and selections. Pastes into a pane are bracketed. Panes get the same bytes as before, except `alt+arrows`, `alt+home/end`, `alt+pgup/pgdn` and `alt+F1-4`, which now send the xterm form with a modifier (`ESC [1;3A`) instead of ESC plus the key; `shift+enter` sends a plain return and `ctrl+space` works as a prefix. `NANNY_OUTSTATS=<file>` logs what the screen writes every 5 s.

- **Git view**: sections My PRs, Reviews, Worktrees, Branches and Profile, with `gh` (needs `gh auth login`). Merge asks first and never uses `--admin`; a PR that isn't ready gets auto-merge. `c` in Reviews starts a Claude on the PR in a worktree; the top bar counts the reviews waiting (`header.reviews`); Branches finds the ones merged, squashes included, and deletes them after asking. `[git]` in the config and Settings → Git: `merge_method`, `reviews`, `review_mins`, `review_prompt`.

- **Jira view (`E`) and a first-run setup wizard.** Your issues by status, by sprint and done in the last 7 days, linked to their PR and their agent, with search, filter, transition, comment, assign, copy and open in the browser. `n` starts work on an issue (worktree `{key}-{slug}` plus a Claude task with the issue in the prompt), `b` only the branch. The sidebar shows `KEY · status` under a branch that has an issue key (`sidebar.jira`). The wizard opens once after the welcome (and from Settings → Connections, the `≡` menu and the palette) to connect Jira and check `gh`. The token is kept in the keyring or a `0600` file, never in `config.toml`; `NANNY_JIRA_TOKEN` takes priority. New `[jira]` table: `url`, `email`, `jql`, `sprint_field`, `branch_template`, `refresh_mins`.

- **Insights view** (`I`): cost, tokens, active time, tools, permissions, edits and friction per day, project and
  model, read from your transcripts with an incremental on-disk cache; the week exports as JSON (`x`, `y`) and
  `nanny insights [--week] [--json]` prints it. `[insights]` in the config: `days`, `expensive_usd`, `export_dir`.

- **Worktrees is now Git** (still key `3`) and the views bar has room for two more: `E` Jira and `I` Insights. In your profiles `view.worktrees` is read once as `view.git`.

- **Nanny waits for changes instead of looking every 2 seconds.** An inotify watch on `~/.claude` says which transcripts, subagents and live processes changed, and only those are read again (in under a second; a full scan every 30 s is the safety net, and without inotify it scans on the tick as before). With nothing moving no frame is drawn at all, the clock and relative times once a minute; branches are read every minute instead of 20 s while no agent works; the Monitor's sources are collected only while it is on screen. Docker and Tilt are asked over their own APIs instead of starting `docker ps` and `tilt get` (200 ms of CPU each time) every 4 s; Hermes' database is queried again only when it changes. The screen flushes at most 30 times a second (`NANNY_FPS`). Idle CPU: 2.7–6% → 0.4–0.7% (Monitor on screen 11% → 1.5%); with agents at work 6–7% → 2–3%.

- Calmer still: footer key letters in plain text instead of pink bold, the muted-sound mark in grey instead of the error red, and the Settings preview matching the views.

- **Screen profiles** replace the full · normal · minimal densities. **Basic** is the bare minimum (Live and Sessions, one line per agent, who needs you, the spend and the context gauges; the Monitor and the rest aren't collected) and **Full** is everything; your own are sets of Customize switches — in Customize `tab` walks them, `N` makes one, `R` renames, `X X` deletes, and a change to a built-in one goes to a copy of yours. `M` walks them from anywhere; the quiet word at the end of the top bar is the one in use (a click lists them, the wheel walks them); also in the ≡ menu, the palette and Settings. Customize gained a Layout group (Detail: off is one line per agent; Extras: model, gauge always, repeats). Saved as `profile = "…"` and `[profiles.<name>] off = […]`; the old `density`, `basic`, `[screen] off` and `sidebar_agents = "off"` are read once into a profile called "mine" (or Full when they asked for everything). When no PR shows anywhere (Basic), `gh` isn't asked for them.

- 3D park: kids walk around the fountain, benches, lamps, trees and the rides (grid paths in the park and the rooms, the teacher too) and wait behind each other; swings, seesaws and the merry-go-round move with whoever is on them and stand still when empty; the slide has a queue; tag and watching the match when the games are full; no naps; nothing in the park or the town overlaps (checked over thousands of frames headless).
- **Your own color themes**: in the theme picker (`T`) `n` makes a new one from the theme under the cursor and `e` edits yours, in an editor that lists every color with what it paints and shows it all over nanny as you go (type a hex, walk through the other themes' values with ←→, lighter/darker with +/-), warning when text gets hard to read. `d` duplicates, `r` renames, `x` twice deletes, `o` exports and `i` imports. They are files in `~/.config/nanny/themes/`, read at start and on Reload config; a file only needs the colors it changes on top of a `base`.
- **Appearance** (`a` in the theme picker, or ≡ → Appearance…): icon set (Unicode, Nerd Font or pure ASCII; auto picks ASCII where Unicode can't show), border style (rounded, square, thick, double, ASCII, none), bold/italic/underline on or off, and the 3D page's font. Nothing moves when they change, what a program prints in a pane is left alone, and the default look costs nothing. The terminal's own font stays the terminal's: no reliable escape changes it.

- **Customize** (`=`, the ≡ menu, or Settings → Customize): a tree with everything that can be turned off — each view (Live included), each Monitor source (Machine, Ports, Containers, Tilt, Jobs, Cron, Hermes, Watchers), the sidebar's branch, PR, worktrees, agents and Hermes dot, the top bar's counts, queue, cost, sound and clock. A parent switches all under it (◩ when part of it is off); presets for everything, the essentials, a Monitor with Tilt only, one with ports/containers/Tilt, or no Monitor. Off means gone: a view leaves the bar, its number and the palette; a Monitor source isn't even collected (nor anything with the Monitor off, nor the sidebar's worktree scan with its worktrees off). Applies at once, saved as `[screen] off = [...]`, kept by Reload config. It replaces the menu's “Views on the bar” and “Features” submenus and the Monitor's “space turns it on”; the old `hide_views`, `hide_cost`, `hide_clock` and `[sources]` are read once and folded into the new list.
- Faster start: the first screen lists your sessions and agents without waiting for their token usage (~0.27 s instead of ~1 s on 700 sessions; usage fills in right after). Session tails are read for their metadata without decoding message contents (3.6× faster, same result on 716 real transcripts); a running subagent's last step is read back from the end of its transcript in chunks, and subagent meta files are cached.
- Background refreshes that come back together after each tick (sessions, subagents, usage, branch state) are drawn once, 50 ms later, instead of one full redraw each. Keys, the mouse and terminal output still draw at once.
- Calmer screen: only agents spin; a running subagent is a steady ●. Project names, branches, subagent types, tool names, ↳ counts, the subagents' cost and today's spend are no longer in their own colours: colour is left for state (working, needs you, idle, done, context filling up) and the accent. Live with two agents working: terminal output 45 → 12 KiB/s, idle CPU ~6 → ~4 %.

- Reattaching to a shell no longer stacks its prompt again each time nanny opens: the holder only nudges full-screen programs (not a shell at its prompt), a pane attached before the layout is known keeps the program's size, and the replay lands in a screen the size it was written for.
- Sidebar: a space's worktrees are folded under its branch by default (“▸ N worktrees”); a click opens them all and closes them again.

- **Select and copy with the mouse**: nanny owns the mouse, so the terminal couldn't select text. Drag to select (it stays inside a pane when it starts in one), let go and it is on the clipboard; a key clears it.
- The 3D school grew a town: the finished kids go across to a fenced **park** and really play — swings, the slide (up the ladder, down the ramp), seesaws, the merry-go-round, the sandbox, football passing the ball between them (no ball rolling on its own any more) — and tag in the corner; nobody sleeps. Kids wear a cap in their class's colour. The teacher has a day: coffee and an apple at her desk, writing on the board, looking out of the window, watering the plant, checking the kids' work. Around: the school building with its sign, houses, a road with cars, trees, flowers, clouds, lamps that light up at night.
- Performance: a frame where only spinners move no longer draws the screen again (placeholders swapped into the last frame); Monitor items that are simply up show a steady ● instead of dozens of spinners; token usage is read without decoding whole multi-megabyte transcript lines (same totals, checked on 700 real transcripts); open ports' owners are only looked up again when the listening sockets change. Idle CPU went from ~10% to ~4%, terminal output in the Monitor from 42 to 17 KiB/s. The 3D page shares geometries and materials, instances its trees and freezes what doesn't move.

- The terminal Nursery is gone. In its place, a **3D classroom in the browser** (`9`, prefix `a`, or the ≡ menu): nanny serves it on localhost behind a random token, with three.js built in. Each agent is a teacher in her own room writing her lesson on the board, waving when she needs you, having a coffee when idle; each subagent is a kid that walks to the bookshelf, the computers, the lab or the globe by the tool it uses and back to its desk to think. Newcomers come in through the door, finished ones raise a hand and go out to the yard to play, and fall asleep later. Sky and lights follow the hour; click anyone for what they do, drag to look around, follow them with the camera.
- Live: subagents that finished a while ago fold into one “✓ N finished subagents” row under their agent (⏎ or a click unfolds them), and the agent's detail lists every finished one, newest first.

- The Nursery is a little video game now: a lab classroom with the teacher (the agent) pacing in front of her board, writing on it, and a desk per kid (subagent). Kids get up and walk to the corner of their tool — bookshelf to read or search, computers to run commands, lab bench to write, globe for the web, the teacher to hand her a note — work there and walk back to their chair. Newcomers come in through the door while her note flies to their desk; finished kids run out to recess, where they play and later fall asleep. Click a kid or a desk to pick it, double-click to read it. It moved off the views bar into the ≡ menu (9 and prefix a still open it).
- In a space, the views bar no longer repeats its name (it is on the top bar).

- Sidebar: “+ new · ⌕ discover · ≡ menu” on one line, pinned just above AGENTS (icons only when it is narrow).

- **≡ menu** (sidebar button under the spaces, the ◆ logo, `m`, or prefix `m` in a space): Settings, keys, theme, and submenus to pick the density (minimal · normal · full), which views sit on the bar, and features (sidebar, sidebar agents, cost, clock, sounds, popups, desktop notifications, each alert). Submenus stay open so you can switch several things in a row. Also reload config, rescan, manage/discover spaces, detach and quit-and-stop. Settings leaves the views bar (8 still opens it).
- Monitor: Tilt resources that are updating show as running, not "needs you". The counts on top are clickable (a state jumps to its next item, ports/containers open their section), and ⏎ on something with nothing to open goes to its detail instead of doing nothing.
- The quiet header's counts are clickable like the full one's; the Nursery's "! N need you" jumps to the next one.

- Branches say how they are: `⎇ main ●3 ↑2 ↓1` (changed files, commits to push, commits to pull;
  `✓` when there is nothing to do) under each space, on Live rows (only when there is something)
  and in details. Read every 20s, no network.
- Open pull requests are detected with `gh` (every 3 minutes, per repo): `#96✓✔` (checks ✓ ✗ ◌,
  approved ✔, changes requested ✎, drafts dimmed) under the space (click opens it), on the agent's
  Live row, on worktrees in the sidebar and the Worktrees view, and in words in the details. `O` in
  Live opens the agent's PR.
- The space and worktree menus open the PR, pull (fast-forward, when behind) and fetch.
- Faster and lighter: refreshes that come back unchanged (subagents, transcripts) no longer rebuild
  the views or redraw the screen; frames drop to one a second when nothing on screen moves (and to
  ~5/s with spinners); a subagent's last activity is read again only when its file changes. On a
  real setup (800 sessions, 365 subagents) drawing time fell by half.
- `NANNY_CPUPROFILE=file nanny` writes a CPU profile, for chasing slowness.
- Fix: reopening nanny no longer types junk into your shells. Reattached panes replay their old
  output, and the terminal's answers to the queries in it (cursor position, colours, device
  attributes) could drain after the replay and reach the program as typed text.
- Inside a space, Live, Sessions, Worktrees and Tasks show only that space; their titles say what
  they show (`all spaces`, `only checkout-service`, `all of ~`). A space opened on your home narrows
  to the repos its panes are in (cd into one and the views follow, worktrees of that repo
  included); until then it shows everything and says so.
- Nursery joins the global views on the right of the views bar (with Prompts, Skills, Monitor and
  Settings): those show the same everywhere, also inside a space.
- In normal density the sidebar lists every agent again outside Live (it only leaves them out
  while Live is on screen); minimal keeps only the busy ones.
- Settings → Screen: agents in the sidebar (auto · all · off), today's cost and the clock in the
  top bar, and which views sit on the views bar (Nursery, Prompts, Tasks, Skills; a hidden one
  still opens on its number). In the config: `[screen]` with `hide_cost`, `hide_clock`,
  `sidebar_agents` and `hide_views`.
- Below full, the top bar leaves out counts of nothing (no more "0 idle").
- Settings scrolls when it doesn't fit, keeping the help of the selected one below.
- Less noise by default. `M` (and Settings → On screen) walks three levels: **full** (everything),
  **normal** (the new default: nothing shown twice) and **minimal** (the old basic mode; `basic = true`
  still means it). In normal the sidebar leaves agents to Live while Live is on screen and elsewhere
  lists only the busy ones (plus a line for the idle), footers keep 4 keys (the rest is in `?`), and
  Live rows drop the model and show the context gauge only once it passes 60%.
- The views bar puts the views of the space (Live, Sessions, Worktrees, Tasks, Nursery) on the left,
  next to the space badge, and the ones that are the same everywhere (Prompts, Skills, Monitor,
  Settings) apart on the right. Numbers don't change.
- The Nursery is a classroom again: the teacher stands by her board (handing out work, teaching,
  asking for you, on a coffee break), each kid at work sits at a desk with the toy of its tool,
  and the ones that finished go to the playground — playing at first, asleep after 20 minutes.
  Every kid can be clicked or reached with `←→`, desks and playground scroll to the selected one.
- The sidebar splits in two halves that scroll on their own: spaces on top, agents below. Under
  a space, its branch line (`▸`/`▾`) shows or hides its worktrees; `+N more` lists them all.
- The branch and worktrees under a space follow where its active shell is (`cd` into a repo).
- The counts on top (needs you · done · working · subs · idle) are clickable: Live with only
  those agents; click again or `esc` to see them all.
- A space's terminal tabs sit one row lower, apart from the views bar; the active one is lit
  whole on the accent color (before, only up to its state glyph). `+ claude` and `+ shell` are
  buttons, set apart from the tabs. The Monitor's sections bar moves down the same way and
  lights up under the pointer, like every tab bar now.
- With the sidebar hidden (`B`), the footer says `B show sidebar`.
- The lit tab (terminal tab, Monitor section) is orange now, not the accent purple. A blank row
  separates the top bar from the views bar. "0 terminal" is always first in the views bar
  (dimmed in all spaces, where it opens the first space), so the bar no longer shifts.
- Themes: no two state colors alike (dracula, rose-pine, gruvbox, everforest, nord, one-light,
  ayu fixed), readable text on lit and hovered tabs (catppuccin-latte, one-dark,
  solarized-dark); the theme test checks both.
- A blank row under the views bar too, so lists and panels don't touch it. The sidebar's
  sections have a colored heading with a rule, agents are spaced apart, and the agents scroll
  with the wheel when they don't fit (↑/↓ N more).
- The harness in Skills (`t` cycles to it): hooks (event, matcher, command), MCP servers
  (from `~/.claude.json` and each repo's `.mcp.json`), plugins (on/off), settings files (model,
  allow/deny/ask rules, env names) and CLAUDE.md memory, global and per repo; `⏎` edits the
  file it is set in. Values of env vars, headers and key-like arguments are never shown.
- Sidebar spaces show their branch and worktree count; the space you are in lists its
  worktrees (dirty files ●N). Click or right-click one: Claude here, shell here, review
  changes, copy folder. Right-click a space → New worktree… makes a branch + worktree of its
  repo and opens Claude in it, in the same space.
- Skills `n` asks what to create (skill, slash command, subagent) and where (global
  `~/.claude`, the repo of any of your spaces, or another folder), then its name.
- An agent that hasn't been asked anything yet (an SDK run, a fresh claude) shows its process
  name instead of a blank row.
- Monitor: containers and the new **Tilt** section have actions (`⏎` or right-click): logs,
  shell inside, stats, restart, stop/start, remove; Tilt rebuild, enable/disable, Tilt UI.
  Stopped containers are listed too.
- Logs open in read-only panes: typing does nothing, `s` opens a shell inside the container
  next to it, `q` closes it.

- Mouse, more: hovering a terminal tab shows what is in it (each pane's program, folder, branch,
  Claude session and context); hovering a space or a view says what it is. Right-click opens a
  menu on terminal tabs (rename, split, zoom, move, review, copy folder, close), panes, spaces
  (open, views, rename, icon, move, add folder, delete) and list rows (the row's actions). Drag
  tabs and spaces to reorder them, double-click a tab to rename it, middle-click to close it.
- Spaces can have an icon: right-click → Icon… picks one from a grid or takes any emoji.
- Renaming a tab or a space starts from its current name.
- "home" is now "all spaces" (`⊞ all` in the top bar).
- Welcome guide on the first start (four pages: what nanny is, the views, spaces and panes, mouse
  and handy keys); `ctrl+k` → Welcome guide shows it again. `NANNY_NO_WELCOME=1` skips it.
- Radar is now the **Monitor**, in sections (All · Machine · Ports · Containers · Jobs · Cron ·
  Hermes · Watchers) on a bar of their own, `← →` or a click to move. A section whose source is
  off says so and `space` turns it on — before, with `system`/`hermes` off the tab only showed
  Today and never said why. New: This machine (load, memory, disk, Claude's processes), Open
  ports (`⏎` opens them in the browser), Containers (Docker, `L` logs), systemd user timers, and
  every crontab entry (not only agent ones) with its schedule in words. New `sources.machine`
  setting (on by default).
- Removed: the palette's "Prompts…" and "Task queue…" (same as Go to Prompts / Tasks) and the
  sidebar's "spaces" dot. Empty-state hints pointed at Settings as `6`; it is `8`.

- Tasks and Prompts are tabs now (`4`, `5`), next to Live, Sessions and Worktrees; Skills, Radar,
  Settings and Nursery move to `6`-`9`.
- The view bar stays in every space, scoped to its folders: prefix `i` (or a click) shows the
  space's Live, Sessions, Tasks…, `0` / `esc` go back to its terminal. Home still shows everything.
- A space's terminal tabs sit right over its panes (not under the sidebar), with `+ claude` and
  `+ shell` buttons and the folder and branch on the right.
- Mouse: every key hint on screen is clickable (footer, empty states, the sidebar, popups) and
  presses its key; inside a space they run as prefix commands so nothing is typed into the pane.
- Fix: a Claude pane that quits (declining to trust a new folder, `/exit`) no longer sits frozen on
  its last screen: it becomes a shell in the same folder.

- The Arcade is now the Nursery: clean cards instead of pixel sprites. The agent is the teacher
  card, each running subagent a kid card with an emoji for its tool, finished ones listed at recess.
- Context & cost meter: Live rows (full and basic) show a context gauge (`▰▰▰▱▱ 62%`) and the
  estimated cost (`≈$0.84`, subagents included; the cost is dropped first on narrow terminals).
  The Live and Sessions details get a Usage block (context used / window, tokens in / out /
  cache, session and subagent cost, model), the full-mode header shows today's estimated spend,
  and an agent crossing 85% context raises one "near auto-compact" alert. Tokens come from
  `message.usage` in the transcripts, read incrementally; prices per model family default to
  the current list prices and can be overridden with `[prices.<family>]` in the config.

- Task queue (`u`, prefix `U`, palette "Task queue…"): a backlog of prompts nanny hands to Claude
  agents. Modes `here` (an idle nanny agent in the folder, never one blocked on a prompt), `new`
  (a claude tab) and `worktree` (a fresh git worktree, then claude). Auto-dispatch (`a`) starts
  pending tasks in order, up to `queue.max_parallel` at once (default 2, also in Settings), without
  pulling you into the space; a task is done when its agent worked and went idle. Pending / Running
  / Done view with add, edit, remove, reorder, run now, retry, go to agent and clear; `q` in the
  prompt library and `+` in Live queue a task; `☰ N queued` header badge; alerts when a task ends
  and when the queue empties. Stored in `$XDG_STATE_HOME/nanny/queue.json`.
- Prompt boxes (`p`, broadcast, new task, queue form): `alt+1-9` adds a top saved prompt, `ctrl+r`
  cycles the folder's most used prompts that contain what you typed. `.` in Live sends an idle
  agent its last prompt again.
- The Arcade is now the Nursery: the agent is a teacher writing on a blackboard, subagents are
  kids at desks playing with the toy of their current tool, finished ones go to recess.
- Basic mode (`M`, prefix `M`, Settings → Look, `basic = true`): one line per agent with its
  subagent count, no detail pane, three tabs, a quiet top bar and fewer footer hints.
- The UI speaks English, Spanish, French and German. `language = "auto"` (the default) follows
  `$LC_ALL` / `$LC_MESSAGES` / `$LANG` and falls back to English; Settings → Look → Language
  switches it live. Translations live in `internal/i18n`, and a test fails when a string used in
  the code is missing from any language.
- Prompt library (`P`, prefix `P`, palette "Prompts…"): saved prompts with tags, pins and use
  counts, plus the prompts you type most, mined from `~/.claude/history.jsonl` (near-repeats
  grouped, ranked by count and recency, per project). Filter, preview, insert into a pane or a
  Live agent, `{placeholders}` filled on insert, save/new/edit/delete/pin/copy. Use counts live
  in `$XDG_STATE_HOME/nanny/prompts.json`; the palette ranks snippets by use.
- Arcade (tab `7`, prefix `a` inside a space, palette "Arcade"): live agents as an 8-bit game.
  Captains with per-state sprites and speech bubbles, subagents as typed minions that warp in,
  wave "+XP" and walk off when done, a SCORE / LIVES / needs-you HUD, mouse selection, and a
  compact one-line mode for small terminals.

- Detach / reattach: panes run under a per-pane holder (`nanny __hold`) and survive
  quitting nanny; the next start reattaches them with their recent output. `prefix Q`
  (or `k` in the quit dialog) quits and stops every pane. New `nanny ps` and
  `nanny kill-server`; `ps` is no longer an alias of `live`.

- Spaces → tabs → split panes with real terminals (pty + VT emulator), restore on start
  (Claude panes resume their session), prefix commands, alt+number space switching.
- Discover: find every place you work in (agent sessions, git repos, open workspaces).
- Live agents with subagent trees, needs-you detection from the screen, full-screen
  subagent/session viewer, send prompts.
- Alerts: sounds, popups, desktop notifications, inbox, unseen "done" state, mute.
- Sessions explorer with fuzzy filter and full-text search; worktrees dashboard;
  skills/commands/agents catalog with enable/disable, new, copy-to-project.
- Radar: Hermes, Claude background jobs, cron, loops, opencode, custom watchers, Today summary.
- CLI: ls, where, resume, search, live, shell-init, demo.
