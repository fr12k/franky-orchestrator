package server

import (
	"net/http"

	"github.com/franky/orchestrator/internal/ui"
)

// registerRoutes sets up all HTTP routes using Go 1.22+ method patterns.
func (s *Server) registerRoutes(mux *http.ServeMux) {
	// Orchestrator self
	mux.HandleFunc("GET /health", s.handleHealth)

	// Agent registry (agents talk to these)
	mux.HandleFunc("POST /register", s.handleRegister)
	mux.HandleFunc("POST /heartbeat", s.handleHeartbeat)
	mux.HandleFunc("POST /unregister", s.handleUnregister)

	// Browser API (read-only + actions)
	mux.HandleFunc("GET /agents", s.handleListAgents)
	mux.HandleFunc("POST /agents", s.handleAddAgent)
	mux.HandleFunc("GET /agents/{id}", s.handleGetAgent)
	mux.HandleFunc("PATCH /agents/{id}", s.handleUpdateAgent)
	mux.HandleFunc("DELETE /agents/{id}", s.handleDeleteAgent)

	// Proxy endpoints to agent APIs
	mux.HandleFunc("GET /agents/{id}/transcript", s.handleProxyTranscript)
	mux.HandleFunc("GET /agents/{id}/role", s.handleProxyRole)
	mux.HandleFunc("GET /agents/{id}/usage", s.handleProxyUsage)
	mux.HandleFunc("GET /agents/{id}/session", s.handleProxySession)
	mux.HandleFunc("GET /agents/{id}/sessions", s.handleProxySessions)
	mux.HandleFunc("GET /agents/{id}/design-docs", s.handleProxyDesignDocs)
	mux.HandleFunc("GET /agents/{id}/health", s.handleProxyHealth)
	mux.HandleFunc("POST /agents/{id}/interrupt", s.handleProxyInterrupt)
	mux.HandleFunc("POST /agents/{id}/restart", s.handleProxyRestart)
	mux.HandleFunc("POST /agents/{id}/command", s.handleProxyCommand)

	// SSE (browser multiplexing)
	mux.HandleFunc("GET /events", s.handleBrowserSSE)

	// Static UI (catch-all — registered last)
	mux.HandleFunc("/", ui.ServeHTTP)
}
