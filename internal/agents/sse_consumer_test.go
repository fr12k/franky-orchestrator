package agents

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/franky/orchestrator/internal/events"
)

func TestNewSSEConsumer(t *testing.T) {
	t.Parallel()

	broker := events.NewBroker()
	var touched string
	touchFn := func(id string) { touched = id }
	checkFn := func(id string) bool { return true }

	c := NewSSEConsumer("agent-1", "http://localhost:9999", broker, touchFn, checkFn)
	if c.AgentID != "agent-1" {
		t.Errorf("expected AgentID 'agent-1', got '%s'", c.AgentID)
	}
	if c.APIURL != "http://localhost:9999" {
		t.Errorf("expected APIURL 'http://localhost:9999', got '%s'", c.APIURL)
	}
	if c.Broker != broker {
		t.Error("expected broker to be set")
	}

	// TouchFn should be callable without panic
	c.TouchFn("test-id")
	if touched != "test-id" {
		t.Errorf("expected touched 'test-id', got '%s'", touched)
	}
}

func TestInjectAgentID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		data     string
		agentID  string
		expected string
	}{
		{
			name:     "empty object",
			data:     `{}`,
			agentID:  "a1",
			expected: `{"agentId":"a1",}`,
		},
		{
			name:     "object with fields",
			data:     `{"kind":"event","msg":"hi"}`,
			agentID:  "xyz",
			expected: `{"agentId":"xyz","kind":"event","msg":"hi"}`,
		},
		{
			name:     "non-json string",
			data:     "just text",
			agentID:  "a1",
			expected: "just text",
		},
		{
			name:     "array",
			data:     `[1,2,3]`,
			agentID:  "a1",
			expected: `[1,2,3]`,
		},
		{
			name:     "empty string",
			data:     "",
			agentID:  "a1",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := injectAgentID(tt.data, tt.agentID)
			if result != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestBackoff(t *testing.T) {
	t.Parallel()

	b := newBackoff(1*time.Second, 10*time.Second)

	// Sequence: 1s, 2s, 4s, 8s, 10s, 10s
	expected := []time.Duration{1, 2, 4, 8, 10, 10}
	for i, exp := range expected {
		got := b.next()
		if got != exp*time.Second {
			t.Errorf("step %d: expected %v, got %v", i, exp*time.Second, got)
		}
	}
}

func TestBackoffReset(t *testing.T) {
	t.Parallel()

	b := newBackoff(1*time.Second, 30*time.Second)
	b.next() // 1s
	b.next() // 2s
	b.next() // 4s
	b.reset()

	got := b.next()
	if got != 1*time.Second {
		t.Errorf("expected 1s after reset, got %v", got)
	}
}

func TestConnectAndReadPing(t *testing.T) {
	t.Parallel()

	broker := events.NewBroker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go broker.Run(ctx)

	var mu sync.Mutex
	var touchedIDs []string
	touchFn := func(id string) {
		mu.Lock()
		defer mu.Unlock()
		touchedIDs = append(touchedIDs, id)
	}
	checkFn := func(id string) bool { return true }

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected flusher")
		}
		// Send a ping comment
		_, _ = w.Write([]byte(": ping\n\n"))
		flusher.Flush()
		// Give consumer time to read, then close
		time.Sleep(200 * time.Millisecond)
	}))
	t.Cleanup(srv.Close)

	c := NewSSEConsumer("agent-ping", srv.URL, broker, touchFn, checkFn)

	done := make(chan struct{})
	go func() {
		_ = c.connectAndRead()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("connectAndRead did not return")
	}

	mu.Lock()
	touched := len(touchedIDs)
	mu.Unlock()
	if touched < 1 {
		t.Error("expected TouchFn to be called at least once for ping")
	}
}

