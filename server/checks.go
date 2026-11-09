package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type check struct {
	ID        int64
	Kind      string // nan, above, below, stall
	Key       string
	Value     *float64 // the threshold, or minutes for stall
	AfterStep int64
	Level     string
}

// match reports whether a metric name fits a check's key, where * stands for any run of characters, slashes included.
func match(pattern, name string) bool {
	ok, _ := regexp.MatchString("^"+strings.ReplaceAll(regexp.QuoteMeta(pattern), `\*`, ".*")+"$", name)
	return ok
}

func (c check) trips(v float64) bool {
	switch c.Kind {
	case "nan":
		return math.IsNaN(v) || math.IsInf(v, 0)
	case "above":
		return v > *c.Value
	case "below":
		return v < *c.Value
	}
	return false
}

func fmtNum(v float64) string { return strconv.FormatFloat(v, 'g', 4, 64) }

// checkMetrics raises the alert of every metric check that a value in rows trips, at its first tripping step, once per run.
func (s *srv) checkMetrics(ctx context.Context, runID string, project int64, rows []metricRow) {
	q, _ := s.db.Query(ctx, `SELECT id, kind, key, value, after_step, level FROM checks c
		WHERE kind <> 'stall' AND (project_id IS NULL OR project_id = $1)
		AND NOT EXISTS (SELECT 1 FROM alerts WHERE run_id = $2::uuid AND check_id = c.id)`, project, runID)
	checks, err := pgx.CollectRows(q, pgx.RowToStructByPos[check])
	if err != nil {
		log.Printf("checks for %s: %v", runID, err)
		return
	}
	old := time.Now().Add(-crashAfter)
	for _, c := range checks {
		found := false
		var step int64
		var key string
		var val float64
		for _, row := range rows {
			// Data older than the crash window is history being synced (runyard sync), not news.
			if row.Step < c.AfterStep || (row.TS != nil && row.TS.Before(old)) || (found && row.Step > step) {
				continue
			}
			for k, v := range row.Values {
				if c.trips(float64(v)) && match(c.Key, k) && (!found || row.Step < step || k < key) { // the cheap test first
					found, step, key, val = true, row.Step, k, float64(v)
				}
			}
		}
		if !found {
			continue
		}
		a := Alert{Level: c.Level, Title: key + " is " + fmtNum(val), Body: fmt.Sprintf("at step %d", step)}
		if c.Kind != "nan" {
			a.Title = fmt.Sprintf("%s %s %s", key, c.Kind, fmtNum(*c.Value))
			a.Body = fmt.Sprintf("%s at step %d", fmtNum(val), step)
		}
		if _, err := s.addAlert(ctx, runID, &c.ID, a); err != nil {
			log.Printf("check %d on %s: %v", c.ID, runID, err)
		}
	}
}

// progressed records that a run made progress (a training metric or an output line), for stall checks.
func (s *srv) progressed(ctx context.Context, runID string) {
	// At most one write per 10 s: clients send a batch every second.
	s.db.Exec(ctx, `UPDATE runs SET progress_at = now() WHERE id = $1::uuid AND progress_at < now() - interval '10 seconds'`, runID)
}

// markStalled raises the alert of every stall check whose running run made no progress for its minutes, once per run.
// markCrashed runs first, so every run still running here sends heartbeats: it is alive but stuck.
func (s *srv) markStalled(ctx context.Context) {
	rows, _ := s.db.Query(ctx, `SELECT r.id::text, c.id, c.level, c.value FROM runs r
		JOIN checks c ON c.kind = 'stall' AND (c.project_id IS NULL OR c.project_id = r.project_id)
		WHERE r.status = 'running' AND r.progress_at < now() - c.value * interval '1 minute'
		AND (r.crash_after IS NULL OR c.project_id IS NOT NULL) -- MLflow runs are quiet by nature: only their project's own check
		AND NOT EXISTS (SELECT 1 FROM alerts WHERE run_id = r.id AND check_id = c.id)`)
	type hit struct {
		Run     string
		Check   int64
		Level   string
		Minutes float64
	}
	hits, err := pgx.CollectRows(rows, pgx.RowToStructByPos[hit])
	if err != nil {
		log.Print("stall check: ", err)
	}
	for _, h := range hits {
		a := Alert{Level: h.Level, Title: fmt.Sprintf("No progress for %gm", h.Minutes),
			Body: "Heartbeats still arrive, but no metric or output line came in that time: the process looks stuck." + s.lastLines(ctx, h.Run)}
		if _, err := s.addAlert(ctx, h.Run, &h.Check, a); err != nil {
			log.Printf("stall check %d on %s: %v", h.Check, h.Run, err)
		}
	}
}

