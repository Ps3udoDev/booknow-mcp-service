// Package httpapi wires the HTTP transport: router, middleware and handlers.
// Business rules live in application packages; handlers only translate HTTP <-> use cases.
package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// NewRouter builds the root handler.
// Route groups to add as features land: /v1 (REST), /webhooks/twilio, /mcp (Streamable HTTP).
func NewRouter(logger *slog.Logger) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(requestLogger(logger))

	r.Get("/healthz", healthz)

	return r
}

// healthz must stay cheap and must not reveal version, config or dependency state.
func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}
