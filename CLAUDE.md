# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
go run . -demo -port 7799          # synthetic data, two rooms, no logs needed — use this for frontend work
go run . -project <dir> -port 7788 # watch a project's real Claude Code + Codex sessions
go test ./...                      # Go tests (scanner state folding, tailer, Codex rollout, end-to-end scan)
go test -run TestScanEndToEnd ./...
node --test web/*.test.mjs         # frontend nav A* + i18n language-resolution checks (stdlib node:test)
go vet ./...
```

`./start.sh` is the convenience launcher (project = cwd, opens browser). There is no build step for the frontend: `web/` is plain ES modules served directly, three.js is vendored under `web/vendor/`. `go build -o agentwatch .` is the only build; `agentwatch` and `log/` are gitignored.

## Architecture

Single Go binary, stdlib only (`go.mod` has no dependencies). Every second the server scans local agent logs, folds them into one `Snapshot` JSON, and both `/api/snapshot` and the `/events` SSE stream serve that cached byte slice. The frontend receives snapshots and animates a Three.js isometric office — one room per session, one desk per agent. The split:

- `scanner.go` — Claude Code source. Finds the project's log dir by reimplementing Claude Code's path encoding (every non-alphanumeric byte → `-`, see `projectLogDir`), reads the `~/.claude/sessions/*.json` registry for liveness (`/proc/<pid>`, hence status can be `dead`), then incrementally tails each session jsonl and its `subagents/agent-*.jsonl`.
- `codex.go` — Codex source. Walks `~/.codex/sessions/**/rollout-*.jsonl`, keeps those whose first-line `session_meta.cwd` matches, reuses the same `tailer`. No process registry: `busy`/`idle` is inferred from the last event time, so `dead` and subagents don't exist here.
- `model.go` — the wire types (`Snapshot`/`Room`/`Agent`/`Msg`/`Event`).
- `main.go` — flags, merges both scanners via the `scanner` interface + `multiScanner`, sorts rooms by `startedAt`, serves HTTP.
- `demo.go` — `scanner` implementation for `-demo`; also the fastest way to exercise ghosts, stuck agents, and cross-room paper planes.
- `web/` — `main.js` applies each snapshot (rooms, actors, sidebar history), `scene.js` builds rooms/furniture/paper planes, `agents.js` loads the robot GLB and maps `state` → animation, `nav.js` is the ground A* grid, `i18n.js` holds every UI string in `zh` and `en`.

### State is incremental and process-lifetime

`tailer` + `agentLog`/`codexLog` keep per-file read offsets, turn/token counters, and event buffers in memory. State only advances on newly appended newline-terminated lines; a shrunk file resets to zero. Restarting the process replays everything from the start of each log. Time-dependent overlays (`stuck`, `done`/ghost removal) are recomputed on every scan in `finalize`, not when lines are read — so they must stay out of `apply`.

### The protocol is the contract

`PROTOCOL.md` is explicitly frozen. Any change to `model.go` fields, state names, ordering semantics, or the SSE frame format must be mirrored there and in `web/main.js` together. Notable invariants: `main` is the fixed main-agent id, arrays serialize as `[]` never `null`, rooms/agents are sorted by `startedAt`, `messages` and `events` are capped (40 / 30) and oldest-first.

### Conventions

- Comments, logs, and README stay Chinese. Web UI strings live in `web/i18n.js` (both `zh` and `en`) — add every new UI string there, never inline, and keep the two dictionaries' keys identical (`i18n.test.mjs` checks).
- `web/nav.js` must stay dependency-free (pure `{x, z}` math, no three import) so `nav.test.mjs` can run under node. The test hardcodes room dimensions and furniture placement copied from `scene.js` — moving furniture means updating both.
- The plugin (`plugins/agentwatch/`) has no hooks. `scripts/ensure-server.sh` scans ports from 7788 (or the exact `AGENTWATCH_PORT` when set): it reuses a server already watching **the same project** (matched against `snapshot.project`, via `pwd -P`), and otherwise starts on the first free port, so several users/projects can share one machine. It `go run`s from `AGENTWATCH_HOME` (default `~/tools/agentwatch`, shallow-cloned on first use) and prints both the localhost and the machine's LAN IP URL. The install-time `userConfig.language` is substituted into the command/skill as `${user_config.language}` and passed to the script, which appends `?lang=`; the page resolves URL > localStorage > browser language.
- `-stuck` and `-ghost` are the two tuning knobs for semantics (`finalize`); `-codex-history` filters old Codex rooms at scan time.
