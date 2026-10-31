package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const channelUsage = `usage:
  runyard-server channel add [-token T] [-project P] [-events finished,failed,crashed,alert] ntfy|webhook URL
  runyard-server channel ls
  runyard-server channel rm ID
  runyard-server channel test ID`

// channelCmd manages notification channels and the rules that route run events to them.
func channelCmd(ctx context.Context, db *pgxpool.Pool, args []string) error {
	if len(args) == 0 {
		return errors.New(channelUsage)
	}
	switch args[0] {
	case "add":
		fs := flag.NewFlagSet("add", flag.ContinueOnError)
		token := fs.String("token", "", "ntfy access token")
		project := fs.String("project", "", "only this project (default: every project)")
		events := fs.String("events", "finished,failed,crashed,alert", "events to send")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 2 || (fs.Arg(0) != "ntfy" && fs.Arg(0) != "webhook") {
			return errors.New(channelUsage)
		}
		cfg, _ := json.Marshal(channelConfig{URL: fs.Arg(1), Token: *token})
		var id int64
		err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
			if err := tx.QueryRow(ctx, `INSERT INTO channels (kind, config) VALUES ($1, $2) RETURNING id`, fs.Arg(0), cfg).Scan(&id); err != nil {
				return err
			}
			var pid *int64
			if *project != "" {
				pid = new(int64)
				if err := tx.QueryRow(ctx, `INSERT INTO projects (name) VALUES ($1) ON CONFLICT (name) DO UPDATE SET name = excluded.name RETURNING id`, *project).Scan(pid); err != nil {
					return err
				}
			}
			_, err := tx.Exec(ctx, `INSERT INTO rules (project_id, event, channel_id) SELECT $1, unnest($2::text[]), $3`, pid, strings.Split(*events, ","), id)
			return err
		})
		if err != nil {
			return err
		}
		fmt.Println("added channel", id)
	case "ls":
		rows, _ := db.Query(ctx, `SELECT c.id, c.kind, c.config->>'url', coalesce(p.name, '*'), string_agg(r.event, ',' ORDER BY r.event)
			FROM channels c LEFT JOIN rules r ON r.channel_id = c.id LEFT JOIN projects p ON p.id = r.project_id
			GROUP BY 1, 2, 3, 4 ORDER BY 1`)
		var id int64
		var kind, u, project string
		var events *string
		_, err := pgx.ForEachRow(rows, []any{&id, &kind, &u, &project, &events}, func() error {
			fmt.Printf("%d\t%s\t%s\tproject=%s\tevents=%s\n", id, kind, u, project, deref(events))
			return nil
		})
		return err
	case "rm":
		if len(args) != 2 {
			return errors.New(channelUsage)
		}
		_, err := db.Exec(ctx, `DELETE FROM channels WHERE id = $1::bigint`, args[1])
		return err
	case "test":
		if len(args) != 2 {
			return errors.New(channelUsage)
		}
		var c channel
		if err := db.QueryRow(ctx, `SELECT id, kind, config FROM channels WHERE id = $1::bigint`, args[1]).Scan(&c.ID, &c.Kind, &c.Config); err != nil {
			return err
		}
		host, _ := os.Hostname()
		m := message{Event: "test", Title: "runyard test", Body: "Notifications from " + host + " reach this channel.", priority: "default", tags: "bell",
			Run: Run{Project: "test", Name: "test", StartedAt: time.Now()}}
		if err := c.send(ctx, m); err != nil {
			return err
		}
		fmt.Println("sent")
	default:
		return errors.New(channelUsage)
	}
	return nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
