package config

import (
	"log/slog"
	"testing"
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
			want: Config{Port: 8080, Env: EnvDevelopment, LogLevel: slog.LevelInfo},
		},
		{
			name: "explicit values",
			env:  map[string]string{"PORT": "9090", "APP_ENV": "Production", "LOG_LEVEL": "debug"},
			want: Config{Port: 9090, Env: EnvProduction, LogLevel: slog.LevelDebug},
		},
		{name: "invalid port", env: map[string]string{"PORT": "abc"}, wantErr: true},
		{name: "port out of range", env: map[string]string{"PORT": "70000"}, wantErr: true},
		{name: "invalid env", env: map[string]string{"APP_ENV": "local"}, wantErr: true},
		{name: "invalid log level", env: map[string]string{"LOG_LEVEL": "loud"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := load(func(k string) string { return tt.env[k] })
			if (err != nil) != tt.wantErr {
				t.Fatalf("load() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && got != tt.want {
				t.Errorf("load() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
