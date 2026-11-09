package main

import (
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"path/filepath"
	"regexp"
	"slices"
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
	auth  bool // RUNYARD_AUTH: users log in, teams see their own projects
}

func (s *srv) routes() http.Handler {
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
		f := runFilter{Project: q.Get("project"), Status: q.Get("status"), Tag: q.Get("tag"), Q: q.Get("q"), Ended: q.Get("sort") == "ended"}
		if t, err := time.Parse(time.RFC3339Nano, q.Get("before")); err == nil {
			f.Before = &t
		}
		runs, err := listRuns(r.Context(), s.db, f, min(intParam(r, "limit", 200), 1000))
		if err == nil && q.Has("metrics") { // each run's metric summary too, as GET /runs/{id} has it
			err = addMetrics(r.Context(), s.db, runs)
		}
		return runs, err
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
		return getMetrics(r.Context(), s.db, r.PathValue("id"), keys, min(max(1, intParam(r, "points", 1000)), 10000))
	}))
	mux.HandleFunc("GET /api/v1/runs/{id}/logs", s.read(func(r *http.Request) (any, error) {
		tail := r.URL.Query().Has("tail")
		return getLogs(r.Context(), s.db, r.PathValue("id"), int64(intParam(r, "after", 0)), int64(intParam(r, "before", 0)), intParam(r, "limit", 1000), tail)
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
		return listAlerts(r.Context(), s.db, q.Get("run"), q.Get("project"), q.Has("active"), intParam(r, "limit", 100))
	}))
	// What the web UI changes in place: a run's notes and tags, and alerts put away.
	mux.HandleFunc("PATCH /api/v1/runs/{id}/meta", s.curate(func(r *http.Request) (any, error) {
		var in struct {
			Notes *string   `json:"notes"`
			Tags  *[]string `json:"tags"`
		}
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		if in.Tags != nil {
			tags := []string{}
			for _, t := range *in.Tags {
				if t = strings.TrimSpace(t); t != "" && !slices.Contains(tags, t) {
					tags = append(tags, t)
				}
			}
			in.Tags = &tags
		}
		_, err := s.patch(r.Context(), r.PathValue("id"), runPatch{Notes: in.Notes, Tags: in.Tags})
		return struct{}{}, err
	}))
	mux.HandleFunc("POST /api/v1/alerts/dismiss", s.curate(func(r *http.Request) (any, error) {
		var in struct {
			IDs []int64 `json:"ids"` // empty: every alert the request may see
		}
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		tag, err := s.db.Exec(r.Context(), `UPDATE alerts a SET dismissed_at = now() FROM runs r JOIN projects p ON p.id = r.project_id
			WHERE r.id = a.run_id AND a.dismissed_at IS NULL AND (coalesce(cardinality($1::bigint[]), 0) = 0 OR a.id = ANY($1))
			AND ($2::bigint[] IS NULL OR p.team_id IS NULL OR p.team_id = ANY($2))`, in.IDs, vis(r.Context()))
		if err == nil {
			s.hub.publish(event{Type: "alert"})
		}
		return map[string]int64{"dismissed": tag.RowsAffected()}, err
	}))
	mux.HandleFunc("GET /api/v1/events", func(w http.ResponseWriter, r *http.Request) { s.hub.serve(w, r, s.runFilter(r.Context())) })
	mux.Handle("/mcp", s.mcpHandler())
	s.mlflowRoutes(mux)
	s.pushRoutes(mux)
	s.authRoutes(mux)
	s.adminRoutes(mux)
	s.hostRoutes(mux)
	s.agentRoutes(mux)

	mux.HandleFunc("GET /media/{key...}", func(w http.ResponseWriter, r *http.Request) {
		key := r.PathValue("key")
		if !blobKey.MatchString(key) {
			http.NotFound(w, r)
			return
		}
		cache := "public"
		if s.auth {
			cache = "private" // a shared cache in front must not hand a team's samples to anyone with the URL
		}
		w.Header().Set("Cache-Control", cache+", max-age=31536000, immutable") // media files never change
		inert(w)
		if width := thumbWidth(intParam(r, "w", 0)); width > 0 { // ?w=640: a smaller copy for grids and phones
			if small, ok := s.thumb(r.Context(), key, width); ok {
				key = small
			}
		}
		s.store.serve(w, r, key)
	})
	static, _ := fs.Sub(webdist, "webdist/dist")
	mux.Handle("/", http.FileServerFS(static))
	return gzipped(s.guard(mux))
}

