package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestTeams: with RUNYARD_AUTH on, a user sees projects without a team and their teams' projects, through every
// door: REST, the session cookie, SSE, MCP, MLflow and notifications.
func TestTeams(t *testing.T) {
	ts, root, s := testServer(t)
	s.auth = true
	ctx := context.Background()
	api := ts.URL + "/api/v1"

	runs := map[string]string{}
	for _, p := range []string{"shared", "vision", "nlp"} {
		id := newUUID()
		call(t, "POST", api+"/runs", root, map[string]any{"id": id, "project": p, "name": p + "-run"}, nil)
		call(t, "POST", api+"/runs/"+id+"/metrics", root, []map[string]any{{"step": 1, "values": map[string]any{"loss": 1}}}, nil)
		call(t, "POST", api+"/runs/"+id+"/alerts", root, map[string]any{"title": p + " alert"}, nil)
		runs[p] = id
	}
	for _, sql := range []string{
		`INSERT INTO users (name, password, admin) VALUES ('alice', '` + hashPassword("alice-pw") + `', false), ('boss', '` + hashPassword("x") + `', true)`,
		`INSERT INTO teams (name) VALUES ('vision'), ('nlp')`,
		`INSERT INTO members SELECT t.id, u.id FROM teams t, users u WHERE t.name = 'vision' AND u.name = 'alice'`,
	} {
		if _, err := s.db.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{"vision", "nlp"} {
		if err := projectCmd(ctx, s.db, []string{"team", p, p}); err != nil {
			t.Fatal(err)
		}
	}
	alice, _ := createKey(ctx, s.db, "alice-laptop", "alice", false)
	boss, _ := createKey(ctx, s.db, "boss", "boss", false)

	if code := call(t, "GET", api+"/runs", "", nil, nil); code != 401 {
		t.Fatalf("anonymous read: %d", code)
	}
	for _, open := range []string{ts.URL + "/", api + "/health", api + "/me"} {
		if code := call(t, "GET", open, "", nil, nil); code != 200 {
			t.Fatalf("%s: %d", open, code)
		}
	}
	names := func(key string) string {
		var rs []Run
		call(t, "GET", api+"/runs", key, nil, &rs)
		var n []string
		for _, r := range rs {
			n = append(n, r.Project)
		}
		return strings.Join(n, ",")
	}
	if got := names(alice); got != "vision,shared" {
		t.Fatalf("alice sees %s", got)
	}
	if got := names(boss); got != "nlp,vision,shared" {
		t.Fatalf("an admin sees %s", got)
	}
	var projects []Project
	call(t, "GET", api+"/projects", alice, nil, &projects)
	var alerts []Alert
	call(t, "GET", api+"/alerts", alice, nil, &alerts)
	if len(projects) != 2 || len(alerts) != 2 {
		t.Fatalf("projects %v, alerts %v", projects, alerts)
	}
	nlp := runs["nlp"]
	for _, u := range []string{"/runs/" + nlp, "/runs/nlp-run", "/runs/" + nlp + "/logs", "/runs/" + nlp + "/metrics", "/alerts?run=" + nlp} {
		var out any
		if code := call(t, "GET", api+u, alice, nil, &out); code != 404 && !(code == 200 && string(must(json.Marshal(out))) == "[]") {
			t.Fatalf("%s: %d %v", u, code, out)
		}
	}
	if code := call(t, "GET", ts.URL+"/media/"+nlp+"/00000000-0000-4000-8000-000000000000.png", alice, nil, nil); code != 404 {
		t.Fatalf("media of a hidden run: %d", code)
	}
	if code := call(t, "POST", api+"/runs/"+nlp+"/metrics", alice, []map[string]any{{"step": 2, "values": map[string]any{"loss": 2}}}, nil); code != 404 {
		t.Fatalf("writing to a hidden run: %d", code)
	}
	if code := call(t, "POST", api+"/runs", alice, map[string]any{"project": "nlp"}, nil); code != 404 {
		t.Fatalf("a run in a hidden project: %d", code)
	}
	// A project a user makes goes to their team.
	call(t, "POST", api+"/runs", alice, map[string]any{"project": "alice-new"}, nil)
	var team *int64
	s.db.QueryRow(ctx, `SELECT team_id FROM projects WHERE name = 'alice-new'`).Scan(&team)
	if team == nil {
		t.Fatal("alice's new project has no team")
	}

	// The web UI logs in and reads with the cookie; the cookie alone cannot write.
	login := func(pw string) (*http.Cookie, int) {
		res, err := http.Post(api+"/login", "application/json", strings.NewReader(`{"name":"alice","password":"`+pw+`"}`))
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		for _, c := range res.Cookies() {
			return c, res.StatusCode
		}
		return nil, res.StatusCode
	}
	if _, code := login("wrong"); code != 401 {
		t.Fatalf("wrong password: %d", code)
	}
	cookie, code := login("alice-pw")
	if code != 200 || cookie == nil || !cookie.HttpOnly {
		t.Fatalf("login: %d %v", code, cookie)
	}
	get := func(path string) (int, string) {
		req, _ := http.NewRequest("GET", api+path, nil)
		req.AddCookie(cookie)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}
	if code, body := get("/me"); !strings.Contains(body, `"user":"alice"`) {
		t.Fatalf("me: %d %s", code, body)
	}
	if code, body := get("/runs"); code != 200 || strings.Contains(body, "nlp") || !strings.Contains(body, "vision-run") {
		t.Fatalf("runs by cookie: %d %s", code, body)
	}
	req, _ := http.NewRequest("POST", api+"/runs", strings.NewReader(`{"project":"x"}`))
	req.AddCookie(cookie)
	if res, _ := http.DefaultClient.Do(req); res.StatusCode != 401 {
		t.Fatalf("write by cookie: %d", res.StatusCode)
	}

	// SSE: alice gets the vision run's events, not nlp's.
	sreq, _ := http.NewRequest("GET", api+"/events", nil)
	sreq.AddCookie(cookie)
	sctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	stream, err := http.DefaultClient.Do(sreq.WithContext(sctx))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	lines := bufio.NewScanner(stream.Body)
	lines.Scan() // ": hello"
	call(t, "POST", api+"/runs/"+nlp+"/heartbeat", root, nil, nil)
	call(t, "PATCH", api+"/runs/"+nlp, root, map[string]any{"notes": "secret"}, nil)
	call(t, "PATCH", api+"/runs/"+runs["vision"], root, map[string]any{"notes": "hi"}, nil)
	for lines.Scan() {
		if l := lines.Text(); strings.HasPrefix(l, "data: ") {
			if !strings.Contains(l, runs["vision"]) {
				t.Fatalf("alice got %s", l)
			}
			break
		}
	}

	// MCP with alice's key.
	sess := mcpSession(t, ts.URL, alice)
	if _, text := tool(t, sess, "list_runs", nil); strings.Contains(text, "nlp") || !strings.Contains(text, "vision") {
		t.Fatalf("mcp list_runs: %s", text)
	}
	if res, _ := tool(t, sess, "get_run", map[string]any{"run": nlp}); !res.IsError {
		t.Fatal("mcp get_run of a hidden run")
	}
	if res, _ := tool(t, sess, "get_run", map[string]any{"run": "nlp-run"}); !res.IsError {
		t.Fatal("mcp get_run of a hidden run by name")
	}

	// MLflow with alice's key.
	ml := func(method, path, body string) (int, string) {
		req, _ := http.NewRequest(method, ts.URL+"/api/2.0/"+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+alice)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}
	if code, body := ml("GET", "mlflow/runs/get?run_id="+nlp, ""); code != 404 {
		t.Fatalf("mlflow runs/get: %d %s", code, body)
	}
	if code, body := ml("POST", "mlflow/runs/log-metric", `{"run_id":"`+nlp+`","key":"x","value":1,"timestamp":1,"step":0}`); code != 404 {
		t.Fatalf("mlflow log-metric: %d %s", code, body)
	}
	if _, body := ml("POST", "mlflow/experiments/search", `{}`); strings.Contains(body, `"nlp"`) || !strings.Contains(body, `"vision"`) {
		t.Fatalf("mlflow experiments: %s", body)
	}
	if code, body := ml("GET", "mlflow-artifacts/artifacts?path=1/"+nlp+"/artifacts", ""); code != 200 || !strings.Contains(body, `"files":[]`) {
		t.Fatalf("artifact list of a hidden run: %d %s", code, body)
	}

	// A channel of alice's (a push subscription made while logged in) hears only about runs she may see.
	hits := make(chan string, 4)
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m message
		json.NewDecoder(r.Body).Decode(&m)
		hits <- m.Title
	}))
	defer hook.Close()
	var ch int64
	s.db.QueryRow(ctx, `INSERT INTO channels (kind, config, user_id) VALUES ('webhook', jsonb_build_object('url', $1::text), (SELECT id FROM users WHERE name = 'alice')) RETURNING id`, hook.URL).Scan(&ch)
	s.db.Exec(ctx, `INSERT INTO rules (channel_id, event) VALUES ($1, 'finished')`, ch)
	call(t, "PATCH", api+"/runs/"+nlp, root, map[string]any{"status": "finished"}, nil)
	call(t, "PATCH", api+"/runs/"+runs["vision"], root, map[string]any{"status": "finished"}, nil)
	select {
	case title := <-hits:
		if title != "vision/vision-run: Finished" {
			t.Fatalf("alice's channel got %q", title)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no notification")
	}
	select {
	case title := <-hits:
		t.Fatalf("alice's channel also got %q", title)
	case <-time.After(300 * time.Millisecond):
	}

	// A run of her own named like the hidden run's id, and that id in capitals: the guard used to find her run by
	// name while the handler read the hidden one by id.
	call(t, "POST", api+"/runs/"+nlp+"/logs", root, []map[string]any{{"seq": 1, "line": "secret"}}, nil)
	upper := strings.ToUpper(nlp)
	call(t, "POST", api+"/runs", alice, map[string]any{"project": "shared", "name": upper}, nil)
	for _, u := range []string{"/logs", "/metrics", "/media", "/artifacts"} {
		if code := call(t, "GET", api+"/runs/"+upper+u, alice, nil, nil); code != 404 {
			t.Fatalf("GET %s of a hidden run by its id in capitals: %d", u, code)
		}
	}
	if code := call(t, "PATCH", api+"/runs/"+upper, alice, map[string]any{"status": "failed"}, nil); code != 404 {
		t.Fatalf("patching a hidden run by its id in capitals: %d", code)
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
