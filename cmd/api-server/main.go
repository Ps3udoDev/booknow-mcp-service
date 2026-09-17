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

	// Embed the IANA timezone database: tenant and branch timezones must resolve in any container image.
	_ "time/tzdata"

	"github.com/Ps3udoDev/booknow-mcp-service/internal/application/business"
	"github.com/Ps3udoDev/booknow-mcp-service/internal/application/drafts"
	"github.com/Ps3udoDev/booknow-mcp-service/internal/auth"
	"github.com/Ps3udoDev/booknow-mcp-service/internal/config"
	"github.com/Ps3udoDev/booknow-mcp-service/internal/httpapi"
	"github.com/Ps3udoDev/booknow-mcp-service/internal/integration/twilio"
	"github.com/Ps3udoDev/booknow-mcp-service/internal/mcpserver"
	"github.com/Ps3udoDev/booknow-mcp-service/internal/platform/ratelimit"
	"github.com/Ps3udoDev/booknow-mcp-service/internal/repository/postgres"
	"github.com/Ps3udoDev/booknow-mcp-service/internal/tenant"
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

	// ctx (process lifetime) bounds the JWKS background refresh; a JWKS fetch failure aborts startup.
	verifier, err := auth.NewVerifier(ctx, logger, cfg.SupabaseURL, cfg.JWTAudience)
	if err != nil {
		return fmt.Errorf("init token verifier: %w", err)
	}

	accessStore := postgres.NewMCPAccessStore(pool)
	businessService := business.NewService(postgres.NewBusinessStore(pool))

	deps := httpapi.Deps{
		DB: pool,
		MCP: httpapi.MCPConfig{
			PublicURL:           cfg.MCPPublicURL,
			AuthorizationServer: auth.IssuerURL(cfg.SupabaseURL),
			AllowedOrigins:      cfg.MCPAllowedOrigins,
			Tokens:              verifier,
			Access:              tenant.NewResolver(accessStore),
			RateLimit:           ratelimit.New(cfg.MCPRateLimitPerMinute),
			Usage:               accessStore,
			Tools: mcpserver.Deps{
				Business: businessService,
				Drafts:   drafts.NewService(postgres.NewDraftStore(pool), businessService, cfg.MCPDraftTTL),
				Audit:    postgres.NewAuditStore(pool),
				Logger:   logger,
			},
		},
	}

	if cfg.TwilioAuthToken != "" {
		validator, err := twilio.NewValidator(cfg.TwilioAuthToken, cfg.TwilioWebhookURL)
		if err != nil {
			return fmt.Errorf("init twilio webhook: %w", err)
		}

		deps.Twilio = httpapi.TwilioConfig{Validator: validator, AccountSID: cfg.TwilioAccountSID}
	}

	// /mcp is stateless and answers with JSON (no long-lived SSE stream), so regular timeouts are safe.
	srv := &http.Server{
		Addr:              net.JoinHostPort("", strconv.Itoa(cfg.Port)),
		Handler:           httpapi.NewRouter(logger, deps),
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
