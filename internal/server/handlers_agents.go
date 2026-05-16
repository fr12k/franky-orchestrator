package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

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

	agent, ok := s.registry.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "agent_not_found", "agent not found")
		return
	}

	// Cancel the SSE consumer before unregistering
	s.cancelConsumer(agent.APIURL)

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

// handleAddAgent handles POST /agents — manually add an agent by name and host.
func (s *Server) handleAddAgent(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read_body_error", "failed to read request body")
		return
	}
	defer func() { _ = r.Body.Close() }()

	var input struct {
		Name string `json:"name"`
		Host string `json:"host"`
	}

	if err := json.Unmarshal(body, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON body")
		return
	}

	if input.Name == "" {
		writeError(w, http.StatusBadRequest, "missing_name", "name is required")
		return
	}
	if input.Host == "" {
		writeError(w, http.StatusBadRequest, "missing_host", "host is required")
		return
	}

	// Normalize host into an API URL
	apiURL := input.Host
	if !strings.HasPrefix(apiURL, "http://") && !strings.HasPrefix(apiURL, "https://") {
		apiURL = "http://" + apiURL
	}
	apiURL = strings.TrimRight(apiURL, "/")

	// Generate a short unique ID from the name
	baseID := slugify(input.Name)
	id := baseID
	// Ensure ID uniqueness by appending a counter suffix if the slug collides
	for i := 2; ; i++ {
		if _, exists := s.registry.Get(id); !exists {
			break
		}
		id = fmt.Sprintf("%s-%d", baseID, i)
	}

	agent := &registry.Agent{
		ID:           id,
		Name:         input.Name,
		APIURL:       apiURL,
		Status:       registry.StatusIdle,
		RegisteredAt: time.Now(),
		LastSeenAt:   time.Now(),
	}

	if _, err := s.registry.Register(agent); err != nil {
		writeError(w, http.StatusInternalServerError, "registration_failed", err.Error())
		return
	}

	// Start SSE consumer for this agent in the background
	go s.startSSEConsumer(agent)

	// Try to enrich agent metadata from the agent's /role endpoint
	go s.enrichAgentFromRole(agent.ID)

	// Publish orchestrator event
	s.broker.PublishOrchestratorEvent("agent_registered", map[string]any{
		"agent": agentToMap(agent),
	})

	slog.Info("agent added manually", "id", id, "name", input.Name, "apiUrl", apiURL)

	writeOK(w, agentToMap(agent))
}

// handleUpdateAgent handles PATCH /agents/{id} — updates agent metadata.
func (s *Server) handleUpdateAgent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "agent id is required")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read_body_error", "failed to read request body")
		return
	}
	defer func() { _ = r.Body.Close() }()

	var input struct {
		Name      *string `json:"name"`
		Workspace *string `json:"workspace"`
		Model     *string `json:"model"`
		Role      *string `json:"role"`
		Status    *string `json:"status"`
	}

	if err := json.Unmarshal(body, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON body")
		return
	}

	// Apply all non-nil updates atomically under the write lock.
	updated := s.registry.UpdateAgent(id, func(a *registry.Agent) bool {
		if input.Name != nil {
			a.Name = *input.Name
		}
		if input.Workspace != nil {
			a.Workspace = *input.Workspace
		}
		if input.Model != nil {
			a.Model = *input.Model
		}
		if input.Role != nil {
			a.Role = *input.Role
		}
		if input.Status != nil {
			a.Status = registry.Status(*input.Status)
		}
		return input.Name != nil || input.Workspace != nil || input.Model != nil || input.Role != nil || input.Status != nil
	})

	if !updated {
		writeError(w, http.StatusNotFound, "agent_not_found", "agent not found")
		return
	}

	// Persist the changes
	if persistErr := s.registry.PersistAll(); persistErr != nil {
		slog.Error("failed to persist after agent update", "id", id, "err", persistErr)
	}

	// Re-fetch a safe copy for the response
	agent, _ := s.registry.Get(id)

	// Publish update event
	s.broker.PublishOrchestratorEvent("agent_updated", map[string]any{
		"agent": agentToMap(agent),
	})

	slog.Info("agent updated", "id", id, "name", agent.Name, "model", agent.Model)

	writeOK(w, agentToMap(agent))
}

// enrichAgentFromRole fetches /role from the agent and updates the registry.
func (s *Server) enrichAgentFromRole(agentID string) {
	// Read the agent's APIURL from a safe copy (Get returns a copy)
	agent, ok := s.registry.Get(agentID)
	if !ok {
		return
	}

	body, err := s.agentClient.GetRole(agent.APIURL)
	if err != nil {
		slog.Debug("enrichAgentFromRole: cannot fetch /role", "agentId", agentID, "err", err)
		return
	}

	var roleData struct {
		Model     string `json:"model"`
		Workspace string `json:"workspace"`
		Role      string `json:"role"`
	}

	if err := json.Unmarshal(body, &roleData); err != nil {
		slog.Debug("enrichAgentFromRole: parse error", "agentId", agentID, "err", err)
		return
	}

	// Short-circuit early if there's nothing to enrich
	if (roleData.Model == "" || agent.Model != "") &&
		(roleData.Workspace == "" || agent.Workspace != "") &&
		(roleData.Role == "" || agent.Role != "") {
		return
	}

	// Atomically update only empty fields under the write lock
	s.registry.UpdateAgent(agentID, func(a *registry.Agent) bool {
		changed := false
		if roleData.Model != "" && a.Model == "" {
			a.Model = roleData.Model
			changed = true
		}
		if roleData.Workspace != "" && a.Workspace == "" {
			a.Workspace = roleData.Workspace
			changed = true
		}
		if roleData.Role != "" && a.Role == "" {
			a.Role = roleData.Role
			changed = true
		}
		return changed
	})

	// Re-fetch a safe copy for publishing
	agent, _ = s.registry.Get(agentID)

	if agent.Model != "" || agent.Workspace != "" || agent.Role != "" {
		if err := s.registry.PersistAll(); err != nil {
			slog.Error("enrichAgentFromRole: persist error", "agentId", agentID, "err", err)
		}
		s.broker.PublishOrchestratorEvent("agent_updated", map[string]any{
			"agent": agentToMap(agent),
		})
		slog.Info("enriched agent metadata from /role", "agentId", agentID, "model", agent.Model, "workspace", agent.Workspace)
	}
}

// slugify converts a name into a simple alphanumeric ID.
func slugify(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else if r == ' ' {
			b.WriteRune('-')
		}
	}
	id := b.String()
	if id == "" {
		id = "agent"
	}
	return id
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
		"messageCount": a.MessageCount,
		"turnCount":    a.TurnCount,
		"tokensIn":     a.TokensIn,
		"tokensOut":    a.TokensOut,
		"stats":        map[string]any{"messages": a.MessageCount},
	}
	if a.ToolStats != nil {
		m["toolStats"] = a.ToolStats
	} else {
		m["toolStats"] = map[string]int{}
	}
	if a.ErrorMessage != "" {
		m["errorMessage"] = a.ErrorMessage
	}
	return m
}