type gzipWriter struct {
	http.ResponseWriter
	gz *gzip.Writer
}

func (w gzipWriter) Write(b []byte) (int, error) { return w.gz.Write(b) }

// gzipped compresses the JSON API and the web UI's files for clients that take it: a run page's chart data
// shrinks about fivefold, which matters most to a phone. Streams and files that are already compressed pass through.
func gzipped(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		api := strings.HasPrefix(p, "/api/v1/") && p != "/api/v1/events" && !strings.HasSuffix(p, "/file")
		if !(api || strings.HasPrefix(p, "/assets/")) || !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") || r.Header.Get("Range") != "" {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		gz, _ := gzip.NewWriterLevel(w, gzip.BestSpeed)
		defer gz.Close()
		next.ServeHTTP(gzipWriter{w, gz}, r)
	})
}

// inert makes an uploaded file harmless to open in the browser: it comes from this origin, so an .svg or .html
// "sample" would otherwise run its script with the viewer's session. Embedded images and videos are unaffected.
func inert(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox")
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
	case errors.Is(err, pgx.ErrNoRows), errors.Is(err, errHidden):
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
	if errors.As(err, new(badRequest)) {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	code := errStatus(err)
	if code == http.StatusInternalServerError {
		log.Print(err)
	}
	httpError(w, code, err.Error())
}

// write guards a handler with the API key; a handler returns the response body or an error.
func (s *srv) write(f func(*http.Request) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.keyOK(r) {
			httpError(w, http.StatusUnauthorized, "missing or invalid API key")
			return
		}
		limit := int64(64 << 20) // JSON: 5000 metric rows are about 1 MB
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
			limit = 4 << 30 // an uploaded checkpoint
		}
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		v, err := f(r)
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, v)
	}
}

