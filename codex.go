package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type codexLine struct {
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Payload   struct {
		Type      string `json:"type"`
		ID        string `json:"id"`
		SessionID string `json:"session_id"`
		Timestamp string `json:"timestamp"`
		Cwd       string `json:"cwd"`
		Role      string `json:"role"`
		Content   []struct {
			Text string `json:"text"`
		} `json:"content"`
		Message   string          `json:"message"`
		Name      string          `json:"name"`
		Arguments string          `json:"arguments"`
		Input     string          `json:"input"` // custom_tool_call 的参数字段
		Output    json.RawMessage `json:"output"`
		Action    struct {
			Command []string `json:"command"`
		} `json:"action"`
		Info *struct {
			Total struct {
				In        int64 `json:"input_tokens"`
				CacheRead int64 `json:"cached_input_tokens"`
				Out       int64 `json:"output_tokens"`
			} `json:"total_token_usage"`
		} `json:"info"`
	} `json:"payload"`
}

func (r *codexLine) ts() (int64, bool) {
	for _, s := range []string{r.Timestamp, r.Payload.Timestamp} {
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return t.UnixMilli(), true
		}
	}
	return 0, false
}

// codexLog 是一个 rollout 文件增量折叠出的主 agent。
type codexLog struct {
	tailer
	agent   Agent
	seenMsg map[string]bool
}

func newCodexLog() *codexLog {
	return &codexLog{agent: Agent{ID: "main", Type: "main", Description: "主 agent", State: "idle", Events: []Event{}}, seenMsg: map[string]bool{}}
}

func (l *codexLog) sync(path string) {
	lines, truncated := l.read(path)
	if truncated {
		*l = *newCodexLog()
	}
	for _, ln := range lines {
		l.apply(ln)
	}
}

func (l *codexLog) apply(line []byte) {
	var r codexLine
	// 格式随 Codex 版本漂移：个别字段类型对不上时其余字段仍已填好，只丢语法坏掉的行。
	var typeErr *json.UnmarshalTypeError
	if err := json.Unmarshal(line, &r); err != nil && !errors.As(err, &typeErr) {
		return
	}
	ts, ok := r.ts()
	if !ok {
		return
	}
	a := &l.agent
	p := &r.Payload
	set := func(state string) { a.State, a.Since = state, ts }
	switch r.Type + "/" + p.Type {
	case "session_meta/":
		if a.StartedAt == 0 {
			a.StartedAt = ts
			if t, err := time.Parse(time.RFC3339Nano, p.Timestamp); err == nil {
				a.StartedAt = t.UnixMilli()
			}
		}
		return
	case "event_msg/user_message":
		set("thinking")
		a.Tool, a.Detail = "", ""
		l.push(Event{ts, "user", "", trunc(p.Message, 120)})
	// 助手文本只认 response_item：event_msg.agent_message 是它的复本。
	case "response_item/message":
		if p.Role != "assistant" {
			return
		}
		if p.ID != "" && l.seenMsg[p.ID] {
			return
		}
		l.seenMsg[p.ID] = true
		a.Turns++
		set("thinking")
		a.Tool, a.Detail = "", ""
		var text []string
		for _, c := range p.Content {
			text = append(text, c.Text)
		}
		l.push(Event{ts, "text", "", trunc(strings.Join(text, "\n"), 200)})
	case "response_item/function_call", "response_item/custom_tool_call", "response_item/local_shell_call":
		name, args := p.Name, p.Arguments
		if args == "" {
			args = p.Input
		}
		if p.Type == "local_shell_call" {
			name = "shell"
			b, _ := json.Marshal(map[string]any{"command": p.Action.Command})
			args = string(b)
		}
		d := codexSummarize(args)
		set("tool")
		a.Tool, a.Detail = name, d
		l.push(Event{ts, "tool", name, name + " " + d})
	case "response_item/function_call_output", "response_item/custom_tool_call_output", "response_item/mcp_tool_call_output":
		set("processing")
		l.push(Event{ts, "result", "", trunc(codexOutput(p.Output), 120)})
	case "event_msg/token_count":
		if p.Info == nil {
			return
		}
		// total_token_usage 本身就是累计值，直接覆盖。
		t := p.Info.Total
		a.Tokens = Tokens{In: t.In, Out: t.Out, CacheRead: t.CacheRead}
	case "event_msg/task_complete":
		set("idle")
		a.Tool, a.Detail = "", ""
	default:
		return
	}
	if a.StartedAt == 0 {
		a.StartedAt = ts
	}
	a.LastEventAt = ts
}

func (l *codexLog) push(e Event) {
	ev := append(l.agent.Events, e)
	if len(ev) > maxEvents {
		ev = append([]Event(nil), ev[len(ev)-maxEvents:]...)
	}
	l.agent.Events = ev
}

// codexSummarize 把 Codex 的参数 JSON 串转成 summarize 认得的形状：
// shell 类工具的 command 是 argv 数组，exec_command 用的是 cmd。
func codexSummarize(args string) string {
	var in map[string]any
	if json.Unmarshal([]byte(args), &in) != nil {
		return trunc(args, 80)
	}
	if argv, ok := in["command"].([]any); ok {
		parts := make([]string, 0, len(argv))
		for _, v := range argv {
			s, _ := v.(string)
			parts = append(parts, s)
		}
		// bash -lc <script> 只看 script。
		if n := len(parts); n >= 3 && parts[n-2] == "-lc" {
			parts = parts[n-1:]
		}
		in["command"] = strings.Join(parts, " ")
	} else if cmd, ok := in["cmd"].(string); ok {
		in["command"] = cmd
	}
	b, _ := json.Marshal(in)
	return summarize(b)
}

