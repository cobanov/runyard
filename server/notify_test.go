package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type hit struct {
	method, auth string
	q            url.Values
	body         []byte
}

// receiver is a stand-in ntfy or webhook endpoint that hands every request to the test.
func receiver(t *testing.T) (string, chan hit) {
	c := make(chan hit, 10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		c <- hit{r.Method, r.Header.Get("Authorization"), r.URL.Query(), b}
	}))
	t.Cleanup(srv.Close)
	return srv.URL, c
}

func next(t *testing.T, c chan hit) hit {
	t.Helper()
	select {
	case h := <-c:
		return h
	case <-time.After(5 * time.Second):
		t.Fatal("no notification arrived")
	}
	return hit{}
}

func TestNotifications(t *testing.T) {
	ts, key, s := testServer(t)
	s.url = "https://runs.example"
	ctx := context.Background()
	ntfy, got := receiver(t)
	hook, hooked := receiver(t)
	if err := channelCmd(ctx, s.db, []string{"add", "-token", "tk", "ntfy", ntfy + "/runs"}); err != nil {
		t.Fatal(err)
	}
	if err := channelCmd(ctx, s.db, []string{"add", "-project", "other", "-events", "finished", "webhook", hook}); err != nil {
		t.Fatal(err)
	}
	api := ts.URL + "/api/v1"
	newRun := func(project, name string) string {
		var r map[string]string
		call(t, "POST", api+"/runs", key, map[string]any{"project": project, "name": name}, &r)
		return r["id"]
	}

	// Finished, with the latest image attached and the final metrics in the text.
	id := newRun("gba", "pixel32")
	call(t, "POST", api+"/runs/"+id+"/metrics", key, []map[string]any{{"step": 1, "values": map[string]any{"loss": 0.1, "sys/cpu": 9}}}, nil)
	call(t, "POST", api+"/runs/"+id+"/media", key, mediaForm(map[string]string{"key": "s", "step": "1", "kind": "image"}, "a.png", []byte("png")), nil)
	call(t, "PATCH", api+"/runs/"+id, key, map[string]any{"status": "finished"}, nil)
	h := next(t, got)
	if h.method != "PUT" || string(h.body) != "png" || h.auth != "Bearer tk" || h.q.Get("title") != "gba/pixel32 finished" ||
		h.q.Get("click") != "https://runs.example/#/r/"+id || !strings.Contains(h.q.Get("message"), "loss: 0.1") ||
		strings.Contains(h.q.Get("message"), "sys/") || h.q.Get("filename") == "" {
		t.Fatalf("finished: %+v", h)
	}
	call(t, "PATCH", api+"/runs/"+id, key, map[string]any{"summary": map[string]any{"x": 1}}, nil) // no status change: no message

	// Failed, with the last output lines.
	id = newRun("gba", "pixel64")
	call(t, "POST", api+"/runs/"+id+"/logs", key, []map[string]any{{"seq": 1, "line": "Traceback"}, {"seq": 2, "line": "RuntimeError: boom"}}, nil)
	call(t, "PATCH", api+"/runs/"+id, key, map[string]any{"status": "failed"}, nil)
	h = next(t, got)
	if h.method != "POST" || h.q.Get("priority") != "high" || !strings.HasSuffix(h.q.Get("message"), "Traceback\nRuntimeError: boom") {
		t.Fatalf("failed: %+v", h)
	}

	call(t, "POST", api+"/runs/"+id+"/alerts", key, map[string]string{"level": "error", "title": "Loss is NaN", "body": "at step 9"}, nil)
	if h = next(t, got); h.q.Get("title") != "Loss is NaN" || h.q.Get("message") != "gba/pixel64: at step 9" || h.q.Get("tags") != "rotating_light" {
		t.Fatalf("alert: %+v", h)
	}

	// Silent: no heartbeat for longer than crashAfter.
	id = newRun("gba", "pixel96")
	s.db.Exec(ctx, `UPDATE runs SET last_heartbeat_at = now() - interval '10 minutes' WHERE id = $1::uuid`, id)
	if ids := s.markCrashed(ctx, crashAfter); len(ids) != 1 || ids[0] != id {
		t.Fatalf("crashed ids: %v", ids)
	}
	if h = next(t, got); h.q.Get("title") != "gba/pixel96 went silent" {
		t.Fatalf("crashed: %+v", h)
	}
	var status map[string]string
	call(t, "POST", api+"/runs/"+id+"/heartbeat", key, nil, &status)
	if status["status"] != "running" {
		t.Fatalf("a heartbeat after a crash should revive the run: %v", status)
	}

	// The webhook only listens to project "other".
	select {
	case h := <-hooked:
		t.Fatalf("webhook fired for another project: %+v", h)
	default:
	}
	id = newRun("other", "x")
	call(t, "PATCH", api+"/runs/"+id, key, map[string]any{"status": "finished"}, nil)
	h = next(t, hooked)
	var m message
	if json.Unmarshal(h.body, &m); m.Event != "finished" || m.Run.ID != id || m.URL == "" {
		t.Fatalf("webhook: %s", h.body)
	}
	next(t, got) // the ntfy channel covers every project
}

func TestEvents(t *testing.T) {
	ts, key, _ := testServer(t)
	res, err := http.Get(ts.URL + "/api/v1/events")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	rd := bufio.NewReader(res.Body)
	rd.ReadString('\n') // the stream opens with a comment, so the subscription is in place
	call(t, "POST", ts.URL+"/api/v1/runs", key, map[string]any{"project": "p", "id": runID}, nil)
	for {
		line, err := rd.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(line, "data: ") {
			if line != `data: {"type":"run","run":"`+runID+`"}`+"\n" {
				t.Fatalf("event: %q", line)
			}
			return
		}
	}
}

func TestResults(t *testing.T) {
	run := Run{Summary: []byte(`{"rec_mse": 0.012345678}`), Metrics: map[string]metricSummary{
		"loss": {Last: num(0.03455612063407898)}, "sys/cpu": {Last: 50}, "a": {Last: num(math.NaN())}, "eval/loss": {Last: 2}}}
	if got, want := results(run), "\neval/loss: 2\nloss: 0.03456\na: NaN\nrec_mse: 0.01235"; got != want {
		t.Fatalf("results = %q, want %q", got, want)
	}
}
