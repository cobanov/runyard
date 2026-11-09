package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// The MLflow tracking API, enough for mlflow's own client: set MLFLOW_TRACKING_URI to the runyard server and
// MLFLOW_TRACKING_TOKEN to an API key, and mlflow.start_run/log_*, mlflow.autolog, report_to="mlflow" and
// Lightning's MLFlowLogger write here. Experiments are projects, params go to the config, metrics to metrics,
// artifacts and logged models to artifacts.
// shortcut: search ignores filters and ordering (newest first); add them when a tool depends on them.

type mlErr struct {
	status     int
	code, text string
}

func (e mlErr) Error() string { return e.text }

func notFound(what string) error { return mlErr{404, "RESOURCE_DOES_NOT_EXIST", what + " not found"} }

type kv struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type mlMetric struct {
	Key       string `json:"key"`
	Value     num    `json:"value"`
	Timestamp int64  `json:"timestamp"`
	Step      int64  `json:"step"`
}

func (s *srv) mlflowRoutes(mux *http.ServeMux) {
	h := func(pattern string, write bool, f func(*http.Request) (any, error)) {
		mux.HandleFunc(pattern, s.ml(write, f))
	}
	const p = "/api/2.0/mlflow/"
	h("GET "+p+"experiments/get-by-name", false, func(r *http.Request) (any, error) {
		e, err := s.mlExperiment(r.Context(), `name = $1`, r.URL.Query().Get("experiment_name"))
		return map[string]any{"experiment": e}, err
	})
	h("GET "+p+"experiments/get", false, func(r *http.Request) (any, error) {
		id, err := s.mlProject(r.Context(), r.URL.Query().Get("experiment_id"))
		if err != nil {
			return nil, err
		}
		e, err := s.mlExperiment(r.Context(), `id = $1`, id)
		return map[string]any{"experiment": e}, err
	})
	h("POST "+p+"experiments/create", true, func(r *http.Request) (any, error) {
		var in struct{ Name string }
		if err := mlDecode(r, &in); err != nil || in.Name == "" {
			return nil, mlErr{400, "INVALID_PARAMETER_VALUE", "an experiment needs a name"}
		}
		var id int64
		err := s.db.QueryRow(r.Context(), `INSERT INTO projects (name, team_id) VALUES ($1, $2) ON CONFLICT DO NOTHING RETURNING id`,
			in.Name, projectTeam(r.Context())).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, mlErr{400, "RESOURCE_ALREADY_EXISTS", "experiment " + in.Name + " already exists"}
		}
		return map[string]string{"experiment_id": strconv.FormatInt(id, 10)}, err
	})
	h("POST "+p+"experiments/search", false, func(r *http.Request) (any, error) {
		rows, _ := s.db.Query(r.Context(), `SELECT id FROM projects p WHERE $1::bigint[] IS NULL OR p.team_id IS NULL OR p.team_id = ANY($1) ORDER BY id`, vis(r.Context()))
		ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
		out := []mlExp{}
		for _, id := range ids {
			e, _ := s.mlExperiment(r.Context(), `id = $1`, id)
			out = append(out, e)
		}
		return map[string]any{"experiments": out}, err
	})

	h("POST "+p+"runs/create", true, s.mlCreateRun)
	h("POST "+p+"runs/update", true, func(r *http.Request) (any, error) {
		var in struct {
			RunID   string `json:"run_id"`
			Status  string `json:"status"`
			RunName string `json:"run_name"`
			EndTime int64  `json:"end_time"`
		}
		if err := mlDecode(r, &in); err != nil {
			return nil, err
		}
		if err := s.mlSees(r.Context(), in.RunID); err != nil {
			return nil, err
		}
		var p runPatch
		if st, ok := map[string]string{"RUNNING": "running", "SCHEDULED": "running", "FINISHED": "finished", "FAILED": "failed", "KILLED": "failed"}[in.Status]; ok {
			p.Status = &st
		}
		if in.RunName != "" {
			p.Name = &in.RunName
		}
		if _, err := s.patch(r.Context(), in.RunID, p); err != nil {
			return nil, err
		}
		run, err := s.mlRun(r.Context(), in.RunID)
		return map[string]any{"run_info": run["info"]}, err
	})
	h("GET "+p+"runs/get", false, func(r *http.Request) (any, error) {
		run, err := s.mlRun(r.Context(), r.URL.Query().Get("run_id"))
		return map[string]any{"run": run}, err
	})
	h("POST "+p+"runs/search", false, func(r *http.Request) (any, error) {
		var in struct {
			ExperimentIDs []string `json:"experiment_ids"`
			MaxResults    int      `json:"max_results"`
		}
		if err := mlDecode(r, &in); err != nil {
			return nil, err
		}
		rows, _ := s.db.Query(r.Context(), `SELECT id::text FROM runs WHERE project_id::text = ANY($1) ORDER BY started_at DESC LIMIT $2`,
			in.ExperimentIDs, min(or(in.MaxResults, 100), 1000))
		ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
		out := []map[string]any{}
		for _, id := range ids {
			if run, err := s.mlRun(r.Context(), id); err == nil {
				out = append(out, run)
			}
		}
		return map[string]any{"runs": out}, err
	})
	h("POST "+p+"runs/log-metric", true, func(r *http.Request) (any, error) {
		var in struct {
			RunID string `json:"run_id"`
			mlMetric
		}
		if err := mlDecode(r, &in); err != nil {
			return nil, err
		}
		return struct{}{}, s.mlLog(r.Context(), in.RunID, []mlMetric{in.mlMetric}, nil, nil)
	})
	h("POST "+p+"runs/log-parameter", true, func(r *http.Request) (any, error) {
		var in struct {
			RunID string `json:"run_id"`
			kv
		}
		if err := mlDecode(r, &in); err != nil {
			return nil, err
		}
		return struct{}{}, s.mlLog(r.Context(), in.RunID, nil, []kv{in.kv}, nil)
	})
	h("POST "+p+"runs/set-tag", true, func(r *http.Request) (any, error) {
		var in struct {
			RunID string `json:"run_id"`
			kv
		}
		if err := mlDecode(r, &in); err != nil {
			return nil, err
		}
		return struct{}{}, s.mlLog(r.Context(), in.RunID, nil, nil, []kv{in.kv})
	})
	h("POST "+p+"runs/delete-tag", true, func(r *http.Request) (any, error) {
		var in struct {
			RunID string `json:"run_id"`
			Key   string `json:"key"`
		}
		if err := mlDecode(r, &in); err != nil {
			return nil, err
		}
		if err := s.mlSees(r.Context(), in.RunID); err != nil {
			return nil, err
		}
		_, err := s.db.Exec(r.Context(), `UPDATE runs SET tags = ARRAY(SELECT t FROM unnest(tags) t WHERE split_part(t, '=', 1) <> $2) WHERE id = $1::uuid`, in.RunID, in.Key)
		return struct{}{}, err
	})
	h("POST "+p+"runs/log-batch", true, func(r *http.Request) (any, error) {
		var in struct {
			RunID   string     `json:"run_id"`
			Metrics []mlMetric `json:"metrics"`
			Params  []kv       `json:"params"`
			Tags    []kv       `json:"tags"`
		}
		if err := mlDecode(r, &in); err != nil {
			return nil, err
		}
		return struct{}{}, s.mlLog(r.Context(), in.RunID, in.Metrics, in.Params, in.Tags)
	})
	for _, ignored := range []string{"runs/log-inputs", "runs/outputs", "runs/log-model"} { // datasets and model links: nothing to show yet
		h("POST "+p+ignored, true, func(r *http.Request) (any, error) { return struct{}{}, nil })
	}
	h("GET "+p+"metrics/get-history", false, func(r *http.Request) (any, error) {
		q := r.URL.Query()
		if err := s.mlSees(r.Context(), q.Get("run_id")); err != nil {
			return nil, err
		}
		rows, _ := s.db.Query(r.Context(), `SELECT k.name, m.value, (extract(epoch FROM m.ts) * 1000)::bigint, m.step
			FROM metrics m JOIN metric_keys k ON k.id = m.key_id WHERE m.run_id = $1::uuid AND k.name = $2 ORDER BY m.step`, q.Get("run_id"), q.Get("metric_key"))
		ms, err := pgx.CollectRows(rows, pgx.RowToStructByPos[mlMetric])
		return map[string]any{"metrics": ms}, err
	})

	// Logged models (MLflow 3) live as an artifact record of their run: kind mlflow-model, named models/<model id>.
	h("POST "+p+"logged-models", true, s.mlCreateModel)
	h("GET "+p+"logged-models/{model}", false, func(r *http.Request) (any, error) { return s.mlModel(r.Context(), r.PathValue("model"), nil) })
	h("PATCH "+p+"logged-models/{model}", true, func(r *http.Request) (any, error) {
		var in struct{ Status string }
		mlDecode(r, &in)
		return s.mlModel(r.Context(), r.PathValue("model"), map[string]any{"status": in.Status})
	})
	h("PATCH "+p+"logged-models/{model}/tags", true, func(r *http.Request) (any, error) {
		var in struct{ Tags []kv }
		mlDecode(r, &in)
		return s.mlModel(r.Context(), r.PathValue("model"), map[string]any{"tags": in.Tags})
	})
	h("POST "+p+"logged-models/{model}/params", true, func(r *http.Request) (any, error) {
		var in struct{ Params []kv }
		mlDecode(r, &in)
		return s.mlModel(r.Context(), r.PathValue("model"), map[string]any{"params": in.Params})
	})
	h("POST "+p+"logged-models/search", false, func(r *http.Request) (any, error) { return map[string]any{"models": []any{}}, nil })

	const a = "/api/2.0/mlflow-artifacts/artifacts"
	h("PUT "+a+"/{path...}", true, s.mlPutArtifact)
	mux.HandleFunc("GET "+a+"/{path...}", func(w http.ResponseWriter, r *http.Request) {
		runID, name, ok := s.mlArtifactPath(r.Context(), r.PathValue("path"))
		var key *string
		if !ok || s.db.QueryRow(r.Context(), `SELECT path FROM artifacts WHERE run_id = $1::uuid AND name = $2 AND kind = 'mlflow'`, runID, name).Scan(&key) != nil || key == nil {
			mlError(w, notFound("artifact"))
			return
		}
		inert(w)
		s.store.serve(w, r, *key)
	})
	h("GET "+a, false, s.mlListArtifacts)
	h("DELETE "+a+"/{path...}", true, func(r *http.Request) (any, error) {
		runID, name, ok := s.mlArtifactPath(r.Context(), r.PathValue("path"))
		if ok {
			s.mlRemoveArtifact(r.Context(), runID, name)
		}
		return struct{}{}, nil
	})

	// Anything else answers in MLflow's error format and is logged, so a missing endpoint shows up in the server log.
	mux.HandleFunc("/api/2.0/", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("mlflow: no endpoint for %s %s", r.Method, r.URL.Path)
		mlError(w, mlErr{404, "ENDPOINT_NOT_FOUND", "runyard does not implement " + r.Method + " " + r.URL.Path})
	})
}

