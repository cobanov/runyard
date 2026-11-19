package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5"
)

// The outbox: every notification is written down before its first try and crossed out once the channel took it.
// A channel that is down (ntfy at home while the home network is out, a push service answering 500) gets it again
// later, after a server restart too; one that keeps refusing for hours is listed on the Alerts page.

// retryAfter is the wait before each further try: six tries over about eight and a half hours.
var retryAfter = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour}

// queued is a message as the outbox keeps it: what the webhook's JSON carries, the parts other channels read, and
// the sample image by its path, read again for each try.
type queued struct {
	message
	Priority string `json:"priority"`
	Tags     string `json:"tags,omitempty"`
	Image    string `json:"image,omitempty"`
}

// statusError is an HTTP answer a channel gave instead of taking the message.
type statusError struct {
	code int
	msg  string
}

func (e statusError) Error() string { return e.msg }

// permanent says whether trying again cannot help: the channel refused the request itself (a wrong chat id, a
// deleted webhook), as opposed to being down, slow or overloaded.
func permanent(err error) bool {
	var se statusError
	if !errors.As(err, &se) {
		return false
	}
	return se.code >= 400 && se.code < 500 && se.code != http.StatusRequestTimeout && se.code != http.StatusTooEarly &&
		se.code != http.StatusTooManyRequests
}

// deliver sends m to every channel, through the outbox, dropping the ones whose endpoint is gone for good.
func (s *srv) deliver(ctx context.Context, chans []channel, m message) {
	q, _ := json.Marshal(queued{m, m.priority, m.tags, m.imagePath})
	for _, c := range chans {
		var id int64
		// The lease (next_at) covers this try: a server stopped in the middle sends it again after a restart.
		err := s.db.QueryRow(ctx, `INSERT INTO outbox (channel_id, run_id, message, next_at)
			VALUES ($1, nullif($2, '')::uuid, $3, now() + $4 * interval '1 second') RETURNING id`,
			c.ID, m.Run.ID, q, retryAfter[0].Seconds()).Scan(&id)
		if err != nil {
			log.Printf("outbox: %v", err) // still worth one try
		}
		s.attempt(ctx, c, m, id, 0)
	}
}

// attempt sends m to c once and records the outcome on outbox row id (0: none), which has had tries before.
func (s *srv) attempt(ctx context.Context, c channel, m message, id int64, tries int) {
	one, cancel := context.WithTimeout(ctx, 15*time.Second) // a channel that hangs must not use up the others' time
	err := c.send(one, m)
	cancel()
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err // without the URL: it holds the Telegram bot token or the Slack webhook secret
	}
	db := context.WithoutCancel(ctx) // the outcome is written even when the send used up the time
	switch {
	case err == nil:
		if id != 0 {
			s.db.Exec(db, `DELETE FROM outbox WHERE id = $1`, id)
		}
	case errors.Is(err, errGone):
		s.db.Exec(db, `DELETE FROM channels WHERE id = $1`, c.ID) // and its outbox rows with it
	default:
		tries++
		var wait *float64 // nil: given up
		if tries <= len(retryAfter) && !permanent(err) {
			w := retryAfter[tries-1].Seconds()
			wait = &w
		}
		then := "giving up"
		if wait != nil {
			then = "trying again in " + fmtDuration(time.Duration(*wait)*time.Second)
		}
		log.Printf("notify %q via channel %d, try %d: %v; %s", m.Title, c.ID, tries, err, then)
		if id == 0 {
			return
		}
		s.db.Exec(db, `UPDATE outbox SET attempts = $2, last_error = $3, next_at = now() + $4 * interval '1 second' WHERE id = $1`,
			id, tries, cut(err.Error(), 300), wait)
		s.hub.publish(event{Type: "alert"}) // the Alerts page lists it
	}
}

