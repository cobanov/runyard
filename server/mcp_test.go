package main

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type bearer struct{ key string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	if b.key != "" {
		r = r.Clone(r.Context())
		r.Header.Set("Authorization", "Bearer "+b.key)
	}
	return http.DefaultTransport.RoundTrip(r)
}

func mcpSession(t *testing.T, url, key string) *mcp.ClientSession {
	c := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil)
	sess, err := c.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: url + "/mcp", HTTPClient: &http.Client{Transport: bearer{key}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sess.Close() })
	return sess
}

func tool(t *testing.T, sess *mcp.ClientSession, name string, args map[string]any) (*mcp.CallToolResult, string) {
	t.Helper()
	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var text strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text.WriteString(tc.Text + "\n")
		}
	}
	return res, text.String()
}

func TestMCP(t *testing.T) {
	ts, key, _ := testServer(t)
	api := ts.URL + "/api/v1"
	for i, name := range []string{"pixel32", "pixel64"} {
		var r map[string]string
		call(t, "POST", api+"/runs", key, map[string]any{"project": "gba", "name": name, "config": map[string]any{"size": 32 * (i + 1), "T": 16}}, &r)
		id := r["id"]
		call(t, "POST", api+"/runs/"+id+"/metrics", key, []map[string]any{{"step": 1, "values": map[string]any{"loss": 1.0}}, {"step": 2, "values": map[string]any{"loss": 0.5 - 0.1*float64(i)}}}, nil)
		call(t, "POST", api+"/runs/"+id+"/logs", key, []map[string]any{{"seq": 1, "line": "step 1"}, {"seq": 2, "stream": 2, "line": "oops"}}, nil)
		call(t, "POST", api+"/runs/"+id+"/media", key, mediaForm(map[string]string{"key": "samples", "step": "1", "kind": "image"}, "a.png", []byte("old")), nil)
		call(t, "POST", api+"/runs/"+id+"/media", key, mediaForm(map[string]string{"key": "samples", "step": "2", "kind": "image", "meta": `{"caption":"grid"}`}, "b.png", []byte("new")), nil)
	}

	sess := mcpSession(t, ts.URL, "")
	if _, text := tool(t, sess, "list_runs", map[string]any{"project": "gba"}); !strings.Contains(text, "pixel64") {
		t.Fatalf("list_runs: %s", text)
	}
	if _, text := tool(t, sess, "get_run", map[string]any{"run": "pixel32"}); !strings.Contains(text, `"last": 0.5`) {
		t.Fatalf("get_run by name: %s", text)
	}
	if _, text := tool(t, sess, "get_metrics", map[string]any{"run": "pixel64"}); !strings.Contains(text, "series") {
		t.Fatalf("get_metrics: %s", text)
	}
	if _, text := tool(t, sess, "tail_logs", map[string]any{"run": "pixel32", "lines": 1}); text != "! oops\n\n" {
		t.Fatalf("tail_logs: %q", text)
	}
	res, text := tool(t, sess, "get_samples", map[string]any{"run": "pixel32"})
	img, ok := res.Content[len(res.Content)-1].(*mcp.ImageContent)
	if !ok || string(img.Data) != "new" || img.MIMEType != "image/png" || !strings.Contains(text, "samples at step 2: grid") {
		t.Fatalf("get_samples: %s %+v", text, res.Content)
	}
	if _, text := tool(t, sess, "compare_runs", map[string]any{"runs": []string{"pixel32", "pixel64"}}); !strings.Contains(text, `"size": [`) || strings.Contains(text, `"T"`) {
		t.Fatalf("compare_runs: %s", text)
	}
	if res, _ := tool(t, sess, "get_run", map[string]any{"run": "nope"}); !res.IsError {
		t.Fatal("unknown run should be a tool error")
	}
	if res, _ := tool(t, sess, "add_tags", map[string]any{"run": "pixel32", "tags": []string{"best"}}); !res.IsError {
		t.Fatal("writing without a key should fail")
	}

	sess = mcpSession(t, ts.URL, key)
	tool(t, sess, "add_tags", map[string]any{"run": "pixel32", "tags": []string{"best", "best"}})
	tool(t, sess, "add_note", map[string]any{"run": "pixel32", "note": "sharpest samples"})
	var runs []Run
	call(t, "GET", api+"/runs?tag=best", "", nil, &runs)
	if len(runs) != 1 || runs[0].Notes != "sharpest samples" || len(runs[0].Tags) != 1 {
		t.Fatalf("after writes: %+v", runs)
	}
}