// ml wraps an MLflow handler: writes need the API key as a Bearer token (MLFLOW_TRACKING_TOKEN); errors use MLflow's shape.
func (s *srv) ml(write bool, f func(*http.Request) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if write {
			key, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if key == "" || !validKey(r.Context(), s.db, key) {
				mlError(w, mlErr{401, "UNAUTHENTICATED", "set MLFLOW_TRACKING_TOKEN to a runyard API key"})
				return
			}
		}
		v, err := f(r)
		if err != nil {
			mlError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, v)
	}
}

func mlError(w http.ResponseWriter, err error) {
	var e mlErr
	if !errors.As(err, &e) {
		switch code := errStatus(err); code {
		case http.StatusNotFound:
			e = mlErr{code, "RESOURCE_DOES_NOT_EXIST", err.Error()}
		case http.StatusBadRequest:
			e = mlErr{code, "INVALID_PARAMETER_VALUE", err.Error()}
		default:
			log.Print("mlflow: ", err)
			e = mlErr{code, "INTERNAL_ERROR", err.Error()}
		}
	}
	writeJSON(w, e.status, map[string]string{"error_code": e.code, "message": e.text})
}

// Python's json writes NaN and Infinity bare; num reads them as strings.
var bareNaN = regexp.MustCompile(`([:\[,]\s*)(-?Infinity|NaN)\b`)

