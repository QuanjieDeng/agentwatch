# agentwatch 协议（冻结）

## 端点

- `GET /api/snapshot`：返回一个 `Snapshot` JSON。
- `GET /events`：`text/event-stream`。连上立即推一帧，之后每秒一帧，每帧格式为 `data: <Snapshot JSON>\n\n`，没有 `event:` 和 `id:` 字段。
- `GET /`、`/main.js`、`/style.css`、`/assets/*`、`/vendor/*`：从 `web/` 目录静态提供，文件不存在返回 404。

## 数据模型

时间字段一律是 epoch 毫秒。数组字段一律输出 `[]`，不会是 `null`。

```go
type Snapshot struct {
    Project     string `json:"project"`
    GeneratedAt int64  `json:"generatedAt"` // epoch ms
    Rooms       []Room `json:"rooms"`
}
type Room struct {
    SessionID string  `json:"sessionId"`
    Name      string  `json:"name"`
    Alias     string  `json:"alias"`     // aliases.json 里的用户别名，空则回落到 Name
    Provider  string  `json:"provider"`  // claude | codex，会话来自哪个宿主
    Status    string  `json:"status"`    // busy | idle | dead
    StartedAt int64   `json:"startedAt"`
    Main      Agent   `json:"main"`
    Agents    []Agent `json:"agents"`
    Messages  []Msg   `json:"messages"`  // 最近 40 条，旧的在前
}
type Msg struct {
    From string `json:"from"` // 本房间的 agent id，"main" 是主 agent
    To   string `json:"to"`   // 同房间的 agent id，或另一个会话的名字（跨房间）
    Ts   int64  `json:"ts"`
}
type Agent struct {
    ID          string  `json:"id"`
    ParentID    string  `json:"parentId"`
    Type        string  `json:"type"`
    Description string  `json:"description"`
    State       string  `json:"state"`   // thinking | tool | processing | idle | done | stuck
    Tool        string  `json:"tool"`
    Detail      string  `json:"detail"`
    Since       int64   `json:"since"`
    StartedAt   int64   `json:"startedAt"`
    LastEventAt int64   `json:"lastEventAt"`
    Turns       int     `json:"turns"`
    Tokens      Tokens  `json:"tokens"`
    Finished    bool    `json:"finished"`
    Events      []Event `json:"events"` // 最多 30 条，旧的在前
}
type Tokens struct {
    In        int64 `json:"in"`
    Out       int64 `json:"out"`
    CacheRead int64 `json:"cacheRead"`
}
type Event struct {
    Ts   int64  `json:"ts"`
    Kind string `json:"kind"` // tool | text | thinking | result | user
    Tool string `json:"tool"`
    Text string `json:"text"`
}
```

## 语义

- 主 agent 的 `id` 固定为 `"main"`，`parentId` 为空，`type` 为 `"main"`。子 agent 的 `parentId` 一律是 `"main"`。
- `state` 含义如下。
  - `thinking`：模型在出文字或思考，或刚收到用户文本。
  - `tool`：模型发起了工具调用，`tool` 为工具名，`detail` 为参数摘要。
  - `processing`：工具结果已返回，模型尚未回应。`tool` 保留上一个工具名。
  - `idle`：还没有任何事件。主 agent 所在会话不是 `busy` 时也显示 `idle`，不会显示 `stuck`。
  - `done`：子 agent 已结束。结束后保留 180 秒（demo 为 10 秒），之后从 `agents` 中消失。
  - `stuck`：未结束，且距 `lastEventAt` 超过 120 秒。
- `since` 是当前状态开始的时间，前端用它算「已持续多久」。
- `turns` 是 assistant 消息条数，`tokens` 是这些消息 usage 的累加。同一条消息被拆成多行日志时只算一次。
- `rooms` 按 `startedAt` 升序，`agents` 按 `startedAt` 升序，前端可依赖此顺序做稳定布局。
- `messages` 是 agent 之间的消息，前端据此放纸飞机：
  - 主 agent 派活给子 agent：`from` 为 `"main"`，`to` 为子 agent id，`ts` 取 Agent 工具调用的时间。
  - 子 agent 交回结果：`from` 为子 agent id，`to` 为 `"main"`，`ts` 取子 agent 最后一条事件的时间。
  - `SendMessage`：`from` 为发起方 agent id，`to` 是收件人。收件人是 `uds:` socket 的记不了，直接丢弃。

## Codex 房间

`provider` 为 `"codex"` 的房间来自 `~/.codex/sessions/**/rollout-*.jsonl`，只收第一行 `session_meta.cwd` 等于本项目的会话，字段语义与上面一致，以下几处不同：

- `name` 取 `~/.codex/session_index.jsonl` 里的 `thread_name`，没有就取会话 id 前 8 位。
- Codex 没有进程注册表，`status` 只有 `busy` 与 `idle`：距主 agent `lastEventAt` 不超过 120 秒为 `busy`，否则 `idle`，从不出现 `dead`。
- 只有主 agent，`agents` 与 `messages` 恒为 `[]`。
- 主 agent 状态：用户输入与助手文本为 `thinking`，工具调用为 `tool`，工具输出为 `processing`，`task_complete` 后为 `idle`。会话不是 `busy` 时不显示 `stuck`。
- `turns` 是去重后的助手消息条数，`tokens` 直接取最近一次 `token_count` 的累计用量，`in` / `out` / `cacheRead` 对应 `input_tokens` / `output_tokens` / `cached_input_tokens`。
- 最后一条事件早于 `-codex-history`（默认 24 小时）的会话不输出。
