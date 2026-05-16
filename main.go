package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/pprof"
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

	// Start batched persistence flush loop (every 5 seconds)
	reg.StartFlushLoop(ctx, 5*time.Second)
	defer reg.StopFlushLoop()

	// Event broker
	broker := events.NewBroker()

	// Agent HTTP client
	agentClient := agents.NewClient()

	// Start broker goroutine
	go broker.Run(ctx)

	// Start stale watcher
	go registry.StartStaleWatcher(ctx, reg, broker)

	// Start usage poller
	go server.StartUsagePoller(ctx, reg, agentClient, broker)

	// Build server
	port := os.Getenv("ORCHESTRATOR_PORT")
	if port == "" {
		port = "9000"
	}
	addr := ":" + port

	srv := server.NewWithContext(ctx, addr, reg, broker, agentClient, dataDir)

	// Register pprof debug endpoints on a separate mux so they are not
	// exposed via the main HTTP server (which is agent-facing). The debug
	// mux listens on a different port (9001 by default, overridable via
	// ORCHESTRATOR_DEBUG_ADDR).
	debugAddr := os.Getenv("ORCHESTRATOR_DEBUG_ADDR")
	if debugAddr == "" {
		debugAddr = ":9001"
	}
	debugMux := http.NewServeMux()
	debugMux.HandleFunc("/debug/pprof/", pprof.Index)
	debugMux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	debugMux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	debugMux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	debugMux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	debugMux.Handle("/debug/pprof/heap", pprof.Handler("heap"))
	debugMux.Handle("/debug/pprof/goroutine", pprof.Handler("goroutine"))
	debugMux.Handle("/debug/pprof/threadcreate", pprof.Handler("threadcreate"))
	debugMux.Handle("/debug/pprof/block", pprof.Handler("block"))
	debugMux.Handle("/debug/pprof/mutex", pprof.Handler("mutex"))
	debugServer := &http.Server{Addr: debugAddr, Handler: debugMux}
	go func() {
		slog.Info("pprof debug endpoints", "addr", debugAddr)
		if err := debugServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Warn("pprof debug server closed", "err", err)
		}
	}()

	// Reconnect to previously-registered agents from disk
	// Starts SSE consumers for each stored agent URL (must be after srv
	// creation because ReconnectToStoredAgents is a method on Server that
	// manages context-cancellation of per-URL SSE consumers).
	srv.ReconnectToStoredAgents()

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

	// Shutdown pprof debug server
	if err := debugServer.Shutdown(shutdownCtx); err != nil {
		slog.Warn("pprof debug server shutdown", "err", err)
	}

	slog.Info("orchestrator stopped")
}
