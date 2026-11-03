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
	Cwd         string `json:"cwd"`
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
	// 日志行里的 cwd，用来判断这个会话属于哪个工作目录。
	cwd string
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
	// 会话中途 cd 会改变后续行的 cwd；房间归属取会话启动时的第一个 cwd。
	if r.Cwd != "" && l.cwd == "" {
		l.cwd = filepath.Clean(r.Cwd)
	}
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
	Home, AliasFile string
	Stuck, Ghost    time.Duration
	History         time.Duration
	logs            map[string]*agentLog  // keyed by jsonl path
	seen            map[string]sessionReg // 上次看到的注册表记录，会话退出后用来保持房间身份稳定
}

func NewScanner(home, aliasFile string, stuck, ghost, history time.Duration) *Scanner {
	return &Scanner{Home: home, AliasFile: aliasFile, Stuck: stuck, Ghost: ghost, History: history, logs: map[string]*agentLog{}, seen: map[string]sessionReg{}}
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

func (s *Scanner) sessions() map[string]sessionReg {
	files, _ := filepath.Glob(filepath.Join(s.Home, ".claude", "sessions", "*.json"))
	byID := map[string]sessionReg{}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var r sessionReg
		if json.Unmarshal(data, &r) != nil || r.SessionID == "" {
			continue
		}
		if !alive(r.PID) {
			r.Status = "dead"
		}
		if prev, ok := byID[r.SessionID]; !ok || prev.Status == "dead" {
			byID[r.SessionID] = r
		}
	}
	return byID
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
	aliases := loadAliases(s.AliasFile)
	snap := Snapshot{Owner: s.Home, GeneratedAt: nowMs, Rooms: []Room{}}
	regs := s.sessions()

	// 候选 = 最近改过的会话日志 + 注册表里的会话（进程还活着，日志可能还没落盘）。
	// ponytail: 用 mtime 预筛历史窗口，避免每次扫描重放几百个旧会话；要更长历史就调 -history。
	mainPaths := map[string]string{}
	files, _ := filepath.Glob(filepath.Join(s.Home, ".claude", "projects", "*", "*.jsonl"))
	for _, p := range files {
		sid := strings.TrimSuffix(filepath.Base(p), ".jsonl")
		if ifo, err := os.Stat(p); err == nil && now.Sub(ifo.ModTime()) <= s.History {
			mainPaths[sid] = p
		}
	}
	for _, reg := range regs {
		if reg.Cwd == "" {
			continue
		}
		if _, ok := mainPaths[reg.SessionID]; ok {
			continue
		}
		mainPaths[reg.SessionID] = filepath.Join(projectLogDir(s.Home, filepath.Clean(reg.Cwd)), reg.SessionID+".jsonl")
	}

	sids := make([]string, 0, len(mainPaths))
	for sid := range mainPaths {
		sids = append(sids, sid)
	}
	sort.Strings(sids)

	for _, sid := range sids {
		mainPath := mainPaths[sid]
		reg, live := regs[sid]
		if live {
			s.seen[sid] = reg
		} else if prev, ok := s.seen[sid]; ok {
			// 注册表文件随进程结束被删除：沿用最后一次记录，名字、分组、开始时间不跳变。
			reg = prev
		}
		ml := s.log(mainPath, Agent{ID: "main", Type: "main", Description: "主 agent"})
		ml.sync(mainPath, false)

		name := reg.Name
		if name == "" {
			name = sid[:min(8, len(sid))]
		}
		status := "idle"
		if live {
			status = reg.Status
		} else if reg.SessionID != "" {
			status = "dead"
		}
		project := ml.cwd
		if reg.Cwd != "" {
			project = filepath.Clean(reg.Cwd)
		}
		if project == "" || project == "." {
			continue // 还没解析出 cwd 的空日志，等下一轮
		}
		startedAt := ml.agent.StartedAt
		if reg.StartedAt > 0 {
			startedAt = reg.StartedAt
		}
		main := finalize(ml.agent, nowMs, s.Stuck)
		// A main agent waiting on the user is idle, not stuck.
		if status != "busy" && main.State == "stuck" {
			main.State = "idle"
		}
		alias := aliases[sid]
		if alias == "" {
			alias = aliases[name]
		}
		room := Room{SessionID: sid, Name: name, Alias: alias, Project: project, Provider: "claude", Status: status, StartedAt: startedAt, Main: main, Agents: []Agent{}, Messages: append([]Msg{}, ml.msgs...)}

		paths, _ := filepath.Glob(filepath.Join(filepath.Dir(mainPath), sid, "subagents", "agent-*.jsonl"))
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
			a := finalize(l.agent, nowMs, s.Stuck)
			// 会话已经结束，没写 finished 的子 agent 是被一起带走的，不算卡住。
			if (!live || status == "dead") && a.State == "stuck" {
				a.State = "idle"
			}
			room.Agents = append(room.Agents, a)
		}
		sort.Slice(room.Agents, func(i, j int) bool { return room.Agents[i].StartedAt < room.Agents[j].StartedAt })
		sort.SliceStable(room.Messages, func(i, j int) bool { return room.Messages[i].Ts < room.Messages[j].Ts })
		if n := len(room.Messages); n > maxMsgs {
			room.Messages = room.Messages[n-maxMsgs:]
		}
		snap.Rooms = append(snap.Rooms, room)
	}
	for sid := range s.seen {
		if _, ok := mainPaths[sid]; !ok {
			delete(s.seen, sid)
		}
	}
	sort.SliceStable(snap.Rooms, func(i, j int) bool { return snap.Rooms[i].StartedAt < snap.Rooms[j].StartedAt })
	return snap
}
