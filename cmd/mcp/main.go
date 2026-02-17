package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/cyfernova/mcp/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		exitWithConfigError(err)
	}

	log := newLogger(cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a, err := newApp(ctx, cfg, log)
	if err != nil {
		log.Error("failed to initialize application", "error", err)
		os.Exit(1)
	}

	httpServer, err := a.newHTTPServer()
	if err != nil {
		log.Error("failed to initialize HTTP server", "error", err)
		os.Exit(1)
	}

	go func() {
		log.Info("starting MCP server",
			"listen_addr", cfg.ListenAddr,
			"backend_base_url", config.EndpointForLog(cfg.Backend.BaseURL),
			"jwks_url", config.EndpointForLog(cfg.Auth.JWKSURL),
		)
		if err := httpServer.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server exited with error", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}

	log.Info("server stopped")
}
