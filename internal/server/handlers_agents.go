package server

import (
	"log/slog"
	"net/http"

	"github.com/franky/orchestrator/internal/registry"
)

// handleListAgents returns all registered agents.
func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	agents := s.registry.List()

	// Convert to a flat list of maps for clean JSON output
	result := make([]map[string]any, 0, len(agents))
	for _, a := range agents {
		result = append(result, agentToMap(a))
	}

	writeOK(w, map[string]any{
		"agents": result,
	})
}

// handleGetAgent returns a single agent by ID.
func (s *Server) handleGetAgent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "agent id is required")
		return
	}

	agent, ok := s.registry.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "agent_not_found", "agent not found")
		return
	}

	writeOK(w, agentToMap(agent))
}

// handleDeleteAgent handles DELETE /agents/{id} — removes an agent from the registry.
func (s *Server) handleDeleteAgent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "agent id is required")
		return
	}

	if err := s.registry.Unregister(id); err != nil {
		writeError(w, http.StatusNotFound, "agent_not_found", "agent not found")
		return
	}

	// Publish orchestrator event
	s.broker.PublishOrchestratorEvent("agent_unregistered", map[string]any{
		"agentId": id,
	})

	slog.Info("agent deleted via browser API", "id", id)

	writeOK(w, nil)
}

// agentToMap converts an Agent to a map for JSON serialization.
func agentToMap(a *registry.Agent) map[string]any {
	m := map[string]any{
		"id":           a.ID,
		"name":         a.Name,
		"apiUrl":       a.APIURL,
		"workspace":    a.Workspace,
		"model":        a.Model,
		"role":         a.Role,
		"pid":          a.PID,
		"status":       a.Status,
		"registeredAt": a.RegisteredAt,
		"lastSeenAt":   a.LastSeenAt,
	}
	if a.MessageCount != 0 {
		m["messageCount"] = a.MessageCount
		m["stats"] = map[string]any{"messages": a.MessageCount}
	}
	if a.TurnCount != 0 {
		m["turnCount"] = a.TurnCount
	}
	if a.TokensIn != 0 || a.TokensOut != 0 {
		m["tokensIn"] = a.TokensIn
		m["tokensOut"] = a.TokensOut
	}
	if len(a.ToolStats) > 0 {
		m["toolStats"] = a.ToolStats
	}
	if a.ErrorMessage != "" {
		m["errorMessage"] = a.ErrorMessage
	}
	return m
}
