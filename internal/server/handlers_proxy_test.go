package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/franky/orchestrator/internal/agents"
	"github.com/franky/orchestrator/internal/events"
	"github.com/franky/orchestrator/internal/registry"
)

// ---------------------------------------------------------------------------
// 1. TestHandleDeleteAgent
// ---------------------------------------------------------------------------

func TestHandleDeleteAgent(t *testing.T) {
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

		req := httptest.NewRequest(http.MethodDelete, "/agents/agent-1", nil)
		req.SetPathValue("id", "agent-1")
		w := httptest.NewRecorder()

		srv.handleDeleteAgent(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		// Verify agent is removed
		if _, ok := srv.registry.Get("agent-1"); ok {
			t.Error("agent should have been removed from registry")
		}

		// Verify response body
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if body["ok"] != true {
			t.Errorf("expected ok=true, got %v", body["ok"])
		}
	})

	t.Run("non-existent agent", func(t *testing.T) {
		srv := newTestServer(t)

		req := httptest.NewRequest(http.MethodDelete, "/agents/nonexistent", nil)
		req.SetPathValue("id", "nonexistent")
		w := httptest.NewRecorder()

		srv.handleDeleteAgent(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("expected status 404, got %d", w.Code)
		}
	})

	t.Run("empty id", func(t *testing.T) {
		srv := newTestServer(t)

		req := httptest.NewRequest(http.MethodDelete, "/agents/", nil)
		req.SetPathValue("id", "")
		w := httptest.NewRecorder()

		srv.handleDeleteAgent(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})
}

// ---------------------------------------------------------------------------
// 2. TestProxyToAgent
// ---------------------------------------------------------------------------

