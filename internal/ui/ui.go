package ui

import (
	"embed"
	"net/http"
	"strings"
)

//go:embed assets/*
var assets embed.FS

// ServeHTTP serves embedded static assets. Falls back to index.html for SPA routing.
func ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Strip leading slash
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}

	// Prepend assets/ prefix for embed.FS root
	data, err := assets.ReadFile("assets/" + path)
	if err != nil {
		// SPA fallback: serve index.html for unknown paths.
		// assets/index.html is always embedded, so this cannot fail.
		data, _ = assets.ReadFile("assets/index.html")
	}

	// Set content type based on extension
	switch {
	case strings.HasSuffix(path, ".html"):
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	case strings.HasSuffix(path, ".js"):
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	case strings.HasSuffix(path, ".css"):
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case strings.HasSuffix(path, ".svg"):
		w.Header().Set("Content-Type", "image/svg+xml")
	case strings.HasSuffix(path, ".png"):
		w.Header().Set("Content-Type", "image/png")
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
	}

	_, _ = w.Write(data)
}
