package main

import (
	"io"
	"net/http"
	"testing"
)

func TestArtifacts(t *testing.T) {
	ts, key, _ := testServer(t)
	api := ts.URL + "/api/v1"
	call(t, "POST", api+"/runs", key, map[string]any{"id": runID, "project": "lora"}, nil)

	// A record of a checkpoint that stays on the training machine.
	var rec Artifact
	call(t, "POST", api+"/runs/"+runID+"/artifacts", key, map[string]any{"name": "checkpoint-500", "kind": "checkpoint", "step": 500,
		"uri": "/data/out/checkpoint-500", "size": 123456789, "meta": map[string]any{"loss": 0.1}}, &rec)
	if rec.ID == "" || rec.URL != "" || rec.Kind != "checkpoint" {
		t.Fatalf("record: %+v", rec)
	}

	// An uploaded copy, sent twice (a retry): one artifact, the first file.
	form := map[string]string{"id": "22222222-3333-4444-8555-666666666666", "name": "lora/owl.safetensors", "kind": "model", "step": "1000"}
	var up Artifact
	call(t, "POST", api+"/runs/"+runID+"/artifacts", key, mediaForm(form, "owl.safetensors", []byte("weights")), &up)
	call(t, "POST", api+"/runs/"+runID+"/artifacts", key, mediaForm(form, "owl.safetensors", []byte("other")), nil)
	if up.URL == "" || up.Size == nil || *up.Size != 7 {
		t.Fatalf("upload: %+v", up)
	}

	var list []Artifact
	call(t, "GET", api+"/runs/"+runID+"/artifacts", "", nil, &list)
	if len(list) != 2 || list[0].Name != "checkpoint-500" || *list[0].Step != 500 || list[1].ID != form["id"] {
		t.Fatalf("list: %+v", list)
	}
	res, err := http.Get(ts.URL + up.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if string(body) != "weights" || res.Header.Get("Content-Disposition") != `attachment; filename=owl.safetensors` {
		t.Fatalf("download: %q %v", body, res.Header)
	}
	if res, _ := http.Get(api + "/artifacts/" + rec.ID + "/file"); res.StatusCode != 404 {
		t.Fatalf("a record without a copy has no file: %d", res.StatusCode)
	}
	if code := call(t, "POST", api+"/runs/"+runID+"/artifacts", key, map[string]any{"kind": "x"}, nil); code != 400 {
		t.Fatalf("no name: %d", code)
	}
}
