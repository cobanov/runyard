package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type srv struct {
	db    *pgxpool.Pool
	store blobs  // media and artifact files
	url   string // public base URL for links in notifications, may be empty
	hub   *hub
}

func (s *srv) routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"ok":true}`)) })

	mux.HandleFunc("POST /api/v1/runs", s.write(s.createRun))
	mux.HandleFunc("PATCH /api/v1/runs/{id}", s.write(s.updateRun))
	mux.HandleFunc("POST /api/v1/runs/{id}/metrics", s.write(s.postMetrics))
	mux.HandleFunc("POST /api/v1/runs/{id}/logs", s.write(s.postLogs))
	mux.HandleFunc("POST /api/v1/runs/{id}/media", s.write(s.postMedia))
	mux.HandleFunc("POST /api/v1/runs/{id}/heartbeat", s.write(s.heartbeat))
	mux.HandleFunc("POST /api/v1/runs/{id}/alerts", s.write(s.postAlert))
	mux.HandleFunc("POST /api/v1/runs/{id}/artifacts", s.write(s.postArtifact))

	mux.HandleFunc("GET /api/v1/projects", s.read(func(r *http.Request) (any, error) { return listProjects(r.Context(), s.db) }))
	mux.HandleFunc("GET /api/v1/runs", s.read(func(r *http.Request) (any, error) {
		q := r.URL.Query()
		return listRuns(r.Context(), s.db, q.Get("project"), q.Get("status"), q.Get("tag"), intParam(r, "limit", 200))
	}))
	mux.HandleFunc("GET /api/v1/runs/{id}", s.read(func(r *http.Request) (any, error) {
		id, err := s.resolveRun(r.Context(), r.PathValue("id")) // also takes an id prefix or a run name, for the CLI
		if err != nil {
			return nil, err
		}
		return getRun(r.Context(), s.db, id)
	}))
	mux.HandleFunc("GET /api/v1/runs/{id}/metrics", s.read(func(r *http.Request) (any, error) {
		var keys []string
		if k := r.URL.Query().Get("keys"); k != "" {
			keys = strings.Split(k, ",")
		}
		return getMetrics(r.Context(), s.db, r.PathValue("id"), keys, max(1, intParam(r, "points", 1000)))
	}))
	mux.HandleFunc("GET /api/v1/runs/{id}/logs", s.read(func(r *http.Request) (any, error) {
		tail := r.URL.Query().Has("tail")
		return getLogs(r.Context(), s.db, r.PathValue("id"), int64(intParam(r, "after", 0)), intParam(r, "limit", 1000), tail)
	}))
	mux.HandleFunc("GET /api/v1/runs/{id}/media", s.read(func(r *http.Request) (any, error) {
		q := r.URL.Query()
		return listMedia(r.Context(), s.db, r.PathValue("id"), q.Get("key"), q.Get("kind"), int64(intParam(r, "step", -1)))
	}))
	mux.HandleFunc("GET /api/v1/runs/{id}/artifacts", s.read(func(r *http.Request) (any, error) {
		return listArtifacts(r.Context(), s.db, r.PathValue("id"))
	}))
	mux.HandleFunc("GET /api/v1/artifacts/{id}/file", s.serveArtifact)
	mux.HandleFunc("GET /api/v1/alerts", s.read(func(r *http.Request) (any, error) {
		q := r.URL.Query()
		return listAlerts(r.Context(), s.db, q.Get("run"), q.Get("project"), intParam(r, "limit", 100))
	}))
	mux.HandleFunc("GET /api/v1/events", s.hub.serve)
	mux.Handle("/mcp", s.mcpHandler())

	mux.HandleFunc("GET /media/{key...}", func(w http.ResponseWriter, r *http.Request) {
		key := r.PathValue("key")
		if !blobKey.MatchString(key) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable") // media files never change
		s.store.serve(w, r, key)
	})
	return mux
}

func intParam(r *http.Request, name string, def int) int {
	if n, err := strconv.Atoi(r.URL.Query().Get(name)); err == nil {
		return n
	}
	return def
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// errStatus maps a store error to an HTTP status: 404 for a missing run, 400 for bad input the database refused.
func errStatus(err error) int {
	var pe *pgconn.PgError
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return http.StatusNotFound
	case errors.As(err, &pe) && pe.Code == "23503": // foreign key: the run does not exist
		return http.StatusNotFound
	case errors.As(err, &pe) && (strings.HasPrefix(pe.Code, "22") || strings.HasPrefix(pe.Code, "23")):
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

func (s *srv) read(f func(*http.Request) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		v, err := f(r)
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, v)
	}
}

func (s *srv) fail(w http.ResponseWriter, err error) {
	code := errStatus(err)
	if code == http.StatusInternalServerError {
		log.Print(err)
	}
	httpError(w, code, err.Error())
}

// write guards a handler with the API key; a handler returns the response body or an error.
func (s *srv) write(f func(*http.Request) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if key == "" || !validKey(r.Context(), s.db, key) {
			httpError(w, http.StatusUnauthorized, "missing or invalid API key")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4<<30)
		v, err := f(r)
		var bad badRequest
		if errors.As(err, &bad) {
			httpError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, v)
	}
}

type badRequest struct{ error }

func decode(r *http.Request, v any) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return badRequest{err}
	}
	return nil
}

func (s *srv) createRun(r *http.Request) (any, error) {
	var in struct {
		ID        string          `json:"id"`
		Project   string          `json:"project"`
		Name      string          `json:"name"`
		Config    json.RawMessage `json:"config"`
		Tags      []string        `json:"tags"`
		Notes     string          `json:"notes"`
		Host      string          `json:"host"`
		Git       json.RawMessage `json:"git"`
		StartedAt *time.Time      `json:"started_at"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if in.Project == "" {
		return nil, badRequest{errors.New("project is required")}
	}
	if in.ID == "" {
		in.ID = newUUID()
	}
	if in.Name == "" {
		in.Name = in.ID[:8]
	}
	if in.Tags == nil {
		in.Tags = []string{}
	}
	// ON CONFLICT DO NOTHING: a client retrying a create it already sent gets the same run back.
	_, err := s.db.Exec(r.Context(), `WITH p AS (
			INSERT INTO projects (name) VALUES ($2) ON CONFLICT (name) DO UPDATE SET name = excluded.name RETURNING id)
		INSERT INTO runs (id, project_id, name, config, tags, notes, host, git, started_at)
		SELECT $1::uuid, p.id, $3, coalesce($4::jsonb, '{}'), $5, $6, $7, $8::jsonb, coalesce($9, now()) FROM p
		ON CONFLICT (id) DO NOTHING`,
		in.ID, in.Project, in.Name, nullJSON(in.Config), in.Tags, in.Notes, in.Host, nullJSON(in.Git), in.StartedAt)
	if err != nil {
		return nil, err
	}
	s.hub.publish(event{Type: "run", Run: in.ID})
	return map[string]string{"id": in.ID}, nil
}

