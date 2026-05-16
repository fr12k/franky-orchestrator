package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"time"

	"github.com/franky/orchestrator/internal/agents"
	"github.com/franky/orchestrator/internal/registry"
)

// handleRegister handles POST /register — agent self-registration.
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read_body_error", "failed to read request body")
		return
	}
	defer func() { _ = r.Body.Close() }()

	var input struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		APIURL    string `json:"apiUrl"`
		Workspace string `json:"workspace"`
		Model     string `json:"model"`
		Role      string `json:"role"`
		PID       int    `json:"pid"`
	}

	if err := json.Unmarshal(body, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON body")
		return
	}

	if input.ID == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "agent id is required")
		return
	}
	if input.APIURL == "" {
		writeError(w, http.StatusBadRequest, "missing_api_url", "apiUrl is required")
		return
	}

	// Cancel any existing SSE consumer for this apiURL before registering.
	// This ensures a stale consumer from a previous session doesn't keep
	// publishing events with an old agentId after the agent re-registers
	// with a new ID at the same URL.
	s.cancelConsumer(input.APIURL)

	agent := &registry.Agent{
		ID:           input.ID,
		Name:         input.Name,
		APIURL:       input.APIURL,
		Workspace:    input.Workspace,
		Model:        input.Model,
		Role:         input.Role,
		PID:          input.PID,
		Status:       registry.StatusIdle,
		RegisteredAt: time.Now(),
		LastSeenAt:   time.Now(),
	}

	// Derive agent name from the last path component of workspace, so
	// agents are uniquely identifiable by their working directory
	// (e.g. /Users/frank/gh/franky-orchestrator → franky-orchestrator).
	if input.Workspace != "" {
		agent.Name = filepath.Base(input.Workspace)
	}

	if _, err := s.registry.Register(agent); err != nil {
		writeError(w, http.StatusInternalServerError, "registration_failed", err.Error())
		return
	}

	// Start SSE consumer for this agent in the background
	go s.startSSEConsumer(agent)

	// Publish orchestrator event
	s.broker.PublishOrchestratorEvent("agent_registered", map[string]any{
		"agent": agentToMap(agent),
	})

	slog.Info("agent registered",
		"id", agent.ID,
		"name", agent.Name,
		"apiUrl", agent.APIURL,
	)

	writeOK(w, map[string]any{
		"registeredAt": agent.RegisteredAt,
	})
}

// handleHeartbeat handles POST /heartbeat — agent heartbeat.
func (s *Server) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read_body_error", "failed to read request body")
		return
	}
	defer func() { _ = r.Body.Close() }()

	var input struct {
		ID string `json:"id"`
	}

	if err := json.Unmarshal(body, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON body")
		return
	}

	if input.ID == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "agent id is required")
		return
	}

	if _, ok := s.registry.Get(input.ID); !ok {
		writeError(w, http.StatusNotFound, "agent_not_found", "agent not found")
		return
	}

	s.registry.Touch(input.ID)
	writeOK(w, nil)
}

// handleUnregister handles POST /unregister — graceful agent removal.
func (s *Server) handleUnregister(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read_body_error", "failed to read request body")
		return
	}
	defer func() { _ = r.Body.Close() }()

	var input struct {
		ID string `json:"id"`
	}

	if err := json.Unmarshal(body, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON body")
		return
	}

	if input.ID == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "agent id is required")
		return
	}

	// Cancel the SSE consumer before unregistering
	if agent, ok := s.registry.Get(input.ID); ok {
		s.cancelConsumer(agent.APIURL)
	}

	if err := s.registry.Unregister(input.ID); err != nil {
		writeError(w, http.StatusNotFound, "agent_not_found", "agent not found")
		return
	}

	// Publish orchestrator event
	s.broker.PublishOrchestratorEvent("agent_unregistered", map[string]any{
		"agentId": input.ID,
	})

	slog.Info("agent unregistered", "id", input.ID)

	writeOK(w, nil)
}

// startSSEConsumer connects to the agent's SSE stream and feeds events to the broker.
func (s *Server) startSSEConsumer(agent *registry.Agent) {
	s.consumerMu.Lock()
	if s.consumerCancels == nil {
		s.consumerCancels = make(map[string]context.CancelFunc)
	}
	// Cancel any previous consumer for the same URL
	if prevCancel, ok := s.consumerCancels[agent.APIURL]; ok {
		prevCancel()
	}
	// Derive from the server's shutdown context so all consumers stop on shutdown.
	ctx, cancel := context.WithCancel(s.shutdownCtx)
	s.consumerCancels[agent.APIURL] = cancel
	s.consumerMu.Unlock()

	consumer := agents.NewSSEConsumer(
		ctx,
		agent.ID,
		agent.APIURL,
		s.broker,
		func(id string) { s.registry.Touch(id) },
		func(id string) bool {
			_, ok := s.registry.Get(id)
			return ok
		},
	)

	// The consumer runs in a loop with reconnect; it exits when ctx is cancelled
	consumer.Run()
}

// ReconnectToStoredAgents starts SSE consumers for all agents in the registry.
// This is called after the orchestrator restarts to reconnect to previously
// registered agents using their stored API URLs.
func (s *Server) ReconnectToStoredAgents() {
	list := s.registry.List()
	if len(list) == 0 {
		return
	}

	slog.Info("reconnecting to stored agents", "count", len(list))
	for _, a := range list {
		slog.Info("starting SSE consumer for stored agent", "agentId", a.ID, "apiUrl", a.APIURL, "name", a.Name)
		go s.startSSEConsumer(a)
	}
}
