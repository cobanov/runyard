// Command runyard-server is the runyard tracking server: REST API, web UI, SSE and MCP in one binary.
package main

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os"
)

//go:embed all:webdist
var webdist embed.FS

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func newMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true}`))
	})
	static, _ := fs.Sub(webdist, "webdist/dist")
	mux.Handle("/", http.FileServerFS(static))
	return mux
}

func main() {
	addr := env("ADDR", ":8080")
	log.Printf("runyard listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, newMux()))
}
