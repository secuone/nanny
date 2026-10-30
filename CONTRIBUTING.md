# Contributing to nanny

Thanks for helping! A few ground rules keep nanny fast and trustworthy.

## Principles

- **Read, don't write.** nanny observes other tools' state (`~/.claude`, `~/.hermes`, git).
  The only things it writes are its own config/layout and actions the user explicitly triggers.
- **Cheap refreshes.** Anything that runs every tick must be cached by (size, mtime) or be O(1).
  Big transcripts are read from the tail; never scan whole files on a timer.
- **No private data in tests or docs.** Use `internal/demo` (the fixture home) for tests,
  screenshots and examples.

## Workflow

```sh
make test   # unit + UI render tests (fixture home, real ptys)
make lint   # vet + gofmt
make demo   # try your change on fake data
```

UI changes: `TestViewsFitTerminal` renders every view at 150×42, 100×30 and 80×24 and fails
if any line overflows. Keep it green.

## Adding a Radar source

Add a function returning `[]sources.Item` in `internal/sources`, call it from `Collect`, and
give it a `Group`. Items with `SessionID` resume a Claude session on ⏎; `Cmd` runs a
command; `LogPath` gets a live tail.

## Commit style

Conventional commits (`feat:`, `fix:`, `docs:` …), small and focused.
