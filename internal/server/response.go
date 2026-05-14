package server

import (
	"encoding/json"
	"net/http"
)

// writeOK writes a JSON success response: {"ok":true, ...data}.
func writeOK(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if data == nil {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		return
	}

	if m, ok := data.(map[string]any); ok {
		m["ok"] = true
		_ = json.NewEncoder(w).Encode(m)
	} else {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":   true,
			"data": data,
		})
	}
}

// writeError writes a structured JSON error response.
func writeError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":        false,
		"error":     msg,
		"errorCode": code,
	})
}
