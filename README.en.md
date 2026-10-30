# agentwatch

[中文](README.md) | English

A live 3D dashboard of every Claude Code and Codex session in this project — one session per room. A Go standard-library scanner tails two log sources and pushes a snapshot over SSE every second; the frontend is a build-free Three.js cozy low-poly isometric office. Codex rooms are labeled `Codex` on the nameplate and in the sidebar.

- Claude Code: the session registry at `~/.claude/sessions/*.json` plus session and sub-agent jsonl under `~/.claude/projects/<encoded-path>`.
- Codex: sessions in `~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl` whose `cwd` matches this project; the room name comes from the thread name in `~/.codex/session_index.jsonl`. Codex draws the main agent only — no sub-agents, no paper planes. There is no process registry, so busy/idle is derived from the latest event time.

## Running

```bash
# Start in the project directory you want to watch; opens the browser
~/tools/agentwatch/start.sh

# Different port
AGENTWATCH_PORT=7788 ~/tools/agentwatch/start.sh

# Fake data (two rooms, cycling states) for frontend work
cd ~/tools/agentwatch && go run . -demo -port 7799
```

Flags: `-project` defaults to the current directory, `-port` to 7788, `-stuck` to 120s, `-ghost` to 180s, `-aliases` to `aliases.json`, and `-codex-history` to 24h (Codex sessions whose last event is older are hidden; raise it to see old sessions, e.g. `-codex-history 720h`).

## Install as a plugin

This directory is also a plugin marketplace for both Claude Code and Codex; the plugin lives in `plugins/agentwatch/`. Once installed, tell the agent "open agentwatch" (or use `/agentwatch` in Claude Code) and it runs `plugins/agentwatch/scripts/ensure-server.sh` in the current project directory: it reuses a server already on the port, otherwise starts one in the background (log at `log/agentwatch.log`) and prints the URL.

```bash
# Claude Code
claude plugin marketplace add ~/tools/agentwatch
claude plugin install agentwatch@agentwatch

# Codex
codex plugin marketplace add ~/tools/agentwatch
codex plugin add agentwatch@agentwatch
```

The script reads `AGENTWATCH_PORT` (default 7788) and `AGENTWATCH_HOME` (set it when the source is not at `~/tools/agentwatch`). The plugin ships no hooks.

## States and animation

| State | Animation |
|-------|-----------|
| `thinking` | Idle, swaying beside the desk |
| `tool` | Sitting, working at the monitor — alternating typing and mouse, desk lamp turns amber |
| `processing` | Yes, standing up and nodding while reading the result |
| `idle` | Slumped in the chair asleep, Zzz floating overhead; agents without a desk stay standing |
| `stuck` | No, shaking head, red exclamation mark, desk lamp and halo pulsing red |
| `done` | Wave or ThumbsUp, walks to the door, turns grey and becomes a ghost |

Each room has a foreman desk at the back center; the main agent sits or stands by the same state table.

A paper plane flies between agents when they message each other: the main agent dispatching work to a sub-agent, a sub-agent handing back a result, and `SendMessage` to another session (which flies across rooms).

## Session aliases

Give sessions aliases in `aliases.json`, matched by sessionId or session name (Claude Code session name / Codex thread name). The server hot-reloads it every second, so edits take effect immediately. The repo only ships `aliases.example.json`; copy it to `aliases.json` and edit that (it is git-ignored, so private session names stay out of the repo):

```json
{
  "slg-server-a0": "3D dashboard",
  "2695dfe4-e519-4dd9-8458-0f065b7c1107": "login triage"
}
```

Nameplates and the sidebar prefer the alias; the original session name stays in the sidebar in small text.

## Layout

- `model.go` snapshot / room / agent / event types
- `scanner.go` project detection, session discovery, incremental jsonl reading, state derivation
- `codex.go` Codex rollout discovery and state derivation, reusing `scanner.go`'s incremental reader
- `main.go` flags, merging snapshots from both sources, HTTP, SSE, static files
- `demo.go` demo data
- `web/` frontend; `web/vendor/three` is vendored three.js, `web/assets/RobotExpressive.glb` is the CC0 character
- `web/nav.js` room floor navigation: furniture carved out as obstacles, A* detours; characters yield to each other while walking. Check: `node --test web/nav.test.mjs`
- `PROTOCOL.md` frontend/backend contract
- `plugins/agentwatch/`, `.claude-plugin/`, `.agents/plugins/` dual-host plugin and marketplace manifests

## Known limitations

- 8 desks per room; extras stand at the doorway and get no connection lines.
- Labels have no collision handling and overlap when there are many agents.
- When upgrading three.js, update both `web/vendor/three/build/three.module.js` and `three.core.js`; since r186 the former depends on the latter.
- If a session registry `status` is stuck at `busy`, the main agent goes silent past `-stuck` and shows as stuck.
- Codex has a main agent only: the logs carry no matching sub-agent metadata, so there are no desks or paper planes.
- Codex has no process registry, so a single tool call that runs longer than `-stuck` (120s default) with no new events shows as idle rather than stuck.
