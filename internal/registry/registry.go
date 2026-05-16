package registry

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// AgentRegistry manages the set of known agents with thread-safe access.
type AgentRegistry struct {
	mu            sync.RWMutex
	agents        map[string]*Agent // id → agent
	persist       *Persister
	dirty         bool
	flushInterval time.Duration
	flushCtx      context.Context
	flushCancel   context.CancelFunc
	flushWG       sync.WaitGroup
}

// New creates a new AgentRegistry with the given persister.
func New(persist *Persister) *AgentRegistry {
	return &AgentRegistry{
		agents:  make(map[string]*Agent),
		persist: persist,
	}
}

// Register adds or updates an agent in the registry. Only one agent is allowed
// per host (apiUrl). If another agent already has the same apiUrl, it is replaced.
// Returns true if this was a brand-new registration (vs. an update).
// Persists immediately — registration changes are user-triggered and should
// survive a crash. Use batched persistence for high-frequency status changes.
func (r *AgentRegistry) Register(a *Agent) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	_, exists := r.agents[a.ID]

	// Only one agent per host (apiUrl) — replace any existing agent at the same URL
	for _, existing := range r.agents {
		if existing.ID == a.ID {
			continue // same agent re-registering — allowed
		}
		if existing.APIURL == a.APIURL {
			// Replace the existing agent with the new one
			delete(r.agents, existing.ID)
			break
		}
	}

	r.agents[a.ID] = a
	return !exists, r.persist.Save(r.agents)
}

// Unregister removes an agent from the registry by ID.
// Persists immediately — unregistration is user-triggered and should
// survive a crash.
func (r *AgentRegistry) Unregister(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.agents[id]; !ok {
		return errors.New("agent not found")
	}

	delete(r.agents, id)
	return r.persist.Save(r.agents)
}

// Get returns a copy of an agent by ID, or nil if not found.
// The returned copy is safe for concurrent access — callers may read it freely.
func (r *AgentRegistry) Get(id string) (*Agent, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	a, ok := r.agents[id]
	if !ok {
		return nil, false
	}
	return copyAgent(a), true
}

// copyAgent returns a shallow copy suitable for safe read access.
// Map fields (ToolStats) are not deep-copied — callers should treat the
// returned copy as read-only for those fields.
func copyAgent(a *Agent) *Agent {
	ac := *a // copy the struct value
	return &ac
}

// List returns a copy of all registered agents.
func (r *AgentRegistry) List() []*Agent {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*Agent, 0, len(r.agents))
	for _, a := range r.agents {
		result = append(result, a)
	}
	return result
}

// SetStatus updates the status of an agent and marks the registry dirty for
// batched persistence.
func (r *AgentRegistry) SetStatus(id string, status Status) {
	r.mu.Lock()
	a, ok := r.agents[id]
	if !ok {
		r.mu.Unlock()
		return
	}
	if a.Status != status {
		a.Status = status
		r.dirty = true
	}
	r.mu.Unlock()
}

// UpdateUsage updates live counters on an agent from usage data.
// Does not persist — usage data is polled every ~15s and is ephemeral.
func (r *AgentRegistry) UpdateUsage(id string, msgCount, turnCount, tokensIn, tokensOut int, toolStats map[string]int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	a, ok := r.agents[id]
	if !ok {
		return
	}
	a.MessageCount = msgCount
	a.TurnCount = turnCount
	a.TokensIn = tokensIn
	a.TokensOut = tokensOut
	a.ToolStats = toolStats
}

// UpdateAgent atomically reads and modifies an agent under the write lock.
// The fn callback receives a pointer to the agent that is safe to mutate
// (the write lock is held). Return true from fn if changes were made.
// Returns false if the agent was not found.
func (r *AgentRegistry) UpdateAgent(id string, fn func(*Agent) bool) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	a, ok := r.agents[id]
	if !ok {
		return false
	}
	// fn receives the real pointer under lock — safe to mutate.
	// No need to re-assign to the map because a is already the pointer
	// stored in the map. Callers must not retain the pointer after fn returns.
	if fn(a) {
		r.dirty = true
	}
	return true
}

// Touch updates the LastSeenAt timestamp of an agent.
// This is called on heartbeat, SSE pings, and event frames.
// Does not persist — too frequent. Status transitions are handled by
// SetStatus / MarkOffline which do persist.
func (r *AgentRegistry) Touch(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	a, ok := r.agents[id]
	if !ok {
		return
	}
	a.LastSeenAt = time.Now()

	// If agent was offline and we see activity, mark idle
	if a.Status == StatusOffline {
		a.Status = StatusIdle
		r.dirty = true
	}
}

