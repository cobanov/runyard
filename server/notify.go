package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// crashAfter is how long a running run may go without a heartbeat (the client sends one every 30s) before it counts as crashed.
const crashAfter = 5 * time.Minute

type channelConfig struct {
	URL   string `json:"url"`            // ntfy: server URL with topic; webhook, slack, discord: the webhook URL; telegram: Bot API base, empty for the official one
	Token string `json:"token"`          // ntfy access token (optional), telegram bot token
	Chat  string `json:"chat,omitempty"` // telegram chat id
}

type message struct {
	Event    string `json:"event"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	URL      string `json:"url,omitempty"`       // the run page, when RUNYARD_URL is set
	ImageURL string `json:"image_url,omitempty"` // the run's latest image, when RUNYARD_URL is set
	Run      Run    `json:"run"`
	image    []byte // that image, attached where the channel allows it
	imageExt string
	priority string
	tags     string
}

// notify sends a run event (finished, failed, crashed, alert) to the channels whose rules match. Runs in its own goroutine.
func (s *srv) notify(runID, event string, a *Alert) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	run, err := getRun(ctx, s.db, runID)
	if err != nil {
		log.Printf("notify %s: %v", runID, err)
		return
	}
	rows, _ := s.db.Query(ctx, `SELECT DISTINCT c.id, c.kind, c.config FROM rules r JOIN channels c ON c.id = r.channel_id
		LEFT JOIN projects p ON p.id = r.project_id WHERE r.event = $1 AND (r.project_id IS NULL OR p.name = $2)`, event, run.Project)
	chans, err := pgx.CollectRows(rows, pgx.RowToStructByPos[channel])
	if err != nil {
		log.Printf("notify %s: %v", runID, err)
	}
	if len(chans) == 0 {
		return
	}
	m := s.message(ctx, run, event, a)
	for _, c := range chans {
		if err := c.send(ctx, m); err != nil {
			log.Printf("notify %s via channel %d: %v", runID, c.ID, err)
		}
	}
}

type channel struct {
	ID     int64
	Kind   string
	Config channelConfig
}

func (s *srv) message(ctx context.Context, run Run, event string, a *Alert) message {
	m := message{Event: event, Run: run, priority: "default"}
	name := run.Project + "/" + run.Name
	took := fmtDuration(time.Since(run.StartedAt))
	if run.EndedAt != nil {
		took = fmtDuration(run.EndedAt.Sub(run.StartedAt))
	}
	switch event {
	case "finished":
		m.Title, m.tags = name+" finished", "white_check_mark"
		m.Body = "Ran for " + took + "." + results(run)
	case "failed":
		m.Title, m.tags, m.priority = name+" failed", "x", "high"
		m.Body = "Failed after " + took + "." + s.lastLines(ctx, run.ID)
	case "crashed":
		m.Title, m.tags, m.priority = name+" went silent", "warning", "high"
		m.Body = fmt.Sprintf("No heartbeat for %s after %s of running: the process likely died (killed, out of memory, machine down).", fmtDuration(time.Since(run.LastHeartbeatAt)), took) + s.lastLines(ctx, run.ID)
	case "alert":
		m.Title, m.Body = a.Title, name
		if a.Body != "" {
			m.Body += ": " + a.Body
		}
		m.tags = map[string]string{"info": "information_source", "warn": "warning", "error": "rotating_light"}[a.Level]
		if a.Level == "error" {
			m.priority = "high"
		}
	}
	if s.url != "" {
		m.URL = s.url + "/#/r/" + run.ID
	}
	var rel string
	if s.db.QueryRow(ctx, `SELECT path FROM media WHERE run_id = $1::uuid AND kind = 'image' ORDER BY step DESC, created_at DESC LIMIT 1`, run.ID).Scan(&rel) == nil {
		m.image, _ = readBlob(ctx, s.store, rel)
		m.imageExt = filepath.Ext(rel)
		if s.url != "" {
			m.ImageURL = s.url + "/media/" + rel
		}
	}
	return m
}

// results lists the last value of each metric and the summary on top, so the phone shows how the run ended.
func results(run Run) string {
	vals := map[string]any{}
	for k, v := range run.Metrics {
		if !strings.HasPrefix(k, "sys/") {
			vals[k] = float64(v.Last)
		}
	}
	json.Unmarshal(run.Summary, &vals)
	keys := make([]string, 0, len(vals))
	for k := range vals {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { // losses first: the phone shows only the first few
		li, lj := strings.Contains(keys[i], "loss"), strings.Contains(keys[j], "loss")
		if li != lj {
			return li
		}
		return keys[i] < keys[j]
	})
	var b strings.Builder
	for i, k := range keys {
		if i == 8 {
			fmt.Fprintf(&b, "\n…and %d more", len(keys)-i)
			break
		}
		v, _ := json.Marshal(vals[k])
		if f, ok := vals[k].(float64); ok {
			v = []byte(strconv.FormatFloat(f, 'g', 4, 64)) // also NaN and ±Inf, which JSON cannot hold
		}
		fmt.Fprintf(&b, "\n%s: %s", k, v)
	}
	return b.String()
}

func (s *srv) lastLines(ctx context.Context, runID string) string {
	lines, _ := getLogs(ctx, s.db, runID, 0, 6, true)
	if len(lines) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\nLast output:")
	for _, l := range lines {
		b.WriteString("\n" + l.Line)
	}
	return b.String()
}

func fmtDuration(d time.Duration) string {
	d = d.Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dd %dh", int(d.Hours())/24, int(d.Hours())%24)
}

func (c channel) send(ctx context.Context, m message) error {
	switch c.Kind {
	case "webhook":
		b, _ := json.Marshal(m)
		return post(ctx, "POST", c.Config.URL, "", bytes.NewReader(b), "application/json")
	case "slack":
		return sendSlack(ctx, c.Config.URL, m)
	case "discord":
		return sendDiscord(ctx, c.Config.URL, m)
	case "telegram":
		return sendTelegram(ctx, c.Config.URL, c.Config.Token, c.Config.Chat, m)
	}
	q := url.Values{"title": {m.Title}, "message": {m.Body}, "priority": {m.priority}}
	if m.tags != "" {
		q.Set("tags", m.tags)
	}
	if m.URL != "" {
		q.Set("click", m.URL)
	}
	if m.image != nil {
		q.Set("filename", "sample"+m.imageExt)
		err := post(ctx, "PUT", c.Config.URL+"?"+q.Encode(), c.Config.Token, bytes.NewReader(m.image), "")
		if err == nil {
			return nil
		}
		log.Printf("ntfy attachment refused, sending without it: %v", err)
		q.Del("filename")
	}
	return post(ctx, "POST", c.Config.URL+"?"+q.Encode(), c.Config.Token, nil, "")
}

func post(ctx context.Context, method, u, token string, body io.Reader, ctype string) error {
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 300))
		return fmt.Errorf("%s: %s", res.Status, b)
	}
	return nil
}

// markCrashed flags running runs whose heartbeat is older than after, notifies, and returns their ids.
func (s *srv) markCrashed(ctx context.Context, after time.Duration) []string {
	// It ended at its last sign of life: the last heartbeat, or a later output line (heartbeats are 30 s apart).
	rows, _ := s.db.Query(ctx, `UPDATE runs SET status = 'crashed', ended_at = greatest(last_heartbeat_at,
			(SELECT ts FROM logs WHERE run_id = runs.id ORDER BY seq DESC LIMIT 1))
		WHERE status = 'running' AND last_heartbeat_at < now() - coalesce(crash_after, make_interval(secs => $1)) RETURNING id::text`, after.Seconds())
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		log.Print("crash check: ", err)
	}
	for _, id := range ids {
		s.hub.publish(event{Type: "run", Run: id})
		go s.notify(id, "crashed", nil)
	}
	return ids
}

func (s *srv) watchCrashes(ctx context.Context) {
	// Wait one window first: right after a server restart every run looks silent until its next heartbeat.
	t := time.NewTimer(crashAfter)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		s.markCrashed(ctx, crashAfter)
		s.markStalled(ctx)
		t.Reset(30 * time.Second)
	}
}
