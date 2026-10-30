package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	lineUser   = `{"type":"user","isSidechain":true,"timestamp":"2026-10-29T13:00:00Z","message":{"role":"user","content":"去查登录链路"}}`
	lineThink  = `{"type":"assistant","isSidechain":true,"timestamp":"2026-10-29T13:00:01Z","message":{"id":"m1","content":[{"type":"thinking","thinking":"嗯"}],"usage":{"input_tokens":2,"output_tokens":10,"cache_read_input_tokens":100}}}`
	lineTool   = `{"type":"assistant","isSidechain":true,"timestamp":"2026-10-29T13:00:02Z","message":{"id":"m1","content":[{"type":"tool_use","name":"Bash","input":{"command":"go vet ./...\nsecond line"}}],"usage":{"input_tokens":2,"output_tokens":10,"cache_read_input_tokens":100}}}`
	lineResult = `{"type":"user","isSidechain":true,"timestamp":"2026-10-29T13:00:03Z","message":{"content":[{"type":"tool_result","content":[{"type":"text","text":"ok"}]}]}}`
	lineText   = `{"type":"assistant","isSidechain":true,"timestamp":"2026-10-29T13:00:04Z","message":{"id":"m2","content":[{"type":"text","text":"完成"}],"usage":{"input_tokens":5,"output_tokens":7}}}`
)

func ms(s string) int64 { t, _ := time.Parse(time.RFC3339, s); return t.UnixMilli() }

func TestStateTransitionsAndTokens(t *testing.T) {
	l := newAgentLog(Agent{ID: "x"})
	if l.agent.State != "idle" {
		t.Fatalf("cold start state = %q, want idle", l.agent.State)
	}
	steps := []struct {
		line, state, tool string
	}{
		{lineUser, "thinking", ""},
		{lineThink, "thinking", ""},
		{lineTool, "tool", "Bash"},
		{lineResult, "processing", "Bash"},
		{lineText, "thinking", ""},
	}
	for _, s := range steps {
		l.apply([]byte(s.line), true)
		if l.agent.State != s.state || l.agent.Tool != s.tool {
			t.Fatalf("after %s: state=%q tool=%q, want %q %q", s.line[:40], l.agent.State, l.agent.Tool, s.state, s.tool)
		}
	}
	a := l.agent
	if a.Turns != 2 {
		t.Errorf("turns = %d, want 2 (m1 split over two lines counts once)", a.Turns)
	}
	if want := (Tokens{In: 7, Out: 17, CacheRead: 100}); a.Tokens != want {
		t.Errorf("tokens = %+v, want %+v", a.Tokens, want)
	}
	if a.StartedAt != ms("2026-10-29T13:00:00Z") || a.LastEventAt != ms("2026-10-29T13:00:04Z") {
		t.Errorf("startedAt=%d lastEventAt=%d", a.StartedAt, a.LastEventAt)
	}
	kinds := []string{}
	for _, e := range a.Events {
		kinds = append(kinds, e.Kind+":"+e.Text)
	}
	if got := strings.Join(kinds, "|"); got != "user:去查登录链路|thinking:嗯|tool:Bash go vet ./...|result:ok|text:完成" {
		t.Errorf("events = %s", got)
	}
}

func TestMainSkipsSidechainAndGarbage(t *testing.T) {
	l := newAgentLog(Agent{ID: "main"})
	l.apply([]byte(lineTool), false)
	l.apply([]byte(`{not json`), false)
	l.apply([]byte(`{"type":"attachment","timestamp":"2026-10-29T13:00:00Z"}`), false)
	if l.agent.LastEventAt != 0 || l.agent.State != "idle" {
		t.Fatalf("main consumed a filtered line: %+v", l.agent)
	}
}

func TestFinalizeOverlays(t *testing.T) {
	base := Agent{State: "tool", LastEventAt: 1_000_000}
	cases := []struct {
		name string
		a    Agent
		now  int64
		want string
	}{
		{"fresh", base, 1_000_000 + 5_000, "tool"},
		{"stuck", base, 1_000_000 + 121_000, "stuck"},
		{"done beats stuck", Agent{State: "tool", LastEventAt: 1, Finished: true}, 1_000_000, "done"},
		{"cold", Agent{State: "idle"}, 1_000_000, "idle"},
	}
	for _, c := range cases {
		if got := finalize(c.a, c.now, 120*time.Second).State; got != c.want {
			t.Errorf("%s: state=%q want %q", c.name, got, c.want)
		}
	}
}

func TestTailerIncrementalAndTruncate(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.jsonl")
	var tl tailer
	write := func(s string, flag int) {
		f, _ := os.OpenFile(p, flag|os.O_WRONLY|os.O_CREATE, 0o644)
		f.WriteString(s)
		f.Close()
	}
	write("one\ntw", os.O_TRUNC)
	if lines, _ := tl.read(p); len(lines) != 1 || string(lines[0]) != "one" {
		t.Fatalf("first read = %q", lines)
	}
	write("o\n", os.O_APPEND)
	if lines, _ := tl.read(p); len(lines) != 1 || string(lines[0]) != "two" {
		t.Fatalf("partial line not joined: %q", lines)
	}
	write("x\n", os.O_TRUNC)
	lines, truncated := tl.read(p)
	if !truncated || len(lines) != 1 || string(lines[0]) != "x" {
		t.Fatalf("truncate: %q %v", lines, truncated)
	}
}

