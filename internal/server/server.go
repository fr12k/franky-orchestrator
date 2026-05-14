package server

import (
	"net/http"

	"github.com/franky/orchestrator/internal/agents"
	"github.com/franky/orchestrator/internal/events"
	"github.com/franky/orchestrator/internal/registry"
)

// Server wraps the HTTP server with all dependencies.
type Server struct {
	registry    *registry.AgentRegistry
	broker      *events.Broker
	agentClient *agents.Client
	dataDir     string
}

// New creates a configured HTTP server.
func New(addr string, reg *registry.AgentRegistry, broker *events.Broker, agentClient *agents.Client, dataDir string) *http.Server {
	s := &Server{
		registry:    reg,
		broker:      broker,
		agentClient: agentClient,
		dataDir:     dataDir,
	}

	mux := http.NewServeMux()
	s.registerRoutes(mux)

	return &http.Server{
		Addr:    addr,
		Handler: withMiddleware(mux),
	}
}
