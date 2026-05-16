package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/franky/orchestrator/internal/agents"
	"github.com/franky/orchestrator/internal/events"
	"github.com/franky/orchestrator/internal/registry"
)

// usageResponse is the JSON shape returned by an agent's GET /usage endpoint.
type usageResponse struct {
	MessageCount int            `json:"messageCount"`
	TurnCount    int            `json:"turnCount"`
	TokensIn     int            `json:"tokensIn"`
	TokensOut    int            `json:"tokensOut"`
	ToolStats    map[string]int `json:"toolStats"`
}

// StartUsagePoller periodically fetches /usage from every non-offline agent and
// updates the registry + broadcasts agent_usage events. Runs until ctx is cancelled.
func StartUsagePoller(ctx context.Context, reg *registry.AgentRegistry, client *agents.Client, broker *events.Broker) {
	startUsagePoller(ctx, reg, client, broker, 15*time.Second)
}

// startUsagePoller is the implementation with configurable interval (for testing).
func startUsagePoller(ctx context.Context, reg *registry.AgentRegistry, client *agents.Client, broker *events.Broker, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	slog.Info("usage poller started", "interval", interval)

	for {
		select {
		case <-ctx.Done():
			slog.Info("usage poller stopped")
			return
		case <-ticker.C:
			pollUsage(ctx, reg, client, broker)
		}
	}
}

// pollUsage fetches usage from all non-offline agents and updates the registry.
func pollUsage(ctx context.Context, reg *registry.AgentRegistry, client *agents.Client, broker *events.Broker) {
	agents := reg.List()
	for _, a := range agents {
		if a.Status == registry.StatusOffline {
			continue
		}

		body, err := client.GetUsage(a.APIURL)
		if err != nil {
			slog.Debug("usage poll failed for agent", "agentId", a.ID, "err", err)
			continue
		}

		var usage usageResponse
		if err := json.Unmarshal(body, &usage); err != nil {
			slog.Debug("usage poll parse failed for agent", "agentId", a.ID, "err", err)
			continue
		}

		reg.UpdateUsage(a.ID, usage.MessageCount, usage.TurnCount, usage.TokensIn, usage.TokensOut, usage.ToolStats)

		if broker != nil {
			// Re-fetch the agent to get the latest state, then publish
			if updated, ok := reg.Get(a.ID); ok {
				m := agentToMap(updated)
				m["agentId"] = a.ID
				broker.PublishOrchestratorEvent("agent_usage", m)
			}
		}
	}
}