func mlDecode(r *http.Request, v any) error {
	b, err := io.ReadAll(io.LimitReader(r.Body, 64<<20))
	if err != nil {
		return err
	}
	if len(b) == 0 {
		return nil
	}
	if err := json.Unmarshal(bareNaN.ReplaceAll(b, []byte(`$1"$2"`)), v); err != nil {
		return mlErr{400, "MALFORMED_REQUEST", err.Error()}
	}
	return nil
}

type mlExp struct {
	ID               string `json:"experiment_id"`
	Name             string `json:"name"`
	ArtifactLocation string `json:"artifact_location"`
	LifecycleStage   string `json:"lifecycle_stage"`
	CreationTime     int64  `json:"creation_time"`
	LastUpdateTime   int64  `json:"last_update_time"`
}

func (s *srv) mlExperiment(ctx context.Context, where string, arg any) (mlExp, error) {
	var id int64
	var e mlExp
	var created time.Time
	err := s.db.QueryRow(ctx, `SELECT id, name, created_at FROM projects p WHERE (`+where+`) AND ($2::bigint[] IS NULL OR p.team_id IS NULL OR p.team_id = ANY($2))`,
		arg, vis(ctx)).Scan(&id, &e.Name, &created)
	if errors.Is(err, pgx.ErrNoRows) {
		return e, notFound("experiment")
	}
	e.ID = strconv.FormatInt(id, 10)
	e.ArtifactLocation, e.LifecycleStage = "mlflow-artifacts:/"+e.ID, "active"
	e.CreationTime, e.LastUpdateTime = created.UnixMilli(), created.UnixMilli()
	return e, err
}

