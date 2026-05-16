package registry

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func newTestRegistry(t *testing.T) *AgentRegistry {
	t.Helper()
	persister := NewPersister(filepath.Join(t.TempDir(), "agents.json"))
	return New(persister)
}

func testAgent(id, name, apiURL string) *Agent {
	return &Agent{
		ID:         id,
		Name:       name,
		APIURL:     apiURL,
		Status:     StatusIdle,
		LastSeenAt: time.Now(),
	}
}

func TestRegister(t *testing.T) {
	t.Parallel()

	t.Run("valid registration", func(t *testing.T) {
		reg := newTestRegistry(t)
		a := testAgent("agent-1", "Test Agent", "http://example.com:8080")
		_, err := reg.Register(a)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		got, ok := reg.Get("agent-1")
		if !ok {
			t.Fatal("expected agent to be found")
		}
		if got.ID != "agent-1" {
			t.Errorf("expected ID agent-1, got %s", got.ID)
		}
		if got.Name != "Test Agent" {
			t.Errorf("expected Name 'Test Agent', got '%s'", got.Name)
		}
	})

	t.Run("duplicate apiUrl replaces existing agent", func(t *testing.T) {
		reg := newTestRegistry(t)
		a1 := testAgent("agent-1", "Agent 1", "http://example.com:8080")
		if _, err := reg.Register(a1); err != nil {
			t.Fatalf("first registration failed: %v", err)
		}
		a2 := testAgent("agent-2", "Agent 2", "http://example.com:8080")
		_, err := reg.Register(a2)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		// Old agent should be removed
		if _, ok := reg.Get("agent-1"); ok {
			t.Error("expected old agent to be removed")
		}
		// New agent should be present
		if _, ok := reg.Get("agent-2"); !ok {
			t.Error("expected new agent to be present")
		}
	})

	t.Run("duplicate apiUrl same agent re-registers (no-op replace)", func(t *testing.T) {
		reg := newTestRegistry(t)
		a1 := testAgent("agent-1", "Agent 1", "http://example.com:8080")
		a1.Status = StatusOffline
		if _, err := reg.Register(a1); err != nil {
			t.Fatalf("first registration failed: %v", err)
		}
		a2 := testAgent("agent-1", "Agent 1 revisited", "http://example.com:8080")
		_, err := reg.Register(a2)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		// Same ID should still be present
		if _, ok := reg.Get("agent-1"); !ok {
			t.Error("expected same agent to still be present")
		}
	})

	t.Run("empty ID and APIURL", func(t *testing.T) {
		reg := newTestRegistry(t)
		a := &Agent{ID: "", APIURL: ""}
		_, err := reg.Register(a)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if _, ok := reg.Get(""); !ok {
			t.Error("expected agent with empty ID to be stored")
		}
	})
}

func TestUnregister(t *testing.T) {
	t.Parallel()

	t.Run("existing agent", func(t *testing.T) {
		reg := newTestRegistry(t)
		a := testAgent("agent-1", "Test", "http://example.com:8080")
		if _, err := reg.Register(a); err != nil {
			t.Fatalf("register failed: %v", err)
		}
		err := reg.Unregister("agent-1")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if _, ok := reg.Get("agent-1"); ok {
			t.Error("expected agent to be removed")
		}
	})

	t.Run("non-existent agent", func(t *testing.T) {
		reg := newTestRegistry(t)
		err := reg.Unregister("nonexistent")
		if err == nil {
			t.Error("expected error for non-existent agent")
		}
	})
}

func TestGet(t *testing.T) {
	t.Parallel()

	reg := newTestRegistry(t)
	a := testAgent("agent-1", "Test", "http://example.com:8080")
	if _, err := reg.Register(a); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	t.Run("found", func(t *testing.T) {
		got, ok := reg.Get("agent-1")
		if !ok {
			t.Fatal("expected agent to be found")
		}
		if got.ID != "agent-1" {
			t.Errorf("expected ID agent-1, got %s", got.ID)
		}
	})

	t.Run("not found", func(t *testing.T) {
		_, ok := reg.Get("nonexistent")
		if ok {
			t.Error("expected agent not to be found")
		}
	})
}

