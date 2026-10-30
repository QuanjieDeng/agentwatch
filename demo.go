package main

import (
	"fmt"
	"time"
)

const demoGhost = 10 * time.Second

type demoAgent struct {
	id, typ, desc string
	tools         []string
	stuck         bool
	// finishAt > 0 makes the agent finish after that many ticks; the cycle
	// restarts every 4*finishAt ticks so the ghost removal stays observable.
	finishAt int
}

var demoRooms = []struct {
	id, name, alias, status, provider string
	agents                            []demoAgent
}{
	{"demo-0001-aaaa", "slg-server-3d", "3D 监控台", "busy", "claude", []demoAgent{
		{id: "a1", typ: "Explore", desc: "扫描 scenesvr 登录链路", tools: []string{"Grep", "Read", "Glob"}},
		{id: "a2", typ: "pstack:poteto-agent", desc: "实现 agentwatch 后端", tools: []string{"Write", "Bash", "Edit"}, finishAt: 4},
		{id: "a3", typ: "general-purpose", desc: "查线上日志", tools: []string{"Bash"}, stuck: true},
	}},
	{"demo-0002-bbbb", "corps-review", "公会复审", "idle", "codex", []demoAgent{
		{id: "b1", typ: "Plan", desc: "设计公会投影方案", tools: []string{"Read", "Grep"}},
		{id: "b2", typ: "agent-skills:code-reviewer", desc: "审查 corpssvr 改动", tools: []string{"Read", "Bash"}},
		{id: "b3", typ: "Explore", desc: "定位护盾结算", tools: []string{"Grep", "Read"}},
		{id: "b4", typ: "pstack:poteto-agent", desc: "补单测", tools: []string{"Edit", "Bash"}},
	}},
}

var demoCycle = []string{"thinking", "tool", "processing", "idle"}

type Demo struct {
	start time.Time
	stuck time.Duration
}

func (d *Demo) Scan(now time.Time) Snapshot {
	step := 2500 * time.Millisecond
	tick := int(now.Sub(d.start) / step)
	ms := func(t time.Time) int64 { return t.UnixMilli() }
	snap := Snapshot{Project: "/demo/project", GeneratedAt: ms(now), Rooms: []Room{}}
	for ri, r := range demoRooms {
		room := Room{SessionID: r.id, Name: r.name, Alias: r.alias, Provider: r.provider, Status: r.status, StartedAt: ms(d.start) - int64(ri+1)*600_000, Agents: []Agent{}, Messages: []Msg{}}
		room.Main = d.agent(demoAgent{id: "main", typ: "main", desc: "主 agent", tools: []string{"Agent", "Read"}}, tick+ri, now, step)
		room.Main.ParentID = ""
		for i, da := range r.agents {
			t := tick + i + ri
			if da.finishAt > 0 {
				t = tick % (da.finishAt * 4)
				finishedAt := now.Truncate(step).Add(-time.Duration(t-da.finishAt) * step)
				if t > da.finishAt && now.Sub(finishedAt) > demoGhost {
					continue
				}
			}
			room.Agents = append(room.Agents, d.agent(da, t, now, step))
		}
		// 每拍一条消息：派活、交回结果、跨房间打招呼轮着来，前端一直有纸飞机可看。
		if n := len(room.Agents); n > 0 {
			ts := d.start.UnixMilli() + int64(tick)*step.Milliseconds()
			switch tick % 4 {
			case 2:
				room.Messages = append(room.Messages, Msg{From: room.Agents[tick%n].ID, To: "main", Ts: ts})
			case 3:
				if ri == 1 {
					room.Messages = append(room.Messages, Msg{From: "main", To: "slg-server-3d", Ts: ts})
				}
			default:
				room.Messages = append(room.Messages, Msg{From: "main", To: room.Agents[(tick/2)%n].ID, Ts: ts})
			}
		}
		snap.Rooms = append(snap.Rooms, room)
	}
	return snap
}

func (d *Demo) agent(da demoAgent, t int, now time.Time, step time.Duration) Agent {
	since := now.Truncate(step)
	a := Agent{
		ID: da.id, ParentID: "main", Type: da.typ, Description: da.desc,
		StartedAt:   d.start.UnixMilli() - 30_000,
		LastEventAt: since.UnixMilli(),
		Since:       since.UnixMilli(),
	}
	if da.finishAt > 0 && t >= da.finishAt {
		a.LastEventAt = since.Add(-time.Duration(t-da.finishAt) * step).UnixMilli()
		a.Since = a.LastEventAt
		a.Finished = true
		t = da.finishAt
	}
	if da.stuck {
		t = 1
		a.LastEventAt = now.Add(-d.stuck - 30*time.Second).UnixMilli()
		a.Since = a.LastEventAt
	}
	a.Turns = t/3 + 1
	a.Tokens = Tokens{In: int64(t+1) * 3, Out: int64(t+1) * 420, CacheRead: int64(t+1) * 18_000}
	a.State = demoCycle[t%len(demoCycle)]
	for k := max(0, t-maxEvents+1); k <= t; k++ {
		a.Events = append(a.Events, demoEvent(da, k, a.Since-int64(t-k)*step.Milliseconds()))
	}
	if a.State == "tool" || a.State == "processing" {
		a.Tool = da.tools[t%len(da.tools)]
		a.Detail = fmt.Sprintf("/home/demo/file_%d.go", t)
	}
	return finalize(a, now.UnixMilli(), d.stuck)
}

func demoEvent(da demoAgent, k int, ts int64) Event {
	tool := da.tools[k%len(da.tools)]
	switch demoCycle[k%len(demoCycle)] {
	case "tool":
		return Event{Ts: ts, Kind: "tool", Tool: tool, Text: fmt.Sprintf("%s /home/demo/file_%d.go", tool, k)}
	case "processing":
		return Event{Ts: ts, Kind: "result", Text: fmt.Sprintf("ok (%d lines)", 10+k)}
	case "idle":
		return Event{Ts: ts, Kind: "text", Text: "呼呼大睡"}
	}
	return Event{Ts: ts, Kind: "thinking", Text: "考虑下一步：" + da.desc}
}
