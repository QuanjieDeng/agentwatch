package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type scanner interface{ Scan(time.Time) Snapshot }

// multiScanner 把 Claude Code 与 Codex 两份快照并成一份，房间顺序仍按 PROTOCOL 的 startedAt 升序。
type multiScanner []scanner

func (m multiScanner) Scan(now time.Time) Snapshot {
	out := Snapshot{GeneratedAt: now.UnixMilli(), Rooms: []Room{}}
	for _, part := range m {
		s := part.Scan(now)
		if out.Project == "" {
			out.Project = s.Project
		}
		out.Rooms = append(out.Rooms, s.Rooms...)
	}
	sort.SliceStable(out.Rooms, func(i, j int) bool { return out.Rooms[i].StartedAt < out.Rooms[j].StartedAt })
	return out
}

func main() {
	cwd, _ := os.Getwd()
	project := flag.String("project", cwd, "project directory whose Claude Code and Codex sessions are watched")
	port := flag.Int("port", 7788, "HTTP port")
	demo := flag.Bool("demo", false, "serve synthetic data instead of scanning")
	stuck := flag.Duration("stuck", 120*time.Second, "silence after which an unfinished agent is stuck")
	ghost := flag.Duration("ghost", 180*time.Second, "how long a finished subagent stays visible")
	aliasFile := flag.String("aliases", "aliases.json", "session alias map, keyed by sessionId or session name")
	codexHistory := flag.Duration("codex-history", 24*time.Hour, "hide Codex sessions whose last event is older than this")
	flag.Parse()

	abs, err := filepath.Abs(*project)
	if err != nil {
		log.Fatal(err)
	}
	abs = filepath.Clean(abs)
	home, _ := os.UserHomeDir()

	var src scanner = multiScanner{
		NewScanner(abs, home, *aliasFile, *stuck, *ghost),
		NewCodexScanner(abs, home, *aliasFile, *stuck, *codexHistory),
	}
	if *demo {
		src = &Demo{start: time.Now(), stuck: *stuck}
	}

	var (
		mu   sync.RWMutex
		last []byte
	)
	refresh := func() Snapshot {
		snap := src.Scan(time.Now())
		b, _ := json.Marshal(snap)
		mu.Lock()
		last = b
		mu.Unlock()
		return snap
	}
	first := refresh()
	fmt.Fprintf(os.Stderr, "agentwatch: project=%s logdir=%s sessions=%d demo=%v\n", abs, projectLogDir(home, abs), len(first.Rooms), *demo)
	go func() {
		for range time.Tick(time.Second) {
			refresh()
		}
	}()
	current := func() []byte {
		mu.RLock()
		defer mu.RUnlock()
		return last
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/snapshot", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(current())
	})
	mux.HandleFunc("GET /events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			if _, err := fmt.Fprintf(w, "data: %s\n\n", current()); err != nil {
				return
			}
			http.NewResponseController(w).Flush()
			select {
			case <-r.Context().Done():
				return
			case <-t.C:
			}
		}
	})
	// ponytail: http.Dir resolves on each request, so a missing web/ just 404s.
	exe, _ := os.Executable()
	webDir := "web"
	if _, err := os.Stat(webDir); err != nil {
		webDir = filepath.Join(filepath.Dir(exe), "web")
	}
	mux.Handle("GET /", http.FileServer(http.Dir(webDir)))

	addr := fmt.Sprintf(":%d", *port)
	fmt.Fprintf(os.Stderr, "agentwatch: listening on http://localhost%s\n", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
