package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoggingMiddleware(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("ok"))
	})

	wrapped := loggingMiddleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/test-path", nil)
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d", w.Code)
	}
	if w.Body.String() != "ok" {
		t.Errorf("expected body 'ok', got '%s'", w.Body.String())
	}

	// Ensure the statusWriter captured the correct status
	// (We can't easily inspect the log output, but we verify the handler still works)
}

func TestRecoveryMiddleware(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	})

	wrapped := recoveryMiddleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/panic-path", nil)
	w := httptest.NewRecorder()

	// This should not propagate the panic
	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", contentType)
	}
}

func TestRecoveryMiddlewareNoPanic(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("all good"))
	})

	wrapped := recoveryMiddleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
	if w.Body.String() != "all good" {
		t.Errorf("expected body 'all good', got '%s'", w.Body.String())
	}
}

func TestStatusWriter(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	sw := &statusWriter{ResponseWriter: rec, status: http.StatusOK}

	sw.WriteHeader(http.StatusNotFound)
	if sw.status != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", sw.status)
	}
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected recorder status 404, got %d", rec.Code)
	}

	// Write also works
	n, err := sw.Write([]byte("hello"))
	if err != nil {
		t.Errorf("write error: %v", err)
	}
	if n != 5 {
		t.Errorf("expected 5 bytes, got %d", n)
	}
	if rec.Body.String() != "hello" {
		t.Errorf("expected 'hello', got '%s'", rec.Body.String())
	}
}

func TestWithMiddleware(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("accepted"))
	})

	wrapped := withMiddleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/accepted", nil)
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("expected status 202, got %d", w.Code)
	}
	if w.Body.String() != "accepted" {
		t.Errorf("expected 'accepted', got '%s'", w.Body.String())
	}
}
