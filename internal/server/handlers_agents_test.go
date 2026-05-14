package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/franky/orchestrator/internal/registry"
)

func TestHandleListAgents(t *testing.T) {
	t.Parallel()

	t.Run("empty registry", func(t *testing.T) {
		srv := newTestServer(t)
		req := httptest.NewRequest(http.MethodGet, "/agents", nil)
		w := httptest.NewRecorder()

		srv.handleListAgents(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", w.Code)
		}

		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		agents, ok := body["agents"]
		if !ok {
			t.Fatal("expected 'agents' key")
		}
		agentsSlice, ok := agents.([]any)
		if !ok {
			t.Fatalf("expected agents to be array, got %T", agents)
		}
		if len(agentsSlice) != 0 {
			t.Errorf("expected empty array, got %d items", len(agentsSlice))
		}
	})

	t.Run("with agents", func(t *testing.T) {
		srv := newTestServer(t)

		// Register agents directly in the registry
		a1 := &registry.Agent{
			ID:           "agent-1",
			Name:         "Agent One",
			APIURL:       "http://one:8080",
			Workspace:    "/ws/one",
			Model:        "gpt-4",
			Role:         "coder",
			PID:          100,
			Status:       registry.StatusIdle,
			RegisteredAt: time.Now(),
			LastSeenAt:   time.Now(),
		}
		a2 := &registry.Agent{
			ID:           "agent-2",
			Name:         "Agent Two",
			APIURL:       "http://two:8080",
			Workspace:    "/ws/two",
			Model:        "claude",
			Role:         "reviewer",
			PID:          200,
			Status:       registry.StatusStreaming,
			RegisteredAt: time.Now(),
			LastSeenAt:   time.Now(),
		}
		if _, err := srv.registry.Register(a1); err != nil {
			t.Fatalf("register a1: %v", err)
		}
		if _, err := srv.registry.Register(a2); err != nil {
			t.Fatalf("register a2: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/agents", nil)
		w := httptest.NewRecorder()

		srv.handleListAgents(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", w.Code)
		}

		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		agentsSlice, ok := body["agents"].([]any)
		if !ok {
			t.Fatalf("expected agents to be array, got %T", body["agents"])
		}
		if len(agentsSlice) != 2 {
			t.Fatalf("expected 2 agents, got %d", len(agentsSlice))
		}

		// Check fields of first agent
		a := agentsSlice[0].(map[string]any)
		if a["id"] == nil {
			t.Error("expected id field")
		}
		if a["name"] == nil {
			t.Error("expected name field")
		}
		if a["apiUrl"] == nil {
			t.Error("expected apiUrl field")
		}
		if a["status"] == nil {
			t.Error("expected status field")
		}
	})
}

func TestHandleGetAgent(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)

	a := &registry.Agent{
		ID:           "agent-1",
		Name:         "Agent One",
		APIURL:       "http://one:8080",
		Status:       registry.StatusIdle,
		RegisteredAt: time.Now(),
		LastSeenAt:   time.Now(),
	}
	if _, err := srv.registry.Register(a); err != nil {
		t.Fatalf("register: %v", err)
	}

	t.Run("existing agent", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/agents/agent-1", nil)
		req.SetPathValue("id", "agent-1")
		w := httptest.NewRecorder()

		srv.handleGetAgent(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", w.Code)
		}

		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if body["id"] != "agent-1" {
			t.Errorf("expected id agent-1, got %v", body["id"])
		}
		if body["name"] != "Agent One" {
			t.Errorf("expected name 'Agent One', got %v", body["name"])
		}
	})

	t.Run("non-existent agent", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/agents/nonexistent", nil)
		req.SetPathValue("id", "nonexistent")
		w := httptest.NewRecorder()

		srv.handleGetAgent(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("expected status 404, got %d", w.Code)
		}
	})

	t.Run("empty id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/agents/", nil)
		req.SetPathValue("id", "")
		w := httptest.NewRecorder()

		srv.handleGetAgent(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})
}