func TestList(t *testing.T) {
	t.Parallel()

	t.Run("empty", func(t *testing.T) {
		reg := newTestRegistry(t)
		agents := reg.List()
		if len(agents) != 0 {
			t.Errorf("expected empty list, got %d agents", len(agents))
		}
	})

	t.Run("multiple agents", func(t *testing.T) {
		reg := newTestRegistry(t)
		if _, err := reg.Register(testAgent("agent-1", "A1", "http://a1.com")); err != nil {
			t.Fatalf("register failed: %v", err)
		}
		if _, err := reg.Register(testAgent("agent-2", "A2", "http://a2.com")); err != nil {
			t.Fatalf("register failed: %v", err)
		}
		agents := reg.List()
		if len(agents) != 2 {
			t.Fatalf("expected 2 agents, got %d", len(agents))
		}
	})

	t.Run("returns copy not reference", func(t *testing.T) {
		reg := newTestRegistry(t)
		if _, err := reg.Register(testAgent("agent-1", "A1", "http://a1.com")); err != nil {
			t.Fatalf("register failed: %v", err)
		}
		agents := reg.List()
		// Modify the returned slice — replace the pointer
		if len(agents) > 0 {
			agents[0] = &Agent{ID: "hacked"}
		}
		// Original should be unchanged
		got, ok := reg.Get("agent-1")
		if !ok {
			t.Fatal("expected agent-1 to still exist")
		}
		if got.ID != "agent-1" {
			t.Error("original agent was modified through List result")
		}
	})
}

func TestSetStatus(t *testing.T) {
	t.Parallel()

	t.Run("existing agent", func(t *testing.T) {
		reg := newTestRegistry(t)
		a := testAgent("agent-1", "Test", "http://example.com:8080")
		a.Status = StatusIdle
		if _, err := reg.Register(a); err != nil {
			t.Fatalf("register failed: %v", err)
		}
		reg.SetStatus("agent-1", StatusStreaming)
		got, _ := reg.Get("agent-1")
		if got.Status != StatusStreaming {
			t.Errorf("expected status streaming, got %s", got.Status)
		}
	})

	t.Run("non-existent agent no-op", func(t *testing.T) {
		reg := newTestRegistry(t)
		// Should not panic
		reg.SetStatus("nonexistent", StatusStreaming)
		// Registry should still be empty
		if len(reg.List()) != 0 {
			t.Error("expected registry to remain empty")
		}
	})
}

func TestTouch(t *testing.T) {
	t.Parallel()

	t.Run("updates LastSeenAt", func(t *testing.T) {
		reg := newTestRegistry(t)
		a := testAgent("agent-1", "Test", "http://example.com:8080")
		oldTime := time.Now().Add(-1 * time.Hour)
		a.LastSeenAt = oldTime
		if _, err := reg.Register(a); err != nil {
			t.Fatalf("register failed: %v", err)
		}
		reg.Touch("agent-1")
		got, _ := reg.Get("agent-1")
		if !got.LastSeenAt.After(oldTime) {
			t.Error("expected LastSeenAt to be updated")
		}
	})

	t.Run("revives offline agent to idle", func(t *testing.T) {
		reg := newTestRegistry(t)
		a := testAgent("agent-1", "Test", "http://example.com:8080")
		a.Status = StatusOffline
		if _, err := reg.Register(a); err != nil {
			t.Fatalf("register failed: %v", err)
		}
		reg.Touch("agent-1")
		got, _ := reg.Get("agent-1")
		if got.Status != StatusIdle {
			t.Errorf("expected status idle, got %s", got.Status)
		}
	})

	t.Run("non-existent agent no-op", func(t *testing.T) {
		reg := newTestRegistry(t)
		// Should not panic
		reg.Touch("nonexistent")
	})
}