func TestScanEndToEnd(t *testing.T) {
	home := t.TempDir()
	project := "/work/slg_server"
	sid := "11112222-3333-4444-5555-666677778888"
	logDir := filepath.Join(home, ".claude", "projects", "-work-slg-server")
	sub := filepath.Join(logDir, sid, "subagents")
	must(t, os.MkdirAll(sub, 0o755))
	must(t, os.MkdirAll(filepath.Join(home, ".claude", "sessions"), 0o755))
	reg := `{"pid":` + strconv.Itoa(os.Getpid()) + `,"sessionId":"` + sid + `","cwd":"` + project + `","status":"busy","name":"","startedAt":42}`
	must(t, os.WriteFile(filepath.Join(home, ".claude", "sessions", "1.json"), []byte(reg), 0o644))
	must(t, os.WriteFile(filepath.Join(home, ".claude", "sessions", "2.json"), []byte(`{"pid":1,"cwd":"/other","sessionId":"zzz"}`), 0o644))
	mainLine := strings.Replace(lineTool, `"isSidechain":true`, `"isSidechain":false`, 1)
	must(t, os.WriteFile(filepath.Join(logDir, sid+".jsonl"), []byte(mainLine+"\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(sub, "agent-abc.jsonl"), []byte(lineUser+"\n"+lineTool+"\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(sub, "agent-abc.meta.json"), []byte(`{"agentType":"Explore","description":"d"}`), 0o644))
	must(t, os.WriteFile(filepath.Join(sub, "agent-old.jsonl"), []byte(lineText+"\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(sub, "agent-old.meta.json"), []byte(`{"agentType":"Plan","finished":true}`), 0o644))

	aliasFile := filepath.Join(home, "aliases.json")
	must(t, os.WriteFile(aliasFile, []byte(`{"`+sid+`":"按会话ID","11112222":"按名字"}`), 0o644))
	s := NewScanner(project, home, aliasFile, 120*time.Second, 180*time.Second)
	snap := s.Scan(time.UnixMilli(ms("2026-10-29T13:00:10Z")))
	if len(snap.Rooms) != 1 {
		t.Fatalf("rooms = %d, want 1", len(snap.Rooms))
	}
	r := snap.Rooms[0]
	if r.Name != "11112222" || r.Alias != "按会话ID" || r.Status != "busy" || r.StartedAt != 42 {
		t.Errorf("room = %+v", r)
	}
	if r.Main.ID != "main" || r.Main.State != "tool" || r.Main.Tool != "Bash" {
		t.Errorf("main = %+v", r.Main)
	}
	if len(r.Agents) != 2 || r.Agents[0].ID != "abc" || r.Agents[0].Type != "Explore" || r.Agents[1].State != "done" {
		t.Fatalf("agents = %+v", r.Agents)
	}

	must(t, os.WriteFile(aliasFile, []byte(`{"11112222":"按名字"}`), 0o644))
	later := s.Scan(time.UnixMilli(ms("2026-10-29T13:05:00Z")))
	if a := later.Rooms[0].Agents; len(a) != 1 || a[0].ID != "abc" || a[0].State != "stuck" {
		t.Fatalf("after ghost window: %+v", a)
	}
	if got := later.Rooms[0].Alias; got != "按名字" {
		t.Errorf("alias by session name = %q, want 按名字", got)
	}
}

func TestMessages(t *testing.T) {
	home := t.TempDir()
	project := "/work/slg_server"
	sid := "11112222-3333-4444-5555-666677778888"
	logDir := filepath.Join(home, ".claude", "projects", "-work-slg-server")
	sub := filepath.Join(logDir, sid, "subagents")
	must(t, os.MkdirAll(sub, 0o755))
	must(t, os.MkdirAll(filepath.Join(home, ".claude", "sessions"), 0o755))
	reg := `{"pid":` + strconv.Itoa(os.Getpid()) + `,"sessionId":"` + sid + `","cwd":"` + project + `","status":"busy","startedAt":42}`
	must(t, os.WriteFile(filepath.Join(home, ".claude", "sessions", "1.json"), []byte(reg), 0o644))

	mainLines := strings.Join([]string{
		`{"type":"assistant","isSidechain":false,"timestamp":"2026-10-29T13:00:00Z","message":{"id":"m1","content":[{"type":"tool_use","id":"tu1","name":"Agent","input":{"description":"d"}}],"usage":{}}}`,
		`{"type":"assistant","isSidechain":false,"timestamp":"2026-10-29T13:00:05Z","message":{"id":"m2","content":[{"type":"tool_use","id":"tu2","name":"SendMessage","input":{"to":"other-room","summary":"hi"}}],"usage":{}}}`,
	}, "\n") + "\n"
	must(t, os.WriteFile(filepath.Join(logDir, sid+".jsonl"), []byte(mainLines), 0o644))
	must(t, os.WriteFile(filepath.Join(sub, "agent-abc.jsonl"), []byte(lineUser+"\n"+lineText+"\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(sub, "agent-abc.meta.json"), []byte(`{"agentType":"Explore","description":"d","finished":true,"toolUseId":"tu1"}`), 0o644))

	s := NewScanner(project, home, filepath.Join(home, "aliases.json"), 120*time.Second, 180*time.Second)
	snap := s.Scan(time.UnixMilli(ms("2026-10-29T13:00:10Z")))
	got := []string{}
	for _, m := range snap.Rooms[0].Messages {
		got = append(got, m.From+"->"+m.To+"@"+strconv.FormatInt(m.Ts-ms("2026-10-29T13:00:00Z"), 10))
	}
	want := "main->abc@0 abc->main@4000 main->other-room@5000"
	if strings.Join(got, " ") != want {
		t.Errorf("messages = %q, want %q", strings.Join(got, " "), want)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
