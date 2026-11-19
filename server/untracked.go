package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
)

// Untracked training: the beacon already lists the processes on each GPU. A process that keeps a GPU busy for ten
// minutes on a machine where no run is running is most likely a training that logs nowhere, the kind that is
// noticed days later with nothing recorded. The Machines page marks it and the channels that hear every project's
// alerts are told once. Long-lived services (ComfyUI, an LLM server) are what makes this noisy, so their names can
// be ignored, from the Machines page.

const (
	untrackedAfter = 10 * time.Minute
	busyUtil       = 40.0 // percent: a GPU working, not one holding a model in memory
	busyMemGB      = 2.0  // a process (or, where processes have no memory figure, the GPU) holding at least this
)

// ignoredByDefault are services seen on GPUs all day; matched against the start of a process name.
var ignoredByDefault = []string{"ollama", "llama-server", "vllm", "tritonserver", "text-generation-launcher", "Xorg", "gnome-shell"}

type untrackedProc struct {
	PID   int       `json:"pid"`
	Name  string    `json:"name"`
	User  string    `json:"user,omitempty"`
	GPU   int       `json:"gpu"`
	MemGB float64   `json:"mem_gb,omitempty"` // 0 where the driver does not say (WSL)
	Since time.Time `json:"since"`
}

type watched struct {
	untrackedProc
	seen, busy int
}

// untrackedWatch holds what each host's processes have been doing since the server started.
type untrackedWatch struct {
	mu    sync.Mutex
	procs map[string]map[int]*watched // host -> pid
	known map[string][]untrackedProc  // host -> what is flagged, as stored
}

var watch = untrackedWatch{procs: map[string]map[int]*watched{}, known: map[string][]untrackedProc{}}

// forget drops a deleted host's state; it starts over if the host posts again.
func (w *untrackedWatch) forget(host string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.procs, host)
	delete(w.known, host)
}

// ignoreList is the defaults plus the names added from the Machines page.
func (s *srv) ignoreList(ctx context.Context) []string {
	var v string
	var added []string
	if s.db.QueryRow(ctx, `SELECT value FROM settings WHERE key = 'untracked_ignore'`).Scan(&v) == nil {
		json.Unmarshal([]byte(v), &added)
	}
	return append(slices.Clone(ignoredByDefault), added...)
}

func ignored(name string, list []string) bool {
	return slices.ContainsFunc(list, func(p string) bool { return p != "" && strings.HasPrefix(name, p) })
}

