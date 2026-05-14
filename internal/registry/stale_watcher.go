package registry

import (
	"context"
	"log/slog"
	"time"
)

// Broker is the minimal interface for publishing events from the stale watcher.
type Broker interface {
	PublishOrchestratorEvent(kind string, data map[string]any)
}

// StartStaleWatcher periodically checks for agents whose LastSeenAt exceeds
// the timeout and marks them offline. Runs until ctx is cancelled.
func StartStaleWatcher(ctx context.Context, reg *AgentRegistry, broker Broker) {
	startStaleWatcher(ctx, reg, broker, 5*time.Minute, 30*time.Second)
}

// startStaleWatcher is the implementation with configurable timeouts (for testing).
func startStaleWatcher(ctx context.Context, reg *AgentRegistry, broker Broker, staleTimeout, tickInterval time.Duration) {
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	slog.Info("stale watcher started", "timeout", staleTimeout, "interval", tickInterval)

	for {
		select {
		case <-ctx.Done():
			slog.Info("stale watcher stopped")
			return
		case <-ticker.C:
			staleIDs := reg.FindStale(staleTimeout)
			for _, id := range staleIDs {
				reg.MarkOffline(id)
				slog.Warn("agent marked offline due to inactivity", "agentId", id)

				if broker != nil {
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
			}
		}
	}
}


