package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpHandler serves the MCP tools over streamable HTTP. Reads are open like the REST reads; notes and tags need the API key.
func (s *srv) mcpHandler() http.Handler {
	server := mcp.NewServer(&mcp.Implementation{Name: "runyard", Version: "0.1.0"}, &mcp.ServerOptions{
		Instructions: "runyard tracks training runs. Start with list_runs (or list_projects), then get_run for status, config and " +
			"final metrics, get_metrics for curves, tail_logs for output and errors, get_samples to look at generated images. " +
			"When you run experiments, call report after each one with what you tried and found; list_agents shows every agent's card.",
	})
	type listRunsIn struct {
		Project string `json:"project,omitempty" jsonschema:"only runs of this project"`
		Status  string `json:"status,omitempty" jsonschema:"running, finished, failed or crashed"`
		Tag     string `json:"tag,omitempty" jsonschema:"only runs with this tag"`
		Limit   int    `json:"limit,omitempty" jsonschema:"at most this many, newest first (default 30)"`
	}
	type runIn struct {
		Run string `json:"run" jsonschema:"run id, its first characters, or its name (the newest run with that name)"`
	}
	type metricsIn struct {
		Run    string   `json:"run" jsonschema:"run id, id prefix or name"`
		Keys   []string `json:"keys,omitempty" jsonschema:"metric names (default: all except sys/ ones)"`
		Points int      `json:"points,omitempty" jsonschema:"points per curve (default 20)"`
	}
	type logsIn struct {
		Run   string `json:"run" jsonschema:"run id, id prefix or name"`
		Lines int    `json:"lines,omitempty" jsonschema:"how many of the last lines (default 50)"`
	}
	type samplesIn struct {
		Run   string `json:"run" jsonschema:"run id, id prefix or name"`
		Key   string `json:"key,omitempty" jsonschema:"sample key, e.g. samples (default: every key)"`
		Step  *int64 `json:"step,omitempty" jsonschema:"step to show (default: the latest step of each key)"`
		Limit int    `json:"limit,omitempty" jsonschema:"at most this many images (default 4)"`
	}
	type compareIn struct {
		Runs []string `json:"runs" jsonschema:"two or more run ids, id prefixes or names"`
	}
	type alertsIn struct {
		Project string `json:"project,omitempty" jsonschema:"only alerts of this project"`
		Run     string `json:"run,omitempty" jsonschema:"only alerts of this run"`
		Limit   int    `json:"limit,omitempty" jsonschema:"at most this many, newest first (default 20)"`
	}
	type noteIn struct {
		Run  string `json:"run" jsonschema:"run id, id prefix or name"`
		Note string `json:"note" jsonschema:"text appended to the run's notes"`
	}
	type reportIn struct {
		Text    string `json:"text" jsonschema:"what you tried and what came out of it, in a sentence or two, for the people watching"`
		Next    string `json:"next,omitempty" jsonschema:"what you will try next"`
		Run     string `json:"run,omitempty" jsonschema:"the run this is about (id, id prefix or name); it also tells runyard which agent session you are"`
		Session string `json:"session,omitempty" jsonschema:"your session, when no run says it: claude:$CLAUDE_CODE_SESSION_ID in Claude Code (Codex and OpenCode need nothing)"`
	}
	type agentsIn struct {
		Project string `json:"project,omitempty" jsonschema:"only agents working on this project"`
		Hours   int    `json:"hours,omitempty" jsonschema:"active in the last this many hours (default 24)"`
	}
	type metricIn struct {
		Project string `json:"project" jsonschema:"project name"`
		Metric  string `json:"metric" jsonschema:"the metric runs are judged by, e.g. eval/acc; empty goes back to a loss"`
		Goal    string `json:"goal,omitempty" jsonschema:"min or max: which way is better (default min)"`
	}
	type tagsIn struct {
		Run  string   `json:"run" jsonschema:"run id, id prefix or name"`
		Tags []string `json:"tags" jsonschema:"tags to add"`
	}

	mcp.AddTool(server, &mcp.Tool{Name: "list_projects", Description: "Projects with run counts and when their last run started."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
			return jsonResult(listProjects(ctx, s.db))
		})
	mcp.AddTool(server, &mcp.Tool{Name: "list_machines", Description: "The machines running `runyard beacon`: latest CPU, RAM, disk and per-GPU utilization, memory, temperature, power and the processes on each GPU (any user's, not only runyard runs), the running runs on each, and whether it went silent. Use it to see whether a GPU is free before starting a run."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
			return jsonResult(listHosts(ctx, s))
		})
	mcp.AddTool(server, &mcp.Tool{Name: "list_runs", Description: "Runs, newest first, with status, config and summary."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in listRunsIn) (*mcp.CallToolResult, any, error) {
			return jsonResult(listRuns(ctx, s.db, runFilter{Project: in.Project, Status: in.Status, Tag: in.Tag}, or(in.Limit, 30)))
		})
	mcp.AddTool(server, &mcp.Tool{Name: "get_run", Description: "One run: status, timing, host, git, config, summary, and per metric its count, last, min and max."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in runIn) (*mcp.CallToolResult, any, error) {
			id, err := s.resolveRun(ctx, in.Run)
			if err != nil {
				return nil, nil, err
			}
			return jsonResult(getRun(ctx, s.db, id))
		})
	mcp.AddTool(server, &mcp.Tool{Name: "get_metrics", Description: "Metric curves summarized: count, last, min, max and a short [step, value] series per key."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in metricsIn) (*mcp.CallToolResult, any, error) {
			id, err := s.resolveRun(ctx, in.Run)
			if err != nil {
				return nil, nil, err
			}
			return jsonResult(s.metricsSummary(ctx, id, in.Keys, or(in.Points, 20)))
		})
	mcp.AddTool(server, &mcp.Tool{Name: "tail_logs", Description: "The last lines the run printed (stdout and stderr; stderr lines start with !)."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in logsIn) (*mcp.CallToolResult, any, error) {
			id, err := s.resolveRun(ctx, in.Run)
			if err != nil {
				return nil, nil, err
			}
			lines, err := getLogs(ctx, s.db, id, 0, 0, or(in.Lines, 50), true)
			if err != nil {
				return nil, nil, err
			}
			var b strings.Builder
			for _, l := range lines {
				if l.Stream == 2 {
					b.WriteString("! ")
				}
				b.WriteString(l.Line + "\n")
			}
			if len(lines) == 0 {
				b.WriteString("(no output logged)")
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: b.String()}}}, nil, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "get_samples", Description: "The run's sample images (returned as images you can look at) and text samples, at the latest step unless one is given."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in samplesIn) (*mcp.CallToolResult, any, error) {
			id, err := s.resolveRun(ctx, in.Run)
			if err != nil {
				return nil, nil, err
			}
			return s.samples(ctx, id, in.Key, in.Step, or(in.Limit, 4))
		})
	mcp.AddTool(server, &mcp.Tool{Name: "list_artifacts", Description: "Checkpoints, models and other files the run recorded: name, kind, step, where the file lives (uri) and a download url when a copy was uploaded."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in runIn) (*mcp.CallToolResult, any, error) {
			id, err := s.resolveRun(ctx, in.Run)
			if err != nil {
				return nil, nil, err
			}
			return jsonResult(listArtifacts(ctx, s.db, id))
		})
	mcp.AddTool(server, &mcp.Tool{Name: "compare_runs", Description: "Runs side by side: status and duration, the config keys that differ, and last/min of the metrics they share."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in compareIn) (*mcp.CallToolResult, any, error) {
			return jsonResult(s.compare(ctx, in.Runs))
		})
	mcp.AddTool(server, &mcp.Tool{Name: "list_alerts", Description: "Alerts raised by runs (run.alert in the training script) and by server checks (a NaN loss, a crossed threshold, a stuck run), newest first."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in alertsIn) (*mcp.CallToolResult, any, error) {
			run := ""
			if in.Run != "" {
				var err error
				if run, err = s.resolveRun(ctx, in.Run); err != nil {
					return nil, nil, err
				}
			}
			return jsonResult(listAlerts(ctx, s.db, run, in.Project, false, or(in.Limit, 20)))
		})
	mcp.AddTool(server, &mcp.Tool{Name: "add_note", Description: "Appends a note to a run. Needs the API key as a Bearer token."},
		func(ctx context.Context, req *mcp.CallToolRequest, in noteIn) (*mcp.CallToolResult, any, error) {
			id, err := s.authorizedRun(ctx, req, in.Run)
			if err != nil {
				return nil, nil, err
			}
			err = s.addNote(ctx, id, in.Note)
			return jsonResult(map[string]string{"run": id, "added": in.Note}, err)
		})
	mcp.AddTool(server, &mcp.Tool{Name: "add_tags", Description: "Adds tags to a run. Needs the API key as a Bearer token."},
		func(ctx context.Context, req *mcp.CallToolRequest, in tagsIn) (*mcp.CallToolResult, any, error) {
			id, err := s.authorizedRun(ctx, req, in.Run)
			if err != nil {
				return nil, nil, err
			}
			var tags []string
			err = s.db.QueryRow(ctx, `UPDATE runs SET tags = ARRAY(SELECT DISTINCT unnest(tags || $2::text[]) ORDER BY 1) WHERE id = $1::uuid RETURNING tags`, id, in.Tags).Scan(&tags)
			s.hub.publish(event{Type: "run", Run: id})
			return jsonResult(map[string]any{"run": id, "tags": tags}, err)
		})
	mcp.AddTool(server, &mcp.Tool{Name: "report", Description: "Writes what you just tried and found on your agent card, which people watch on the kiosk page " +
		"and list_agents shows. Call it after each experiment. The card's score comes from the project's main metric, not from the report. " +
		"Needs the API key as a Bearer token."},
		func(ctx context.Context, req *mcp.CallToolRequest, in reportIn) (*mcp.CallToolResult, any, error) {
			return jsonResult(s.report(ctx, req, in.Text, in.Next, in.Run, in.Session))
		})
	mcp.AddTool(server, &mcp.Tool{Name: "list_agents", Description: "Coding agent sessions (Claude Code, Codex, OpenCode, ...) that started runs, newest first: " +
		"what each is doing (waiting for a person, working, training, done, error, idle, stale, ended), its runs, the runs tagged keep, its last report."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in agentsIn) (*mcp.CallToolResult, any, error) {
			return jsonResult(listAgents(ctx, s.db, in.Project, min(or(in.Hours, 24), 24*30)))
		})
	mcp.AddTool(server, &mcp.Tool{Name: "set_metric", Description: "Sets the metric a project's runs are judged by and whether lower (min) or higher (max) is better; " +
		"agent cards show its last value and the best so far. Needs the API key as a Bearer token."},
		func(ctx context.Context, req *mcp.CallToolRequest, in metricIn) (*mcp.CallToolResult, any, error) {
			if err := s.mcpKey(ctx, req); err != nil {
				return nil, nil, err
			}
			return jsonResult(in, s.setMetric(ctx, in.Project, in.Metric, in.Goal))
		})

	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
}

