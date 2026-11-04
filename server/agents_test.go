package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAgents(t *testing.T) {
	ts, key, _ := testServer(t)
	api := ts.URL + "/api/v1"
	agents := func() map[string]Agent {
		var list []Agent
		call(t, "GET", api+"/agents", "", nil, &list)
		out := map[string]Agent{}
		for _, a := range list {
			out[a.ID] = a
		}
		return out
	}

	// A run started from Claude Code names its session; a malformed agent is dropped, never the run.
	var r map[string]string
	call(t, "POST", api+"/runs", key, map[string]any{"project": "gba", "name": "lr3e-4", "host": "toji",
		"agent": map[string]any{"tool": "claude", "session": "s1", "cwd": "/repo"}}, &r)
	call(t, "POST", api+"/runs", key, map[string]any{"project": "gba", "name": "hand", "agent": map[string]any{"tool": "Bad Tool", "session": "x"}}, nil)
	var runs []Run
	call(t, "GET", api+"/runs?project=gba", "", nil, &runs)
	if len(runs) != 2 || runs[0].Agent != nil || runs[1].Agent == nil || *runs[1].Agent != "claude:s1" {
		t.Fatalf("runs: %+v", runs)
	}
	a := agents()["claude:s1"]
	if len(agents()) != 1 || a.State != "training" || a.Runs != 1 || a.Running != 1 || a.Host != "toji" || a.CWD != "/repo" || *a.Project != "gba" {
		t.Fatalf("after the run started: %+v", agents())
	}
	call(t, "PATCH", api+"/runs/"+r["id"], key, map[string]any{"status": "finished", "tags": []string{"keep"}}, nil)
	if a := agents()["claude:s1"]; a.State != "done" || a.Kept != 1 || a.LastRun.Name != "lr3e-4" {
		t.Fatalf("after it finished: %+v", a)
	}

	// Hooks: 204 with no body, a key needed, untracked events ignored.
	hook := func(tool string, body map[string]any) int {
		return call(t, "POST", api+"/agents/events?tool="+tool, key, body, nil)
	}
	if code := call(t, "POST", api+"/agents/events?tool=claude", "", map[string]any{"session_id": "s1", "hook_event_name": "Stop"}, nil); code != 401 {
		t.Fatalf("hook without a key: %d", code)
	}
	if code := hook("claude", map[string]any{"session_id": "s1", "hook_event_name": "PermissionRequest"}); code != 204 {
		t.Fatalf("hook: %d", code)
	}
	hook("claude", map[string]any{"session_id": "s1", "hook_event_name": "SubagentStart"})
	if a := agents()["claude:s1"]; a.State != "waiting" {
		t.Fatalf("PermissionRequest: %+v", a)
	}
	// The approved command is the training: PostToolUse only comes when it exits, so the run starting ends the wait.
	var r2 map[string]string
	call(t, "POST", api+"/runs", key, map[string]any{"project": "gba", "name": "second", "agent": map[string]any{"tool": "claude", "session": "s1"}}, &r2)
	if a := agents()["claude:s1"]; a.State != "training" {
		t.Fatalf("a run started after the prompt: %+v", a)
	}
	call(t, "PATCH", api+"/runs/"+r2["id"], key, map[string]any{"status": "finished"}, nil)
	hook("claude", map[string]any{"session_id": "s1", "hook_event_name": "Stop", "last_assistant_message": "lr 3e-4 wins"})
	if a := agents()["claude:s1"]; a.State != "done" || a.Said != "lr 3e-4 wins" {
		t.Fatalf("Stop: %+v", a)
	}
	// A session seen only through its hooks has no card and leaves no message behind: it may have nothing to do with training.
	hook("codex", map[string]any{"session_id": "c1", "hook_event_name": "UserPromptSubmit", "cwd": "/other"})
	hook("codex", map[string]any{"session_id": "c1", "hook_event_name": "Stop", "last_assistant_message": "private"})
	if a, ok := agents()["codex:c1"]; ok {
		t.Fatalf("a session with no run or report: %+v", a)
	}
	// Hooks installed for a repository: a card from the first prompt, so its first permission prompt shows.
	hook("claude&card=1", map[string]any{"session_id": "repo", "hook_event_name": "PermissionRequest"})
	hook("claude&card=1", map[string]any{"session_id": "repo", "hook_event_name": "Stop", "last_assistant_message": "asked"})
	if a := agents()["claude:repo"]; a.State != "idle" || a.Said != "asked" {
		t.Fatalf("card=1: %+v", a)
	}

	// report: needs the key, finds the session from the run, from Codex's _meta, or from the session argument.
	sess := mcpSession(t, ts.URL, "")
	if res, _ := tool(t, sess, "report", map[string]any{"text": "x", "run": "lr3e-4"}); !res.IsError {
		t.Fatal("report without a key should fail")
	}
	sess = mcpSession(t, ts.URL, key)
	tool(t, sess, "report", map[string]any{"text": "3e-4 beats 1e-3", "next": "try 1e-4", "run": "lr3e-4"})
	if a := agents()["claude:s1"]; a.Report != "3e-4 beats 1e-3" || a.Next != "try 1e-4" || a.ReportRun == nil || *a.ReportRun != "lr3e-4" {
		t.Fatalf("report by run: %+v", a)
	}
	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{Name: "report", Arguments: map[string]any{"text": "from codex"},
		Meta: mcp.Meta{"x-codex-turn-metadata": map[string]any{"session_id": "c1"}}})
	if a := agents()["codex:c1"]; err != nil || res.IsError || a.Report != "from codex" || a.Project != nil || a.CWD != "/other" || a.Said != "" {
		t.Fatalf("report by _meta: %v %+v %+v", err, res, a)
	}
	hook("codex", map[string]any{"session_id": "c1", "hook_event_name": "Stop", "last_assistant_message": "from its card"})
	if a := agents()["codex:c1"]; a.Said != "from its card" {
		t.Fatalf("said once it has a card: %+v", a)
	}
	tool(t, sess, "report", map[string]any{"text": "bare id", "session": "s1"})
	tool(t, sess, "report", map[string]any{"text": "new", "session": "opencode:o1"})
	if a := agents(); a["claude:s1"].Report != "bare id" || a["opencode:o1"].Report != "new" {
		t.Fatalf("report by session: %+v", a)
	}
	if res, text := tool(t, sess, "report", map[string]any{"text": "who am I"}); !res.IsError || !strings.Contains(text, "session") {
		t.Fatalf("report with no session: %s", text)
	}
	if _, text := tool(t, sess, "list_agents", map[string]any{"project": "gba"}); !strings.Contains(text, `"claude:s1"`) || strings.Contains(text, "codex") {
		t.Fatalf("list_agents: %s", text)
	}

	// The project's main metric.
	tool(t, sess, "set_metric", map[string]any{"project": "gba", "metric": "eval/acc", "goal": "max"})
	if code := call(t, "PATCH", api+"/projects/gba", "", map[string]any{"metric": "loss", "goal": "up"}, nil); code != 400 {
		t.Fatalf("bad goal: %d", code)
	}
	var projects []Project
	call(t, "GET", api+"/projects", "", nil, &projects)
	if a := agents()["claude:s1"]; projects[0].Metric != "eval/acc" || projects[0].Goal != "max" || a.Metric != "eval/acc" || a.Goal != "max" {
		t.Fatalf("metric: %+v %+v", projects, a)
	}
}

