// Package app wires config, storage, engine, and HTTP API for gfire server.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/hrodrig/gfire/internal/api"
	"github.com/hrodrig/gfire/internal/config"
	"github.com/hrodrig/gfire/internal/engine"
	"github.com/hrodrig/gfire/internal/handler"
)

// RunServer starts storage, engine, and REST API until SIGINT/SIGTERM.
func RunServer(ctx context.Context, cfg *config.Config) error {
	serverID := cfg.Server.ServerID
	if serverID == "" {
		hostname, _ := os.Hostname()
		serverID = hostname
	}

	LogStartup(cfg, serverID)

	store, err := config.OpenStorage(ctx, cfg.Storage, serverID)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer store.Close()

	reg := cfg.HandlerRegistry()
	runner := handler.Runner(reg)
	if len(cfg.Handlers) == 0 {
		runner = handler.EchoRunner{}
	}

	logger := slog.Default()
	eng := engine.New(store, cfg.EngineConfig(serverID), runner, logger)
	if err := eng.Start(ctx); err != nil {
		return fmt.Errorf("start engine: %w", err)
	}

	apiSrv := api.NewServer(cfg, store, eng)
	httpServer := &http.Server{
		Addr:    cfg.ListenAddr(),
		Handler: apiSrv.Handler(),
	}

	errCh := make(chan error, 1)
	go func() {
		base := BaseURL(cfg)
		logger.Info("listening",
			"url", base,
			"api", base+"/v1",
			"bind", cfg.ListenAddr(),
		)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	select {
	case err := <-errCh:
		return err
	case sig := <-sigCh:
		logger.Info("shutdown signal", "signal", sig.String())
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
	return eng.Stop(shutdownCtx)
}