func or(n, def int) int {
	if n > 0 {
		return n
	}
	return def
}

func jsonResult(v any, err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		return nil, nil, err
	}
	b, _ := json.MarshalIndent(v, "", " ")
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}, nil, nil
}

// resolveRun accepts a run id, a prefix of one, or a run name (the newest run with that name wins).
func (s *srv) resolveRun(ctx context.Context, ref string) (string, error) {
	var id string
	err := s.db.QueryRow(ctx, `SELECT r.id::text FROM runs r JOIN projects p ON p.id = r.project_id
		WHERE (r.id::text LIKE $1 || '%' OR r.name = $3) AND ($2::bigint[] IS NULL OR p.team_id IS NULL OR p.team_id = ANY($2))
		ORDER BY (r.id::text = $1) DESC, (r.name = $3) DESC, r.started_at DESC LIMIT 1`, // a name wins over another run's id prefix
		strings.ToLower(strings.TrimSpace(ref)), vis(ctx), strings.TrimSpace(ref)).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) || ref == "" {
		return "", fmt.Errorf("no run matches %q; list_runs shows the runs: %w", ref, pgx.ErrNoRows)
	}
	return id, err
}

func (s *srv) authorizedRun(ctx context.Context, req *mcp.CallToolRequest, ref string) (string, error) {
	if err := s.mcpKey(ctx, req); err != nil {
		return "", err
	}
	return s.resolveRun(ctx, ref)
}

