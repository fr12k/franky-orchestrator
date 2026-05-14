package server

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/franky/orchestrator/internal/events"
)

// handleBrowserSSE serves the multiplexed SSE stream to browser clients.
func (s *Server) handleBrowserSSE(w http.ResponseWriter, r *http.Request) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming_unsupported", "streaming not supported")
		return
	}

	// Create subscriber channel (buffered, 64)
	ch := make(chan *events.SseFrame, 64)
	s.broker.Subscribe(ch)
	defer s.broker.Unsubscribe(ch)

	// Send initial comment
	_, _ = fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	slog.Info("browser SSE client connected")

	// Event loop — blocks until client disconnects or server shuts down
	for {
		select {
		case <-r.Context().Done():
			slog.Info("browser SSE client disconnected")
			return
		case frame, ok := <-ch:
			if !ok {
				return
			}
			writeSSEFrame(w, frame)
			flusher.Flush()
		}
	}
}

// writeSSEFrame writes a single SSE frame to the response writer.
func writeSSEFrame(w http.ResponseWriter, frame *events.SseFrame) {
	if frame.ID != "" {
		_, _ = fmt.Fprintf(w, "id: %s\n", frame.ID)
	}
	if frame.Event != "" {
		_, _ = fmt.Fprintf(w, "event: %s\n", frame.Event)
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", frame.Data)
}