func TestAgentState(t *testing.T) {
	now := time.Now()
	ago := func(d time.Duration) *time.Time { t := now.Add(-d); return &t }
	for _, c := range []struct {
		a    Agent
		want string
	}{
		{Agent{hook: "waiting", hookAt: ago(time.Minute), Running: 1, ActiveAt: now}, "waiting"},
		{Agent{hook: "waiting", hookAt: ago(3 * time.Hour), ActiveAt: *ago(3 * time.Hour)}, "stale"}, // killed at its prompt
		{Agent{hook: "waiting", hookAt: ago(3 * time.Hour), Running: 1, ActiveAt: now}, "training"},
		{Agent{hook: "working", hookAt: ago(time.Minute), Running: 1, ActiveAt: now}, "working"},
		{Agent{hook: "working", hookAt: ago(time.Hour), Running: 1, ActiveAt: now}, "training"}, // the CLI died mid-turn
		{Agent{hook: "ended", Running: 1, ActiveAt: now}, "training"},                           // the session closed, training goes on
		{Agent{hook: "ended", ActiveAt: now}, "ended"},
		{Agent{LastRun: &agentRun{Status: "crashed"}, ActiveAt: *ago(3 * time.Hour)}, "stale"},
		{Agent{LastRun: &agentRun{Status: "crashed"}, ActiveAt: now}, "error"},
		{Agent{LastRun: &agentRun{Status: "finished", EndedAt: ago(time.Minute)}, ActiveAt: now}, "done"},
		{Agent{LastRun: &agentRun{Status: "finished", EndedAt: ago(time.Hour)}, ActiveAt: now}, "idle"},
	} {
		if got := c.a.state(now); got != c.want {
			t.Errorf("%+v: %s, want %s", c.a, got, c.want)
		}
	}
}