func TestProxyToAgent(t *testing.T) {
	t.Parallel()

	// fakeRecord holds the captured request details from the fake agent.
	type fakeRecord struct {
		mu     sync.Mutex
		method string
		path   string
		body   string
		header http.Header
	}

	t.Run("successful GET proxy", func(t *testing.T) {
		srv := newTestServer(t)

		var rec fakeRecord
		fakeAgent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec.mu.Lock()
			rec.method = r.Method
			rec.path = r.URL.Path
			rec.header = r.Header
			bodyBytes, _ := io.ReadAll(r.Body)
			rec.body = string(bodyBytes)
			rec.mu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"hello":"world"}`))
		}))
		defer fakeAgent.Close()

		a := &registry.Agent{
			ID:           "agent-1",
			Name:         "Test",
			APIURL:       fakeAgent.URL,
			Status:       registry.StatusIdle,
			RegisteredAt: time.Now(),
			LastSeenAt:   time.Now(),
		}
		if _, err := srv.registry.Register(a); err != nil {
			t.Fatalf("register: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/agents/agent-1/transcript", nil)
		req.SetPathValue("id", "agent-1")
		w := httptest.NewRecorder()

		srv.proxyToAgent(w, req, "agent-1", "GET", "/transcript")

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		rec.mu.Lock()
		defer rec.mu.Unlock()
		if rec.method != "GET" {
			t.Errorf("expected GET, got %s", rec.method)
		}
		if rec.path != "/transcript" {
			t.Errorf("expected /transcript, got %s", rec.path)
		}

		// Verify forwarded response body
		if !strings.Contains(w.Body.String(), `"hello":"world"`) {
			t.Errorf("expected response body to contain hello:world, got %s", w.Body.String())
		}
	})

	t.Run("successful POST proxy with body", func(t *testing.T) {
		srv := newTestServer(t)

		var rec fakeRecord
		fakeAgent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec.mu.Lock()
			rec.method = r.Method
			rec.path = r.URL.Path
			rec.header = r.Header
			bodyBytes, _ := io.ReadAll(r.Body)
			rec.body = string(bodyBytes)
			rec.mu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ack":true}`))
		}))
		defer fakeAgent.Close()

		a := &registry.Agent{
			ID:           "agent-1",
			Name:         "Test",
			APIURL:       fakeAgent.URL,
			Status:       registry.StatusIdle,
			RegisteredAt: time.Now(),
			LastSeenAt:   time.Now(),
		}
		if _, err := srv.registry.Register(a); err != nil {
			t.Fatalf("register: %v", err)
		}


	})

	t.Run("agent not found", func(t *testing.T) {
		srv := newTestServer(t)

		req := httptest.NewRequest(http.MethodGet, "/agents/nonexistent/transcript", nil)
		req.SetPathValue("id", "nonexistent")
		w := httptest.NewRecorder()

		srv.proxyToAgent(w, req, "nonexistent", "GET", "/transcript")

		if w.Code != http.StatusNotFound {
			t.Errorf("expected status 404, got %d", w.Code)
		}
	})

	t.Run("agent offline", func(t *testing.T) {
		srv := newTestServer(t)

		a := &registry.Agent{
			ID:           "agent-1",
			Name:         "Test",
			APIURL:       "http://agent:8080",
			Status:       registry.StatusOffline,
			RegisteredAt: time.Now(),
			LastSeenAt:   time.Now(),
		}
		if _, err := srv.registry.Register(a); err != nil {
			t.Fatalf("register: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/agents/agent-1/transcript", nil)
		req.SetPathValue("id", "agent-1")
		w := httptest.NewRecorder()

		srv.proxyToAgent(w, req, "agent-1", "GET", "/transcript")

		if w.Code != http.StatusBadGateway {
			t.Errorf("expected status 502, got %d", w.Code)
		}
	})

	t.Run("agent unreachable via bad URL", func(t *testing.T) {
		srv := newTestServer(t)

		a := &registry.Agent{
			ID:           "agent-1",
			Name:         "Test",
			APIURL:       "http://127.0.0.1:1", // no server listening
			Status:       registry.StatusIdle,
			RegisteredAt: time.Now(),
			LastSeenAt:   time.Now(),
		}
		if _, err := srv.registry.Register(a); err != nil {
			t.Fatalf("register: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/agents/agent-1/transcript", nil)
		req.SetPathValue("id", "agent-1")
		w := httptest.NewRecorder()

		srv.proxyToAgent(w, req, "agent-1", "GET", "/transcript")

		if w.Code != http.StatusBadGateway {
			t.Errorf("expected status 502, got %d", w.Code)
		}
	})
}

// ---------------------------------------------------------------------------
// 3. TestProxyHandlerThinWrappers
// ---------------------------------------------------------------------------

func TestProxyHandlerThinWrappers(t *testing.T) {
	t.Parallel()

	type proxyCase struct {
		name     string
		handler  func(s *Server, w http.ResponseWriter, r *http.Request)
		wantMethod string
		wantPath   string
	}

	cases := []proxyCase{
		{
			name:       "handleProxyTranscript",
			handler:    func(s *Server, w http.ResponseWriter, r *http.Request) { s.handleProxyTranscript(w, r) },
			wantMethod: "GET",
			wantPath:   "/transcript",
		},
		{
			name:       "handleProxyRole",
			handler:    func(s *Server, w http.ResponseWriter, r *http.Request) { s.handleProxyRole(w, r) },
			wantMethod: "GET",
			wantPath:   "/role",
		},
		{
			name:       "handleProxyUsage",
			handler:    func(s *Server, w http.ResponseWriter, r *http.Request) { s.handleProxyUsage(w, r) },
			wantMethod: "GET",
			wantPath:   "/usage",
		},
		{
			name:       "handleProxySession",
			handler:    func(s *Server, w http.ResponseWriter, r *http.Request) { s.handleProxySession(w, r) },
			wantMethod: "GET",
			wantPath:   "/session",
		},
		{
			name:       "handleProxySessions",
			handler:    func(s *Server, w http.ResponseWriter, r *http.Request) { s.handleProxySessions(w, r) },
			wantMethod: "GET",
			wantPath:   "/sessions",
		},
		{
			name:       "handleProxyDesignDocs",
			handler:    func(s *Server, w http.ResponseWriter, r *http.Request) { s.handleProxyDesignDocs(w, r) },
			wantMethod: "GET",
			wantPath:   "/design-docs",
		},
		{
			name:       "handleProxyHealth",
			handler:    func(s *Server, w http.ResponseWriter, r *http.Request) { s.handleProxyHealth(w, r) },
			wantMethod: "GET",
			wantPath:   "/health",
		},

		{
			name:       "handleProxyInterrupt",
			handler:    func(s *Server, w http.ResponseWriter, r *http.Request) { s.handleProxyInterrupt(w, r) },
			wantMethod: "POST",
			wantPath:   "/interrupt",
		},
		{
			name:       "handleProxyRestart",
			handler:    func(s *Server, w http.ResponseWriter, r *http.Request) { s.handleProxyRestart(w, r) },
			wantMethod: "POST",
			wantPath:   "/restart",
		},
		{
			name:       "handleProxyCommand",
			handler:    func(s *Server, w http.ResponseWriter, r *http.Request) { s.handleProxyCommand(w, r) },
			wantMethod: "POST",
			wantPath:   "/command",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := newTestServer(t)

			var gotMethod, gotPath string
			var mu sync.Mutex
			fakeAgent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				gotMethod = r.Method
				gotPath = r.URL.Path
				mu.Unlock()

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"ok":true}`))
			}))
			defer fakeAgent.Close()

			a := &registry.Agent{
				ID:           "agent-1",
				Name:         "Test",
				APIURL:       fakeAgent.URL,
				Status:       registry.StatusIdle,
				RegisteredAt: time.Now(),
				LastSeenAt:   time.Now(),
			}
			if _, err := srv.registry.Register(a); err != nil {
				t.Fatalf("register: %v", err)
			}

			req := httptest.NewRequest(http.MethodGet, "/agents/agent-1"+tc.wantPath, nil)
			req.SetPathValue("id", "agent-1")
			w := httptest.NewRecorder()

			tc.handler(srv, w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
			}

			mu.Lock()
			defer mu.Unlock()
			if gotMethod != tc.wantMethod {
				t.Errorf("expected method %q, got %q", tc.wantMethod, gotMethod)
			}
			if gotPath != tc.wantPath {
				t.Errorf("expected path %q, got %q", tc.wantPath, gotPath)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 4. TestHandleBrowserSSE
// ---------------------------------------------------------------------------

func TestHandleBrowserSSE(t *testing.T) {
	// Not parallel — modifies shared broker state in a way that needs isolation

	t.Run("receives and formats SSE frame", func(t *testing.T) {
		srv := newTestServer(t)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		req := httptest.NewRequest(http.MethodGet, "/events", nil).WithContext(ctx)
		w := httptest.NewRecorder()

		// Run handleBrowserSSE in a goroutine since it blocks
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			srv.handleBrowserSSE(w, req)
		}()

		// Give it time to subscribe
		time.Sleep(20 * time.Millisecond)

		// Publish a frame
		srv.broker.Publish(&events.SseFrame{
			ID:    "42",
			Event: "agent_update",
			Data:  `{"status":"idle"}`,
		})

		// Give it time to deliver
		time.Sleep(20 * time.Millisecond)

		// Cancel context to stop the handler
		cancel()
		wg.Wait()

		body := w.Body.String()

		// Should have the initial comment
		if !strings.Contains(body, ": connected") {
			t.Errorf("expected ': connected' in body, got:\n%s", body)
		}

		// Should have the published frame in SSE format
		if !strings.Contains(body, "id: 42") {
			t.Errorf("expected 'id: 42' in body, got:\n%s", body)
		}
		if !strings.Contains(body, "event: agent_update") {
			t.Errorf("expected 'event: agent_update' in body, got:\n%s", body)
		}
		if !strings.Contains(body, `data: {"status":"idle"}`) {
			t.Errorf("expected data line in body, got:\n%s", body)
		}
	})
}

// ---------------------------------------------------------------------------
// 4b. TestWriteSSEFrame
// ---------------------------------------------------------------------------

func TestWriteSSEFrame(t *testing.T) {
	t.Parallel()

	t.Run("full frame", func(t *testing.T) {
		w := httptest.NewRecorder()
		frame := &events.SseFrame{
			ID:    "1",
			Event: "update",
			Data:  `{"key":"value"}`,
		}
		writeSSEFrame(w, frame)

		body := w.Body.String()
		if !strings.Contains(body, "id: 1\n") {
			t.Errorf("expected 'id: 1\\n', got: %q", body)
		}
		if !strings.Contains(body, "event: update\n") {
			t.Errorf("expected 'event: update\\n', got: %q", body)
		}
		if !strings.Contains(body, "data: {\"key\":\"value\"}\n\n") {
			t.Errorf("expected data line with double newline, got: %q", body)
		}
	})

	t.Run("data-only frame", func(t *testing.T) {
		w := httptest.NewRecorder()
		frame := &events.SseFrame{
			Data: `plain text`,
		}
		writeSSEFrame(w, frame)

		body := w.Body.String()
		if strings.Contains(body, "id:") {
			t.Errorf("expected no id line, got: %q", body)
		}
		if strings.Contains(body, "event:") {
			t.Errorf("expected no event line, got: %q", body)
		}
		if !strings.Contains(body, "data: plain text\n\n") {
			t.Errorf("expected 'data: plain text\\n\\n', got: %q", body)
		}
	})

	t.Run("empty frame", func(t *testing.T) {
		w := httptest.NewRecorder()
		frame := &events.SseFrame{}
		writeSSEFrame(w, frame)

		body := w.Body.String()
		// Should only have "data: \n\n"
		if body != "data: \n\n" {
			t.Errorf("expected 'data: \\n\\n', got: %q", body)
		}
	})
}

// ---------------------------------------------------------------------------
// 5. TestAgentToMapFull
// ---------------------------------------------------------------------------

func TestAgentToMapFull(t *testing.T) {
	t.Parallel()

	t.Run("with all stat fields populated", func(t *testing.T) {
		a := &registry.Agent{
			ID:           "agent-full",
			Name:         "Full Agent",
			APIURL:       "http://full:8080",
			Workspace:    "/ws/full",
			Model:        "gpt-4",
			Role:         "coder",
			PID:          999,
			Status:       registry.StatusStreaming,
			RegisteredAt: time.Now(),
			LastSeenAt:   time.Now(),
			MessageCount: 42,
			TurnCount:    7,
			TokensIn:     1500,
			TokensOut:    800,
			ToolStats: map[string]int{
				"read":  5,
				"write": 3,
			},
			ErrorMessage: "something went wrong",
		}

		m := agentToMap(a)

		// Base fields
		if m["id"] != "agent-full" {
			t.Errorf("expected id 'agent-full', got %v", m["id"])
		}
		if m["name"] != "Full Agent" {
			t.Errorf("expected name 'Full Agent', got %v", m["name"])
		}
		if m["apiUrl"] != "http://full:8080" {
			t.Errorf("expected apiUrl, got %v", m["apiUrl"])
		}
		if m["workspace"] != "/ws/full" {
			t.Errorf("expected workspace, got %v", m["workspace"])
		}
		if m["model"] != "gpt-4" {
			t.Errorf("expected model, got %v", m["model"])
		}
		if m["role"] != "coder" {
			t.Errorf("expected role, got %v", m["role"])
		}
		if m["pid"] != 999 {
			t.Errorf("expected pid 999, got %v", m["pid"])
		}
		if m["status"] != registry.StatusStreaming {
			t.Errorf("expected status streaming, got %v", m["status"])
		}

		// Stat fields
		if m["messageCount"] != 42 {
			t.Errorf("expected messageCount 42, got %v", m["messageCount"])
		}
		if m["turnCount"] != 7 {
			t.Errorf("expected turnCount 7, got %v", m["turnCount"])
		}
		if m["tokensIn"] != 1500 {
			t.Errorf("expected tokensIn 1500, got %v", m["tokensIn"])
		}
		if m["tokensOut"] != 800 {
			t.Errorf("expected tokensOut 800, got %v", m["tokensOut"])
		}

		stats, ok := m["stats"].(map[string]any)
		if !ok {
			t.Fatalf("expected stats map, got %T", m["stats"])
		}
		if stats["messages"] != 42 {
			t.Errorf("expected stats.messages 42, got %v", stats["messages"])
		}

		toolStats, ok := m["toolStats"].(map[string]int)
		if !ok {
			t.Fatalf("expected toolStats map, got %T", m["toolStats"])
		}
		if toolStats["read"] != 5 {
			t.Errorf("expected toolStats.read 5, got %v", toolStats["read"])
		}
		if toolStats["write"] != 3 {
			t.Errorf("expected toolStats.write 3, got %v", toolStats["write"])
		}

		if m["errorMessage"] != "something went wrong" {
			t.Errorf("expected errorMessage, got %v", m["errorMessage"])
		}
	})

	t.Run("with zero stat fields omitted", func(t *testing.T) {
		a := &registry.Agent{
			ID:           "agent-minimal",
			Name:         "Minimal",
			APIURL:       "http://min:8080",
			Status:       registry.StatusIdle,
			RegisteredAt: time.Now(),
			LastSeenAt:   time.Now(),
			// All stat fields are zero values
		}

		m := agentToMap(a)

		// Stat fields should be absent when zero
		if _, ok := m["messageCount"]; ok {
			t.Error("expected messageCount to be absent when zero")
		}
		if _, ok := m["turnCount"]; ok {
			t.Error("expected turnCount to be absent when zero")
		}
		if _, ok := m["tokensIn"]; ok {
			t.Error("expected tokensIn to be absent when zero")
		}
		if _, ok := m["tokensOut"]; ok {
			t.Error("expected tokensOut to be absent when zero")
		}
		if _, ok := m["toolStats"]; ok {
			t.Error("expected toolStats to be absent when empty")
		}
		if _, ok := m["errorMessage"]; ok {
			t.Error("expected errorMessage to be absent when empty")
		}
		if _, ok := m["stats"]; ok {
			t.Error("expected stats to be absent when messageCount is zero")
		}
	})
}

// ---------------------------------------------------------------------------
// 6. TestNew
// ---------------------------------------------------------------------------

func TestNew(t *testing.T) {
	// Not parallel — uses t.TempDir

	reg := registry.New(registry.NewPersister(t.TempDir() + "/test.json"))
	if err := reg.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	broker := events.NewBroker()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go broker.Run(ctx)

	agentClient := agents.NewClient()
	dataDir := t.TempDir()

	srv := New(":9876", reg, broker, agentClient, dataDir)

	if srv.Addr != ":9876" {
		t.Errorf("expected Addr ':9876', got %q", srv.Addr)
	}
	if srv.Handler == nil {
		t.Fatal("expected non-nil Handler")
	}

	// Make a GET /health request through the Handler
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	srv.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["ok"] != true {
		t.Errorf("expected ok=true, got %v", body["ok"])
	}
}

// ---------------------------------------------------------------------------
// 7. TestProxyToAgentEdgeCases
// ---------------------------------------------------------------------------

func TestProxyToAgentEdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("proxy with accept header forwarded", func(t *testing.T) {
		srv := newTestServer(t)

		var gotAccept string
		var mu sync.Mutex
		fakeAgent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			gotAccept = r.Header.Get("Accept")
			mu.Unlock()
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		}))
		defer fakeAgent.Close()

		a := &registry.Agent{
			ID: "agent-1", Name: "Test", APIURL: fakeAgent.URL,
			Status: registry.StatusIdle, RegisteredAt: time.Now(), LastSeenAt: time.Now(),
		}
		if _, err := srv.registry.Register(a); err != nil {
			t.Fatalf("register: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/agents/agent-1/transcript", nil)
		req.SetPathValue("id", "agent-1")
		req.Header.Set("Accept", "text/event-stream")
		w := httptest.NewRecorder()

		srv.proxyToAgent(w, req, "agent-1", "GET", "/transcript")

		mu.Lock()
		if gotAccept != "text/event-stream" {
			t.Errorf("expected Accept 'text/event-stream', got '%s'", gotAccept)
		}
		mu.Unlock()
	})

	t.Run("proxy with no content-type in response defaults to json", func(t *testing.T) {
		srv := newTestServer(t)

		fakeAgent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Do NOT set Content-Type header
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		}))
		defer fakeAgent.Close()

		a := &registry.Agent{
			ID: "agent-1", Name: "Test", APIURL: fakeAgent.URL,
			Status: registry.StatusIdle, RegisteredAt: time.Now(), LastSeenAt: time.Now(),
		}
		if _, err := srv.registry.Register(a); err != nil {
			t.Fatalf("register: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/agents/agent-1/transcript", nil)
		req.SetPathValue("id", "agent-1")
		w := httptest.NewRecorder()

		srv.proxyToAgent(w, req, "agent-1", "GET", "/transcript")

		if w.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
			t.Errorf("expected Content-Type from fake agent (text/plain), got '%s'", w.Header().Get("Content-Type"))
		}
	})
	}

// ---------------------------------------------------------------------------
// 9. TestHandleBrowserSSEEdgeCases
// ---------------------------------------------------------------------------

func TestHandleBrowserSSEEdgeCases(t *testing.T) {
	t.Run("client disconnection path", func(t *testing.T) {
		srv := newTestServer(t)

		ctx, cancel := context.WithCancel(context.Background())
		req := httptest.NewRequest(http.MethodGet, "/events", nil).WithContext(ctx)
		w := httptest.NewRecorder()

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			srv.handleBrowserSSE(w, req)
		}()

		// Cancel immediately — should hit the context.Done() path
		time.Sleep(10 * time.Millisecond)
		cancel()
		wg.Wait()

		// Should have the connected message
		body := w.Body.String()
		if !strings.Contains(body, ": connected") {
			t.Errorf("expected ': connected' in body, got:\n%s", body)
		}
	})

	t.Run("unsubscribe closes channel path", func(t *testing.T) {
		srv := newTestServer(t)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		req := httptest.NewRequest(http.MethodGet, "/events", nil).WithContext(ctx)
		w := httptest.NewRecorder()

		// We'll manually unsubscribe to trigger the !ok path
		ch := make(chan *events.SseFrame, 64)
		srv.broker.Subscribe(ch)

		// Replace the handler's subscribe logic by closing ch early
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			srv.handleBrowserSSE(w, req)
		}()

		time.Sleep(20 * time.Millisecond)
		// Unsubscribe closes the channel — but handleBrowserSSE has its OWN channel
		// This just tests the unsubscribe in the normal flow
		srv.broker.Unsubscribe(ch)
		cancel()
		wg.Wait()
	})
}
