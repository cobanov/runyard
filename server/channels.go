package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const channelUsage = `usage:
  runyard-server channel add [-token T] [-project P] [-events finished,failed,crashed,alert] KIND TARGET
    ntfy URL             server URL with topic; -token defaults to $NTFY_TOKEN
    webhook URL          JSON POST of every event
    slack URL            an incoming webhook URL
    discord URL          a channel webhook URL
    telegram CHAT_ID     -token defaults to $TELEGRAM_TOKEN (the bot token)
    (tokens from the environment stay out of the process list)
  runyard-server channel ls
  runyard-server channel rm ID
  runyard-server channel test ID`

// channelCmd manages notification channels and the rules that route run events to them, as the web UI's
// Settings page does through the API (admin.go); both call the functions below.
func channelCmd(ctx context.Context, db *pgxpool.Pool, args []string) error {
	if len(args) == 0 {
		return errors.New(channelUsage)
	}
	switch args[0] {
	case "add":
		fs := flag.NewFlagSet("add", flag.ContinueOnError)
		token := fs.String("token", "", "ntfy access token or telegram bot token")
		project := fs.String("project", "", "only this project (default: every project)")
		events := fs.String("events", "finished,failed,crashed,alert", "events to send")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		kind := fs.Arg(0)
		if fs.NArg() != 2 || !slices.Contains(channelKinds, kind) {
			return errors.New(channelUsage)
		}
		if *token == "" {
			*token = os.Getenv(map[string]string{"ntfy": "NTFY_TOKEN", "telegram": "TELEGRAM_TOKEN"}[kind])
		}
		id, err := addChannel(ctx, db, kind, fs.Arg(1), *token, *project, strings.Split(*events, ","))
		if err != nil {
			return err
		}
		fmt.Println("added channel", id)
	case "ls":
		cs, err := listChannels(ctx, db)
		for _, c := range cs {
			fmt.Printf("%d\t%s\t%s\tproject=%s\tevents=%s\n", c.ID, c.Kind, c.Target, or2(c.Project, "*"), strings.Join(c.Events, ","))
		}
		return err
	case "rm":
		if len(args) != 2 {
			return errors.New(channelUsage)
		}
		return removeChannel(ctx, db, args[1])
	case "test":
		if len(args) != 2 {
			return errors.New(channelUsage)
		}
		if err := testChannel(ctx, db, args[1]); err != nil {
			return err
		}
		fmt.Println("sent")
	default:
		return errors.New(channelUsage)
	}
	return nil
}

var channelKinds = []string{"ntfy", "webhook", "slack", "discord", "telegram"}

// addChannel stores a channel and its rules. target is the URL, or the chat id for telegram; project "" means
// every project (a named one is made if missing); no events means all four.
func addChannel(ctx context.Context, db *pgxpool.Pool, kind, target, token, project string, events []string) (int64, error) {
	if !slices.Contains(channelKinds, kind) || target == "" {
		return 0, badRequest{errors.New("a channel needs a kind (ntfy, webhook, slack, discord, telegram) and a URL, or a chat id for telegram")}
	}
	c := channelConfig{URL: target, Token: token}
	if kind == "telegram" {
		if token == "" {
			return 0, badRequest{errors.New("telegram needs the bot token")}
		}
		c = channelConfig{Token: token, Chat: target}
	}
	if len(events) == 0 {
		events = []string{"finished", "failed", "crashed", "alert"}
	}
	cfg, _ := json.Marshal(c)
	var id int64
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO channels (kind, config) VALUES ($1, $2) RETURNING id`, kind, cfg).Scan(&id); err != nil {
			return err
		}
		var pid *int64
		if project != "" {
			pid = new(int64)
			if err := tx.QueryRow(ctx, `INSERT INTO projects (name) VALUES ($1) ON CONFLICT (name) DO UPDATE SET name = excluded.name RETURNING id`, project).Scan(pid); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `INSERT INTO rules (project_id, event, channel_id) SELECT $1, unnest($2::text[]), $3`, pid, events, id)
		return err
	})
	return id, err
}

// channelInfo is a channel as listed: never its token.
type channelInfo struct {
	ID      int64    `json:"id"`
	Kind    string   `json:"kind"`
	Target  string   `json:"target"`  // the URL (a push endpoint for webpush), or the telegram chat id
	Project string   `json:"project"` // "" for every project
	Events  []string `json:"events"`
}

func listChannels(ctx context.Context, db *pgxpool.Pool) ([]channelInfo, error) {
	rows, _ := db.Query(ctx, `SELECT c.id, c.kind, coalesce(nullif(c.config->>'chat', ''), c.config->>'url', ''), coalesce(p.name, ''),
			coalesce(array_agg(r.event ORDER BY r.event) FILTER (WHERE r.event IS NOT NULL), '{}')
		FROM channels c LEFT JOIN rules r ON r.channel_id = c.id LEFT JOIN projects p ON p.id = r.project_id
		GROUP BY 1, 2, 3, 4 ORDER BY 1`)
	return pgx.CollectRows(rows, pgx.RowToStructByPos[channelInfo])
}

func removeChannel(ctx context.Context, db *pgxpool.Pool, id string) error {
	return exec(ctx, db, "channel", `DELETE FROM channels WHERE id = $1::bigint`, id)
}

// testChannel sends a test message; a channel that refuses it is a bad request, not a server error.
func testChannel(ctx context.Context, db *pgxpool.Pool, id string) error {
	var c channel
	err := db.QueryRow(ctx, `SELECT id, kind, config FROM channels WHERE id = $1::bigint`, id).Scan(&c.ID, &c.Kind, &c.Config)
	if errors.Is(err, pgx.ErrNoRows) {
		return noSuch("channel")
	} else if err != nil {
		return err
	}
	host, _ := os.Hostname()
	m := message{Event: "test", Title: "runyard: Test", Body: "Notifications from " + host + " reach this channel.", priority: "default", tags: "bell",
		Run: Run{Project: "test", Name: "test", StartedAt: time.Now()}}
	if err := c.send(ctx, m); err != nil {
		return badRequest{err}
	}
	return nil
}
