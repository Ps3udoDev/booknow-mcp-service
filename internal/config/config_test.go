package config

import (
	"log/slog"
	"maps"
	"strings"
	"testing"
)

const (
	testDatabaseURL = "postgres://user:secret@localhost:5432/postgres"
	testSupabaseURL = "https://abc.supabase.co"
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
			},
		},
		{
			name: "explicit values",
			env: map[string]string{
				"PORT": "9090", "APP_ENV": "Production", "LOG_LEVEL": "debug", "DB_MAX_CONNS": "10",
				"SUPABASE_URL": "https://abc.supabase.co/", "SUPABASE_JWT_AUDIENCE": "booknow-api",
			},
			want: Config{
				Port: 9090, Env: EnvProduction, LogLevel: slog.LevelDebug,
				DatabaseURL: testDatabaseURL, DBMaxConns: 10,
				SupabaseURL: testSupabaseURL, JWTAudience: "booknow-api",
			},
		},
		{
			name: "local http supabase in development",
			env:  map[string]string{"SUPABASE_URL": "http://127.0.0.1:54321"},
			want: Config{
				Port: 8080, Env: EnvDevelopment, LogLevel: slog.LevelInfo,
				DatabaseURL: testDatabaseURL, DBMaxConns: 5,
				SupabaseURL: "http://127.0.0.1:54321", JWTAudience: "authenticated",
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Required variables start valid; each case may override them.
			env := map[string]string{"DATABASE_URL": testDatabaseURL, "SUPABASE_URL": testSupabaseURL}
			maps.Copy(env, tt.env)

			got, err := load(func(k string) string { return env[k] })
			if (err != nil) != tt.wantErr {
				t.Fatalf("load() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && got != tt.want {
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