func (s *srv) addNote(ctx context.Context, run, note string) error {
	_, err := s.db.Exec(ctx, `UPDATE runs SET notes = CASE WHEN notes = '' THEN $2 ELSE notes || E'\n' || $2 END WHERE id = $1::uuid`, run, note)
	s.hub.publish(event{Type: "run", Run: run})
	return err
}

func (s *srv) mcpKey(ctx context.Context, req *mcp.CallToolRequest) error {
	key, _ := strings.CutPrefix(req.Extra.Header.Get("Authorization"), "Bearer ")
	if key == "" || !validKey(ctx, s.db, key, "write") {
		return errors.New("writing needs the runyard API key: configure the MCP server with the header Authorization: Bearer <key>")
	}
	return nil
}

type curve struct {
	metricSummary
	Series [][2]num `json:"series"` // [step, value] pairs, downsampled
}

func (s *srv) metricsSummary(ctx context.Context, id string, keys []string, points int) (map[string]curve, error) {
	run, err := getRun(ctx, s.db, id)
	if err != nil {
		return nil, err
	}
	series, err := getMetrics(ctx, s.db, id, keys, points)
	if err != nil {
		return nil, err
	}
	out := map[string]curve{}
	for k, sr := range series {
		if len(keys) == 0 && strings.HasPrefix(k, "sys/") {
			continue
		}
		c := curve{metricSummary: run.Metrics[k]}
		for i := range sr.Step {
			c.Series = append(c.Series, [2]num{num(sr.Step[i]), sr.Last[i]})
		}
		out[k] = c
	}
	return out, nil
}

