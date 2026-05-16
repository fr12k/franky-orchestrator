package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/franky/orchestrator/internal/registry"
)

func TestHandleRegister(t *testing.T) {
	t.Parallel()

	t.Run("valid registration", func(t *testing.T) {
		srv := newTestServer(t)
		body := `{"id":"agent-1","name":"Test","apiUrl":"http://agent:8080","workspace":"/ws","model":"gpt-4","role":"coder","pid":1234}`
		req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		srv.handleRegister(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		// Verify agent is in registry
		got, ok := srv.registry.Get("agent-1")
		if !ok {
			t.Fatal("agent not found in registry")
		}
		if got.Name != "ws" {
			t.Errorf("expected name 'ws' (derived from workspace '/ws'), got '%s'", got.Name)
		}
		if got.APIURL != "http://agent:8080" {
			t.Errorf("expected apiUrl 'http://agent:8080', got '%s'", got.APIURL)
		}
		if got.Workspace != "/ws" {
			t.Errorf("expected workspace '/ws', got '%s'", got.Workspace)
		}
		if got.Model != "gpt-4" {
			t.Errorf("expected model 'gpt-4', got '%s'", got.Model)
		}
		if got.Role != "coder" {
			t.Errorf("expected role 'coder', got '%s'", got.Role)
		}
		if got.PID != 1234 {
			t.Errorf("expected pid 1234, got %d", got.PID)
		}
		if got.Status != registry.StatusIdle {
			t.Errorf("expected status idle, got %s", got.Status)
		}

		// Verify response body
		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		if resp["ok"] != true {
			t.Error("expected ok=true")
		}
		if resp["registeredAt"] == nil {
			t.Error("expected registeredAt field")
		}
	})

	t.Run("missing id", func(t *testing.T) {
		srv := newTestServer(t)
		body := `{"name":"Test","apiUrl":"http://agent:8080"}`
		req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		srv.handleRegister(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})

	t.Run("missing apiUrl", func(t *testing.T) {
		srv := newTestServer(t)
		body := `{"id":"agent-1","name":"Test"}`
		req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		srv.handleRegister(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		srv := newTestServer(t)
		req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader("not json"))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		srv.handleRegister(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})

	t.Run("duplicate apiUrl replaces previous", func(t *testing.T) {
		srv := newTestServer(t)

		// First registration
		a1 := &registry.Agent{
			ID:           "agent-1",
			Name:         "First",
			APIURL:       "http://agent:8080",
			Status:       registry.StatusIdle,
			RegisteredAt: time.Now(),
			LastSeenAt:   time.Now(),
		}
		if _, err := srv.registry.Register(a1); err != nil {
			t.Fatalf("first register: %v", err)
		}

		// Second registration with same apiUrl — should replace, not error
		body := `{"id":"agent-2","name":"Second","apiUrl":"http://agent:8080"}`
		req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		srv.handleRegister(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		// Old agent should be gone
		if _, ok := srv.registry.Get("agent-1"); ok {
			t.Error("expected old agent to be replaced")
		}
		// New agent should be present
		if _, ok := srv.registry.Get("agent-2"); !ok {
			t.Error("expected new agent to be present")
		}
	})
}

func TestHandleHeartbeat(t *testing.T) {
	t.Parallel()

	t.Run("existing agent", func(t *testing.T) {
		srv := newTestServer(t)

		oldTime := time.Now().Add(-1 * time.Hour)
		a := &registry.Agent{
			ID:           "agent-1",
			Name:         "Test",
			APIURL:       "http://agent:8080",
			Status:       registry.StatusIdle,
			RegisteredAt: time.Now(),
			LastSeenAt:   oldTime,
		}
		if _, err := srv.registry.Register(a); err != nil {
			t.Fatalf("register: %v", err)
		}

		body := `{"id":"agent-1"}`
		req := httptest.NewRequest(http.MethodPost, "/heartbeat", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		srv.handleHeartbeat(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		// Verify LastSeenAt was updated
		got, _ := srv.registry.Get("agent-1")
		if !got.LastSeenAt.After(oldTime) {
			t.Error("expected LastSeenAt to be updated")
		}
	})

	t.Run("non-existent agent", func(t *testing.T) {
		srv := newTestServer(t)

		body := `{"id":"nonexistent"}`
		req := httptest.NewRequest(http.MethodPost, "/heartbeat", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		srv.handleHeartbeat(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("expected status 404, got %d", w.Code)
		}
	})

	t.Run("missing id", func(t *testing.T) {
		srv := newTestServer(t)

		body := `{}`
		req := httptest.NewRequest(http.MethodPost, "/heartbeat", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		srv.handleHeartbeat(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})
}

func TestHandleUnregister(t *testing.T) {
	t.Parallel()

	t.Run("existing agent", func(t *testing.T) {
		srv := newTestServer(t)

		a := &registry.Agent{
			ID:           "agent-1",
			Name:         "Test",
			APIURL:       "http://agent:8080",
			Status:       registry.StatusIdle,
			RegisteredAt: time.Now(),
			LastSeenAt:   time.Now(),
		}
		if _, err := srv.registry.Register(a); err != nil {
			t.Fatalf("register: %v", err)
		}

		body := `{"id":"agent-1"}`
		req := httptest.NewRequest(http.MethodPost, "/unregister", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		srv.handleUnregister(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		if _, ok := srv.registry.Get("agent-1"); ok {
			t.Error("agent should have been removed")
		}
	})

	t.Run("non-existent agent", func(t *testing.T) {
		srv := newTestServer(t)

		body := `{"id":"nonexistent"}`
		req := httptest.NewRequest(http.MethodPost, "/unregister", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		srv.handleUnregister(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("expected status 404, got %d", w.Code)
		}
	})

	t.Run("missing id", func(t *testing.T) {
		srv := newTestServer(t)

		body := `{}`
		req := httptest.NewRequest(http.MethodPost, "/unregister", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		srv.handleUnregister(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})
}

func TestAgentToMap(t *testing.T) {
	t.Parallel()

	now := time.Now()
	a := &registry.Agent{
		ID:           "agent-1",
		Name:         "Test Agent",
		APIURL:       "http://example:8080",
		Workspace:    "/ws",
		Model:        "gpt-4",
		Role:         "coder",
		PID:          42,
		Status:       registry.StatusIdle,
		RegisteredAt: now,
		LastSeenAt:   now,
	}

	m := agentToMap(a)

	if m["id"] != "agent-1" {
		t.Errorf("id: got %v", m["id"])
	}
	if m["name"] != "Test Agent" {
		t.Errorf("name: got %v", m["name"])
	}
	if m["apiUrl"] != "http://example:8080" {
		t.Errorf("apiUrl: got %v", m["apiUrl"])
	}
	if m["workspace"] != "/ws" {
		t.Errorf("workspace: got %v", m["workspace"])
	}
	if m["model"] != "gpt-4" {
		t.Errorf("model: got %v", m["model"])
	}
	if m["role"] != "coder" {
		t.Errorf("role: got %v", m["role"])
	}
	if m["pid"] != 42 {
		t.Errorf("pid: got %v", m["pid"])
	}
	if m["status"] != registry.StatusIdle {
		t.Errorf("status: got %v", m["status"])
	}
	if m["registeredAt"] != now {
		t.Errorf("registeredAt: got %v", m["registeredAt"])
	}
	if m["lastSeenAt"] != now {
		t.Errorf("lastSeenAt: got %v", m["lastSeenAt"])
	}
}

// errorReader is an io.Reader that always returns an error.
type errorReader struct{}

func (e *errorReader) Read(p []byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

func TestHandleRegistrationErrorPaths(t *testing.T) {
	t.Parallel()

	t.Run("register read body error", func(t *testing.T) {
		srv := newTestServer(t)
		req := httptest.NewRequest(http.MethodPost, "/register", &errorReader{})
		w := httptest.NewRecorder()
		srv.handleRegister(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})

	t.Run("heartbeat read body error", func(t *testing.T) {
		srv := newTestServer(t)
		req := httptest.NewRequest(http.MethodPost, "/heartbeat", &errorReader{})
		w := httptest.NewRecorder()
		srv.handleHeartbeat(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})

	t.Run("heartbeat invalid json", func(t *testing.T) {
		srv := newTestServer(t)
		req := httptest.NewRequest(http.MethodPost, "/heartbeat", strings.NewReader("not json"))
		w := httptest.NewRecorder()
		srv.handleHeartbeat(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})

	t.Run("unregister read body error", func(t *testing.T) {
		srv := newTestServer(t)
		req := httptest.NewRequest(http.MethodPost, "/unregister", &errorReader{})
		w := httptest.NewRecorder()
		srv.handleUnregister(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})

	t.Run("unregister invalid json", func(t *testing.T) {
		srv := newTestServer(t)
		req := httptest.NewRequest(http.MethodPost, "/unregister", strings.NewReader("not json"))
		w := httptest.NewRecorder()
		srv.handleUnregister(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})
}