// codexOutput 取工具输出文本：output 可能是字符串，也可能是包着 {"output": ...} 的 JSON 串。
func codexOutput(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return string(raw)
	}
	var wrapped struct {
		Output string `json:"output"`
	}
	if json.Unmarshal([]byte(s), &wrapped) == nil && wrapped.Output != "" {
		return wrapped.Output
	}
	return s
}

type codexMeta struct {
	id, cwd   string
	startedAt int64
}

// CodexScanner 把 ~/.codex/sessions 下 cwd 等于本项目的 rollout 变成房间。
// 子 agent 与纸飞机留空：Codex 日志里还没有可对上的子 agent 元数据，有真实样本再补。
type CodexScanner struct {
	Project, Home, AliasFile string
	Stuck, History           time.Duration
	metas                    map[string]codexMeta // rollout 路径 -> 第一行 session_meta
	logs                     map[string]*codexLog
}

func NewCodexScanner(project, home, aliasFile string, stuck, history time.Duration) *CodexScanner {
	return &CodexScanner{Project: project, Home: home, AliasFile: aliasFile, Stuck: stuck, History: history, metas: map[string]codexMeta{}, logs: map[string]*codexLog{}}
}

func readCodexMeta(path string) (codexMeta, bool) {
	f, err := os.Open(path)
	if err != nil {
		return codexMeta{}, false
	}
	defer f.Close()
	// session_meta 带完整 base_instructions，可能远超 bufio.Scanner 的默认上限。
	line, err := bufio.NewReader(f).ReadBytes('\n')
	if err != nil {
		return codexMeta{}, false
	}
	var r codexLine
	if json.Unmarshal(line, &r) != nil {
		return codexMeta{}, false
	}
	if r.Type != "session_meta" {
		return codexMeta{}, true
	}
	m := codexMeta{id: r.Payload.ID, cwd: filepath.Clean(r.Payload.Cwd)}
	if m.id == "" {
		m.id = r.Payload.SessionID
	}
	if t, err := time.Parse(time.RFC3339Nano, r.Payload.Timestamp); err == nil {
		m.startedAt = t.UnixMilli()
	} else {
		m.startedAt, _ = r.ts()
	}
	return m, true
}

// codexThreadNames 每次扫描都热读，Codex 里改了线程名一秒内就能看到。
func codexThreadNames(path string) map[string]string {
	out := map[string]string{}
	data, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for _, ln := range strings.Split(string(data), "\n") {
		var e struct {
			ID   string `json:"id"`
			Name string `json:"thread_name"`
		}
		if json.Unmarshal([]byte(ln), &e) == nil && e.ID != "" && e.Name != "" {
			out[e.ID] = e.Name
		}
	}
	return out
}

func (s *CodexScanner) Scan(now time.Time) Snapshot {
	nowMs := now.UnixMilli()
	snap := Snapshot{Project: s.Project, GeneratedAt: nowMs, Rooms: []Room{}}
	root := filepath.Join(s.Home, ".codex")
	var paths []string
	// ponytail: 每秒全量 walk，会话目录涨到上万个文件再改成按日期目录剪枝。
	_ = filepath.WalkDir(filepath.Join(root, "sessions"), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".jsonl") {
			return nil
		}
		m, ok := s.metas[p]
		if !ok {
			if m, ok = readCodexMeta(p); !ok {
				return nil
			}
			s.metas[p] = m
		}
		if m.cwd == s.Project && m.id != "" {
			paths = append(paths, p)
		}
		return nil
	})
	if len(paths) == 0 {
		return snap
	}
	names := codexThreadNames(filepath.Join(root, "session_index.jsonl"))
	aliases := loadAliases(s.AliasFile)
	for _, p := range paths {
		m := s.metas[p]
		l, ok := s.logs[p]
		if !ok {
			l = newCodexLog()
			s.logs[p] = l
		}
		l.sync(p)
		last := l.agent.LastEventAt
		if last == 0 {
			last = m.startedAt
		}
		if nowMs-last > s.History.Milliseconds() {
			continue
		}
		name := names[m.id]
		if name == "" {
			name = m.id[:min(8, len(m.id))]
		}
		alias := aliases[m.id]
		if alias == "" {
			alias = aliases[name]
		}
		// Codex 没有进程注册表，只能拿最近有没有事件近似「会话在跑」。
		status := "idle"
		if l.agent.LastEventAt > 0 && nowMs-l.agent.LastEventAt <= s.Stuck.Milliseconds() {
			status = "busy"
		}
		main := finalize(l.agent, nowMs, s.Stuck)
		if status != "busy" && main.State == "stuck" {
			main.State = "idle"
		}
		snap.Rooms = append(snap.Rooms, Room{
			SessionID: m.id, Name: name, Alias: alias, Provider: "codex", Status: status,
			StartedAt: m.startedAt, Main: main, Agents: []Agent{}, Messages: []Msg{},
		})
	}
	return snap
}
