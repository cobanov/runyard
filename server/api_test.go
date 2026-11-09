package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// testServer gives a server on a freshly reset schema of the database in RUNYARD_TEST_DATABASE_URL
// (`make test` starts a Postgres container for it) and a valid write key.
func testServer(t *testing.T) (*httptest.Server, string, *srv) {
	dsn := os.Getenv("RUNYARD_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("RUNYARD_TEST_DATABASE_URL not set; run `make test`")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := migrate(ctx, db); err != nil { // a second start must be a no-op
		t.Fatal(err)
	}
	if err := loadVAPID(ctx, db); err != nil {
		t.Fatal(err)
	}
	key, err := createKey(ctx, db, "test", "")
	if err != nil {
		t.Fatal(err)
	}
	s := &srv{db: db, store: localBlobs(t.TempDir()), hub: newHub()}
	ts := httptest.NewServer(s.routes())
	t.Cleanup(func() { ts.Close(); db.Close() })
	return ts, key, s
}

// call sends body as JSON (or as-is when it is an io.Reader) and decodes the JSON response into out.
func call(t *testing.T, method, url, key string, body any, out any) int {
	t.Helper()
	var rd io.Reader
	ctype := "application/json"
	switch b := body.(type) {
	case nil:
	case *multipart.Writer:
		t.Fatal("pass the buffer, not the writer")
	case formBody:
		rd, ctype = b.buf, b.ctype
	default:
		j, _ := json.Marshal(b)
		rd = bytes.NewReader(j)
	}
	req, _ := http.NewRequest(method, url, rd)
	req.Header.Set("Content-Type", ctype)
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("%s %s: %d %s", method, url, res.StatusCode, raw)
		}
	}
	return res.StatusCode
}

type formBody struct {
	buf   *bytes.Buffer
	ctype string
}

func mediaForm(fields map[string]string, filename string, content []byte) formBody {
	buf := &bytes.Buffer{}
	w := multipart.NewWriter(buf)
	for k, v := range fields {
		w.WriteField(k, v)
	}
	f, _ := w.CreateFormFile("file", filename)
	f.Write(content)
	w.Close()
	return formBody{buf, w.FormDataContentType()}
}

const runID = "0b5c4a8e-1f2d-4c3b-9a8e-7d6c5b4a3f21"