func TestMarkOffline(t *testing.T) {
	t.Parallel()

	t.Run("idle to offline", func(t *testing.T) {
		reg := newTestRegistry(t)
		a := testAgent("agent-1", "Test", "http://example.com:8080")
		a.Status = StatusIdle
		if _, err := reg.Register(a); err != nil {
			t.Fatalf("register failed: %v", err)
		}
		reg.MarkOffline("agent-1")
		got, _ := reg.Get("agent-1")
		if got.Status != StatusOffline {
			t.Errorf("expected status offline, got %s", got.Status)
		}
	})

	t.Run("already offline no-op", func(t *testing.T) {
		reg := newTestRegistry(t)
		a := testAgent("agent-1", "Test", "http://example.com:8080")
		a.Status = StatusOffline
		if _, err := reg.Register(a); err != nil {
			t.Fatalf("register failed: %v", err)
		}
		// Should not panic and status should remain offline
		reg.MarkOffline("agent-1")
		got, _ := reg.Get("agent-1")
		if got.Status != StatusOffline {
			t.Errorf("expected status offline, got %s", got.Status)
		}
	})

	t.Run("non-existent agent no-op", func(t *testing.T) {
		reg := newTestRegistry(t)
		// Should not panic
		reg.MarkOffline("nonexistent")
	})
}

func TestFindStale(t *testing.T) {
	t.Parallel()

	t.Run("all fresh", func(t *testing.T) {
		reg := newTestRegistry(t)
		a := testAgent("agent-1", "Test", "http://example.com:8080")
		a.LastSeenAt = time.Now()
		if _, err := reg.Register(a); err != nil {
			t.Fatalf("register failed: %v", err)
		}
		stale := reg.FindStale(30 * time.Second)
		if len(stale) != 0 {
			t.Errorf("expected 0 stale, got %d", len(stale))
		}
	})

	t.Run("some stale", func(t *testing.T) {
		reg := newTestRegistry(t)
		a1 := testAgent("agent-1", "Fresh", "http://fresh.com")
		a1.LastSeenAt = time.Now()
		if _, err := reg.Register(a1); err != nil {
			t.Fatalf("register failed: %v", err)
		}
		a2 := testAgent("agent-2", "Stale", "http://stale.com")
		a2.LastSeenAt = time.Now().Add(-2 * time.Minute)
		if _, err := reg.Register(a2); err != nil {
			t.Fatalf("register failed: %v", err)
		}
		stale := reg.FindStale(30 * time.Second)
		if len(stale) != 1 {
			t.Fatalf("expected 1 stale, got %d", len(stale))
		}
		if stale[0] != "agent-2" {
			t.Errorf("expected agent-2 to be stale, got %s", stale[0])
		}
	})

	t.Run("all offline skipped", func(t *testing.T) {
		reg := newTestRegistry(t)
		a := testAgent("agent-1", "Offline", "http://offline.com")
		a.Status = StatusOffline
		a.LastSeenAt = time.Now().Add(-2 * time.Minute)
		if _, err := reg.Register(a); err != nil {
			t.Fatalf("register failed: %v", err)
		}
		stale := reg.FindStale(30 * time.Second)
		if len(stale) != 0 {
			t.Errorf("expected 0 stale (offline skipped), got %d", len(stale))
		}
	})
}

