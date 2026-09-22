// Package logging builds the service's JSON logger in the shape Cloud Logging understands.
package logging

import (
	"io"
	"log/slog"
)

// New returns a JSON logger whose entries use the fields Cloud Logging parses from stdout:
// "severity" (DEBUG, INFO, WARNING, ERROR) and "message". With slog's default "level" and "msg"
// every entry would land with DEFAULT severity and error alerts could not filter on it.
func New(w io.Writer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level, ReplaceAttr: cloudLoggingAttr}))
}

func cloudLoggingAttr(groups []string, a slog.Attr) slog.Attr {
	// Only the top-level built-ins are renamed; a grouped attribute that happens to be called "msg" is data.
	if len(groups) > 0 {
		return a
	}

	switch a.Key {
	case slog.MessageKey:
		a.Key = "message"
	case slog.LevelKey:
		a.Key = "severity"

		if lvl, ok := a.Value.Any().(slog.Level); ok {
			a.Value = slog.StringValue(severity(lvl))
		}
	}

	return a
}

func severity(lvl slog.Level) string {
	switch {
	case lvl >= slog.LevelError:
		return "ERROR"
	case lvl >= slog.LevelWarn:
		return "WARNING"
	case lvl >= slog.LevelInfo:
		return "INFO"
	default:
		return "DEBUG"
	}
}
