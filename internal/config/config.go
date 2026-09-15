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
}

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

	if len(errs) > 0 {
		return Config{}, errors.Join(errs...)
	}

	return Config{Port: port, Env: env, LogLevel: level}, nil
}

func withDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}

	return v
}