func TestConnectAndReadEvent(t *testing.T) {
	t.Parallel()

	broker := events.NewBroker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	subCh := make(chan *events.SseFrame, 16)
	broker.Subscribe(subCh)
	go broker.Run(ctx)

	var mu sync.Mutex
	var touchedIDs []string
	touchFn := func(id string) {
		mu.Lock()
		defer mu.Unlock()
		touchedIDs = append(touchedIDs, id)
	}
	checkFn := func(id string) bool { return true }

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected flusher")
		}
		// Send a proper SSE event
		_, _ = w.Write([]byte("id: 1\n"))
		_, _ = w.Write([]byte("event: chunk\n"))
		_, _ = w.Write([]byte("data: {\"text\":\"hello\"}\n"))
		_, _ = w.Write([]byte("\n"))
		flusher.Flush()
		time.Sleep(200 * time.Millisecond)
	}))
	t.Cleanup(srv.Close)

	c := NewSSEConsumer("agent-evt", srv.URL, broker, touchFn, checkFn)

	go func() { _ = c.connectAndRead() }()

	select {
	case frame := <-subCh:
		if frame.Event != "chunk" {
			t.Errorf("expected event 'chunk', got '%s'", frame.Event)
		}
		if frame.ID != "1" {
			t.Errorf("expected id '1', got '%s'", frame.ID)
		}
		// agentId should be injected
		if !strings.Contains(frame.Data, `"agentId":"agent-evt"`) {
			t.Errorf("expected agentId in data, got '%s'", frame.Data)
		}
		if !strings.Contains(frame.Data, `"text":"hello"`) {
			t.Errorf("expected 'text:hello' in data, got '%s'", frame.Data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for SSE event")
	}

	mu.Lock()
	touched := len(touchedIDs)
	mu.Unlock()
	if touched < 1 {
		t.Error("expected TouchFn to be called for event")
	}
}

func TestConnectAndReadHTTPError(t *testing.T) {
	t.Parallel()

	broker := events.NewBroker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go broker.Run(ctx)

	touchFn := func(id string) {}
	checkFn := func(id string) bool { return true }

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	t.Cleanup(srv.Close)

	c := NewSSEConsumer("agent-err", srv.URL, broker, touchFn, checkFn)

	err := c.connectAndRead()
	if err == nil {
		t.Fatal("expected error for non-200 response")
	}
}

func TestRunAgentRemoved(t *testing.T) {
	t.Parallel()

	broker := events.NewBroker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go broker.Run(ctx)

	touchFn := func(id string) {}
	checkFn := func(id string) bool { return false } // agent does not exist

	c := NewSSEConsumer("gone", "http://localhost:1", broker, touchFn, checkFn)

	done := make(chan struct{})
	go func() {
		c.Run()
		close(done)
	}()

	select {
	case <-done:
		// Run returned because CheckFn returned false
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return when agent is removed")
	}
}

func TestConnectAndReadWithTouchFnNil(t *testing.T) {
	t.Parallel()

	broker := events.NewBroker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go broker.Run(ctx)

	checkFn := func(id string) bool { return true }

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected flusher")
		}
		_, _ = w.Write([]byte(": ping\n\n"))
		flusher.Flush()
		time.Sleep(200 * time.Millisecond)
	}))
	t.Cleanup(srv.Close)

	// nil TouchFn should not panic
	c := NewSSEConsumer("agent-nil", srv.URL, broker, nil, checkFn)

	done := make(chan struct{})
	go func() {
		_ = c.connectAndRead()
		close(done)
	}()

	select {
	case <-done:
		// no panic = success
	case <-time.After(2 * time.Second):
		t.Fatal("connectAndRead did not return")
	}
}

func TestInjectedAgentIDIsValidJSON(t *testing.T) {
	t.Parallel()

	result := injectAgentID(`{"kind":"test"}`, "my-agent")
	// The result should be valid JSON (modulo the trailing comma injection style)
	var payload map[string]any
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Errorf("injected data should be valid JSON: %v (data: '%s')", err, result)
	}
	if payload["agentId"] != "my-agent" {
		t.Errorf("expected agentId 'my-agent', got '%v'", payload["agentId"])
	}
}

func TestRunReconnectLoop(t *testing.T) {
	broker := events.NewBroker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go broker.Run(ctx)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return 500 — causes connectAndRead to fail, triggering reconnect loop
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	// Use a mutable variable the closure captures by reference
	agentExists := true
	checkFn := func(id string) bool { return agentExists }
	touchFn := func(id string) {}

	c := NewSSEConsumer("agent-loop", srv.URL, broker, touchFn, checkFn)

	done := make(chan struct{})
	go func() {
		c.Run()
		close(done)
	}()

	// Give it time to attempt one connection and enter backoff sleep
	time.Sleep(300 * time.Millisecond)

	// Kill the agent — the checkFn closure reads this updated value
	agentExists = false

	// Wait for Run to exit
	select {
	case <-done:
		// success
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not exit after agentExists set to false")
	}
}

func TestConnectAndReadScannerError(t *testing.T) {
	broker := events.NewBroker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go broker.Run(ctx)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		// Write a line that exceeds the scanner's max buffer (1MB)
		// Use ~1.5MB of data on a single line
		big := make([]byte, 1500000)
		for i := range big {
			big[i] = 'x'
		}
		_, _ = w.Write(big)
		_, _ = w.Write([]byte("\n"))
		flusher.Flush()
		time.Sleep(200 * time.Millisecond)
	}))
	defer srv.Close()

	c := NewSSEConsumer("agent-scan", srv.URL, broker, nil, func(id string) bool { return false })

	err := c.connectAndRead()
	if err == nil {
		t.Error("expected scanner error, got nil")
	}
}
