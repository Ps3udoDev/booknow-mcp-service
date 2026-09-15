// Command api-server runs the BookNow dedicated backend (REST, Twilio webhooks and MCP).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/Ps3udoDev/booknow-mcp-service/internal/config"
	"github.com/Ps3udoDev/booknow-mcp-service/internal/httpapi"
	"github.com/Ps3udoDev/booknow-mcp-service/internal/repository/postgres"
)

// Cloud Run sends SIGTERM and waits 10s before SIGKILL; leave margin.
const shutdownTimeout = 8 * time.Second

// dbStartupTimeout bounds the initial connect+ping so a bad DATABASE_URL fails the revision fast.
const dbStartupTimeout = 10 * time.Second

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "api-server: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	startupCtx, cancelStartup := context.WithTimeout(ctx, dbStartupTimeout)
	defer cancelStartup()

	pool, err := postgres.NewPool(startupCtx, cfg.DatabaseURL, cfg.DBMaxConns)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	// Closed after srv.Shutdown returns (defers run LIFO), so in-flight requests keep their connections.
	defer pool.Close()

	// WriteTimeout is intentionally generous; the MCP SSE route will need its own
	// per-request deadline handling (http.ResponseController) instead of a short global limit.
	srv := &http.Server{
		Addr:              net.JoinHostPort("", strconv.Itoa(cfg.Port)),
		Handler:           httpapi.NewRouter(logger, pool),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}

	serveErr := make(chan error, 1)

	go func() {
		logger.Info("server starting", slog.Int("port", cfg.Port), slog.String("env", cfg.Env))

		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}

		close(serveErr)
	}()

	select {
	case err := <-serveErr:
		return fmt.Errorf("listen: %w", err)
	case <-ctx.Done():
	}

	logger.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}

	logger.Info("server stopped")

	return nil
}
