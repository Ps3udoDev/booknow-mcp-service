package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestNewUsesCloudLoggingFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		log          func(*slog.Logger)
		wantSeverity string
	}{
		{name: "debug", log: func(l *slog.Logger) { l.Debug("hello") }, wantSeverity: "DEBUG"},
		{name: "info", log: func(l *slog.Logger) { l.Info("hello") }, wantSeverity: "INFO"},
		{name: "warn maps to WARNING", log: func(l *slog.Logger) { l.Warn("hello") }, wantSeverity: "WARNING"},
		{name: "error", log: func(l *slog.Logger) { l.Error("hello") }, wantSeverity: "ERROR"},
		{name: "above error stays ERROR", log: func(l *slog.Logger) { l.Log(t.Context(), slog.LevelError+4, "hello") }, wantSeverity: "ERROR"},
		{name: "below debug stays DEBUG", log: func(l *slog.Logger) { l.Log(t.Context(), slog.LevelDebug-4, "hello") }, wantSeverity: "DEBUG"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			tt.log(New(&buf, slog.LevelDebug-4))

			var entry map[string]any
			if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
				t.Fatalf("log line is not JSON: %v (%q)", err, buf.String())
			}

			if got := entry["severity"]; got != tt.wantSeverity {
				t.Errorf("severity = %v, want %q", got, tt.wantSeverity)
			}

			if got := entry["message"]; got != "hello" {
				t.Errorf("message = %v, want %q", got, "hello")
			}

			for _, stale := range []string{"level", "msg"} {
				if _, ok := entry[stale]; ok {
					t.Errorf("entry still has %q: %v", stale, entry)
				}
			}
		})
	}
}

func TestNewKeepsGroupedAttributesNamedLikeBuiltins(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	New(&buf, slog.LevelInfo).Info("hello", slog.Group("req", slog.String("msg", "inner"), slog.String("level", "x")))

	var entry struct {
		Req map[string]string `json:"req"`
	}
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("log line is not JSON: %v", err)
	}

	if entry.Req["msg"] != "inner" || entry.Req["level"] != "x" {
		t.Errorf("grouped attributes were renamed: %v", entry.Req)
	}
}

func TestNewFiltersBelowLevel(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	New(&buf, slog.LevelInfo).Debug("hidden")

	if buf.Len() != 0 {
		t.Errorf("debug entry written at info level: %q", buf.String())
	}
}
