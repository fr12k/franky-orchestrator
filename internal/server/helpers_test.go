package server

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/franky/orchestrator/internal/agents"
	"github.com/franky/orchestrator/internal/events"
	"github.com/franky/orchestrator/internal/registry"
)

// newTestServer creates a Server with fresh registry + broker for testing.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	reg := registry.New(registry.NewPersister(filepath.Join(t.TempDir(), "test.json")))
	if err := reg.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	broker := events.NewBroker()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go broker.Run(ctx)
	agentClient := agents.NewClient()
	return &Server{
		registry:        reg,
		broker:          broker,
		agentClient:     agentClient,
		dataDir:         t.TempDir(),
		shutdownCtx:     ctx,
		consumerCancels: make(map[string]context.CancelFunc),
	}
}
