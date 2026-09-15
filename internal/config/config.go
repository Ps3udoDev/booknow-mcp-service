// Package config loads and validates runtime configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
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

	// SupabaseURL is the project base URL (no path); JWT issuer and JWKS URL derive from it.
	SupabaseURL string
	// JWTAudience is the required "aud" claim of Supabase access tokens.
	JWTAudience string

	// MCPPublicURL is the public origin clients use to reach this service (no path);
	// the MCP resource identifier and OAuth protected resource metadata derive from it.
	MCPPublicURL string
	// MCPAllowedOrigins lists browser origins allowed to call /mcp. Requests without Origin are not affected.
	MCPAllowedOrigins []string
	// MCPRateLimitPerMinute caps MCP requests per connection per instance.
	MCPRateLimitPerMinute int
	// MCPDraftTTL is how long an appointment draft waits for human approval before it expires.
	MCPDraftTTL time.Duration
}

const (
	// maxDBConns guards against a misconfigured pool exhausting Supabase connections across instances.
	maxDBConns = 50
	// maxRateLimitPerMinute keeps a typo from effectively disabling the MCP rate limit.
	maxRateLimitPerMinute = 10000
	// maxDraftTTLMinutes keeps drafts short-lived: the slot is not held while waiting for approval.
	maxDraftTTLMinutes = 60
)

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

	supabaseURL, err := parseOrigin(getenv("SUPABASE_URL"), env)
	if err != nil {
		errs = append(errs, fmt.Errorf("SUPABASE_URL: %w", err))
	}

	mcp, mcpErrs := loadMCP(getenv, env)
	errs = append(errs, mcpErrs...)

	if len(errs) > 0 {
		return Config{}, errors.Join(errs...)
	}

	return Config{
		Port:        port,
		Env:         env,
		LogLevel:    level,
		DatabaseURL: databaseURL,
		DBMaxConns:  int32(maxConns),
		SupabaseURL: supabaseURL,
		JWTAudience: strings.TrimSpace(withDefault(getenv("SUPABASE_JWT_AUDIENCE"), "authenticated")),

		MCPPublicURL:          mcp.MCPPublicURL,
		MCPAllowedOrigins:     mcp.MCPAllowedOrigins,
		MCPRateLimitPerMinute: mcp.MCPRateLimitPerMinute,
		MCPDraftTTL:           mcp.MCPDraftTTL,
	}, nil
}

// loadMCP reads the MCP endpoint settings into the MCP fields of a Config.
func loadMCP(getenv func(string) string, env string) (Config, []error) {
	var (
		cfg  Config
		errs []error
		err  error
	)

	if cfg.MCPPublicURL, err = parseOrigin(getenv("MCP_PUBLIC_URL"), env); err != nil {
		errs = append(errs, fmt.Errorf("MCP_PUBLIC_URL: %w", err))
	}

	if cfg.MCPAllowedOrigins, err = parseOriginList(getenv("MCP_ALLOWED_ORIGINS"), env); err != nil {
		errs = append(errs, fmt.Errorf("MCP_ALLOWED_ORIGINS: %w", err))
	}

	cfg.MCPRateLimitPerMinute, err = strconv.Atoi(withDefault(getenv("MCP_RATE_LIMIT_PER_MINUTE"), "60"))
	if err != nil || cfg.MCPRateLimitPerMinute < 1 || cfg.MCPRateLimitPerMinute > maxRateLimitPerMinute {
		errs = append(errs, fmt.Errorf("MCP_RATE_LIMIT_PER_MINUTE must be between 1 and %d, got %q",
			maxRateLimitPerMinute, getenv("MCP_RATE_LIMIT_PER_MINUTE")))
	}

	draftTTL, err := strconv.Atoi(withDefault(getenv("MCP_DRAFT_TTL_MINUTES"), "10"))
	if err != nil || draftTTL < 1 || draftTTL > maxDraftTTLMinutes {
		errs = append(errs, fmt.Errorf("MCP_DRAFT_TTL_MINUTES must be between 1 and %d, got %q",
			maxDraftTTLMinutes, getenv("MCP_DRAFT_TTL_MINUTES")))
	}

	cfg.MCPDraftTTL = time.Duration(draftTTL) * time.Minute

	return cfg, errs
}

// parseOriginList parses a comma-separated list of origins, ignoring empty entries.
func parseOriginList(raw, env string) ([]string, error) {
	var origins []string

	for item := range strings.SplitSeq(raw, ",") {
		if strings.TrimSpace(item) == "" {
			continue
		}

		origin, err := parseOrigin(item, env)
		if err != nil {
			return nil, err
		}

		origins = append(origins, origin)
	}

	return origins, nil
}

// parseOrigin accepts only a bare origin (scheme://host[:port]): security decisions are based on it,
// so anything ambiguous is rejected. Plain http is allowed only in development, for local stacks.
// The result is normalized to a lowercase host without trailing slash.
func parseOrigin(raw, env string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("is required")
	}

	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		// The raw value is not echoed: a malformed URL may carry credentials.
		return "", errors.New("must be an absolute URL like https://host.example.com")
	}

	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && env == EnvDevelopment:
	default:
		return "", fmt.Errorf("must use https (http only in %s), got scheme %q", EnvDevelopment, u.Scheme)
	}

	if u.User != nil || strings.Trim(u.Path, "/") != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("must be an origin only (no credentials, path, query or fragment), e.g. https://host.example.com")
	}

	return u.Scheme + "://" + strings.ToLower(u.Host), nil
}

func withDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}

	return v
}
