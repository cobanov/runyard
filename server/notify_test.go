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
	if h.method != "PUT" || string(h.body) != "png" || h.auth != "Bearer tk" || h.q.Get("title") != "gba/pixel32: Finished" ||
		h.q.Get("click") != "https://runs.example/#/r/"+id || !strings.Contains(h.q.Get("message"), "loss: 0.1") ||
		strings.Contains(h.q.Get("message"), "sys/") || h.q.Get("filename") == "" {
		t.Fatalf("finished: %+v", h)
	}
	call(t, "PATCH", api+"/runs/"+id, key, map[string]any{"summary": map[string]any{"x": 1}}, nil) // no status change: no message

	// Failed, with the last output lines.
	id = newRun("gba", "pixel64")
	call(t, "POST", api+"/runs/"+id+"/logs", key, []map[string]any{{"seq": 1, "line": "Traceback"}, {"seq": 2, "line": "RuntimeError: boom"}}, nil)
	call(t, "PATCH", api+"/runs/"+id, key, map[string]any{"status": "failed", "error": "RuntimeError: boom", "exit_code": 1}, nil)
	h = next(t, got)
	if h.method != "POST" || h.q.Get("priority") != "high" || !strings.HasSuffix(h.q.Get("message"), "Traceback\nRuntimeError: boom") ||
		!strings.Contains(h.q.Get("message"), ".\nRuntimeError: boom\nLast output:") {
		t.Fatalf("failed: %+v", h)
	}

	call(t, "POST", api+"/runs/"+id+"/alerts", key, map[string]string{"level": "error", "title": "Loss is NaN", "body": "at step 9"}, nil)
	if h = next(t, got); h.q.Get("title") != "gba/pixel64: Loss is NaN" || h.q.Get("message") != "at step 9" || h.q.Get("tags") != "rotating_light" || h.q.Get("priority") != "urgent" {
		t.Fatalf("alert: %+v", h)
	}

	// Silent: no heartbeat for longer than crashAfter.
	id = newRun("gba", "pixel96")
	s.db.Exec(ctx, `UPDATE runs SET started_at = now() - interval '20 minutes', last_heartbeat_at = now() - interval '10 minutes' WHERE id = $1::uuid`, id)
	s.db.Exec(ctx, `INSERT INTO logs (run_id, seq, ts, stream, line) VALUES ($1::uuid, 1, now() - interval '9 minutes', 1, 'step 19')`, id)
	if ids := s.markCrashed(ctx, crashAfter); len(ids) != 1 || ids[0] != id {
		t.Fatalf("crashed ids: %v", ids)
	}
	if h = next(t, got); h.q.Get("title") != "gba/pixel96: Went silent" || !strings.Contains(h.q.Get("message"), "after 11m 0s of running") {
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

// TestOutbox: a channel that answers 500 gets the notification again later, as many times as it takes within the
// schedule, after which it is listed as undelivered until dismissed; one that refuses the request is given up at once.
func TestOutbox(t *testing.T) {
	ts, key, s := testServer(t)
	ctx := context.Background()
	codes := make(chan int, 20) // the answer to each request, in order; 200 when none is queued
	got := make(chan hit, 20)
	ntfy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code := 200
		select {
		case code = <-codes:
		default:
		}
		b, _ := io.ReadAll(r.Body)
		got <- hit{r.Method, r.Header.Get("Authorization"), r.URL.Query(), b}
		w.WriteHeader(code)
	}))
	t.Cleanup(ntfy.Close)
	if err := channelCmd(ctx, s.db, []string{"add", "ntfy", ntfy.URL + "/runs"}); err != nil {
		t.Fatal(err)
	}
	api := ts.URL + "/api/v1"
	finish := func(name string) string {
		var r map[string]string
		call(t, "POST", api+"/runs", key, map[string]any{"project": "box", "name": name}, &r)
		call(t, "PATCH", api+"/runs/"+r["id"], key, map[string]any{"status": "finished"}, nil)
		return r["id"]
	}
	// row waits for the outbox row of run id to reach tries, and returns its state.
	type row struct {
		tries int
		due   *time.Time
		err   string
	}
	waitRow := func(id string, tries int) row {
		t.Helper()
		var r row
		for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
			if s.db.QueryRow(ctx, `SELECT attempts, next_at, last_error FROM outbox WHERE run_id = $1::uuid`, id).Scan(&r.tries, &r.due, &r.err) == nil && r.tries == tries {
				return r
			}
		}
		t.Fatalf("outbox row of %s: %+v, want %d tries", id, r, tries)
		return r
	}
	due := func() { s.db.Exec(ctx, `UPDATE outbox SET next_at = now() WHERE next_at IS NOT NULL`) }
	var list []undelivered

	// Down twice, then up: delivered on the third try, saying it is late, and crossed out.
	codes <- 500
	codes <- 502
	id := finish("flaky")
	next(t, got)
	if r := waitRow(id, 1); r.due == nil || time.Until(*r.due) < 50*time.Second || !strings.Contains(r.err, "500") {
		t.Fatalf("after the first failure: %+v", r)
	}
	call(t, "GET", api+"/notifications/undelivered", "", nil, &list)
	if len(list) != 1 || list[0].Title != "box/flaky: Finished" || list[0].NextAt == nil || list[0].Kind != "ntfy" {
		t.Fatalf("undelivered while retrying: %+v", list)
	}
	due()
	s.retryOutbox(ctx)
	next(t, got)
	if r := waitRow(id, 2); time.Until(*r.due) < 4*time.Minute {
		t.Fatalf("second wait should be five minutes: %+v", r)
	}
	due()
	s.retryOutbox(ctx)
	if h := next(t, got); h.q.Get("title") != "box/flaky: Finished" || !strings.Contains(h.q.Get("message"), "late") {
		t.Fatalf("late delivery: %+v", h)
	}
	var n int
	s.db.QueryRow(ctx, `SELECT count(*) FROM outbox`).Scan(&n)
	if n != 0 {
		t.Fatalf("a delivered notification stayed in the outbox: %d rows", n)
	}

	// Down for good: given up after the last try, listed, then dismissed.
	codes <- 503
	id = finish("down")
	next(t, got)
	waitRow(id, 1)
	s.db.Exec(ctx, `UPDATE outbox SET attempts = $1`, len(retryAfter))
	codes <- 503
	due()
	s.retryOutbox(ctx)
	next(t, got)
	if r := waitRow(id, len(retryAfter)+1); r.due != nil {
		t.Fatalf("not given up after the last try: %+v", r)
	}
	call(t, "GET", api+"/notifications/undelivered", "", nil, &list)
	if len(list) != 1 || list[0].NextAt != nil || list[0].Run == nil || *list[0].Run != id {
		t.Fatalf("given up: %+v", list)
	}
	due()
	s.retryOutbox(ctx) // a given-up row is not tried again
	select {
	case h := <-got:
		t.Fatalf("given-up notification was sent again: %+v", h)
	case <-time.After(200 * time.Millisecond):
	}
	call(t, "POST", api+"/notifications/dismiss", "", map[string]any{"ids": []int64{list[0].ID}}, nil)
	call(t, "GET", api+"/notifications/undelivered", "", nil, &list)
	if len(list) != 0 {
		t.Fatalf("dismissed but listed: %+v", list)
	}

	// Refused (a 4xx other than 408, 425, 429): no point trying again.
	codes <- 400
	id = finish("refused")
	next(t, got)
	if r := waitRow(id, 1); r.due != nil {
		t.Fatalf("a refused notification is retried: %+v", r)
	}

	// A server stopped in the middle of a send left a row behind with its lease: the next sweep after it sends it.
	s.db.Exec(ctx, `INSERT INTO outbox (channel_id, message, next_at) SELECT id, '{"event":"host","title":"white: Went silent","body":"b","priority":"high"}', now() FROM channels`)
	s.retryOutbox(ctx)
	if h := next(t, got); h.q.Get("title") != "white: Went silent" || h.q.Get("priority") != "high" {
		t.Fatalf("left-behind row: %+v", h)
	}
}

