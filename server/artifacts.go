package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Artifact struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Kind      string          `json:"kind"`
	Step      *int64          `json:"step"`
	URI       string          `json:"uri"`
	Path      *string         `json:"-"`
	Size      *int64          `json:"size"`
	Meta      json.RawMessage `json:"meta"`
	CreatedAt time.Time       `json:"created_at"`
	URL       string          `json:"url,omitempty"` // download link, when a copy was uploaded
}

const artifactSelect = `SELECT id::text, name, kind, step, uri, path, size, meta, created_at FROM artifacts `

func scanArtifacts(rows pgx.Rows, err error) ([]Artifact, error) {
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (a Artifact, err error) {
		err = row.Scan(&a.ID, &a.Name, &a.Kind, &a.Step, &a.URI, &a.Path, &a.Size, &a.Meta, &a.CreatedAt)
		if a.Path != nil {
			a.URL = "/api/v1/artifacts/" + a.ID + "/file"
		}
		return
	})
}

func listArtifacts(ctx context.Context, db *pgxpool.Pool, runID string) ([]Artifact, error) {
	return scanArtifacts(db.Query(ctx, artifactSelect+`WHERE run_id = $1::uuid ORDER BY created_at, name`, runID))
}

// postArtifact records a file the run produced: JSON {id, name, kind, step, uri, size, meta} for a file that stays
// where it is, or multipart with the same fields plus file to upload a copy.
func (s *srv) postArtifact(r *http.Request) (any, error) {
	runID := r.PathValue("id")
	if !uuidRe.MatchString(runID) {
		return nil, badRequest{errors.New("bad run id")}
	}
	var a Artifact
	var body io.Reader
	var size int64
	var ext string
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			return nil, badRequest{err}
		}
		defer r.MultipartForm.RemoveAll()
		a = Artifact{ID: r.FormValue("id"), Name: r.FormValue("name"), Kind: r.FormValue("kind"), URI: r.FormValue("uri"), Meta: json.RawMessage(r.FormValue("meta"))}
		if v := r.FormValue("step"); v != "" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return nil, badRequest{errors.New("step must be a number")}
			}
			a.Step = &n
		}
		f, hdr, err := r.FormFile("file")
		if err != nil {
			return nil, badRequest{err}
		}
		defer f.Close()
		body, size, ext = f, hdr.Size, hdr.Filename
		if a.Name == "" {
			a.Name = hdr.Filename
		}
	} else if err := decode(r, &a); err != nil {
		return nil, err
	}
	if a.Name == "" {
		return nil, badRequest{errors.New("name is required")}
	}
	if a.ID == "" {
		a.ID = newUUID()
	} else if !uuidRe.MatchString(a.ID) {
		return nil, badRequest{errors.New("bad artifact id")}
	}
	return s.addArtifact(r.Context(), runID, a, body, size, ext)
}

// addArtifact stores body (when not nil) and records the artifact. A retry of one that already landed returns it as is.
func (s *srv) addArtifact(ctx context.Context, runID string, a Artifact, body io.Reader, size int64, filename string) (Artifact, error) {
	if prev, err := scanArtifacts(s.db.Query(ctx, artifactSelect+`WHERE id = $1::uuid`, a.ID)); err != nil || len(prev) == 1 {
		if err != nil {
			return a, err
		}
		return prev[0], nil
	}
	if a.Kind == "" {
		a.Kind = "file"
	}
	if len(a.Meta) == 0 {
		a.Meta = json.RawMessage("{}")
	}
	if body != nil {
		ext := strings.ToLower(filepath.Ext(filename))
		if !safeExt.MatchString(ext) {
			ext = ""
		}
		key := runID + "/artifacts/" + a.ID + ext
		if err := s.store.put(ctx, key, body, size); err != nil {
			return a, err
		}
		a.Path, a.Size = &key, &size
	}
	err := s.db.QueryRow(ctx, `INSERT INTO artifacts (id, run_id, name, kind, step, uri, path, size, meta)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8, $9::jsonb) RETURNING created_at`,
		a.ID, runID, a.Name, a.Kind, a.Step, a.URI, a.Path, a.Size, string(a.Meta)).Scan(&a.CreatedAt)
	if err != nil {
		if a.Path != nil {
			s.store.remove(context.WithoutCancel(ctx), *a.Path)
		}
		return a, err
	}
	if a.Path != nil {
		a.URL = "/api/v1/artifacts/" + a.ID + "/file"
	}
	s.hub.publish(event{Type: "artifact", Run: runID})
	return a, nil
}

// serveArtifact sends an uploaded artifact as a download named after it.
func (s *srv) serveArtifact(w http.ResponseWriter, r *http.Request) {
	var name string
	var key *string
	err := s.db.QueryRow(r.Context(), `SELECT name, path FROM artifacts WHERE id = $1::uuid`, r.PathValue("id")).Scan(&name, &key)
	if err != nil || key == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": path.Base(name)}))
	s.store.serve(w, r, *key)
}
