package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestKeys: a server without a key lets the browser make the first one, once; a key made for logging writes and
// reads but does not delete or manage.
func TestKeys(t *testing.T) {
	ts, root, s := testServer(t)
	ctx := context.Background()
	api := ts.URL + "/api/v1"

	var me map[string]any
	call(t, "GET", api+"/me", "", nil, &me)
	if me["setup"] != false {
		t.Fatalf("a server with a key offers setup: %v", me)
	}
	if code := call(t, "POST", api+"/keys", "", map[string]string{"name": "x"}, nil); code != 401 {
		t.Fatalf("a second key without one: %d", code)
	}
	s.db.Exec(ctx, `DELETE FROM api_keys`)
	call(t, "GET", api+"/me", "", nil, &me)
	var first map[string]string
	// It asks for a logging key, and still gets a full one: something must be able to make the others.
	if code := call(t, "POST", api+"/keys", "", map[string]string{"name": "first", "scope": "log"}, &first); code != 200 || me["setup"] != true {
		t.Fatalf("first key from the browser: %d, me %v", code, me)
	}
	if code := call(t, "POST", api+"/keys", "", map[string]string{"name": "second"}, nil); code != 401 {
		t.Fatalf("the door stays open after the first key: %d", code)
	}
	root = first["key"]
	var logger map[string]string
	call(t, "POST", api+"/keys", root, map[string]string{"name": "gpu-box", "scope": "log"}, &logger)
	var run map[string]string
	if code := call(t, "POST", api+"/runs", logger["key"], map[string]any{"project": "p"}, &run); code != 200 {
		t.Fatalf("a logging key logs: %d", code)
	}
	for _, c := range [][2]string{{"DELETE", "/runs/" + run["id"]}, {"GET", "/keys"}, {"POST", "/keys"}, {"GET", "/channels"}} {
		if code := call(t, c[0], api+c[1], logger["key"], map[string]string{"name": "more"}, nil); code != 403 {
			t.Fatalf("%s %s with a logging key: %d", c[0], c[1], code)
		}
	}
	// With users, a logging key of an admin is still only a logging key.
	s.auth = true
	s.db.Exec(ctx, `INSERT INTO users (name, password, admin) VALUES ('boss', $1, true)`, hashPassword("b"))
	bossLog, _ := createKey(ctx, s.db, "boss-gpu", "boss", true)
	if code := call(t, "DELETE", api+"/runs/"+run["id"], bossLog, nil, nil); code != 403 {
		t.Fatalf("an admin's logging key deletes: %d", code)
	}
	if code := call(t, "DELETE", api+"/runs/"+run["id"], root, nil, nil); code != 200 {
		t.Fatalf("a full key deletes: %d", code)
	}

	// Ten wrong passwords, then the door is shut for a while, for the right one too.
	login := func(pw string) int {
		res, _ := http.Post(api+"/login", "application/json", strings.NewReader(`{"name":"boss","password":"`+pw+`"}`))
		res.Body.Close()
		return res.StatusCode
	}
	for i := range 10 {
		if code := login("wrong"); code != 401 {
			t.Fatalf("wrong password %d: %d", i, code)
		}
	}
	if code := login("b"); code != 429 {
		t.Fatalf("after ten wrong passwords: %d", code)
	}
	logins = &limiter{fails: map[string][]time.Time{}}
}

// TestThumbs: ?w= on a sample serves a smaller JPEG, made once and deleted with the run.
func TestThumbs(t *testing.T) {
	ts, key, s := testServer(t)
	api := ts.URL + "/api/v1"
	call(t, "POST", api+"/runs", key, map[string]any{"id": runID, "project": "p"}, nil)
	img := image.NewRGBA(image.Rect(0, 0, 1000, 500))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	png.Encode(&buf, img)
	var up map[string]string
	call(t, "POST", api+"/runs/"+runID+"/media", key, mediaForm(map[string]string{"key": "s", "step": "1", "kind": "image"}, "s.png", buf.Bytes()), &up)
	for _, c := range []struct {
		query string
		width int
	}{{"?w=300", 320}, {"?w=640", 640}, {"?w=2000", 1000}, {"", 1000}, {"?w=1280", 1000}} { // never scaled up
		res, _ := http.Get(ts.URL + up["url"] + c.query)
		got, _, err := image.Decode(res.Body)
		res.Body.Close()
		if err != nil || got.Bounds().Dx() != c.width || got.Bounds().Dy() != c.width/2 {
			t.Fatalf("%q: %v %v, want width %d", c.query, err, got.Bounds(), c.width)
		}
	}
	small, _ := s.store.open(context.Background(), thumbKey(up["url"][len("/media/"):], 320))
	if _, err := jpeg.Decode(small); err != nil {
		t.Fatal("the small copy is not kept:", err)
	}
	small.Close()
	call(t, "DELETE", api+"/runs/"+runID, key, nil, nil)
	wait(t, func() bool {
		_, err := s.store.open(context.Background(), thumbKey(up["url"][len("/media/"):], 320))
		return err != nil
	})
}

