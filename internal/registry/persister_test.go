package registry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAndLoad(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "agents.json")
	persister := NewPersister(path)

	agents := map[string]*Agent{
		"agent-1": {
			ID:     "agent-1",
			Name:   "Agent One",
			APIURL: "http://one.com:8080",
			Status: StatusIdle,
		},
		"agent-2": {
			ID:     "agent-2",
			Name:   "Agent Two",
			APIURL: "http://two.com:8080",
			Status: StatusStreaming,
		},
	}

	if err := persister.Save(agents); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}

	loaded, err := persister.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(loaded) != len(agents) {
		t.Fatalf("expected %d agents, got %d", len(agents), len(loaded))
	}

	for id, want := range agents {
		got, ok := loaded[id]
		if !ok {
			t.Errorf("missing agent %s in loaded data", id)
			continue
		}
		if got.Name != want.Name {
			t.Errorf("agent %s: expected name %s, got %s", id, want.Name, got.Name)
		}
		if got.APIURL != want.APIURL {
			t.Errorf("agent %s: expected apiUrl %s, got %s", id, want.APIURL, got.APIURL)
		}
		if got.Status != want.Status {
			t.Errorf("agent %s: expected status %s, got %s", id, want.Status, got.Status)
		}
	}
}

func TestLoadNonExistent(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "nonexistent.json")
	persister := NewPersister(path)

	agents, err := persister.Load()
	if err != nil {
		t.Fatalf("Load should not error for non-existent file: %v", err)
	}
	if agents == nil {
		t.Error("expected non-nil map")
	}
	if len(agents) != 0 {
		t.Errorf("expected empty map, got %d agents", len(agents))
	}
}

func TestSaveCreatesDir(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "subdir", "nested")
	path := filepath.Join(dir, "agents.json")
	persister := NewPersister(path)

	// Directory does not exist yet
	if _, err := os.Stat(dir); err == nil {
		t.Fatal("expected directory not to exist yet")
	}

	agents := map[string]*Agent{
		"agent-1": {ID: "agent-1", Name: "Test", APIURL: "http://test.com", Status: StatusIdle},
	}

	if err := persister.Save(agents); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify directory was created
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatal("expected directory to be created")
	}

	// Verify file exists
	if _, err := os.Stat(path); err != nil {
		t.Fatal("expected file to exist")
	}
}

func TestSaveCorruptedJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "agents.json")
	persister := NewPersister(path)

	// Write invalid JSON to the file
	if err := os.WriteFile(path, []byte("not json"), 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	_, err := persister.Load()
	if err == nil {
		t.Error("expected error loading invalid JSON")
	}
}

func TestSaveRoundTripJSONIntegrity(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "agents.json")
	persister := NewPersister(path)

	original := map[string]*Agent{
		"a": {ID: "a", Name: "Name A", APIURL: "http://a.com", Workspace: "/ws/a", Model: "gpt-4", Role: "coder", PID: 1234, Status: StatusIdle},
	}

	if err := persister.Save(original); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Read raw file and compare
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file failed: %v", err)
	}

	var decoded map[string]*Agent
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal file failed: %v", err)
	}

	if len(decoded) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(decoded))
	}

	got := decoded["a"]
	if got.Name != "Name A" {
		t.Errorf("expected Name A, got %s", got.Name)
	}
	if got.PID != 1234 {
		t.Errorf("expected PID 1234, got %d", got.PID)
	}
}