// MarkOffline marks an agent as offline and marks the registry dirty for
// batched persistence.
func (r *AgentRegistry) MarkOffline(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	a, ok := r.agents[id]
	if !ok {
		return
	}
	if a.Status != StatusOffline {
		a.Status = StatusOffline
		r.dirty = true
	}
}

// PersistAll immediately persists the full agents map to disk.
func (r *AgentRegistry) PersistAll() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.persist.Save(r.agents)
}

// StartFlushLoop launches a background goroutine that flushes pending changes
// to disk every flushInterval. Call StopFlushLoop to shut it down cleanly.
func (r *AgentRegistry) StartFlushLoop(ctx context.Context, interval time.Duration) {
	r.mu.Lock()
	if r.flushCancel != nil {
		r.mu.Unlock()
		return // already running
	}
	r.flushInterval = interval
	r.flushCtx, r.flushCancel = context.WithCancel(ctx)
	r.flushWG.Add(1)
	r.mu.Unlock()

	go r.flushLoop()
}

// StopFlushLoop stops the background flush goroutine and does a final flush.
func (r *AgentRegistry) StopFlushLoop() {
	r.mu.Lock()
	cancel := r.flushCancel
	r.mu.Unlock()

	if cancel != nil {
		cancel() // signal the loop to stop
		r.flushWG.Wait()
	}

	// One final flush to catch any in-flight dirty changes
	r.doFlush()
}

// flushLoop runs in a goroutine, periodically flushing dirty state.
func (r *AgentRegistry) flushLoop() {
	defer r.flushWG.Done()

	ticker := time.NewTicker(r.flushInterval)
	defer ticker.Stop()

	slog.Info("batched persistence flush loop started", "interval", r.flushInterval)

	for {
		select {
		case <-r.flushCtx.Done():
			slog.Info("batched persistence flush loop stopped")
			return
		case <-ticker.C:
			r.doFlush()
		}
	}
}

// doFlush persists the agents map to disk if there are pending dirty changes.
// A deep copy is made under lock so concurrent SetStatus / MarkOffline / etc.
// do not race with the serialisation performed outside the lock.
// Uses compact JSON (no indent) to reduce allocation size.
func (r *AgentRegistry) doFlush() {
	r.mu.Lock()
	if !r.dirty {
		r.mu.Unlock()
		return
	}
	cp := r.deepCopyAgents()
	r.dirty = false
	r.mu.Unlock()

	if err := r.persist.SaveCompact(cp); err != nil {
		slog.Error("batched persist flush failed", "err", err)
		// Re-mark dirty so we retry on next tick.
		// If a concurrent mutation also set dirty=true between the clear above
		// and this re-mark, the flag remains true — which only causes an extra
		// flush on the next tick (harmless).
		r.mu.Lock()
		r.dirty = true
		r.mu.Unlock()
	}
}

// deepCopyAgents returns a deep copy of the agents map.
// The caller must hold r.mu (at least read lock) and it stays held on return.
func (r *AgentRegistry) deepCopyAgents() map[string]*Agent {
	cp := make(map[string]*Agent, len(r.agents))
	for id, a := range r.agents {
		ac := *a // copy the struct value
		if a.ToolStats != nil {
			ac.ToolStats = make(map[string]int, len(a.ToolStats))
			for k, v := range a.ToolStats {
				ac.ToolStats[k] = v
			}
		}
		cp[id] = &ac
	}
	return cp
}

// FindStale returns IDs of agents that haven't been seen within the given
// duration and are not already offline.
func (r *AgentRegistry) FindStale(timeout time.Duration) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var stale []string
	cutoff := time.Now().Add(-timeout)
	for _, a := range r.agents {
		if a.Status != StatusOffline && a.LastSeenAt.Before(cutoff) {
			stale = append(stale, a.ID)
		}
	}
	return stale
}

// Load reads persisted agents and populates the registry.
func (r *AgentRegistry) Load() error {
	agents, err := r.persist.Load()
	if err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for id, a := range agents {
		// All agents loaded from disk start as offline until proven live
		a.Status = StatusOffline
		r.agents[id] = a
	}

	return nil
}

// ForEachByAPIURL calls fn for each agent with a matching API URL.
// Used by the SSEConsumer to locate the agent after SSE reconnection.
func (r *AgentRegistry) ForEachByAPIURL(apiURL string, fn func(*Agent)) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, a := range r.agents {
		if a.APIURL == apiURL {
			fn(a)
		}
	}
}
