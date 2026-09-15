package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestNewPoolConfig(t *testing.T) {
	t.Parallel()

	cfg, err := newPoolConfig("postgres://user:secret@localhost:5432/postgres?sslmode=disable", 7)
	if err != nil {
		t.Fatalf("newPoolConfig() error = %v", err)
	}

	if cfg.MaxConns != 7 {
		t.Errorf("MaxConns = %d, want 7", cfg.MaxConns)
	}

	if cfg.MinConns != 0 {
		t.Errorf("MinConns = %d, want 0 so idle instances hold no connections", cfg.MinConns)
	}

	if cfg.MaxConnLifetime <= 0 || cfg.MaxConnIdleTime <= 0 {
		t.Errorf("MaxConnLifetime = %v, MaxConnIdleTime = %v; both must be bounded", cfg.MaxConnLifetime, cfg.MaxConnIdleTime)
	}

	if got := cfg.ConnConfig.RuntimeParams["application_name"]; got != applicationName {
		t.Errorf("application_name = %q, want %q", got, applicationName)
	}
}

func TestNewPoolConfigInvalidURLDoesNotLeakPassword(t *testing.T) {
	t.Parallel()

	_, err := newPoolConfig("postgres://user:supersecret@localhost:notaport/postgres", 5)
	if err == nil {
		t.Fatal("newPoolConfig() error = nil, want error")
	}

	if strings.Contains(err.Error(), "supersecret") {
		t.Errorf("error leaks password: %v", err)
	}
}

func TestNewPoolFailsWhenDatabaseUnreachable(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()

	// Port 1 on loopback refuses connections immediately.
	pool, err := NewPool(ctx, "postgres://user:supersecret@127.0.0.1:1/postgres?sslmode=disable&connect_timeout=2", 1)
	if err == nil {
		pool.Close()
		t.Fatal("NewPool() error = nil, want error for unreachable database")
	}

	if strings.Contains(err.Error(), "supersecret") {
		t.Errorf("error leaks password: %v", err)
	}
}

// TestNewPoolIntegration runs against a real Postgres (e.g. `supabase start`).
// Set TEST_DATABASE_URL=postgresql://postgres:postgres@127.0.0.1:54322/postgres to enable it.
func TestNewPoolIntegration(t *testing.T) {
	t.Parallel()

	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration test")
	}

	pool, err := NewPool(t.Context(), url, 2)
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	t.Cleanup(pool.Close)

	var one int
	if err := pool.QueryRow(t.Context(), "select $1::int", 1).Scan(&one); err != nil {
		t.Fatalf("query error = %v", err)
	}

	if one != 1 {
		t.Errorf("select returned %d, want 1", one)
	}
}