// TestLongRun: a metric with far more rows than a chart has points is read from its rollup, with the same
// envelope; output lines past the kept count are dropped from the front.
func TestLongRun(t *testing.T) {
	ts, key, s := testServer(t)
	api := ts.URL + "/api/v1"
	var created map[string]string
	call(t, "POST", api+"/runs", key, map[string]any{"project": "p"}, &created)
	id := created["id"]
	const n = 30000
	for from := 0; from < n; from += 5000 {
		var batch []map[string]any
		for i := from; i < from+5000; i++ {
			batch = append(batch, map[string]any{"step": i, "values": map[string]any{"loss": float64(i % 1000), "lr": 1}})
		}
		call(t, "POST", api+"/runs/"+id+"/metrics", key, batch, nil)
	}
	var rolled int
	s.db.QueryRow(context.Background(), `SELECT count(*) FROM metric_rollup WHERE run_id = $1::uuid`, id).Scan(&rolled)
	if rolled != 2*n/100 {
		t.Fatalf("rollup rows: %d", rolled)
	}
	var out map[string]series
	call(t, "GET", api+"/runs/"+id+"/metrics?points=100", "", nil, &out) // 30000 > 100 * 200: from the rollup
	l := out["loss"]
	last := len(l.Step) - 1
	if len(l.Step) != 100 || l.Step[last] != n-1 || l.Last[last] != 999 || l.Min[0] != 0 || l.Max[0] != 299 || l.T[last] < 1e9 {
		t.Fatalf("rolled up: buckets=%d step=%d last=%v min=%v max=%v t=%v", len(l.Step), l.Step[last], l.Last[last], l.Min[0], l.Max[0], l.T[last])
	}
	call(t, "GET", api+"/runs/"+id+"/metrics?points=1000", "", nil, &out) // 30000 <= 1000 * 200: from the rows
	if l := out["loss"]; len(l.Step) != 1000 || l.Step[999] != n-1 || l.Max[0] != 29 || l.T[999] < 1e9 {
		t.Fatalf("raw: buckets=%d step=%d max=%v", len(l.Step), l.Step[len(l.Step)-1], l.Max[0])
	}
	if code := call(t, "POST", api+"/runs/"+id+"/metrics", key, []map[string]any{{"step": 1, "values": map[string]any{"x": nil}}}, nil); code != 400 {
		t.Fatalf("a null value: %d", code)
	}
	if code := call(t, "PATCH", api+"/runs/"+id, key, map[string]any{"config": []int{1}}, nil); code != 400 {
		t.Fatalf("a config that is not an object: %d", code)
	}

	keepLogs = 5
	defer func() { keepLogs = 1_000_000 }()
	var lines []map[string]any
	for seq := 9990; seq <= 10004; seq++ {
		lines = append(lines, map[string]any{"seq": seq, "line": fmt.Sprint("line ", seq)})
	}
	call(t, "POST", api+"/runs/"+id+"/logs", key, lines, nil)
	var kept []LogLine
	call(t, "GET", api+"/runs/"+id+"/logs", "", nil, &kept)
	if len(kept) != 5 || kept[0].Seq != 10000 {
		t.Fatalf("kept %d lines from %d", len(kept), kept[0].Seq)
	}
}

// wait polls for something another goroutine does (deleting files after a purge).
func wait(t *testing.T, done func() bool) {
	t.Helper()
	for range 100 {
		if done() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out")
}
