package registry

import (
	"errors"
	"sync"
	"time"
)

// ErrDuplicateAPIURL is returned when registering an agent with an API URL
// that is already used by another non-offline agent.
var ErrDuplicateAPIURL = errors.New("duplicate apiUrl")

// AgentRegistry manages the set of known agents with thread-safe access.
type AgentRegistry struct {
	mu       sync.RWMutex
	agents   map[string]*Agent // id → agent
	persist  *Persister
}

// New creates a new AgentRegistry with the given persister.
func New(persist *Persister) *AgentRegistry {
	return &AgentRegistry{
		agents:  make(map[string]*Agent),
		persist: persist,
	}
}

// Register adds or updates an agent in the registry. Returns false and
// ErrDuplicateAPIURL if a different non-offline agent already has the same apiUrl.
// Returns true if this was a brand-new registration (vs. an update).
func (r *AgentRegistry) Register(a *Agent) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	_, exists := r.agents[a.ID]

	// Check for duplicate apiUrl among other non-offline agents
	for _, existing := range r.agents {
		if existing.ID == a.ID {
			continue // same agent re-registering — allowed
		}
		if existing.APIURL == a.APIURL {
			if existing.Status == StatusOffline {
				// Replace the offline agent with the new one
				delete(r.agents, existing.ID)
				break
			}
			return false, ErrDuplicateAPIURL
		}
	}

	r.agents[a.ID] = a
	return !exists, r.persist.Save(r.agents)
}

// Unregister removes an agent from the registry by ID.
func (r *AgentRegistry) Unregister(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.agents[id]; !ok {
		return errors.New("agent not found")
	}

	delete(r.agents, id)
	return r.persist.Save(r.agents)
}

// Get returns an agent by ID, or nil if not found.
func (r *AgentRegistry) Get(id string) (*Agent, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	a, ok := r.agents[id]
	return a, ok
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

// SetStatus updates the status of an agent and persists the change.
func (r *AgentRegistry) SetStatus(id string, status Status) {
	r.mu.Lock()
	defer r.mu.Unlock()

	a, ok := r.agents[id]
	if !ok {
		return
	}
	a.Status = status
	_ = r.persist.Save(r.agents)
}

// Touch updates the LastSeenAt timestamp of an agent.
// This is called on heartbeat, SSE pings, and event frames.
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
		_ = r.persist.Save(r.agents)
	}
}

// MarkOffline marks an agent as offline and persists.
func (r *AgentRegistry) MarkOffline(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	a, ok := r.agents[id]
	if !ok {
		return
	}
	if a.Status != StatusOffline {
		a.Status = StatusOffline
		_ = r.persist.Save(r.agents)
	}
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
