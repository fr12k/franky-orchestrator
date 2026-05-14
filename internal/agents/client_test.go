package agents

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	t.Parallel()

	c := NewClient()
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.httpClient == nil {
		t.Fatal("expected non-nil http client")
	}
	if c.httpClient.Timeout != 10*time.Second {
		t.Errorf("expected timeout 10s, got %v", c.httpClient.Timeout)
	}
}

func TestDoGetSuccess(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)

	c := NewClient()
	body, err := c.doGet(srv.URL + "/test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(body) != `{"ok":true}` {
		t.Errorf("expected '{\"ok\":true}', got '%s'", string(body))
	}
}

func TestDoGetNotFound(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("not found"))
	}))
	t.Cleanup(srv.Close)

	c := NewClient()
	_, err := c.doGet(srv.URL + "/missing")
	if err == nil {
		t.Fatal("expected error for 404")
	}
}

func TestDoGetUnreachable(t *testing.T) {
	t.Parallel()

	c := NewClient()
	_, err := c.doGet("http://127.0.0.1:1/test")
	if err == nil {
		t.Fatal("expected error for unreachable host")
	}
}

func TestGetTranscript(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/transcript" {
			t.Errorf("expected path /transcript, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("transcript data"))
	}))
	t.Cleanup(srv.Close)

	c := NewClient()
	body, err := c.GetTranscript(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(body) != "transcript data" {
		t.Errorf("expected 'transcript data', got '%s'", string(body))
	}
}

func TestGetRole(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/role" {
			t.Errorf("expected path /role, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("role data"))
	}))
	t.Cleanup(srv.Close)

	c := NewClient()
	body, err := c.GetRole(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(body) != "role data" {
		t.Errorf("expected 'role data', got '%s'", string(body))
	}
}

func TestGetUsage(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/usage" {
			t.Errorf("expected path /usage, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("usage data"))
	}))
	t.Cleanup(srv.Close)

	c := NewClient()
	body, err := c.GetUsage(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(body) != "usage data" {
		t.Errorf("expected 'usage data', got '%s'", string(body))
	}
}

func TestGetSession(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/session" {
			t.Errorf("expected path /session, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("session data"))
	}))
	t.Cleanup(srv.Close)

	c := NewClient()
	body, err := c.GetSession(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(body) != "session data" {
		t.Errorf("expected 'session data', got '%s'", string(body))
	}
}

func TestGetSessions(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sessions" {
			t.Errorf("expected path /sessions, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("sessions data"))
	}))
	t.Cleanup(srv.Close)

	c := NewClient()
	body, err := c.GetSessions(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(body) != "sessions data" {
		t.Errorf("expected 'sessions data', got '%s'", string(body))
	}
}

func TestGetDesignDocs(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/design-docs" {
			t.Errorf("expected path /design-docs, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("design docs data"))
	}))
	t.Cleanup(srv.Close)

	c := NewClient()
	body, err := c.GetDesignDocs(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(body) != "design docs data" {
		t.Errorf("expected 'design docs data', got '%s'", string(body))
	}
}

func TestDoGetLargeBody(t *testing.T) {
	t.Parallel()

	// Test that bodies are read correctly (up to 10MB limit)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Write a moderate amount of data
		_, _ = w.Write(make([]byte, 10000))
	}))
	t.Cleanup(srv.Close)

	c := NewClient()
	body, err := c.doGet(srv.URL + "/large")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(body) != 10000 {
		t.Errorf("expected 10000 bytes, got %d", len(body))
	}
}
