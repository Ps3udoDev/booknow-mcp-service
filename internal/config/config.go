// Package config loads and validates runtime configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
)

// Environment names accepted in APP_ENV.
const (
	EnvDevelopment = "development"
	EnvStaging     = "staging"
	EnvProduction  = "production"
)

// Config holds the settings the process needs to start.
// Secrets must come from the environment (Secret Manager in Cloud Run), never from files in the repo.
type Config struct {
	Port     int
	Env      string
	LogLevel slog.Level

	// DatabaseURL contains credentials: never log it or include it in errors.
	DatabaseURL string
	// DBMaxConns caps the per-instance pool; total connections = Cloud Run max_instances × DBMaxConns.
	DBMaxConns int32
}

// maxDBConns guards against a misconfigured pool exhausting Supabase connections across instances.
const maxDBConns = 50

// Load reads configuration from the environment and validates it.
func Load() (Config, error) {
	return load(os.Getenv)
}

func load(getenv func(string) string) (Config, error) {
	var errs []error

	port, err := strconv.Atoi(withDefault(getenv("PORT"), "8080"))
	if err != nil || port < 1 || port > 65535 {
		errs = append(errs, fmt.Errorf("PORT must be a valid TCP port, got %q", getenv("PORT")))
	}

	env := strings.ToLower(withDefault(getenv("APP_ENV"), EnvDevelopment))
	switch env {
	case EnvDevelopment, EnvStaging, EnvProduction:
	default:
		errs = append(errs, fmt.Errorf("APP_ENV must be one of development|staging|production, got %q", env))
	}

	var level slog.Level
	if err := level.UnmarshalText([]byte(withDefault(getenv("LOG_LEVEL"), "info"))); err != nil {
		errs = append(errs, fmt.Errorf("LOG_LEVEL: %w", err))
	}

	databaseURL := strings.TrimSpace(getenv("DATABASE_URL"))
	if databaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}

	maxConns, err := strconv.ParseInt(withDefault(getenv("DB_MAX_CONNS"), "5"), 10, 32)
	if err != nil || maxConns < 1 || maxConns > maxDBConns {
		errs = append(errs, fmt.Errorf("DB_MAX_CONNS must be between 1 and %d, got %q", maxDBConns, getenv("DB_MAX_CONNS")))
	}

	if len(errs) > 0 {
		return Config{}, errors.Join(errs...)
	}

	return Config{
		Port:        port,
		Env:         env,
		LogLevel:    level,
		DatabaseURL: databaseURL,
		DBMaxConns:  int32(maxConns),
	}, nil
}

func withDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}

	return v
}