func TestWriteFlow(t *testing.T) {
	ts, key, s := testServer(t)
	api := ts.URL + "/api/v1"
	create := map[string]any{"id": runID, "project": "gba", "name": "pixel32", "config": map[string]any{"size": 32}, "tags": []string{"sweep"}}
	var created map[string]string
	if code := call(t, "POST", api+"/runs", key, create, &created); code != 200 || created["id"] != runID {
		t.Fatalf("create: %d %v", code, created)
	}
	if code := call(t, "POST", api+"/runs", key, create, nil); code != 200 { // a retried create is harmless
		t.Fatalf("second create: %d", code)
	}

	// Two batches overlap on step 2: the retry overwrites, no duplicate rows. NaN and ±Inf survive.
	batch := []map[string]any{
		{"step": 1, "values": map[string]any{"loss": 1.0, "lr": 0.1}},
		{"step": 2, "values": map[string]any{"loss": 0.5}},
		{"step": 2, "values": map[string]any{"loss": 0.4}},
	}
	if code := call(t, "POST", api+"/runs/"+runID+"/metrics", key, batch, nil); code != 200 {
		t.Fatalf("metrics: %d", code)
	}
	batch2 := []map[string]any{{"step": 2, "values": map[string]any{"loss": 0.3}}, {"step": 3, "values": map[string]any{"loss": "NaN", "lr": "-Infinity"}}}
	if code := call(t, "POST", api+"/runs/"+runID+"/metrics", key, batch2, nil); code != 200 {
		t.Fatalf("metrics 2: %d", code)
	}

	logs := []map[string]any{{"seq": 1, "line": "epoch 1"}, {"seq": 2, "stream": 2, "line": "warn\x00ing"}}
	call(t, "POST", api+"/runs/"+runID+"/logs", key, logs, nil)
	if code := call(t, "POST", api+"/runs/"+runID+"/logs", key, logs, nil); code != 200 {
		t.Fatalf("logs retry: %d", code)
	}

	png := []byte("\x89PNG fake")
	form := map[string]string{"id": "11111111-2222-4333-8444-555555555555", "key": "samples", "step": "3", "kind": "image", "meta": `{"caption":"grid"}`}
	var up map[string]string
	if code := call(t, "POST", api+"/runs/"+runID+"/media", key, mediaForm(form, "grid.PNG", png), &up); code != 200 {
		t.Fatalf("media: %d %v", code, up)
	}
	call(t, "POST", api+"/runs/"+runID+"/media", key, mediaForm(form, "grid.PNG", []byte("other")), nil) // retry keeps the first file
	got, _ := readBlob(context.Background(), s.store, runID+"/"+form["id"]+".png")
	if !bytes.Equal(got, png) {
		t.Fatalf("media file = %q", got)
	}
	res, _ := http.Get(ts.URL + up["url"])
	served, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 || !bytes.Equal(served, png) {
		t.Fatalf("served media: %d %q", res.StatusCode, served)
	}
	if res, _ := http.Get(ts.URL + "/media/" + runID + "/"); res.StatusCode != 404 {
		t.Fatalf("directory listing: %d", res.StatusCode)
	}
	// An uploaded "sample" that is really a script must not run with the viewer's session when opened.
	var svg map[string]string
	call(t, "POST", api+"/runs/"+runID+"/media", key, mediaForm(map[string]string{"key": "x", "step": "9", "kind": "text"}, "x.svg",
		[]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)), &svg)
	res, _ = http.Get(ts.URL + svg["url"])
	if h := res.Header; h.Get("Content-Security-Policy") != "sandbox" || h.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("an uploaded svg is served live: %v", h)
	}

	if code := call(t, "POST", api+"/runs/"+runID+"/heartbeat", key, nil, nil); code != 200 {
		t.Fatalf("heartbeat: %d", code)
	}
	if code := call(t, "POST", api+"/runs/"+runID+"/alerts", key, map[string]string{"level": "warn", "title": "loss is NaN"}, nil); code != 200 {
		t.Fatalf("alert: %d", code)
	}
	patch := map[string]any{"status": "finished", "summary": map[string]any{"best": 0.3}, "config": map[string]any{"extra": true}}
	if code := call(t, "PATCH", api+"/runs/"+runID, key, patch, nil); code != 200 {
		t.Fatalf("patch: %d", code)
	}

	var run Run
	call(t, "GET", api+"/runs/"+runID, "", nil, &run)
	loss := run.Metrics["loss"]
	if run.Status != "finished" || run.EndedAt == nil || loss.Count != 3 || loss.Step != 3 || !math.IsNaN(float64(loss.Last)) {
		t.Fatalf("run: %+v", run)
	}
	if !math.IsInf(float64(run.Metrics["lr"].Last), -1) {
		t.Fatalf("lr: %+v", run.Metrics["lr"])
	}
	if string(run.Config) != `{"size":32,"extra":true}` || string(run.Summary) != `{"best":0.3}` {
		t.Fatalf("config %s summary %s", run.Config, run.Summary)
	}

	var series map[string]series
	call(t, "GET", api+"/runs/"+runID+"/metrics?keys=loss", "", nil, &series)
	if l := series["loss"]; len(series) != 1 || fmt.Sprint(l.Step) != "[1 2 3]" || l.Last[1] != 0.3 {
		t.Fatalf("series: %+v", series)
	}
	var lines []LogLine
	call(t, "GET", api+"/runs/"+runID+"/logs?tail&limit=1", "", nil, &lines)
	if len(lines) != 1 || lines[0].Line != "warning" || lines[0].Stream != 2 {
		t.Fatalf("logs: %+v", lines)
	}
	var media []Media
	call(t, "GET", api+"/runs/"+runID+"/media?kind=image", "", nil, &media)
	if len(media) != 1 || media[0].Step != 3 || !strings.Contains(string(media[0].Meta), "grid") {
		t.Fatalf("media list: %+v", media)
	}
	var alerts []Alert
	call(t, "GET", api+"/alerts?project=gba", "", nil, &alerts)
	// The run's own alert, and the one the default check raised when loss went NaN at step 3.
	if len(alerts) != 2 || alerts[0].Title != "loss is NaN" || alerts[0].RunName != "pixel32" || alerts[1].Body != "at step 3" {
		t.Fatalf("alerts: %+v", alerts)
	}
	var runs []Run
	call(t, "GET", api+"/runs?project=gba&tag=sweep", "", nil, &runs)
	var projects []Project
	call(t, "GET", api+"/projects", "", nil, &projects)
	if len(runs) != 1 || len(projects) != 1 || projects[0].Runs != 1 || runs[0].Metrics != nil {
		t.Fatalf("runs %+v projects %+v", runs, projects)
	}
	call(t, "GET", api+"/runs?project=gba&metrics=1", "", nil, &runs)
	if m := runs[0].Metrics["loss"]; len(runs) != 1 || m.Count != 3 || m.Step != 3 {
		t.Fatalf("runs with metrics: %+v", runs)
	}

	// A long list comes in pages: before is the start of the last run shown. q searches names; sort=ended is
	// for "recently ended", where a long run that just ended comes first.
	second := map[string]any{"project": "gba", "name": "Pixel64-long", "started_at": time.Now().Add(-time.Hour)}
	call(t, "POST", api+"/runs", key, second, &created)
	page := func(query string) string {
		var rs []Run
		call(t, "GET", api+"/runs?project=gba&"+query, "", nil, &rs)
		names := []string{}
		for _, r := range rs {
			names = append(names, r.Name)
		}
		return strings.Join(names, ",")
	}
	if got := page("limit=1"); got != "pixel32" {
		t.Fatalf("first page: %s", got)
	}
	if got := page("limit=1&before=" + url.QueryEscape(run.StartedAt.Format(time.RFC3339Nano))); got != "Pixel64-long" {
		t.Fatalf("second page: %s", got)
	}
	if got := page("q=PIXEL64"); got != "Pixel64-long" {
		t.Fatalf("search: %s", got)
	}
	call(t, "PATCH", api+"/runs/"+created["id"], key, map[string]any{"status": "finished"}, nil)
	if got := page("sort=ended"); got != "Pixel64-long,pixel32" {
		t.Fatalf("by end: %s", got)
	}
	// Clients that ask get the API compressed.
	req, _ := http.NewRequest("GET", api+"/runs", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	if res, _ := http.DefaultTransport.RoundTrip(req); res.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("not compressed: %v", res.Header)
	}
}

