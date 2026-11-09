package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"math"
	"path"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrations embed.FS

// migrate applies every migrations/*.sql file not yet recorded, in name order, each in its own transaction.
func migrate(ctx context.Context, db *pgxpool.Pool) error {
	if _, err := db.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	files, _ := fs.Glob(migrations, "migrations/*.sql")
	for _, f := range files {
		name := path.Base(f)
		sql, _ := migrations.ReadFile(f)
		err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
			tag, err := tx.Exec(ctx, `INSERT INTO schema_migrations (name) VALUES ($1) ON CONFLICT DO NOTHING`, name)
			if err != nil || tag.RowsAffected() == 0 {
				return err
			}
			_, err = tx.Exec(ctx, string(sql))
			return err
		})
		if err != nil {
			return errors.New(name + ": " + err.Error())
		}
	}
	return nil
}

// num is a float64 that travels as a JSON string when it is NaN or ±Inf, which JSON numbers cannot express.
type num float64

func (n num) MarshalJSON() ([]byte, error) {
	f := float64(n)
	switch {
	case math.IsNaN(f):
		return []byte(`"NaN"`), nil
	case math.IsInf(f, 1):
		return []byte(`"Infinity"`), nil
	case math.IsInf(f, -1):
		return []byte(`"-Infinity"`), nil
	}
	return json.Marshal(f)
}

func (n *num) UnmarshalJSON(b []byte) error {
	var f float64
	if err := json.Unmarshal(b, &f); err == nil {
		*n = num(f)
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.New("metric values must be numbers or NaN/Infinity strings")
	}
	f, err := strconv.ParseFloat(s, 64)
	*n = num(f)
	return err
}

type Run struct {
	ID              string                   `json:"id"`
	Project         string                   `json:"project"`
	Name            string                   `json:"name"`
	Status          string                   `json:"status"`
	Config          json.RawMessage          `json:"config"`
	Summary         json.RawMessage          `json:"summary"`
	Tags            []string                 `json:"tags"`
	Notes           string                   `json:"notes"`
	Host            string                   `json:"host"`
	Git             json.RawMessage          `json:"git"`
	StartedAt       time.Time                `json:"started_at"`
	EndedAt         *time.Time               `json:"ended_at"`
	LastHeartbeatAt time.Time                `json:"last_heartbeat_at"`
	Agent           *string                  `json:"agent"` // the agent session that started it, "<tool>:<session>"
	Env             json.RawMessage          `json:"env,omitempty"` // command line, Python, hardware, diff; GET /runs/{id} only
	Metrics         map[string]metricSummary `json:"metrics,omitempty"`
}

type metricSummary struct {
	Count int64 `json:"count"`
	Step  int64 `json:"step"` // last step
	Last  num   `json:"last"`
	Min   num   `json:"min"`
	Max   num   `json:"max"`
}

const runSelect = `SELECT r.id::text, p.name, r.name, r.status, r.config, r.summary, r.tags, r.notes, r.host, r.git,
	r.started_at, r.ended_at, r.last_heartbeat_at, r.agent FROM runs r JOIN projects p ON p.id = r.project_id `

func scanRun(row pgx.Row) (Run, error) {
	var r Run
	err := row.Scan(&r.ID, &r.Project, &r.Name, &r.Status, &r.Config, &r.Summary, &r.Tags, &r.Notes, &r.Host, &r.Git,
		&r.StartedAt, &r.EndedAt, &r.LastHeartbeatAt, &r.Agent)
	return r, err
}

type Project struct {
	Name      string     `json:"name"`
	CreatedAt time.Time  `json:"created_at"`
	Runs      int64      `json:"runs"`
	Running   int64      `json:"running"`
	LastRunAt *time.Time `json:"last_run_at"`
	Metric    string     `json:"metric"` // the number its runs are judged by ('': a loss), and min or max
	Goal      string     `json:"goal"`
}

func listProjects(ctx context.Context, db *pgxpool.Pool) ([]Project, error) {
	rows, _ := db.Query(ctx, `SELECT p.name, p.created_at, count(r.id), count(r.id) FILTER (WHERE r.status = 'running'), max(r.started_at), p.metric, p.goal
		FROM projects p LEFT JOIN runs r ON r.project_id = p.id WHERE ($1::bigint[] IS NULL OR p.team_id IS NULL OR p.team_id = ANY($1))
		GROUP BY p.id ORDER BY max(r.started_at) DESC NULLS LAST, p.name`, vis(ctx))
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (p Project, err error) {
		err = row.Scan(&p.Name, &p.CreatedAt, &p.Runs, &p.Running, &p.LastRunAt, &p.Metric, &p.Goal)
		return
	})
}

// listRuns filters by project, status and tag; empty filters match everything.
func listRuns(ctx context.Context, db *pgxpool.Pool, project, status, tag string, limit int) ([]Run, error) {
	rows, _ := db.Query(ctx, runSelect+`WHERE ($1 = '' OR p.name = $1) AND ($2 = '' OR r.status = $2) AND ($3 = '' OR $3 = ANY(r.tags))
		AND ($5::bigint[] IS NULL OR p.team_id IS NULL OR p.team_id = ANY($5)) ORDER BY r.started_at DESC LIMIT $4`, project, status, tag, limit, vis(ctx))
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Run, error) { return scanRun(row) })
}

