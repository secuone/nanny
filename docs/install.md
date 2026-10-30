# Install & compatibility

[← back to README](../README.md)

- [Requirements and compatibility](#requirements-and-compatibility)
- [Install](#install)
- [First run](#first-run)
- [Update](#update)
- [Uninstall](#uninstall)
- [Where nanny keeps its files](#where-nanny-keeps-its-files)

## Requirements and compatibility

**Operating system.** Linux, on amd64 and arm64 (the release workflow builds `linux` and `darwin` for both, see below).

- nanny finds live agents, panes' foreground programs and the Monitor's machine and port data through `/proc`, and refreshes on change through inotify (`golang.org/x/sys/unix`, `IN_*`). Neither exists on macOS.
- **macOS does not build today**: `GOOS=darwin go build ./cmd/nanny` fails in `internal/claude/watch.go` (inotify), and process detection would need a `/proc` replacement. A few spots already know macOS (`afplay` for sounds, `osascript` for desktop notifications), but the port is not done. Until it is, the install script refuses to run on macOS.
- **Windows is not supported** (the code uses unix ptys, unix sockets and `syscall.Exec`). WSL2 is a Linux, so it should work; it has not been tried.
- Without inotify (or when it runs out of watches) nanny falls back to scanning on a timer (`refresh_secs`); `NANNY_NO_WATCH=1` forces that.

**Build from source.** Go 1.26 or newer (`go.mod` says `go 1.26.0`, needed by Bubble Tea v2). A Go between 1.21 and 1.25 downloads the 1.26 toolchain by itself unless `GOTOOLCHAIN=local`.

**Release builds.** `.goreleaser.yaml` builds `linux` and `darwin` × `amd64` and `arm64` with `CGO_ENABLED=0`, as `nanny_<version>_<os>_<arch>.tar.gz` plus `checksums.txt`. Nothing has been tagged yet, so there are no release assets; and while macOS does not build, a tagged release would fail on the `darwin` targets.

**Terminal.** nanny draws with Bubble Tea v2, which on start asks the terminal for:

- the kitty keyboard protocol and `modifyOtherKeys` (so `ctrl+q`, `ctrl+space` and `alt+arrows` arrive as themselves), and
- synchronized output (mode 2026, so frames don't tear).

Terminals that lack them ignore the requests and nanny keeps working, with the keys the terminal can send. Truecolor and UTF-8 are recommended; the themes use 24-bit colors, and on the Linux console or a non-UTF-8 locale the icons switch to ASCII (`[look] icons = "auto"`). Programs inside panes see `TERM=xterm-256color` and `COLORTERM=truecolor`. No terminal has been tested systematically; nothing in nanny is specific to one.

- **tmux, screen, zellij**: inside one, `ctrl+b` is probably taken, so nanny says so on start; change `prefix` (for example `ctrl+a` or `ctrl+g`). Nesting is otherwise not special-cased.
- **ssh**: nanny is a terminal program and runs remotely like any other. Copying uses `wl-copy`, `xclip` or `xsel` when present and falls back to OSC 52, which your terminal must allow. Sound and desktop notifications happen on the machine nanny runs on.

**Fonts and icons.** No special font is required: the default icon set is plain Unicode. `[look] icons = "nerd"` swaps some glyphs (branches, checks, warnings, keys) for Nerd Font ones, so use it only when your terminal font is a [Nerd Font](https://www.nerdfonts.com/). `"ascii"` needs nothing. The font is your terminal's setting, not nanny's.

**Optional tools.** None of these is needed to start; each feature says what is missing and stays quiet.

| Tool | Used for |
| --- | --- |
| `git` | worktrees, branches, diffs (required for most of nanny) |
| `claude` | starting and resuming agents from nanny, `nanny resume`, `nanny new` |
| `gh` | Git: your PRs, reviews, merge, PR badges on branches (`gh auth login` once) |
| Jira Cloud API token | the Jira view; kept in the keyring (`secret-tool`) or a `0600` file, or `NANNY_JIRA_TOKEN` |
| `python3` | reading Hermes and opencode SQLite databases (no cgo) |
| `docker`, `tilt` | Monitor: containers and Tilt resources, over their own sockets and APIs; the commands are used when there is no socket or config |
| `crontab`, `systemctl` | Monitor: cron entries and systemd user timers |
| `pw-play`, `paplay`, `afplay`, `canberra-gtk-play`, `ffplay`, `aplay` | alert sounds (the first found; the terminal bell otherwise) |
| `notify-send` (Linux), `osascript` | desktop notifications (`[alerts] desktop = true`) |
| `wl-copy`, `xclip`, `xsel` | the clipboard (OSC 52 otherwise) |
| `xdg-open` (`open`) | opening PRs, Jira issues and the 3D classroom in the browser |
| `secret-tool` | storing the Jira token in the desktop keyring |

## Install

**Install script.**

```sh
curl -fsSL https://raw.githubusercontent.com/secuone/nanny/main/scripts/install.sh | sh
```

It installs to `~/.local/bin` (`BIN_DIR=/some/dir` changes it) and tells you if that is not on your `PATH`. It downloads the latest release when there is one for your platform. **There is no release yet**, so today it tells you so and builds the `main` branch instead, which needs `git` and Go 1.26+ and takes about a minute. It refuses to run on anything but Linux, and on architectures other than amd64 and arm64.

**From a clone.**

```sh
git clone https://github.com/secuone/nanny && cd nanny
make install                  # builds bin/nanny and copies it to ~/.local/bin/nanny
make install PREFIX=/usr/local   # → /usr/local/bin/nanny (may need sudo)
```

`make build` only builds `./bin/nanny`; `make run` builds and starts it.

**`go install` does not work.** `go install github.com/secuone/nanny/cmd/nanny@latest` is rejected by Go because `go.mod` has a `replace` directive (`third_party/vt`, a vendored terminal emulator). Use one of the two ways above.

**Shell helpers.** `sp <query>` goes to the folder of a past session, `spr <query>` resumes it:

```sh
echo 'eval "$(nanny shell-init zsh)"'  >> ~/.zshrc       # zsh
echo 'eval "$(nanny shell-init bash)"' >> ~/.bashrc      # bash
echo 'nanny shell-init fish | source'  >> ~/.config/fish/config.fish
```

## First run

1. `nanny demo` opens nanny on a throwaway `$HOME` in `$TMPDIR/nanny-demo` with fake sessions, subagents and worktrees. Nothing real is read or written; its panes are stopped and its files recreated on the next `nanny demo`.
2. `nanny` creates `~/.config/nanny/config.toml`, shows a short welcome guide (`ctrl+k` → Welcome guide brings it back), and then a setup wizard that connects Jira and checks `gh`. Skip it with `esc`; it comes back from Settings → Connections, the `≡` menu and the palette. `NANNY_NO_WELCOME=1` skips both.
3. Press `D` to discover your projects and tick the ones you want as spaces.
4. To leave: `ctrl+q` from anywhere (it asks first), or `q` from a view, or prefix `q` in a space. Panes keep running in the background; `nanny` again reattaches them. `k` in the dialog (or prefix `Q`) quits and stops every pane. `nanny kill-server` stops them from the shell.

## Update

Run the install script again, or from a clone:

```sh
git pull && make install
```

Panes keep running while you replace the binary. Quit and start nanny again to use the new one; the holders (`nanny __hold`) of running panes stay on the old binary until their program exits. `nanny version` says what you have.

## Uninstall

```sh
nanny kill-server                 # stop the background panes
rm ~/.local/bin/nanny             # or: make uninstall (PREFIX=… if you set it)
```

Then, to remove everything nanny wrote:

```sh
rm -r ~/.config/nanny ~/.local/state/nanny
rm -rf "$XDG_RUNTIME_DIR/nanny"         # empty once the panes are stopped
secret-tool clear service nanny-jira    # only if you stored a Jira token in the keyring
```

Remove the `eval "$(nanny shell-init …)"` line from your shell rc too.

## Where nanny keeps its files

| What | Where |
| --- | --- |
| Config | `$XDG_CONFIG_HOME/nanny/config.toml` (default `~/.config/nanny/`); `nanny config` prints the path |
| Saved prompts, your themes | `snippets.toml` and `themes/*.toml` next to the config |
| Jira token (when there is no keyring) | `secrets/` next to the config, folder `0700`, file `0600` |
| Layout of spaces and tabs | `$XDG_STATE_HOME/nanny/workspace.json` (default `~/.local/state/nanny/`) |
| Task queue, prompt use counts | `queue.json` and `prompts.json` in the same state folder |
| Insights cache | `insights/v1.gob` in the state folder |
| First-run markers | `welcomed` and `setup-done` in the state folder |
| Pane sockets (one per background pane) | `$XDG_RUNTIME_DIR/nanny/`, else `<state folder>/sock/`; `NANNY_SOCK_DIR` overrides |
| Debug log (`NANNY_DEBUG=1`) | `debug.log` in the state folder |

To try nanny without touching your real files, point `XDG_CONFIG_HOME`, `XDG_STATE_HOME`, `XDG_RUNTIME_DIR` (and `NANNY_SOCK_DIR`) at an empty folder, or use `nanny demo`. Environment variables are listed in [configuration.md](configuration.md#environment-variables).
