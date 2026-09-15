// Package postgres provides Postgres (Supabase) access through a pgx connection pool.
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// applicationName identifies this service in pg_stat_activity and Supabase dashboards.
const applicationName = "booknow-mcp-service"

const (
	// Recycling connections lets the pool follow Supabase pooler failovers and DNS changes.
	maxConnLifetime = 30 * time.Minute
	// Idle connections are released so scaled-down Cloud Run instances stop holding Supabase slots.
	maxConnIdleTime   = 5 * time.Minute
	healthCheckPeriod = time.Minute
)

// NewPool creates a pool and verifies connectivity with a ping, so a bad DATABASE_URL fails at startup.
// Errors never include the password: pgx redacts it when reporting parse and connect failures.
func NewPool(ctx context.Context, databaseURL string, maxConns int32) (*pgxpool.Pool, error) {
	cfg, err := newPoolConfig(databaseURL, maxConns)
	if err != nil {
		return nil, err
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()

		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return pool, nil
}

func newPoolConfig(databaseURL string, maxConns int32) (*pgxpool.Config, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}

	cfg.MaxConns = maxConns
	cfg.MinConns = 0
	cfg.MaxConnLifetime = maxConnLifetime
	cfg.MaxConnIdleTime = maxConnIdleTime
	cfg.HealthCheckPeriod = healthCheckPeriod
	cfg.ConnConfig.RuntimeParams["application_name"] = applicationName

	return cfg, nil
}
