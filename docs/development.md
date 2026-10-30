# Development

[← back to README](../README.md)

- [Build and run](#build-and-run)
- [Tests](#tests)
- [Measuring the screen](#measuring-the-screen)
- [Packages](#packages)
- [Adding a Monitor source](#adding-a-monitor-source)
- [Release](#release)
- [Demo GIF](#demo-gif)
- [Roadmap ideas](#roadmap-ideas)

Ground rules and commit style are in [CONTRIBUTING.md](../CONTRIBUTING.md).

## Build and run

```sh
make run      # build + launch
make demo     # launch on the fake $HOME
make test     # tests use a generated fixture home and real ptys
make lint     # vet + gofmt
make build    # ./bin/nanny
make install  # → ~/.local/bin/nanny (PREFIX=… changes it)
make snapshot # cross-compiled release in dist/ (needs goreleaser)
```

## Tests

`make test` runs `go test ./...` on a generated fixture home (`internal/demo`) and real ptys.
`TestViewsFitTerminal` renders every view at 150×42, 100×30 and 80×24 and fails if any line
overflows. CI (`.github/workflows/ci.yml`) runs `make lint`, `go test ./...` and `make build`.

Building needs Go 1.26 (Bubble Tea v2) and Linux: `internal/claude/watch.go` uses inotify, so `GOOS=darwin` does not compile yet
(see [install.md](install.md#requirements-and-compatibility)). Key labels in code and tests go through `keyPress("ctrl+b")`,
and `keyBytes` turns a key press into what a terminal would send to a pane (pinned by `TestKeyBytes`).

## Measuring the screen

`NANNY_OUTSTATS=<file> nanny` logs the bytes, writes, KiB/s and bytes per write that nanny sends to the
terminal every 5 s. `NANNY_DUMP=<dir>` makes the render tests write every view they draw. `NANNY_BYTES=1 go test ./internal/ui -run TestOutBytes -v` measures the same with
10 panes and one printing, inside a real `tea.Program`. `NANNY_CPUPROFILE=<file>` writes a CPU profile
(`NANNY_CPUPROFILE_AFTER=10s` leaves the start-up out). Run them against `nanny demo`, not your own state.

## Packages

| Package | What it holds |
| --- | --- |
| `cmd/nanny` | CLI: subcommands, `insights`, `demo` |
| `internal/term` | pty + emulator panes, the holder process and its sockets |
| `internal/ws` | spaces, tabs, persistence, process tree |
| `internal/claude` | sessions, subagents, search, matching, usage and cost, the inotify watcher |
| `internal/skills` | skills catalog |
| `internal/prompts` | most used prompts from history, placeholders |
| `internal/discover` | Discover |
| `internal/gitwt` | worktrees, branches, PRs through `gh` |
| `internal/gh` | GitHub data for Git & PRs (My PRs, Reviews, Profile) |
| `internal/jira` | Jira Cloud client |
| `internal/secret` | the Jira token: keyring or a private file |
| `internal/insights` | transcript index, weekly aggregates, JSON export |
| `internal/queue` | the task queue |
| `internal/sources` | Monitor |
| `internal/classroom` | the 3D classroom page |
| `internal/config` | config file, themes, profiles, saved prompts |
| `internal/ui` | Bubble Tea views |
| `internal/i18n` | UI translations (`T`, `Tf`, `Tn`), one table per language |
| `internal/demo` | fixture home |
| `third_party/vt` | vendored terminal emulator (`x/vt`) with a smaller parser buffer; this `replace` is why `go install …@latest` doesn't work |

## Adding a Monitor source

Add a function returning `[]sources.Item` in `internal/sources`, call it from `Collect`, and
give it a `Group`. Items with `SessionID` resume a Claude session on ⏎; `Cmd` runs a
command; `LogPath` gets a live tail.

## Release

Pushing a `v*` tag runs GoReleaser (`.github/workflows/release.yml`, config in `.goreleaser.yaml`): `linux` and `darwin` on `amd64` and
`arm64`, as `nanny_<version>_<os>_<arch>.tar.gz` and `checksums.txt`; `docs:`, `test:` and `chore:` commits stay out of the notes.
`make snapshot` runs it locally (`goreleaser release --snapshot --clean`). The install script (`scripts/install.sh`) downloads that
asset for `linux` and falls back to cloning `main` and building it. No tag exists yet, and the `darwin` targets fail until macOS builds.

## Demo GIF

The README shows `docs/demo.gif`, recorded from `docs/demo.tape` with
[vhs](https://github.com/charmbracelet/vhs) and `ffmpeg`:

```sh
vhs docs/demo.tape      # writes docs/demo.gif (plays too fast, see below)
ffmpeg -y -i docs/demo.gif -vf "setpts=1.4*PTS,fps=12,split[a][b];[a]palettegen=max_colors=96:stats_mode=diff[p];[b][p]paletteuse=dither=none:diff_mode=rectangle" docs/demo.gif
```

vhs captures frames slower than real time, so the raw GIF is shorter than the tape and
looks sped up (nanny reacts to every key). The low `Set Framerate` keeps capture stable
and the `setpts` step stretches it back to about 22 s. Run vhs with a throwaway
`XDG_CONFIG_HOME`, `XDG_STATE_HOME` and `XDG_RUNTIME_DIR`, and keep the Monitor on its
Machine section: other sections list the host's real ports, paths and cron jobs.

## Roadmap ideas

- macOS support (inotify and `/proc` replacements)
- Codex, Gemini and opencode as first-class live agents
- per-session cost charts
- session tags and pins
