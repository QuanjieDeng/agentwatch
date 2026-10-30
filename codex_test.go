package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeRollout(t *testing.T, home, name string, lines ...string) {
	t.Helper()
	dir := filepath.Join(home, ".codex", "sessions", "2026", "10", "30")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCodexRollout(t *testing.T) {
	home, project := t.TempDir(), "/work/proj"
	writeRollout(t, home, "rollout-a.jsonl",
		`{"timestamp":"2026-10-30T10:00:00.100Z","type":"session_meta","payload":{"id":"019a-sess-aaaa","session_id":"019a-sess-aaaa","timestamp":"2026-10-30T10:00:00Z","cwd":"/work/proj/","originator":"codex_cli_rs","source":"cli"}}`,
		`{"timestamp":"2026-10-30T10:00:01Z","type":"turn_context","payload":{"turn_id":"t1","cwd":"/work/proj","model":"gpt-5"}}`,
		`{"timestamp":"2026-10-30T10:00:01Z","type":"response_item","payload":{"type":"message","id":"u0","role":"developer","content":[{"type":"input_text","text":"<permissions>"}]}}`,
		`{"timestamp":"2026-10-30T10:00:01Z","type":"response_item","payload":{"type":"message","id":"u1","role":"user","content":[{"type":"input_text","text":"<environment_context>"}]}}`,
		`{"timestamp":"2026-10-30T10:00:01Z","type":"event_msg","payload":{"type":"user_message","message":"跑一下测试"}}`,
		`{"timestamp":"2026-10-30T10:00:02Z","type":"response_item","payload":{"type":"reasoning","encrypted_content":"xx"}}`,
		`{"timestamp":"2026-10-30T10:00:03Z","type":"response_item","payload":{"type":"function_call","call_id":"c1","name":"shell","arguments":"{\"command\":[\"bash\",\"-lc\",\"go test ./...\"],\"workdir\":\"/work/proj\"}"}}`,
		`{"timestamp":"2026-10-30T10:00:04Z","type":"response_item","payload":{"type":"function_call_output","call_id":"c1","output":"{\"output\":\"ok  agentwatch\",\"metadata\":{\"exit_code\":0}}"}}`,
		`{"timestamp":"2026-10-30T10:00:05Z","type":"response_item","payload":{"type":"message","id":"m1","role":"assistant","content":[{"type":"output_text","text":"测试全过"}]}}`,
		`{"timestamp":"2026-10-30T10:00:05Z","type":"response_item","payload":{"type":"message","id":"m1","role":"assistant","content":[{"type":"output_text","text":"测试全过"}]}}`,
		`{"timestamp":"2026-10-30T10:00:05Z","type":"event_msg","payload":{"type":"agent_message","message":"测试全过","phase":"final"}}`,
		`{"timestamp":"2026-10-30T10:00:05Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":80,"output_tokens":20,"reasoning_output_tokens":5}}}}`,
		`{"timestamp":"2026-10-30T10:00:06Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":300,"cached_input_tokens":200,"output_tokens":40,"reasoning_output_tokens":9}}}}`,
		`{"timestamp":"2026-10-30T10:00:06Z","type":"event_msg","payload":{"type":"task_complete"}}`,
	)
	writeRollout(t, home, "rollout-other.jsonl",
		`{"timestamp":"2026-10-30T10:00:00Z","type":"session_meta","payload":{"id":"other","timestamp":"2026-10-30T10:00:00Z","cwd":"/elsewhere"}}`)
	writeRollout(t, home, "rollout-old.jsonl",
		`{"timestamp":"2026-10-01T10:00:00Z","type":"session_meta","payload":{"id":"old","timestamp":"2026-10-01T10:00:00Z","cwd":"/work/proj"}}`)
	half := `{"timestamp":"2026-10-30T10:00:05Z","type":"session_meta","payload":{"id":"late","timestamp":"2026-10-30T10:00:05Z","cwd":"/work/proj"}}`
	halfPath := filepath.Join(home, ".codex", "sessions", "2026", "10", "30", "rollout-half.jsonl")
	os.WriteFile(halfPath, []byte(half[:40]), 0o644)
	os.WriteFile(filepath.Join(home, ".codex", "session_index.jsonl"),
		[]byte(`{"id":"019a-sess-aaaa","thread_name":"跑测试","updated_at":"2026-10-30T10:00:06Z"}`+"\n"), 0o644)

	s := NewCodexScanner(project, home, filepath.Join(home, "none.json"), 2*time.Minute, 24*time.Hour)
	last := time.Date(2026, 10, 30, 10, 0, 6, 0, time.UTC)
	snap := s.Scan(last.Add(10 * time.Second))
	if len(snap.Rooms) != 1 {
		t.Fatalf("rooms = %d, want 1 (other cwd and out-of-window sessions dropped)", len(snap.Rooms))
	}
	r := snap.Rooms[0]
	if r.Provider != "codex" || r.Name != "跑测试" || r.SessionID != "019a-sess-aaaa" || r.Status != "busy" {
		t.Errorf("room = provider %q name %q id %q status %q", r.Provider, r.Name, r.SessionID, r.Status)
	}
	if r.StartedAt != ms("2026-10-30T10:00:00Z") || r.Agents == nil || r.Messages == nil {
		t.Errorf("startedAt=%d agents=%v messages=%v", r.StartedAt, r.Agents, r.Messages)
	}
	a := r.Main
	if a.State != "idle" || a.Tool != "" || a.Turns != 1 {
		t.Errorf("main state=%q tool=%q turns=%d, want idle \"\" 1", a.State, a.Tool, a.Turns)
	}
	if want := (Tokens{In: 300, Out: 40, CacheRead: 200}); a.Tokens != want {
		t.Errorf("tokens = %+v, want %+v (total_token_usage is cumulative)", a.Tokens, want)
	}
	var kinds []string
	for _, e := range a.Events {
		kinds = append(kinds, e.Kind+":"+e.Text)
	}
	if got := strings.Join(kinds, "|"); got != "user:跑一下测试|tool:shell go test ./...|result:ok  agentwatch|text:测试全过" {
		t.Errorf("events = %s", got)
	}

	l := newCodexLog()
	l.apply([]byte(`{"timestamp":"2026-10-30T10:00:03Z","type":"response_item","payload":{"type":"function_call","call_id":"c1","name":"shell","arguments":"{\"command\":[\"bash\",\"-lc\",\"go test ./...\"]}"}}`))
	if l.agent.State != "tool" || l.agent.Tool != "shell" || l.agent.Detail != "go test ./..." {
		t.Errorf("function_call: state=%q tool=%q detail=%q", l.agent.State, l.agent.Tool, l.agent.Detail)
	}

	os.WriteFile(halfPath, []byte(half+"\n"), 0o644)
	if snap := s.Scan(last.Add(10 * time.Second)); len(snap.Rooms) != 2 {
		t.Errorf("rooms after half line completed = %d, want 2 (failed meta read must not be cached)", len(snap.Rooms))
	}

	if snap := s.Scan(last.Add(48 * time.Hour)); len(snap.Rooms) != 0 {
		t.Errorf("rooms after history window = %d, want 0", len(snap.Rooms))
	}
}
