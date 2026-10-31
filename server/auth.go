package main

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// With RUNYARD_AUTH on, every API, media and MCP request needs a user: an API key in the Authorization header,
// or the session cookie the web UI gets by logging in. Admins and keys made without -user see everything; other
// users see projects without a team and their teams' projects. Without RUNYARD_AUTH reads are open, as before.

type principal struct {
	user  int64 // 0 for a server key
	name  string
	teams []int64 // nil: sees everything
}

type principalKey struct{}

// vis is what the request may see, as a query parameter: nil (everything) or the user's team ids.
// Queries filter with ($n::bigint[] IS NULL OR p.team_id IS NULL OR p.team_id = ANY($n)).
func vis(ctx context.Context) []int64 {
	if p, _ := ctx.Value(principalKey{}).(*principal); p != nil {
		return p.teams
	}
	return nil // no principal: auth is off, or the server itself (notifications, checks)
}

func principalOf(ctx context.Context) *principal {
	p, _ := ctx.Value(principalKey{}).(*principal)
	return p
}

// lookup finds who a request is: by API key when it has an Authorization header, else by session cookie.
func (s *srv) lookup(r *http.Request) (*principal, error) {
	ctx := r.Context()
	p := &principal{}
	var admin bool
	if h := r.Header.Get("Authorization"); h != "" {
		key, _ := strings.CutPrefix(h, "Bearer ")
		var user *int64
		var name *string
		err := s.db.QueryRow(ctx, `SELECT k.user_id, u.name, coalesce(u.admin, false) FROM api_keys k LEFT JOIN users u ON u.id = k.user_id
			WHERE k.hash = $1`, keyHash(key)).Scan(&user, &name, &admin)
		if err != nil {
			return nil, err
		}
		if user == nil {
			return p, nil // a server key
		}
		p.user, p.name = *user, *name
	} else if c, err := r.Cookie(sessionCookie); err == nil {
		err := s.db.QueryRow(ctx, `SELECT u.id, u.name, u.admin FROM sessions s JOIN users u ON u.id = s.user_id
			WHERE s.hash = $1 AND s.expires_at > now()`, keyHash(c.Value)).Scan(&p.user, &p.name, &admin)
		if err != nil {
			return nil, err
		}
	} else {
		return nil, pgx.ErrNoRows
	}
	if admin {
		return p, nil
	}
	rows, _ := s.db.Query(ctx, `SELECT team_id FROM members WHERE user_id = $1 ORDER BY team_id`, p.user)
	teams, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	p.teams = append([]int64{}, teams...) // never nil: no teams sees only the projects without one
	return p, err
}

const sessionCookie = "runyard_session"

