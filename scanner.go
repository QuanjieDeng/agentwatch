package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// projectLogDir mirrors Claude Code's own encoding, which maps every
// non-alphanumeric byte (not just '/') to '-': /a/slg_server -> -a-slg-server.
func projectLogDir(home, project string) string {
	var b strings.Builder
	for _, r := range project {
		if r < utf8.RuneSelf && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return filepath.Join(home, ".claude", "projects", b.String())
}

// tailer yields only newline-terminated lines appended since the last read.
type tailer struct {
	off     int64
	partial []byte
}

func (t *tailer) read(path string) ([][]byte, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, false
	}
	truncated := st.Size() < t.off
	if truncated {
		t.off, t.partial = 0, nil
	}
	if st.Size() == t.off {
		return nil, truncated
	}
	if _, err := f.Seek(t.off, io.SeekStart); err != nil {
		return nil, truncated
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, truncated
	}
	t.off += int64(len(data))
	data = append(t.partial, data...)
	end := bytes.LastIndexByte(data, '\n')
	if end < 0 {
		t.partial = data
		return nil, truncated
	}
	t.partial = append([]byte(nil), data[end+1:]...)
	return bytes.Split(data[:end], []byte{'\n'}), truncated
}

type rawLine struct {
	Type        string `json:"type"`
	IsSidechain bool   `json:"isSidechain"`
	Timestamp   string `json:"timestamp"`
	Message     struct {
		ID      string          `json:"id"`
		Content json.RawMessage `json:"content"`
		Usage   struct {
			In        int64 `json:"input_tokens"`
			Out       int64 `json:"output_tokens"`
			CacheRead int64 `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

type block struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`          // tool_use 的 id，和子 agent meta 的 toolUseId 对得上
	ToolUseID string          `json:"tool_use_id"` // tool_result 指回哪个 tool_use
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	Content   json.RawMessage `json:"content"`
}

// agentLog is the incrementally folded state of one jsonl file.
type agentLog struct {
	tailer
	agent Agent
	// Claude Code writes one assistant message as several lines (one per
	// content block) that repeat the same id and usage.
	seenMsg map[string]bool
	// 派活的时间点，键是 Agent 工具调用的 tool_use id；子 agent 的 meta 用 toolUseId 回来查。
	calls map[string]int64
	msgs  []Msg
}

func newAgentLog(a Agent) *agentLog {
	a.State = "idle"
	a.Events = []Event{}
	return &agentLog{agent: a, seenMsg: map[string]bool{}, calls: map[string]int64{}}
}

func (l *agentLog) sync(path string, sidechainOK bool) {
	lines, truncated := l.read(path)
	if truncated {
		*l = *newAgentLog(Agent{ID: l.agent.ID, ParentID: l.agent.ParentID, Type: l.agent.Type, Description: l.agent.Description})
	}
	for _, ln := range lines {
		l.apply(ln, sidechainOK)
	}
}

func (l *agentLog) apply(line []byte, sidechainOK bool) {
	var r rawLine
	if json.Unmarshal(line, &r) != nil {
		return
	}
	if r.Type != "user" && r.Type != "assistant" || r.IsSidechain && !sidechainOK {
		return
	}
	t, err := time.Parse(time.RFC3339Nano, r.Timestamp)
	if err != nil {
		return
	}
	ts := t.UnixMilli()
	a := &l.agent
	if a.StartedAt == 0 {
		a.StartedAt = ts
	}
	a.LastEventAt = ts

	var blocks []block
	var plain string
	if json.Unmarshal(r.Message.Content, &plain) != nil {
		_ = json.Unmarshal(r.Message.Content, &blocks)
	}

	if r.Type == "assistant" {
		if id := r.Message.ID; id == "" || !l.seenMsg[id] {
			l.seenMsg[id] = true
			a.Turns++
			a.Tokens.In += r.Message.Usage.In
			a.Tokens.Out += r.Message.Usage.Out
			a.Tokens.CacheRead += r.Message.Usage.CacheRead
		}
		state, tool, detail := "thinking", "", ""
		for _, b := range blocks {
			switch b.Type {
			case "tool_use":
				d := summarize(b.Input)
				state, tool, detail = "tool", b.Name, d
				l.push(Event{ts, "tool", b.Name, b.Name + " " + d})
				switch b.Name {
				case "Agent", "Task":
					if b.ID != "" {
						l.calls[b.ID] = ts
					}
				case "SendMessage":
					if to := recipient(b.Input); to != "" {
						l.msg(Msg{From: a.ID, To: to, Ts: ts})
					}
				}
			case "text":
				l.push(Event{ts, "text", "", trunc(b.Text, 200)})
			case "thinking":
				if b.Thinking != "" {
					l.push(Event{ts, "thinking", "", trunc(b.Thinking, 80)})
				}
			}
		}
		a.State, a.Tool, a.Detail, a.Since = state, tool, detail, ts
		return
	}

	state := "thinking"
	if plain != "" {
		l.push(Event{ts, "user", "", trunc(plain, 120)})
	}
	for _, b := range blocks {
		switch b.Type {
		case "tool_result":
			state = "processing"
			l.push(Event{ts, "result", "", trunc(resultText(b.Content), 120)})
		case "text":
			l.push(Event{ts, "user", "", trunc(b.Text, 120)})
		}
	}
	a.State, a.Since = state, ts
	if state != "processing" {
		a.Tool, a.Detail = "", ""
	}
}

func (l *agentLog) push(e Event) {
	ev := append(l.agent.Events, e)
	if len(ev) > maxEvents {
		ev = append([]Event(nil), ev[len(ev)-maxEvents:]...)
	}
	l.agent.Events = ev
}

func (l *agentLog) msg(m Msg) {
	l.msgs = append(l.msgs, m)
	if len(l.msgs) > maxMsgs {
		l.msgs = append([]Msg(nil), l.msgs[len(l.msgs)-maxMsgs:]...)
	}
}

// recipient 取 SendMessage 的收件人；uds: socket 形式的收件人映射不到房间，丢掉。
func recipient(raw json.RawMessage) string {
	var in struct {
		To string `json:"to"`
	}
	if json.Unmarshal(raw, &in) != nil {
		return ""
	}
	to := strings.TrimSpace(in.To)
	if to == "" || strings.HasPrefix(to, "uds:") {
		return ""
	}
	return to
}

func resultText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var bs []block
	_ = json.Unmarshal(raw, &bs)
	for _, b := range bs {
		if b.Type == "text" {
			return b.Text
		}
	}
	return ""
}

func summarize(raw json.RawMessage) string {
	var in map[string]any
	if json.Unmarshal(raw, &in) != nil {
		return trunc(string(raw), 80)
	}
	str := func(k string) string { s, _ := in[k].(string); return s }
	switch {
	case str("file_path") != "":
		return str("file_path")
	case str("command") != "":
		cmd, _, _ := strings.Cut(str("command"), "\n")
		return trunc(cmd, 80)
	case str("pattern") != "":
		return str("pattern")
	case str("description") != "":
		return str("description")
	case str("prompt") != "":
		return trunc(str("prompt"), 80)
	case str("url") != "":
		return str("url")
	}
	compact, _ := json.Marshal(in)
	return trunc(string(compact), 80)
}

func trunc(s string, n int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "…"
}

// finalize applies the time-dependent overlays that must be recomputed on
// every snapshot even when no new lines arrived.
func finalize(a Agent, now int64, stuck time.Duration) Agent {
	switch {
	case a.Finished:
		a.State = "done"
	case a.LastEventAt == 0:
		a.State = "idle"
	case now-a.LastEventAt > stuck.Milliseconds():
		a.State = "stuck"
	}
	return a
}

type sessionReg struct {
	PID       int    `json:"pid"`
	SessionID string `json:"sessionId"`
	Cwd       string `json:"cwd"`
	Status    string `json:"status"`
	Name      string `json:"name"`
	StartedAt int64  `json:"startedAt"`
}

type subMeta struct {
	AgentType   string `json:"agentType"`
	Description string `json:"description"`
	Finished    bool   `json:"finished"`
	ToolUseID   string `json:"toolUseId"`
}

type Scanner struct {
	Project, Home, AliasFile string
	Stuck, Ghost             time.Duration
	logs                     map[string]*agentLog // keyed by jsonl path
}

func NewScanner(project, home, aliasFile string, stuck, ghost time.Duration) *Scanner {
	return &Scanner{Project: project, Home: home, AliasFile: aliasFile, Stuck: stuck, Ghost: ghost, logs: map[string]*agentLog{}}
}

// loadAliases re-reads the alias file on every scan so edits show up within a second.
func loadAliases(path string) map[string]string {
	out := map[string]string{}
	data, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	var raw map[string]string
	if json.Unmarshal(data, &raw) != nil {
		return out
	}
	for k, v := range raw {
		if k, v = strings.TrimSpace(k), strings.TrimSpace(v); k != "" && v != "" {
			out[k] = v
		}
	}
	return out
}

func (s *Scanner) sessions() []sessionReg {
	files, _ := filepath.Glob(filepath.Join(s.Home, ".claude", "sessions", "*.json"))
	byID := map[string]sessionReg{}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var r sessionReg
		if json.Unmarshal(data, &r) != nil || r.Cwd != s.Project || r.SessionID == "" {
			continue
		}
		if !alive(r.PID) {
			r.Status = "dead"
		}
		if prev, ok := byID[r.SessionID]; !ok || prev.Status == "dead" {
			byID[r.SessionID] = r
		}
	}
	out := make([]sessionReg, 0, len(byID))
	for _, r := range byID {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt < out[j].StartedAt })
	return out
}

func alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	_, err := os.Stat(filepath.Join("/proc", strconv.Itoa(pid)))
	return err == nil
}

func (s *Scanner) log(path string, a Agent) *agentLog {
	l, ok := s.logs[path]
	if !ok {
		l = newAgentLog(a)
		s.logs[path] = l
	}
	return l
}

func (s *Scanner) Scan(now time.Time) Snapshot {
	nowMs := now.UnixMilli()
	dir := projectLogDir(s.Home, s.Project)
	aliases := loadAliases(s.AliasFile)
	snap := Snapshot{Project: s.Project, GeneratedAt: nowMs, Rooms: []Room{}}
	for _, reg := range s.sessions() {
		name := reg.Name
		if name == "" {
			name = reg.SessionID[:min(8, len(reg.SessionID))]
		}
		mainPath := filepath.Join(dir, reg.SessionID+".jsonl")
		ml := s.log(mainPath, Agent{ID: "main", Type: "main", Description: "主 agent"})
		ml.sync(mainPath, false)
		main := finalize(ml.agent, nowMs, s.Stuck)
		// A main agent waiting on the user is idle, not stuck.
		if reg.Status != "busy" && main.State == "stuck" {
			main.State = "idle"
		}
		alias := aliases[reg.SessionID]
		if alias == "" {
			alias = aliases[name]
		}
		room := Room{SessionID: reg.SessionID, Name: name, Alias: alias, Provider: "claude", Status: reg.Status, StartedAt: reg.StartedAt, Main: main, Agents: []Agent{}, Messages: append([]Msg{}, ml.msgs...)}

		paths, _ := filepath.Glob(filepath.Join(dir, reg.SessionID, "subagents", "agent-*.jsonl"))
		for _, p := range paths {
			id := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(p), "agent-"), ".jsonl")
			var meta subMeta
			if data, err := os.ReadFile(strings.TrimSuffix(p, ".jsonl") + ".meta.json"); err == nil {
				_ = json.Unmarshal(data, &meta)
			}
			l := s.log(p, Agent{ID: id, ParentID: "main", Type: meta.AgentType, Description: meta.Description})
			l.sync(p, true)
			l.agent.Finished = meta.Finished
			if meta.Finished && nowMs-l.agent.LastEventAt > s.Ghost.Milliseconds() {
				continue
			}
			// 派活看主 agent 那边 Agent 调用的时间点，交回结果看子 agent 自己最后一条事件。
			if meta.ToolUseID != "" {
				if at, ok := ml.calls[meta.ToolUseID]; ok {
					room.Messages = append(room.Messages, Msg{From: "main", To: id, Ts: at})
				}
			}
			if meta.Finished && l.agent.LastEventAt > 0 {
				room.Messages = append(room.Messages, Msg{From: id, To: "main", Ts: l.agent.LastEventAt})
			}
			room.Messages = append(room.Messages, l.msgs...)
			room.Agents = append(room.Agents, finalize(l.agent, nowMs, s.Stuck))
		}
		sort.Slice(room.Agents, func(i, j int) bool { return room.Agents[i].StartedAt < room.Agents[j].StartedAt })
		sort.SliceStable(room.Messages, func(i, j int) bool { return room.Messages[i].Ts < room.Messages[j].Ts })
		if n := len(room.Messages); n > maxMsgs {
			room.Messages = room.Messages[n-maxMsgs:]
		}
		snap.Rooms = append(snap.Rooms, room)
	}
	return snap
}