// mlProject turns an experiment id into a project id. MLflow's default experiment "0" is the project "default".
func (s *srv) mlProject(ctx context.Context, expID string) (int64, error) {
	if expID == "0" || expID == "" {
		return s.project(ctx, "default")
	}
	id, err := strconv.ParseInt(expID, 10, 64)
	if err != nil {
		return 0, notFound("experiment " + expID)
	}
	if err := s.seesProject(ctx, id); err != nil {
		return 0, notFound("experiment " + expID)
	}
	return id, nil
}

func (s *srv) mlCreateRun(r *http.Request) (any, error) {
	var in struct {
		ExperimentID string `json:"experiment_id"`
		RunName      string `json:"run_name"`
		StartTime    int64  `json:"start_time"`
		Tags         []kv   `json:"tags"`
	}
	if err := mlDecode(r, &in); err != nil {
		return nil, err
	}
	ctx := r.Context()
	project, err := s.mlProject(ctx, in.ExperimentID)
	if err != nil {
		return nil, err
	}
	id := newUUID()
	name := or2(in.RunName, id[:8])
	started := time.Now()
	if in.StartTime > 0 {
		started = time.UnixMilli(in.StartTime)
	}
	// No heartbeats come from MLflow clients: writes count as signs of life, and only an hour of silence is a crash.
	_, err = s.db.Exec(ctx, `INSERT INTO runs (id, project_id, name, started_at, crash_after) VALUES ($1::uuid, $2, $3, $4, interval '1 hour')`,
		id, project, name, started)
	if err != nil {
		return nil, err
	}
	if err := s.mlLog(ctx, id, nil, nil, in.Tags); err != nil {
		return nil, err
	}
	s.hub.publish(event{Type: "run", Run: id})
	run, err := s.mlRun(ctx, id)
	return map[string]any{"run": run}, err
}