// checkUntracked looks at one reading of host. It is called for every beacon post, so it asks the database only
// when a GPU is busy.
func (s *srv) checkUntracked(ctx context.Context, host string, st hostStats) {
	type candidate struct {
		p    gpuProc
		gpu  int
		busy bool
	}
	var cands []candidate
	var list []string
	for _, g := range st.GPUs {
		busy := g.Util != nil && *g.Util >= busyUtil
		known := slices.ContainsFunc(g.Procs, func(p gpuProc) bool { return p.MemGB > 0 })
		gpuHeld := g.MemGB != nil && *g.MemGB >= busyMemGB
		for _, p := range g.Procs {
			// Where no process has a memory figure (WSL), the GPU's own stands in for all of them.
			if (known && p.MemGB < busyMemGB) || (!known && !gpuHeld) {
				continue
			}
			if list == nil {
				list = s.ignoreList(ctx)
			}
			if !ignored(p.Name, list) {
				cands = append(cands, candidate{p, g.I, busy})
			}
		}
	}
	tracked := false
	if len(cands) > 0 {
		s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM runs WHERE status = 'running' AND host = $1)`, host).Scan(&tracked)
	}

	watch.mu.Lock()
	known, loaded := watch.known[host]
	if !loaded {
		var raw []byte
		s.db.QueryRow(ctx, `SELECT untracked FROM hosts WHERE name = $1`, host).Scan(&raw)
		json.Unmarshal(raw, &known)
		watch.known[host] = known
	}
	procs := watch.procs[host]
	if procs == nil || tracked {
		procs = map[int]*watched{} // a run is logging here: whatever runs on the GPUs is its
		watch.procs[host] = procs
	}
	now := time.Now()
	var flagged []untrackedProc
	if !tracked {
		present := map[int]bool{}
		for _, c := range cands {
			present[c.p.PID] = true
			w := procs[c.p.PID]
			if w == nil {
				w = &watched{untrackedProc: untrackedProc{PID: c.p.PID, Since: now}}
				if i := slices.IndexFunc(known, func(k untrackedProc) bool { return k.PID == c.p.PID }); i >= 0 {
					w.Since = known[i].Since // flagged before a restart: still flagged, not news
					w.seen, w.busy = 1, 1
				}
				procs[c.p.PID] = w
			}
			w.Name, w.User, w.GPU, w.MemGB = c.p.Name, c.p.User, c.gpu, c.p.MemGB
			w.seen++
			if c.busy {
				w.busy++
			}
		}
		for pid, w := range procs {
			if !present[pid] {
				delete(procs, pid) // ended, ignored, or let go of the GPU
			} else if now.Sub(w.Since) >= untrackedAfter && w.busy*2 >= w.seen {
				flagged = append(flagged, w.untrackedProc)
			}
		}
	}
	slices.SortFunc(flagged, func(a, b untrackedProc) int { return a.PID - b.PID })
	var news []untrackedProc
	for _, f := range flagged {
		if !slices.ContainsFunc(known, func(k untrackedProc) bool { return k.PID == f.PID }) {
			news = append(news, f)
		}
	}
	changed := len(flagged) != len(known) || len(news) > 0
	if changed {
		watch.known[host] = flagged
	}
	watch.mu.Unlock()

	if !changed {
		return
	}
	b, _ := json.Marshal(flagged)
	if string(b) == "null" {
		b = []byte("[]")
	}
	if _, err := s.db.Exec(ctx, `UPDATE hosts SET untracked = $2 WHERE name = $1`, host, b); err != nil {
		log.Print("untracked: ", err)
	}
	s.hub.publish(event{Type: "host"})
	if os.Getenv("RUNYARD_UNTRACKED_NOTIFY") == "0" {
		return
	}
	for _, f := range news {
		go s.notifyUntracked(host, f)
	}
}

func (s *srv) notifyUntracked(host string, p untrackedProc) {
	who := fmt.Sprintf("pid %d", p.PID)
	if p.User != "" {
		who += ", " + p.User
	}
	name := p.Name
	if name == "" {
		name = "A process"
	}
	m := message{Event: "untracked", Title: host + ": Training without runyard", priority: "high", tags: "warning",
		Body: fmt.Sprintf("%s (%s) has kept GPU %d busy for %s, and no run is logging on this machine. Start it with "+
			"`runyard run --` or ry.init() to keep its record. If it is a service, not a training, ignore it on the Machines page.",
			name, who, p.GPU, fmtDuration(time.Since(p.Since)))}
	if s.url != "" {
		m.URL = s.url + "/#/machines"
	}
	s.notifyAll(m)
}

// ignoreProcess adds a process name to the ignore list and drops it from every host's flags.
func (s *srv) ignoreProcess(r *http.Request) (any, error) {
	var in struct {
		Name string `json:"name"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if in.Name = strings.TrimSpace(in.Name); in.Name == "" || len(in.Name) > 200 {
		return nil, badRequest{errors.New("name is the process name as the Machines page shows it")}
	}
	ctx := r.Context()
	list := s.ignoreList(ctx)[len(ignoredByDefault):]
	if !slices.Contains(list, in.Name) {
		list = append(list, in.Name)
	}
	b, _ := json.Marshal(list)
	if _, err := s.db.Exec(ctx, `INSERT INTO settings (key, value) VALUES ('untracked_ignore', $1)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, string(b)); err != nil {
		return nil, err
	}
	watch.mu.Lock()
	for host, known := range watch.known {
		watch.known[host] = slices.DeleteFunc(known, func(k untrackedProc) bool { return strings.HasPrefix(k.Name, in.Name) })
		for pid, w := range watch.procs[host] {
			if strings.HasPrefix(w.Name, in.Name) {
				delete(watch.procs[host], pid)
			}
		}
	}
	watch.mu.Unlock()
	_, err := s.db.Exec(ctx, `UPDATE hosts SET untracked = coalesce((SELECT jsonb_agg(u) FROM jsonb_array_elements(untracked) u
		WHERE NOT starts_with(u->>'name', $1)), '[]')`, in.Name)
	s.hub.publish(event{Type: "host"})
	return map[string][]string{"ignored": list}, err
}
