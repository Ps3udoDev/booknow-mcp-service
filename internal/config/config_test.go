package config

import (
	"log/slog"
	"maps"
	"reflect"
	"strings"
	"testing"
	"time"
)

const (
	testDatabaseURL = "postgres://user:secret@localhost:5432/postgres"
	testSupabaseURL = "https://abc.supabase.co"
	testPublicURL   = "https://mcp.booknow.app"
)

func TestLoad(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		env     map[string]string
		want    Config
		wantErr bool
	}{
		{
			name: "defaults",
			env:  map[string]string{},
			want: Config{
				Port: 8080, Env: EnvDevelopment, LogLevel: slog.LevelInfo,
				DatabaseURL: testDatabaseURL, DBMaxConns: 5,
				SupabaseURL: testSupabaseURL, JWTAudience: "authenticated",
				MCPPublicURL: testPublicURL, MCPRateLimitPerMinute: 60, MCPDraftTTL: 10 * time.Minute,
			},
		},
		{
			name: "explicit values",
			env: map[string]string{
				"PORT": "9090", "APP_ENV": "Production", "LOG_LEVEL": "debug", "DB_MAX_CONNS": "10",
				"SUPABASE_URL": "https://abc.supabase.co/", "SUPABASE_JWT_AUDIENCE": "booknow-api",
				"MCP_PUBLIC_URL": "https://MCP.booknow.app/", "MCP_ALLOWED_ORIGINS": " https://claude.ai, https://ChatGPT.com/ ,",
				"MCP_RATE_LIMIT_PER_MINUTE": "120", "MCP_DRAFT_TTL_MINUTES": "15",
			},
			want: Config{
				Port: 9090, Env: EnvProduction, LogLevel: slog.LevelDebug,
				DatabaseURL: testDatabaseURL, DBMaxConns: 10,
				SupabaseURL: testSupabaseURL, JWTAudience: "booknow-api",
				MCPPublicURL: testPublicURL, MCPAllowedOrigins: []string{"https://claude.ai", "https://chatgpt.com"},
				MCPRateLimitPerMinute: 120, MCPDraftTTL: 15 * time.Minute,
			},
		},
		{
			name: "twilio webhook configured",
			env:  map[string]string{"TWILIO_AUTH_TOKEN": " secreto ", "TWILIO_WEBHOOK_URL": " https://mcp.booknow.app/webhooks/twilio "},
			want: Config{
				Port: 8080, Env: EnvDevelopment, LogLevel: slog.LevelInfo,
				DatabaseURL: testDatabaseURL, DBMaxConns: 5,
				SupabaseURL: testSupabaseURL, JWTAudience: "authenticated",
				MCPPublicURL: testPublicURL, MCPRateLimitPerMinute: 60, MCPDraftTTL: 10 * time.Minute,
				TwilioAuthToken: "secreto", TwilioWebhookURL: "https://mcp.booknow.app/webhooks/twilio",
			},
		},
		{
			name: "local http urls in development",
			env: map[string]string{
				"SUPABASE_URL": "http://127.0.0.1:54321", "MCP_PUBLIC_URL": "http://localhost:8080",
				"MCP_ALLOWED_ORIGINS": "http://localhost:6274",
			},
			want: Config{
				Port: 8080, Env: EnvDevelopment, LogLevel: slog.LevelInfo,
				DatabaseURL: testDatabaseURL, DBMaxConns: 5,
				SupabaseURL: "http://127.0.0.1:54321", JWTAudience: "authenticated",
				MCPPublicURL: "http://localhost:8080", MCPAllowedOrigins: []string{"http://localhost:6274"},
				MCPRateLimitPerMinute: 60, MCPDraftTTL: 10 * time.Minute,
			},
		},
		{name: "invalid port", env: map[string]string{"PORT": "abc"}, wantErr: true},
		{name: "port out of range", env: map[string]string{"PORT": "70000"}, wantErr: true},
		{name: "invalid env", env: map[string]string{"APP_ENV": "local"}, wantErr: true},
		{name: "invalid log level", env: map[string]string{"LOG_LEVEL": "loud"}, wantErr: true},
		{name: "missing database url", env: map[string]string{"DATABASE_URL": ""}, wantErr: true},
		{name: "blank database url", env: map[string]string{"DATABASE_URL": "   "}, wantErr: true},
		{name: "invalid max conns", env: map[string]string{"DB_MAX_CONNS": "many"}, wantErr: true},
		{name: "zero max conns", env: map[string]string{"DB_MAX_CONNS": "0"}, wantErr: true},
		{name: "max conns above limit", env: map[string]string{"DB_MAX_CONNS": "51"}, wantErr: true},
		{name: "missing supabase url", env: map[string]string{"SUPABASE_URL": ""}, wantErr: true},
		{name: "supabase url not absolute", env: map[string]string{"SUPABASE_URL": "abc.supabase.co"}, wantErr: true},
		{name: "supabase url with path", env: map[string]string{"SUPABASE_URL": "https://abc.supabase.co/auth/v1"}, wantErr: true},
		{name: "supabase url with query", env: map[string]string{"SUPABASE_URL": "https://abc.supabase.co?x=1"}, wantErr: true},
		{name: "supabase url with credentials", env: map[string]string{"SUPABASE_URL": "https://u:p@abc.supabase.co"}, wantErr: true},
		{name: "http supabase outside development", env: map[string]string{"APP_ENV": "staging", "SUPABASE_URL": "http://abc.supabase.co"}, wantErr: true},
		{name: "unsupported supabase scheme", env: map[string]string{"SUPABASE_URL": "ftp://abc.supabase.co"}, wantErr: true},
		{name: "missing mcp public url", env: map[string]string{"MCP_PUBLIC_URL": ""}, wantErr: true},
		{name: "mcp public url with path", env: map[string]string{"MCP_PUBLIC_URL": "https://mcp.booknow.app/mcp"}, wantErr: true},
		{name: "http mcp public url outside development", env: map[string]string{"APP_ENV": "production", "MCP_PUBLIC_URL": "http://mcp.booknow.app"}, wantErr: true},
		{name: "allowed origin without scheme", env: map[string]string{"MCP_ALLOWED_ORIGINS": "claude.ai"}, wantErr: true},
		{name: "allowed origin with path", env: map[string]string{"MCP_ALLOWED_ORIGINS": "https://claude.ai/chat"}, wantErr: true},
		{name: "wildcard allowed origin", env: map[string]string{"MCP_ALLOWED_ORIGINS": "*"}, wantErr: true},
		{name: "http allowed origin outside development", env: map[string]string{"APP_ENV": "staging", "MCP_ALLOWED_ORIGINS": "http://claude.ai"}, wantErr: true},
		{name: "invalid rate limit", env: map[string]string{"MCP_RATE_LIMIT_PER_MINUTE": "lots"}, wantErr: true},
		{name: "zero rate limit", env: map[string]string{"MCP_RATE_LIMIT_PER_MINUTE": "0"}, wantErr: true},
		{name: "rate limit above ceiling", env: map[string]string{"MCP_RATE_LIMIT_PER_MINUTE": "10001"}, wantErr: true},
		{name: "twilio token without url", env: map[string]string{"TWILIO_AUTH_TOKEN": "secreto"}, wantErr: true},
		{name: "twilio url without token", env: map[string]string{"TWILIO_WEBHOOK_URL": "https://mcp.booknow.app/webhooks/twilio"}, wantErr: true},
		{name: "twilio url without path", env: map[string]string{"TWILIO_AUTH_TOKEN": "secreto", "TWILIO_WEBHOOK_URL": "https://mcp.booknow.app"}, wantErr: true},
		{name: "twilio url not absolute", env: map[string]string{"TWILIO_AUTH_TOKEN": "secreto", "TWILIO_WEBHOOK_URL": "/webhooks/twilio"}, wantErr: true},
		{name: "http twilio url outside development", env: map[string]string{"APP_ENV": "production", "MCP_PUBLIC_URL": "https://mcp.booknow.app", "TWILIO_AUTH_TOKEN": "secreto", "TWILIO_WEBHOOK_URL": "http://mcp.booknow.app/webhooks/twilio"}, wantErr: true},
		{name: "invalid draft ttl", env: map[string]string{"MCP_DRAFT_TTL_MINUTES": "diez"}, wantErr: true},
		{name: "zero draft ttl", env: map[string]string{"MCP_DRAFT_TTL_MINUTES": "0"}, wantErr: true},
		{name: "draft ttl above an hour", env: map[string]string{"MCP_DRAFT_TTL_MINUTES": "61"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Required variables start valid; each case may override them.
			env := map[string]string{"DATABASE_URL": testDatabaseURL, "SUPABASE_URL": testSupabaseURL, "MCP_PUBLIC_URL": testPublicURL}
			maps.Copy(env, tt.env)

			got, err := load(func(k string) string { return env[k] })
			if (err != nil) != tt.wantErr {
				t.Fatalf("load() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("load() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestLoadErrorDoesNotLeakDatabaseURL(t *testing.T) {
	t.Parallel()

	env := map[string]string{"DATABASE_URL": testDatabaseURL, "DB_MAX_CONNS": "0", "PORT": "abc"}

	_, err := load(func(k string) string { return env[k] })
	if err == nil {
		t.Fatal("load() error = nil, want error")
	}

	if strings.Contains(err.Error(), "secret") {
		t.Errorf("load() error leaks connection string: %v", err)
	}
}

func TestLoadErrorDoesNotLeakTwilioToken(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		"DATABASE_URL": testDatabaseURL, "SUPABASE_URL": testSupabaseURL, "MCP_PUBLIC_URL": testPublicURL,
		"TWILIO_AUTH_TOKEN": "super-secreto", "TWILIO_WEBHOOK_URL": "notaurl",
	}

	_, err := load(func(k string) string { return env[k] })
	if err == nil {
		t.Fatal("load() error = nil, want error")
	}

	if strings.Contains(err.Error(), "super-secreto") {
		t.Errorf("load() error leaks the Twilio auth token: %v", err)
	}
}
