package server

import (
	"context"
	"net/http"
	"sync"

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

	// HTTPServer is the underlying net/http server. Callers use
	// ListenAndServe / Shutdown via this field.
	HTTPServer *http.Server

	// consumerCancels maps apiURL -> cancel func so that when an agent
	// re-registers at the same URL we can stop the old SSE consumer.
	consumerCancels map[string]context.CancelFunc
	consumerMu      sync.Mutex

	// shutdownCtx is the top-level context that is cancelled on SIGINT/SIGTERM.
	// SSE consumer contexts are derived from this so they stop on shutdown.
	shutdownCtx context.Context
}

// New creates a configured Server.
func New(addr string, reg *registry.AgentRegistry, broker *events.Broker, agentClient *agents.Client, dataDir string) *Server {
	return NewWithContext(context.Background(), addr, reg, broker, agentClient, dataDir)
}

// NewWithContext creates a configured Server with a top-level shutdown context.
// SSE consumer contexts are derived from shutdownCtx so they stop on shutdown.
func NewWithContext(shutdownCtx context.Context, addr string, reg *registry.AgentRegistry, broker *events.Broker, agentClient *agents.Client, dataDir string) *Server {
	s := &Server{
		registry:        reg,
		broker:          broker,
		agentClient:     agentClient,
		dataDir:         dataDir,
		consumerCancels: make(map[string]context.CancelFunc),
		shutdownCtx:     shutdownCtx,
	}

	mux := http.NewServeMux()
	s.registerRoutes(mux)

	s.HTTPServer = &http.Server{
		Addr:    addr,
		Handler: withMiddleware(mux),
	}

	return s
}

// ListenAndServe starts the HTTP server.
func (s *Server) ListenAndServe() error {
	return s.HTTPServer.ListenAndServe()
}

// Shutdown gracefully shuts down the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.HTTPServer.Shutdown(ctx)
}

// cancelConsumer stops the SSE consumer for the given apiURL, if any.
func (s *Server) cancelConsumer(apiURL string) {
	s.consumerMu.Lock()
	defer s.consumerMu.Unlock()

	if s.consumerCancels == nil {
		return
	}
	if cancel, ok := s.consumerCancels[apiURL]; ok {
		cancel()
		delete(s.consumerCancels, apiURL)
	}
}
