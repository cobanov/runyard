package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// csrf refuses cross-origin browser requests: admin calls may come with the session cookie alone.
var csrf http.CrossOriginProtection

// admin guards what changes the server itself (deleting runs and projects, API keys, channels). With RUNYARD_AUTH,
// an admin or a server key, by session cookie or API key; without it, any API key, as for writes.
func (s *srv) admin(f func(*http.Request) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := csrf.Check(r); err != nil {
			httpError(w, http.StatusForbidden, err.Error())
			return
		}
		if !s.auth {
			s.write(f)(w, r)
			return
		}
		if p := principalOf(r.Context()); p == nil || p.teams != nil || p.logOnly {
			httpError(w, http.StatusForbidden, "only an admin can do this")
			return
		}
		s.read(f)(w, r)
	}
}

// curate guards the light changes the web UI makes in place (a run's notes and tags, dismissing alerts): with
// RUNYARD_AUTH, any user who may see the run (guard checks that); without it, anyone, as anyone may read.
// Deleting, keys and channels stay with admin.
func (s *srv) curate(f func(*http.Request) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := csrf.Check(r); err != nil {
			httpError(w, http.StatusForbidden, err.Error())
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		s.read(f)(w, r)
	}
}

// adminRoutes: the web UI's Settings page and delete buttons, and the same for scripts and agents with an admin key.
func (s *srv) adminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("DELETE /api/v1/runs/{id}", s.admin(func(r *http.Request) (any, error) {
		return struct{}{}, s.purge(r.Context(), "run", `SELECT id FROM runs WHERE id = $1::uuid`, `DELETE FROM runs WHERE id = $1::uuid`, r.PathValue("id"))
	}))
	mux.HandleFunc("DELETE /api/v1/projects/{name...}", s.admin(func(r *http.Request) (any, error) {
		return struct{}{}, s.purge(r.Context(), "project", `SELECT r.id FROM runs r JOIN projects p ON p.id = r.project_id WHERE p.name = $1`,
			`DELETE FROM projects WHERE name = $1`, r.PathValue("name"))
	}))

	mux.HandleFunc("GET /api/v1/keys", s.admin(func(r *http.Request) (any, error) {
		type key struct {
			ID        int64     `json:"id"`
			Name      string    `json:"name"`
			User      string    `json:"user"` // "" for a server key, which sees everything
			CreatedAt time.Time `json:"created_at"`
			Scope     string    `json:"scope"` // "log": a training machine's key; "full": it also deletes and manages
		}
		rows, _ := s.db.Query(r.Context(), `SELECT k.id, k.name, coalesce(u.name, ''), k.created_at,
			CASE WHEN 'admin' = ANY(k.scopes) THEN 'full' ELSE 'log' END FROM api_keys k
			LEFT JOIN users u ON u.id = k.user_id ORDER BY k.id`)
		return pgx.CollectRows(rows, pgx.RowToStructByPos[key])
	}))
	makeKey := func(r *http.Request) (any, error) {
		var in struct{ Name, User, Scope string }
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.Name) == "" {
			return nil, badRequest{errors.New("name is required")}
		}
		// The first key of a server is always a full one: something has to be able to make the others.
		key, err := createKey(r.Context(), s.db, strings.TrimSpace(in.Name), strings.TrimSpace(in.User), in.Scope == "log" && s.hasKeys(r.Context()))
		return map[string]string{"key": key}, err // the only time the key is shown
	}
	mux.HandleFunc("POST /api/v1/keys", func(w http.ResponseWriter, r *http.Request) {
		// A fresh server without users has no key to ask for, so whoever opens it first makes the first one from the
		// browser, as most self-hosted tools start. Once a key exists this door is shut.
		if !s.auth && r.Header.Get("Authorization") == "" && !s.hasKeys(r.Context()) {
			s.curate(makeKey)(w, r)
			return
		}
		s.admin(makeKey)(w, r)
	})
	mux.HandleFunc("DELETE /api/v1/keys/{id}", s.admin(func(r *http.Request) (any, error) {
		return struct{}{}, exec(r.Context(), s.db, "key", `DELETE FROM api_keys WHERE id = $1::bigint`, r.PathValue("id"))
	}))

	mux.HandleFunc("GET /api/v1/channels", s.admin(func(r *http.Request) (any, error) { return listChannels(r.Context(), s.db) }))
	mux.HandleFunc("POST /api/v1/channels", s.admin(func(r *http.Request) (any, error) {
		var in struct {
			Kind, Target, Token, Project string
			Events                       []string
		}
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		id, err := addChannel(r.Context(), s.db, in.Kind, strings.TrimSpace(in.Target), strings.TrimSpace(in.Token), in.Project, in.Events)
		return map[string]int64{"id": id}, err
	}))
	mux.HandleFunc("DELETE /api/v1/channels/{id}", s.admin(func(r *http.Request) (any, error) {
		return struct{}{}, removeChannel(r.Context(), s.db, r.PathValue("id"))
	}))
	mux.HandleFunc("POST /api/v1/channels/{id}/test", s.admin(func(r *http.Request) (any, error) {
		return map[string]bool{"sent": true}, testChannel(r.Context(), s.db, r.PathValue("id"))
	}))
}

func (s *srv) hasKeys(ctx context.Context) bool {
	var ok bool
	s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM api_keys)`).Scan(&ok)
	return ok
}

// purge deletes runs and the files they had in the blob store. runs selects the run ids (from arg) and locks
// them, so an upload to one waits, then fails on the missing run and drops its own file; del deletes the rows
// (metrics, logs, media, alerts and artifacts cascade). Rows go first: a failure can leave a stray file, never
// a run pointing at a missing one.
func (s *srv) purge(ctx context.Context, what, runs, del string, arg any) error {
	var keys []string
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		rows, _ := tx.Query(ctx, `WITH r AS (`+runs+` FOR UPDATE)
			SELECT path FROM media WHERE run_id IN (SELECT id FROM r)
			UNION ALL SELECT path FROM artifacts WHERE path IS NOT NULL AND run_id IN (SELECT id FROM r)`, arg)
		var err error
		if keys, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, del, arg)
		if err == nil && tag.RowsAffected() == 0 {
			return noSuch(what)
		}
		return err
	})
	if err != nil {
		return err
	}
	go func() { // a project can hold thousands of files; the caller need not wait for the bucket
		for _, k := range keys {
			if err := s.store.remove(context.WithoutCancel(ctx), k); err != nil && !errors.Is(err, os.ErrNotExist) {
				log.Printf("delete %s: %v", k, err)
			}
		}
	}()
	s.hub.publish(event{Type: "run"}) // no run id, so every client reloads its lists, whatever it may see
	return nil
}