func (s *srv) updateRun(r *http.Request) (any, error) {
	var p runPatch
	if err := decode(r, &p); err != nil {
		return nil, err
	}
	id := r.PathValue("id")
	before, after, err := patchRun(r.Context(), s.db, id, p)
	if err != nil {
		return nil, err
	}
	s.hub.publish(event{Type: "run", Run: id})
	if before != after && (after == "finished" || after == "failed") {
		go s.notify(id, after, nil)
	}
	return map[string]string{"status": after}, nil
}

type metricRow struct {
	Step   int64          `json:"step"`
	TS     *time.Time     `json:"ts"`
	Values map[string]num `json:"values"`
}

func (s *srv) postMetrics(r *http.Request) (any, error) {
	var rows []metricRow
	if err := decode(r, &rows); err != nil {
		return nil, err
	}
	id := r.PathValue("id")
	ctx := r.Context()
	var project int64
	if err := s.db.QueryRow(ctx, `SELECT project_id FROM runs WHERE id = $1::uuid`, id).Scan(&project); err != nil {
		return nil, err
	}
	var names []string
	seen := map[string]bool{}
	progress := false
	for _, row := range rows {
		for k := range row.Values {
			if !seen[k] {
				seen[k] = true
				names = append(names, k)
				progress = progress || !strings.HasPrefix(k, "sys/")
			}
		}
	}
	ids, err := s.keyIDs(ctx, project, names)
	if err != nil {
		return nil, err
	}
	// Last write wins inside a batch: one INSERT cannot touch the same (key, step) twice.
	type pk struct{ key, step int64 }
	at := map[pk]int{}
	var keyCol, stepCol []int64
	var tsCol []time.Time
	var valCol []float64
	now := time.Now()
	for _, row := range rows {
		ts := now
		if row.TS != nil {
			ts = *row.TS
		}
		for k, v := range row.Values {
			p := pk{ids[k], row.Step}
			if i, ok := at[p]; ok {
				tsCol[i], valCol[i] = ts, float64(v)
				continue
			}
			at[p] = len(keyCol)
			keyCol, stepCol, tsCol, valCol = append(keyCol, p.key), append(stepCol, p.step), append(tsCol, ts), append(valCol, float64(v))
		}
	}
	_, err = s.db.Exec(ctx, `INSERT INTO metrics (run_id, key_id, step, ts, value)
		SELECT $1::uuid, k, st, t, v FROM unnest($2::bigint[], $3::bigint[], $4::timestamptz[], $5::float8[]) AS u(k, st, t, v)
		ON CONFLICT (run_id, key_id, step) DO UPDATE SET value = excluded.value, ts = excluded.ts`,
		id, keyCol, stepCol, tsCol, valCol)
	if err != nil {
		return nil, err
	}
	if progress {
		s.progressed(ctx, id)
	}
	s.checkMetrics(ctx, id, project, rows)
	s.hub.publish(event{Type: "metrics", Run: id})
	return map[string]int{"written": len(keyCol)}, nil
}

