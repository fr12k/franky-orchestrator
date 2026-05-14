package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/franky/orchestrator/internal/agents"
	"github.com/franky/orchestrator/internal/events"
	"github.com/franky/orchestrator/internal/registry"
	"github.com/franky/orchestrator/internal/server"
)

// Build info — populated via ldflags by GoReleaser.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	// Structured logging
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	slog.Info("franky orchestrator", "version", version, "commit", commit, "built", date)

	// Signal-aware context
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Data directory
	dataDir := os.Getenv("ORCHESTRATOR_DATA_DIR")
	if dataDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			slog.Error("cannot determine home directory", "err", err)
			os.Exit(1)
		}
		dataDir = filepath.Join(home, ".franky-orchestrator")
	}

	// Persister
	persister := registry.NewPersister(filepath.Join(dataDir, "agents.json"))

	// Agent registry — load persisted agents
	reg := registry.New(persister)
	if err := reg.Load(); err != nil {
		slog.Warn("could not load persisted agents, starting fresh", "err", err)
	}

	// Event broker
	broker := events.NewBroker()

	// Agent HTTP client
	agentClient := agents.NewClient()

	// Start broker goroutine
	go broker.Run(ctx)

	// Start stale watcher
	go registry.StartStaleWatcher(ctx, reg, broker)

	// Build server
	port := os.Getenv("ORCHESTRATOR_PORT")
	if port == "" {
		port = "9000"
	}
	addr := ":" + port

	srv := server.New(addr, reg, broker, agentClient, dataDir)

	// Start HTTP server in background
	go func() {
		slog.Info("orchestrator starting", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("listen error", "err", err)
			os.Exit(1)
		}
	}()

	// Block until signal
	<-ctx.Done()
	slog.Info("shutdown signal received, draining…")

	// Graceful shutdown with 10s timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown error", "err", err)
	}

	slog.Info("orchestrator stopped")
}
