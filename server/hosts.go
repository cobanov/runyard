package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
)

// Machines: `runyard agent` on each training machine posts what its GPUs, CPU, memory and disk are doing every
// few seconds, whether or not a run is training there. The server keeps the latest reading per host and a day of
// history, and tells the channels when a host goes silent and when it is back.

// hostSilentAfter is how long a host may go without a reading (the agent sends one every 10 s) before it counts as silent.
const hostSilentAfter = 2 * time.Minute

var hostName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$`)

type gpuProc struct {
	PID   int     `json:"pid"`
	Name  string  `json:"name"` // the program and its script, "python train.py"; never the whole command line
	User  string  `json:"user,omitempty"`
	MemGB float64 `json:"mem_gb"`
}

type gpuStat struct {
	I          int       `json:"i"`
	Name       string    `json:"name,omitempty"`
	Util       *float64  `json:"util,omitempty"`
	MemGB      *float64  `json:"mem_gb,omitempty"`
	MemTotalGB *float64  `json:"mem_total_gb,omitempty"`
	TempC      *float64  `json:"temp_c,omitempty"`
	PowerW     *float64  `json:"power_w,omitempty"`
	PowerMaxW  *float64  `json:"power_max_w,omitempty"`
	Procs      []gpuProc `json:"procs,omitempty"`
}

// hostStats is one reading; a field the machine cannot measure (no psutil, no NVIDIA GPU) is left out.
type hostStats struct {
	CPU         *float64  `json:"cpu,omitempty"` // percent of all cores
	Cores       int       `json:"cores,omitempty"`
	Load        *float64  `json:"load,omitempty"` // 1-minute load average
	RAMGB       *float64  `json:"ram_gb,omitempty"`
	RAMTotalGB  *float64  `json:"ram_total_gb,omitempty"`
	DiskGB      *float64  `json:"disk_gb,omitempty"`
	DiskTotalGB *float64  `json:"disk_total_gb,omitempty"`
	GPUs        []gpuStat `json:"gpus,omitempty"`
}

// flat is what the charts plot: the numbers that change, by the same names runs use under sys/.
func (h hostStats) flat() map[string]float64 {
	v := map[string]float64{}
	put := func(k string, f *float64) {
		if f != nil {
			v[k] = *f
		}
	}
	put("cpu", h.CPU)
	put("ram_gb", h.RAMGB)
	put("disk_gb", h.DiskGB)
	for _, g := range h.GPUs {
		p := fmt.Sprintf("gpu%d.", g.I)
		put(p+"util", g.Util)
		put(p+"mem_gb", g.MemGB)
		put(p+"temp_c", g.TempC)
		put(p+"power_w", g.PowerW)
	}
	return v
}

type hostRun struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Project string `json:"project"`
}

type host struct {
	Name   string          `json:"name"`
	SeenAt time.Time       `json:"seen_at"`
	Silent bool            `json:"silent"`
	Stats  json.RawMessage `json:"stats"`
	Runs   []hostRun       `json:"runs"` // running runs that started on this host, those the request may see
}

func (s *srv) hostRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/hosts/{name}", s.write(s.postHost))
	mux.HandleFunc("GET /api/v1/hosts", s.read(func(r *http.Request) (any, error) { return listHosts(r.Context(), s) }))
	mux.HandleFunc("GET /api/v1/hosts/{name}/history", s.read(s.hostHistory))
	mux.HandleFunc("DELETE /api/v1/hosts/{name}", s.admin(func(r *http.Request) (any, error) {
		err := exec(r.Context(), s.db, "host", `DELETE FROM hosts WHERE name = $1`, r.PathValue("name"))
		if err == nil {
			s.hub.publish(event{Type: "host"})
		}
		return struct{}{}, err
	}))
}

func (s *srv) postHost(r *http.Request) (any, error) {
	name := r.PathValue("name")
	if !hostName.MatchString(name) {
		return nil, badRequest{errors.New("a host name is letters, digits, dots, dashes and underscores")}
	}
	var st hostStats
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&st); err != nil {
		return nil, badRequest{err}
	}
	ctx := r.Context()
	var wasSilent bool
	err := s.db.QueryRow(ctx, `WITH old AS (SELECT silent FROM hosts WHERE name = $1)
		INSERT INTO hosts (name, seen_at, stats) VALUES ($1, now(), $2)
		ON CONFLICT (name) DO UPDATE SET seen_at = now(), stats = excluded.stats, silent = false
		RETURNING coalesce((SELECT silent FROM old), false)`, name, st).Scan(&wasSilent)
	if err != nil {
		return nil, err
	}
	if _, err := s.db.Exec(ctx, `INSERT INTO host_samples (host, vals) VALUES ($1, $2) ON CONFLICT DO NOTHING`, name, st.flat()); err != nil {
		return nil, err
	}
	if wasSilent {
		go s.notifyHost(name+" is back", "Machine stats are arriving again.", false)
	}
	s.hub.publish(event{Type: "host"})
	return map[string]bool{"ok": true}, nil
}

func listHosts(ctx context.Context, s *srv) ([]host, error) {
	rows, _ := s.db.Query(ctx, `SELECT h.name, h.seen_at, h.seen_at < now() - $2 * interval '1 second', h.stats,
			coalesce(json_agg(json_build_object('id', r.id, 'name', r.name, 'project', r.project) ORDER BY r.started_at)
				FILTER (WHERE r.id IS NOT NULL), '[]')
		FROM hosts h LEFT JOIN (SELECT r.id, r.name, p.name AS project, r.host, r.started_at FROM runs r JOIN projects p ON p.id = r.project_id
			WHERE r.status = 'running' AND ($1::bigint[] IS NULL OR p.team_id IS NULL OR p.team_id = ANY($1))) r ON r.host = h.name
		GROUP BY h.name ORDER BY h.name`, vis(ctx), int(hostSilentAfter.Seconds()))
	return pgx.CollectRows(rows, pgx.RowToStructByPos[host])
}

// hostHistory returns a host's readings over the last ?hours (1 to 24, default 24) as at most 360 averaged points:
// {"ts": [unix seconds], "values": {"cpu": [...], "gpu0.util": [...]}}, a null where a bucket has no value.
func (s *srv) hostHistory(r *http.Request) (any, error) {
	hours := min(max(intParam(r, "hours", 24), 1), 24)
	rows, _ := s.db.Query(r.Context(), `SELECT extract(epoch FROM date_bin($2 * interval '1 second', s.ts, timestamptz 'epoch'))::bigint, kv.key, avg(kv.value::float8)
		FROM host_samples s, jsonb_each_text(s.vals) kv
		WHERE s.host = $1 AND s.ts > now() - $3 * interval '1 hour' GROUP BY 1, 2 ORDER BY 1`, r.PathValue("name"), hours*10, hours)
	out := struct {
		TS     []int64               `json:"ts"`
		Values map[string][]*float64 `json:"values"`
	}{TS: []int64{}, Values: map[string][]*float64{}}
	var t int64
	var k string
	var v float64
	_, err := pgx.ForEachRow(rows, []any{&t, &k, &v}, func() error {
		if n := len(out.TS); n == 0 || out.TS[n-1] != t {
			out.TS = append(out.TS, t)
		}
		col := out.Values[k]
		for len(col) < len(out.TS)-1 {
			col = append(col, nil)
		}
		val := v
		out.Values[k] = append(col, &val)
		return nil
	})
	for k, col := range out.Values {
		for len(col) < len(out.TS) {
			col = append(col, nil)
		}
		out.Values[k] = col
	}
	return out, err
}

// markSilentHosts tells the channels about each host whose agent stopped posting, once until it is back, and drops
// readings older than a day.
func (s *srv) markSilentHosts(ctx context.Context) {
	rows, _ := s.db.Query(ctx, `UPDATE hosts SET silent = true WHERE NOT silent AND seen_at < now() - $1 * interval '1 second'
		RETURNING name, seen_at`, int(hostSilentAfter.Seconds()))
	var name string
	var seen time.Time
	_, err := pgx.ForEachRow(rows, []any{&name, &seen}, func() error {
		go s.notifyHost(name+" went silent", fmt.Sprintf("No machine stats for %s: the machine may be down, or runyard agent stopped.",
			fmtDuration(time.Since(seen))), true)
		s.hub.publish(event{Type: "host"})
		return nil
	})
	if err != nil {
		log.Print("silent hosts: ", err)
	}
	s.db.Exec(ctx, `DELETE FROM host_samples WHERE ts < now() - interval '24 hours'`)
}

// notifyHost sends a machine notice to the channels that hear every project's alerts. Runs in its own goroutine.
func (s *srv) notifyHost(title, body string, bad bool) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	rows, _ := s.db.Query(ctx, `SELECT DISTINCT c.id, c.kind, c.config FROM rules r JOIN channels c ON c.id = r.channel_id
		WHERE r.event = 'alert' AND r.project_id IS NULL`)
	chans, err := pgx.CollectRows(rows, pgx.RowToStructByPos[channel])
	if err != nil {
		log.Printf("notify %s: %v", title, err)
	}
	m := message{Event: "host", Title: title, Body: body, priority: "default", tags: "white_check_mark"}
	if bad {
		m.priority, m.tags = "high", "warning"
	}
	if s.url != "" {
		m.URL = s.url + "/#/machines"
	}
	s.deliver(ctx, chans, m)
}
