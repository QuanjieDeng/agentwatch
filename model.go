package main

type Snapshot struct {
	Project     string `json:"project"`
	GeneratedAt int64  `json:"generatedAt"`
	Rooms       []Room `json:"rooms"`
}

type Room struct {
	SessionID string  `json:"sessionId"`
	Name      string  `json:"name"`
	Alias     string  `json:"alias"`    // 用户在 aliases.json 里起的别名，空则前端回落到 Name
	Provider  string  `json:"provider"` // claude | codex
	Status    string  `json:"status"`   // busy | idle | dead
	StartedAt int64   `json:"startedAt"`
	Main      Agent   `json:"main"`
	Agents    []Agent `json:"agents"`
	Messages  []Msg   `json:"messages"`
}

// Msg 是一条 agent 之间的消息，前端据此放纸飞机。
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
	State       string  `json:"state"` // thinking | tool | processing | idle | done | stuck
	Tool        string  `json:"tool"`
	Detail      string  `json:"detail"`
	Since       int64   `json:"since"`
	StartedAt   int64   `json:"startedAt"`
	LastEventAt int64   `json:"lastEventAt"`
	Turns       int     `json:"turns"`
	Tokens      Tokens  `json:"tokens"`
	Finished    bool    `json:"finished"`
	Events      []Event `json:"events"`
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

const (
	maxEvents = 30
	maxMsgs   = 40
)