// mlLog writes metrics, params (into the config) and tags, and counts as a heartbeat.
func (s *srv) mlLog(ctx context.Context, runID string, metrics []mlMetric, params, tags []kv) error {
	if err := s.mlSees(ctx, runID); err != nil {
		return err
	}
	if _, err := s.beat(ctx, runID); err != nil {
		return err
	}
	if len(metrics) > 0 {
		rows := make([]metricRow, len(metrics))
		for i, m := range metrics {
			ts := time.UnixMilli(m.Timestamp)
			rows[i] = metricRow{Step: m.Step, TS: &ts, Values: map[string]num{m.Key: m.Value}}
		}
		if _, err := s.writeMetrics(ctx, runID, rows); err != nil {
			return err
		}
	}
	var p runPatch
	if len(params) > 0 {
		cfg := map[string]string{}
		for _, kv := range params {
			cfg[kv.Key] = kv.Value
		}
		p.Config, _ = json.Marshal(cfg)
	}
	for _, t := range tags {
		switch {
		case t.Key == "mlflow.runName":
			p.Name = &t.Value
		case t.Key == "mlflow.note.content":
			p.Notes = &t.Value
		case t.Key == "mlflow.source.git.commit":
			if _, err := s.db.Exec(ctx, `UPDATE runs SET git = coalesce(git, '{}') || jsonb_build_object('commit', $2::text) WHERE id = $1::uuid`, runID, t.Value); err != nil {
				return err
			}
		case strings.HasPrefix(t.Key, "mlflow."): // the client's bookkeeping (user, source, autologging...)
		default:
			tag := t.Key
			if t.Value != "" {
				tag += "=" + t.Value
			}
			if _, err := s.db.Exec(ctx, `UPDATE runs SET tags = ARRAY(SELECT x FROM unnest(tags) x WHERE split_part(x, '=', 1) <> $2) || ARRAY[$3]
				WHERE id = $1::uuid`, runID, t.Key, tag); err != nil {
				return err
			}
		}
	}
	if p.Config != nil || p.Name != nil || p.Notes != nil || len(tags) > 0 {
		_, err := s.patch(ctx, runID, p)
		return err
	}
	return nil
}

func mlStatus(status string) string {
	return map[string]string{"running": "RUNNING", "finished": "FINISHED", "failed": "FAILED", "crashed": "FAILED"}[status]
}

// mlRun is a run in MLflow's shape: info, and data with the latest metrics, the config as params, and the tags.
func (s *srv) mlRun(ctx context.Context, id string) (map[string]any, error) {
	if err := s.mlSees(ctx, id); err != nil {
		return nil, err
	}
	run, err := getRun(ctx, s.db, id)
	if errors.Is(err, pgx.ErrNoRows) || errStatus(err) == http.StatusBadRequest {
		return nil, notFound("run " + id)
	}
	if err != nil {
		return nil, err
	}
	var project int64
	s.db.QueryRow(ctx, `SELECT project_id FROM runs WHERE id = $1::uuid`, id).Scan(&project)
	exp := strconv.FormatInt(project, 10)
	info := map[string]any{"run_id": run.ID, "run_uuid": run.ID, "run_name": run.Name, "experiment_id": exp, "user_id": "",
		"status": mlStatus(run.Status), "start_time": run.StartedAt.UnixMilli(), "lifecycle_stage": "active",
		"artifact_uri": "mlflow-artifacts:/" + exp + "/" + run.ID + "/artifacts"}
	if run.EndedAt != nil {
		info["end_time"] = run.EndedAt.UnixMilli()
	}
	metrics := []mlMetric{}
	for k, m := range run.Metrics {
		metrics = append(metrics, mlMetric{Key: k, Value: m.Last, Step: m.Step, Timestamp: run.LastHeartbeatAt.UnixMilli()})
	}
	var config map[string]any
	json.Unmarshal(run.Config, &config)
	params := []kv{}
	for k, v := range config {
		sv, ok := v.(string)
		if !ok {
			b, _ := json.Marshal(v)
			sv = string(b)
		}
		params = append(params, kv{k, sv})
	}
	sort.Slice(params, func(i, j int) bool { return params[i].Key < params[j].Key })
	tags := []kv{{"mlflow.runName", run.Name}}
	for _, t := range run.Tags {
		k, v, _ := strings.Cut(t, "=")
		tags = append(tags, kv{k, v})
	}
	if run.Notes != "" {
		tags = append(tags, kv{"mlflow.note.content", run.Notes})
	}
	return map[string]any{"info": info, "data": map[string]any{"metrics": metrics, "params": params, "tags": tags}}, nil
}

