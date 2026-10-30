# How it works

[← back to README](../README.md)

- [Holders and sockets](#holders-and-sockets)
- [What nanny reads](#what-nanny-reads)
- [Refresh: event-driven](#refresh-event-driven)
- [Measured](#measured)
- [Privacy](#privacy)

## Holders and sockets

Each pane's pty is owned by a tiny holder process (`nanny __hold`, idle at 0% CPU) that
outlives nanny, like dtach. nanny talks to it over a unix socket in
`$XDG_RUNTIME_DIR/nanny/` (or `~/.local/state/nanny/sock/`): input, resizes and kill go
in, output comes out. The holder keeps the last ~2 MB of output and replays it into a
fresh screen when nanny reattaches; it exits when its program does. Sockets nobody
answers on are cleaned up.

## What nanny reads

nanny runs on Linux (it reads `/proc` and uses inotify). It renders each pane with an in-memory VT emulator, and *reads* what your tools
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
| `git worktree list`, `git status` | worktrees and branches |
| `gh` | your PRs, reviews and checks (only while the view or badge needs them) |
| the Jira REST API | the Jira view and the sidebar's issue line, only with a URL and token |

## Refresh: event-driven

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

## Measured

On a real machine (72 projects, 326 sessions, 824 MB of transcripts):

| Operation | Time |
| --- | --- |
| Cold scan | ≈ 350 ms |
| Rescan | ≈ 5 ms |
| 128 subagents (warm) | ≈ 7 ms |
| Full-text search | ≈ 0.6 s |

## Privacy

nanny has no telemetry and no daemon of its own. The only network calls are the ones you turn on: `gh` talks to GitHub and the Jira view to your Jira site. Transcripts are read locally and never leave the machine.