// getRun returns the run with a per-key summary of its metrics, or pgx.ErrNoRows.
func getRun(ctx context.Context, db *pgxpool.Pool, id string) (Run, error) {
	r, err := scanRun(db.QueryRow(ctx, runSelect+`WHERE r.id = $1::uuid`, id))
	if err != nil {
		return r, err
	}
	runs := []Run{r}
	if err = db.QueryRow(ctx, `SELECT env FROM runs WHERE id = $1::uuid`, id).Scan(&runs[0].Env); err != nil { // not in lists: it can hold a diff
		return r, err
	}
	err = addMetrics(ctx, db, runs)
	return runs[0], err
}

// addMetrics fills in each run's per-key metric summary, for all the runs in one query.
// shortcut: scans every metric row of the runs; fine to ~10M rows, keep a per-key summary table past that.
func addMetrics(ctx context.Context, db *pgxpool.Pool, runs []Run) error {
	ids := make([]string, len(runs))
	byID := make(map[string]*Run, len(runs))
	for i := range runs {
		ids[i] = runs[i].ID
		runs[i].Metrics = map[string]metricSummary{}
		byID[runs[i].ID] = &runs[i]
	}
	rows, _ := db.Query(ctx, `SELECT m.run_id::text, k.name, count(*), max(m.step), (array_agg(m.value ORDER BY m.step DESC))[1], min(m.value), max(m.value)
		FROM metrics m JOIN metric_keys k ON k.id = m.key_id WHERE m.run_id = ANY($1::uuid[]) GROUP BY 1, 2`, ids)
	var id, name string
	var s metricSummary
	var last, lo, hi float64
	_, err := pgx.ForEachRow(rows, []any{&id, &name, &s.Count, &s.Step, &last, &lo, &hi}, func() error {
		s.Last, s.Min, s.Max = num(last), num(lo), num(hi)
		byID[id].Metrics[name] = s
		return nil
	})
	return err
}

type series struct {
	Step []int64 `json:"step"` // last step in each bucket
	Min  []num   `json:"min"`
	Max  []num   `json:"max"`
	Last []num   `json:"last"`
}

// getMetrics returns each key's history cut into at most points buckets of equal step width.
// A key with no more than points rows comes back raw.
func getMetrics(ctx context.Context, db *pgxpool.Pool, id string, keys []string, points int) (map[string]*series, error) {
	rows, _ := db.Query(ctx, `WITH m AS (
			SELECT k.name, m.step, m.value FROM metrics m JOIN metric_keys k ON k.id = m.key_id
			WHERE m.run_id = $1::uuid AND (coalesce(cardinality($2::text[]), 0) = 0 OR k.name = ANY($2))
		), r AS (SELECT name, min(step) lo, max(step) hi, count(*) n FROM m GROUP BY name)
		SELECT m.name, max(m.step), min(m.value), max(m.value), (array_agg(m.value ORDER BY m.step DESC))[1]
		FROM m JOIN r USING (name)
		GROUP BY m.name, CASE WHEN r.n <= $3 THEN m.step ELSE (m.step - r.lo) * $3 / (r.hi - r.lo + 1) END
		ORDER BY 1, 2`, id, keys, points)
	out := map[string]*series{}
	var name string
	var step int64
	var lo, hi, last float64
	_, err := pgx.ForEachRow(rows, []any{&name, &step, &lo, &hi, &last}, func() error {
		s := out[name]
		if s == nil {
			s = &series{}
			out[name] = s
		}
		s.Step = append(s.Step, step)
		s.Min, s.Max, s.Last = append(s.Min, num(lo)), append(s.Max, num(hi)), append(s.Last, num(last))
		return nil
	})
	return out, err
}

type LogLine struct {
	Seq    int64     `json:"seq"`
	TS     time.Time `json:"ts"`
	Stream int16     `json:"stream"`
	Line   string    `json:"line"`
}

// getLogs returns up to limit lines after seq after, or the last limit lines when tail is set.
func getLogs(ctx context.Context, db *pgxpool.Pool, id string, after int64, limit int, tail bool) ([]LogLine, error) {
	q := `SELECT seq, ts, stream, line FROM logs WHERE run_id = $1::uuid AND seq > $2 ORDER BY seq LIMIT $3`
	if tail {
		q = `SELECT * FROM (SELECT seq, ts, stream, line FROM logs WHERE run_id = $1::uuid AND seq > $2 ORDER BY seq DESC LIMIT $3) t ORDER BY seq`
	}
	rows, _ := db.Query(ctx, q, id, after, limit)
	return pgx.CollectRows(rows, pgx.RowToStructByPos[LogLine])
}