func (s *srv) keyOK(r *http.Request) bool {
	key, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return key != "" && validKey(r.Context(), s.db, key, "write")
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
		Agent     *runAgent       `json:"agent"`
		Env       json.RawMessage `json:"env"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if in.Project == "" {
		return nil, badRequest{errors.New("project is required")}
	}
	if in.ID = strings.ToLower(in.ID); in.ID == "" {
		in.ID = newUUID()
	} else if !uuidRe.MatchString(in.ID) {
		return nil, badRequest{errors.New("id must be a UUID")}
	}
	if in.Name == "" {
		in.Name = in.ID[:8]
	}
	if in.Tags == nil {
		in.Tags = []string{}
	}
	project, err := s.project(r.Context(), in.Project)
	if err != nil {
		return nil, err
	}
	agent := s.recordAgent(r.Context(), in.Agent, project, in.Host)
	// ON CONFLICT DO NOTHING: a client retrying a create it already sent gets the same run back.
	_, err = s.db.Exec(r.Context(), `INSERT INTO runs (id, project_id, name, config, tags, notes, host, git, started_at, agent, env)
		VALUES ($1::uuid, $2, $3, coalesce($4::jsonb, '{}'), $5, $6, $7, $8::jsonb, coalesce($9, now()), $10, $11::jsonb) ON CONFLICT (id) DO NOTHING`,
		in.ID, project, in.Name, nullJSON(in.Config), in.Tags, in.Notes, in.Host, nullJSON(in.Git), in.StartedAt, agent, nullJSON(in.Env))
	if err != nil {
		return nil, err
	}
	s.hub.publish(event{Type: "run", Run: in.ID})
	if agent != nil {
		s.hub.publish(event{Type: "agent"})
	}
	return map[string]string{"id": in.ID}, nil
}

// project returns the id of the named project, made on first use (in the writer's first team, with auth on).
func (s *srv) project(ctx context.Context, name string) (int64, error) {
	var id int64
	err := s.db.QueryRow(ctx, `INSERT INTO projects (name, team_id) VALUES ($1, $2) ON CONFLICT (name) DO UPDATE SET name = excluded.name RETURNING id`,
		name, projectTeam(ctx)).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, s.seesProject(ctx, id)
}

func (s *srv) updateRun(r *http.Request) (any, error) {
	var p runPatch
	if err := decode(r, &p); err != nil {
		return nil, err
	}
	after, err := s.patch(r.Context(), r.PathValue("id"), p)
	if err != nil {
		return nil, err
	}
	return map[string]string{"status": after}, nil
}

// patch applies p, tells the live views, and notifies when the run just finished or failed.
func (s *srv) patch(ctx context.Context, id string, p runPatch) (string, error) {
	before, after, err := patchRun(ctx, s.db, id, p)
	if err != nil {
		return "", err
	}
	s.hub.publish(event{Type: "run", Run: id})
	if before != after && (after == "finished" || after == "failed") {
		go s.notify(id, after, nil)
	}
	return after, nil
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
	n, err := s.writeMetrics(r.Context(), r.PathValue("id"), rows)
	if err != nil {
		return nil, err
	}
	return map[string]int{"written": n}, nil
}

// writeMetrics stores rows (a retry overwrites, never duplicates), runs the checks, and returns how many values it wrote.
func (s *srv) writeMetrics(ctx context.Context, id string, rows []metricRow) (int, error) {
	var project int64
	if err := s.db.QueryRow(ctx, `SELECT project_id FROM runs WHERE id = $1::uuid`, id).Scan(&project); err != nil {
		return 0, err
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
		return 0, err
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
	// The summary and the 100-step rollup are updated in the same statement, from the rows just written; xmax = 0
	// marks a row that is new, so a retried batch does not count twice.
	// shortcut: a step logged again with another value leaves the old one in min and max; recompute from metrics
	// (as migration 011 does) if runs that rewrite history need exact extremes.
	_, err = s.db.Exec(ctx, `WITH ins AS (
			INSERT INTO metrics (run_id, key_id, step, ts, value)
			SELECT $1::uuid, k, st, t, v FROM unnest($2::bigint[], $3::bigint[], $4::timestamptz[], $5::float8[]) AS u(k, st, t, v)
			ON CONFLICT (run_id, key_id, step) DO UPDATE SET value = excluded.value, ts = excluded.ts
			RETURNING key_id, step, ts, value, (xmax = 0) AS fresh)
		, sums AS (INSERT INTO metric_summary AS s (run_id, key_id, count, first, step, last, min, max)
			SELECT $1::uuid, key_id, count(*) FILTER (WHERE fresh), min(step), max(step), (array_agg(value ORDER BY step DESC))[1], min(value), max(value)
			FROM ins GROUP BY key_id ORDER BY key_id
		ON CONFLICT (run_id, key_id) DO UPDATE SET count = s.count + excluded.count, first = least(s.first, excluded.first),
			last = CASE WHEN excluded.step >= s.step THEN excluded.last ELSE s.last END,
			step = greatest(s.step, excluded.step), min = least(s.min, excluded.min), max = greatest(s.max, excluded.max))
		INSERT INTO metric_rollup AS r (run_id, key_id, bucket, step, t, last, min, max)
			SELECT $1::uuid, key_id, step / 100, max(step), (max(ARRAY[step::float8, extract(epoch FROM ts)::float8]))[2],
				(max(ARRAY[step::float8, value]))[2], min(value), max(value)
			FROM ins GROUP BY key_id, step / 100 ORDER BY 2, 3
		ON CONFLICT (run_id, key_id, bucket) DO UPDATE SET
			last = CASE WHEN excluded.step >= r.step THEN excluded.last ELSE r.last END,
			t = CASE WHEN excluded.step >= r.step THEN excluded.t ELSE r.t END,
			step = greatest(r.step, excluded.step), min = least(r.min, excluded.min), max = greatest(r.max, excluded.max)`,
		id, keyCol, stepCol, tsCol, valCol)
	if err != nil {
		return 0, err
	}
	if progress {
		s.progressed(ctx, id)
	}
	s.checkMetrics(ctx, id, project, rows)
	s.hub.publish(event{Type: "metrics", Run: id})
	return len(keyCol), nil
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
	s.hub.publish(event{Type: "run", Run: id}) // open pages show the heartbeat's age, and a revived run as running
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
