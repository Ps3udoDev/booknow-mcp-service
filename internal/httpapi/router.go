// Package httpapi wires the HTTP transport: router, middleware and handlers.
// Business rules live in application packages; handlers only translate HTTP <-> use cases.
package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// readinessTimeout keeps /readyz well under Cloud Run's probe timeout when the database hangs.
const readinessTimeout = 2 * time.Second

// Pinger checks that a dependency required to serve traffic is reachable.
type Pinger interface {
	Ping(ctx context.Context) error
}

// NewRouter builds the root handler.
// Route groups to add as features land: /v1 (REST), /webhooks/twilio, /mcp (Streamable HTTP).
func NewRouter(logger *slog.Logger, db Pinger) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(requestLogger(logger))

	r.Get("/healthz", healthz)
	r.Get("/readyz", readyz(logger, db))

	return r
}

// healthz must stay cheap and must not reveal version, config or dependency state.
func healthz(w http.ResponseWriter, _ *http.Request) {
	writePlain(w, http.StatusOK, "ok\n")
}

// readyz reports whether the database is reachable. Failure details go to the logs, never to the response.
func readyz(logger *slog.Logger, db Pinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
		defer cancel()

		if err := db.Ping(ctx); err != nil {
			logger.WarnContext(r.Context(), "readiness check failed", slog.String("dependency", "postgres"), slog.Any("error", err))
			writePlain(w, http.StatusServiceUnavailable, "unavailable\n")

			return
		}

		writePlain(w, http.StatusOK, "ok\n")
	}
}

func writePlain(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}