func (s *srv) samples(ctx context.Context, id, key string, step *int64, limit int) (*mcp.CallToolResult, any, error) {
	all, err := listMedia(ctx, s.db, id, key, "", -1)
	if err != nil {
		return nil, nil, err
	}
	at := map[string]int64{} // key -> the step to show
	for _, m := range all {
		if step != nil {
			at[m.Key] = *step
		} else if m.Step >= at[m.Key] {
			at[m.Key] = m.Step
		}
	}
	res := &mcp.CallToolResult{}
	images := 0
	for _, m := range all {
		if s, ok := at[m.Key]; !ok || m.Step != s {
			continue
		}
		var meta struct {
			Caption string `json:"caption"`
		}
		json.Unmarshal(m.Meta, &meta)
		label := fmt.Sprintf("%s at step %d", m.Key, m.Step)
		if meta.Caption != "" {
			label += ": " + meta.Caption
		}
		switch {
		case m.Kind == "text":
			b, err := readBlob(ctx, s.store, m.Path)
			if err != nil {
				continue
			}
			res.Content = append(res.Content, &mcp.TextContent{Text: label + "\n" + string(b)})
		case m.Kind == "image" && images < limit:
			b, err := readBlob(ctx, s.store, m.Path)
			if err != nil {
				continue
			}
			images++
			mime := map[string]string{".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp"}[strings.ToLower(filepath.Ext(m.Path))]
			res.Content = append(res.Content, &mcp.TextContent{Text: label}, &mcp.ImageContent{Data: b, MIMEType: or2(mime, "image/png")})
		default:
			res.Content = append(res.Content, &mcp.TextContent{Text: label + " (" + m.Kind + ", not returned: " + m.URL + ")"})
		}
	}
	if len(res.Content) == 0 {
		res.Content = []mcp.Content{&mcp.TextContent{Text: "No samples logged for this run yet."}}
	}
	return res, nil, nil
}

func or2(s, def string) string {
	if s != "" {
		return s
	}
	return def
}

func (s *srv) compare(ctx context.Context, refs []string) (any, error) {
	if len(refs) < 2 {
		return nil, errors.New("give at least two runs")
	}
	type runInfo struct {
		ID       string          `json:"id"`
		Name     string          `json:"name"`
		Status   string          `json:"status"`
		Duration string          `json:"duration"`
		Summary  json.RawMessage `json:"summary"`
	}
	var runs []Run
	var info []runInfo
	for _, ref := range refs {
		id, err := s.resolveRun(ctx, ref)
		if err != nil {
			return nil, err
		}
		r, err := getRun(ctx, s.db, id)
		if err != nil {
			return nil, err
		}
		end := time.Now()
		if r.EndedAt != nil {
			end = *r.EndedAt
		}
		runs = append(runs, r)
		info = append(info, runInfo{r.ID, r.Name, r.Status, fmtDuration(end.Sub(r.StartedAt)), r.Summary})
	}
	configs := make([]map[string]any, len(runs))
	keys := map[string]bool{}
	for i, r := range runs {
		configs[i] = map[string]any{}
		json.Unmarshal(r.Config, &configs[i])
		for k := range configs[i] {
			keys[k] = true
		}
	}
	diff := map[string][]any{}
	for k := range keys {
		vals := make([]any, len(runs))
		same := true
		for i := range runs {
			vals[i] = configs[i][k]
			a, _ := json.Marshal(vals[i])
			b, _ := json.Marshal(vals[0])
			same = same && string(a) == string(b)
		}
		if !same {
			diff[k] = vals
		}
	}
	type lastMin struct {
		Last num `json:"last"`
		Min  num `json:"min"`
	}
	metrics := map[string][]lastMin{}
	var shared []string
	for k := range runs[0].Metrics {
		ok := !strings.HasPrefix(k, "sys/")
		for _, r := range runs[1:] {
			_, has := r.Metrics[k]
			ok = ok && has
		}
		if ok {
			shared = append(shared, k)
		}
	}
	sort.Strings(shared)
	for _, k := range shared {
		for _, r := range runs {
			metrics[k] = append(metrics[k], lastMin{r.Metrics[k].Last, r.Metrics[k].Min})
		}
	}
	return map[string]any{"runs": info, "config_diff": diff, "metrics": metrics}, nil
}