// addAlert records an alert and notifies. A check alerts once per run: a repeat records nothing and returns id 0.
func (s *srv) addAlert(ctx context.Context, runID string, checkID *int64, a Alert) (int64, error) {
	var id int64
	err := s.db.QueryRow(ctx, `INSERT INTO alerts (run_id, level, title, body, check_id) VALUES ($1::uuid, $2, $3, $4, $5)
		ON CONFLICT (run_id, check_id) DO NOTHING RETURNING id`, runID, a.Level, a.Title, a.Body, checkID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	s.hub.publish(event{Type: "alert", Run: runID})
	go s.notify(runID, "alert", &a)
	return id, nil
}

const checkUsage = `usage:
  runyard-server check add [-project P] [-level L] [-after STEP] nan KEY
  runyard-server check add [-project P] [-level L] [-after STEP] above|below KEY VALUE
  runyard-server check add [-project P] [-level L] stall DURATION     (for example 30m)
  runyard-server check ls
  runyard-server check rm ID
KEY is a metric name; * matches anything, so '*loss*' covers train/loss and eval/loss.
A check alerts once per run, through the channels listening to "alert". -level: info, warn or error
(default error for nan and stall, warn otherwise). -after skips steps before STEP, for warmup.`

// checkCmd manages the checks that raise alerts from a run's data.
func checkCmd(ctx context.Context, db *pgxpool.Pool, args []string) error {
	if len(args) == 0 {
		return errors.New(checkUsage)
	}
	switch args[0] {
	case "add":
		fs := flag.NewFlagSet("add", flag.ContinueOnError)
		project := fs.String("project", "", "only this project (default: every project)")
		level := fs.String("level", "", "info, warn or error")
		after := fs.Int64("after", 0, "skip steps before this one")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		c := check{Kind: fs.Arg(0), Level: *level, AfterStep: *after}
		a := fs.Args()
		switch {
		case c.Kind == "nan" && len(a) == 2:
			c.Key = a[1]
		case (c.Kind == "above" || c.Kind == "below") && len(a) == 3:
			v, err := strconv.ParseFloat(a[2], 64)
			if err != nil {
				return fmt.Errorf("threshold %q is not a number", a[2])
			}
			c.Key, c.Value = a[1], &v
		case c.Kind == "stall" && len(a) == 2:
			d, err := time.ParseDuration(a[1])
			if err != nil || d < time.Minute {
				return fmt.Errorf("stall needs a duration of at least 1m, like 30m or 2h, not %q", a[1])
			}
			m := d.Minutes()
			c.Value = &m
		default:
			return errors.New(checkUsage)
		}
		if c.Level == "" {
			c.Level = "warn"
			if c.Kind == "nan" || c.Kind == "stall" {
				c.Level = "error"
			}
		}
		var pid *int64
		if *project != "" {
			pid = new(int64)
			if err := db.QueryRow(ctx, `INSERT INTO projects (name) VALUES ($1) ON CONFLICT (name) DO UPDATE SET name = excluded.name RETURNING id`, *project).Scan(pid); err != nil {
				return err
			}
		}
		var id int64
		if err := db.QueryRow(ctx, `INSERT INTO checks (project_id, kind, key, value, after_step, level) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
			pid, c.Kind, c.Key, c.Value, c.AfterStep, c.Level).Scan(&id); err != nil {
			return err
		}
		fmt.Println("added check", id)
	case "ls":
		rows, _ := db.Query(ctx, `SELECT c.id, c.kind, c.key, c.value, c.after_step, c.level, coalesce(p.name, '*')
			FROM checks c LEFT JOIN projects p ON p.id = c.project_id ORDER BY c.id`)
		var c check
		var project string
		_, err := pgx.ForEachRow(rows, []any{&c.ID, &c.Kind, &c.Key, &c.Value, &c.AfterStep, &c.Level, &project}, func() error {
			fmt.Printf("%d\t%s\tproject=%s\tlevel=%s\n", c.ID, c, project, c.Level)
			return nil
		})
		return err
	case "rm":
		if len(args) != 2 {
			return errors.New(checkUsage)
		}
		tag, err := db.Exec(ctx, `DELETE FROM checks WHERE id = $1::bigint`, args[1])
		if err == nil && tag.RowsAffected() == 0 {
			err = errors.New("no check " + args[1])
		}
		return err
	default:
		return errors.New(checkUsage)
	}
	return nil
}

// String reads a check back the way it was added: "nan *loss*", "train/loss above 10 from step 500", "stall 30m".
func (c check) String() string {
	var s string
	switch c.Kind {
	case "nan":
		s = "nan " + c.Key
	case "stall":
		return fmt.Sprintf("stall %gm", *c.Value)
	default:
		s = fmt.Sprintf("%s %s %s", c.Key, c.Kind, fmtNum(*c.Value))
	}
	if c.AfterStep > 0 {
		s += fmt.Sprintf(" from step %d", c.AfterStep)
	}
	return s
}
