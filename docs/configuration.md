# Configuration

[← back to README](../README.md)

- [The config file](#the-config-file)
- [Annotated example](#annotated-example)
- [Themes](#themes)
- [Icons, borders, fonts](#icons-borders-fonts)
- [Screen profiles](#screen-profiles)
- [Saved prompts](#saved-prompts)
- [State files](#state-files)
- [Environment variables](#environment-variables)
- [Git, Jira and Insights](#git-jira-and-insights)

## The config file

`~/.config/nanny/config.toml` is created on first run. Everything in it can also be
edited from the **Settings** tab. Every key is optional. The full list, with comments, is in
[`config.example.toml`](config.example.toml).

`T` (or prefix `T` inside a space, or "Theme…" in the
palette) opens the theme picker: it previews each theme as you move, `⏎` keeps it, `esc`
reverts. Light themes are meant for terminals with a light background.

## Annotated example

```toml
theme = "nanny"              # dark: catppuccin-mocha, tokyo-night, dracula, nord, gruvbox, rose-pine, kanagawa, one-dark, solarized-dark, everforest, github-dark, monokai, ayu, high-contrast
                             # light: nanny-light, catppuccin-latte, one-light, solarized-light, github-light
language = "auto"            # en, es, fr, de; auto follows $LC_ALL / $LC_MESSAGES / $LANG (English if unsupported)
prefix = "ctrl+b"            # pane command prefix
quit_key = "ctrl+q"          # asks to quit from anywhere, panes too; "none" turns it off (emacs, micro)
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

Cost estimates use list prices per model family. Override them with `[prices.<family>]`
(`opus`, `sonnet`, `haiku`, `fable`); see [`config.example.toml`](config.example.toml).

## Themes

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

## Icons, borders, fonts

`a` in the theme picker (or "Appearance…" in the ≡ menu) picks
the icon set — `unicode`, `nerd` (Nerd Font icons for branches, checks, warnings, keys…) or
`ascii` (plain ASCII everywhere; `auto` picks it on the Linux console or a non-UTF-8
locale) — the borders (rounded, square, thick, double, ascii, none), bold/italic/underline
on or off, and the font of the 3D page. What a program prints in a pane is never changed.
The terminal's own font is the terminal's: nanny can't change it reliably (xterm's OSC 50
is the only escape for it and Ghostty, kitty, WezTerm or GNOME Terminal ignore it), so set it
in your terminal.

## Screen profiles

`profile = "…"` picks the one in use; `[profiles.<name>] off = […]` defines yours. The ids a
profile can turn off are listed in the example above. Profiles are made and walked from
Customize (`=`) and with `M`; see [features.md](features.md#screen-profiles-and-customize).

## Saved prompts

Saved prompts live in `~/.config/nanny/snippets.toml`:

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

## State files

Layout state lives in `~/.local/state/nanny/workspace.json`, the task queue in `queue.json`
next to it (`$XDG_STATE_HOME/nanny/queue.json`).

## Environment variables

- `NANNY_FPS`: maximum screen flushes per second (default 30).
- `NANNY_DEBUG=1`: logs what the file watch does to `~/.local/state/nanny/debug.log`.
- `NANNY_CPUPROFILE=file`: writes a CPU profile.

## Git, Jira and Insights

<!-- Git view -->

<!-- Jira view and setup wizard -->

<!-- Insights view -->
