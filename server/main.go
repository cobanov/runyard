// Command runyard-server is the runyard tracking server: REST API, web UI, SSE and MCP in one binary.
//
//	runyard-server                  serve (default)
//	runyard-server key create NAME  print a new write API key
//	runyard-server channel ...      manage notification channels, see channelCmd
//	runyard-server check ...        manage the checks that raise alerts (NaN, threshold, stall), see checkCmd
package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
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
	case len(args) == 3 && args[0] == "key" && args[1] == "create":
		key, err := createKey(ctx, db, args[2])
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(key)
		return
	case args[0] == "channel" || args[0] == "check":
		cmd := channelCmd
		if args[0] == "check" {
			cmd = checkCmd
		}
		if err := cmd(ctx, db, args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		return
	default:
		fmt.Fprintln(os.Stderr, "usage: runyard-server [serve] | key create NAME | channel add|ls|rm|test | check add|ls|rm")
		os.Exit(2)
	}

	store, err := newBlobs()
	if err != nil {
		log.Fatal(err)
	}
	s := &srv{db: db, store: store, url: strings.TrimRight(os.Getenv("RUNYARD_URL"), "/"), hub: newHub()}
	go s.watchCrashes(ctx)
	mux := s.routes()
	static, _ := fs.Sub(webdist, "webdist/dist")
	mux.Handle("/", http.FileServerFS(static))

	addr := env("ADDR", ":8080")
	log.Printf("runyard listening on %s", addr)
	hs := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second} // no WriteTimeout: SSE streams stay open
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
