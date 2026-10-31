package main

import (
	"context"
	"net/http"
	"testing"
)

func TestHosts(t *testing.T) {
	ts, key, s := testServer(t)
	api := ts.URL + "/api/v1"
	ctx := context.Background()

	reading := map[string]any{"cpu": 12.5, "ram_gb": 30, "gpus": []map[string]any{
		{"i": 0, "name": "RTX", "util": 97, "mem_gb": 80, "procs": []map[string]any{{"pid": 42, "name": "python train.py", "user": "ege", "mem_gb": 79}}},
	}}
	if c := call(t, "POST", api+"/hosts/toji", "", reading, nil); c != http.StatusUnauthorized {
		t.Fatalf("post without a key: %d", c)
	}
	if c := call(t, "POST", api+"/hosts/bad%20name", key, reading, nil); c != http.StatusBadRequest {
		t.Fatalf("bad host name: %d", c)
	}
	for range 2 {
		if c := call(t, "POST", api+"/hosts/toji", key, reading, nil); c != 200 {
			t.Fatalf("post: %d", c)
		}
	}
	var run struct{ ID string }
	call(t, "POST", api+"/runs", key, map[string]any{"project": "p", "name": "r1", "host": "toji"}, &run)

	var hosts []struct {
		Name   string
		Silent bool
		Stats  struct {
			CPU  float64
			GPUs []struct {
				Util  float64
				Procs []struct{ PID int }
			}
		}
		Runs []struct{ ID, Name, Project string }
	}
	call(t, "GET", api+"/hosts", "", nil, &hosts)
	if len(hosts) != 1 || hosts[0].Name != "toji" || hosts[0].Silent || hosts[0].Stats.CPU != 12.5 ||
		hosts[0].Stats.GPUs[0].Util != 97 || hosts[0].Stats.GPUs[0].Procs[0].PID != 42 {
		t.Fatalf("hosts: %+v", hosts)
	}
	if len(hosts[0].Runs) != 1 || hosts[0].Runs[0].ID != run.ID || hosts[0].Runs[0].Project != "p" {
		t.Fatalf("runs on the host: %+v", hosts[0].Runs)
	}

	var hist struct {
		TS     []int64
		Values map[string][]*float64
	}
	call(t, "GET", api+"/hosts/toji/history?hours=1", "", nil, &hist)
	if len(hist.TS) == 0 || len(hist.Values["gpu0.util"]) != len(hist.TS) || *hist.Values["gpu0.util"][0] != 97 || *hist.Values["cpu"][0] != 12.5 {
		t.Fatalf("history: %+v", hist)
	}

	// A host that stops posting goes silent once, and is back with its next reading.
	s.db.Exec(ctx, `UPDATE hosts SET seen_at = now() - interval '10 minutes'`)
	s.markSilentHosts(ctx)
	call(t, "GET", api+"/hosts", "", nil, &hosts)
	var flagged bool
	s.db.QueryRow(ctx, `SELECT silent FROM hosts WHERE name = 'toji'`).Scan(&flagged)
	if !hosts[0].Silent || !flagged {
		t.Fatalf("silent: %v, flagged %v", hosts[0].Silent, flagged)
	}
	call(t, "POST", api+"/hosts/toji", key, reading, nil)
	s.db.QueryRow(ctx, `SELECT silent FROM hosts WHERE name = 'toji'`).Scan(&flagged)
	if flagged {
		t.Fatal("still flagged silent after a reading")
	}

	if c := call(t, "DELETE", api+"/hosts/toji", key, nil, nil); c != 200 {
		t.Fatalf("delete: %d", c)
	}
	call(t, "GET", api+"/hosts", "", nil, &hosts)
	if len(hosts) != 0 {
		t.Fatalf("after delete: %+v", hosts)
	}
}

func TestCurate(t *testing.T) {
	ts, key, _ := testServer(t)
	api := ts.URL + "/api/v1"
	var run struct{ ID string }
	call(t, "POST", api+"/runs", key, map[string]any{"project": "p", "name": "r1"}, &run)

	// Without RUNYARD_AUTH the web UI edits notes and tags without a key, as it reads without one.
	if c := call(t, "PATCH", api+"/runs/"+run.ID+"/meta", "", map[string]any{"notes": "lr too high", "tags": []string{" a ", "b", "a", ""}}, nil); c != 200 {
		t.Fatalf("meta: %d", c)
	}
	var got struct {
		Notes string
		Tags  []string
	}
	call(t, "GET", api+"/runs/"+run.ID, "", nil, &got)
	if got.Notes != "lr too high" || len(got.Tags) != 2 || got.Tags[0] != "a" || got.Tags[1] != "b" {
		t.Fatalf("after meta: %+v", got)
	}

	for _, title := range []string{"one", "two"} {
		call(t, "POST", api+"/runs/"+run.ID+"/alerts", key, map[string]any{"title": title}, nil)
	}
	var alerts []struct {
		ID        int64
		Title     string
		Dismissed *string `json:"dismissed_at"`
	}
	call(t, "GET", api+"/alerts", "", nil, &alerts)
	var out struct{ Dismissed int64 }
	call(t, "POST", api+"/alerts/dismiss", "", map[string]any{"ids": []int64{alerts[0].ID}}, &out)
	if out.Dismissed != 1 {
		t.Fatalf("dismissed %d", out.Dismissed)
	}
	call(t, "GET", api+"/alerts?active", "", nil, &alerts)
	if len(alerts) != 1 {
		t.Fatalf("active after one dismissed: %+v", alerts)
	}
	call(t, "POST", api+"/alerts/dismiss", "", map[string]any{}, &out)
	call(t, "GET", api+"/alerts?active", "", nil, &alerts)
	if out.Dismissed != 1 || len(alerts) != 0 {
		t.Fatalf("dismiss all: %d, left %+v", out.Dismissed, alerts)
	}
	call(t, "GET", api+"/alerts", "", nil, &alerts)
	if len(alerts) != 2 || alerts[0].Dismissed == nil {
		t.Fatalf("all alerts: %+v", alerts)
	}
}