// TestRunError: the reason a run ended is kept as one line with the exit code, listed with the run, and cleared when
// the run starts again; a client that sends neither (runyard 0.7 and older) leaves both out of the JSON.
func TestRunError(t *testing.T) {
	ts, key, _ := testServer(t)
	api := ts.URL + "/api/v1"
	var r map[string]string
	call(t, "POST", api+"/runs", key, map[string]any{"project": "e", "name": "oom"}, &r)
	id := r["id"]
	call(t, "PATCH", api+"/runs/"+id, key, map[string]any{"status": "failed", "exit_code": 1,
		"error": "\ntorch.OutOfMemoryError: CUDA out of memory. Tried to allocate 2.00 GiB\nIncluding non-PyTorch memory"}, nil)
	var runs []map[string]any
	call(t, "GET", api+"/runs?project=e", "", nil, &runs)
	if runs[0]["error"] != "torch.OutOfMemoryError: CUDA out of memory. Tried to allocate 2.00 GiB" || runs[0]["exit_code"] != 1.0 {
		t.Fatalf("listed: %v", runs[0])
	}
	call(t, "PATCH", api+"/runs/"+id, key, map[string]any{"summary": map[string]any{"x": 1}}, nil) // no status: kept
	call(t, "PATCH", api+"/runs/"+id, key, map[string]any{"status": "running"}, nil)               // resumed: cleared
	var run map[string]any
	call(t, "GET", api+"/runs/"+id, "", nil, &run)
	if _, has := run["error"]; has || run["exit_code"] != nil {
		t.Fatalf("a resumed run keeps its old error: %v", run)
	}
	call(t, "PATCH", api+"/runs/"+id, key, map[string]any{"status": "failed"}, nil) // an old client
	call(t, "GET", api+"/runs/"+id, "", nil, &run)
	if _, has := run["error"]; has || run["status"] != "failed" {
		t.Fatalf("old client: %v", run)
	}
}
