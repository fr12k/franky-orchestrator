package server

import (
	"net/http"
)

// handleHealth responds with a simple health check.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeOK(w, nil)
}
