package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Agents: the coding agents (Claude Code, Codex, OpenCode, Gemini CLI, ...) that start runs. The SDK names the session a
// run comes from (each CLI puts its session id in the environment of the commands it runs), hooks say what the agent
// is doing between runs, and the agent writes what it found with the MCP report tool. The kiosk page and list_agents
// show one card per session; its score is the project's main metric, never what the agent says.

var agentTool = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,39}$`)

const (
	agentStale = 2 * time.Hour    // no hook, report or run activity for this long
	hookFresh  = 30 * time.Minute // a "working" hook older than this means the CLI stopped mid-turn
	doneFor    = 30 * time.Minute // a run that finished shows its session as done for this long
	waitNotice = 2 * time.Minute  // a prompt answered sooner never reaches a phone
)

type Agent struct {
	ID        string     `json:"id"`
	Tool      string     `json:"tool"`
	Label     string     `json:"label"`
	Host      string     `json:"host"`
	CWD       string     `json:"cwd"`
	Project   *string    `json:"project"`
	Metric    string     `json:"metric"` // the project's main metric ('': a loss) and which way is better
	Goal      string     `json:"goal"`
	State     string     `json:"state"` // waiting, working, training, ended, stale, error, done or idle
	Said      string     `json:"said"`  // its last message at the end of a turn, when hooks are installed
	Report    string     `json:"report"`
	Next      string     `json:"next"`
	ReportRun *string    `json:"report_run"` // the name of the run the report is about
	ReportAt  *time.Time `json:"report_at"`
	Runs      int64      `json:"runs"`
	Running   int64      `json:"running"`
	Kept      int64      `json:"kept"` // runs tagged keep
	LastRun   *agentRun  `json:"last_run"`
	StartedAt time.Time  `json:"started_at"`
	ActiveAt  time.Time  `json:"active_at"` // its last hook, report or run activity
	hook      string
	hookAt    *time.Time
}

type agentRun struct {
	ID      string     `json:"id"`
	Name    string     `json:"name"`
	Status  string     `json:"status"`
	EndedAt *time.Time `json:"ended_at"`
}

// state folds what the hooks last said and what the runs are doing into one word, most urgent first.
func (a *Agent) state(now time.Time) string {
	last := ""
	if a.LastRun != nil {
		last = a.LastRun.Status
	}
	switch {
	case a.hook == "waiting" && a.hookAt != nil && now.Sub(*a.hookAt) < agentStale: // older: the CLI was likely killed at its prompt
		return "waiting"
	case a.hook == "working" && a.hookAt != nil && now.Sub(*a.hookAt) < hookFresh:
		return "working"
	case a.Running > 0:
		return "training"
	case a.hook == "ended":
		return "ended"
	case now.Sub(a.ActiveAt) > agentStale:
		return "stale"
	case a.hook == "error" || last == "failed" || last == "crashed":
		return "error"
	case last == "finished" && a.LastRun.EndedAt != nil && now.Sub(*a.LastRun.EndedAt) < doneFor:
		return "done"
	}
	return "idle"
}

// listAgents returns the sessions active in the last hours, newest activity first, those the request may see. A card
// starts with the session's first run or report: per-user hooks hear from every session on a machine, most of them not
// training. Hooks installed for a repository (`--project`, card=1) start it at once.
func listAgents(ctx context.Context, db *pgxpool.Pool, project string, hours int) ([]Agent, error) {
	rows, _ := db.Query(ctx, `SELECT a.id, a.tool, a.label, a.host, a.cwd, p.name, coalesce(p.metric, ''), coalesce(p.goal, 'min'),
			a.hook, a.hook_at, a.said, a.report, a.report_next, rr.name, a.report_at, a.started_at,
			count(r.id), count(r.id) FILTER (WHERE r.status = 'running'), count(r.id) FILTER (WHERE 'keep' = ANY(r.tags)),
			l.id::text, l.name, l.status, l.ended_at, greatest(a.seen_at, max(coalesce(r.ended_at, r.last_heartbeat_at)))
		FROM agent_sessions a LEFT JOIN projects p ON p.id = a.project_id LEFT JOIN runs rr ON rr.id = a.report_run
		LEFT JOIN runs r ON r.agent = a.id
		LEFT JOIN LATERAL (SELECT id, name, status, ended_at FROM runs WHERE agent = a.id ORDER BY started_at DESC LIMIT 1) l ON true
		WHERE ($1::bigint[] IS NULL OR p.team_id IS NULL OR p.team_id = ANY($1)) AND ($2 = '' OR p.name = $2)
		GROUP BY a.id, p.id, rr.id, l.id, l.name, l.status, l.ended_at
		HAVING (a.card OR count(r.id) > 0 OR a.report <> '') AND greatest(a.seen_at, max(coalesce(r.ended_at, r.last_heartbeat_at))) > now() - $3 * interval '1 hour'
		ORDER BY 24 DESC LIMIT 200`, vis(ctx), project, hours)
	now := time.Now()
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (a Agent, err error) {
		var id, name, status *string
		var ended *time.Time
		err = row.Scan(&a.ID, &a.Tool, &a.Label, &a.Host, &a.CWD, &a.Project, &a.Metric, &a.Goal, &a.hook, &a.hookAt, &a.Said,
			&a.Report, &a.Next, &a.ReportRun, &a.ReportAt, &a.StartedAt, &a.Runs, &a.Running, &a.Kept, &id, &name, &status, &ended, &a.ActiveAt)
		if id != nil {
			a.LastRun = &agentRun{*id, *name, *status, ended}
		}
		a.State = a.state(now)
		return
	})
}

// runAgent is the agent session a run says it was started from, as POST /api/v1/runs takes it.
type runAgent struct {
	Tool    string `json:"tool"`
	Session string `json:"session"`
	Label   string `json:"label"`
	CWD     string `json:"cwd"`
}

// recordAgent notes the session a new run comes from and returns its id. A malformed or failed one is dropped: the
// run is created without it, as a run must never be refused for its agent. A run starting also ends a wait: no hook
// says a prompt was answered until the approved command exits, and that command is often the training itself.
func (s *srv) recordAgent(ctx context.Context, a *runAgent, project int64, host string) *string {
	if a == nil || !agentTool.MatchString(a.Tool) || a.Session == "" || len(a.Session) > 200 || len(a.Label) > 100 || len(a.CWD) > 1000 {
		return nil
	}
	id := a.Tool + ":" + a.Session
	_, err := s.db.Exec(ctx, `INSERT INTO agent_sessions (id, tool, label, host, cwd, project_id) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (id) DO UPDATE SET seen_at = now(), project_id = excluded.project_id, host = excluded.host,
			hook = CASE WHEN agent_sessions.hook = 'waiting' THEN 'idle' ELSE agent_sessions.hook END,
			label = CASE WHEN excluded.label <> '' THEN excluded.label ELSE agent_sessions.label END,
			cwd = CASE WHEN agent_sessions.cwd = '' THEN excluded.cwd ELSE agent_sessions.cwd END`,
		id, a.Tool, a.Label, host, a.CWD, project)
	if err != nil {
		log.Print("agent session: ", err)
		return nil
	}
	return &id
}

// hookStates maps a hook event to what the agent does after it. Claude Code and Codex share these names.
var hookStates = map[string]string{
	"SessionStart": "idle", "UserPromptSubmit": "working", "PreToolUse": "working", "PostToolUse": "working",
	"PermissionRequest": "waiting", "Stop": "idle", "StopFailure": "error", "SessionEnd": "ended",
	"BeforeAgent": "working", "AfterTool": "working", "AfterAgent": "idle", // Gemini CLI's names
}

func (s *srv) agentRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/agents", s.read(func(r *http.Request) (any, error) {
		return listAgents(r.Context(), s.db, r.URL.Query().Get("project"), min(max(intParam(r, "hours", 24), 1), 24*30))
	}))
	// Hooks post their event as is (`runyard hooks install` sets them up): ?tool= names the CLI. The answer is an empty
	// 204, because Claude Code reads a JSON body as the hook's decision; events it does not track are taken and ignored.
	mux.HandleFunc("POST /api/v1/agents/events", func(w http.ResponseWriter, r *http.Request) {
		if !s.keyOK(r) {
			httpError(w, http.StatusUnauthorized, "missing or invalid API key")
			return
		}
		var in struct {
			Session string `json:"session_id"`
			Event   string `json:"hook_event_name"`
			CWD     string `json:"cwd"`
			Type    string `json:"notification_type"`
			Said    string `json:"last_assistant_message"`
			Reply   string `json:"prompt_response"` // the same, from Gemini CLI
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 32<<20)).Decode(&in); err != nil { // PostToolUse carries the tool's whole output
			httpError(w, http.StatusBadRequest, err.Error())
			return
		}
		state := hookStates[in.Event]
		if in.Event == "Notification" {
			switch in.Type {
			case "permission_prompt", "elicitation_dialog", "agent_needs_input", "ToolPermission":
				state = "waiting"
			case "idle_prompt":
				state = "idle"
			}
		}
		if card := r.Header.Get("X-Runyard-Session"); card != "" { // Gemini CLI keeps its session id from runs, so its hooks name the card runs use
			in.Session = card
		}
		tool := r.URL.Query().Get("tool")
		if state == "" || in.Session == "" || len(in.Session) > 200 || !agentTool.MatchString(tool) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		// said is kept only for a session with a card: what an agent tells someone in an unrelated session stays out of the database.
		_, err := s.db.Exec(r.Context(), `INSERT INTO agent_sessions (id, tool, cwd, hook, hook_at, card) VALUES ($1, $2, $3, $4, now(), $6)
			ON CONFLICT (id) DO UPDATE SET hook = excluded.hook, hook_at = now(), seen_at = now(), card = agent_sessions.card OR excluded.card,
				cwd = CASE WHEN excluded.cwd <> '' THEN excluded.cwd ELSE agent_sessions.cwd END,
				said = CASE WHEN $5::text <> '' AND (excluded.card OR agent_sessions.card OR agent_sessions.report <> ''
						OR EXISTS (SELECT 1 FROM runs WHERE agent = agent_sessions.id))
					THEN $5::text ELSE agent_sessions.said END`,
			tool+":"+in.Session, tool, clip(in.CWD, 1000), state, clip(in.Said+in.Reply, 2000), r.URL.Query().Get("card") == "1")
		if err != nil {
			s.fail(w, err)
			return
		}
		s.hub.publish(event{Type: "agent"})
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("PATCH /api/v1/projects/{name...}", s.curate(func(r *http.Request) (any, error) {
		var in struct{ Metric, Goal string }
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		return struct{}{}, s.setMetric(r.Context(), r.PathValue("name"), in.Metric, in.Goal)
	}))
}

// setMetric sets the number a project is judged by; goal is min or max (default min).
func (s *srv) setMetric(ctx context.Context, project, metric, goal string) error {
	if goal == "" {
		goal = "min"
	}
	if goal != "min" && goal != "max" {
		return badRequest{errors.New("goal is min or max")}
	}
	err := exec(ctx, s.db, "project "+project, `UPDATE projects SET metric = $2, goal = $3
		WHERE name = $1 AND ($4::bigint[] IS NULL OR team_id IS NULL OR team_id = ANY($4))`, project, strings.TrimSpace(metric), goal, vis(ctx))
	if err == nil {
		s.hub.publish(event{Type: "agent"})
	}
	return err
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "") + "…"
}

// metaSession is the agent session a tool call names in its _meta: Codex sends x-codex-turn-metadata with every call,
// OpenCode ai.opencode/sessionID. Claude Code sends none.
func metaSession(m mcp.Meta) string {
	if t, ok := m["x-codex-turn-metadata"].(map[string]any); ok {
		if id, _ := t["session_id"].(string); id != "" {
			return "codex:" + id
		}
	}
	if id, _ := m["ai.opencode/sessionID"].(string); id != "" {
		return "opencode:" + id
	}
	return ""
}

// report stores what an agent says it did on its session's card. The session comes from the run's agent, else the
// call's _meta, else the session argument: "<tool>:<id>" (made if new) or a bare id of a session runyard already knows.
func (s *srv) report(ctx context.Context, req *mcp.CallToolRequest, text, next, runRef, session string) (any, error) {
	if err := s.mcpKey(ctx, req); err != nil {
		return nil, err
	}
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("text is empty: say what you tried and what came out of it")
	}
	var run *string
	agent := ""
	if runRef != "" {
		id, err := s.resolveRun(ctx, runRef)
		if err != nil {
			return nil, err
		}
		run = &id
		s.db.QueryRow(ctx, `SELECT coalesce(agent, '') FROM runs WHERE id = $1::uuid`, id).Scan(&agent)
	}
	if agent == "" {
		agent = metaSession(req.Params.Meta)
	}
	if agent == "" {
		agent = strings.TrimSpace(session)
	}
	if agent == "" {
		return nil, errors.New("runyard cannot tell which agent session this is: pass run (a run you started with the runyard SDK) " +
			"or session (Claude Code: claude:$CLAUDE_CODE_SESSION_ID)")
	}
	if tool, id, ok := strings.Cut(agent, ":"); ok && agentTool.MatchString(tool) && id != "" && len(agent) <= 240 {
		if _, err := s.db.Exec(ctx, `INSERT INTO agent_sessions (id, tool) VALUES ($1, $2) ON CONFLICT DO NOTHING`, agent, tool); err != nil {
			return nil, err
		}
	}
	var found string
	err := s.db.QueryRow(ctx, `UPDATE agent_sessions a SET report = $2, report_next = $3, report_run = $4::uuid, report_at = now(), seen_at = now()
		WHERE a.id = (SELECT id FROM agent_sessions WHERE id = $1 OR right(id, length($1) + 1) = ':' || $1 ORDER BY id = $1 DESC, seen_at DESC LIMIT 1)
		AND ($5::bigint[] IS NULL OR a.project_id IS NULL OR EXISTS (SELECT 1 FROM projects p WHERE p.id = a.project_id AND (p.team_id IS NULL OR p.team_id = ANY($5))))
		RETURNING a.id`, agent, clip(text, 4000), clip(next, 1000), run, vis(ctx)).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errors.New("no agent session " + agent + " yet: start a run with the runyard SDK first, or pass session as <tool>:<id>")
	}
	if err != nil {
		return nil, err
	}
	s.hub.publish(event{Type: "agent"})
	if run != nil { // the card keeps the last report; the run keeps every report about it, on its page
		note := clip(text, 4000)
		if next != "" {
			note += "\nNext: " + clip(next, 1000)
		}
		if err := s.addNote(ctx, *run, note); err != nil {
			return nil, err
		}
	}
	return map[string]any{"session": found, "run": run, "report": text}, nil
}

// notifyWaiting tells the channels that hear every project's alerts about an agent stuck at a permission prompt, once
// per wait. Only sessions with a card: a prompt in an unrelated session is no one's business here.
// shortcut: Claude Code only says a prompt was answered when the command exits, so a long approved command that is not a
// run also notifies; upgrade if its hooks ever report the answer, as OpenCode's events do.
func (s *srv) notifyWaiting(ctx context.Context) {
	rows, _ := s.db.Query(ctx, `UPDATE agent_sessions a SET told_at = now()
		WHERE hook = 'waiting' AND hook_at < now() - $1::interval AND hook_at > now() - $2::interval AND (told_at IS NULL OR told_at < hook_at)
			AND (card OR report <> '' OR EXISTS (SELECT 1 FROM runs WHERE agent = a.id))
		RETURNING tool, label, cwd`, waitNotice.String(), agentStale.String())
	waiting, err := pgx.CollectRows(rows, pgx.RowToStructByPos[struct{ Tool, Label, CWD string }])
	if err != nil {
		log.Print("waiting agents: ", err)
	}
	for _, a := range waiting {
		name := a.Label
		if name == "" && a.CWD != "" {
			name = path.Base(a.CWD)
		}
		if name == "" {
			name = a.Tool
		}
		m := message{Event: "agent", Title: name + ": Waiting for you", Body: a.Tool + " asked for a permission and is stuck until someone answers.", priority: "high", tags: "warning"}
		if s.url != "" {
			m.URL = s.url + "/#/kiosk"
		}
		go s.notifyAll(m)
	}
}
