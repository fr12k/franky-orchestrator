package server

import (
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/franky/orchestrator/internal/registry"
)

// proxyToAgent forwards a request to an agent's API and returns the response.
func (s *Server) proxyToAgent(w http.ResponseWriter, r *http.Request, id, method, path string) {
	agent, ok := s.registry.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "agent_not_found", "agent not found")
		return
	}

	if agent.Status == registry.StatusOffline {
		writeError(w, http.StatusBadGateway, "agent_offline", "agent is offline")
		return
	}

	targetURL := strings.TrimRight(agent.APIURL, "/") + path

	// Read request body (limited to 1MB)
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read_body_error", "failed to read request body")
		return
	}
	defer func() { _ = r.Body.Close() }()

	// Build proxy request
	proxyReq, err := http.NewRequestWithContext(r.Context(), method, targetURL, strings.NewReader(string(body)))
	if err != nil {
		slog.Error("proxy request build error", "err", err, "targetURL", targetURL)
		writeError(w, http.StatusInternalServerError, "proxy_error", "failed to build proxy request")
		return
	}

	// Copy Content-Type header if present
	if ct := r.Header.Get("Content-Type"); ct != "" {
		proxyReq.Header.Set("Content-Type", ct)
	}
	// Copy Accept header if present
	if accept := r.Header.Get("Accept"); accept != "" {
		proxyReq.Header.Set("Accept", accept)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(proxyReq)
	if err != nil {
		slog.Error("agent unreachable", "err", err, "agentId", id, "apiUrl", agent.APIURL)
		writeError(w, http.StatusBadGateway, "agent_unreachable", "cannot connect to agent: "+err.Error())
		return
	}
	defer func() { _ = resp.Body.Close() }()

	// Read response body (limited to 4MB)
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		writeError(w, http.StatusBadGateway, "read_response_error", "failed to read agent response")
		return
	}

	// Determine content type from response
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json"
	}

	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(resp.StatusCode)
	if _, err := w.Write(respBody); err != nil {
		slog.Error("proxy response write error", "err", err, "agentId", id)
	}

	// Touch agent on successful proxy (proves connectivity)
	if resp.StatusCode < 500 {
		s.registry.Touch(id)
	}
}

// handleProxyTranscript proxies GET /agents/{id}/transcript.
func (s *Server) handleProxyTranscript(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.proxyToAgent(w, r, id, "GET", "/transcript")
}

// handleProxyRole proxies GET /agents/{id}/role.
func (s *Server) handleProxyRole(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.proxyToAgent(w, r, id, "GET", "/role")
}

// handleProxyUsage proxies GET /agents/{id}/usage.
func (s *Server) handleProxyUsage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.proxyToAgent(w, r, id, "GET", "/usage")
}

// handleProxyInterrupt proxies POST /agents/{id}/interrupt.
func (s *Server) handleProxyInterrupt(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.proxyToAgent(w, r, id, "POST", "/interrupt")
}

// handleProxyRestart proxies POST /agents/{id}/restart.
func (s *Server) handleProxyRestart(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.proxyToAgent(w, r, id, "POST", "/restart")
}

// handleProxyCommand proxies POST /agents/{id}/command (not routed in v0, but here for completeness).
func (s *Server) handleProxyCommand(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.proxyToAgent(w, r, id, "POST", "/command")
}

// handleProxySession proxies GET /agents/{id}/session.
func (s *Server) handleProxySession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.proxyToAgent(w, r, id, "GET", "/session")
}

// handleProxySessions proxies GET /agents/{id}/sessions.
func (s *Server) handleProxySessions(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.proxyToAgent(w, r, id, "GET", "/sessions")
}

// handleProxyDesignDocs proxies GET /agents/{id}/design-docs.
func (s *Server) handleProxyDesignDocs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.proxyToAgent(w, r, id, "GET", "/design-docs")
}

// handleProxyHealth proxies GET /agents/{id}/health directly to the agent.
func (s *Server) handleProxyHealth(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.proxyToAgent(w, r, id, "GET", "/health")
}