// mlSees is notFound unless runID is a run the request may see.
func (s *srv) mlSees(ctx context.Context, runID string) error {
	if !uuidRe.MatchString(runID) || !s.canSee(ctx, runID) {
		return notFound("run " + runID)
	}
	return nil
}

// beat records a sign of life; a run marked crashed comes back to running. Returns the status.
func (s *srv) beat(ctx context.Context, id string) (string, error) {
	var status string
	err := s.db.QueryRow(ctx, `UPDATE runs SET last_heartbeat_at = now(),
			ended_at = CASE WHEN status = 'crashed' THEN NULL ELSE ended_at END,
			status = CASE WHEN status = 'crashed' THEN 'running' ELSE status END
		WHERE id = $1::uuid RETURNING status`, id).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) || errStatus(err) == http.StatusBadRequest {
		return "", notFound("run " + id)
	}
	return status, err
}

// Model ids carry their run: m-<run id without dashes>-<random>, so artifacts uploaded under models/<id>/ find their run.
var modelID = regexp.MustCompile(`^m-([0-9a-f]{32})-[0-9a-f]{8}$`)

func modelRun(id string) (string, bool) {
	m := modelID.FindStringSubmatch(id)
	if m == nil {
		return "", false
	}
	h := m[1]
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], true
}

func (s *srv) mlCreateModel(r *http.Request) (any, error) {
	var in struct {
		ExperimentID string `json:"experiment_id"`
		Name         string `json:"name"`
		ModelType    string `json:"model_type"`
		SourceRunID  string `json:"source_run_id"`
		Params       []kv   `json:"params"`
		Tags         []kv   `json:"tags"`
	}
	if err := mlDecode(r, &in); err != nil {
		return nil, err
	}
	if !uuidRe.MatchString(in.SourceRunID) {
		return nil, mlErr{400, "INVALID_PARAMETER_VALUE", "runyard keeps a model with its run: log it inside a run"}
	}
	if err := s.mlSees(r.Context(), in.SourceRunID); err != nil {
		return nil, err
	}
	id := "m-" + strings.ReplaceAll(in.SourceRunID, "-", "") + "-" + newUUID()[:8]
	meta, _ := json.Marshal(map[string]any{"model_id": id, "experiment_id": in.ExperimentID, "name": in.Name, "model_type": in.ModelType,
		"status": "LOGGED_MODEL_PENDING", "params": in.Params, "tags": in.Tags})
	_, err := s.addArtifact(r.Context(), in.SourceRunID, Artifact{ID: newUUID(), Name: "models/" + id, Kind: "mlflow-model", Meta: meta}, nil, 0, "")
	if err != nil {
		return nil, err
	}
	return s.mlModel(r.Context(), id, nil)
}

// mlModel returns a logged model, after merging change (status, tags, params) into it when given.
func (s *srv) mlModel(ctx context.Context, id string, change map[string]any) (any, error) {
	runID, ok := modelRun(id)
	if !ok || s.mlSees(ctx, runID) != nil {
		return nil, notFound("model " + id)
	}
	var meta map[string]any
	var created time.Time
	err := s.db.QueryRow(ctx, `SELECT meta, created_at FROM artifacts WHERE run_id = $1::uuid AND name = $2 AND kind = 'mlflow-model'`,
		runID, "models/"+id).Scan(&meta, &created)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFound("model " + id)
	}
	if err != nil {
		return nil, err
	}
	if change != nil {
		for k, v := range change {
			if l, ok := v.([]kv); ok { // tags and params add up
				old, _ := meta[k].([]any)
				for _, x := range l {
					old = append(old, x)
				}
				v = old
			}
			if v != "" {
				meta[k] = v
			}
		}
		b, _ := json.Marshal(meta)
		if _, err := s.db.Exec(ctx, `UPDATE artifacts SET meta = $3 WHERE run_id = $1::uuid AND name = $2 AND kind = 'mlflow-model'`, runID, "models/"+id, b); err != nil {
			return nil, err
		}
	}
	exp, _ := meta["experiment_id"].(string)
	info := map[string]any{"model_id": id, "experiment_id": exp, "name": meta["name"], "model_type": meta["model_type"],
		"source_run_id": runID, "status": meta["status"], "creation_timestamp_ms": created.UnixMilli(),
		"last_updated_timestamp_ms": time.Now().UnixMilli(), "artifact_uri": fmt.Sprintf("mlflow-artifacts:/%s/models/%s/artifacts", exp, id),
		"tags": meta["tags"]}
	return map[string]any{"model": map[string]any{"info": info, "data": map[string]any{"params": meta["params"]}}}, nil
}

