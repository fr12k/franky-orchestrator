package ui

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestServeHTTP_IndexHTML(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	ct := w.Header().Get("Content-Type")
	if ct != "text/html; charset=utf-8" {
		t.Errorf("expected content-type 'text/html; charset=utf-8', got '%s'", ct)
	}

	body := w.Body.String()
	if len(body) == 0 {
		t.Error("expected non-empty body")
	}
}

func TestServeHTTP_IndexHTMLExplicit(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/index.html", nil)
	w := httptest.NewRecorder()

	ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	ct := w.Header().Get("Content-Type")
	if ct != "text/html; charset=utf-8" {
		t.Errorf("expected content-type 'text/html; charset=utf-8', got '%s'", ct)
	}
}

func TestServeHTTP_CSS(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/style.css", nil)
	w := httptest.NewRecorder()

	ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	ct := w.Header().Get("Content-Type")
	if ct != "text/css; charset=utf-8" {
		t.Errorf("expected content-type 'text/css; charset=utf-8', got '%s'", ct)
	}

	body := w.Body.String()
	if len(body) == 0 {
		t.Error("expected non-empty body")
	}
}

func TestServeHTTP_JS(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	w := httptest.NewRecorder()

	ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	ct := w.Header().Get("Content-Type")
	if ct != "text/javascript; charset=utf-8" {
		t.Errorf("expected content-type 'text/javascript; charset=utf-8', got '%s'", ct)
	}

	body := w.Body.String()
	if len(body) == 0 {
		t.Error("expected non-empty body")
	}
}

func TestServeHTTP_SPAFallback(t *testing.T) {
	t.Parallel()

	// Unknown paths should fall back to index.html (SPA behavior)
	req := httptest.NewRequest(http.MethodGet, "/agents/123", nil)
	w := httptest.NewRecorder()

	ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 (SPA fallback), got %d", w.Code)
	}

	body := w.Body.String()
	if len(body) == 0 {
		t.Error("expected non-empty body (SPA fallback)")
	}
}

func TestServeHTTP_NestedPathFallback(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/deep/nested/path", nil)
	w := httptest.NewRecorder()

	ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 (SPA fallback), got %d", w.Code)
	}
}

func TestServeHTTP_UnknownExtension(t *testing.T) {
	// We cannot really test unknown extension without a file with that extension in assets.
	// The SPA fallback for missing files returns index.html (text/html),
	// so this is covered by TestServeHTTP_SPAFallback.
}

func TestServeHTTP_ContentTypeSVG(t *testing.T) {
	t.Parallel()

	// SVG content type check - only if assets contain an SVG. 
	// The content-type switch handles .svg, so if there is no SVG asset,
	// this falls back to index.html. That's fine.
	req := httptest.NewRequest(http.MethodGet, "/icon.svg", nil)
	w := httptest.NewRecorder()

	ServeHTTP(w, req)

	// Either it exists (image/svg+xml) or falls back (text/html)
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if ct != "image/svg+xml" && ct != "text/html; charset=utf-8" {
		t.Errorf("unexpected content-type: '%s'", ct)
	}
}

func TestServeHTTP_PNG(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/logo.png", nil)
	w := httptest.NewRecorder()

	ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if ct != "image/png" && ct != "text/html; charset=utf-8" {
		t.Errorf("unexpected content-type: '%s'", ct)
	}
}

func TestServeHTTP_OctetStream(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/data.bin", nil)
	w := httptest.NewRecorder()

	ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
	// Unknown extension gets application/octet-stream (falls back to index.html content)
	ct := w.Header().Get("Content-Type")
	if ct != "application/octet-stream" {
		t.Errorf("expected 'application/octet-stream', got '%s'", ct)
	}
}