// keyIDs returns the metric_keys id of every name, creating missing keys.
func (s *srv) keyIDs(ctx context.Context, project int64, names []string) (map[string]int64, error) {
	ids := map[string]int64{}
	// A concurrent insert of the same new key is invisible to this statement's snapshot, so look again once.
	for try := 0; try < 2 && len(ids) < len(names); try++ {
		rows, _ := s.db.Query(ctx, `WITH ins AS (
				INSERT INTO metric_keys (project_id, name) SELECT $1, unnest($2::text[]) ON CONFLICT DO NOTHING RETURNING id, name)
			SELECT id, name FROM ins UNION ALL SELECT id, name FROM metric_keys WHERE project_id = $1 AND name = ANY($2)`, project, names)
		var id int64
		var name string
		if _, err := pgx.ForEachRow(rows, []any{&id, &name}, func() error { ids[name] = id; return nil }); err != nil {
			return nil, err
		}
	}
	if len(ids) < len(names) {
		return nil, errors.New("could not create metric keys")
	}
	return ids, nil
}

func (s *srv) postLogs(r *http.Request) (any, error) {
	var lines []LogLine
	if err := decode(r, &lines); err != nil {
		return nil, err
	}
	var seq []int64
	var ts []time.Time
	var stream []int16
	var text []string
	for _, l := range lines {
		if l.TS.IsZero() {
			l.TS = time.Now()
		}
		if l.Stream == 0 {
			l.Stream = 1
		}
		seq, ts, stream, text = append(seq, l.Seq), append(ts, l.TS), append(stream, l.Stream), append(text, strings.ToValidUTF8(strings.ReplaceAll(l.Line, "\x00", ""), "�"))
	}
	id := r.PathValue("id")
	_, err := s.db.Exec(r.Context(), `INSERT INTO logs (run_id, seq, ts, stream, line)
		SELECT $1::uuid, * FROM unnest($2::bigint[], $3::timestamptz[], $4::smallint[], $5::text[]) ON CONFLICT DO NOTHING`,
		id, seq, ts, stream, text)
	if err != nil {
		return nil, err
	}
	s.progressed(r.Context(), id)
	s.hub.publish(event{Type: "logs", Run: id})
	return map[string]int{"written": len(lines)}, nil
}