// retryOutbox tries again every notification that is due, a batch at a time; several servers on one database
// each take their own rows.
func (s *srv) retryOutbox(ctx context.Context) {
	rows, _ := s.db.Query(ctx, `UPDATE outbox o SET next_at = now() + interval '2 minutes'
		FROM channels c WHERE c.id = o.channel_id AND o.id IN (
			SELECT id FROM outbox WHERE next_at <= now() ORDER BY next_at LIMIT 20 FOR UPDATE SKIP LOCKED)
		RETURNING o.id, o.attempts, o.message, o.created_at, c.id, c.kind, c.config`)
	type due struct {
		id    int64
		tries int
		q     queued
		at    time.Time
		c     channel
	}
	var all []due
	var d due
	_, err := pgx.ForEachRow(rows, []any{&d.id, &d.tries, &d.q, &d.at, &d.c.ID, &d.c.Kind, &d.c.Config}, func() error {
		all = append(all, d)
		d = due{}
		return nil
	})
	if err != nil {
		log.Print("outbox: ", err)
	}
	for _, d := range all {
		m := d.q.message
		m.priority, m.tags, m.imagePath = d.q.Priority, d.q.Tags, d.q.Image
		if d.q.Image != "" {
			m.image, _ = readBlob(ctx, s.store, d.q.Image)
			m.imageExt = filepath.Ext(d.q.Image)
		}
		m.Body += fmt.Sprintf("\n\n(Sent %s late: the channel did not take it earlier.)", fmtDuration(time.Since(d.at)))
		s.attempt(ctx, d.c, m, d.id, d.tries)
	}
}

func (s *srv) watchOutbox(ctx context.Context) {
	for sweeps := 0; ; sweeps++ {
		s.retryOutbox(ctx)
		if sweeps%120 == 0 { // hourly: what was given up on a month ago is history
			s.db.Exec(ctx, `DELETE FROM outbox WHERE next_at IS NULL AND created_at < now() - interval '30 days'`)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}
	}
}

// undelivered is a notification that did not reach its channel at the first try.
type undelivered struct {
	ID        int64      `json:"id"`
	Title     string     `json:"title"`
	Event     string     `json:"event"`
	Channel   int64      `json:"channel"`
	Kind      string     `json:"kind"`
	Run       *string    `json:"run"` // null for a machine notice
	Tries     int        `json:"tries"`
	Error     string     `json:"error"`
	CreatedAt time.Time  `json:"created_at"`
	NextAt    *time.Time `json:"next_at"` // null: given up
}

func (s *srv) outboxRoutes(mux *http.ServeMux) {
	// Notifications that failed at least once and are still being tried, or were given up on and not dismissed.
	mux.HandleFunc("GET /api/v1/notifications/undelivered", s.read(func(r *http.Request) (any, error) {
		rows, _ := s.db.Query(r.Context(), `SELECT o.id, o.message->>'title', o.message->>'event', c.id, c.kind, o.run_id::text,
				o.attempts, o.last_error, o.created_at, o.next_at
			FROM outbox o JOIN channels c ON c.id = o.channel_id
			LEFT JOIN runs r ON r.id = o.run_id LEFT JOIN projects p ON p.id = r.project_id
			WHERE o.attempts > 0 AND o.dismissed_at IS NULL
			AND ($1::bigint[] IS NULL OR o.run_id IS NULL OR p.team_id IS NULL OR p.team_id = ANY($1))
			ORDER BY o.created_at DESC LIMIT 100`, vis(r.Context()))
		return pgx.CollectRows(rows, pgx.RowToStructByPos[undelivered])
	}))
	mux.HandleFunc("POST /api/v1/notifications/dismiss", s.curate(func(r *http.Request) (any, error) {
		var in struct {
			IDs []int64 `json:"ids"` // empty: every given-up notification the request may see
		}
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		tag, err := s.db.Exec(r.Context(), `UPDATE outbox o SET dismissed_at = now() WHERE o.next_at IS NULL AND o.dismissed_at IS NULL
			AND (coalesce(cardinality($1::bigint[]), 0) = 0 OR o.id = ANY($1))
			AND ($2::bigint[] IS NULL OR o.run_id IS NULL OR EXISTS (SELECT 1 FROM runs r JOIN projects p ON p.id = r.project_id
				WHERE r.id = o.run_id AND (p.team_id IS NULL OR p.team_id = ANY($2))))`, in.IDs, vis(r.Context()))
		if err == nil {
			s.hub.publish(event{Type: "alert"})
		}
		return map[string]int64{"dismissed": tag.RowsAffected()}, err
	}))
}
