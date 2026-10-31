package main

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestMatch(t *testing.T) {
	for _, c := range []struct {
		pattern, name string
		want          bool
	}{
		{"*loss*", "train/loss", true},
		{"*loss*", "loss_ema", true},
		{"*loss*", "grad_norm", false},
		{"train/loss", "train/loss", true},
		{"train/loss", "eval/train/loss", false},
		{"a*a", "a", false},
		{"eval/*", "eval/acc", true},
		{"x.y", "xzy", false}, // the dot is literal
	} {
		if got := match(c.pattern, c.name); got != c.want {
			t.Errorf("match(%q, %q) = %v", c.pattern, c.name, got)
		}
	}
}

func TestChecks(t *testing.T) {
	ts, key, s := testServer(t)
	ctx := context.Background()
	ntfy, got := receiver(t)
	if err := channelCmd(ctx, s.db, []string{"add", "-events", "alert", "ntfy", ntfy}); err != nil {
		t.Fatal(err)
	}
	api := ts.URL + "/api/v1"
	newRun := func(name string) string {
		var r map[string]string
		call(t, "POST", api+"/runs", key, map[string]any{"project": "llm", "name": name}, &r)
		return r["id"]
	}
	metrics := func(id string, rows ...map[string]any) {
		t.Helper()
		if code := call(t, "POST", api+"/runs/"+id+"/metrics", key, rows, nil); code != 200 {
			t.Fatalf("metrics: %d", code)
		}
	}
	alerts := func(id string) (titles []string) {
		t.Helper()
		var as []Alert
		call(t, "GET", api+"/alerts?run="+id, "", nil, &as)
		for _, a := range as {
			titles = append(titles, a.Title+": "+a.Body)
		}
		sort.Strings(titles)
		return titles
	}

	// The default check: a NaN loss alerts once, at its first step, however often it repeats or is resent.
	id := newRun("nan")
	batch := []map[string]any{
		{"step": 7, "values": map[string]any{"train/loss": "NaN", "grad_norm": "Infinity"}},
		{"step": 5, "values": map[string]any{"train/loss": "Infinity"}},
	}
	metrics(id, batch...)
	metrics(id, batch...)
	metrics(id, map[string]any{"step": 8, "values": map[string]any{"train/loss": "NaN"}})
	if a := alerts(id); len(a) != 1 || a[0] != "train/loss is +Inf: at step 5" {
		t.Fatalf("nan alerts: %q", a)
	}
	if h := next(t, got); h.q.Get("title") != "train/loss is +Inf" || h.q.Get("message") != "llm/nan: at step 5" || h.q.Get("priority") != "high" {
		t.Fatalf("nan notification: %+v", h)
	}

	// A threshold, quiet during warmup; data older than the crash window (runyard sync) does not alert.
	for _, args := range [][]string{{"add", "-project", "llm", "-after", "100", "above", "train/loss", "10"}, {"add", "below", "eval/*", "0.5"}} {
		if err := checkCmd(ctx, s.db, args); err != nil {
			t.Fatal(err)
		}
	}
	id = newRun("diverged")
	old := time.Now().Add(-time.Hour)
	metrics(id, map[string]any{"step": 1, "values": map[string]any{"train/loss": 11.0}},
		map[string]any{"step": 150, "ts": old, "values": map[string]any{"eval/acc": 0.1}})
	if a := alerts(id); len(a) != 0 {
		t.Fatalf("warmup or old data alerted: %q", a)
	}
	metrics(id, map[string]any{"step": 200, "values": map[string]any{"train/loss": 12.25, "eval/acc": 0.4}})
	if a := alerts(id); strings.Join(a, " | ") != "eval/acc below 0.5: 0.4 at step 200 | train/loss above 10: 12.25 at step 200" {
		t.Fatalf("threshold alerts: %q", a)
	}
	next(t, got)
	next(t, got)

	// Stall: heartbeats arrive but nothing else does. System metrics are not progress, output lines are.
	if err := checkCmd(ctx, s.db, []string{"add", "-project", "llm", "stall", "30m"}); err != nil {
		t.Fatal(err)
	}
	stuck, busy, chatty := newRun("stuck"), newRun("busy"), newRun("chatty")
	s.db.Exec(ctx, `UPDATE runs SET progress_at = now() - interval '40 minutes' WHERE id = ANY($1::uuid[])`, []string{stuck, busy, chatty})
	s.db.Exec(ctx, `INSERT INTO logs (run_id, seq, ts, line) VALUES ($1::uuid, 1, now() - interval '40 minutes', 'epoch 3')`, stuck)
	metrics(stuck, map[string]any{"step": 1, "values": map[string]any{"sys/gpu0.util": 0}})
	metrics(busy, map[string]any{"step": 1, "values": map[string]any{"sys/cpu": 5, "loss": 1}})
	call(t, "POST", api+"/runs/"+chatty+"/logs", key, []map[string]any{{"seq": 1, "line": "evaluating 40%"}}, nil)
	s.markStalled(ctx)
	s.markStalled(ctx)
	if a := alerts(stuck); len(a) != 1 || !strings.HasPrefix(a[0], "No progress for 30m: Heartbeats still arrive") || !strings.HasSuffix(a[0], "epoch 3") {
		t.Fatalf("stall alerts: %q", a)
	}
	if a, b := alerts(busy), alerts(chatty); len(a)+len(b) != 0 {
		t.Fatalf("runs sending metrics or output stalled: %q %q", a, b)
	}
	if h := next(t, got); h.q.Get("title") != "No progress for 30m" {
		t.Fatalf("stall notification: %+v", h)
	}

	if err := checkCmd(ctx, s.db, []string{"ls"}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]string{{"add", "stall", "30"}, {"add", "above", "loss", "ten"}, {"add", "nan"}, {"rm", "999"}} {
		if checkCmd(ctx, s.db, bad) == nil {
			t.Errorf("check %v should fail", bad)
		}
	}
	if err := checkCmd(ctx, s.db, []string{"rm", "1"}); err != nil {
		t.Fatal(err)
	}
	id = newRun("after-rm")
	metrics(id, map[string]any{"step": 1, "values": map[string]any{"loss": "NaN"}})
	if a := alerts(id); len(a) != 0 {
		t.Fatalf("a removed check alerted: %q", a)
	}
}

func TestCheckString(t *testing.T) {
	ten, half := 10.0, 30.0
	for c, want := range map[check]string{
		{Kind: "nan", Key: "*loss*"}:                                    "nan *loss*",
		{Kind: "above", Key: "train/loss", Value: &ten, AfterStep: 500}: "train/loss above 10 from step 500",
		{Kind: "stall", Value: &half}:                                   "stall 30m",
	} {
		if got := c.String(); got != want {
			t.Errorf("%+v = %q, want %q", c, got, want)
		}
	}
}