// mlArtifactPath maps an artifact path to its run and the artifact's name:
// <exp>/<run id>/artifacts/<rel> is rel of that run, <exp>/models/<model id>/artifacts/<rel> is models/<model id>/rel.
func (s *srv) mlArtifactPath(ctx context.Context, p string) (runID, name string, ok bool) {
	runID, name, ok = mlArtifactName(p)
	return runID, name, ok && s.mlSees(ctx, runID) == nil
}

func mlArtifactName(p string) (runID, name string, ok bool) {
	parts := strings.SplitN(strings.Trim(p, "/"), "/", 5)
	switch {
	case len(parts) >= 3 && parts[1] != "models" && parts[2] == "artifacts" && uuidRe.MatchString(parts[1]):
		return parts[1], strings.Join(parts[3:], "/"), true
	case len(parts) >= 4 && parts[1] == "models" && parts[3] == "artifacts":
		runID, ok = modelRun(parts[2])
		return runID, strings.Join(append([]string{"models", parts[2]}, parts[4:]...), "/"), ok
	}
	return "", "", false
}

func (s *srv) mlRemoveArtifact(ctx context.Context, runID, name string) {
	var key *string
	err := s.db.QueryRow(ctx, `DELETE FROM artifacts WHERE run_id = $1::uuid AND name = $2 AND kind = 'mlflow' RETURNING path`, runID, name).Scan(&key)
	if err == nil && key != nil {
		s.store.remove(ctx, *key)
	}
}

func (s *srv) mlPutArtifact(r *http.Request) (any, error) {
	runID, name, ok := s.mlArtifactPath(r.Context(), r.PathValue("path"))
	if !ok || name == "" || strings.HasSuffix(name, "/") {
		return nil, mlErr{400, "INVALID_PARAMETER_VALUE", "not an artifact path of a run: " + r.PathValue("path")}
	}
	s.mlRemoveArtifact(r.Context(), runID, name) // MLflow overwrites by path
	_, err := s.addArtifact(r.Context(), runID, Artifact{ID: newUUID(), Name: name, Kind: "mlflow"}, r.Body, r.ContentLength, name)
	return struct{}{}, err
}

func (s *srv) mlListArtifacts(r *http.Request) (any, error) {
	runID, dir, ok := s.mlArtifactPath(r.Context(), r.URL.Query().Get("path"))
	if !ok {
		return map[string]any{"files": []any{}}, nil
	}
	prefix := strings.Trim(dir, "/")
	if prefix != "" {
		prefix += "/"
	}
	rows, _ := s.db.Query(r.Context(), `SELECT name, coalesce(size, 0) FROM artifacts WHERE run_id = $1::uuid AND kind = 'mlflow' AND starts_with(name, $2) ORDER BY name`,
		runID, prefix)
	type file struct {
		Path  string `json:"path"`
		IsDir bool   `json:"is_dir"`
		Size  int64  `json:"file_size,omitempty"`
	}
	var name string
	var size int64
	files := []file{}
	seen := map[string]bool{}
	_, err := pgx.ForEachRow(rows, []any{&name, &size}, func() error {
		rest := strings.TrimPrefix(name, prefix)
		if first, _, isDir := strings.Cut(rest, "/"); isDir {
			if !seen[first] {
				seen[first] = true
				files = append(files, file{Path: first, IsDir: true})
			}
		} else {
			files = append(files, file{Path: rest, Size: size})
		}
		return nil
	})
	return map[string]any{"files": files}, err
}
