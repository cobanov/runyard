// Command runyard-server is the runyard tracking server: REST API, web UI, SSE and MCP in one binary.
//
//	runyard-server                  serve (default)
//	runyard-server key create [-user U] NAME  print a new write API key (acting as user U)
//	runyard-server channel ...      manage notification channels, see channelCmd
//	runyard-server check ...        manage the checks that raise alerts (NaN, threshold, stall), see checkCmd
//	runyard-server user|team|project ...  users, teams and which team owns a project (RUNYARD_AUTH=1), see usersUsage
package main

import (
	"context"
	"embed"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed all:webdist
var webdist embed.FS

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

var cmds = map[string]func(context.Context, *pgxpool.Pool, []string) error{
	"key": keyCmd, "channel": channelCmd, "check": checkCmd, "user": userCmd, "team": teamCmd, "project": projectCmd,
}

func main() {
	ctx := context.Background()
	db, err := pgxpool.New(ctx, env("DATABASE_URL", "postgres://runyard:runyard@localhost:5432/runyard?sslmode=disable"))
	if err != nil {
		log.Fatal(err)
	}
	if err := waitDB(ctx, db); err != nil {
		log.Fatal(err)
	}
	if err := migrate(ctx, db); err != nil {
		log.Fatal("migrate: ", err)
	}
	if err := loadVAPID(ctx, db); err != nil {
		log.Fatal(err)
	}

	args := os.Args[1:]
	switch {
	case len(args) == 0 || args[0] == "serve":
	case cmds[args[0]] != nil:
		if err := cmds[args[0]](ctx, db, args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		return
	default:
		fmt.Fprintln(os.Stderr, "usage: runyard-server [serve] | key create [-user U] NAME | channel add|ls|rm|test | check add|ls|rm | user | team | project")
		os.Exit(2)
	}

	store, err := newBlobs()
	if err != nil {
		log.Fatal(err)
	}
	auth := os.Getenv("RUNYARD_AUTH")
	s := &srv{db: db, store: store, url: strings.TrimRight(os.Getenv("RUNYARD_URL"), "/"), hub: newHub(), auth: auth != "" && auth != "0" && auth != "false"}
	go s.watchCrashes(ctx)

	addr := env("ADDR", ":8080")
	log.Printf("runyard listening on %s", addr)
	hs := &http.Server{Addr: addr, Handler: s.routes(), ReadHeaderTimeout: 10 * time.Second} // no WriteTimeout: SSE streams stay open
	log.Fatal(hs.ListenAndServe())
}

// waitDB retries the first connection for a while; under docker compose Postgres often starts after us.
func waitDB(ctx context.Context, db *pgxpool.Pool) (err error) {
	for i := 0; i < 30; i++ {
		if err = db.Ping(ctx); err == nil {
			return nil
		}
		time.Sleep(time.Second)
	}
	return err
}
