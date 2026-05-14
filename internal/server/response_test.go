package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteOK(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		data       any
		wantStatus int
		checkBody  func(t *testing.T, body map[string]any)
	}{
		{
			name:       "nil",
			data:       nil,
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, body map[string]any) {
				if body["ok"] != true {
					t.Errorf("expected ok=true, got %v", body["ok"])
				}
				if len(body) != 1 {
					t.Errorf("expected 1 key, got %d", len(body))
				}
			},
		},
		{
			name:       "map",
			data:       map[string]any{"foo": "bar"},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, body map[string]any) {
				if body["ok"] != true {
					t.Errorf("expected ok=true, got %v", body["ok"])
				}
				if body["foo"] != "bar" {
					t.Errorf("expected foo=bar, got %v", body["foo"])
				}
			},
		},
		{
			name:       "non-map struct",
			data:       struct{ Name string }{Name: "test"},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, body map[string]any) {
				if body["ok"] != true {
					t.Errorf("expected ok=true, got %v", body["ok"])
				}
				dataField, ok := body["data"]
				if !ok {
					t.Fatal("expected 'data' key")
				}
				dataMap, ok := dataField.(map[string]any)
				if !ok {
					t.Fatalf("expected data to be map, got %T", dataField)
				}
				if dataMap["Name"] != "test" {
					t.Errorf("expected Name=test, got %v", dataMap["Name"])
				}
			},
		},
		{
			name:       "string",
			data:       "just a string",
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, body map[string]any) {
				if body["ok"] != true {
					t.Errorf("expected ok=true, got %v", body["ok"])
				}
				if body["data"] != "just a string" {
					t.Errorf("expected data='just a string', got %v", body["data"])
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w := httptest.NewRecorder()
			writeOK(w, tt.data)

			if w.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, w.Code)
			}

			contentType := w.Header().Get("Content-Type")
			if contentType != "application/json" {
				t.Errorf("expected Content-Type application/json, got %s", contentType)
			}

			var body map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("failed to unmarshal body: %v", err)
			}

			tt.checkBody(t, body)
		})
	}
}

func TestWriteError(t *testing.T) {
	t.Parallel()

	w := httptest.NewRecorder()
	writeError(w, http.StatusNotFound, "agent_not_found", "agent not found")

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", contentType)
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to unmarshal body: %v", err)
	}

	if body["ok"] != false {
		t.Errorf("expected ok=false, got %v", body["ok"])
	}
	if body["error"] != "agent not found" {
		t.Errorf("expected error='agent not found', got '%v'", body["error"])
	}
	if body["errorCode"] != "agent_not_found" {
		t.Errorf("expected errorCode='agent_not_found', got '%v'", body["errorCode"])
	}
}
