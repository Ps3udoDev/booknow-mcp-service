package config

import (
	"log/slog"
	"maps"
	"strings"
	"testing"
)

const testDatabaseURL = "postgres://user:secret@localhost:5432/postgres"

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
			},
		},
		{
			name: "explicit values",
			env: map[string]string{
				"PORT": "9090", "APP_ENV": "Production", "LOG_LEVEL": "debug", "DB_MAX_CONNS": "10",
			},
			want: Config{
				Port: 9090, Env: EnvProduction, LogLevel: slog.LevelDebug,
				DatabaseURL: testDatabaseURL, DBMaxConns: 10,
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// DATABASE_URL is required, so every case starts from a valid one and may override it.
			env := map[string]string{"DATABASE_URL": testDatabaseURL}
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