func TestLoad(t *testing.T) {
	t.Parallel()

	t.Run("populates map and marks all offline", func(t *testing.T) {
		// First, create a persister and save some agents
		dir := t.TempDir()
		path := filepath.Join(dir, "agents.json")
		persister := NewPersister(path)
		reg1 := New(persister)
		a1 := testAgent("agent-1", "A1", "http://a1.com")
		a1.Status = StatusIdle
		a2 := testAgent("agent-2", "A2", "http://a2.com")
		a2.Status = StatusStreaming
		if _, err := reg1.Register(a1); err != nil {
			t.Fatalf("register failed: %v", err)
		}
		if _, err := reg1.Register(a2); err != nil {
			t.Fatalf("register failed: %v", err)
		}

		// Now create a new registry with the same persister and load
		reg2 := New(persister)
		if err := reg2.Load(); err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		agents := reg2.List()
		if len(agents) != 2 {
			t.Fatalf("expected 2 agents, got %d", len(agents))
		}
		for _, a := range agents {
			if a.Status != StatusOffline {
				t.Errorf("agent %s: expected status offline, got %s", a.ID, a.Status)
			}
		}
	})

	t.Run("load empty file", func(t *testing.T) {
		reg := newTestRegistry(t)
		if err := reg.Load(); err != nil {
			t.Fatalf("Load on empty file should not error: %v", err)
		}
		if len(reg.List()) != 0 {
			t.Error("expected empty registry after loading non-existent file")
		}
	})
}

func TestConcurrentAccess(t *testing.T) {
	// Not parallel — this test is already concurrent internally
	reg := newTestRegistry(t)

	// Pre-register some agents
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("agent-%d", i)
		if _, err := reg.Register(testAgent(id, id, "http://"+id+".com")); err != nil {
			t.Fatalf("pre-register failed: %v", err)
		}
	}

	var wg sync.WaitGroup
	n := 50

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Register a new agent
			id := fmt.Sprintf("concurrent-%d", i)
			a := testAgent(id, id, "http://"+id+".com")
			_, _ = reg.Register(a)
			// Get an existing agent
			reg.Get(fmt.Sprintf("agent-%d", i%10))
			// List all agents
			reg.List()
		}(i)
	}
	wg.Wait()

	// All pre-registered agents should still exist
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("agent-%d", i)
		if _, ok := reg.Get(id); !ok {
			t.Errorf("pre-registered agent %s missing after concurrent access", id)
		}
	}
}

// fakeBroker is a minimal Broker implementation for testing StartStaleWatcher.
type fakeBroker struct {
	mu     sync.Mutex
	events []struct {
		kind string
		data map[string]any
	}
}

func (b *fakeBroker) PublishOrchestratorEvent(kind string, data map[string]any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, struct {
		kind string
		data map[string]any
	}{kind, data})
}

func (b *fakeBroker) eventsByKind(kind string) []map[string]any {
	b.mu.Lock()
	defer b.mu.Unlock()
	var result []map[string]any
	for _, e := range b.events {
		if e.kind == kind {
			result = append(result, e.data)
		}
	}
	return result
}

