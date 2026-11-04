# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
go run . -demo -port 7799          # synthetic data, three rooms in two working directories, no logs needed — use this for frontend work
go run . -port 7788                # watch every project under $HOME (Claude Code + Codex)
go test ./...                      # Go tests (scanner state folding, tailer, Codex rollout, end-to-end scan)
go test -run TestScanEndToEnd ./...
node --test web/*.test.mjs         # frontend nav A* + i18n language-resolution checks (stdlib node:test)
go vet ./...
```

`./start.sh` is the convenience launcher (scans all of `$HOME`, opens browser). There is no build step for the frontend: `web/` is plain ES modules served directly, three.js is vendored under `web/vendor/`. `go build -o agentwatch .` is the only build; `agentwatch` and `log/` are gitignored.

## Architecture

Single Go binary, stdlib only (`go.mod` has no dependencies). Every second the server scans local agent logs for the current user, folds them into one `Snapshot` JSON, and both `/api/snapshot` and the `/events` SSE stream serve that cached byte slice. The frontend receives snapshots and animates a Three.js isometric office — one room per session, one desk per agent, rooms grouped by working directory. The split:

- `scanner.go` — Claude Code source. Reads the `~/.claude/sessions/*.json` registry for liveness (`/proc/<pid>`) and, by default, only outputs sessions whose process is alive. With `-all` it also globs `~/.claude/projects/*/*.jsonl` for sessions modified within `-history`, including finished (`dead`) and history-only rooms; each room's `project` comes from the registry `cwd` or, for history-only sessions, the `cwd` field on log lines. It still reimplements Claude Code's path encoding (every non-alphanumeric byte → `-`, see `projectLogDir`) to locate a registered session's log. Then it incrementally tails each session jsonl and its `subagents/agent-*.jsonl`.
- `codex.go` — Codex source. Walks `~/.codex/sessions/**/rollout-*.jsonl`, groups each rollout by its first-line `session_meta.cwd`, reuses the same `tailer`. No process registry: `busy`/`idle` is inferred from the last event time, so `dead` and subagents don't exist here.
- `model.go` — the wire types (`Snapshot`/`Room`/`Agent`/`Msg`/`Event`).
- `main.go` — flags, merges both scanners via the `scanner` interface + `multiScanner`, sorts rooms by `startedAt`, serves HTTP.
- `demo.go` — `scanner` implementation for `-demo`; also the fastest way to exercise ghosts, stuck agents, and cross-room paper planes.
- `web/` — `main.js` applies each snapshot (rooms, actors, sidebar history), `scene.js` builds rooms/furniture/paper planes, `agents.js` loads the robot GLB and maps `state` → animation, `nav.js` is the ground A* grid, `i18n.js` holds every UI string in `zh` and `en`.

### State is incremental and process-lifetime

`tailer` + `agentLog`/`codexLog` keep per-file read offsets, turn/token counters, and event buffers in memory. State only advances on newly appended newline-terminated lines; a shrunk file resets to zero. Restarting the process replays everything from the start of each log. Time-dependent overlays (`stuck`, `done`/ghost removal) are recomputed on every scan in `finalize`, not when lines are read — so they must stay out of `apply`.

### The protocol is the contract

`PROTOCOL.md` is explicitly frozen. Any change to `model.go` fields, state names, ordering semantics, or the SSE frame format must be mirrored there and in `web/main.js` together. Notable invariants: `main` is the fixed main-agent id, arrays serialize as `[]` never `null`, rooms/agents are sorted by `startedAt`, every room carries its working directory in `project`, `messages` and `events` are capped (40 / 30) and oldest-first.

### Conventions

- Comments, logs, and README stay Chinese. Web UI strings live in `web/i18n.js` (both `zh` and `en`) — add every new UI string there, never inline, and keep the two dictionaries' keys identical (`i18n.test.mjs` checks).
- `web/nav.js` must stay dependency-free (pure `{x, z}` math, no three import) so `nav.test.mjs` can run under node. The test hardcodes room dimensions and furniture placement copied from `scene.js` — moving furniture means updating both.
- The plugin (`plugins/agentwatch/`) has no hooks. `scripts/ensure-server.sh` scans ports from 7788 (or the exact `AGENTWATCH_PORT` when set): it reuses a server already serving **the same user** (matched against `snapshot.owner` = `$HOME`), and otherwise starts on the first free port, so several users can share one machine. It `go run`s from `AGENTWATCH_HOME` (default `~/tools/agentwatch`, shallow-cloned on first use) and prints both the localhost and the machine's LAN IP URL. The install-time `userConfig.language` is substituted into the command/skill as `${user_config.language}` and passed to the script, which appends `?lang=`; the page resolves URL > localStorage > browser language.
- `-stuck` and `-ghost` are the two tuning knobs for semantics (`finalize`); `-all` turns on finished/history rooms and `-history` filters them at scan time (Claude by log mtime, Codex by last event). `-project` (ignored) and `-codex-history` (alias for `-history`) stay as deprecated flags: plugin caches installed before 0.1.4 still pass them.
