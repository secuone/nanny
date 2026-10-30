# Keys

[← back to README](../README.md)

- [Inside a space (prefix)](#inside-a-space-prefix)
- [Views](#views)
- [Same key in every list](#same-key-in-every-list)
- [View extras](#view-extras)
- [Prompt library](#prompt-library)
- [Task queue](#task-queue)
- [Answer box](#answer-box)
- [Changes view](#changes-view)

Press `?` in nanny for the full list for the screen you are on.

## Inside a space (prefix)

Prefix (`ctrl+b`, configurable with `prefix` in the config) then:

| Key | Action |
| --- | --- |
| `c` / `s` | new Claude tab / new shell tab |
| `C` / `g` | new task (folder, prompt, worktree) / review this pane's changes |
| `G` | this space's Git: branch, PR and worktrees (the Overview section) |
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
| `m` | the ≡ menu |
| `q` / `Q` | quit, panes keep running (reattach on next start) / quit and stop every pane |
| `ctrl+q` | the same as `q`, without the prefix (see below) |
| `ctrl+b` `ctrl+b` | send the prefix key to the program |

`alt+1-9`, `alt+←/→` and `alt+0` switch spaces without the prefix. `alt+↑/↓` goes to the
previous / next agent in the sidebar.

`ctrl+q` quits from anywhere, inside a pane too: it asks first (panes keep running; `k` quits and stops
them), and `ctrl+q` again answers yes. It is shown as `^q quit` in the footer. A program in a pane that
needs `ctrl+q` (emacs, micro) can have it back with `quit_key = "none"` in the config.

## Views

Everywhere (all spaces, or a space after prefix `i`):

| Key | Action |
| --- | --- |
| `1-8` | Live · Sessions · Git · Tasks · Prompts · Skills · Monitor · Settings |
| `E` / `I` | Jira / Insights |
| `9` | the 3D classroom |
| `tab` | next view |
| `0` | back to the space's terminal |
| `ctrl+k` | command palette |
| `P` / `u` | prompt library / task queue |
| `!` | next agent that needs you |
| `[` `]` / `\` | scope to a project / clear the scope |
| `B` | sidebar |
| `M` | next screen profile (Basic · Full · yours) |
| `=` | Customize |
| `A` / `S` | alerts / mute |
| `T` | themes |
| `W` / `N` / `D` | manage spaces / new space / discover places |
| `R` | rescan everything |
| `?` | the full list |
| `q` | quit (panes keep running) |
| `ctrl+q` | quit from anywhere (asks first; panes keep running) |
| `alt+↑/↓` | previous / next agent |

## Same key in every list

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

## View extras

- **Live**: `⏎` answers a waiting agent · `p` prompt · `space` pick agents then `p` sends to all ·
  `n` new task · `+` queue a task in its folder · `.` repeat its last prompt · `C` changes ·
  `s` screen mirror · `a` finished subagents · `O` open its PR.
- **Sessions**: `F` full-text search · `b` / `w` / `L` bots, worktrees, live.
- **Git**: sections `←→`. **Overview** (the default inside a space): branch, state and PR of the space and
  its worktrees; `⏎` go to it · `O` open its PR · `C` changes · `P` pull · `F` fetch · `n` new Claude in it ·
  `w` new worktree · `y` copy the branch · `r` refresh. **Worktrees**: `C` changes · `n` new Claude · `a` add ·
  `d` remove · `p` prune. Without `gh` signed in, the empty state offers `i` sign in (`gh auth login` in a tab) ·
  `s` setup wizard · `r` try again · `o` open cli.github.com when `gh` is missing. In a space, My PRs and Reviews show that repo's PRs; when there are none, `a` shows All spaces.
- **Jira** (`E`): sections `←→` (and `Search` once you have searched) · `/` filter as you type · `e` search all of Jira (words, or `jql:` and a query) · `f` filter by status (all, to do, in progress, done, then each status) · `o` sort (updated, priority, status) · `F` clear filter and search · `l` then `j`/`k` scroll the reader · `⏎` open in the browser · `t` transition · `c` comment · `a` assign · `y`/`Y` copy key/link · `O` its PR · `s` its agent · `v` its conversation · `n` start work (worktree and agent) · `b` only the branch.
- **Insights** (`I`): sections `←→` · `t` 7 / 30 / 90 days / today · `[` `]` scope to a project · `,` `.` move through weeks · `x` write the week as JSON · `y` copy it · `r` read everything again · `⏎` read an expensive session.
- **Skills**: `space` on/off · `n` new (skill, command or subagent; global or in a repo) ·
  `t` type, including the harness (hooks, MCP, plugins, CLAUDE.md, settings) ·
  `c` copy to a project · `H` hide disabled.
- **Monitor**: `← →` sections · `L` follow log; `=` customizes what shows.

## Prompt library

`⏎` insert · `/` filter · `J/K` Saved or Most used · `s` save · `n` new · `e` edit ·
`d` delete · `p` pin · `y` copy · `q` add to the task queue · `esc` back; in the editor `tab` field · `ctrl+s` save · `ctrl+e` `$EDITOR`.

## Task queue

`n` add · `e` edit · `d` remove · `J/K` reorder · `⏎` run now (running: go to it) · `a` auto-dispatch ·
`g` go to its agent · `r` retry · `c` clear finished · `esc` back. In any prompt box `alt+1-9` saved prompt · `ctrl+r` most used.

## Answer box

`y` yes · `a` always · `n` no · `1-9` option · `g` go to the pane · `esc` close.

## Changes view

`j/k` file · `l` into the diff (`h` back) · `space`/`b` page · `n`/`N` hunk · `t` uncommitted or
branch · `c` ask to commit · `w` open PR · `y` copy branch · `o` shell · `r` refresh · `esc` back.
