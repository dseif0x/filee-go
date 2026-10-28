package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// events streams the app's events (job progress and state, settings,
// library, history, watch folders, engines) as Server-Sent Events.
func (s *server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	ch, stop := s.a.Events(512)
	defer stop()
	tools, stopTools := toolsOf(s.a).subscribe()
	defer stopTools()
	fmt.Fprint(w, "retry: 1500\n: hello\n\n")
	flusher.Flush()
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
		case e, ok := <-ch:
			if !ok {
				return
			}
			b, _ := json.Marshal(e)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Kind, b)
		case t := <-tools:
			b, _ := json.Marshal(t)
			fmt.Fprintf(w, "event: tool.updated\ndata: %s\n\n", b)
		}
		flusher.Flush()
	}
}