func TestForEachByAPIURL(t *testing.T) {
	t.Parallel()

	t.Run("single match via Register", func(t *testing.T) {
		t.Parallel()

		reg := newTestRegistry(t)

		// Register two agents with different apiUrls.
		a1 := testAgent("agent-1", "Agent 1", "http://shared.com:8080")
		if _, err := reg.Register(a1); err != nil {
			t.Fatalf("register agent-1 failed: %v", err)
		}

		a2 := testAgent("agent-2", "Agent 2", "http://other.com:8080")
		if _, err := reg.Register(a2); err != nil {
			t.Fatalf("register agent-2 failed: %v", err)
		}

		// Mark agent-1 offline so a new agent with the same apiUrl can replace it.
		reg.MarkOffline("agent-1")
		a3 := testAgent("agent-3", "Agent 3", "http://shared.com:8080")
		if _, err := reg.Register(a3); err != nil {
			t.Fatalf("register agent-3 failed: %v", err)
		}
		// agent-1 (offline) was replaced by agent-3 (idle).

		var iterated []string
		reg.ForEachByAPIURL("http://shared.com:8080", func(a *Agent) {
			iterated = append(iterated, a.ID)
		})

		if len(iterated) != 1 {
			t.Fatalf("expected 1 agent iterated, got %d: %v", len(iterated), iterated)
		}
		if iterated[0] != "agent-3" {
			t.Errorf("expected agent-3, got %s", iterated[0])
		}

		// Verify agent-2 is only iterated for its own apiUrl.
		iterated = nil
		reg.ForEachByAPIURL("http://other.com:8080", func(a *Agent) {
			iterated = append(iterated, a.ID)
		})
		if len(iterated) != 1 || iterated[0] != "agent-2" {
			t.Errorf("expected only agent-2, got %v", iterated)
		}

		// Verify no agents for an unmatched apiUrl.
		iterated = nil
		reg.ForEachByAPIURL("http://nope.com:8080", func(a *Agent) {
			iterated = append(iterated, a.ID)
		})
		if len(iterated) != 0 {
			t.Errorf("expected 0 agents for unknown apiUrl, got %d", len(iterated))
		}
	})

	t.Run("iterates both when offline and non-offline share apiUrl via Load", func(t *testing.T) {
		t.Parallel()

		// Two agents with the same apiUrl can coexist in the persisted file
		// even though Register would prevent it. ForEachByAPIURL must iterate
		// over all matching agents.
		dir := t.TempDir()
		path := filepath.Join(dir, "agents.json")
		persister := NewPersister(path)

		// Persist two agents with the same apiUrl directly.
		agents := map[string]*Agent{
			"agent-offline": {
				ID:     "agent-offline",
				Name:   "Offline Agent",
				APIURL: "http://shared.com:8080",
				Status: StatusOffline,
			},
			"agent-active": {
				ID:     "agent-active",
				Name:   "Active Agent",
				APIURL: "http://shared.com:8080",
				Status: StatusIdle,
			},
		}
		if err := persister.Save(agents); err != nil {
			t.Fatalf("persist failed: %v", err)
		}

		// Load into a fresh registry (Load marks all as offline, so we
		// re-register one to make it active).
		reg := New(persister)
		if err := reg.Load(); err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		// Both agents are now offline after Load. Touch the active one to
		// revive it.
		reg.Touch("agent-active")

		var iterated []string
		reg.ForEachByAPIURL("http://shared.com:8080", func(a *Agent) {
			iterated = append(iterated, a.ID)
		})

		if len(iterated) != 2 {
			t.Fatalf("expected 2 agents iterated, got %d: %v", len(iterated), iterated)
		}

		// Verify one is offline and one is idle.
		foundOffline := false
		foundIdle := false
		for _, id := range iterated {
			a, _ := reg.Get(id)
			if a.Status == StatusOffline {
				foundOffline = true
			}
			if a.Status == StatusIdle {
				foundIdle = true
			}
		}
		if !foundOffline {
			t.Error("expected one offline agent in iteration")
		}
		if !foundIdle {
			t.Error("expected one idle agent in iteration")
		}
	})
}

func TestPath(t *testing.T) {
	t.Parallel()

	p := NewPersister("/tmp/test-agents.json")
	if p.Path() != "/tmp/test-agents.json" {
		t.Errorf("expected /tmp/test-agents.json, got %s", p.Path())
	}

	// Also test with a relative path
	p2 := NewPersister("data/agents.json")
	if p2.Path() != "data/agents.json" {
		t.Errorf("expected data/agents.json, got %s", p2.Path())
	}
}