type Media struct {
	ID        string          `json:"id"`
	Key       string          `json:"key"`
	Step      int64           `json:"step"`
	Kind      string          `json:"kind"`
	Path      string          `json:"-"`
	URL       string          `json:"url"`
	Meta      json.RawMessage `json:"meta"`
	CreatedAt time.Time       `json:"created_at"`
}

// listMedia returns a run's media ordered by key and step; key and kind filter when set, step < 0 means any.
func listMedia(ctx context.Context, db *pgxpool.Pool, id, key, kind string, step int64) ([]Media, error) {
	rows, _ := db.Query(ctx, `SELECT id::text, key, step, kind, path, meta, created_at FROM media
		WHERE run_id = $1::uuid AND ($2 = '' OR key = $2) AND ($3 = '' OR kind = $3) AND ($4 < 0 OR step = $4)
		ORDER BY key, step, created_at`, id, key, kind, step)
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (m Media, err error) {
		err = row.Scan(&m.ID, &m.Key, &m.Step, &m.Kind, &m.Path, &m.Meta, &m.CreatedAt)
		m.URL = "/media/" + m.Path
		return
	})
}

type Alert struct {
	ID      int64     `json:"id"`
	Run     string    `json:"run"`
	RunName string    `json:"run_name"`
	Project string    `json:"project"`
	TS      time.Time `json:"ts"`
	Level   string    `json:"level"`
	Title   string    `json:"title"`
	Body    string    `json:"body"`
	// Dismissed is when someone put the alert away; it is left out of the Overview.
	Dismissed *time.Time `json:"dismissed_at"`
}

// listAlerts lists the newest alerts, of one run or project when given; active leaves out the dismissed ones.
func listAlerts(ctx context.Context, db *pgxpool.Pool, run, project string, active bool, limit int) ([]Alert, error) {
	rows, _ := db.Query(ctx, `SELECT a.id, r.id::text, r.name, p.name, a.ts, a.level, a.title, a.body, a.dismissed_at
		FROM alerts a JOIN runs r ON r.id = a.run_id JOIN projects p ON p.id = r.project_id
		WHERE ($1 = '' OR r.id::text = $1) AND ($2 = '' OR p.name = $2) AND ($4::bigint[] IS NULL OR p.team_id IS NULL OR p.team_id = ANY($4))
		AND NOT ($5 AND a.dismissed_at IS NOT NULL)
		ORDER BY a.ts DESC LIMIT $3`, run, project, limit, vis(ctx), active)
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Alert])
}

// runPatch holds the fields a PATCH may change; nil means unchanged. Config and summary are merged, not replaced.
type runPatch struct {
	Name    *string         `json:"name"`
	Status  *string         `json:"status"`
	Config  json.RawMessage `json:"config"`
	Summary json.RawMessage `json:"summary"`
	Tags    *[]string       `json:"tags"`
	Notes   *string         `json:"notes"`
	EndedAt *time.Time      `json:"ended_at"` // when it really ended, for a run whose end arrives late (runyard sync)
}

// patchRun applies p and reports the status before and after, so callers can notify on a transition.
func patchRun(ctx context.Context, db *pgxpool.Pool, id string, p runPatch) (before, after string, err error) {
	err = db.QueryRow(ctx, `UPDATE runs r SET
			name = coalesce($2, r.name),
			status = coalesce($3, r.status),
			config = r.config || coalesce($4::jsonb, '{}'),
			summary = r.summary || coalesce($5::jsonb, '{}'),
			tags = coalesce($6, r.tags),
			notes = coalesce($7, r.notes),
			ended_at = CASE WHEN coalesce($3, r.status) = 'running' THEN NULL ELSE coalesce(r.ended_at, $8, now()) END
		FROM (SELECT status FROM runs WHERE id = $1::uuid FOR UPDATE) old
		WHERE r.id = $1::uuid RETURNING old.status, r.status`,
		id, p.Name, p.Status, nullJSON(p.Config), nullJSON(p.Summary), p.Tags, p.Notes, p.EndedAt).Scan(&before, &after)
	return
}

func nullJSON(b json.RawMessage) *string {
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	s := string(b)
	return &s
}

func keyHash(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// createKey makes a write API key and returns it, keeping only its hash; with a user name it acts as that user
// (empty: a server key that sees everything).
func createKey(ctx context.Context, db *pgxpool.Pool, name, user string) (string, error) {
	b := make([]byte, 24)
	rand.Read(b)
	key := "ry_" + hex.EncodeToString(b)
	tag, err := db.Exec(ctx, `INSERT INTO api_keys (name, hash, user_id) SELECT $1, $2, (SELECT id FROM users WHERE name = $3)
		WHERE $3 = '' OR EXISTS (SELECT 1 FROM users WHERE name = $3)`, name, keyHash(key), user)
	if err == nil && tag.RowsAffected() == 0 {
		return "", noSuch("user " + user)
	}
	return key, err
}

func validKey(ctx context.Context, db *pgxpool.Pool, key string) bool {
	var ok bool
	db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM api_keys WHERE hash = $1 AND 'write' = ANY(scopes))`, keyHash(key)).Scan(&ok)
	return ok
}