var safeExt = regexp.MustCompile(`^\.[a-z0-9]{1,5}$`)
var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// blobKey is the shape of a media file's key: <run id>/<file id><ext>.
var blobKey = regexp.MustCompile(`^[0-9a-f-]{36}/[0-9a-f-]{36}(\.[a-z0-9]{1,5})?$`)

// postMedia takes multipart fields id (optional, makes retries idempotent), key, step, kind, meta (JSON) and file.
func (s *srv) postMedia(r *http.Request) (any, error) {
	runID := r.PathValue("id")
	if !uuidRe.MatchString(runID) {
		return nil, badRequest{errors.New("bad run id")}
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		return nil, badRequest{err}
	}
	defer r.MultipartForm.RemoveAll()
	id, key, kind := r.FormValue("id"), r.FormValue("key"), r.FormValue("kind")
	step, err := strconv.ParseInt(r.FormValue("step"), 10, 64)
	if err != nil || key == "" || (kind != "image" && kind != "video" && kind != "text") {
		return nil, badRequest{errors.New("key, numeric step and kind (image|video|text) are required")}
	}
	if id == "" {
		id = newUUID()
	} else if !uuidRe.MatchString(id) {
		return nil, badRequest{errors.New("bad media id")}
	}
	var prev string
	if s.db.QueryRow(r.Context(), `SELECT path FROM media WHERE id = $1::uuid`, id).Scan(&prev) == nil {
		return map[string]string{"id": id, "url": "/media/" + prev}, nil // a retry of an upload that already landed
	}
	meta := r.FormValue("meta")
	if meta == "" {
		meta = "{}"
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		return nil, badRequest{err}
	}
	defer file.Close()
	ext := strings.ToLower(filepath.Ext(hdr.Filename))
	if !safeExt.MatchString(ext) {
		ext = ""
	}
	rel := runID + "/" + id + ext
	if err := s.store.put(r.Context(), rel, file, hdr.Size); err != nil {
		return nil, err
	}
	_, err = s.db.Exec(r.Context(), `INSERT INTO media (id, run_id, key, step, kind, path, meta)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7::jsonb)`, id, runID, key, step, kind, rel, meta)
	if err != nil {
		s.store.remove(context.WithoutCancel(r.Context()), rel)
		return nil, err
	}
	s.hub.publish(event{Type: "media", Run: runID})
	return map[string]string{"id": id, "url": "/media/" + rel}, nil
}

func newUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6], b[8] = b[6]&0x0f|0x40, b[8]&0x3f|0x80 // version 4, RFC 4122 variant
	h := hex.EncodeToString(b)
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

func (s *srv) heartbeat(r *http.Request) (any, error) {
	id := r.PathValue("id")
	var status string
	// A heartbeat from a run marked crashed means it was only cut off; it is running again.
	err := s.db.QueryRow(r.Context(), `UPDATE runs SET last_heartbeat_at = now(),
			ended_at = CASE WHEN status = 'crashed' THEN NULL ELSE ended_at END,
			status = CASE WHEN status = 'crashed' THEN 'running' ELSE status END
		WHERE id = $1::uuid RETURNING status`, id).Scan(&status)
	if err != nil {
		return nil, err
	}
	return map[string]string{"status": status}, nil
}

func (s *srv) postAlert(r *http.Request) (any, error) {
	var a struct {
		Level string `json:"level"`
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := decode(r, &a); err != nil {
		return nil, err
	}
	if a.Level == "" {
		a.Level = "info"
	}
	aid, err := s.addAlert(r.Context(), r.PathValue("id"), nil, Alert{Level: a.Level, Title: a.Title, Body: a.Body})
	if err != nil {
		return nil, err
	}
	return map[string]int64{"id": aid}, nil
}