// guard puts the request's principal in its context, and with auth on refuses requests without one and requests
// for a run (by path) the principal may not see.
func (s *srv) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		open := !strings.HasPrefix(path, "/api/") && !strings.HasPrefix(path, "/media/") && path != "/mcp" ||
			slices.Contains([]string{"/api/v1/health", "/api/v1/login", "/api/v1/logout", "/api/v1/me"}, path)
		if !s.auth || open {
			next.ServeHTTP(w, r)
			return
		}
		p, err := s.lookup(r)
		if err != nil {
			if strings.HasPrefix(path, "/api/2.0/") {
				mlError(w, mlErr{401, "UNAUTHENTICATED", "set MLFLOW_TRACKING_TOKEN to a runyard API key"})
			} else {
				httpError(w, http.StatusUnauthorized, "log in, or send an API key as a Bearer token")
			}
			return
		}
		ctx := context.WithValue(r.Context(), principalKey{}, p)
		r = r.WithContext(ctx)
		if p.teams != nil {
			if run := pathRun(path); run != "" && !s.canSee(ctx, run) {
				httpError(w, http.StatusNotFound, "no run "+run)
				return
			}
			if id, ok := strings.CutPrefix(path, "/api/v1/artifacts/"); ok {
				var run string
				s.db.QueryRow(ctx, `SELECT run_id::text FROM artifacts WHERE id::text = $1`, strings.TrimSuffix(id, "/file")).Scan(&run)
				if !s.canSee(ctx, run) {
					http.NotFound(w, r)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// pathRun is the run a path names: /api/v1/runs/<ref>[/...] or /media/<run id>/<file>.
func pathRun(path string) string {
	for _, prefix := range []string{"/api/v1/runs/", "/media/"} {
		if rest, ok := strings.CutPrefix(path, prefix); ok {
			ref, _, _ := strings.Cut(rest, "/")
			return ref
		}
	}
	return ""
}

// canSee reports whether the request may see the run. ref is an exact id, except on GET /api/v1/runs/<ref> where it
// may be a prefix or name: then the visible run it resolves to counts, the same one the handler resolves.
func (s *srv) canSee(ctx context.Context, ref string) bool {
	if !uuidRe.MatchString(ref) {
		_, err := s.resolveRun(ctx, ref)
		return err == nil
	}
	var ok bool
	s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM runs r JOIN projects p ON p.id = r.project_id
		WHERE r.id = $1::uuid AND ($2::bigint[] IS NULL OR p.team_id IS NULL OR p.team_id = ANY($2)))`, ref, vis(ctx)).Scan(&ok)
	return ok
}

// projectTeam is the team a project created by this request goes to: the user's first team, none for admins and server keys.
func projectTeam(ctx context.Context) *int64 {
	if t := vis(ctx); len(t) > 0 {
		return &t[0]
	}
	return nil
}

var errHidden = errors.New("this project belongs to a team you are not in")

// seesProject checks a project the request is about to write to.
func (s *srv) seesProject(ctx context.Context, project int64) error {
	var ok bool
	err := s.db.QueryRow(ctx, `SELECT $2::bigint[] IS NULL OR team_id IS NULL OR team_id = ANY($2) FROM projects WHERE id = $1`, project, vis(ctx)).Scan(&ok)
	if err == nil && !ok {
		return errHidden
	}
	return err
}

// Passwords: PBKDF2-SHA256 from the standard library, 600k iterations (OWASP's figure for it).
const pbkdf2Iter = 600_000

func hashPassword(pw string) string {
	salt := make([]byte, 16)
	rand.Read(salt)
	k, _ := pbkdf2.Key(sha256.New, pw, salt, pbkdf2Iter, 32)
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", pbkdf2Iter, b64.EncodeToString(salt), b64.EncodeToString(k))
}

func checkPassword(stored, pw string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iter, err1 := strconv.Atoi(parts[1])
	salt, err2 := b64.DecodeString(parts[2])
	want, err3 := b64.DecodeString(parts[3])
	if err1 != nil || err2 != nil || err3 != nil {
		return false
	}
	got, _ := pbkdf2.Key(sha256.New, pw, salt, iter, len(want))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// dummyHash makes a login for a missing user take as long as one with a wrong password.
var dummyHash = hashPassword("")

func (s *srv) authRoutes(mux *http.ServeMux) {
	// shortcut: no rate limit on logins beyond PBKDF2's cost (~0.2 s each); put one in front if the server is public.
	mux.HandleFunc("POST /api/v1/login", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Name, Password string }
		if err := decode(r, &in); err != nil {
			s.fail(w, err)
			return
		}
		var id int64
		stored := dummyHash
		err := s.db.QueryRow(r.Context(), `SELECT id, password FROM users WHERE name = $1`, in.Name).Scan(&id, &stored)
		if !checkPassword(stored, in.Password) || err != nil {
			httpError(w, http.StatusUnauthorized, "wrong name or password")
			return
		}
		b := make([]byte, 32)
		rand.Read(b)
		token := hex.EncodeToString(b)
		expires := time.Now().Add(30 * 24 * time.Hour)
		if _, err := s.db.Exec(r.Context(), `INSERT INTO sessions (hash, user_id, expires_at) VALUES ($1, $2, $3)`, keyHash(token), id, expires); err != nil {
			s.fail(w, err)
			return
		}
		s.db.Exec(r.Context(), `DELETE FROM sessions WHERE expires_at < now()`)
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", Expires: expires, HttpOnly: true,
			SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"})
		writeJSON(w, http.StatusOK, map[string]string{"user": in.Name})
	})
	mux.HandleFunc("POST /api/v1/logout", func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(sessionCookie); err == nil {
			s.db.Exec(r.Context(), `DELETE FROM sessions WHERE hash = $1`, keyHash(c.Value))
		}
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", MaxAge: -1})
		writeJSON(w, http.StatusOK, struct{}{})
	})
	mux.HandleFunc("GET /api/v1/me", func(w http.ResponseWriter, r *http.Request) {
		me := map[string]any{"auth": s.auth, "user": ""}
		if s.auth {
			if p, err := s.lookup(r); err == nil {
				me["user"] = or2(p.name, "server key")
			}
		}
		writeJSON(w, http.StatusOK, me)
	})
}

// runFilter decides which runs' events an SSE client gets; runs never change project, so answers are kept.
// shortcut: a project moved to another team mid-stream keeps its old answer until the client reconnects.
func (s *srv) runFilter(ctx context.Context) func(string) bool {
	if vis(ctx) == nil {
		return nil
	}
	seen := map[string]bool{}
	return func(run string) bool {
		ok, known := seen[run]
		if !known {
			ok = s.canSee(ctx, run)
			seen[run] = ok
		}
		return ok
	}
}