func TestStartStaleWatcherLogic(t *testing.T) {
	t.Parallel()

	reg := newTestRegistry(t)

	// Register an agent whose LastSeenAt is 2 hours ago (definitely stale).
	stale := testAgent("stale-agent", "Stale Agent", "http://stale.com:8080")
	stale.LastSeenAt = time.Now().Add(-2 * time.Hour)
	if _, err := reg.Register(stale); err != nil {
		t.Fatalf("register stale agent failed: %v", err)
	}

	// Also register a fresh agent that should NOT be marked offline.
	fresh := testAgent("fresh-agent", "Fresh Agent", "http://fresh.com:8080")
	if _, err := reg.Register(fresh); err != nil {
		t.Fatalf("register fresh agent failed: %v", err)
	}

	broker := &fakeBroker{}

	// Inline the core watcher logic — FindStale + MarkOffline + publish.
	// This avoids the 15s ticker in the real StartStaleWatcher goroutine.
	staleIDs := reg.FindStale(90 * time.Second)
	for _, id := range staleIDs {
		reg.MarkOffline(id)
		agent, ok := reg.Get(id)
		lastSeenAt := ""
		if ok {
			lastSeenAt = agent.LastSeenAt.Format(time.RFC3339)
		}
		broker.PublishOrchestratorEvent("agent_status", map[string]any{
			"agentId":    id,
			"status":     string(StatusOffline),
			"lastSeenAt": lastSeenAt,
		})
	}

	// Fresh agent should still be idle.
	got, ok := reg.Get("fresh-agent")
	if !ok {
		t.Fatal("fresh agent should still exist")
	}
	if got.Status != StatusIdle {
		t.Errorf("expected fresh agent to be idle, got %s", got.Status)
	}

	// Verify broker received the event.
	events := broker.eventsByKind("agent_status")
	if len(events) == 0 {
		t.Fatal("expected at least one agent_status event")
	}

	found := false
	for _, e := range events {
		if e["agentId"] == "stale-agent" && e["status"] == string(StatusOffline) {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected broker event for stale-agent offline")
	}
}

func TestStartStaleWatcher(t *testing.T) {
	t.Parallel()

	t.Run("ctx done path", func(t *testing.T) {
		t.Parallel()

		reg := newTestRegistry(t)
		broker := &fakeBroker{}

		ctx, cancel := context.WithCancel(context.Background())

		done := make(chan struct{})
		go func() {
			defer close(done)
			StartStaleWatcher(ctx, reg, broker)
		}()

		cancel()
		<-done
	})

	t.Run("ticker path marks stale agents", func(t *testing.T) {
		t.Parallel()

		reg := newTestRegistry(t)

		stale := testAgent("stale-agent", "Stale", "http://stale.com")
		stale.LastSeenAt = time.Now().Add(-2 * time.Hour)
		if _, err := reg.Register(stale); err != nil {
			t.Fatalf("register: %v", err)
		}

		broker := &fakeBroker{}

		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()

		// Use 10ms ticks — fast enough for tests
		startStaleWatcher(ctx, reg, broker, 90*time.Second, 10*time.Millisecond)

		// Should have marked stale agent offline and published event
		if a, ok := reg.Get("stale-agent"); !ok || a.Status != StatusOffline {
			t.Fatal("expected stale agent to be marked offline")
		}

		events := broker.eventsByKind("agent_status")
		if len(events) == 0 {
			t.Fatal("expected at least one agent_status event")
		}
	})
}

func TestSaveError(t *testing.T) {
	t.Run("Load from directory path returns error", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		p := NewPersister(dir) // dir is a directory, not a file
		_, err := p.Load()
		if err == nil {
			t.Error("expected error when loading from a directory path")
		}
	})

	t.Run("Save to non-existent directory succeeds (creates dirs)", func(t *testing.T) {
		t.Parallel()

		dir := filepath.Join(t.TempDir(), "deep", "nested", "path")
		path := filepath.Join(dir, "agents.json")
		p := NewPersister(path)

		// Verify the directory does not exist yet
		if _, err := os.Stat(dir); err == nil {
			t.Fatal("expected directory not to exist yet")
		}

		agents := map[string]*Agent{
			"agent-1": {ID: "agent-1", Name: "Test", APIURL: "http://test.com", Status: StatusIdle},
		}

		if err := p.Save(agents); err != nil {
			t.Fatalf("Save should succeed creating dirs: %v", err)
		}

		// Verify file was created
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected file to be created: %v", err)
		}
	})
}
