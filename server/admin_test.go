package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestAdmin: deleting runs and projects (with their files), API keys and channels, for admins only, by cookie or key.
func TestAdmin(t *testing.T) {
	ts, root, s := testServer(t)
	ctx := context.Background()
	api := ts.URL + "/api/v1"

	// Without RUNYARD_AUTH an admin call needs any API key, as a write does.
	if code := call(t, "GET", api+"/keys", "", nil, nil); code != 401 {
		t.Fatalf("no key, auth off: %d", code)
	}
	if code := call(t, "GET", api+"/keys", root, nil, nil); code != 200 {
		t.Fatalf("key, auth off: %d", code)
	}

	s.auth = true
	s.db.Exec(ctx, `INSERT INTO users (name, password, admin) VALUES ('alice', $1, false), ('boss', $2, true)`, hashPassword("a"), hashPassword("b"))
	alice, _ := createKey(ctx, s.db, "alice-laptop", "alice", false)
	res, err := http.Post(api+"/login", "application/json", strings.NewReader(`{"name":"boss","password":"b"}`))
	if err != nil || res.StatusCode != 200 {
		t.Fatal("login", err)
	}
	cookie := res.Cookies()[0]
	// boss calls with the session cookie, as the web UI does; header pairs are added to the request.
	boss := func(method, path string, body any, out any, header ...string) int {
		t.Helper()
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest(method, api+path, bytes.NewReader(b))
		req.AddCookie(cookie)
		for i := 0; i+1 < len(header); i += 2 {
			req.Header.Set(header[i], header[i+1])
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		if out != nil {
			json.Unmarshal(raw, out)
		}
		return res.StatusCode
	}

	// Two runs in one project, each with an image and an uploaded artifact.
	ids := []string{newUUID(), newUUID()}
	var files []string
	for _, id := range ids {
		call(t, "POST", api+"/runs", root, map[string]any{"id": id, "project": "doomed"}, nil)
		var m map[string]string
		call(t, "POST", api+"/runs/"+id+"/media", root, mediaForm(map[string]string{"key": "s", "step": "1", "kind": "image"}, "a.png", []byte("png")), &m)
		var a Artifact
		call(t, "POST", api+"/runs/"+id+"/artifacts", root, mediaForm(map[string]string{"name": "ckpt"}, "ckpt.pt", []byte("w")), &a)
		files = append(files, strings.TrimPrefix(m["url"], "/media/"), id+"/artifacts/"+a.ID+".pt")
	}
	dir := string(s.store.(localBlobs))
	gone := func(key string) bool {
		for range 50 {
			if _, err := os.Stat(filepath.Join(dir, key)); os.IsNotExist(err) {
				return true
			}
			time.Sleep(20 * time.Millisecond)
		}
		return false
	}
	for _, f := range files {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Fatal(err)
		}
	}

	// Not an admin: refused, by key or with no login.
	for _, u := range []string{"/keys", "/channels"} {
		if code := call(t, "GET", api+u, alice, nil, nil); code != 403 {
			t.Fatalf("alice GET %s: %d", u, code)
		}
	}
	if code := call(t, "DELETE", api+"/runs/"+ids[0], alice, nil, nil); code != 403 {
		t.Fatalf("alice deletes a run: %d", code)
	}
	if code := call(t, "DELETE", api+"/projects/doomed", "", nil, nil); code != 401 {
		t.Fatalf("anonymous deletes a project: %d", code)
	}
	// A cookie sent from another site is refused (CSRF).
	if code := boss("DELETE", "/runs/"+ids[0], nil, nil, "Sec-Fetch-Site", "cross-site"); code != 403 {
		t.Fatalf("cross-site delete: %d", code)
	}

	// Delete one run: its rows and files go, the other run stays.
	if code := boss("DELETE", "/runs/"+ids[0], nil, nil, "Sec-Fetch-Site", "same-origin"); code != 200 {
		t.Fatalf("delete run: %d", code)
	}
	if code := call(t, "GET", api+"/runs/"+ids[0], root, nil, nil); code != 404 {
		t.Fatalf("deleted run: %d", code)
	}
	if !gone(files[0]) || !gone(files[1]) {
		t.Fatal("the deleted run's files are still there")
	}
	if _, err := os.Stat(filepath.Join(dir, files[2])); err != nil {
		t.Fatal("the other run's file went too")
	}
	if code := boss("DELETE", "/runs/"+ids[0], nil, nil); code != 404 {
		t.Fatalf("delete it again: %d", code)
	}

	// Delete the project with a server key: the other run and its files go too.
	if code := call(t, "DELETE", api+"/projects/doomed", root, nil, nil); code != 200 {
		t.Fatalf("delete project: %d", code)
	}
	var projects []Project
	call(t, "GET", api+"/projects", root, nil, &projects)
	if len(projects) != 0 || !gone(files[2]) || !gone(files[3]) {
		t.Fatalf("after deleting the project: %v", projects)
	}
	if code := call(t, "DELETE", api+"/projects/doomed", root, nil, nil); code != 404 {
		t.Fatalf("delete it again: %d", code)
	}

	// Keys: made once, listed without the secret, revoked.
	var made map[string]string
	if code := boss("POST", "/keys", map[string]string{"name": "agent", "user": "alice"}, &made); code != 200 || !strings.HasPrefix(made["key"], "ry_") {
		t.Fatalf("create key: %d %v", code, made)
	}
	if code := boss("POST", "/keys", map[string]string{"name": "x", "user": "nobody"}, nil); code != 404 {
		t.Fatalf("key for a missing user: %d", code)
	}
	var keys []struct {
		ID   int64
		Name string
		User string
	}
	boss("GET", "/keys", nil, &keys)
	if len(keys) != 3 || keys[2].Name != "agent" || keys[2].User != "alice" {
		t.Fatalf("keys: %+v", keys)
	}
	if code := call(t, "GET", api+"/runs", made["key"], nil, nil); code != 200 {
		t.Fatalf("the new key: %d", code)
	}
	if code := boss("DELETE", "/keys/"+strconv.FormatInt(keys[2].ID, 10), nil, nil); code != 200 {
		t.Fatalf("revoke: %d", code)
	}
	if code := call(t, "GET", api+"/runs", made["key"], nil, nil); code != 401 {
		t.Fatalf("a revoked key: %d", code)
	}

	// Channels: the CLI and the API share one list; tokens never come back.
	hits := make(chan string, 4)
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m message
		json.NewDecoder(r.Body).Decode(&m)
		hits <- m.Title
	}))
	defer hook.Close()
	if err := channelCmd(ctx, s.db, []string{"add", "-token", "tk_secret", "ntfy", "https://ntfy.example/topic"}); err != nil {
		t.Fatal(err)
	}
	var added map[string]int64
	if code := boss("POST", "/channels", map[string]any{"kind": "webhook", "target": hook.URL, "project": "sft", "events": []string{"failed"}}, &added); code != 200 {
		t.Fatalf("add channel: %d", code)
	}
	for _, bad := range []map[string]any{{"kind": "email", "target": "x"}, {"kind": "telegram", "target": "123"}, {"kind": "webhook", "events": []string{"done"}, "target": "x"}} {
		if code := boss("POST", "/channels", bad, nil); code != 400 {
			t.Fatalf("bad channel %v: %d", bad, code)
		}
	}
	var chans []channelInfo
	boss("GET", "/channels", nil, &chans)
	raw, _ := json.Marshal(chans)
	if len(chans) != 2 || strings.Contains(string(raw), "tk_secret") || chans[0].Project != "" || len(chans[0].Events) != 4 ||
		chans[1].Target != hook.URL || chans[1].Project != "sft" || strings.Join(chans[1].Events, ",") != "failed" {
		t.Fatalf("channels: %s", raw)
	}
	id := strconv.FormatInt(added["id"], 10)
	if code := boss("POST", "/channels/"+id+"/test", nil, nil); code != 200 || <-hits != "runyard: Test" {
		t.Fatalf("test: %d", code)
	}
	if code := boss("DELETE", "/channels/"+id, nil, nil); code != 200 {
		t.Fatalf("remove: %d", code)
	}
	if code := boss("POST", "/channels/"+id+"/test", nil, nil); code != 404 {
		t.Fatalf("test a removed channel: %d", code)
	}
	if code := boss("DELETE", "/channels/"+id, nil, nil); code != 404 {
		t.Fatalf("remove it again: %d", code)
	}
}