func TestAuthAndBadInput(t *testing.T) {
	ts, key, _ := testServer(t)
	api := ts.URL + "/api/v1"
	body := map[string]any{"project": "p"}
	for _, k := range []string{"", "ry_wrong"} {
		if code := call(t, "POST", api+"/runs", k, body, nil); code != 401 {
			t.Fatalf("key %q: %d", k, code)
		}
	}
	var created map[string]string
	call(t, "POST", api+"/runs", key, body, &created)
	id := created["id"]
	cases := []struct {
		method, path string
		body         any
		want         int
	}{
		{"POST", "/runs", map[string]any{}, 400},                            // no project
		{"POST", "/runs", map[string]any{"project": "p", "id": "abc"}, 400}, // was a panic
		{"POST", "/runs", map[string]any{"project": "p", "name": "ResNet50-A"}, 200},
		{"GET", "/runs/ResNet50-A", nil, 200},                                                                         // a name keeps its capitals
		{"POST", "/runs/" + id + "/metrics", []map[string]any{{"step": 1, "values": map[string]any{"x": true}}}, 400}, // not a number
		{"POST", "/runs/" + newUUID() + "/metrics", []map[string]any{{"step": 1, "values": map[string]any{"x": 1}}}, 404},
		{"POST", "/runs/" + newUUID() + "/heartbeat", nil, 404},
		{"PATCH", "/runs/" + id, map[string]any{"status": "exploded"}, 400},
		{"POST", "/runs/" + id + "/alerts", map[string]any{"level": "panic", "title": "x"}, 400},
		{"POST", "/runs/" + id + "/media", mediaForm(map[string]string{"key": "k", "step": "1", "kind": "audio"}, "a.wav", nil), 400},
		{"POST", "/runs/" + newUUID() + "/media", mediaForm(map[string]string{"key": "k", "step": "1", "kind": "image"}, "a.png", nil), 404},
		{"GET", "/runs/" + newUUID(), nil, 404},
		{"GET", "/runs/no-such-name", nil, 404},
		{"GET", "/runs/" + id + "/logs?after=x", nil, 200},
		{"GET", "/runs/not-a-uuid/logs", nil, 400},
	}
	for _, c := range cases {
		k := key
		if c.method == "GET" {
			k = ""
		}
		if code := call(t, c.method, api+c.path, k, c.body, nil); code != c.want {
			t.Errorf("%s %s: %d, want %d", c.method, c.path, code, c.want)
		}
	}
}

func TestDownsample(t *testing.T) {
	ts, key, _ := testServer(t)
	api := ts.URL + "/api/v1"
	var created map[string]string
	call(t, "POST", api+"/runs", key, map[string]any{"project": "p"}, &created)
	id := created["id"]
	var batch []map[string]any
	for i := 0; i < 10000; i++ {
		batch = append(batch, map[string]any{"step": i, "values": map[string]any{"loss": float64(i % 100)}})
	}
	call(t, "POST", api+"/runs/"+id+"/metrics", key, batch, nil)
	var out map[string]series
	call(t, "GET", api+"/runs/"+id+"/metrics?points=100", "", nil, &out)
	l := out["loss"]
	if len(l.Step) != 100 || l.Step[99] != 9999 || l.Min[0] != 0 || l.Max[0] != 99 || l.Last[99] != 99 {
		t.Fatalf("buckets=%d last step=%d min=%v max=%v last=%v", len(l.Step), l.Step[len(l.Step)-1], l.Min[0], l.Max[0], l.Last[len(l.Last)-1])
	}
}
