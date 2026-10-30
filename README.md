# agentwatch

中文 | [English](README.en.md)

本工程所有 Claude Code 与 Codex 会话的实时 3D 看板，一个会话一个房间。Go 标准库扫描两处日志，每秒经 SSE 推送快照；前端是无构建的 Three.js 温馨低多边形等距办公室，Codex 房间的名牌与侧栏会标出 `Codex`。

- Claude Code：`~/.claude/sessions/*.json` 注册表与 `~/.claude/projects/<编码路径>` 下的会话、子 agent jsonl。
- Codex：`~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl` 中 `cwd` 等于本项目的会话，房间名取 `~/.codex/session_index.jsonl` 的线程名。Codex 只画主 agent，没有子 agent 与纸飞机；没有进程注册表，按最近事件时间判断忙闲。

## 运行

```bash
# 在要观察的项目目录下启动，自动打开浏览器
~/tools/agentwatch/start.sh

# 换端口
AGENTWATCH_PORT=7788 ~/tools/agentwatch/start.sh

# 假数据（两个房间、状态循环），用于调前端
cd ~/tools/agentwatch && go run . -demo -port 7799
```

参数：`-project` 默认当前目录，`-port` 默认 7788，`-stuck` 默认 120s，`-ghost` 默认 180s，`-aliases` 默认 `aliases.json`，`-codex-history` 默认 24h（最后事件早于它的 Codex 会话不显示，看老会话就调大，如 `-codex-history 720h`）。

## 作为插件安装

本目录同时是 Claude Code 与 Codex 的插件 marketplace，插件在 `plugins/agentwatch/`。装好后对 agent 说「打开 agentwatch」，或在 Claude Code 里用 `/agentwatch`，它会在当前项目目录运行 `plugins/agentwatch/scripts/ensure-server.sh`：端口上已有服务就复用，否则后台起服务（日志 `log/agentwatch.log`），最后给出 URL。

```bash
# Claude Code
claude plugin marketplace add ~/tools/agentwatch
claude plugin install agentwatch@agentwatch

# Codex
codex plugin marketplace add ~/tools/agentwatch
codex plugin add agentwatch@agentwatch
```

脚本读 `AGENTWATCH_PORT`（默认 7788）与 `AGENTWATCH_HOME`（源码不在 `~/tools/agentwatch` 时指定）。插件不带 hook。

## 状态与动画

| 状态 | 动画 |
|------|------|
| `thinking` | Idle，站在工位旁轻晃 |
| `tool` | Sitting，坐下对着显示器干活，一会打字一会用鼠标，台灯变琥珀色 |
| `processing` | Yes，站起来点头读结果 |
| `idle` | 躺进椅子里呼呼大睡，头顶飘 Zzz；没有工位的 agent 站着 |
| `stuck` | No，摇头，头顶红色感叹号，工位灯与光环红色脉冲 |
| `done` | Wave 或 ThumbsUp，走向门口变灰成幽灵 |

每个房间的中央后方有一张工头桌，主 agent 也按同一套状态表坐下或起立。

agent 之间发消息时会飞过一架纸飞机：主 agent 派活给子 agent、子 agent 交回结果，以及 `SendMessage` 发给别的会话（跨房间飞）。

## 会话别名

在 `aliases.json` 里给 session 起别名，按 sessionId 或会话名（Claude Code 会话名 / Codex 线程名）匹配，服务端每秒热读，改完即可生效。仓库只带 `aliases.example.json`，复制成 `aliases.json` 再改（该文件已被 git 忽略，不会把私人会话名提交上去）：

```json
{
  "slg-server-a0": "3D 监控台",
  "2695dfe4-e519-4dd9-8458-0f065b7c1107": "登录排查"
}
```

房间名牌与侧栏优先显示别名，原始会话名以小字保留在侧栏。

## 目录

- `model.go` 快照/房间/agent/事件类型
- `scanner.go` 项目定位、会话发现、jsonl 增量读取、状态推导
- `codex.go` Codex rollout 发现与状态推导，复用 `scanner.go` 的增量读取
- `main.go` flag、两端快照合并、HTTP、SSE、静态文件
- `demo.go` 演示数据
- `web/` 前端；`web/vendor/three` 是 vendored three.js，`web/assets/RobotExpressive.glb` 是 CC0 角色
- `web/nav.js` 房间地面导航：家具挖成障碍，A* 绕行；角色走动时互相推让。检查：`node --test web/nav.test.mjs`
- `PROTOCOL.md` 前后端契约
- `plugins/agentwatch/`、`.claude-plugin/`、`.agents/plugins/` 双端插件与 marketplace 清单

## 已知限制

- 每个房间 8 个工位，超出部分站在门口且不画连线。
- 标签没有碰撞处理，agent 很多时会重叠。
- 角色模型升级 three.js 时要同时更新 `web/vendor/three/build/three.module.js` 与 `three.core.js`，r186 起前者依赖后者。
- session 注册表 `status` 若停在 `busy`，主 agent 静默超过 `-stuck` 会显示卡住。
- Codex 只有主 agent：日志里还没有可对上的子 agent 元数据，所以没有工位和纸飞机。
- Codex 没有进程注册表，单次工具调用超过 `-stuck`（默认 120 秒）没有新事件时会显示 idle，而不是 stuck。
