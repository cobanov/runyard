package main

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// event tells SSE clients what changed; they refetch what they show.
type event struct {
	Type string `json:"type"` // run | metrics | logs | media | alert
	Run  string `json:"run,omitempty"`
}

type hub struct {
	mu   sync.Mutex
	subs map[chan []byte]struct{}
}

func newHub() *hub { return &hub{subs: map[chan []byte]struct{}{}} }

func (h *hub) publish(e event) {
	b, _ := json.Marshal(e)
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.subs {
		select {
		case c <- b:
		default: // a slow client misses events and catches up on its next refetch
		}
	}
}

func (h *hub) serve(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	c := make(chan []byte, 256)
	h.mu.Lock()
	h.subs[c] = struct{}{}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.subs, c)
		h.mu.Unlock()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Write([]byte(": hello\n\n"))
	flusher.Flush()
	ping := time.NewTicker(20 * time.Second) // keeps proxies from closing an idle stream
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case b := <-c:
			w.Write([]byte("data: "))
			w.Write(b)
			w.Write([]byte("\n\n"))
		case <-ping.C:
			w.Write([]byte(": ping\n\n"))
		}
		flusher.Flush()
	}
}
