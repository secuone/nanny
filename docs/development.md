# Development

[← back to README](../README.md)

- [Build and run](#build-and-run)
- [Tests](#tests)
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
make install  # → ~/.local/bin/nanny
```

## Tests

`make test` runs `go test ./...` on a generated fixture home (`internal/demo`) and real ptys.
`TestViewsFitTerminal` renders every view at 150×42, 100×30 and 80×24 and fails if any line
overflows. CI (`.github/workflows/ci.yml`) runs `make lint`, `go test ./...` and `make build`.

## Packages

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

## Adding a Monitor source

Add a function returning `[]sources.Item` in `internal/sources`, call it from `Collect`, and
give it a `Group`. Items with `SessionID` resume a Claude session on ⏎; `Cmd` runs a
command; `LogPath` gets a live tail.

## Release

Pushing a `v*` tag runs GoReleaser (`.github/workflows/release.yml`) and publishes the release. `make snapshot` runs a
local cross-compiled release (`goreleaser release --snapshot --clean`). The install script
(`scripts/install.sh`) downloads the release binary and falls back to `go install`.

## Demo GIF

The README can show `docs/demo.gif`, recorded from the script `docs/demo.tape` with
[vhs](https://github.com/charmbracelet/vhs):

```sh
vhs docs/demo.tape      # writes docs/demo.gif
```

TODO: record the GIF, commit it, and uncomment the `<img>` line in the README header.

## Roadmap ideas

- detach/attach with a background server
- Codex, Gemini and opencode as first-class live agents
- per-session cost charts
- session tags and pins
